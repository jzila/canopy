package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/jzila/canopy/pkg/events"
	"github.com/jzila/canopy/pkg/logging"
	"github.com/jzila/canopy/pkg/metrics"
)

// Server manages IPC connections from canopy run clients
// It listens on a Unix socket, accepts connections, and forwards
// received events to the daemon's EventBus for WebSocket broadcasting
type Server struct {
	socketPath string
	listener   net.Listener
	eventBus   *events.EventBus

	// Connection management
	mu          sync.RWMutex
	connections map[net.Conn]struct{}
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

// NewServer creates a new IPC server
// socketPath: path to Unix socket (e.g., /tmp/canopy.sock)
// eventBus: EventBus to forward events to
func NewServer(socketPath string, eventBus *events.EventBus) *Server {
	return &Server{
		socketPath:  socketPath,
		eventBus:    eventBus,
		connections: make(map[net.Conn]struct{}),
		stopChan:    make(chan struct{}),
	}
}

// Start begins listening for connections
// Returns an error if the socket cannot be created or listened on
func (s *Server) Start() error {
	// Remove existing socket file if present
	if err := os.RemoveAll(s.socketPath); err != nil {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	// Create Unix socket listener
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	s.listener = listener

	// Set socket permissions to owner-only (0600)
	if err := os.Chmod(s.socketPath, 0600); err != nil {
		_ = listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	// Start accept loop in goroutine
	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// acceptLoop continuously accepts new connections until stopped
func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for {
		// Set accept deadline to allow periodic stop checks
		_ = s.listener.(*net.UnixListener).SetDeadline(time.Now().Add(time.Second))

		conn, err := s.listener.Accept()
		if err != nil {
			// Check if we're shutting down
			select {
			case <-s.stopChan:
				return
			default:
			}

			// Check for timeout (expected due to deadline)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}

			// Unexpected error
			// TODO: Consider logging framework integration
			continue
		}

		// Register connection
		s.mu.Lock()
		s.connections[conn] = struct{}{}
		connCount := len(s.connections)
		s.mu.Unlock()

		logging.Debug("IPC accepted connection", "total_connections", connCount)

		// Handle connection in goroutine
		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// handleConnection reads and processes messages from a single connection
func (s *Server) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		// Unregister connection
		s.mu.Lock()
		delete(s.connections, conn)
		s.mu.Unlock()

		_ = conn.Close()
	}()

	// Create limited reader to enforce message size limits
	// bufio.Scanner handles line reading more safely than ReadBytes for size limits
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), MaxMessageSize) // Start with 64KB, max 1MB

	for scanner.Scan() {
		line := scanner.Bytes()

		// Enforce message size limit
		if len(line) > MaxMessageSize {
			logging.Warn("IPC message size exceeds maximum, dropping",
				"size", len(line),
				"max_size", MaxMessageSize)
			continue
		}

		// Parse as RawMessage first to check version and payload size
		var rawMsg RawMessage
		if err := json.Unmarshal(line, &rawMsg); err != nil {
			logging.Warn("IPC failed to parse message", "error", err)
			continue
		}

		// Enforce payload size limit
		if len(rawMsg.Payload) > MaxPayloadSize {
			logging.Warn("IPC payload size exceeds maximum, dropping",
				"size", len(rawMsg.Payload),
				"max_size", MaxPayloadSize)
			continue
		}

		// Convert RawMessage to Message for processing
		msg := Message{
			Type:      rawMsg.Type,
			Timestamp: rawMsg.Timestamp,
		}

		// Unmarshal payload into interface{} for convertToEvent
		if len(rawMsg.Payload) > 0 {
			if err := json.Unmarshal(rawMsg.Payload, &msg.Payload); err != nil {
				logging.Warn("IPC failed to parse payload", "error", err)
				continue
			}
		}

		// Extract identifier for logging
		identifier := s.extractIdentifier(&msg)
		logging.Debug("IPC received message", "type", msg.Type, "identifier", identifier)

		// Log agent completion events with pretty-printed JSON for readability
		if msg.Type == MessageTypeAgentDone || msg.Type == MessageTypeAgentFail {
			if formatted, err := FormatJSON(msg.Payload); err == nil {
				logging.Debug("IPC agent completion payload",
					"type", msg.Type,
					"payload", IndentMultilineString(formatted, "  "))
			}
		}

		// Convert and forward to EventBus
		if event := s.convertToEvent(&msg); event != nil {
			logging.Debug("IPC forwarding event to EventBus", "event_type", event.Type, "identifier", identifier)
			s.eventBus.Publish(*event)
			metrics.IncIPCMessages(string(msg.Type))
		} else {
			logging.Warn("IPC failed to convert message to event", "type", msg.Type, "identifier", identifier)
		}
	}

	if err := scanner.Err(); err != nil {
		if err != io.EOF {
			logging.Warn("IPC connection error", "error", err)
		}
	}
}

// extractIdentifier extracts agent_id or run_id from message payload for logging
// Returns formatted string like " agent=canopy-abc" or " run=xyz123" or empty string
func (s *Server) extractIdentifier(msg *Message) string {
	// Re-marshal payload for type extraction
	payloadBytes, err := json.Marshal(msg.Payload)
	if err != nil {
		return ""
	}

	switch msg.Type {
	case MessageTypeAgentStart:
		var payload AgentStartPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return ""
		}
		return fmt.Sprintf(" agent=%s", payload.AgentID)

	case MessageTypeAgentOutput:
		var payload AgentOutputPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return ""
		}
		return fmt.Sprintf(" agent=%s", payload.AgentID)

	case MessageTypeAgentLiveFeed:
		var payload AgentLiveFeedPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return ""
		}
		return fmt.Sprintf(" agent=%s", payload.AgentID)

	case MessageTypeAgentCommit:
		var payload AgentCommitPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return ""
		}
		return fmt.Sprintf(" agent=%s", payload.AgentID)

	case MessageTypeAgentMergeStatus:
		var payload AgentMergeStatusPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return ""
		}
		return fmt.Sprintf(" agent=%s", payload.AgentID)

	case MessageTypeAgentDone:
		var payload AgentDonePayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return ""
		}
		return fmt.Sprintf(" agent=%s", payload.AgentID)

	case MessageTypeAgentFail:
		var payload AgentFailPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return ""
		}
		return fmt.Sprintf(" agent=%s", payload.AgentID)

	case MessageTypeRunStarted:
		var payload RunStartedPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return ""
		}
		return fmt.Sprintf(" run=%s", payload.RunID)

	case MessageTypeRunCompleted:
		var payload RunCompletedPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return ""
		}
		return fmt.Sprintf(" run=%s", payload.RunID)

	default:
		return ""
	}
}

// convertToEvent converts IPC messages to events.Event
// Returns nil for unknown message types
func (s *Server) convertToEvent(msg *Message) *events.Event {
	// Re-marshal payload for type conversion
	// This handles the interface{} -> concrete type conversion
	payloadBytes, err := json.Marshal(msg.Payload)
	if err != nil {
		return nil
	}

	switch msg.Type {
	case MessageTypeAgentStart:
		var payload AgentStartPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		eventPayload := map[string]interface{}{
			"agent_id":   payload.AgentID,
			"task_id":    payload.TaskID,
			"task_title": payload.TaskTitle,
		}
		if payload.RunID != "" {
			eventPayload["run_id"] = payload.RunID
		}
		if payload.TaskDescription != "" {
			eventPayload["task_description"] = payload.TaskDescription
		}
		if payload.ParentAgentID != "" {
			eventPayload["parent_agent_id"] = payload.ParentAgentID
		}
		if payload.RepoID != "" {
			eventPayload["repo_id"] = payload.RepoID
		}
		return &events.Event{
			Type:      events.EventAgentStarted,
			Timestamp: msg.Timestamp,
			Payload:   eventPayload,
		}

	case MessageTypeAgentOutput:
		var payload AgentOutputPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		return &events.Event{
			Type:      events.EventAgentOutput,
			Timestamp: msg.Timestamp,
			Payload: map[string]interface{}{
				"agent_id": payload.AgentID,
				"output":   payload.Output,
				"is_error": payload.IsError,
			},
		}

	case MessageTypeAgentLiveFeed:
		var payload AgentLiveFeedPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		return &events.Event{
			Type:      events.EventAgentLiveFeed,
			Timestamp: msg.Timestamp,
			Payload: map[string]interface{}{
				"agent_id":   payload.AgentID,
				"event_type": payload.EventType,
				"data":       payload.Data,
			},
		}

	case MessageTypeAgentCommit:
		var payload AgentCommitPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		return &events.Event{
			Type:      events.EventAgentCommit,
			Timestamp: msg.Timestamp,
			Payload: map[string]interface{}{
				"agent_id":      payload.AgentID,
				"hash":          payload.Hash,
				"short_hash":    payload.ShortHash,
				"message":       payload.Message,
				"author":        payload.Author,
				"author_email":  payload.AuthorEmail,
				"timestamp":     payload.Timestamp,
				"files_changed": payload.FilesChanged,
			},
		}

	case MessageTypeAgentMergeStatus:
		var payload AgentMergeStatusPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		eventPayload := map[string]interface{}{
			"agent_id":     payload.AgentID,
			"merge_status": string(payload.MergeStatus),
		}
		if payload.QueuePos > 0 {
			eventPayload["queue_pos"] = payload.QueuePos
		}
		if payload.Error != "" {
			eventPayload["error"] = payload.Error
		}
		// Forward merge result fields (for final status updates)
		if payload.CommitsApplied > 0 {
			eventPayload["commits_applied"] = payload.CommitsApplied
		}
		if payload.HadConflict {
			eventPayload["had_conflict"] = payload.HadConflict
		}
		if payload.ResolverSpawned {
			eventPayload["resolver_spawned"] = payload.ResolverSpawned
		}
		// Forward validation fields
		if payload.ValidationStatus != "" {
			eventPayload["validation_status"] = payload.ValidationStatus
		}
		if payload.ValidationError != "" {
			eventPayload["validation_error"] = payload.ValidationError
		}
		if payload.ValidationDuration > 0 {
			eventPayload["validation_duration_ms"] = payload.ValidationDuration
		}
		if len(payload.ValidationSteps) > 0 {
			eventPayload["validation_steps"] = payload.ValidationSteps
		}
		// Forward repair tracking fields
		if payload.RepairAttempts > 0 {
			eventPayload["repair_attempts"] = payload.RepairAttempts
		}
		if payload.LastRepairOutput != "" {
			eventPayload["last_repair_output"] = payload.LastRepairOutput
		}
		return &events.Event{
			Type:      events.EventAgentMergeStatus,
			Timestamp: msg.Timestamp,
			Payload:   eventPayload,
		}

	case MessageTypeAgentDone:
		var payload AgentDonePayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		eventPayload := map[string]interface{}{
			"agent_id":                    payload.AgentID,
			"exit_code":                   payload.Result.ExitCode,
			"duration":                    payload.Result.DurationSeconds,
			"input_tokens":                payload.Result.InputTokens,
			"output_tokens":               payload.Result.OutputTokens,
			"cache_creation_input_tokens": payload.Result.CacheCreationInputTokens,
			"cache_read_input_tokens":     payload.Result.CacheReadInputTokens,
			"cost_usd":                    payload.Result.CostUSD,
			"files_changed":               payload.Result.FilesChanged,
			"commits_created":             payload.Result.CommitsCreated,
			"num_turns":                   payload.Result.NumTurns,
			"result_message":              payload.Result.ResultMessage,
			"stdout":                      payload.Result.Stdout,
			"stderr":                      payload.Result.Stderr,
		}
		if payload.ParentAgentID != "" {
			eventPayload["parent_agent_id"] = payload.ParentAgentID
		}
		return &events.Event{
			Type:      events.EventAgentCompleted,
			Timestamp: msg.Timestamp,
			Payload:   eventPayload,
		}

	case MessageTypeAgentFail:
		var payload AgentFailPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		eventPayload := map[string]interface{}{
			"agent_id":                    payload.AgentID,
			"error":                       payload.Error,
			"exit_code":                   payload.Result.ExitCode,
			"duration":                    payload.Result.DurationSeconds,
			"input_tokens":                payload.Result.InputTokens,
			"output_tokens":               payload.Result.OutputTokens,
			"cache_creation_input_tokens": payload.Result.CacheCreationInputTokens,
			"cache_read_input_tokens":     payload.Result.CacheReadInputTokens,
			"cost_usd":                    payload.Result.CostUSD,
			"files_changed":               payload.Result.FilesChanged,
			"commits_created":             payload.Result.CommitsCreated,
			"num_turns":                   payload.Result.NumTurns,
			"result_message":              payload.Result.ResultMessage,
			"stdout":                      payload.Result.Stdout,
			"stderr":                      payload.Result.Stderr,
		}
		if payload.ParentAgentID != "" {
			eventPayload["parent_agent_id"] = payload.ParentAgentID
		}
		return &events.Event{
			Type:      events.EventAgentCompleted,
			Timestamp: msg.Timestamp,
			Payload:   eventPayload,
		}

	case MessageTypeRunStarted:
		var payload RunStartedPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		// Map to stats update event
		eventPayload := map[string]interface{}{
			"run_id":     payload.RunID,
			"task_count": payload.TaskCount,
		}
		if payload.RepoID != "" {
			eventPayload["repo_id"] = payload.RepoID
		}
		if payload.RepoPath != "" {
			eventPayload["repo_path"] = payload.RepoPath
		}
		if payload.RepoName != "" {
			eventPayload["repo_name"] = payload.RepoName
		}
		return &events.Event{
			Type:      events.EventRunStarted,
			Timestamp: msg.Timestamp,
			Payload:   eventPayload,
		}

	case MessageTypeRunCompleted:
		var payload RunCompletedPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		// Map to run completed event
		// Key names must match what persistence_handler.go expects
		return &events.Event{
			Type:      events.EventRunCompleted,
			Timestamp: msg.Timestamp,
			Payload: map[string]interface{}{
				"run_id":                            payload.RunID,
				"total_tasks":                       payload.Stats.TotalTasks,
				"succeeded_tasks":                   payload.Stats.SucceededTasks,
				"failed_tasks":                      payload.Stats.FailedTasks,
				"total_duration_seconds":            payload.Stats.TotalDuration,
				"total_input_tokens":                payload.Stats.TotalInputTokens,
				"total_output_tokens":               payload.Stats.TotalOutputTokens,
				"total_cache_creation_input_tokens": payload.Stats.TotalCacheCreationInputToken,
				"total_cache_read_input_tokens":     payload.Stats.TotalCacheReadInputTokens,
				"total_cost_usd":                    payload.Stats.TotalCostUSD,
				"total_turns":                       payload.Stats.TotalTurns,
				"files_changed":                     payload.Stats.FilesChanged,
				"git_commits":                       payload.Stats.GitCommits,
				"conflicts_resolved":                payload.Stats.ConflictsResolved,
			},
		}

	case MessageTypeTaskUpdated:
		var payload TaskUpdatedPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		eventPayload := map[string]interface{}{
			"id":       payload.ID,
			"title":    payload.Title,
			"status":   payload.Status,
			"agent_id": payload.AgentID,
		}
		if payload.RepoID != "" {
			eventPayload["repo_id"] = payload.RepoID
		}
		return &events.Event{
			Type:      events.EventTaskUpdated,
			Timestamp: msg.Timestamp,
			Payload:   eventPayload,
		}

	default:
		// Unknown message type
		return nil
	}
}

// Stop gracefully shuts down the server
// Closes the listener and all active connections
// Blocks until all goroutines complete
func (s *Server) Stop() error {
	// Signal stop
	close(s.stopChan)

	// Close listener (stops accepting new connections)
	if s.listener != nil {
		_ = s.listener.Close()
	}

	// Close all active connections
	s.mu.Lock()
	for conn := range s.connections {
		_ = conn.Close()
	}
	s.mu.Unlock()

	// Wait for all goroutines to finish
	s.wg.Wait()

	// Clean up socket file
	if err := os.RemoveAll(s.socketPath); err != nil {
		return fmt.Errorf("failed to remove socket: %w", err)
	}

	return nil
}

// ConnectionCount returns the number of active connections
// Useful for testing and monitoring
func (s *Server) ConnectionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.connections)
}
