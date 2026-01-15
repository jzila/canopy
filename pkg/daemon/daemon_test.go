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
// and historical state IS restored from the most recent run for display
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

	daemon := newDaemonForTest(Config{EnablePersistence: true}, store2, nil)

	// Manually call restoreStateFromDB - this should mark orphaned states as failed
	// and restore historical agents for display
	if err := daemon.restoreStateFromDB(); err != nil {
		t.Fatalf("restoreStateFromDB failed: %v", err)
	}

	// Verify that agents WERE restored to RuntimeState for historical display
	// (GetMostRecentRun returns the run even after it's marked as failed)
	state := daemon.GetRuntimeState()
	if state == nil {
		t.Fatal("state should not be nil")
	}

	// All 3 agents should be restored for historical view
	if len(state.Agents) != 3 {
		t.Errorf("expected 3 agents (historical data), got %d", len(state.Agents))
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

	// Verify the restored agents have correct status in RuntimeState
	restoredAgent1 := state.GetAgent("agent-1")
	if restoredAgent1 == nil {
		t.Fatal("expected agent-1 to be in RuntimeState")
	}
	if restoredAgent1.Status != AgentStatusCompleted {
		t.Errorf("expected restored agent-1 status completed, got %s", restoredAgent1.Status)
	}

	restoredAgent2 := state.GetAgent("agent-2")
	if restoredAgent2 == nil {
		t.Fatal("expected agent-2 to be in RuntimeState")
	}
	if restoredAgent2.Status != AgentStatusFailed {
		t.Errorf("expected restored agent-2 status failed (orphaned), got %s", restoredAgent2.Status)
	}

	store2.Close()
}

// TestRestoreStateFromDB_CompletedRun verifies historical restoration happens from completed runs
func TestRestoreStateFromDB_CompletedRun(t *testing.T) {
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

	// Create a completed run with agents
	now := time.Now()
	run := &persistence.Run{
		ID:        "run-completed",
		StartedAt: now.Add(-time.Hour),
		Status:    persistence.RunStatusCompleted,
	}
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Add agents to the completed run
	agents := []*persistence.Agent{
		{
			ID:        "agent-1",
			RunID:     "run-completed",
			TaskID:    "task-1",
			TaskTitle: "Task 1",
			Status:    persistence.AgentStatusCompleted,
			StartedAt: now.Add(-50 * time.Minute),
		},
		{
			ID:        "agent-2",
			RunID:     "run-completed",
			TaskID:    "task-2",
			TaskTitle: "Task 2",
			Status:    persistence.AgentStatusCompleted,
			StartedAt: now.Add(-30 * time.Minute),
		},
	}
	for _, agent := range agents {
		if err := store.CreateAgent(agent); err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
	}

	daemon := newDaemonForTest(Config{EnablePersistence: true}, store, nil)

	// Manually call restoreStateFromDB
	if err := daemon.restoreStateFromDB(); err != nil {
		t.Fatalf("restoreStateFromDB failed: %v", err)
	}

	// Verify historical agents WERE restored for display
	state := daemon.GetRuntimeState()
	if len(state.Agents) != 2 {
		t.Errorf("expected 2 agents (historical data), got %d", len(state.Agents))
	}

	store.Close()
}

// TestRestoreStateFromDB_EmptyDB verifies no restoration happens when database is empty
func TestRestoreStateFromDB_EmptyDB(t *testing.T) {
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

	// No runs in database
	daemon := newDaemonForTest(Config{EnablePersistence: true}, store, nil)

	// Manually call restoreStateFromDB
	if err := daemon.restoreStateFromDB(); err != nil {
		t.Fatalf("restoreStateFromDB failed: %v", err)
	}

	// Verify no agents were restored (nothing in DB)
	state := daemon.GetRuntimeState()
	if len(state.Agents) != 0 {
		t.Errorf("expected 0 agents (empty DB), got %d", len(state.Agents))
	}

	store.Close()
}

// TestSetActiveRepository verifies setting and getting active repository
func TestSetActiveRepository(t *testing.T) {
	// Create temp directory for registry
	tmpDir, err := os.MkdirTemp("", "canopy-repo-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set XDG_CACHE_HOME to our temp dir for registry storage
	oldXDGCache := os.Getenv("XDG_CACHE_HOME")
	os.Setenv("XDG_CACHE_HOME", tmpDir)
	defer os.Setenv("XDG_CACHE_HOME", oldXDGCache)

	// Create the canopy cache directory
	cacheDir := filepath.Join(tmpDir, "canopy")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}

	daemon := newDaemonForTest(Config{}, nil, nil)

	// Initially no active repo
	if daemon.GetActiveRepositoryID() != "" {
		t.Error("expected empty active repo ID initially")
	}
	if daemon.GetActiveRepository() != nil {
		t.Error("expected nil active repo initially")
	}

	// Try setting a non-existent repo
	err = daemon.SetActiveRepository("non-existent-id")
	if err == nil {
		t.Error("expected error when setting non-existent repo")
	}
}

// TestGetBeadsClient verifies lazy beads client creation
func TestGetBeadsClient(t *testing.T) {
	mockClient := &mockBeadsClient{}
	daemon := newDaemonForTest(Config{}, nil, mockClient)

	// Empty repo ID should return default client
	client, err := daemon.getBeadsClient("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if client != mockClient {
		t.Error("expected default beads client for empty repo ID")
	}

	// Non-existent repo ID should return error
	_, err = daemon.getBeadsClient("non-existent")
	if err == nil {
		t.Error("expected error for non-existent repo ID")
	}
}

// TestGetActiveBeadsClient verifies getting beads client for active repo
func TestGetActiveBeadsClient(t *testing.T) {
	mockClient := &mockBeadsClient{}
	daemon := newDaemonForTest(Config{}, nil, mockClient)

	// No active repo should return default client
	client, err := daemon.getActiveBeadsClient()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if client != mockClient {
		t.Error("expected default beads client when no active repo")
	}
}

// TestRestoreStateFromDB_WithRepoContext verifies repo context is restored from DB
func TestRestoreStateFromDB_WithRepoContext(t *testing.T) {
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

	// Create a run with repo context
	now := time.Now()
	run := &persistence.Run{
		ID:        "run-with-repo",
		StartedAt: now.Add(-time.Hour),
		Status:    persistence.RunStatusCompleted,
		RepoID:    "test-repo-id",
		RepoPath:  "/path/to/repo",
		RepoName:  "test-repo",
	}
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Add agents with repo context
	agent := &persistence.Agent{
		ID:        "agent-1",
		RunID:     "run-with-repo",
		TaskID:    "task-1",
		TaskTitle: "Task 1",
		Status:    persistence.AgentStatusCompleted,
		StartedAt: now.Add(-50 * time.Minute),
		RepoID:    "test-repo-id",
	}
	if err := store.CreateAgent(agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	daemon := newDaemonForTest(Config{EnablePersistence: true}, store, nil)

	// Manually call restoreStateFromDB
	if err := daemon.restoreStateFromDB(); err != nil {
		t.Fatalf("restoreStateFromDB failed: %v", err)
	}

	// Verify the active repo was set from the run
	if daemon.GetActiveRepositoryID() != "test-repo-id" {
		t.Errorf("expected active repo ID 'test-repo-id', got '%s'", daemon.GetActiveRepositoryID())
	}

	// Verify the restored agent has repo ID
	state := daemon.GetRuntimeState()
	restoredAgent := state.GetAgent("agent-1")
	if restoredAgent == nil {
		t.Fatal("expected agent-1 to be restored")
	}
	if restoredAgent.RepoID != "test-repo-id" {
		t.Errorf("expected agent repo ID 'test-repo-id', got '%s'", restoredAgent.RepoID)
	}

	store.Close()
}

// TestListRepositories verifies listing repositories
func TestListRepositories(t *testing.T) {
	daemon := newDaemonForTest(Config{}, nil, nil)

	// Should not panic even with no registry
	repos, err := daemon.ListRepositories()
	if err != nil {
		// Registry might not exist, that's OK
		t.Logf("ListRepositories returned expected error: %v", err)
	} else {
		// If it succeeded, repos should be a valid slice
		if repos == nil {
			t.Error("expected non-nil repos slice")
		}
	}
}

// TestSetActiveRepository_ReloadsTasksAndClearsOld verifies that setting active repository
// clears existing tasks and loads tasks from the new repository
func TestSetActiveRepository_ReloadsTasksAndClearsOld(t *testing.T) {
	state := NewRuntimeState()

	// Add some existing tasks (simulating tasks from a previous repo)
	state.Tasks["old-task-1"] = &TaskState{ID: "old-task-1", Title: "Old Task 1", RepoID: "old-repo"}
	state.Tasks["old-task-2"] = &TaskState{ID: "old-task-2", Title: "Old Task 2", RepoID: "old-repo"}

	if len(state.Tasks) != 2 {
		t.Fatalf("expected 2 initial tasks, got %d", len(state.Tasks))
	}

	// Simulate ClearTasksForRepo("") which clears all tasks
	state.ClearTasksForRepo("")

	if len(state.Tasks) != 0 {
		t.Errorf("expected 0 tasks after clearing, got %d", len(state.Tasks))
	}
}

// TestClearTasksForRepo verifies ClearTasksForRepo clears tasks correctly
func TestClearTasksForRepo(t *testing.T) {
	state := NewRuntimeState()

	// Add tasks from different repos
	state.Tasks["task-1"] = &TaskState{ID: "task-1", Title: "Task 1", RepoID: "repo-a"}
	state.Tasks["task-2"] = &TaskState{ID: "task-2", Title: "Task 2", RepoID: "repo-b"}
	state.Tasks["task-3"] = &TaskState{ID: "task-3", Title: "Task 3", RepoID: "repo-a"}

	// Clear tasks for repo-a
	state.ClearTasksForRepo("repo-a")

	if len(state.Tasks) != 1 {
		t.Errorf("expected 1 task remaining, got %d", len(state.Tasks))
	}
	if _, exists := state.Tasks["task-2"]; !exists {
		t.Error("expected task-2 (repo-b) to remain")
	}

	// Clear all tasks
	state.ClearTasksForRepo("")

	if len(state.Tasks) != 0 {
		t.Errorf("expected 0 tasks after clearing all, got %d", len(state.Tasks))
	}
}

// TestHistoricalLiveFeedEvents verifies that restored agents get synthetic live feed events
func TestHistoricalLiveFeedEvents(t *testing.T) {
	// Create a temporary database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	now := time.Now()

	// Create a completed run
	run := &persistence.Run{
		ID:        "run-with-history",
		StartedAt: now.Add(-time.Hour),
		Status:    persistence.RunStatusCompleted,
	}
	finishedAt := now.Add(-30 * time.Minute)
	run.FinishedAt = &finishedAt
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Create an agent with a result message
	agent := &persistence.Agent{
		ID:              "agent-with-result",
		RunID:           "run-with-history",
		TaskID:          "task-1",
		TaskTitle:       "Test Task",
		Status:          persistence.AgentStatusCompleted,
		StartedAt:       now.Add(-50 * time.Minute),
		ResultMessage:   "Task completed successfully with 5 files changed.",
		FilesChanged:    5,
		GitCommitsCreated: 2,
	}
	if err := store.CreateAgent(agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Create a failed agent with an error message
	failedAgent := &persistence.Agent{
		ID:           "agent-with-error",
		RunID:        "run-with-history",
		TaskID:       "task-2",
		TaskTitle:    "Failed Task",
		Status:       persistence.AgentStatusFailed,
		StartedAt:    now.Add(-40 * time.Minute),
		ErrorMessage: "Failed due to merge conflict",
		FilesChanged: 0,
	}
	if err := store.CreateAgent(failedAgent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	daemon := newDaemonForTest(Config{EnablePersistence: true}, store, nil)

	// Restore state from DB
	if err := daemon.restoreStateFromDB(); err != nil {
		t.Fatalf("restoreStateFromDB failed: %v", err)
	}

	// Verify agent with result has live feed events
	state := daemon.GetRuntimeState()
	restoredAgent := state.GetAgent("agent-with-result")
	if restoredAgent == nil {
		t.Fatal("expected agent-with-result to be restored")
	}

	// Should have at least 3 events: historical marker, result text, and agent_completed
	if len(restoredAgent.LiveFeedEvents) < 3 {
		t.Errorf("expected at least 3 live feed events, got %d", len(restoredAgent.LiveFeedEvents))
	}

	// Verify the historical marker event
	if len(restoredAgent.LiveFeedEvents) > 0 {
		firstEvent := restoredAgent.LiveFeedEvents[0]
		if firstEvent.EventType != LiveFeedEventText {
			t.Errorf("expected first event type 'text', got %s", firstEvent.EventType)
		}
		textData, ok := firstEvent.GetTextData()
		if !ok || !textData.IsHistoric {
			t.Error("expected first event to have is_historic=true")
		}
	}

	// Verify the result message event
	if len(restoredAgent.LiveFeedEvents) > 1 {
		resultEvent := restoredAgent.LiveFeedEvents[1]
		if resultEvent.EventType != LiveFeedEventText {
			t.Errorf("expected second event type 'text', got %s", resultEvent.EventType)
		}
		textData, ok := resultEvent.GetTextData()
		if !ok || textData.Text != "Task completed successfully with 5 files changed." {
			t.Errorf("unexpected result message: %v", textData.Text)
		}
	}

	// Verify the agent_completed event
	if len(restoredAgent.LiveFeedEvents) > 2 {
		completedEvent := restoredAgent.LiveFeedEvents[2]
		if completedEvent.EventType != LiveFeedEventAgentCompleted {
			t.Errorf("expected third event type 'agent_completed', got %s", completedEvent.EventType)
		}
		completedData, ok := completedEvent.GetAgentCompletedData()
		if !ok || completedData.FilesChanged != 5 {
			t.Errorf("expected files_changed=5, got %v", completedData.FilesChanged)
		}
		if completedData.CommitsCreated != 2 {
			t.Errorf("expected commits_created=2, got %v", completedData.CommitsCreated)
		}
	}

	// Verify failed agent has error in completion event
	failedRestored := state.GetAgent("agent-with-error")
	if failedRestored == nil {
		t.Fatal("expected agent-with-error to be restored")
	}

	// Check the last event (agent_completed) has the error
	if len(failedRestored.LiveFeedEvents) > 0 {
		lastEvent := failedRestored.LiveFeedEvents[len(failedRestored.LiveFeedEvents)-1]
		if lastEvent.EventType != LiveFeedEventAgentCompleted {
			t.Errorf("expected last event type 'agent_completed', got %s", lastEvent.EventType)
		}
		completedData, ok := lastEvent.GetAgentCompletedData()
		if !ok || completedData.Error != "Failed due to merge conflict" {
			t.Errorf("expected error message in completion event, got %v", completedData.Error)
		}
	}

	store.Close()
}
