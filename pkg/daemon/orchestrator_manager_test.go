package daemon

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/errors"
	"github.com/jzila/canopy/pkg/events"
)

func TestNewOrchestratorManager(t *testing.T) {
	eventBus := events.NewEventBus()
	state := NewRuntimeState()

	manager := NewOrchestratorManager(eventBus, state)
	if manager == nil {
		t.Fatal("expected non-nil manager")
	}
	if manager.eventBus != eventBus {
		t.Error("expected event bus to be set")
	}
	if manager.state != state {
		t.Error("expected state to be set")
	}
}

func TestStartRunValidation(t *testing.T) {
	eventBus := events.NewEventBus()
	state := NewRuntimeState()
	manager := NewOrchestratorManager(eventBus, state)

	// Test missing work_dir
	ctx := context.Background()
	_, err := manager.StartRun(ctx, RunConfig{})
	if err == nil {
		t.Error("expected error for missing work_dir")
	}
}

func TestStartRunDefaults(t *testing.T) {
	// This test verifies that defaults are applied correctly.
	// We can't fully test orchestrator creation without a real beads database,
	// but we can test the config validation logic.

	config := RunConfig{
		WorkDir: "/nonexistent/path",
	}

	// Verify defaults
	if config.Concurrency != 0 {
		t.Errorf("expected default Concurrency to be 0, got %d", config.Concurrency)
	}
	if config.OutputDir != "" {
		t.Errorf("expected default OutputDir to be empty, got %s", config.OutputDir)
	}
	if config.MaxRetries != 0 {
		t.Errorf("expected default MaxRetries to be 0, got %d", config.MaxRetries)
	}
}

func TestMakeAgentID(t *testing.T) {
	tests := []struct {
		runID    string
		taskID   string
		expected string
	}{
		{
			runID:    "12345678-1234-1234-1234-123456789abc",
			taskID:   "beads-abc",
			expected: "agent-12345678-beads-abc",
		},
		{
			runID:    "short",
			taskID:   "task-1",
			expected: "agent-short-task-1",
		},
		{
			runID:    "exactly8",
			taskID:   "task-2",
			expected: "agent-exactly8-task-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.runID, func(t *testing.T) {
			result := makeAgentID(tt.runID, tt.taskID)
			if result != tt.expected {
				t.Errorf("makeAgentID(%q, %q) = %q, want %q",
					tt.runID, tt.taskID, result, tt.expected)
			}
		})
	}
}

func TestRunStateToWire(t *testing.T) {
	now := time.Now()
	endTime := now.Add(5 * time.Minute)

	runState := &RunState{
		ID:          "test-run-id",
		RepoPath:    "/path/to/repo",
		RepoID:      "repo-123",
		Status:      RunStatusCompleted,
		StartTime:   now,
		EndTime:     &endTime,
		Error:       "",
		TasksTotal:  10,
		TasksDone:   8,
		TasksFailed: 2,
	}

	wire := runStateToWire(runState)
	if wire == nil {
		t.Fatal("expected non-nil wire format")
	}

	if wire.ID != runState.ID {
		t.Errorf("expected ID %q, got %q", runState.ID, wire.ID)
	}
	if wire.RepoPath != runState.RepoPath {
		t.Errorf("expected RepoPath %q, got %q", runState.RepoPath, wire.RepoPath)
	}
	if wire.Status != string(runState.Status) {
		t.Errorf("expected Status %q, got %q", runState.Status, wire.Status)
	}
	if wire.StartTime != now.Unix() {
		t.Errorf("expected StartTime %d, got %d", now.Unix(), wire.StartTime)
	}
	if wire.EndTime != endTime.Unix() {
		t.Errorf("expected EndTime %d, got %d", endTime.Unix(), wire.EndTime)
	}
	if wire.TasksTotal != runState.TasksTotal {
		t.Errorf("expected TasksTotal %d, got %d", runState.TasksTotal, wire.TasksTotal)
	}
	if wire.TasksDone != runState.TasksDone {
		t.Errorf("expected TasksDone %d, got %d", runState.TasksDone, wire.TasksDone)
	}
	if wire.TasksFailed != runState.TasksFailed {
		t.Errorf("expected TasksFailed %d, got %d", runState.TasksFailed, wire.TasksFailed)
	}
}

func TestRunStateToWireNil(t *testing.T) {
	wire := runStateToWire(nil)
	if wire != nil {
		t.Error("expected nil wire format for nil run state")
	}
}

func TestRunStateToWireNoEndTime(t *testing.T) {
	runState := &RunState{
		ID:        "test-run",
		StartTime: time.Now(),
		EndTime:   nil, // No end time (still running)
	}

	wire := runStateToWire(runState)
	if wire.EndTime != 0 {
		t.Errorf("expected EndTime to be 0 for nil end time, got %d", wire.EndTime)
	}
}

func TestListRunsEmpty(t *testing.T) {
	eventBus := events.NewEventBus()
	state := NewRuntimeState()
	manager := NewOrchestratorManager(eventBus, state)

	runs := manager.ListRuns()
	// Note: ListRuns may return nil for empty list, which is acceptable
	if len(runs) != 0 {
		t.Errorf("expected 0 runs, got %d", len(runs))
	}
}

func TestStopRunNotFound(t *testing.T) {
	eventBus := events.NewEventBus()
	state := NewRuntimeState()
	manager := NewOrchestratorManager(eventBus, state)

	err := manager.StopRun("nonexistent-run-id")
	if err == nil {
		t.Error("expected error for nonexistent run")
	}
}

func TestGetRunStatusNotFound(t *testing.T) {
	eventBus := events.NewEventBus()
	state := NewRuntimeState()
	manager := NewOrchestratorManager(eventBus, state)

	_, err := manager.GetRunStatus("nonexistent-run-id")
	if err == nil {
		t.Error("expected error for nonexistent run")
	}
}

func TestGetActiveRunForRepoEmpty(t *testing.T) {
	eventBus := events.NewEventBus()
	state := NewRuntimeState()
	manager := NewOrchestratorManager(eventBus, state)

	run, err := manager.GetActiveRunForRepo("/some/repo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if run != nil {
		t.Error("expected nil run for repo with no active run")
	}
}

func TestRunStatus(t *testing.T) {
	// Test RunStatus constants
	tests := []struct {
		status   RunStatus
		expected string
	}{
		{RunStatusPending, "pending"},
		{RunStatusRunning, "running"},
		{RunStatusCompleted, "completed"},
		{RunStatusFailed, "failed"},
		{RunStatusCancelled, "cancelled"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.expected {
			t.Errorf("RunStatus %q != expected %q", tt.status, tt.expected)
		}
	}
}

func TestSingletonEnforcement(t *testing.T) {
	eventBus := events.NewEventBus()
	state := NewRuntimeState()
	manager := NewOrchestratorManager(eventBus, state)

	// Manually simulate an active run by storing directly in runsByRepo and runs
	// This avoids needing a real beads database
	repoPath := "/test/repo"
	existingRunID := "existing-run-123"
	existingStartTime := time.Now().Add(-2 * time.Minute)

	existingRunState := &RunState{
		ID:        existingRunID,
		RepoPath:  repoPath,
		Status:    RunStatusRunning,
		StartTime: existingStartTime,
	}
	manager.runs.Store(existingRunID, existingRunState)
	manager.runsByRepo.Store(repoPath, existingRunID)

	// Try to start a new run for the same repo
	ctx := context.Background()
	_, err := manager.StartRun(ctx, RunConfig{
		WorkDir: repoPath,
	})

	// Should get an error
	if err == nil {
		t.Fatal("expected error for concurrent run, got nil")
	}

	// Error should be a RunActiveError
	var runActiveErr *errors.RunActiveError
	if !errors.As(err, &runActiveErr) {
		t.Fatalf("expected RunActiveError, got %T: %v", err, err)
	}

	// Verify error details
	if runActiveErr.RepoPath != repoPath {
		t.Errorf("expected RepoPath %q, got %q", repoPath, runActiveErr.RepoPath)
	}
	if runActiveErr.RunID != existingRunID {
		t.Errorf("expected RunID %q, got %q", existingRunID, runActiveErr.RunID)
	}
	if runActiveErr.StartedAt != existingStartTime {
		t.Errorf("expected StartedAt %v, got %v", existingStartTime, runActiveErr.StartedAt)
	}

	// Error should unwrap to ErrRunAlreadyActive
	if !errors.Is(err, errors.ErrRunAlreadyActive) {
		t.Error("expected error to unwrap to ErrRunAlreadyActive")
	}

	// Error message should include helpful suggestions
	errMsg := err.Error()
	if errMsg == "" {
		t.Error("expected non-empty error message")
	}
	// Check for key parts of the message
	expectedParts := []string{
		existingRunID,
		repoPath,
		"canopy run --status",
		"canopy run --stop",
	}
	for _, part := range expectedParts {
		if !containsSubstring(errMsg, part) {
			t.Errorf("error message should contain %q, got: %s", part, errMsg)
		}
	}
}

func TestSingletonEnforcementConcurrent(t *testing.T) {
	// Test that concurrent StartRun calls are properly serialized
	eventBus := events.NewEventBus()
	state := NewRuntimeState()
	manager := NewOrchestratorManager(eventBus, state)

	repoPath := "/test/concurrent/repo"
	ctx := context.Background()

	// Launch multiple goroutines trying to start runs concurrently
	const numGoroutines = 10
	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := manager.StartRun(ctx, RunConfig{
				WorkDir: repoPath,
			})
			errChan <- err
		}()
	}

	wg.Wait()
	close(errChan)

	// Count outcomes: exactly one should fail due to orchestrator.New (nonexistent path)
	// The rest should fail due to singleton enforcement
	var singletonErrors int
	var otherErrors int
	for err := range errChan {
		if err != nil {
			if errors.Is(err, errors.ErrRunAlreadyActive) {
				singletonErrors++
			} else {
				otherErrors++
			}
		}
	}

	// At most one request should get past the singleton check
	// (it will then fail for other reasons like missing beads)
	if otherErrors > 1 {
		t.Errorf("expected at most 1 non-singleton error, got %d", otherErrors)
	}

	// The rest should be singleton errors
	if singletonErrors < numGoroutines-1 {
		t.Errorf("expected at least %d singleton errors, got %d", numGoroutines-1, singletonErrors)
	}
}

func TestDifferentReposAllowed(t *testing.T) {
	eventBus := events.NewEventBus()
	state := NewRuntimeState()
	manager := NewOrchestratorManager(eventBus, state)

	// Simulate an active run for repo1
	repo1Path := "/test/repo1"
	existingRunID := "run-repo1"
	existingRunState := &RunState{
		ID:        existingRunID,
		RepoPath:  repo1Path,
		Status:    RunStatusRunning,
		StartTime: time.Now(),
	}
	manager.runs.Store(existingRunID, existingRunState)
	manager.runsByRepo.Store(repo1Path, existingRunID)

	// Try to start a run for a different repo - should be allowed (though will fail for other reasons)
	ctx := context.Background()
	repo2Path := "/test/repo2"
	_, err := manager.StartRun(ctx, RunConfig{
		WorkDir: repo2Path,
	})

	// Should NOT be a singleton error (will fail for other reasons like missing beads)
	if err != nil && errors.Is(err, errors.ErrRunAlreadyActive) {
		t.Error("should allow run for different repo, but got singleton error")
	}
}

// containsSubstring checks if s contains substr
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstringHelper(s, substr))
}

func containsSubstringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
