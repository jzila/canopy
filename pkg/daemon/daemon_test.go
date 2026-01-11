package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/persistence"
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

// TestRestoreStateFromDB verifies that state is restored from database on daemon init
func TestRestoreStateFromDB(t *testing.T) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "canopy-daemon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create a running run with agents in the database
	now := time.Now()
	run := &persistence.Run{
		ID:          "run-to-restore",
		StartedAt:   now.Add(-time.Hour),
		Status:      persistence.RunStatusRunning,
		Concurrency: 4,
		TotalTasks:  5,
	}
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Create agents with various statuses
	agents := []*persistence.Agent{
		{
			ID:           "agent-1",
			RunID:        "run-to-restore",
			TaskID:       "task-1",
			TaskTitle:    "Task 1",
			Status:       persistence.AgentStatusCompleted,
			StartedAt:    now.Add(-50 * time.Minute),
			InputTokens:  1000,
			OutputTokens: 500,
			TotalTokens:  1500,
			CostUSD:      0.05,
		},
		{
			ID:           "agent-2",
			RunID:        "run-to-restore",
			TaskID:       "task-2",
			TaskTitle:    "Task 2",
			Status:       persistence.AgentStatusRunning,
			StartedAt:    now.Add(-30 * time.Minute),
			InputTokens:  2000,
			OutputTokens: 800,
			TotalTokens:  2800,
			CostUSD:      0.08,
		},
		{
			ID:              "agent-3",
			RunID:           "run-to-restore",
			TaskID:          "task-3",
			TaskTitle:       "Task 3",
			Status:          persistence.AgentStatusFailed,
			StartedAt:       now.Add(-20 * time.Minute),
			ErrorMessage:    "test error",
			DurationSeconds: 120.5,
		},
	}
	for _, agent := range agents {
		if err := store.CreateAgent(agent); err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
	}

	store.Close()

	// Now create a daemon with persistence enabled using the same database
	store2, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}

	daemon := &Daemon{
		config:           Config{EnablePersistence: true},
		eventBus:         NewEventBus(),
		state:            NewRuntimeState(),
		persistenceStore: store2,
	}

	// Manually call restoreStateFromDB
	if err := daemon.restoreStateFromDB(); err != nil {
		t.Fatalf("restoreStateFromDB failed: %v", err)
	}

	// Verify state was restored
	state := daemon.GetRuntimeState()
	if state == nil {
		t.Fatal("state should not be nil")
	}

	if len(state.Agents) != 3 {
		t.Errorf("expected 3 agents, got %d", len(state.Agents))
	}

	// Check agent-1 was restored correctly
	agent1 := state.GetAgent("agent-1")
	if agent1 == nil {
		t.Fatal("agent-1 should exist")
	}
	if agent1.TaskID != "task-1" {
		t.Errorf("expected task-1, got %s", agent1.TaskID)
	}
	if agent1.Status != AgentStatusCompleted {
		t.Errorf("expected completed status, got %s", agent1.Status)
	}
	if agent1.TokenUsage.InputTokens != 1000 {
		t.Errorf("expected 1000 input tokens, got %d", agent1.TokenUsage.InputTokens)
	}

	// Check agent-2 was restored correctly
	agent2 := state.GetAgent("agent-2")
	if agent2 == nil {
		t.Fatal("agent-2 should exist")
	}
	if agent2.Status != AgentStatusRunning {
		t.Errorf("expected running status, got %s", agent2.Status)
	}

	// Check agent-3 was restored with error
	agent3 := state.GetAgent("agent-3")
	if agent3 == nil {
		t.Fatal("agent-3 should exist")
	}
	if agent3.Status != AgentStatusFailed {
		t.Errorf("expected failed status, got %s", agent3.Status)
	}
	if agent3.Error != "test error" {
		t.Errorf("expected 'test error', got %s", agent3.Error)
	}
	if agent3.Duration != 120.5 {
		t.Errorf("expected duration 120.5, got %f", agent3.Duration)
	}

	// Verify stats were updated
	state.UpdateStats()
	if state.Stats.CompletedTasks != 1 {
		t.Errorf("expected 1 completed task, got %d", state.Stats.CompletedTasks)
	}
	if state.Stats.FailedTasks != 1 {
		t.Errorf("expected 1 failed task, got %d", state.Stats.FailedTasks)
	}
	if state.Stats.RunningTasks != 1 {
		t.Errorf("expected 1 running task, got %d", state.Stats.RunningTasks)
	}

	store2.Close()
}

// TestRestoreStateFromDB_NoRunningRun verifies no restoration happens when no running run exists
func TestRestoreStateFromDB_NoRunningRun(t *testing.T) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "canopy-daemon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create only completed runs
	now := time.Now()
	run := &persistence.Run{
		ID:        "run-completed",
		StartedAt: now.Add(-time.Hour),
		Status:    persistence.RunStatusCompleted,
	}
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	daemon := &Daemon{
		config:           Config{EnablePersistence: true},
		eventBus:         NewEventBus(),
		state:            NewRuntimeState(),
		persistenceStore: store,
	}

	// Manually call restoreStateFromDB
	if err := daemon.restoreStateFromDB(); err != nil {
		t.Fatalf("restoreStateFromDB failed: %v", err)
	}

	// Verify no agents were restored
	state := daemon.GetRuntimeState()
	if len(state.Agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(state.Agents))
	}

	store.Close()
}
