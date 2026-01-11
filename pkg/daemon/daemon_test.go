package daemon

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/persistence"
)

// getAvailablePort finds an available port starting from 8081, iterating up to avoid
// conflicts with the production daemon which typically runs on port 8080.
func getAvailablePort(t *testing.T) int {
	t.Helper()
	for port := 8081; port < 8200; port++ {
		addr := fmt.Sprintf(":%d", port)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			continue // Port in use, try next
		}
		listener.Close()
		return port
	}
	t.Fatal("Could not find an available port in range 8081-8199")
	return 0
}

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
	port := getAvailablePort(t)
	config := Config{
		Port:       port,
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

// TestRestoreStateFromDB verifies that orphaned runs/agents are marked as failed
// and state is NOT restored (since orphaned runs are no longer "running")
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
	// This simulates a daemon that crashed while a run was in progress
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
			Status:       persistence.AgentStatusRunning, // This should be marked as failed
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

	// Manually call restoreStateFromDB - this should mark orphaned states as failed
	if err := daemon.restoreStateFromDB(); err != nil {
		t.Fatalf("restoreStateFromDB failed: %v", err)
	}

	// Verify that NO agents were restored to RuntimeState
	// (orphaned run was marked as failed, so GetRunningRun returns nil)
	state := daemon.GetRuntimeState()
	if state == nil {
		t.Fatal("state should not be nil")
	}

	if len(state.Agents) != 0 {
		t.Errorf("expected 0 agents (orphaned run marked as failed), got %d", len(state.Agents))
	}

	// Verify the orphaned run and agent were marked as failed in the database
	restoredRun, err := store2.GetRun("run-to-restore")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if restoredRun.Status != persistence.RunStatusFailed {
		t.Errorf("expected run status to be failed (orphaned), got %s", restoredRun.Status)
	}
	if restoredRun.FinishedAt == nil {
		t.Error("expected orphaned run to have finished_at set")
	}

	// Verify agent-2 (which was running) is now marked as failed
	agent2, err := store2.GetAgent("agent-2")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent2.Status != persistence.AgentStatusFailed {
		t.Errorf("expected agent-2 status to be failed (orphaned), got %s", agent2.Status)
	}
	if agent2.ErrorMessage != "daemon terminated unexpectedly" {
		t.Errorf("expected error message 'daemon terminated unexpectedly', got %s", agent2.ErrorMessage)
	}

	// Verify agent-1 (completed) and agent-3 (already failed) are unchanged
	agent1, err := store2.GetAgent("agent-1")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent1.Status != persistence.AgentStatusCompleted {
		t.Errorf("expected agent-1 to remain completed, got %s", agent1.Status)
	}

	agent3, err := store2.GetAgent("agent-3")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent3.Status != persistence.AgentStatusFailed {
		t.Errorf("expected agent-3 to remain failed, got %s", agent3.Status)
	}
	if agent3.ErrorMessage != "test error" {
		t.Errorf("expected agent-3 error message 'test error', got %s", agent3.ErrorMessage)
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
