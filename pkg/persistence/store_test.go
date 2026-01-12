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

func TestGetRunningRun(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create multiple runs with different statuses
	runs := []*Run{
		{ID: "run-completed-1", StartedAt: now.Add(-3 * time.Hour), Status: RunStatusCompleted},
		{ID: "run-failed", StartedAt: now.Add(-2 * time.Hour), Status: RunStatusFailed},
		{ID: "run-running", StartedAt: now.Add(-1 * time.Hour), Status: RunStatusRunning},
		{ID: "run-completed-2", StartedAt: now.Add(-30 * time.Minute), Status: RunStatusCompleted},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Should return the running run
	runningRun, err := store.GetRunningRun()
	if err != nil {
		t.Fatalf("failed to get running run: %v", err)
	}

	if runningRun == nil {
		t.Fatal("expected running run, got nil")
	}

	if runningRun.ID != "run-running" {
		t.Errorf("expected run-running, got %s", runningRun.ID)
	}
	if runningRun.Status != RunStatusRunning {
		t.Errorf("expected status running, got %s", runningRun.Status)
	}
}

func TestGetRunningRun_NoRunning(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create only completed runs
	runs := []*Run{
		{ID: "run-completed-1", StartedAt: now.Add(-2 * time.Hour), Status: RunStatusCompleted},
		{ID: "run-completed-2", StartedAt: now.Add(-1 * time.Hour), Status: RunStatusCompleted},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Should return nil when no running run exists
	runningRun, err := store.GetRunningRun()
	if err != nil {
		t.Fatalf("failed to get running run: %v", err)
	}

	if runningRun != nil {
		t.Errorf("expected nil, got run: %v", runningRun)
	}
}

func TestGetRunningRun_MultipleRunning(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create multiple running runs (edge case - should return most recent)
	runs := []*Run{
		{ID: "run-running-old", StartedAt: now.Add(-2 * time.Hour), Status: RunStatusRunning},
		{ID: "run-running-new", StartedAt: now.Add(-1 * time.Hour), Status: RunStatusRunning},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Should return the most recent running run
	runningRun, err := store.GetRunningRun()
	if err != nil {
		t.Fatalf("failed to get running run: %v", err)
	}

	if runningRun == nil {
		t.Fatal("expected running run, got nil")
	}

	if runningRun.ID != "run-running-new" {
		t.Errorf("expected run-running-new (most recent), got %s", runningRun.ID)
	}
}

func TestMarkOrphanedRunsFailed(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create runs with different statuses
	runs := []*Run{
		{ID: "run-completed", StartedAt: now.Add(-3 * time.Hour), Status: RunStatusCompleted},
		{ID: "run-failed", StartedAt: now.Add(-2 * time.Hour), Status: RunStatusFailed},
		{ID: "run-running-1", StartedAt: now.Add(-1 * time.Hour), Status: RunStatusRunning},
		{ID: "run-running-2", StartedAt: now.Add(-30 * time.Minute), Status: RunStatusRunning},
		{ID: "run-cancelled", StartedAt: now.Add(-15 * time.Minute), Status: RunStatusCancelled},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Mark orphaned runs as failed
	count, err := store.MarkOrphanedRunsFailed()
	if err != nil {
		t.Fatalf("failed to mark orphaned runs: %v", err)
	}

	if count != 2 {
		t.Errorf("expected 2 runs marked as failed, got %d", count)
	}

	// Verify the running runs are now failed
	run1, err := store.GetRun("run-running-1")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if run1.Status != RunStatusFailed {
		t.Errorf("expected run-running-1 status to be failed, got %s", run1.Status)
	}
	if run1.FinishedAt == nil {
		t.Error("expected run-running-1 to have finished_at set")
	}

	run2, err := store.GetRun("run-running-2")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if run2.Status != RunStatusFailed {
		t.Errorf("expected run-running-2 status to be failed, got %s", run2.Status)
	}

	// Verify other runs are unchanged
	completedRun, err := store.GetRun("run-completed")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if completedRun.Status != RunStatusCompleted {
		t.Errorf("expected run-completed to remain completed, got %s", completedRun.Status)
	}

	cancelledRun, err := store.GetRun("run-cancelled")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if cancelledRun.Status != RunStatusCancelled {
		t.Errorf("expected run-cancelled to remain cancelled, got %s", cancelledRun.Status)
	}

	// GetRunningRun should now return nil since all running runs are failed
	runningRun, err := store.GetRunningRun()
	if err != nil {
		t.Fatalf("failed to get running run: %v", err)
	}
	if runningRun != nil {
		t.Errorf("expected no running runs, got %v", runningRun)
	}
}

func TestMarkOrphanedRunsFailed_NoOrphans(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create only completed/failed runs
	runs := []*Run{
		{ID: "run-completed", StartedAt: now.Add(-2 * time.Hour), Status: RunStatusCompleted},
		{ID: "run-failed", StartedAt: now.Add(-1 * time.Hour), Status: RunStatusFailed},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Mark orphaned runs - should affect nothing
	count, err := store.MarkOrphanedRunsFailed()
	if err != nil {
		t.Fatalf("failed to mark orphaned runs: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 runs marked as failed, got %d", count)
	}
}

func TestMarkOrphanedAgentsFailed(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create a run
	run := &Run{ID: "run-for-orphan-test", StartedAt: now, Status: RunStatusRunning}
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Create agents with different statuses
	agents := []*Agent{
		{ID: "agent-completed", RunID: "run-for-orphan-test", TaskID: "task-1", TaskTitle: "Completed Task", Status: AgentStatusCompleted, StartedAt: now},
		{ID: "agent-failed", RunID: "run-for-orphan-test", TaskID: "task-2", TaskTitle: "Failed Task", Status: AgentStatusFailed, StartedAt: now},
		{ID: "agent-starting", RunID: "run-for-orphan-test", TaskID: "task-3", TaskTitle: "Starting Task", Status: AgentStatusStarting, StartedAt: now},
		{ID: "agent-running", RunID: "run-for-orphan-test", TaskID: "task-4", TaskTitle: "Running Task", Status: AgentStatusRunning, StartedAt: now},
		{ID: "agent-timed-out", RunID: "run-for-orphan-test", TaskID: "task-5", TaskTitle: "Timed Out Task", Status: AgentStatusTimedOut, StartedAt: now},
		{ID: "agent-cancelled", RunID: "run-for-orphan-test", TaskID: "task-6", TaskTitle: "Cancelled Task", Status: AgentStatusCancelled, StartedAt: now},
	}

	for _, agent := range agents {
		if err := store.CreateAgent(agent); err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
	}

	// Mark orphaned agents as failed
	count, err := store.MarkOrphanedAgentsFailed()
	if err != nil {
		t.Fatalf("failed to mark orphaned agents: %v", err)
	}

	if count != 2 {
		t.Errorf("expected 2 agents marked as failed (starting + running), got %d", count)
	}

	// Verify starting agent is now failed
	startingAgent, err := store.GetAgent("agent-starting")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if startingAgent.Status != AgentStatusFailed {
		t.Errorf("expected agent-starting status to be failed, got %s", startingAgent.Status)
	}
	if startingAgent.FinishedAt == nil {
		t.Error("expected agent-starting to have finished_at set")
	}
	if startingAgent.ErrorMessage != "daemon terminated unexpectedly" {
		t.Errorf("expected error message 'daemon terminated unexpectedly', got %s", startingAgent.ErrorMessage)
	}

	// Verify running agent is now failed
	runningAgent, err := store.GetAgent("agent-running")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if runningAgent.Status != AgentStatusFailed {
		t.Errorf("expected agent-running status to be failed, got %s", runningAgent.Status)
	}
	if runningAgent.ErrorMessage != "daemon terminated unexpectedly" {
		t.Errorf("expected error message 'daemon terminated unexpectedly', got %s", runningAgent.ErrorMessage)
	}

	// Verify other agents are unchanged
	completedAgent, err := store.GetAgent("agent-completed")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if completedAgent.Status != AgentStatusCompleted {
		t.Errorf("expected agent-completed to remain completed, got %s", completedAgent.Status)
	}

	timedOutAgent, err := store.GetAgent("agent-timed-out")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if timedOutAgent.Status != AgentStatusTimedOut {
		t.Errorf("expected agent-timed-out to remain timed_out, got %s", timedOutAgent.Status)
	}
}

func TestMarkOrphanedAgentsFailed_NoOrphans(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create a run
	run := &Run{ID: "run-no-orphans", StartedAt: now, Status: RunStatusCompleted}
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Create only completed/failed agents
	agents := []*Agent{
		{ID: "agent-completed", RunID: "run-no-orphans", TaskID: "task-1", TaskTitle: "Completed Task", Status: AgentStatusCompleted, StartedAt: now},
		{ID: "agent-failed", RunID: "run-no-orphans", TaskID: "task-2", TaskTitle: "Failed Task", Status: AgentStatusFailed, StartedAt: now},
	}

	for _, agent := range agents {
		if err := store.CreateAgent(agent); err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
	}

	// Mark orphaned agents - should affect nothing
	count, err := store.MarkOrphanedAgentsFailed()
	if err != nil {
		t.Fatalf("failed to mark orphaned agents: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 agents marked as failed, got %d", count)
	}
}

func TestGetMostRecentRun(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create multiple runs with different statuses
	runs := []*Run{
		{ID: "run-old-completed", StartedAt: now.Add(-3 * time.Hour), Status: RunStatusCompleted},
		{ID: "run-old-failed", StartedAt: now.Add(-2 * time.Hour), Status: RunStatusFailed},
		{ID: "run-recent-completed", StartedAt: now.Add(-1 * time.Hour), Status: RunStatusCompleted},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Should return the most recent run regardless of status
	mostRecent, err := store.GetMostRecentRun()
	if err != nil {
		t.Fatalf("failed to get most recent run: %v", err)
	}

	if mostRecent == nil {
		t.Fatal("expected most recent run, got nil")
	}

	if mostRecent.ID != "run-recent-completed" {
		t.Errorf("expected run-recent-completed, got %s", mostRecent.ID)
	}
	if mostRecent.Status != RunStatusCompleted {
		t.Errorf("expected status completed, got %s", mostRecent.Status)
	}
}

func TestGetMostRecentRun_Empty(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	// No runs in database
	mostRecent, err := store.GetMostRecentRun()
	if err != nil {
		t.Fatalf("failed to get most recent run: %v", err)
	}

	if mostRecent != nil {
		t.Errorf("expected nil for empty database, got %v", mostRecent)
	}
}

func TestGetMostRecentRun_AfterMarkOrphaned(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create a running run (simulates daemon crash)
	run := &Run{ID: "run-orphaned", StartedAt: now.Add(-1 * time.Hour), Status: RunStatusRunning}
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Mark orphaned runs as failed
	_, err := store.MarkOrphanedRunsFailed()
	if err != nil {
		t.Fatalf("failed to mark orphaned runs: %v", err)
	}

	// GetRunningRun should return nil (no running runs)
	runningRun, err := store.GetRunningRun()
	if err != nil {
		t.Fatalf("failed to get running run: %v", err)
	}
	if runningRun != nil {
		t.Error("expected GetRunningRun to return nil after orphan cleanup")
	}

	// GetMostRecentRun should still return the run (now failed)
	mostRecent, err := store.GetMostRecentRun()
	if err != nil {
		t.Fatalf("failed to get most recent run: %v", err)
	}

	if mostRecent == nil {
		t.Fatal("expected GetMostRecentRun to return the orphaned (now failed) run")
	}
	if mostRecent.ID != "run-orphaned" {
		t.Errorf("expected run-orphaned, got %s", mostRecent.ID)
	}
	if mostRecent.Status != RunStatusFailed {
		t.Errorf("expected status failed (after orphan cleanup), got %s", mostRecent.Status)
	}
}

func TestCreateRunWithRepoID(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()
	run := &Run{
		ID:          "run-with-repo",
		StartedAt:   now,
		Status:      RunStatusRunning,
		Concurrency: 4,
		GitBranch:   "main",
		GitCommit:   "abc123",
		TotalTasks:  5,
		RepoID:      "repo-123",
		RepoPath:    "/path/to/repo",
		RepoName:    "my-repo",
	}

	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	retrieved, err := store.GetRun("run-with-repo")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}

	if retrieved == nil {
		t.Fatal("run not found")
	}

	if retrieved.RepoID != "repo-123" {
		t.Errorf("expected RepoID 'repo-123', got '%s'", retrieved.RepoID)
	}
	if retrieved.RepoPath != "/path/to/repo" {
		t.Errorf("expected RepoPath '/path/to/repo', got '%s'", retrieved.RepoPath)
	}
	if retrieved.RepoName != "my-repo" {
		t.Errorf("expected RepoName 'my-repo', got '%s'", retrieved.RepoName)
	}
}

func TestListRunsFilterByRepoID(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create runs with different repo IDs
	runs := []*Run{
		{ID: "run-1", StartedAt: now.Add(-3 * time.Hour), Status: RunStatusCompleted, RepoID: "repo-a"},
		{ID: "run-2", StartedAt: now.Add(-2 * time.Hour), Status: RunStatusCompleted, RepoID: "repo-a"},
		{ID: "run-3", StartedAt: now.Add(-1 * time.Hour), Status: RunStatusCompleted, RepoID: "repo-b"},
		{ID: "run-4", StartedAt: now, Status: RunStatusRunning, RepoID: "repo-b"},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Filter by repo-a
	result, err := store.ListRuns(RunFilter{RepoID: "repo-a"})
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}

	if len(result.Runs) != 2 {
		t.Errorf("expected 2 runs for repo-a, got %d", len(result.Runs))
	}

	// Filter by repo-b
	result, err = store.ListRuns(RunFilter{RepoID: "repo-b"})
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}

	if len(result.Runs) != 2 {
		t.Errorf("expected 2 runs for repo-b, got %d", len(result.Runs))
	}

	// Filter by status and repo
	result, err = store.ListRuns(RunFilter{RepoID: "repo-b", Status: RunStatusRunning})
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}

	if len(result.Runs) != 1 {
		t.Errorf("expected 1 running run for repo-b, got %d", len(result.Runs))
	}
}

func TestGetRunsByRepo(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create runs with different repo IDs
	runs := []*Run{
		{ID: "run-1", StartedAt: now.Add(-3 * time.Hour), Status: RunStatusCompleted, RepoID: "repo-a"},
		{ID: "run-2", StartedAt: now.Add(-2 * time.Hour), Status: RunStatusCompleted, RepoID: "repo-a"},
		{ID: "run-3", StartedAt: now.Add(-1 * time.Hour), Status: RunStatusCompleted, RepoID: "repo-b"},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Get runs for repo-a
	repoARuns, err := store.GetRunsByRepo("repo-a")
	if err != nil {
		t.Fatalf("failed to get runs by repo: %v", err)
	}

	if len(repoARuns) != 2 {
		t.Errorf("expected 2 runs for repo-a, got %d", len(repoARuns))
	}

	// Verify ordering (most recent first)
	if repoARuns[0].ID != "run-2" {
		t.Errorf("expected run-2 first (most recent), got %s", repoARuns[0].ID)
	}

	// Get runs for repo-b
	repoBRuns, err := store.GetRunsByRepo("repo-b")
	if err != nil {
		t.Fatalf("failed to get runs by repo: %v", err)
	}

	if len(repoBRuns) != 1 {
		t.Errorf("expected 1 run for repo-b, got %d", len(repoBRuns))
	}

	// Get runs for non-existent repo
	noRuns, err := store.GetRunsByRepo("repo-nonexistent")
	if err != nil {
		t.Fatalf("failed to get runs by repo: %v", err)
	}

	if len(noRuns) != 0 {
		t.Errorf("expected 0 runs for nonexistent repo, got %d", len(noRuns))
	}
}

func TestGetStatsByRepo(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create runs with different repo IDs
	runs := []*Run{
		{ID: "run-1", StartedAt: now.Add(-time.Hour), Status: RunStatusCompleted, RepoID: "repo-a"},
		{ID: "run-2", StartedAt: now.Add(-30 * time.Minute), Status: RunStatusCompleted, RepoID: "repo-a"},
		{ID: "run-3", StartedAt: now, Status: RunStatusFailed, RepoID: "repo-b"},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Create agents with repo IDs
	agents := []*Agent{
		{ID: "agent-1", RunID: "run-1", TaskID: "task-1", TaskTitle: "Task 1", Status: AgentStatusCompleted, StartedAt: now, InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500, CostUSD: 0.05, DurationSeconds: 60, RepoID: "repo-a"},
		{ID: "agent-2", RunID: "run-2", TaskID: "task-2", TaskTitle: "Task 2", Status: AgentStatusCompleted, StartedAt: now, InputTokens: 2000, OutputTokens: 800, TotalTokens: 2800, CostUSD: 0.08, DurationSeconds: 120, RepoID: "repo-a"},
		{ID: "agent-3", RunID: "run-3", TaskID: "task-3", TaskTitle: "Task 3", Status: AgentStatusFailed, StartedAt: now, InputTokens: 500, OutputTokens: 200, TotalTokens: 700, CostUSD: 0.02, DurationSeconds: 30, RepoID: "repo-b"},
	}

	for _, agent := range agents {
		if err := store.CreateAgent(agent); err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
	}

	// Get stats for repo-a
	statsA, err := store.GetStatsByRepo("repo-a", nil)
	if err != nil {
		t.Fatalf("failed to get stats by repo: %v", err)
	}

	if statsA.TotalRuns != 2 {
		t.Errorf("expected 2 total runs for repo-a, got %d", statsA.TotalRuns)
	}
	if statsA.CompletedRuns != 2 {
		t.Errorf("expected 2 completed runs for repo-a, got %d", statsA.CompletedRuns)
	}
	if statsA.TotalAgents != 2 {
		t.Errorf("expected 2 agents for repo-a, got %d", statsA.TotalAgents)
	}
	if statsA.TotalInputTokens != 3000 {
		t.Errorf("expected 3000 input tokens for repo-a, got %d", statsA.TotalInputTokens)
	}

	// Get stats for repo-b
	statsB, err := store.GetStatsByRepo("repo-b", nil)
	if err != nil {
		t.Fatalf("failed to get stats by repo: %v", err)
	}

	if statsB.TotalRuns != 1 {
		t.Errorf("expected 1 total run for repo-b, got %d", statsB.TotalRuns)
	}
	if statsB.FailedRuns != 1 {
		t.Errorf("expected 1 failed run for repo-b, got %d", statsB.FailedRuns)
	}
}

func TestGetStatsByRepo_WithSince(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create runs with different times
	runs := []*Run{
		{ID: "run-old", StartedAt: now.Add(-7 * 24 * time.Hour), Status: RunStatusCompleted, RepoID: "repo-a"},
		{ID: "run-recent", StartedAt: now.Add(-time.Hour), Status: RunStatusCompleted, RepoID: "repo-a"},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Get stats for last 24 hours only
	since := now.Add(-24 * time.Hour)
	stats, err := store.GetStatsByRepo("repo-a", &since)
	if err != nil {
		t.Fatalf("failed to get stats by repo: %v", err)
	}

	if stats.TotalRuns != 1 {
		t.Errorf("expected 1 recent run for repo-a, got %d", stats.TotalRuns)
	}
}

func TestMigrateOrphanedRepoIDs(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// Create runs with NULL repo_id but with repo_path
	runs := []*Run{
		{ID: "run-1", StartedAt: now.Add(-2 * time.Hour), Status: RunStatusCompleted, RepoPath: "/path/to/repo-a"},
		{ID: "run-2", StartedAt: now.Add(-1 * time.Hour), Status: RunStatusCompleted, RepoPath: "/path/to/repo-b"},
		{ID: "run-3", StartedAt: now, Status: RunStatusCompleted, RepoPath: "/path/to/unknown"},
	}

	for _, run := range runs {
		if err := store.CreateRun(run); err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
	}

	// Create agents for these runs
	agents := []*Agent{
		{ID: "agent-1", RunID: "run-1", TaskID: "task-1", TaskTitle: "Task 1", Status: AgentStatusCompleted, StartedAt: now},
		{ID: "agent-2", RunID: "run-2", TaskID: "task-2", TaskTitle: "Task 2", Status: AgentStatusCompleted, StartedAt: now},
	}

	for _, agent := range agents {
		if err := store.CreateAgent(agent); err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
	}

	// Lookup function that knows about repo-a and repo-b
	lookupFn := func(path string) (string, bool) {
		switch path {
		case "/path/to/repo-a":
			return "repo-id-a", true
		case "/path/to/repo-b":
			return "repo-id-b", true
		default:
			return "", false
		}
	}

	// Run migration
	updated, err := store.MigrateOrphanedRepoIDs(lookupFn)
	if err != nil {
		t.Fatalf("failed to migrate orphaned repo IDs: %v", err)
	}

	if updated != 2 {
		t.Errorf("expected 2 runs updated, got %d", updated)
	}

	// Verify run-1 has repo_id
	run1, err := store.GetRun("run-1")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if run1.RepoID != "repo-id-a" {
		t.Errorf("expected run-1 RepoID 'repo-id-a', got '%s'", run1.RepoID)
	}

	// Verify run-2 has repo_id
	run2, err := store.GetRun("run-2")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if run2.RepoID != "repo-id-b" {
		t.Errorf("expected run-2 RepoID 'repo-id-b', got '%s'", run2.RepoID)
	}

	// Verify run-3 still has NULL repo_id (unknown path)
	run3, err := store.GetRun("run-3")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if run3.RepoID != "" {
		t.Errorf("expected run-3 RepoID to be empty, got '%s'", run3.RepoID)
	}

	// Verify agent-1 has repo_id
	agent1, err := store.GetAgent("agent-1")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent1.RepoID != "repo-id-a" {
		t.Errorf("expected agent-1 RepoID 'repo-id-a', got '%s'", agent1.RepoID)
	}
}

func TestCreateAgentWithRepoID(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()

	// First create a run
	run := &Run{ID: "run-for-agent", StartedAt: now, Status: RunStatusRunning, RepoID: "repo-123"}
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	agent := &Agent{
		ID:           "agent-with-repo",
		RunID:        "run-for-agent",
		TaskID:       "task-123",
		TaskTitle:    "Test Task",
		Status:       AgentStatusRunning,
		StartedAt:    now,
		InputTokens:  1000,
		OutputTokens: 500,
		TotalTokens:  1500,
		CostUSD:      0.05,
		RepoID:       "repo-123",
	}

	if err := store.CreateAgent(agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	retrieved, err := store.GetAgent("agent-with-repo")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}

	if retrieved == nil {
		t.Fatal("agent not found")
	}

	if retrieved.RepoID != "repo-123" {
		t.Errorf("expected RepoID 'repo-123', got '%s'", retrieved.RepoID)
	}
}

func TestMigrationV2(t *testing.T) {
	// Create a store, which runs migrations including v2
	store := createTestStore(t)
	defer store.Close()

	// Verify the new columns exist by creating a run with repo fields
	now := time.Now()
	run := &Run{
		ID:        "migration-test-run",
		StartedAt: now,
		Status:    RunStatusCompleted,
		RepoID:    "test-repo-id",
		RepoPath:  "/test/path",
		RepoName:  "test-repo",
	}

	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run with repo fields after migration: %v", err)
	}

	// Verify by reading back
	retrieved, err := store.GetRun("migration-test-run")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}

	if retrieved.RepoID != "test-repo-id" {
		t.Errorf("expected RepoID 'test-repo-id', got '%s'", retrieved.RepoID)
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
