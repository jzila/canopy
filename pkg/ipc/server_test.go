package ipc

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/events"
)

func TestServerStartStop(t *testing.T) {
	// Create temporary socket path
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	// Create event bus and server
	eventBus := events.NewEventBus()
	server := NewServer(socketPath, eventBus)

	// Start server
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Verify socket exists
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		t.Fatal("Socket file was not created")
	}

	// Stop server
	if err := server.Stop(); err != nil {
		t.Fatalf("Failed to stop server: %v", err)
	}

	// Verify socket removed
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatal("Socket file was not removed")
	}
}

func TestServerConnection(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	eventBus := events.NewEventBus()
	server := NewServer(socketPath, eventBus)

	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Connect to server
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Wait for connection to be registered
	time.Sleep(50 * time.Millisecond)

	// Verify connection count
	if count := server.ConnectionCount(); count != 1 {
		t.Errorf("Expected 1 connection, got %d", count)
	}
}

func TestServerEventForwarding(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	eventBus := events.NewEventBus()
	server := NewServer(socketPath, eventBus)

	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Subscribe to event bus
	receivedEvents := make(chan events.Event, 10)
	eventBus.Subscribe(func(event events.Event) {
		receivedEvents <- event
	})

	// Connect to server
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Test agent start event
	t.Run("AgentStart", func(t *testing.T) {
		msg := Message{
			Type:      MessageTypeAgentStart,
			Timestamp: time.Now(),
			Payload: AgentStartPayload{
				AgentID:   "agent-1",
				TaskID:    "task-1",
				TaskTitle: "Test Task",
			},
		}

		data, _ := json.Marshal(msg)
		data = append(data, '\n')
		if _, err := conn.Write(data); err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}

		// Wait for event
		select {
		case event := <-receivedEvents:
			if event.Type != events.EventAgentStarted {
				t.Errorf("Expected EventAgentStarted, got %s", event.Type)
			}
			payload := event.Payload.(map[string]interface{})
			if payload["agent_id"] != "agent-1" {
				t.Errorf("Expected agent_id=agent-1, got %v", payload["agent_id"])
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for event")
		}
	})

	// Test agent output event
	t.Run("AgentOutput", func(t *testing.T) {
		msg := Message{
			Type:      MessageTypeAgentOutput,
			Timestamp: time.Now(),
			Payload: AgentOutputPayload{
				AgentID: "agent-1",
				Output:  "test output",
				IsError: false,
			},
		}

		data, _ := json.Marshal(msg)
		data = append(data, '\n')
		if _, err := conn.Write(data); err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}

		select {
		case event := <-receivedEvents:
			if event.Type != events.EventAgentOutput {
				t.Errorf("Expected EventAgentOutput, got %s", event.Type)
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for event")
		}
	})

	// Test agent done event
	t.Run("AgentDone", func(t *testing.T) {
		msg := Message{
			Type:      MessageTypeAgentDone,
			Timestamp: time.Now(),
			Payload: AgentDonePayload{
				AgentID: "agent-1",
				Result: AgentResult{
					ExitCode:        0,
					DurationSeconds: 10.5,
					InputTokens:     100,
					OutputTokens:    200,
					CostUSD:         0.05,
					FilesChanged:    3,
					CommitsCreated:  1,
					Stdout:          "test stdout",
					Stderr:          "test stderr",
				},
			},
		}

		data, _ := json.Marshal(msg)
		data = append(data, '\n')
		if _, err := conn.Write(data); err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}

		select {
		case event := <-receivedEvents:
			if event.Type != events.EventAgentCompleted {
				t.Errorf("Expected EventAgentCompleted, got %s", event.Type)
			}
			payload := event.Payload.(map[string]interface{})
			if payload["exit_code"] != 0 {
				t.Errorf("Expected exit_code=0, got %v", payload["exit_code"])
			}
			if payload["stdout"] != "test stdout" {
				t.Errorf("Expected stdout='test stdout', got %v", payload["stdout"])
			}
			if payload["stderr"] != "test stderr" {
				t.Errorf("Expected stderr='test stderr', got %v", payload["stderr"])
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for event")
		}
	})

	// Test agent fail event
	t.Run("AgentFail", func(t *testing.T) {
		msg := Message{
			Type:      MessageTypeAgentFail,
			Timestamp: time.Now(),
			Payload: AgentFailPayload{
				AgentID: "agent-2",
				Error:   "task failed",
				Result: AgentResult{
					ExitCode:        1,
					DurationSeconds: 5.0,
				},
			},
		}

		data, _ := json.Marshal(msg)
		data = append(data, '\n')
		if _, err := conn.Write(data); err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}

		select {
		case event := <-receivedEvents:
			if event.Type != events.EventAgentCompleted {
				t.Errorf("Expected EventAgentCompleted, got %s", event.Type)
			}
			payload := event.Payload.(map[string]interface{})
			if payload["error"] != "task failed" {
				t.Errorf("Expected error='task failed', got %v", payload["error"])
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for event")
		}
	})
}

func TestServerMultipleConnections(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	eventBus := events.NewEventBus()
	server := NewServer(socketPath, eventBus)

	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Create multiple connections
	conns := make([]net.Conn, 3)
	for i := 0; i < 3; i++ {
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		conns[i] = conn
		defer conn.Close()
	}

	// Wait for connections to be registered
	time.Sleep(50 * time.Millisecond)

	// Verify connection count
	if count := server.ConnectionCount(); count != 3 {
		t.Errorf("Expected 3 connections, got %d", count)
	}

	// Close one connection
	conns[0].Close()
	time.Sleep(50 * time.Millisecond)

	// Verify updated count
	if count := server.ConnectionCount(); count != 2 {
		t.Errorf("Expected 2 connections after close, got %d", count)
	}
}

func TestServerInvalidJSON(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	eventBus := events.NewEventBus()
	server := NewServer(socketPath, eventBus)

	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Subscribe to event bus
	receivedEvents := make(chan events.Event, 10)
	eventBus.Subscribe(func(event events.Event) {
		receivedEvents <- event
	})

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send invalid JSON
	if _, err := conn.Write([]byte("invalid json\n")); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Send valid message after invalid one
	msg := Message{
		Type:      MessageTypeAgentStart,
		Timestamp: time.Now(),
		Payload: AgentStartPayload{
			AgentID:   "agent-1",
			TaskID:    "task-1",
			TaskTitle: "Test",
		},
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Should receive the valid message
	select {
	case event := <-receivedEvents:
		if event.Type != events.EventAgentStarted {
			t.Errorf("Expected EventAgentStarted, got %s", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for event")
	}
}

func TestServerProtocolVersioning(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	eventBus := events.NewEventBus()
	server := NewServer(socketPath, eventBus)

	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	receivedEvents := make(chan events.Event, 10)
	eventBus.Subscribe(func(event events.Event) {
		receivedEvents <- event
	})

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Test v1 message (no version field - backwards compatibility)
	t.Run("V1Message", func(t *testing.T) {
		// V1 message without version field
		msg := map[string]interface{}{
			"type":      "agent_start",
			"timestamp": time.Now().Format(time.RFC3339Nano),
			"payload": map[string]interface{}{
				"agent_id":   "agent-v1",
				"task_id":    "task-1",
				"task_title": "V1 Task",
			},
		}

		data, _ := json.Marshal(msg)
		data = append(data, '\n')
		if _, err := conn.Write(data); err != nil {
			t.Fatalf("Failed to send v1 message: %v", err)
		}

		select {
		case event := <-receivedEvents:
			if event.Type != events.EventAgentStarted {
				t.Errorf("Expected EventAgentStarted, got %s", event.Type)
			}
			payload := event.Payload.(map[string]interface{})
			if payload["agent_id"] != "agent-v1" {
				t.Errorf("Expected agent_id=agent-v1, got %v", payload["agent_id"])
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for v1 event")
		}
	})

	// Test v2 message (with version field)
	t.Run("V2Message", func(t *testing.T) {
		msg := Message{
			Version:   ProtocolVersion2,
			Type:      MessageTypeAgentStart,
			Timestamp: time.Now(),
			Payload: AgentStartPayload{
				AgentID:   "agent-v2",
				TaskID:    "task-2",
				TaskTitle: "V2 Task",
			},
		}

		data, _ := json.Marshal(msg)
		data = append(data, '\n')
		if _, err := conn.Write(data); err != nil {
			t.Fatalf("Failed to send v2 message: %v", err)
		}

		select {
		case event := <-receivedEvents:
			if event.Type != events.EventAgentStarted {
				t.Errorf("Expected EventAgentStarted, got %s", event.Type)
			}
			payload := event.Payload.(map[string]interface{})
			if payload["agent_id"] != "agent-v2" {
				t.Errorf("Expected agent_id=agent-v2, got %v", payload["agent_id"])
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for v2 event")
		}
	})
}

func TestServerMessageSizeLimits(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	eventBus := events.NewEventBus()
	server := NewServer(socketPath, eventBus)

	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	receivedEvents := make(chan events.Event, 10)
	eventBus.Subscribe(func(event events.Event) {
		receivedEvents <- event
	})

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Test message within limits
	t.Run("WithinLimits", func(t *testing.T) {
		msg := Message{
			Version:   ProtocolVersion2,
			Type:      MessageTypeAgentOutput,
			Timestamp: time.Now(),
			Payload: AgentOutputPayload{
				AgentID: "agent-1",
				Output:  string(make([]byte, 1024)), // 1KB output - well within limits
				IsError: false,
			},
		}

		data, _ := json.Marshal(msg)
		data = append(data, '\n')
		if _, err := conn.Write(data); err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}

		select {
		case event := <-receivedEvents:
			if event.Type != events.EventAgentOutput {
				t.Errorf("Expected EventAgentOutput, got %s", event.Type)
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for event")
		}
	})

	// Test that subsequent messages work after size limit handling
	t.Run("RecoveryAfterLargeMessage", func(t *testing.T) {
		// Send a normal message to verify the connection still works
		msg := Message{
			Version:   ProtocolVersion2,
			Type:      MessageTypeAgentStart,
			Timestamp: time.Now(),
			Payload: AgentStartPayload{
				AgentID:   "agent-recovery",
				TaskID:    "task-recovery",
				TaskTitle: "Recovery Test",
			},
		}

		data, _ := json.Marshal(msg)
		data = append(data, '\n')
		if _, err := conn.Write(data); err != nil {
			t.Fatalf("Failed to send recovery message: %v", err)
		}

		select {
		case event := <-receivedEvents:
			if event.Type != events.EventAgentStarted {
				t.Errorf("Expected EventAgentStarted, got %s", event.Type)
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for recovery event")
		}
	})
}
