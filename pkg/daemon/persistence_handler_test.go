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
	defer store.Close()

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
	if run.Status != persistence.RunStatusFailed {
		t.Errorf("Expected status failed (due to failed tasks), got %s", run.Status)
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
	defer store.Close()

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
}

func TestPersistenceHandler_AgentFailed(t *testing.T) {
	// Create a temporary database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

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
	defer store.Close()

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

// Ensure dbPath cleanup
func init() {
	os.Setenv("XDG_CACHE_HOME", os.TempDir())
}
