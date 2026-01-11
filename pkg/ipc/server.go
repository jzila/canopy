package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/jzila/canopy/pkg/daemon"
)

// Server manages IPC connections from canopy run clients
// It listens on a Unix socket, accepts connections, and forwards
// received events to the daemon's EventBus for WebSocket broadcasting
type Server struct {
	socketPath string
	listener   net.Listener
	eventBus   *daemon.EventBus

	// Connection management
	mu          sync.RWMutex
	connections map[net.Conn]struct{}
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

// NewServer creates a new IPC server
// socketPath: path to Unix socket (e.g., /tmp/canopy.sock)
// eventBus: daemon EventBus to forward events to
func NewServer(socketPath string, eventBus *daemon.EventBus) *Server {
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
		listener.Close()
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
		s.listener.(*net.UnixListener).SetDeadline(time.Now().Add(time.Second))

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

		log.Printf("IPC: accepted connection from canopy run client (total: %d)", connCount)

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

		conn.Close()
	}()

	// Create buffered reader for efficient line reading
	reader := bufio.NewReader(conn)

	for {
		// Read line-delimited JSON
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				// TODO: Consider logging parse errors
			}
			return
		}

		// Parse message
		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Printf("IPC: failed to parse message: %v", err)
			continue
		}

		log.Printf("IPC: received message type=%s", msg.Type)

		// Convert and forward to EventBus
		if event := s.convertToEvent(&msg); event != nil {
			log.Printf("IPC: forwarding event type=%s to EventBus", event.Type)
			s.eventBus.Publish(*event)
		} else {
			log.Printf("IPC: warning - failed to convert message type=%s to event", msg.Type)
		}
	}
}

// convertToEvent converts IPC messages to daemon Events
// Returns nil for unknown message types
func (s *Server) convertToEvent(msg *Message) *daemon.Event {
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
		return &daemon.Event{
			Type:      daemon.EventAgentStarted,
			Timestamp: msg.Timestamp,
			Payload: map[string]interface{}{
				"agent_id":   payload.AgentID,
				"task_id":    payload.TaskID,
				"task_title": payload.TaskTitle,
			},
		}

	case MessageTypeAgentOutput:
		var payload AgentOutputPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		return &daemon.Event{
			Type:      daemon.EventAgentOutput,
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
		return &daemon.Event{
			Type:      daemon.EventAgentLiveFeed,
			Timestamp: msg.Timestamp,
			Payload: map[string]interface{}{
				"agent_id":   payload.AgentID,
				"event_type": payload.EventType,
				"data":       payload.Data,
			},
		}

	case MessageTypeAgentDone:
		var payload AgentDonePayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		return &daemon.Event{
			Type:      daemon.EventAgentCompleted,
			Timestamp: msg.Timestamp,
			Payload: map[string]interface{}{
				"agent_id":        payload.AgentID,
				"exit_code":       payload.Result.ExitCode,
				"duration":        payload.Result.DurationSeconds,
				"input_tokens":    payload.Result.InputTokens,
				"output_tokens":   payload.Result.OutputTokens,
				"cost_usd":        payload.Result.CostUSD,
				"files_changed":   payload.Result.FilesChanged,
				"commits_created": payload.Result.CommitsCreated,
			},
		}

	case MessageTypeAgentFail:
		var payload AgentFailPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		return &daemon.Event{
			Type:      daemon.EventAgentCompleted,
			Timestamp: msg.Timestamp,
			Payload: map[string]interface{}{
				"agent_id":        payload.AgentID,
				"error":           payload.Error,
				"exit_code":       payload.Result.ExitCode,
				"duration":        payload.Result.DurationSeconds,
				"input_tokens":    payload.Result.InputTokens,
				"output_tokens":   payload.Result.OutputTokens,
				"cost_usd":        payload.Result.CostUSD,
				"files_changed":   payload.Result.FilesChanged,
				"commits_created": payload.Result.CommitsCreated,
			},
		}

	case MessageTypeRunStarted:
		var payload RunStartedPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		// Map to stats update event
		return &daemon.Event{
			Type:      daemon.EventStatsUpdated,
			Timestamp: msg.Timestamp,
			Payload: map[string]interface{}{
				"run_id":     payload.RunID,
				"task_count": payload.TaskCount,
			},
		}

	case MessageTypeRunCompleted:
		var payload RunCompletedPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil
		}
		// Map to stats update event
		return &daemon.Event{
			Type:      daemon.EventStatsUpdated,
			Timestamp: msg.Timestamp,
			Payload: map[string]interface{}{
				"run_id":              payload.RunID,
				"total_tasks":         payload.Stats.TotalTasks,
				"succeeded_tasks":     payload.Stats.SucceededTasks,
				"failed_tasks":        payload.Stats.FailedTasks,
				"total_duration":      payload.Stats.TotalDuration,
				"total_input_tokens":  payload.Stats.TotalInputTokens,
				"total_output_tokens": payload.Stats.TotalOutputTokens,
				"total_cost_usd":      payload.Stats.TotalCostUSD,
				"files_changed":       payload.Stats.FilesChanged,
				"conflicts_resolved":  payload.Stats.ConflictsResolved,
			},
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
		s.listener.Close()
	}

	// Close all active connections
	s.mu.Lock()
	for conn := range s.connections {
		conn.Close()
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
