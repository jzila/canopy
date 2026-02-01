package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/persistence"
)

// mockPersistenceStore is a mock implementation of PersistenceStoreInterface
type mockPersistenceStore struct {
	runs   map[string]*persistence.Run
	agents map[string][]persistence.Agent
}

func newMockPersistenceStore() *mockPersistenceStore {
	return &mockPersistenceStore{
		runs:   make(map[string]*persistence.Run),
		agents: make(map[string][]persistence.Agent),
	}
}

func (m *mockPersistenceStore) GetRun(id string) (*persistence.Run, error) {
	run, exists := m.runs[id]
	if !exists {
		return nil, nil
	}
	return run, nil
}

func (m *mockPersistenceStore) ListRuns(filter persistence.RunFilter) (*persistence.RunListResult, error) {
	runs := []persistence.Run{}
	for _, run := range m.runs {
		// Apply status filter
		if filter.Status != "" && run.Status != filter.Status {
			continue
		}
		// Apply since filter
		if filter.Since != nil && run.StartedAt.Before(*filter.Since) {
			continue
		}
		// Apply before filter
		if filter.Before != nil && run.StartedAt.After(*filter.Before) {
			continue
		}
		runs = append(runs, *run)
	}

	// Apply limit
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(runs) > limit {
		runs = runs[:limit]
	}

	result := &persistence.RunListResult{
		Runs: runs,
	}
	result.Pagination.Total = len(m.runs)
	result.Pagination.Limit = limit
	result.Pagination.Offset = filter.Offset

	return result, nil
}

func (m *mockPersistenceStore) GetAgentsByRun(runID string) ([]persistence.Agent, error) {
	agents, exists := m.agents[runID]
	if !exists {
		return []persistence.Agent{}, nil
	}
	return agents, nil
}

func (m *mockPersistenceStore) GetStats(since *time.Time) (*persistence.AggregateStats, error) {
	stats := &persistence.AggregateStats{
		TotalRuns:     len(m.runs),
		CompletedRuns: 0,
		FailedRuns:    0,
	}
	for _, run := range m.runs {
		switch run.Status {
		case persistence.RunStatusCompleted:
			stats.CompletedRuns++
		case persistence.RunStatusFailed:
			stats.FailedRuns++
		}
	}
	return stats, nil
}

func (m *mockPersistenceStore) GetStatsByRepo(repoID string, since *time.Time) (*persistence.AggregateStats, error) {
	stats := &persistence.AggregateStats{
		TotalRuns:     0,
		CompletedRuns: 0,
		FailedRuns:    0,
	}
	for _, run := range m.runs {
		if run.RepoID != repoID {
			continue
		}
		stats.TotalRuns++
		switch run.Status {
		case persistence.RunStatusCompleted:
			stats.CompletedRuns++
		case persistence.RunStatusFailed:
			stats.FailedRuns++
		}
	}
	return stats, nil
}

func (m *mockPersistenceStore) SetAgentArchived(agentID string, archived bool) error {
	// Mock implementation - in real use, this would update the agent in storage
	return nil
}

func (m *mockPersistenceStore) MarkAgentFailed(agentID string, errorMessage string) error {
	return nil
}

func TestHandleListRuns(t *testing.T) {
	store := newMockPersistenceStore()
	now := time.Now()
	store.runs["run-1"] = &persistence.Run{
		ID:        "run-1",
		StartedAt: now.Add(-time.Hour),
		Status:    persistence.RunStatusCompleted,
	}
	store.runs["run-2"] = &persistence.Run{
		ID:        "run-2",
		StartedAt: now.Add(-30 * time.Minute),
		Status:    persistence.RunStatusRunning,
	}

	handler := NewRunsHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	rec := httptest.NewRecorder()

	handler.HandleListRuns(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var result persistence.RunListResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(result.Runs))
	}
}

func TestHandleListRuns_WithStatusFilter(t *testing.T) {
	store := newMockPersistenceStore()
	now := time.Now()
	store.runs["run-1"] = &persistence.Run{
		ID:        "run-1",
		StartedAt: now.Add(-time.Hour),
		Status:    persistence.RunStatusCompleted,
	}
	store.runs["run-2"] = &persistence.Run{
		ID:        "run-2",
		StartedAt: now.Add(-30 * time.Minute),
		Status:    persistence.RunStatusRunning,
	}

	handler := NewRunsHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/runs?status=completed", nil)
	rec := httptest.NewRecorder()

	handler.HandleListRuns(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var result persistence.RunListResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Runs) != 1 {
		t.Errorf("expected 1 run, got %d", len(result.Runs))
	}
}

func TestHandleGetRun(t *testing.T) {
	store := newMockPersistenceStore()
	now := time.Now()
	store.runs["run-1"] = &persistence.Run{
		ID:        "run-1",
		StartedAt: now.Add(-time.Hour),
		Status:    persistence.RunStatusCompleted,
	}
	store.agents["run-1"] = []persistence.Agent{
		{ID: "agent-1", RunID: "run-1", TaskTitle: "Test Task"},
	}

	handler := NewRunsHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run-1", nil)
	rec := httptest.NewRecorder()

	handler.HandleGetRun(rec, req, "run-1")

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var response struct {
		ID     string              `json:"id"`
		Agents []persistence.Agent `json:"agents"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.ID != "run-1" {
		t.Errorf("expected run-1, got %s", response.ID)
	}
	if len(response.Agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(response.Agents))
	}
}

func TestHandleGetRun_NotFound(t *testing.T) {
	store := newMockPersistenceStore()
	handler := NewRunsHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/nonexistent", nil)
	rec := httptest.NewRecorder()

	handler.HandleGetRun(rec, req, "nonexistent")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestHandleGetRunAgents(t *testing.T) {
	store := newMockPersistenceStore()
	now := time.Now()
	store.runs["run-1"] = &persistence.Run{
		ID:        "run-1",
		StartedAt: now.Add(-time.Hour),
		Status:    persistence.RunStatusCompleted,
	}
	store.agents["run-1"] = []persistence.Agent{
		{ID: "agent-1", RunID: "run-1", TaskTitle: "Task 1"},
		{ID: "agent-2", RunID: "run-1", TaskTitle: "Task 2"},
	}

	handler := NewRunsHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run-1/agents", nil)
	rec := httptest.NewRecorder()

	handler.HandleGetRunAgents(rec, req, "run-1")

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var agents []persistence.Agent
	if err := json.NewDecoder(rec.Body).Decode(&agents); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(agents))
	}
}

func TestHandleGetHistoricalStats(t *testing.T) {
	store := newMockPersistenceStore()
	now := time.Now()
	store.runs["run-1"] = &persistence.Run{
		ID:        "run-1",
		StartedAt: now.Add(-time.Hour),
		Status:    persistence.RunStatusCompleted,
	}
	store.runs["run-2"] = &persistence.Run{
		ID:        "run-2",
		StartedAt: now.Add(-30 * time.Minute),
		Status:    persistence.RunStatusFailed,
	}

	handler := NewRunsHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/stats/history", nil)
	rec := httptest.NewRecorder()

	handler.HandleGetHistoricalStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var stats persistence.AggregateStats
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if stats.TotalRuns != 2 {
		t.Errorf("expected 2 total runs, got %d", stats.TotalRuns)
	}
	if stats.CompletedRuns != 1 {
		t.Errorf("expected 1 completed run, got %d", stats.CompletedRuns)
	}
	if stats.FailedRuns != 1 {
		t.Errorf("expected 1 failed run, got %d", stats.FailedRuns)
	}
}

func TestParseTimeOrDuration(t *testing.T) {
	tests := []struct {
		input       string
		expectError bool
	}{
		{"7d", false},
		{"24h", false},
		{"30m", false},
		{"60s", false},
		{"2026-01-10T12:00:00Z", false},
		{"invalid", true},
		{"abc123", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := parseTimeOrDuration(tt.input)
			if tt.expectError && err == nil {
				t.Errorf("expected error for input %q", tt.input)
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error for input %q: %v", tt.input, err)
			}
		})
	}
}

func TestHandleListRuns_NilStore(t *testing.T) {
	handler := NewRunsHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	rec := httptest.NewRecorder()

	handler.HandleListRuns(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected status 501, got %d", rec.Code)
	}
}

func TestHandleListRuns_InvalidMethod(t *testing.T) {
	store := newMockPersistenceStore()
	handler := NewRunsHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/api/runs", nil)
	rec := httptest.NewRecorder()

	handler.HandleListRuns(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}
