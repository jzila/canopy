package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jzila/canopy/pkg/beads"
)

// mockBeadsClientWithList extends mockBeadsClient with configurable List behavior
type mockBeadsClientWithList struct {
	mockBeadsClient
	listTasks []beads.Task
	listErr   error
}

func (m *mockBeadsClientWithList) List(_ context.Context) ([]beads.Task, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listTasks, nil
}

func TestHandleSyncBeads_MethodNotAllowed(t *testing.T) {
	daemon := newDaemonForTest(Config{}, nil, nil)
	handler := NewBeadsHandler(daemon)

	req := httptest.NewRequest(http.MethodGet, "/api/beads/sync", nil)
	w := httptest.NewRecorder()

	handler.HandleSyncBeads(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandleSyncBeads_NoActiveRepo(t *testing.T) {
	daemon := newDaemonForTest(Config{}, nil, nil)
	handler := NewBeadsHandler(daemon)

	req := httptest.NewRequest(http.MethodPost, "/api/beads/sync", nil)
	w := httptest.NewRecorder()

	handler.HandleSyncBeads(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestHandleSyncBeads_WithActiveRepo(t *testing.T) {
	mockClient := &mockBeadsClientWithList{
		listTasks: []beads.Task{
			{ID: "task-1", Title: "Task 1", Status: "open", Priority: 2},
			{ID: "task-2", Title: "Task 2", Status: "open", Priority: 1},
		},
	}

	daemon := newDaemonForTest(Config{}, nil, mockClient)
	// Set an active repo directly on the manager AND register the client
	daemon.repoManager.SetActiveRepositoryDirect("test-repo-id")
	// Register the mock client for this repo ID
	daemon.repoManager.mu.Lock()
	daemon.repoManager.clients["test-repo-id"] = mockClient
	daemon.repoManager.mu.Unlock()

	handler := NewBeadsHandler(daemon)

	req := httptest.NewRequest(http.MethodPost, "/api/beads/sync", nil)
	w := httptest.NewRecorder()

	handler.HandleSyncBeads(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response SyncBeadsResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Synced != 2 {
		t.Errorf("expected synced=2, got %d", response.Synced)
	}

	if response.RepoID != "test-repo-id" {
		t.Errorf("expected repo_id=test-repo-id, got %s", response.RepoID)
	}

	// Verify tasks were added to runtime state
	state := daemon.GetRuntimeState()
	if len(state.Tasks) != 2 {
		t.Errorf("expected 2 tasks in state, got %d", len(state.Tasks))
	}
}

func TestHandleSyncBeads_InvalidRepo(t *testing.T) {
	mockClient := &mockBeadsClientWithList{}
	daemon := newDaemonForTest(Config{}, nil, mockClient)
	handler := NewBeadsHandler(daemon)

	// Try to sync with a non-existent repo
	req := httptest.NewRequest(http.MethodPost, "/api/beads/sync?repo=non-existent-repo", nil)
	w := httptest.NewRecorder()

	handler.HandleSyncBeads(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleSyncBeads_SyncFailure(t *testing.T) {
	mockClient := &mockBeadsClientWithList{
		listErr: context.DeadlineExceeded,
	}

	daemon := newDaemonForTest(Config{}, nil, mockClient)
	daemon.repoManager.SetActiveRepositoryDirect("test-repo-id")
	// Register the mock client for this repo ID
	daemon.repoManager.mu.Lock()
	daemon.repoManager.clients["test-repo-id"] = mockClient
	daemon.repoManager.mu.Unlock()

	handler := NewBeadsHandler(daemon)

	req := httptest.NewRequest(http.MethodPost, "/api/beads/sync", nil)
	w := httptest.NewRecorder()

	handler.HandleSyncBeads(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHandleSyncBeads_EmptyTaskList(t *testing.T) {
	mockClient := &mockBeadsClientWithList{
		listTasks: []beads.Task{}, // Empty task list
	}

	daemon := newDaemonForTest(Config{}, nil, mockClient)
	daemon.repoManager.SetActiveRepositoryDirect("test-repo-id")
	// Register the mock client for this repo ID
	daemon.repoManager.mu.Lock()
	daemon.repoManager.clients["test-repo-id"] = mockClient
	daemon.repoManager.mu.Unlock()

	handler := NewBeadsHandler(daemon)

	req := httptest.NewRequest(http.MethodPost, "/api/beads/sync", nil)
	w := httptest.NewRecorder()

	handler.HandleSyncBeads(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response SyncBeadsResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Synced != 0 {
		t.Errorf("expected synced=0, got %d", response.Synced)
	}
}

func TestHandleSyncBeads_BroadcastsStateEvent(t *testing.T) {
	mockClient := &mockBeadsClientWithList{
		listTasks: []beads.Task{
			{ID: "task-1", Title: "Task 1", Status: "open", Priority: 2},
		},
	}

	daemon := newDaemonForTest(Config{}, nil, mockClient)
	daemon.repoManager.SetActiveRepositoryDirect("test-repo-id")
	// Register the mock client for this repo ID
	daemon.repoManager.mu.Lock()
	daemon.repoManager.clients["test-repo-id"] = mockClient
	daemon.repoManager.mu.Unlock()

	// Subscribe to events to verify broadcast
	eventReceived := false
	daemon.eventBus.Subscribe(func(e Event) {
		if e.Type == EventStateSync {
			eventReceived = true
		}
	})

	handler := NewBeadsHandler(daemon)

	req := httptest.NewRequest(http.MethodPost, "/api/beads/sync", nil)
	w := httptest.NewRecorder()

	handler.HandleSyncBeads(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if !eventReceived {
		t.Error("expected EventStateSync to be broadcast")
	}
}
