package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/jzila/canopy/pkg/repository"
)

// Queue configuration constants
const (
	// DefaultMaxQueueSize is the maximum number of events to queue when disconnected
	DefaultMaxQueueSize = 10000
	// reconnectInitialDelay is the starting delay for reconnection attempts
	reconnectInitialDelay = 100 * time.Millisecond
	// reconnectMaxDelay is the maximum delay between reconnection attempts
	reconnectMaxDelay = 5 * time.Second
	// reconnectTimeout is how long to keep trying to reconnect before giving up
	reconnectTimeout = 5 * time.Minute
)

// queuedEvent holds a pre-encoded message waiting to be sent
type queuedEvent struct {
	data      []byte    // Pre-encoded JSON with newline
	timestamp time.Time // When the event was originally created
}

// Client manages IPC communication from canopy run to canopy daemon
// Connects to the daemon's Unix socket and sends events as workers execute.
// If the connection is lost, events are queued locally and automatically
// flushed when the connection is re-established.
type Client struct {
	socketPath string
	conn       net.Conn
	encoder    *json.Encoder
	mu         sync.Mutex

	// Event queuing for disconnection resilience
	eventQueue     []queuedEvent // Pending events when disconnected
	maxQueueSize   int           // Maximum queue size (default: DefaultMaxQueueSize)
	droppedEvents  int           // Count of events dropped due to queue overflow
	reconnecting   bool          // Whether a reconnect goroutine is running
	reconnectStop  chan struct{} // Signal to stop reconnect goroutine
	verbose        bool          // Whether to log warnings to stderr
}

// NewClient creates a new IPC client and connects to the daemon socket
// socketPath: path to Unix socket (e.g., /tmp/canopy.sock)
// Returns error if connection fails
func NewClient(socketPath string) (*Client, error) {
	client := &Client{
		socketPath:   socketPath,
		maxQueueSize: DefaultMaxQueueSize,
		eventQueue:   make([]queuedEvent, 0),
	}

	if err := client.Connect(); err != nil {
		return nil, err
	}

	return client, nil
}

// SetVerbose enables or disables verbose logging for the client.
// When enabled, the client will log warnings about reconnection attempts
// and dropped events to stderr.
func (c *Client) SetVerbose(verbose bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.verbose = verbose
}

// SetMaxQueueSize sets the maximum number of events to queue when disconnected.
// Events beyond this limit will be dropped (oldest first).
func (c *Client) SetMaxQueueSize(size int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if size > 0 {
		c.maxQueueSize = size
	}
}

// QueueStats returns the current queue statistics.
// Returns: queued events count, dropped events count, whether currently reconnecting
func (c *Client) QueueStats() (queued, dropped int, reconnecting bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.eventQueue), c.droppedEvents, c.reconnecting
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
// Enforces message size limits to prevent oversized messages.
// If the connection is lost, queues the message and starts auto-reconnect.
func (c *Client) sendMessage(msgType MessageType, payload interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	msg := Message{
		Type:      msgType,
		Timestamp: now,
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

	// Add newline delimiter
	data = append(data, '\n')

	// If not connected, queue the event and start reconnection
	if c.conn == nil {
		c.queueEventLocked(data, now)
		c.startReconnectLocked()
		return nil // Don't return error - event is queued
	}

	// Try to send
	if _, err := c.conn.Write(data); err != nil {
		// Connection failed - mark as disconnected, queue event, start reconnect
		c.conn.Close()
		c.conn = nil
		c.encoder = nil
		c.queueEventLocked(data, now)
		c.startReconnectLocked()
		if c.verbose {
			fmt.Fprintf(os.Stderr, "warning: IPC connection lost, queued event and starting reconnect: %v\n", err)
		}
		return nil // Don't return error - event is queued
	}

	return nil
}

// queueEventLocked adds an event to the queue. Must be called with c.mu held.
// Implements graceful degradation by dropping oldest events when queue is full.
func (c *Client) queueEventLocked(data []byte, timestamp time.Time) {
	// Initialize queue if nil
	if c.eventQueue == nil {
		c.eventQueue = make([]queuedEvent, 0)
	}

	// Initialize maxQueueSize if not set
	if c.maxQueueSize <= 0 {
		c.maxQueueSize = DefaultMaxQueueSize
	}

	// Check if queue is full
	if len(c.eventQueue) >= c.maxQueueSize {
		// Drop oldest event
		c.eventQueue = c.eventQueue[1:]
		c.droppedEvents++
		if c.verbose && c.droppedEvents == 1 {
			fmt.Fprintf(os.Stderr, "warning: IPC event queue full, dropping oldest events\n")
		}
	}

	c.eventQueue = append(c.eventQueue, queuedEvent{
		data:      data,
		timestamp: timestamp,
	})
}

// startReconnectLocked starts the reconnect goroutine if not already running.
// Must be called with c.mu held.
func (c *Client) startReconnectLocked() {
	if c.reconnecting {
		return // Already reconnecting
	}

	c.reconnecting = true
	c.reconnectStop = make(chan struct{})

	go c.reconnectLoop()
}

// reconnectLoop attempts to reconnect to the daemon with exponential backoff.
// On successful reconnect, flushes all queued events.
func (c *Client) reconnectLoop() {
	backoff := reconnectInitialDelay
	deadline := time.Now().Add(reconnectTimeout)

	for {
		// Check if we should stop
		select {
		case <-c.reconnectStop:
			c.mu.Lock()
			c.reconnecting = false
			c.mu.Unlock()
			return
		default:
		}

		// Check timeout
		if time.Now().After(deadline) {
			c.mu.Lock()
			if c.verbose {
				fmt.Fprintf(os.Stderr, "warning: IPC reconnect timed out after %v, %d events in queue, %d dropped\n",
					reconnectTimeout, len(c.eventQueue), c.droppedEvents)
			}
			c.reconnecting = false
			c.mu.Unlock()
			return
		}

		// Attempt to connect
		conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
		if err == nil {
			// Success! Flush queue and return
			c.mu.Lock()
			c.conn = conn
			c.encoder = json.NewEncoder(conn)
			queueSize := len(c.eventQueue)
			droppedCount := c.droppedEvents

			if c.verbose && queueSize > 0 {
				fmt.Fprintf(os.Stderr, "info: IPC reconnected, flushing %d queued events", queueSize)
				if droppedCount > 0 {
					fmt.Fprintf(os.Stderr, " (%d events were dropped)", droppedCount)
				}
				fmt.Fprintf(os.Stderr, "\n")
			}

			// Flush queue while holding the lock
			c.flushQueueLocked()
			c.reconnecting = false
			c.mu.Unlock()
			return
		}

		// Sleep with backoff, but check for stop signal
		select {
		case <-c.reconnectStop:
			c.mu.Lock()
			c.reconnecting = false
			c.mu.Unlock()
			return
		case <-time.After(backoff):
		}

		// Exponential backoff with cap
		backoff *= 2
		if backoff > reconnectMaxDelay {
			backoff = reconnectMaxDelay
		}
	}
}

// flushQueueLocked sends all queued events to the daemon.
// Must be called with c.mu held and c.conn != nil.
// Events are sent in order, preserving their original timestamps.
func (c *Client) flushQueueLocked() {
	if c.conn == nil || len(c.eventQueue) == 0 {
		return
	}

	var failedEvents []queuedEvent
	for _, event := range c.eventQueue {
		if _, err := c.conn.Write(event.data); err != nil {
			// Connection failed again during flush
			if c.verbose {
				fmt.Fprintf(os.Stderr, "warning: IPC flush failed, re-queuing %d events: %v\n",
					len(c.eventQueue)-len(failedEvents), err)
			}
			// Keep remaining events in queue
			failedEvents = append(failedEvents, event)
			// Mark connection as dead
			c.conn.Close()
			c.conn = nil
			c.encoder = nil
			break
		}
	}

	// Update queue with any events that failed to send
	c.eventQueue = failedEvents

	// If we have remaining events, restart reconnect
	if len(c.eventQueue) > 0 && !c.reconnecting {
		c.reconnecting = true
		c.reconnectStop = make(chan struct{})
		go c.reconnectLoop()
	}
}

// SendAgentStart notifies the daemon that an agent has started executing a task
// runID is the ID of the run this agent belongs to (for historical filtering)
// taskDescription is optional and provides the full task description
// parentAgentID is optional and specifies the ID of the parent agent if this agent was spawned by another
// repoID is optional and specifies the repository ID for tracking
func (c *Client) SendAgentStart(agentID, runID, taskID, taskTitle, taskDescription, parentAgentID, repoID string) error {
	payload := AgentStartPayload{
		AgentID:         agentID,
		RunID:           runID,
		TaskID:          taskID,
		TaskTitle:       taskTitle,
		TaskDescription: taskDescription,
		ParentAgentID:   parentAgentID,
		RepoID:          repoID,
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

// SendAgentMergeStatusWithValidation notifies the daemon of an agent's merge status including validation results.
// This should be used after validation runs to include validation outcome.
func (c *Client) SendAgentMergeStatusWithValidation(agentID string, status MergeStatus, errMsg string, commitsApplied int, hadConflict, resolverSpawned bool, validationStatus string, validationError string, validationDurationMS int64, validationSteps []ValidationStep) error {
	payload := AgentMergeStatusPayload{
		AgentID:            agentID,
		MergeStatus:        status,
		Error:              errMsg,
		CommitsApplied:     commitsApplied,
		HadConflict:        hadConflict,
		ResolverSpawned:    resolverSpawned,
		ValidationStatus:   validationStatus,
		ValidationError:    validationError,
		ValidationDuration: validationDurationMS,
		ValidationSteps:    validationSteps,
	}

	return c.sendMessage(MessageTypeAgentMergeStatus, payload)
}

// SendAgentRepairStatus notifies the daemon of an agent's repair status.
// This should be used when a repair agent starts or completes.
func (c *Client) SendAgentRepairStatus(agentID string, repairAttempts int, lastRepairOutput string, validationStatus string) error {
	payload := AgentMergeStatusPayload{
		AgentID:          agentID,
		MergeStatus:      MergeStatusResolving, // Repair uses resolving status
		ValidationStatus: validationStatus,
		RepairAttempts:   repairAttempts,
		LastRepairOutput: lastRepairOutput,
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

// Close closes the connection to the daemon and stops any reconnection attempts.
// Should be called when the client is no longer needed.
// Any queued events will be discarded.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Stop reconnect goroutine if running
	if c.reconnecting && c.reconnectStop != nil {
		close(c.reconnectStop)
		c.reconnecting = false
	}

	// Clear the event queue
	c.eventQueue = nil

	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	c.encoder = nil

	return err
}
