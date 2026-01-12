package ipc

import (
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/daemon"
)

func TestClientConnectClose(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	// Start server
	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Create client
	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Verify connection
	time.Sleep(50 * time.Millisecond)
	if count := server.ConnectionCount(); count != 1 {
		t.Errorf("Expected 1 connection, got %d", count)
	}

	// Close client
	if err := client.Close(); err != nil {
		t.Errorf("Failed to close client: %v", err)
	}

	// Verify disconnection
	time.Sleep(50 * time.Millisecond)
	if count := server.ConnectionCount(); count != 0 {
		t.Errorf("Expected 0 connections after close, got %d", count)
	}
}

func TestClientConnectFailure(t *testing.T) {
	// Try to connect to non-existent socket
	socketPath := filepath.Join(t.TempDir(), "nonexistent.sock")

	client, err := NewClient(socketPath)
	if err == nil {
		client.Close()
		t.Fatal("Expected connection to fail, but it succeeded")
	}
}

func TestClientSendAgentStart(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	// Setup server
	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Subscribe to events
	receivedEvents := make(chan daemon.Event, 10)
	eventBus.Subscribe(func(event daemon.Event) {
		receivedEvents <- event
	})

	// Create client
	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Send agent start event (empty parent and repo for top-level agent)
	if err := client.SendAgentStart("agent-1", "task-1", "Test Task", "", ""); err != nil {
		t.Fatalf("Failed to send agent start: %v", err)
	}

	// Verify event received
	select {
	case event := <-receivedEvents:
		if event.Type != daemon.EventAgentStarted {
			t.Errorf("Expected EventAgentStarted, got %s", event.Type)
		}
		payload := event.Payload.(map[string]interface{})
		if payload["agent_id"] != "agent-1" {
			t.Errorf("Expected agent_id=agent-1, got %v", payload["agent_id"])
		}
		if payload["task_id"] != "task-1" {
			t.Errorf("Expected task_id=task-1, got %v", payload["task_id"])
		}
		if payload["task_title"] != "Test Task" {
			t.Errorf("Expected task_title='Test Task', got %v", payload["task_title"])
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for event")
	}
}

func TestClientSendAgentStartWithParent(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	// Setup server
	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Subscribe to events
	receivedEvents := make(chan daemon.Event, 10)
	eventBus.Subscribe(func(event daemon.Event) {
		receivedEvents <- event
	})

	// Create client
	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Send agent start event with parent agent ID (no repo)
	if err := client.SendAgentStart("agent-child", "task-1", "Child Task", "agent-parent", ""); err != nil {
		t.Fatalf("Failed to send agent start: %v", err)
	}

	// Verify event received with parent_agent_id
	select {
	case event := <-receivedEvents:
		if event.Type != daemon.EventAgentStarted {
			t.Errorf("Expected EventAgentStarted, got %s", event.Type)
		}
		payload := event.Payload.(map[string]interface{})
		if payload["agent_id"] != "agent-child" {
			t.Errorf("Expected agent_id=agent-child, got %v", payload["agent_id"])
		}
		if payload["parent_agent_id"] != "agent-parent" {
			t.Errorf("Expected parent_agent_id=agent-parent, got %v", payload["parent_agent_id"])
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for event")
	}
}

func TestClientSendAgentOutput(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	receivedEvents := make(chan daemon.Event, 10)
	eventBus.Subscribe(func(event daemon.Event) {
		receivedEvents <- event
	})

	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Test stdout output
	t.Run("Stdout", func(t *testing.T) {
		if err := client.SendAgentOutput("agent-1", "test output", false); err != nil {
			t.Fatalf("Failed to send output: %v", err)
		}

		select {
		case event := <-receivedEvents:
			if event.Type != daemon.EventAgentOutput {
				t.Errorf("Expected EventAgentOutput, got %s", event.Type)
			}
			payload := event.Payload.(map[string]interface{})
			if payload["output"] != "test output" {
				t.Errorf("Expected output='test output', got %v", payload["output"])
			}
			if payload["is_error"] != false {
				t.Errorf("Expected is_error=false, got %v", payload["is_error"])
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for event")
		}
	})

	// Test stderr output
	t.Run("Stderr", func(t *testing.T) {
		if err := client.SendAgentOutput("agent-1", "error output", true); err != nil {
			t.Fatalf("Failed to send output: %v", err)
		}

		select {
		case event := <-receivedEvents:
			payload := event.Payload.(map[string]interface{})
			if payload["is_error"] != true {
				t.Errorf("Expected is_error=true, got %v", payload["is_error"])
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for event")
		}
	})
}

func TestClientSendAgentDone(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	receivedEvents := make(chan daemon.Event, 10)
	eventBus.Subscribe(func(event daemon.Event) {
		receivedEvents <- event
	})

	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	result := &AgentResult{
		ExitCode:        0,
		DurationSeconds: 10.5,
		InputTokens:     100,
		OutputTokens:    200,
		CostUSD:         0.05,
		FilesChanged:    3,
		CommitsCreated:  1,
	}

	if err := client.SendAgentDone("agent-1", "", result); err != nil {
		t.Fatalf("Failed to send agent done: %v", err)
	}

	select {
	case event := <-receivedEvents:
		if event.Type != daemon.EventAgentCompleted {
			t.Errorf("Expected EventAgentCompleted, got %s", event.Type)
		}
		payload := event.Payload.(map[string]interface{})
		if exitCode := payload["exit_code"].(int); exitCode != 0 {
			t.Errorf("Expected exit_code=0, got %v", exitCode)
		}
		if duration := payload["duration"].(float64); duration != 10.5 {
			t.Errorf("Expected duration=10.5, got %v", duration)
		}
		if inputTokens := payload["input_tokens"].(int); inputTokens != 100 {
			t.Errorf("Expected input_tokens=100, got %v", inputTokens)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for event")
	}
}

func TestClientSendAgentDoneNilResult(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Should fail with nil result
	if err := client.SendAgentDone("agent-1", "", nil); err == nil {
		t.Error("Expected error when sending nil result, got nil")
	}
}

func TestClientSendAgentFail(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	receivedEvents := make(chan daemon.Event, 10)
	eventBus.Subscribe(func(event daemon.Event) {
		receivedEvents <- event
	})

	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	result := &AgentResult{
		ExitCode:        1,
		DurationSeconds: 5.0,
	}

	testErr := errors.New("task failed")
	if err := client.SendAgentFail("agent-1", "", testErr, result); err != nil {
		t.Fatalf("Failed to send agent fail: %v", err)
	}

	select {
	case event := <-receivedEvents:
		if event.Type != daemon.EventAgentCompleted {
			t.Errorf("Expected EventAgentCompleted, got %s", event.Type)
		}
		payload := event.Payload.(map[string]interface{})
		if errMsg := payload["error"].(string); errMsg != "task failed" {
			t.Errorf("Expected error='task failed', got %v", errMsg)
		}
		if exitCode := payload["exit_code"].(int); exitCode != 1 {
			t.Errorf("Expected exit_code=1, got %v", exitCode)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for event")
	}
}

func TestClientSendAgentFailNilError(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	result := &AgentResult{ExitCode: 1}

	// Should fail with nil error
	if err := client.SendAgentFail("agent-1", "", nil, result); err == nil {
		t.Error("Expected error when sending nil error, got nil")
	}

	// Should fail with nil result
	if err := client.SendAgentFail("agent-1", "", errors.New("test"), nil); err == nil {
		t.Error("Expected error when sending nil result, got nil")
	}
}

func TestClientSendRunStarted(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	receivedEvents := make(chan daemon.Event, 10)
	eventBus.Subscribe(func(event daemon.Event) {
		receivedEvents <- event
	})

	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	if err := client.SendRunStarted("run-123", 5, nil); err != nil {
		t.Fatalf("Failed to send run started: %v", err)
	}

	select {
	case event := <-receivedEvents:
		// RunStarted is mapped to EventRunStarted by the server
		if event.Type != daemon.EventRunStarted {
			t.Errorf("Expected EventRunStarted, got %s", event.Type)
		}
		payload := event.Payload.(map[string]interface{})
		if runID := payload["run_id"].(string); runID != "run-123" {
			t.Errorf("Expected run_id=run-123, got %v", runID)
		}
		if taskCount := payload["task_count"].(int); taskCount != 5 {
			t.Errorf("Expected task_count=5, got %v", taskCount)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for event")
	}
}

func TestClientSendRunCompleted(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	receivedEvents := make(chan daemon.Event, 10)
	eventBus.Subscribe(func(event daemon.Event) {
		receivedEvents <- event
	})

	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	stats := &RunStats{
		TotalTasks:        10,
		SucceededTasks:    8,
		FailedTasks:       2,
		TotalDuration:     120.5,
		TotalInputTokens:  1000,
		TotalOutputTokens: 2000,
		TotalCostUSD:      0.50,
		FilesChanged:      15,
		ConflictsResolved: 2,
	}

	if err := client.SendRunCompleted("run-123", stats); err != nil {
		t.Fatalf("Failed to send run completed: %v", err)
	}

	select {
	case event := <-receivedEvents:
		// RunCompleted is mapped to EventRunCompleted by the server
		if event.Type != daemon.EventRunCompleted {
			t.Errorf("Expected EventRunCompleted, got %s", event.Type)
		}
		payload := event.Payload.(map[string]interface{})
		if totalTasks := payload["total_tasks"].(int); totalTasks != 10 {
			t.Errorf("Expected total_tasks=10, got %v", totalTasks)
		}
		if succeededTasks := payload["succeeded_tasks"].(int); succeededTasks != 8 {
			t.Errorf("Expected succeeded_tasks=8, got %v", succeededTasks)
		}
		if failedTasks := payload["failed_tasks"].(int); failedTasks != 2 {
			t.Errorf("Expected failed_tasks=2, got %v", failedTasks)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for event")
	}
}

func TestClientSendRunCompletedNilStats(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Should fail with nil stats
	if err := client.SendRunCompleted("run-123", nil); err == nil {
		t.Error("Expected error when sending nil stats, got nil")
	}
}

func TestClientSendWithoutConnection(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	// Create client but don't start server
	client := &Client{socketPath: socketPath}

	// All send operations should fail
	if err := client.SendAgentStart("a1", "t1", "title", "", ""); err == nil {
		t.Error("Expected error when not connected")
	}

	if err := client.SendAgentOutput("a1", "output", false); err == nil {
		t.Error("Expected error when not connected")
	}

	result := &AgentResult{ExitCode: 0}
	if err := client.SendAgentDone("a1", "", result); err == nil {
		t.Error("Expected error when not connected")
	}

	if err := client.SendAgentFail("a1", "", errors.New("err"), result); err == nil {
		t.Error("Expected error when not connected")
	}

	if err := client.SendRunStarted("run1", 5, nil); err == nil {
		t.Error("Expected error when not connected")
	}

	stats := &RunStats{TotalTasks: 5}
	if err := client.SendRunCompleted("run1", stats); err == nil {
		t.Error("Expected error when not connected")
	}
}

func TestClientReconnect(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Verify initial connection
	time.Sleep(50 * time.Millisecond)
	if count := server.ConnectionCount(); count != 1 {
		t.Errorf("Expected 1 connection, got %d", count)
	}

	// Reconnect
	if err := client.Connect(); err != nil {
		t.Fatalf("Failed to reconnect: %v", err)
	}

	// Should still have 1 connection (old one replaced)
	time.Sleep(50 * time.Millisecond)
	if count := server.ConnectionCount(); count != 1 {
		t.Errorf("Expected 1 connection after reconnect, got %d", count)
	}

	// Verify reconnected client can send messages
	receivedEvents := make(chan daemon.Event, 10)
	eventBus.Subscribe(func(event daemon.Event) {
		receivedEvents <- event
	})

	if err := client.SendAgentStart("agent-1", "task-1", "Test", "", ""); err != nil {
		t.Fatalf("Failed to send after reconnect: %v", err)
	}

	select {
	case event := <-receivedEvents:
		if event.Type != daemon.EventAgentStarted {
			t.Errorf("Expected EventAgentStarted, got %s", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for event after reconnect")
	}
}

func TestClientMultipleMessages(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	receivedEvents := make(chan daemon.Event, 100)
	eventBus.Subscribe(func(event daemon.Event) {
		receivedEvents <- event
	})

	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Send multiple messages rapidly
	for i := 0; i < 10; i++ {
		if err := client.SendAgentOutput("agent-1", "output", false); err != nil {
			t.Fatalf("Failed to send message %d: %v", i, err)
		}
	}

	// Verify all messages received
	eventCount := 0
	timeout := time.After(2 * time.Second)
	for eventCount < 10 {
		select {
		case event := <-receivedEvents:
			if event.Type == daemon.EventAgentOutput {
				eventCount++
			}
		case <-timeout:
			t.Fatalf("Timeout: received %d/10 events", eventCount)
		}
	}
}

func TestClientMessageFormat(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Connect directly to socket to inspect raw messages
	rawConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer rawConn.Close()

	// Use the client connection and capture what would be sent
	// We'll verify the message structure by parsing what the server receives

	receivedEvents := make(chan daemon.Event, 10)
	eventBus.Subscribe(func(event daemon.Event) {
		receivedEvents <- event
	})

	// Send a message and verify it arrives properly formatted
	if err := client.SendAgentStart("agent-1", "task-1", "Test", "", ""); err != nil {
		t.Fatalf("Failed to send: %v", err)
	}

	select {
	case event := <-receivedEvents:
		// If we received the event, the format was correct
		if event.Type != daemon.EventAgentStarted {
			t.Errorf("Expected EventAgentStarted, got %s", event.Type)
		}
		// Verify timestamp is recent (within last 5 seconds)
		if time.Since(event.Timestamp) > 5*time.Second {
			t.Errorf("Event timestamp too old: %v", event.Timestamp)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for event")
	}
}

func TestClientDoubleClose(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	eventBus := daemon.NewEventBus()
	server := NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// First close
	if err := client.Close(); err != nil {
		t.Errorf("First close failed: %v", err)
	}

	// Second close should not panic
	if err := client.Close(); err != nil {
		t.Errorf("Second close failed: %v", err)
	}
}
