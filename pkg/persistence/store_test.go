package persistence

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "canopy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("database file was not created")
	}
}

func TestCreateAndGetRun(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()
	run := &Run{
		ID:          "run-test-1",
		StartedAt:   now,
		Status:      RunStatusRunning,
		Concurrency: 4,
		GitBranch:   "main",
		GitCommit:   "abc123",
		TotalTasks:  5,
	}

	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	retrieved, err := store.GetRun("run-test-1")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}

	if retrieved == nil {
		t.Fatal("run not found")
	}

	if retrieved.ID != run.ID {
		t.Errorf("expected ID %s, got %s", run.ID, retrieved.ID)
	}
	if retrieved.Status != RunStatusRunning {
		t.Errorf("expected status running, got %s", retrieved.Status)
	}
	if retrieved.Concurrency != 4 {
		t.Errorf("expected concurrency 4, got %d", retrieved.Concurrency)
	}
}

func TestUpdateRun(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()
	run := &Run{
		ID:        "run-update-test",
		StartedAt: now,
		Status:    RunStatusRunning,
	}

	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Update the run
	finishedAt := now.Add(time.Hour)
	run.Status = RunStatusCompleted
	run.FinishedAt = &finishedAt
	run.CompletedTasks = 5

	if err := store.UpdateRun(run); err != nil {
		t.Fatalf("failed to update run: %v", err)
	}

	retrieved, err := store.GetRun("run-update-test")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}

	if retrieved.Status != RunStatusCompleted {
		t.Errorf("expected status completed, got %s", retrieved.Status)
	}
	if retrieved.CompletedTasks != 5 {
		t.Errorf("expected 5 completed tasks, got %d", retrieved.CompletedTasks)
	}
}

func TestListRuns(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create multiple runs
	runs := []*Run{
		{ID: "run-1", StartedAt: now.Add(-3 * time.Hour), Status: RunStatusCompleted},
		{ID: "run-2", StartedAt: now.Add(-2 * time.Hour), Status: RunStatusCompleted},
		{ID: "run-3", StartedAt: now.Add(-1 * time.Hour), Status: RunStatusFailed},
		{ID: "run-4", StartedAt: now, Status: RunStatusRunning},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// List all runs
	result, err := store.ListRuns(RunFilter{})
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}

	if len(result.Runs) != 4 {
		t.Errorf("expected 4 runs, got %d", len(result.Runs))
	}

	// Filter by status
	result, err = store.ListRuns(RunFilter{Status: RunStatusCompleted})
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}

	if len(result.Runs) != 2 {
		t.Errorf("expected 2 completed runs, got %d", len(result.Runs))
	}

	// Filter by time
	since := now.Add(-90 * time.Minute)
	result, err = store.ListRuns(RunFilter{Since: &since})
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}

	if len(result.Runs) != 2 {
		t.Errorf("expected 2 recent runs, got %d", len(result.Runs))
	}
}

func TestListRuns_Pagination(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create 10 runs
	for i := 0; i < 10; i++ {
		run := &Run{
			ID:        "run-" + string(rune('a'+i)),
			StartedAt: now.Add(-time.Duration(10-i) * time.Hour),
			Status:    RunStatusCompleted,
		}
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Get first page
	result, err := store.ListRuns(RunFilter{Limit: 3, Offset: 0})
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}

	if len(result.Runs) != 3 {
		t.Errorf("expected 3 runs on first page, got %d", len(result.Runs))
	}
	if result.Pagination.Total != 10 {
		t.Errorf("expected total 10, got %d", result.Pagination.Total)
	}
}

func TestCreateAndGetAgent(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// First create a run
	run := &Run{ID: "run-for-agent", StartedAt: now, Status: RunStatusRunning}
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	agent := &Agent{
		ID:           "agent-test-1",
		RunID:        "run-for-agent",
		TaskID:       "task-123",
		TaskTitle:    "Test Task",
		Status:       AgentStatusRunning,
		StartedAt:    now,
		InputTokens:  1000,
		OutputTokens: 500,
		TotalTokens:  1500,
		CostUSD:      0.05,
	}

	if err := store.CreateAgent(agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	retrieved, err := store.GetAgent("agent-test-1")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}

	if retrieved == nil {
		t.Fatal("agent not found")
	}

	if retrieved.TaskTitle != "Test Task" {
		t.Errorf("expected task title 'Test Task', got %s", retrieved.TaskTitle)
	}
	if retrieved.InputTokens != 1000 {
		t.Errorf("expected 1000 input tokens, got %d", retrieved.InputTokens)
	}
}

func TestGetAgentsByRun(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create run
	run := &Run{ID: "run-with-agents", StartedAt: now, Status: RunStatusRunning}
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Create agents
	agents := []*Agent{
		{ID: "agent-1", RunID: "run-with-agents", TaskID: "task-1", TaskTitle: "Task 1", Status: AgentStatusCompleted, StartedAt: now},
		{ID: "agent-2", RunID: "run-with-agents", TaskID: "task-2", TaskTitle: "Task 2", Status: AgentStatusCompleted, StartedAt: now.Add(time.Minute)},
		{ID: "agent-3", RunID: "run-with-agents", TaskID: "task-3", TaskTitle: "Task 3", Status: AgentStatusRunning, StartedAt: now.Add(2 * time.Minute)},
	}

	for _, agent := range agents {
		if err := store.CreateAgent(agent); err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
	}

	retrieved, err := store.GetAgentsByRun("run-with-agents")
	if err != nil {
		t.Fatalf("failed to get agents: %v", err)
	}

	if len(retrieved) != 3 {
		t.Errorf("expected 3 agents, got %d", len(retrieved))
	}
}

func TestGetStats(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create runs
	runs := []*Run{
		{ID: "run-1", StartedAt: now.Add(-time.Hour), Status: RunStatusCompleted},
		{ID: "run-2", StartedAt: now.Add(-30 * time.Minute), Status: RunStatusCompleted},
		{ID: "run-3", StartedAt: now, Status: RunStatusFailed},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Create agents with token usage
	agents := []*Agent{
		{ID: "agent-1", RunID: "run-1", TaskID: "task-1", TaskTitle: "Task 1", Status: AgentStatusCompleted, StartedAt: now, InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500, CostUSD: 0.05, DurationSeconds: 60},
		{ID: "agent-2", RunID: "run-2", TaskID: "task-2", TaskTitle: "Task 2", Status: AgentStatusCompleted, StartedAt: now, InputTokens: 2000, OutputTokens: 800, TotalTokens: 2800, CostUSD: 0.08, DurationSeconds: 120},
	}

	for _, agent := range agents {
		if err := store.CreateAgent(agent); err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
	}

	stats, err := store.GetStats(nil)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if stats.TotalRuns != 3 {
		t.Errorf("expected 3 total runs, got %d", stats.TotalRuns)
	}
	if stats.CompletedRuns != 2 {
		t.Errorf("expected 2 completed runs, got %d", stats.CompletedRuns)
	}
	if stats.FailedRuns != 1 {
		t.Errorf("expected 1 failed run, got %d", stats.FailedRuns)
	}
	if stats.TotalAgents != 2 {
		t.Errorf("expected 2 agents, got %d", stats.TotalAgents)
	}
	if stats.TotalInputTokens != 3000 {
		t.Errorf("expected 3000 input tokens, got %d", stats.TotalInputTokens)
	}
	if stats.TotalOutputTokens != 1300 {
		t.Errorf("expected 1300 output tokens, got %d", stats.TotalOutputTokens)
	}
}

func TestGetStats_WithSince(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create runs
	runs := []*Run{
		{ID: "run-old", StartedAt: now.Add(-7 * 24 * time.Hour), Status: RunStatusCompleted},
		{ID: "run-recent", StartedAt: now.Add(-time.Hour), Status: RunStatusCompleted},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Get stats for last 24 hours only
	since := now.Add(-24 * time.Hour)
	stats, err := store.GetStats(&since)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if stats.TotalRuns != 1 {
		t.Errorf("expected 1 recent run, got %d", stats.TotalRuns)
	}
}

func TestGetRun_NotFound(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	run, err := store.GetRun("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run != nil {
		t.Errorf("expected nil, got run: %v", run)
	}
}

// Helper function to create a test store
func createTestStore(t *testing.T) *Store {
	tmpDir, err := os.MkdirTemp("", "canopy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	return store
}
