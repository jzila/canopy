package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/persistence"
	"github.com/jzila/canopy/pkg/repository"
)

// mockDaemon implements a minimal daemon for testing
type mockDaemon struct {
	activeRepoID string
}

func (m *mockDaemon) GetActiveRepositoryID() string {
	return m.activeRepoID
}

func (m *mockDaemon) SetActiveRepository(repoID string) error {
	m.activeRepoID = repoID
	return nil
}

func (m *mockDaemon) ListRepositories() ([]repository.Repository, error) {
	return repository.List()
}

// mockRepoStore implements RepositoryStoreInterface for testing
type mockRepoStore struct {
	stats *persistence.AggregateStats
	runs  []persistence.Run
}

func (m *mockRepoStore) GetStatsByRepo(repoID string, since *time.Time) (*persistence.AggregateStats, error) {
	return m.stats, nil
}

func (m *mockRepoStore) GetRunsByRepo(repoID string) ([]persistence.Run, error) {
	return m.runs, nil
}

// setupTestRegistry creates a temporary registry file for testing
func setupTestRegistry(t *testing.T) (cleanup func()) {
	t.Helper()

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "canopy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Set XDG_CACHE_HOME to use our temp directory
	oldCache := os.Getenv("XDG_CACHE_HOME")
	os.Setenv("XDG_CACHE_HOME", tmpDir)

	// Create the canopy subdirectory
	canopyDir := filepath.Join(tmpDir, "canopy")
	if err := os.MkdirAll(canopyDir, 0755); err != nil {
		t.Fatalf("Failed to create canopy dir: %v", err)
	}

	return func() {
		os.Setenv("XDG_CACHE_HOME", oldCache)
		os.RemoveAll(tmpDir)
	}
}

func TestHandleListRepositories(t *testing.T) {
	cleanup := setupTestRegistry(t)
	defer cleanup()

	// Create a test repository
	tmpRepo, err := os.MkdirTemp("", "test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp repo: %v", err)
	}
	defer os.RemoveAll(tmpRepo)

	repo, err := repository.GetOrCreate(tmpRepo)
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}

	// Create mock daemon and handler
	daemon := newDaemonForTest(Config{}, nil, nil)
	daemon.repoManager.SetActiveRepositoryDirect(repo.ID)
	handler := NewRepoHandler(daemon, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/repositories", nil)
	w := httptest.NewRecorder()

	handler.HandleListRepositories(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response RepositoryListResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(response.Repositories) != 1 {
		t.Errorf("Expected 1 repository, got %d", len(response.Repositories))
	}

	if response.ActiveRepoID != repo.ID {
		t.Errorf("Expected active repo ID %s, got %s", repo.ID, response.ActiveRepoID)
	}

	if !response.Repositories[0].IsActive {
		t.Error("Expected repository to be marked as active")
	}
}

func TestHandleGetRepository(t *testing.T) {
	cleanup := setupTestRegistry(t)
	defer cleanup()

	// Create a test repository
	tmpRepo, err := os.MkdirTemp("", "test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp repo: %v", err)
	}
	defer os.RemoveAll(tmpRepo)

	repo, err := repository.GetOrCreate(tmpRepo)
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}

	// Create mock store with stats
	mockStore := &mockRepoStore{
		stats: &persistence.AggregateStats{
			TotalRuns:    5,
			TotalAgents:  10,
			TotalCostUSD: 1.23,
		},
	}

	daemon := &Daemon{
		eventBus: NewEventBus(),
	}
	handler := NewRepoHandler(daemon, mockStore)

	req := httptest.NewRequest(http.MethodGet, "/api/repositories/"+repo.ID, nil)
	w := httptest.NewRecorder()

	handler.HandleGetRepository(w, req, repo.ID)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response RepositoryDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.ID != repo.ID {
		t.Errorf("Expected repo ID %s, got %s", repo.ID, response.ID)
	}

	if response.Stats == nil {
		t.Fatal("Expected stats to be present")
	}

	if response.Stats.TotalRuns != 5 {
		t.Errorf("Expected 5 total runs, got %d", response.Stats.TotalRuns)
	}

	if response.Stats.TotalCostUSD != 1.23 {
		t.Errorf("Expected cost 1.23, got %f", response.Stats.TotalCostUSD)
	}
}

func TestHandleGetRepositoryNotFound(t *testing.T) {
	cleanup := setupTestRegistry(t)
	defer cleanup()

	daemon := &Daemon{
		eventBus: NewEventBus(),
	}
	handler := NewRepoHandler(daemon, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/repositories/non-existent-id", nil)
	w := httptest.NewRecorder()

	handler.HandleGetRepository(w, req, "non-existent-id")

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestHandleActivateRepository(t *testing.T) {
	cleanup := setupTestRegistry(t)
	defer cleanup()

	// Create a test repository
	tmpRepo, err := os.MkdirTemp("", "test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp repo: %v", err)
	}
	defer os.RemoveAll(tmpRepo)

	repo, err := repository.GetOrCreate(tmpRepo)
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}

	daemon := newDaemonForTest(Config{}, nil, nil)
	handler := NewRepoHandler(daemon, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/repositories/"+repo.ID+"/activate", nil)
	w := httptest.NewRecorder()

	handler.HandleActivateRepository(w, req, repo.ID)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response RepositoryResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.ID != repo.ID {
		t.Errorf("Expected repo ID %s, got %s", repo.ID, response.ID)
	}

	if !response.IsActive {
		t.Error("Expected repository to be marked as active")
	}

	// Verify daemon state was updated
	if daemon.GetActiveRepositoryID() != repo.ID {
		t.Errorf("Expected daemon active repo ID %s, got %s", repo.ID, daemon.GetActiveRepositoryID())
	}
}

func TestHandleActivateRepositoryNotFound(t *testing.T) {
	cleanup := setupTestRegistry(t)
	defer cleanup()

	daemon := &Daemon{
		eventBus: NewEventBus(),
	}
	handler := NewRepoHandler(daemon, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/repositories/non-existent-id/activate", nil)
	w := httptest.NewRecorder()

	handler.HandleActivateRepository(w, req, "non-existent-id")

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestRepoHandlersMethodNotAllowed(t *testing.T) {
	cleanup := setupTestRegistry(t)
	defer cleanup()

	daemon := &Daemon{
		eventBus: NewEventBus(),
	}
	handler := NewRepoHandler(daemon, nil)

	tests := []struct {
		name     string
		method   string
		handler  func(w http.ResponseWriter, r *http.Request)
	}{
		{"ListRepositories POST", http.MethodPost, handler.HandleListRepositories},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/repositories", nil)
			w := httptest.NewRecorder()

			tt.handler(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status 405, got %d", w.Code)
			}
		})
	}
}

func TestHandleGetRepositoryMethodNotAllowed(t *testing.T) {
	cleanup := setupTestRegistry(t)
	defer cleanup()

	daemon := &Daemon{
		eventBus: NewEventBus(),
	}
	handler := NewRepoHandler(daemon, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/repositories/some-id", nil)
	w := httptest.NewRecorder()

	handler.HandleGetRepository(w, req, "some-id")

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestHandleActivateRepositoryMethodNotAllowed(t *testing.T) {
	cleanup := setupTestRegistry(t)
	defer cleanup()

	daemon := &Daemon{
		eventBus: NewEventBus(),
	}
	handler := NewRepoHandler(daemon, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/repositories/some-id/activate", nil)
	w := httptest.NewRecorder()

	handler.HandleActivateRepository(w, req, "some-id")

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestHandleGetStateWithActiveRepoID(t *testing.T) {
	state := NewRuntimeState()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient, nil)

	// Set daemon reference with active repo ID
	mockD := &mockDaemon{activeRepoID: "test-repo-123"}
	handler.SetDaemon(mockD)

	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	w := httptest.NewRecorder()

	handler.HandleGetState(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response StateResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.ActiveRepoID != "test-repo-123" {
		t.Errorf("Expected active_repo_id 'test-repo-123', got '%s'", response.ActiveRepoID)
	}
}
