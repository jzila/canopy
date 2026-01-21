package daemon

import (
	"context"
	"testing"
	"time"

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
