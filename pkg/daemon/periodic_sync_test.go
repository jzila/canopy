package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/events"
)

// mockBeadsClientForSync extends mockBeadsClient with configurable List behavior and call tracking
type mockBeadsClientForSync struct {
	mockBeadsClient
	listTasks []beads.Task
	listErr   error
	listCalls atomic.Int32
	mu        sync.Mutex
}

func (m *mockBeadsClientForSync) List(_ context.Context) ([]beads.Task, error) {
	m.listCalls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listTasks, nil
}

func (m *mockBeadsClientForSync) setTasks(tasks []beads.Task) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listTasks = tasks
}

func TestDefaultPeriodicSyncConfig(t *testing.T) {
	config := DefaultPeriodicSyncConfig()

	if config.Interval != 30*time.Second {
		t.Errorf("expected default interval 30s, got %v", config.Interval)
	}
	if !config.Enabled {
		t.Error("expected default Enabled to be true")
	}
	if config.SyncAllRepos {
		t.Error("expected default SyncAllRepos to be false")
	}
}

func TestPeriodicSyncManager_Disabled(t *testing.T) {
	config := PeriodicSyncConfig{
		Interval: 100 * time.Millisecond,
		Enabled:  false,
	}

	daemon := newDaemonForTest(Config{}, nil, nil)
	manager := NewPeriodicSyncManager(config, daemon)

	manager.Start()
	defer manager.Stop()

	// Should not be running when disabled
	if manager.IsRunning() {
		t.Error("expected manager to not be running when disabled")
	}
}

func TestPeriodicSyncManager_StartStop(t *testing.T) {
	config := PeriodicSyncConfig{
		Interval: 100 * time.Millisecond,
		Enabled:  true,
	}

	daemon := newDaemonForTest(Config{}, nil, nil)
	manager := NewPeriodicSyncManager(config, daemon)

	// Before start
	if manager.IsRunning() {
		t.Error("expected manager to not be running before Start()")
	}

	// Start
	manager.Start()
	if !manager.IsRunning() {
		t.Error("expected manager to be running after Start()")
	}

	// Double start should be idempotent
	manager.Start()
	if !manager.IsRunning() {
		t.Error("expected manager to still be running after double Start()")
	}

	// Stop
	manager.Stop()
	if manager.IsRunning() {
		t.Error("expected manager to not be running after Stop()")
	}

	// Double stop should be safe
	manager.Stop()
}

func TestPeriodicSyncManager_SyncsActiveRepo(t *testing.T) {
	mockClient := &mockBeadsClientForSync{
		listTasks: []beads.Task{
			{ID: "task-1", Title: "Task 1", Status: "open", Priority: 2},
			{ID: "task-2", Title: "Task 2", Status: "open", Priority: 1},
		},
	}

	daemon := newDaemonForTest(Config{}, nil, mockClient)
	daemon.repoManager.SetActiveRepositoryDirect("test-repo-id")
	daemon.repoManager.mu.Lock()
	daemon.repoManager.clients["test-repo-id"] = mockClient
	daemon.repoManager.mu.Unlock()

	config := PeriodicSyncConfig{
		Interval: 50 * time.Millisecond,
		Enabled:  true,
	}

	manager := NewPeriodicSyncManager(config, daemon)
	manager.Start()

	// Wait for at least one sync to happen
	time.Sleep(150 * time.Millisecond)
	manager.Stop()

	// Should have called List at least once (probably 2-3 times)
	listCalls := mockClient.listCalls.Load()
	if listCalls < 1 {
		t.Errorf("expected at least 1 List call, got %d", listCalls)
	}

	// Verify tasks were synced to state
	state := daemon.GetRuntimeState()
	if len(state.Tasks) != 2 {
		t.Errorf("expected 2 tasks in state, got %d", len(state.Tasks))
	}
}

func TestPeriodicSyncManager_NoActiveRepo(t *testing.T) {
	mockClient := &mockBeadsClientForSync{
		listTasks: []beads.Task{
			{ID: "task-1", Title: "Task 1", Status: "open", Priority: 2},
		},
	}

	daemon := newDaemonForTest(Config{}, nil, mockClient)
	// Don't set active repo - should not call List

	config := PeriodicSyncConfig{
		Interval: 50 * time.Millisecond,
		Enabled:  true,
	}

	manager := NewPeriodicSyncManager(config, daemon)
	manager.Start()

	// Wait for sync attempts
	time.Sleep(150 * time.Millisecond)
	manager.Stop()

	// Should not have called List since no active repo
	listCalls := mockClient.listCalls.Load()
	if listCalls != 0 {
		t.Errorf("expected 0 List calls with no active repo, got %d", listCalls)
	}
}

func TestPeriodicSyncManager_BroadcastsOnChange(t *testing.T) {
	mockClient := &mockBeadsClientForSync{
		listTasks: []beads.Task{
			{ID: "task-1", Title: "Task 1", Status: "open", Priority: 2},
		},
	}

	daemon := newDaemonForTest(Config{}, nil, mockClient)
	daemon.repoManager.SetActiveRepositoryDirect("test-repo-id")
	daemon.repoManager.mu.Lock()
	daemon.repoManager.clients["test-repo-id"] = mockClient
	daemon.repoManager.mu.Unlock()

	// Subscribe to events
	var stateSyncCount atomic.Int32
	daemon.eventBus.Subscribe(func(e events.Event) {
		if e.Type == events.EventStateSync {
			stateSyncCount.Add(1)
		}
	})

	config := PeriodicSyncConfig{
		Interval: 50 * time.Millisecond,
		Enabled:  true,
	}

	manager := NewPeriodicSyncManager(config, daemon)
	manager.Start()

	// Wait for syncs to happen
	time.Sleep(150 * time.Millisecond)
	manager.Stop()

	// Should have broadcast at least one state sync (new task discovered)
	syncCount := stateSyncCount.Load()
	if syncCount < 1 {
		t.Errorf("expected at least 1 state sync event, got %d", syncCount)
	}
}

func TestPeriodicSyncManager_UpdatesExistingTasks(t *testing.T) {
	initialTasks := []beads.Task{
		{ID: "task-1", Title: "Task 1", Status: "open", Priority: 2},
	}

	mockClient := &mockBeadsClientForSync{
		listTasks: initialTasks,
	}

	daemon := newDaemonForTest(Config{}, nil, mockClient)
	daemon.repoManager.SetActiveRepositoryDirect("test-repo-id")
	daemon.repoManager.mu.Lock()
	daemon.repoManager.clients["test-repo-id"] = mockClient
	daemon.repoManager.mu.Unlock()

	config := PeriodicSyncConfig{
		Interval: 50 * time.Millisecond,
		Enabled:  true,
	}

	manager := NewPeriodicSyncManager(config, daemon)
	manager.Start()

	// Wait for first sync
	time.Sleep(75 * time.Millisecond)

	// Verify initial task
	state := daemon.GetRuntimeState()
	if len(state.Tasks) != 1 {
		t.Fatalf("expected 1 task after first sync, got %d", len(state.Tasks))
	}

	// Update mock to return a new task
	mockClient.setTasks([]beads.Task{
		{ID: "task-1", Title: "Task 1", Status: "open", Priority: 2},
		{ID: "task-2", Title: "Task 2", Status: "open", Priority: 1},
	})

	// Wait for another sync
	time.Sleep(75 * time.Millisecond)
	manager.Stop()

	// Verify both tasks are now present
	if len(state.Tasks) != 2 {
		t.Errorf("expected 2 tasks after update sync, got %d", len(state.Tasks))
	}
}

func TestPeriodicSyncManager_GetConfig(t *testing.T) {
	config := PeriodicSyncConfig{
		Interval:     45 * time.Second,
		Enabled:      true,
		SyncAllRepos: true,
	}

	daemon := newDaemonForTest(Config{}, nil, nil)
	manager := NewPeriodicSyncManager(config, daemon)

	gotConfig := manager.GetConfig()

	if gotConfig.Interval != 45*time.Second {
		t.Errorf("expected interval 45s, got %v", gotConfig.Interval)
	}
	if !gotConfig.Enabled {
		t.Error("expected Enabled to be true")
	}
	if !gotConfig.SyncAllRepos {
		t.Error("expected SyncAllRepos to be true")
	}
}

func TestPeriodicSyncManager_GracefulShutdown(t *testing.T) {
	mockClient := &mockBeadsClientForSync{
		listTasks: []beads.Task{
			{ID: "task-1", Title: "Task 1", Status: "open", Priority: 2},
		},
	}

	daemon := newDaemonForTest(Config{}, nil, mockClient)
	daemon.repoManager.SetActiveRepositoryDirect("test-repo-id")
	daemon.repoManager.mu.Lock()
	daemon.repoManager.clients["test-repo-id"] = mockClient
	daemon.repoManager.mu.Unlock()

	config := PeriodicSyncConfig{
		Interval: 10 * time.Millisecond,
		Enabled:  true,
	}

	manager := NewPeriodicSyncManager(config, daemon)
	manager.Start()

	// Let it run for a bit
	time.Sleep(50 * time.Millisecond)

	// Stop should complete promptly (not hang)
	done := make(chan struct{})
	go func() {
		manager.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Good - stopped in time
	case <-time.After(1 * time.Second):
		t.Error("Stop() did not complete in time")
	}
}

func TestDaemonInit_CreatesPeriodicSyncManager(t *testing.T) {
	daemon := &Daemon{}
	daemon.Init()

	if daemon.GetPeriodicSyncManager() == nil {
		t.Error("expected PeriodicSyncManager to be created during Init()")
	}

	// Verify default config is applied
	config := daemon.GetPeriodicSyncManager().GetConfig()
	if config.Interval != 30*time.Second {
		t.Errorf("expected default interval 30s, got %v", config.Interval)
	}
}

func TestDaemonConfig_PeriodicSyncConfig(t *testing.T) {
	config := Config{
		PeriodicSync: PeriodicSyncConfig{
			Interval:     60 * time.Second,
			Enabled:      true,
			SyncAllRepos: true,
		},
	}

	daemon := &Daemon{config: config}
	daemon.Init()

	if daemon.GetPeriodicSyncManager() == nil {
		t.Fatal("expected PeriodicSyncManager to be created")
	}

	syncConfig := daemon.GetPeriodicSyncManager().GetConfig()
	if syncConfig.Interval != 60*time.Second {
		t.Errorf("expected custom interval 60s, got %v", syncConfig.Interval)
	}
	if !syncConfig.SyncAllRepos {
		t.Error("expected SyncAllRepos to be true")
	}
}

func TestPeriodicSyncManager_GarbageCollectsRemovedTasks(t *testing.T) {
	// Start with two tasks
	initialTasks := []beads.Task{
		{ID: "task-1", Title: "Task 1", Status: "open", Priority: 2},
		{ID: "task-2", Title: "Task 2", Status: "open", Priority: 1},
	}

	mockClient := &mockBeadsClientForSync{
		listTasks: initialTasks,
	}

	daemon := newDaemonForTest(Config{}, nil, mockClient)
	daemon.repoManager.SetActiveRepositoryDirect("test-repo-id")
	daemon.repoManager.mu.Lock()
	daemon.repoManager.clients["test-repo-id"] = mockClient
	daemon.repoManager.mu.Unlock()

	config := PeriodicSyncConfig{
		Interval: 50 * time.Millisecond,
		Enabled:  true,
	}

	manager := NewPeriodicSyncManager(config, daemon)
	manager.Start()

	// Wait for first sync
	time.Sleep(75 * time.Millisecond)

	// Verify both tasks exist
	state := daemon.GetRuntimeState()
	if len(state.Tasks) != 2 {
		t.Fatalf("expected 2 tasks after first sync, got %d", len(state.Tasks))
	}
	if _, exists := state.Tasks["task-1"]; !exists {
		t.Error("task-1 should exist after first sync")
	}
	if _, exists := state.Tasks["task-2"]; !exists {
		t.Error("task-2 should exist after first sync")
	}

	// Simulate closing task-1 in beads (remove from list)
	mockClient.setTasks([]beads.Task{
		{ID: "task-2", Title: "Task 2", Status: "open", Priority: 1},
	})

	// Wait for another sync to garbage collect the removed task
	time.Sleep(75 * time.Millisecond)
	manager.Stop()

	// Verify task-1 is garbage collected, task-2 remains
	if len(state.Tasks) != 1 {
		t.Errorf("expected 1 task after garbage collection, got %d", len(state.Tasks))
	}
	if _, exists := state.Tasks["task-1"]; exists {
		t.Error("task-1 should be removed after garbage collection")
	}
	if _, exists := state.Tasks["task-2"]; !exists {
		t.Error("task-2 should still exist after garbage collection")
	}
}

func TestPeriodicSyncManager_GarbageCollectsAllTasks(t *testing.T) {
	// Start with tasks, then simulate all being closed
	initialTasks := []beads.Task{
		{ID: "task-1", Title: "Task 1", Status: "open", Priority: 2},
		{ID: "task-2", Title: "Task 2", Status: "open", Priority: 1},
	}

	mockClient := &mockBeadsClientForSync{
		listTasks: initialTasks,
	}

	daemon := newDaemonForTest(Config{}, nil, mockClient)
	daemon.repoManager.SetActiveRepositoryDirect("test-repo-id")
	daemon.repoManager.mu.Lock()
	daemon.repoManager.clients["test-repo-id"] = mockClient
	daemon.repoManager.mu.Unlock()

	config := PeriodicSyncConfig{
		Interval: 50 * time.Millisecond,
		Enabled:  true,
	}

	manager := NewPeriodicSyncManager(config, daemon)
	manager.Start()

	// Wait for first sync
	time.Sleep(75 * time.Millisecond)

	// Verify tasks exist
	state := daemon.GetRuntimeState()
	if len(state.Tasks) != 2 {
		t.Fatalf("expected 2 tasks after first sync, got %d", len(state.Tasks))
	}

	// Simulate all tasks being closed in beads
	mockClient.setTasks([]beads.Task{})

	// Wait for another sync to garbage collect
	time.Sleep(75 * time.Millisecond)
	manager.Stop()

	// Verify all tasks are removed
	if len(state.Tasks) != 0 {
		t.Errorf("expected 0 tasks after all closed, got %d", len(state.Tasks))
	}
}
