package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jzila/canopy/pkg/repository"
)

// Client manages IPC communication from canopy run to canopy daemon
// Connects to the daemon's Unix socket and sends events as workers execute
type Client struct {
	socketPath string
	conn       net.Conn
	encoder    *json.Encoder
	mu         sync.Mutex
}

// NewClient creates a new IPC client and connects to the daemon socket
// socketPath: path to Unix socket (e.g., /tmp/canopy.sock)
// Returns error if connection fails
func NewClient(socketPath string) (*Client, error) {
	client := &Client{
		socketPath: socketPath,
	}

	if err := client.Connect(); err != nil {
		return nil, err
	}

	return client, nil
}

// Connect establishes connection to the canopy daemon Unix socket
// Can be called to reconnect after connection loss
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Close existing connection if present
	if c.conn != nil {
		c.conn.Close()
	}

	// Connect to Unix socket with timeout
	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to canopy daemon socket: %w", err)
	}

	c.conn = conn
	c.encoder = json.NewEncoder(conn)

	return nil
}

// sendMessage sends a message with the given type and payload
// Automatically adds version, timestamp and encodes as newline-delimited JSON
// Enforces message size limits to prevent oversized messages
func (c *Client) sendMessage(msgType MessageType, payload interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected to daemon")
	}

	msg := Message{
		Version:   CurrentProtocolVersion,
		Type:      msgType,
		Timestamp: time.Now(),
		Payload:   payload,
	}

	// Pre-encode to check size limits before sending
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to encode message: %w", err)
	}

	if len(data) > MaxMessageSize {
		return fmt.Errorf("message size %d exceeds maximum %d", len(data), MaxMessageSize)
	}

	// Write with newline delimiter
	data = append(data, '\n')
	if _, err := c.conn.Write(data); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// SendAgentStart notifies the daemon that an agent has started executing a task
// parentAgentID is optional and specifies the ID of the parent agent if this agent was spawned by another
// repoID is optional and specifies the repository ID for tracking
func (c *Client) SendAgentStart(agentID, taskID, taskTitle, parentAgentID, repoID string) error {
	payload := AgentStartPayload{
		AgentID:       agentID,
		TaskID:        taskID,
		TaskTitle:     taskTitle,
		ParentAgentID: parentAgentID,
		RepoID:        repoID,
	}

	return c.sendMessage(MessageTypeAgentStart, payload)
}

// SendAgentOutput sends agent output (stdout/stderr) to the daemon
// isError should be true for stderr output
func (c *Client) SendAgentOutput(agentID, output string, isError bool) error {
	payload := AgentOutputPayload{
		AgentID: agentID,
		Output:  output,
		IsError: isError,
	}

	return c.sendMessage(MessageTypeAgentOutput, payload)
}

// SendAgentLiveFeed sends real-time streaming events from an agent
func (c *Client) SendAgentLiveFeed(agentID, eventType string, data map[string]interface{}) error {
	payload := AgentLiveFeedPayload{
		AgentID:   agentID,
		EventType: eventType,
		Data:      data,
	}

	return c.sendMessage(MessageTypeAgentLiveFeed, payload)
}

// SendAgentCommit notifies the daemon that an agent created a git commit
func (c *Client) SendAgentCommit(agentID string, commit *AgentCommitPayload) error {
	if commit == nil {
		return fmt.Errorf("commit cannot be nil")
	}

	commit.AgentID = agentID
	return c.sendMessage(MessageTypeAgentCommit, commit)
}

// SendAgentMergeStatus notifies the daemon of an agent's merge queue status
func (c *Client) SendAgentMergeStatus(agentID string, status MergeStatus, queuePos int, errMsg string) error {
	payload := AgentMergeStatusPayload{
		AgentID:     agentID,
		MergeStatus: status,
		QueuePos:    queuePos,
		Error:       errMsg,
	}

	return c.sendMessage(MessageTypeAgentMergeStatus, payload)
}

// SendAgentMergeStatusFull notifies the daemon of an agent's merge queue status with full details.
// This should be used for final statuses (merged/failed) to include commit and conflict information.
func (c *Client) SendAgentMergeStatusFull(agentID string, status MergeStatus, queuePos int, errMsg string, commitsApplied int, hadConflict, resolverSpawned bool) error {
	payload := AgentMergeStatusPayload{
		AgentID:         agentID,
		MergeStatus:     status,
		QueuePos:        queuePos,
		Error:           errMsg,
		CommitsApplied:  commitsApplied,
		HadConflict:     hadConflict,
		ResolverSpawned: resolverSpawned,
	}

	return c.sendMessage(MessageTypeAgentMergeStatus, payload)
}

// SendAgentDone notifies the daemon that an agent completed successfully
// parentAgentID is optional and specifies the ID of the parent agent if this agent was spawned by another
func (c *Client) SendAgentDone(agentID, parentAgentID string, result *AgentResult) error {
	if result == nil {
		return fmt.Errorf("result cannot be nil")
	}

	payload := AgentDonePayload{
		AgentID:       agentID,
		ParentAgentID: parentAgentID,
		Result:        *result,
	}

	return c.sendMessage(MessageTypeAgentDone, payload)
}

// SendAgentFail notifies the daemon that an agent failed
// parentAgentID is optional and specifies the ID of the parent agent if this agent was spawned by another
func (c *Client) SendAgentFail(agentID, parentAgentID string, err error, result *AgentResult) error {
	if err == nil {
		return fmt.Errorf("error cannot be nil")
	}
	if result == nil {
		return fmt.Errorf("result cannot be nil")
	}

	payload := AgentFailPayload{
		AgentID:       agentID,
		ParentAgentID: parentAgentID,
		Error:         err.Error(),
		Result:        *result,
	}

	return c.sendMessage(MessageTypeAgentFail, payload)
}

// SendTaskUpdated notifies the daemon that a task status has changed
// repoID is optional and specifies the repository ID for tracking
func (c *Client) SendTaskUpdated(taskID, title, status, agentID, repoID string) error {
	payload := TaskUpdatedPayload{
		ID:      taskID,
		Title:   title,
		Status:  status,
		AgentID: agentID,
		RepoID:  repoID,
	}

	return c.sendMessage(MessageTypeTaskUpdated, payload)
}

// SendRunStarted notifies the daemon that a canopy run has started
// repo is optional and contains repository information for tracking
func (c *Client) SendRunStarted(runID string, taskCount int, repo *repository.Repository) error {
	payload := RunStartedPayload{
		RunID:     runID,
		TaskCount: taskCount,
	}

	if repo != nil {
		payload.RepoID = repo.ID
		payload.RepoPath = repo.Path
		payload.RepoName = repo.Name
	}

	return c.sendMessage(MessageTypeRunStarted, payload)
}

// SendRunCompleted notifies the daemon that a canopy run has completed
func (c *Client) SendRunCompleted(runID string, stats *RunStats) error {
	if stats == nil {
		return fmt.Errorf("stats cannot be nil")
	}

	payload := RunCompletedPayload{
		RunID: runID,
		Stats: *stats,
	}

	return c.sendMessage(MessageTypeRunCompleted, payload)
}

// Close closes the connection to the daemon
// Should be called when the client is no longer needed
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	c.encoder = nil

	return err
}
