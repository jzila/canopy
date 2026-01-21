// Package tui provides remote client functionality for connecting to a running daemon.
package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jzila/canopy/pkg/daemon"
	"github.com/jzila/canopy/pkg/events"
)

// RemoteClient connects to a running daemon via HTTP/WebSocket
type RemoteClient struct {
	baseURL   string
	wsURL     string
	conn      *websocket.Conn
	state     *daemon.RuntimeState
	eventBus  *events.EventBus
	mu        sync.RWMutex
	done      chan struct{}
	connected bool
}

// NewRemoteClient creates a new remote client for the given daemon address
func NewRemoteClient(addr string) *RemoteClient {
	// Parse the address - it can be just a port number, host:port, or full URL
	baseURL := normalizeAddr(addr)
	wsURL := httpToWS(baseURL)

	state := daemon.NewRuntimeState()
	eventBus := events.NewEventBus()

	// Subscribe state to eventBus so WebSocket events update the state
	// This mirrors what daemon.go does for in-process mode
	_ = state.SubscribeToEventBus(eventBus)

	return &RemoteClient{
		baseURL:  baseURL,
		wsURL:    wsURL,
		state:    state,
		eventBus: eventBus,
		done:     make(chan struct{}),
	}
}

// normalizeAddr converts various address formats to a base HTTP URL
func normalizeAddr(addr string) string {
	// Try parsing as URL first - but only if it looks like a URL with scheme
	if strings.Contains(addr, "://") {
		if u, err := url.Parse(addr); err == nil && u.Scheme != "" && u.Host != "" {
			return fmt.Sprintf("%s://%s", u.Scheme, u.Host)
		}
	}

	// Check if it's just a port number
	var port int
	if _, err := fmt.Sscanf(addr, "%d", &port); err == nil && !strings.Contains(addr, ".") && !strings.Contains(addr, ":") {
		return fmt.Sprintf("http://localhost:%d", port)
	}

	// Assume host:port format
	return fmt.Sprintf("http://%s", addr)
}

// httpToWS converts an HTTP URL to a WebSocket URL
func httpToWS(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}

	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = "/ws"
	return u.String()
}

// Connect establishes connection to the daemon
// First fetches initial state via HTTP, then connects WebSocket for updates
func (c *RemoteClient) Connect() error {
	// Fetch initial state
	if err := c.fetchInitialState(); err != nil {
		return fmt.Errorf("failed to fetch initial state: %w", err)
	}

	// Connect WebSocket
	if err := c.connectWebSocket(); err != nil {
		return fmt.Errorf("failed to connect websocket: %w", err)
	}

	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()

	return nil
}

// fetchInitialState fetches the current state from the daemon's HTTP API
func (c *RemoteClient) fetchInitialState() error {
	stateURL := c.baseURL + "/api/state"

	resp, err := http.Get(stateURL)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("server returned %d (failed to read body: %v)", resp.StatusCode, err)
		}
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var stateResp daemon.RuntimeState
	if err := json.NewDecoder(resp.Body).Decode(&stateResp); err != nil {
		return fmt.Errorf("failed to decode state: %w", err)
	}

	// Update local state
	c.mu.Lock()
	c.state.IsPaused = stateResp.IsPaused
	c.state.StartTime = stateResp.StartTime
	c.state.Stats = stateResp.Stats

	// Copy agents (use GetSnapshot to avoid copying the mutex)
	for id, agent := range stateResp.Agents {
		agentCopy := agent.GetSnapshot()
		c.state.Agents[id] = &agentCopy
	}

	// Copy tasks
	for id, task := range stateResp.Tasks {
		taskCopy := *task
		c.state.Tasks[id] = &taskCopy
	}
	c.mu.Unlock()

	return nil
}

// connectWebSocket establishes WebSocket connection for real-time updates
func (c *RemoteClient) connectWebSocket() error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(c.wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	c.conn = conn

	// Start reading messages in background
	go c.readLoop()

	return nil
}

// readLoop reads messages from WebSocket and updates state
func (c *RemoteClient) readLoop() {
	defer func() {
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()
	}()

	for {
		select {
		case <-c.done:
			return
		default:
		}

		// Read message with timeout
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			// Any error (including unexpected close) terminates the read loop
			return
		}

		// Parse event
		var event events.Event
		if err := json.Unmarshal(message, &event); err != nil {
			continue
		}

		// Handle the event
		c.handleEvent(event)

		// Publish to local event bus for TUI to receive
		c.eventBus.Publish(event)
	}
}

// handleEvent processes an event and updates the local state
func (c *RemoteClient) handleEvent(event events.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Handle state sync event specially - it replaces the entire state
	if event.Type == events.EventStateSync {
		if stateData, ok := event.Payload.(map[string]interface{}); ok {
			c.applyStateSync(stateData)
		}
		return
	}

	// For other events, let the RuntimeState handle it through its subscription
	// The state's handleEvent will be called via eventBus subscription
}

// applyStateSync applies a full state sync from the server
func (c *RemoteClient) applyStateSync(data map[string]interface{}) {
	// Parse agents
	if agentsData, ok := data["agents"].(map[string]interface{}); ok {
		for id, agentData := range agentsData {
			if agentMap, ok := agentData.(map[string]interface{}); ok {
				agent := c.parseAgentState(agentMap)
				if agent != nil {
					agent.ID = id
					c.state.Agents[id] = agent
				}
			}
		}
	}

	// Parse tasks
	if tasksData, ok := data["tasks"].(map[string]interface{}); ok {
		for id, taskData := range tasksData {
			if taskMap, ok := taskData.(map[string]interface{}); ok {
				task := c.parseTaskState(taskMap)
				if task != nil {
					task.ID = id
					c.state.Tasks[id] = task
				}
			}
		}
	}

	// Parse stats
	if statsData, ok := data["stats"].(map[string]interface{}); ok {
		c.state.Stats = c.parseStats(statsData)
	}

	// Parse paused state
	if paused, ok := data["is_paused"].(bool); ok {
		c.state.IsPaused = paused
	}
}

// parseAgentState parses agent state from a map
func (c *RemoteClient) parseAgentState(data map[string]interface{}) *daemon.AgentState {
	agent := &daemon.AgentState{}

	if id, ok := data["id"].(string); ok {
		agent.ID = id
	}
	if taskID, ok := data["task_id"].(string); ok {
		agent.TaskID = taskID
	}
	if taskTitle, ok := data["task_title"].(string); ok {
		agent.TaskTitle = taskTitle
	}
	if status, ok := data["status"].(string); ok {
		agent.Status = daemon.AgentStatus(status)
	}
	if mergeStatus, ok := data["merge_status"].(string); ok {
		agent.MergeStatus = daemon.MergeStatus(mergeStatus)
	}
	if numTurns, ok := data["num_turns"].(float64); ok {
		agent.NumTurns = int(numTurns)
	}
	if duration, ok := data["duration"].(float64); ok {
		agent.Duration = duration
	}
	if commits, ok := data["commits"].(float64); ok {
		agent.Commits = int(commits)
	}

	// Parse start_time
	if startTimeStr, ok := data["start_time"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, startTimeStr); err == nil {
			agent.StartTime = t
		}
	}

	// Parse git_commits
	if gitCommits, ok := data["git_commits"].([]interface{}); ok {
		for _, gc := range gitCommits {
			if gcMap, ok := gc.(map[string]interface{}); ok {
				commit := daemon.GitCommit{}
				if hash, ok := gcMap["hash"].(string); ok {
					commit.Hash = hash
				}
				if shortHash, ok := gcMap["short_hash"].(string); ok {
					commit.ShortHash = shortHash
				}
				if message, ok := gcMap["message"].(string); ok {
					commit.Message = message
				}
				agent.GitCommits = append(agent.GitCommits, commit)
			}
		}
	}

	// Parse token_usage
	if tokenUsage, ok := data["token_usage"].(map[string]interface{}); ok {
		if inputTokens, ok := tokenUsage["input_tokens"].(float64); ok {
			agent.TokenUsage.InputTokens = int(inputTokens)
		}
		if outputTokens, ok := tokenUsage["output_tokens"].(float64); ok {
			agent.TokenUsage.OutputTokens = int(outputTokens)
		}
		if costUSD, ok := tokenUsage["cost_usd"].(float64); ok {
			agent.TokenUsage.CostUSD = costUSD
		}
	}

	return agent
}

// parseTaskState parses task state from a map
func (c *RemoteClient) parseTaskState(data map[string]interface{}) *daemon.TaskState {
	task := &daemon.TaskState{}

	if id, ok := data["id"].(string); ok {
		task.ID = id
	}
	if title, ok := data["title"].(string); ok {
		task.Title = title
	}
	if status, ok := data["status"].(string); ok {
		task.Status = status
	}
	if priority, ok := data["priority"].(float64); ok {
		task.Priority = int(priority)
	}

	return task
}

// parseStats parses stats from a map
func (c *RemoteClient) parseStats(data map[string]interface{}) daemon.Stats {
	stats := daemon.Stats{}

	if v, ok := data["total_tasks"].(float64); ok {
		stats.TotalTasks = int(v)
	}
	if v, ok := data["completed_tasks"].(float64); ok {
		stats.CompletedTasks = int(v)
	}
	if v, ok := data["failed_tasks"].(float64); ok {
		stats.FailedTasks = int(v)
	}
	if v, ok := data["running_tasks"].(float64); ok {
		stats.RunningTasks = int(v)
	}
	if v, ok := data["total_input_tokens"].(float64); ok {
		stats.TotalInputTokens = int(v)
	}
	if v, ok := data["total_output_tokens"].(float64); ok {
		stats.TotalOutputTokens = int(v)
	}
	if v, ok := data["total_cache_read_tokens"].(float64); ok {
		stats.TotalCacheReadTokens = int(v)
	}
	if v, ok := data["total_cost_usd"].(float64); ok {
		stats.TotalCostUSD = v
	}
	if v, ok := data["total_turns"].(float64); ok {
		stats.TotalTurns = int(v)
	}

	return stats
}

// Close closes the WebSocket connection
func (c *RemoteClient) Close() error {
	close(c.done)

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// GetState returns the runtime state for use by the TUI
func (c *RemoteClient) GetState() *daemon.RuntimeState {
	return c.state
}

// GetEventBus returns the event bus for use by the TUI
func (c *RemoteClient) GetEventBus() *events.EventBus {
	return c.eventBus
}

// IsConnected returns whether the client is connected
func (c *RemoteClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// BaseURL returns the base HTTP URL
func (c *RemoteClient) BaseURL() string {
	return c.baseURL
}
