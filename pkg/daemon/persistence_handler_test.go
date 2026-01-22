package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/persistence"
)

func TestPersistenceHandler_RunLifecycle(t *testing.T) {
	// Create a temporary database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create event bus and persistence handler
	eventBus := NewEventBus()
	handler := NewPersistenceHandler(store, eventBus)
	unsubscribe := handler.Start()
	defer unsubscribe()

	// Test run started event
	runID := "test-run-123"
	eventBus.Publish(Event{
		Type:      EventRunStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id":     runID,
			"task_count": 5,
		},
	})

	// Give the handler time to process
	time.Sleep(10 * time.Millisecond)

	// Verify run was created
	run, err := store.GetRun(runID)
	if err != nil {
		t.Fatalf("Failed to get run: %v", err)
	}
	if run == nil {
		t.Fatal("Run was not created")
	}
	if run.Status != persistence.RunStatusRunning {
		t.Errorf("Expected status running, got %s", run.Status)
	}
	if run.TotalTasks != 5 {
		t.Errorf("Expected 5 total tasks, got %d", run.TotalTasks)
	}

	// Test run completed event
	eventBus.Publish(Event{
		Type:      EventRunCompleted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id":          runID,
			"total_tasks":     5,
			"succeeded_tasks": 4,
			"failed_tasks":    1,
		},
	})

	time.Sleep(10 * time.Millisecond)

	// Verify run was updated
	run, err = store.GetRun(runID)
	if err != nil {
		t.Fatalf("Failed to get run: %v", err)
	}
	if run.Status != persistence.RunStatusPartial {
		t.Errorf("Expected status partial (some succeeded, some failed), got %s", run.Status)
	}
	if run.CompletedTasks != 4 {
		t.Errorf("Expected 4 completed tasks, got %d", run.CompletedTasks)
	}
	if run.FailedTasks != 1 {
		t.Errorf("Expected 1 failed task, got %d", run.FailedTasks)
	}
	if run.FinishedAt == nil {
		t.Error("Expected FinishedAt to be set")
	}
}

func TestPersistenceHandler_AgentLifecycle(t *testing.T) {
	// Create a temporary database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create event bus and persistence handler
	eventBus := NewEventBus()
	handler := NewPersistenceHandler(store, eventBus)
	unsubscribe := handler.Start()
	defer unsubscribe()

	// Start a run first
	runID := "test-run-456"
	eventBus.Publish(Event{
		Type:      EventRunStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id":     runID,
			"task_count": 1,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Test agent started event
	agentID := "agent-1"
	eventBus.Publish(Event{
		Type:      EventAgentStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":   agentID,
			"task_id":    "task-1",
			"task_title": "Test Task",
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Verify agent was created
	agent, err := store.GetAgent(agentID)
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}
	if agent == nil {
		t.Fatal("Agent was not created")
	}
	if agent.RunID != runID {
		t.Errorf("Expected run_id %s, got %s", runID, agent.RunID)
	}
	if agent.TaskID != "task-1" {
		t.Errorf("Expected task_id task-1, got %s", agent.TaskID)
	}
	if agent.TaskTitle != "Test Task" {
		t.Errorf("Expected task_title 'Test Task', got %s", agent.TaskTitle)
	}
	if agent.Status != persistence.AgentStatusRunning {
		t.Errorf("Expected status running, got %s", agent.Status)
	}

	// Test agent completed event (success)
	eventBus.Publish(Event{
		Type:      EventAgentCompleted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":        agentID,
			"exit_code":       0,
			"duration":        10.5,
			"input_tokens":    100,
			"output_tokens":   200,
			"cost_usd":        0.05,
			"files_changed":   3,
			"commits_created": 1,
			"stdout":          "test stdout output",
			"stderr":          "test stderr output",
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Verify agent was updated
	agent, err = store.GetAgent(agentID)
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}
	if agent.Status != persistence.AgentStatusCompleted {
		t.Errorf("Expected status completed, got %s", agent.Status)
	}
	if agent.ExitCode == nil || *agent.ExitCode != 0 {
		t.Errorf("Expected exit_code 0, got %v", agent.ExitCode)
	}
	if agent.InputTokens != 100 {
		t.Errorf("Expected input_tokens 100, got %d", agent.InputTokens)
	}
	if agent.OutputTokens != 200 {
		t.Errorf("Expected output_tokens 200, got %d", agent.OutputTokens)
	}
	if agent.TotalTokens != 300 {
		t.Errorf("Expected total_tokens 300, got %d", agent.TotalTokens)
	}
	if agent.Stdout != "test stdout output" {
		t.Errorf("Expected stdout 'test stdout output', got %q", agent.Stdout)
	}
	if agent.Stderr != "test stderr output" {
		t.Errorf("Expected stderr 'test stderr output', got %q", agent.Stderr)
	}
	if agent.GitCommitsCreated != 1 {
		t.Errorf("Expected git_commits_created 1, got %d", agent.GitCommitsCreated)
	}
}

func TestPersistenceHandler_AgentFailed(t *testing.T) {
	// Create a temporary database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create event bus and persistence handler
	eventBus := NewEventBus()
	handler := NewPersistenceHandler(store, eventBus)
	unsubscribe := handler.Start()
	defer unsubscribe()

	// Start a run first
	runID := "test-run-789"
	eventBus.Publish(Event{
		Type:      EventRunStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id":     runID,
			"task_count": 1,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Test agent started
	agentID := "agent-fail"
	eventBus.Publish(Event{
		Type:      EventAgentStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":   agentID,
			"task_id":    "task-fail",
			"task_title": "Failing Task",
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Test agent completed with error
	eventBus.Publish(Event{
		Type:      EventAgentCompleted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":  agentID,
			"error":     "task failed: something went wrong",
			"exit_code": 1,
			"duration":  5.0,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Verify agent was updated with failure status
	agent, err := store.GetAgent(agentID)
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}
	if agent.Status != persistence.AgentStatusFailed {
		t.Errorf("Expected status failed, got %s", agent.Status)
	}
	if agent.ErrorMessage != "task failed: something went wrong" {
		t.Errorf("Expected error message, got %s", agent.ErrorMessage)
	}
	if agent.ExitCode == nil || *agent.ExitCode != 1 {
		t.Errorf("Expected exit_code 1, got %v", agent.ExitCode)
	}
}

func TestPersistenceHandler_SuccessfulRun(t *testing.T) {
	// Create a temporary database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create event bus and persistence handler
	eventBus := NewEventBus()
	handler := NewPersistenceHandler(store, eventBus)
	unsubscribe := handler.Start()
	defer unsubscribe()

	// Simulate a complete successful run
	runID := "successful-run"
	eventBus.Publish(Event{
		Type:      EventRunStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id":     runID,
			"task_count": 2,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Run completes with all tasks successful
	eventBus.Publish(Event{
		Type:      EventRunCompleted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id":          runID,
			"total_tasks":     2,
			"succeeded_tasks": 2,
			"failed_tasks":    0,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Verify run status is completed (not failed)
	run, err := store.GetRun(runID)
	if err != nil {
		t.Fatalf("Failed to get run: %v", err)
	}
	if run.Status != persistence.RunStatusCompleted {
		t.Errorf("Expected status completed, got %s", run.Status)
	}
}

func TestPersistenceHandler_AgentMergeStatus(t *testing.T) {
	// Create a temporary database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create event bus and persistence handler
	eventBus := NewEventBus()
	handler := NewPersistenceHandler(store, eventBus)
	unsubscribe := handler.Start()
	defer unsubscribe()

	// Start a run first
	runID := "test-run-merge"
	eventBus.Publish(Event{
		Type:      EventRunStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id":     runID,
			"task_count": 1,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Start an agent
	agentID := "agent-merge-test"
	eventBus.Publish(Event{
		Type:      EventAgentStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":   agentID,
			"task_id":    "task-merge",
			"task_title": "Merge Test Task",
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Verify agent was created
	agent, err := store.GetAgent(agentID)
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}
	if agent == nil {
		t.Fatal("Agent was not created")
	}

	// Send merge status event with "merged" status (final status)
	eventBus.Publish(Event{
		Type:      EventAgentMergeStatus,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":         agentID,
			"merge_status":     "merged",
			"commits_applied":  3,
			"had_conflict":     true,
			"resolver_spawned": true,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Verify merge status was persisted
	agent, err = store.GetAgent(agentID)
	if err != nil {
		t.Fatalf("Failed to get agent after merge: %v", err)
	}
	if agent.MergeStatus != persistence.MergeStatusMerged {
		t.Errorf("Expected merge_status 'merged', got %q", agent.MergeStatus)
	}
	if agent.MergeCommitsApplied != 3 {
		t.Errorf("Expected merge_commits_applied 3, got %d", agent.MergeCommitsApplied)
	}
	if !agent.MergeHadConflict {
		t.Error("Expected merge_had_conflict to be true")
	}
	if !agent.MergeResolverSpawned {
		t.Error("Expected merge_resolver_spawned to be true")
	}
}

func TestPersistenceHandler_AgentMergeStatus_Failed(t *testing.T) {
	// Create a temporary database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create event bus and persistence handler
	eventBus := NewEventBus()
	handler := NewPersistenceHandler(store, eventBus)
	unsubscribe := handler.Start()
	defer unsubscribe()

	// Start a run first
	runID := "test-run-merge-fail"
	eventBus.Publish(Event{
		Type:      EventRunStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id":     runID,
			"task_count": 1,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Start an agent
	agentID := "agent-merge-fail-test"
	eventBus.Publish(Event{
		Type:      EventAgentStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":   agentID,
			"task_id":    "task-merge-fail",
			"task_title": "Merge Fail Test Task",
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Send merge status event with "failed" status
	eventBus.Publish(Event{
		Type:      EventAgentMergeStatus,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":     agentID,
			"merge_status": "failed",
			"error":        "merge conflict could not be resolved",
			"had_conflict": true,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Verify merge status was persisted
	agent, err := store.GetAgent(agentID)
	if err != nil {
		t.Fatalf("Failed to get agent after merge: %v", err)
	}
	if agent.MergeStatus != persistence.MergeStatusFailed {
		t.Errorf("Expected merge_status 'failed', got %q", agent.MergeStatus)
	}
	if agent.MergeError != "merge conflict could not be resolved" {
		t.Errorf("Expected merge_error message, got %q", agent.MergeError)
	}
	if !agent.MergeHadConflict {
		t.Error("Expected merge_had_conflict to be true")
	}
}

func TestPersistenceHandler_AgentMergeStatus_IntermediateStatusIgnored(t *testing.T) {
	// Create a temporary database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create event bus and persistence handler
	eventBus := NewEventBus()
	handler := NewPersistenceHandler(store, eventBus)
	unsubscribe := handler.Start()
	defer unsubscribe()

	// Start a run first
	runID := "test-run-intermediate"
	eventBus.Publish(Event{
		Type:      EventRunStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id":     runID,
			"task_count": 1,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Start an agent
	agentID := "agent-intermediate-test"
	eventBus.Publish(Event{
		Type:      EventAgentStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":   agentID,
			"task_id":    "task-intermediate",
			"task_title": "Intermediate Status Test",
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Send intermediate status events (should be ignored for persistence)
	intermediateStatuses := []string{"pending", "queued", "merging", "resolving"}
	for _, status := range intermediateStatuses {
		eventBus.Publish(Event{
			Type:      EventAgentMergeStatus,
			Timestamp: time.Now(),
			Payload: map[string]interface{}{
				"agent_id":     agentID,
				"merge_status": status,
			},
		})
	}
	time.Sleep(10 * time.Millisecond)

	// Verify merge status was NOT persisted (still empty)
	agent, err := store.GetAgent(agentID)
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}
	if agent.MergeStatus != "" {
		t.Errorf("Expected empty merge_status for intermediate statuses, got %q", agent.MergeStatus)
	}
}

func TestPersistenceHandler_AgentMergeStatus_WithValidation(t *testing.T) {
	// Create a temporary database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create event bus and persistence handler
	eventBus := NewEventBus()
	handler := NewPersistenceHandler(store, eventBus)
	unsubscribe := handler.Start()
	defer unsubscribe()

	// Start a run first
	runID := "test-run-validation"
	eventBus.Publish(Event{
		Type:      EventRunStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id":     runID,
			"task_count": 1,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Start an agent
	agentID := "agent-validation-test"
	eventBus.Publish(Event{
		Type:      EventAgentStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":   agentID,
			"task_id":    "task-validation",
			"task_title": "Validation Test Task",
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Send merge status with validation info
	eventBus.Publish(Event{
		Type:      EventAgentMergeStatus,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":              agentID,
			"merge_status":          "merged",
			"commits_applied":       2,
			"validation_status":     "passed",
			"validation_duration_ms": int64(1500),
			"validation_steps": []interface{}{
				map[string]interface{}{"name": "build", "passed": true},
				map[string]interface{}{"name": "test", "passed": true},
			},
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Verify merge and validation status were persisted
	agent, err := store.GetAgent(agentID)
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}
	if agent.MergeStatus != persistence.MergeStatusMerged {
		t.Errorf("Expected merge_status 'merged', got %q", agent.MergeStatus)
	}
	if agent.ValidationStatus != "passed" {
		t.Errorf("Expected validation_status 'passed', got %q", agent.ValidationStatus)
	}
	if agent.ValidationDuration != 1500 {
		t.Errorf("Expected validation_duration_ms 1500, got %d", agent.ValidationDuration)
	}
}

func TestPersistenceHandler_AgentMergeStatus_WithRepair(t *testing.T) {
	// Create a temporary database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create event bus and persistence handler
	eventBus := NewEventBus()
	handler := NewPersistenceHandler(store, eventBus)
	unsubscribe := handler.Start()
	defer unsubscribe()

	// Start a run first
	runID := "test-run-repair"
	eventBus.Publish(Event{
		Type:      EventRunStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id":     runID,
			"task_count": 1,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Start an agent
	agentID := "agent-repair-test"
	eventBus.Publish(Event{
		Type:      EventAgentStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":   agentID,
			"task_id":    "task-repair",
			"task_title": "Repair Test Task",
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Send merge status with repair info (validation repairing)
	eventBus.Publish(Event{
		Type:      EventAgentMergeStatus,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":           agentID,
			"merge_status":       "merged_needs_repair",
			"commits_applied":    1,
			"validation_status":  "repairing",
			"repair_attempts":    2,
			"last_repair_output": "Fixed linting errors in main.go",
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Verify repair state was persisted
	agent, err := store.GetAgent(agentID)
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}
	if agent.MergeStatus != persistence.MergeStatusMergedNeedsRepair {
		t.Errorf("Expected merge_status 'merged_needs_repair', got %q", agent.MergeStatus)
	}
	if agent.RepairAttempts != 2 {
		t.Errorf("Expected repair_attempts 2, got %d", agent.RepairAttempts)
	}
	if agent.LastRepairOutput != "Fixed linting errors in main.go" {
		t.Errorf("Expected last_repair_output, got %q", agent.LastRepairOutput)
	}
}

func TestPersistenceHandler_MergeStatusNotOverwrittenByCompletion(t *testing.T) {
	// This test verifies the fix for the bug where agent completion events
	// would overwrite previously persisted merge status values.
	// The sequence is:
	// 1. Agent started
	// 2. Merge status event arrives (merge_status = "merged")
	// 3. Agent completed event arrives
	// 4. Verify merge_status is still "merged" (not overwritten to empty)

	// Create a temporary database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create event bus and persistence handler
	eventBus := NewEventBus()
	handler := NewPersistenceHandler(store, eventBus)
	unsubscribe := handler.Start()
	defer unsubscribe()

	// Start a run first
	runID := "test-run-overwrite"
	eventBus.Publish(Event{
		Type:      EventRunStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id":     runID,
			"task_count": 1,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Start an agent
	agentID := "agent-overwrite-test"
	eventBus.Publish(Event{
		Type:      EventAgentStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":   agentID,
			"task_id":    "task-overwrite",
			"task_title": "Overwrite Test Task",
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Send merge status event BEFORE completion
	eventBus.Publish(Event{
		Type:      EventAgentMergeStatus,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":              agentID,
			"merge_status":          "merged",
			"commits_applied":       5,
			"had_conflict":          true,
			"resolver_spawned":      true,
			"validation_status":     "passed",
			"validation_duration_ms": int64(2000),
			"repair_attempts":       1,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// Verify merge status was persisted
	agent, err := store.GetAgent(agentID)
	if err != nil {
		t.Fatalf("Failed to get agent after merge status: %v", err)
	}
	if agent.MergeStatus != persistence.MergeStatusMerged {
		t.Fatalf("Merge status not persisted correctly, got %q", agent.MergeStatus)
	}

	// Now send agent completion event (this would have overwritten merge status before the fix)
	eventBus.Publish(Event{
		Type:      EventAgentCompleted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"agent_id":      agentID,
			"exit_code":     0,
			"duration":      30.0,
			"input_tokens":  500,
			"output_tokens": 1000,
			"cost_usd":      0.10,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// CRITICAL CHECK: Verify merge status was NOT overwritten by completion event
	agent, err = store.GetAgent(agentID)
	if err != nil {
		t.Fatalf("Failed to get agent after completion: %v", err)
	}

	// Agent completion fields should be updated
	if agent.Status != persistence.AgentStatusCompleted {
		t.Errorf("Expected status completed, got %s", agent.Status)
	}
	if agent.InputTokens != 500 {
		t.Errorf("Expected input_tokens 500, got %d", agent.InputTokens)
	}

	// Merge fields should be preserved (not overwritten to empty/zero)
	if agent.MergeStatus != persistence.MergeStatusMerged {
		t.Errorf("REGRESSION: merge_status was overwritten! Expected 'merged', got %q", agent.MergeStatus)
	}
	if agent.MergeCommitsApplied != 5 {
		t.Errorf("REGRESSION: merge_commits_applied was overwritten! Expected 5, got %d", agent.MergeCommitsApplied)
	}
	if !agent.MergeHadConflict {
		t.Error("REGRESSION: merge_had_conflict was overwritten! Expected true")
	}
	if !agent.MergeResolverSpawned {
		t.Error("REGRESSION: merge_resolver_spawned was overwritten! Expected true")
	}

	// Validation/repair fields should also be preserved
	if agent.ValidationStatus != "passed" {
		t.Errorf("REGRESSION: validation_status was overwritten! Expected 'passed', got %q", agent.ValidationStatus)
	}
	if agent.ValidationDuration != 2000 {
		t.Errorf("REGRESSION: validation_duration was overwritten! Expected 2000, got %d", agent.ValidationDuration)
	}
	if agent.RepairAttempts != 1 {
		t.Errorf("REGRESSION: repair_attempts was overwritten! Expected 1, got %d", agent.RepairAttempts)
	}
}

func TestRebuildAgentChildLinks(t *testing.T) {
	tests := []struct {
		name     string
		agents   map[string]*AgentState
		expected map[string][]string // agentID -> expected ChildAgentIDs
	}{
		{
			name:     "empty agents",
			agents:   map[string]*AgentState{},
			expected: map[string][]string{},
		},
		{
			name: "single agent no parent",
			agents: map[string]*AgentState{
				"agent-1": {ID: "agent-1"},
			},
			expected: map[string][]string{
				"agent-1": nil,
			},
		},
		{
			name: "parent with one child",
			agents: map[string]*AgentState{
				"parent":  {ID: "parent"},
				"child-1": {ID: "child-1", ParentAgentID: "parent"},
			},
			expected: map[string][]string{
				"parent":  {"child-1"},
				"child-1": nil,
			},
		},
		{
			name: "parent with multiple children",
			agents: map[string]*AgentState{
				"parent":  {ID: "parent"},
				"child-1": {ID: "child-1", ParentAgentID: "parent"},
				"child-2": {ID: "child-2", ParentAgentID: "parent"},
				"child-3": {ID: "child-3", ParentAgentID: "parent"},
			},
			expected: map[string][]string{
				"parent":  {"child-1", "child-2", "child-3"},
				"child-1": nil,
				"child-2": nil,
				"child-3": nil,
			},
		},
		{
			name: "nested parent-child relationships",
			agents: map[string]*AgentState{
				"grandparent": {ID: "grandparent"},
				"parent":      {ID: "parent", ParentAgentID: "grandparent"},
				"child":       {ID: "child", ParentAgentID: "parent"},
			},
			expected: map[string][]string{
				"grandparent": {"parent"},
				"parent":      {"child"},
				"child":       nil,
			},
		},
		{
			name: "orphaned child (parent does not exist)",
			agents: map[string]*AgentState{
				"orphan": {ID: "orphan", ParentAgentID: "nonexistent"},
			},
			expected: map[string][]string{
				"orphan": nil,
			},
		},
		{
			name: "clears existing ChildAgentIDs before rebuilding",
			agents: map[string]*AgentState{
				"parent":  {ID: "parent", ChildAgentIDs: []string{"stale-child"}},
				"child-1": {ID: "child-1", ParentAgentID: "parent"},
			},
			expected: map[string][]string{
				"parent":  {"child-1"},
				"child-1": nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RebuildAgentChildLinks(tt.agents)

			for agentID, expectedChildren := range tt.expected {
				agent, exists := tt.agents[agentID]
				if !exists {
					t.Errorf("agent %s not found", agentID)
					continue
				}

				// For nil expected, check that actual is nil or empty
				if expectedChildren == nil {
					if len(agent.ChildAgentIDs) != 0 {
						t.Errorf("agent %s: expected no children, got %v", agentID, agent.ChildAgentIDs)
					}
					continue
				}

				// Check that all expected children are present
				if len(agent.ChildAgentIDs) != len(expectedChildren) {
					t.Errorf("agent %s: expected %d children, got %d (%v)", agentID, len(expectedChildren), len(agent.ChildAgentIDs), agent.ChildAgentIDs)
					continue
				}

				// Build a set of actual children for easy lookup
				actualSet := make(map[string]bool)
				for _, child := range agent.ChildAgentIDs {
					actualSet[child] = true
				}

				for _, expected := range expectedChildren {
					if !actualSet[expected] {
						t.Errorf("agent %s: expected child %s not found in %v", agentID, expected, agent.ChildAgentIDs)
					}
				}
			}
		})
	}
}

// Ensure dbPath cleanup
func init() {
	_ = os.Setenv("XDG_CACHE_HOME", os.TempDir())
}
