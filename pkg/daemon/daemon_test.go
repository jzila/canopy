package daemon

import (
	"testing"
	"time"
)

// mockIPCServer implements IPCServer interface for testing
type mockIPCServer struct {
	started bool
	stopped bool
}

func (m *mockIPCServer) Start() error {
	m.started = true
	return nil
}

func (m *mockIPCServer) Stop() error {
	m.stopped = true
	return nil
}

// TestDaemonInitialization verifies daemon component initialization
func TestDaemonInitialization(t *testing.T) {
	config := Config{
		Port:       8080,
		SocketPath: "/tmp/test-canopy.sock",
	}

	mockIPC := &mockIPCServer{}
	mockSched := &mockScheduler{}
	mockBeads := &mockBeadsClient{}

	// Factory function that returns our mock IPC server
	ipcFactory := func(socketPath string, eventBus *EventBus) IPCServer {
		if socketPath != config.SocketPath {
			t.Errorf("Expected socketPath %s, got %s", config.SocketPath, socketPath)
		}
		if eventBus == nil {
			t.Error("EventBus should not be nil")
		}
		return mockIPC
	}

	daemon := NewDaemon(config, ipcFactory, mockSched, mockBeads)

	// Start daemon in a goroutine since it blocks
	errChan := make(chan error, 1)
	go func() {
		errChan <- daemon.Start()
	}()

	// Give it time to initialize
	time.Sleep(100 * time.Millisecond)

	// Verify components were initialized
	if daemon.GetEventBus() == nil {
		t.Error("EventBus should be initialized")
	}

	if daemon.state == nil {
		t.Error("RuntimeState should be initialized")
	}

	if !mockIPC.started {
		t.Error("IPC server should be started")
	}

	// Stop daemon
	if err := daemon.Stop(); err != nil {
		t.Errorf("Stop() failed: %v", err)
	}

	if !mockIPC.stopped {
		t.Error("IPC server should be stopped")
	}

	// Check for errors from Start()
	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Start() returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		// Timeout is ok, Start() might still be running
	}
}

// TestBroadcastEvent verifies event broadcasting
func TestBroadcastEvent(t *testing.T) {
	daemon := &Daemon{}
	daemon.eventBus = NewEventBus()

	eventReceived := false
	daemon.eventBus.Subscribe(func(e Event) {
		if e.Type == EventAgentStarted {
			eventReceived = true
		}
	})

	// Broadcast an event
	daemon.BroadcastEvent(Event{
		Type:      EventAgentStarted,
		Timestamp: time.Now(),
		Payload:   map[string]interface{}{"test": "data"},
	})

	// Give event time to propagate
	time.Sleep(10 * time.Millisecond)

	if !eventReceived {
		t.Error("Event should have been received by subscriber")
	}
}

// TestGetState verifies state snapshot retrieval
func TestGetState(t *testing.T) {
	daemon := &Daemon{}
	daemon.state = NewRuntimeState()

	// Add test data
	agent := &AgentState{
		ID:        "agent-1",
		TaskID:    "task-1",
		TaskTitle: "Test Task",
		Status:    AgentStatusRunning,
		StartTime: time.Now(),
	}
	daemon.state.AddAgent(agent)

	// Get snapshot
	snapshot := daemon.GetState()

	if len(snapshot.Agents) != 1 {
		t.Errorf("Expected 1 agent, got %d", len(snapshot.Agents))
	}

	if snapshot.Agents["agent-1"].TaskID != "task-1" {
		t.Errorf("Expected task-1, got %s", snapshot.Agents["agent-1"].TaskID)
	}
}

// TestDaemonInit verifies Init() initializes components
func TestDaemonInit(t *testing.T) {
	daemon := &Daemon{}

	// Before Init
	if daemon.GetEventBus() != nil {
		t.Error("EventBus should be nil before Init")
	}
	if daemon.GetRuntimeState() != nil {
		t.Error("RuntimeState should be nil before Init")
	}

	// Call Init
	daemon.Init()

	// After Init
	if daemon.GetEventBus() == nil {
		t.Error("EventBus should be initialized after Init")
	}
	if daemon.GetRuntimeState() == nil {
		t.Error("RuntimeState should be initialized after Init")
	}

	// Init should be idempotent
	eventBus := daemon.GetEventBus()
	state := daemon.GetRuntimeState()
	daemon.Init()

	if daemon.GetEventBus() != eventBus {
		t.Error("Init should not re-create EventBus")
	}
	if daemon.GetRuntimeState() != state {
		t.Error("Init should not re-create RuntimeState")
	}
}

// TestGetRuntimeState verifies GetRuntimeState returns the actual state
func TestGetRuntimeState(t *testing.T) {
	daemon := &Daemon{}
	daemon.Init()

	state := daemon.GetRuntimeState()
	if state == nil {
		t.Error("GetRuntimeState should return non-nil after Init")
	}

	// Verify we can modify through the returned pointer
	agent := &AgentState{
		ID:        "agent-1",
		TaskID:    "task-1",
		TaskTitle: "Test Task",
		Status:    AgentStatusRunning,
		StartTime: time.Now(),
	}
	state.AddAgent(agent)

	// Verify change is reflected
	if daemon.GetRuntimeState().GetAgent("agent-1") == nil {
		t.Error("Agent should be accessible after adding to returned state")
	}
}
