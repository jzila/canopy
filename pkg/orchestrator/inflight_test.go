package orchestrator

import (
	"context"
	"sync"
	"testing"

	"github.com/jzila/canopy/pkg/beads"
)

// MockBeadsClientForDynamic is a mock beads client that returns tasks dynamically
type MockBeadsClientForDynamic struct {
	beads.BeadsClient
	mu     sync.Mutex
	tasks  []beads.Task
	closed map[string]bool
}

func (m *MockBeadsClientForDynamic) Ready(ctx context.Context) ([]beads.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var ready []beads.Task
	for _, t := range m.tasks {
		if t.Status != "completed" && !m.closed[t.ID] {
			ready = append(ready, t)
		}
	}
	return ready, nil
}

func (m *MockBeadsClientForDynamic) CloseTask(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed[id] = true
}

func TestInFlightTracking_MarkAndUnmark(t *testing.T) {
	o := &Orchestrator{
		inFlight:     make(map[string]bool),
		inFlightTask: make(map[string]*beads.Task),
	}

	// Initially not in-flight
	if o.isInFlight("task-1") {
		t.Error("task-1 should not be in-flight initially")
	}

	// Mark as in-flight
	o.markInFlight("task-1")
	if !o.isInFlight("task-1") {
		t.Error("task-1 should be in-flight after marking")
	}

	// Other tasks still not in-flight
	if o.isInFlight("task-2") {
		t.Error("task-2 should not be in-flight")
	}

	// Unmark
	o.unmarkInFlight("task-1")
	if o.isInFlight("task-1") {
		t.Error("task-1 should not be in-flight after unmarking")
	}
}

func TestInFlightTracking_ConcurrentAccess(t *testing.T) {
	o := &Orchestrator{
		inFlight:     make(map[string]bool),
		inFlightTask: make(map[string]*beads.Task),
	}

	var wg sync.WaitGroup

	// Mark multiple tasks concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			taskID := "task-" + string(rune('0'+id%10))
			o.markInFlight(taskID)
			_ = o.isInFlight(taskID)
			o.unmarkInFlight(taskID)
		}(i)
	}

	wg.Wait()
}

func TestGetNextTask_FiltersInFlight(t *testing.T) {
	mockClient := &MockBeadsClientForDynamic{
		tasks: []beads.Task{
			{ID: "task-1", Title: "First task"},
			{ID: "task-2", Title: "Second task"},
			{ID: "task-3", Title: "Third task"},
		},
		closed: make(map[string]bool),
	}

	o := &Orchestrator{
		config:       &Config{MaxPriority: -1},
		beadsClient:  mockClient,
		inFlight:     make(map[string]bool),
		inFlightTask: make(map[string]*beads.Task),
	}

	ctx := context.Background()

	// Get first task
	task1, shouldStop, err := o.getNextTask(ctx)
	if err != nil {
		t.Fatalf("getNextTask failed: %v", err)
	}
	if shouldStop {
		t.Error("should not stop")
	}
	if task1 == nil || task1.ID != "task-1" {
		t.Errorf("expected task-1, got %v", task1)
	}

	// Get second task (task-1 is now in-flight)
	task2, shouldStop, err := o.getNextTask(ctx)
	if err != nil {
		t.Fatalf("getNextTask failed: %v", err)
	}
	if shouldStop {
		t.Error("should not stop")
	}
	if task2 == nil || task2.ID != "task-2" {
		t.Errorf("expected task-2, got %v", task2)
	}

	// Get third task (task-1 and task-2 are now in-flight)
	task3, shouldStop, err := o.getNextTask(ctx)
	if err != nil {
		t.Fatalf("getNextTask failed: %v", err)
	}
	if shouldStop {
		t.Error("should not stop")
	}
	if task3 == nil || task3.ID != "task-3" {
		t.Errorf("expected task-3, got %v", task3)
	}

	// No more tasks (all are in-flight)
	task4, shouldStop, err := o.getNextTask(ctx)
	if err != nil {
		t.Fatalf("getNextTask failed: %v", err)
	}
	if shouldStop {
		t.Error("should not stop - there are in-flight tasks")
	}
	if task4 != nil {
		t.Errorf("expected no task, got %v", task4)
	}
}

func TestGetNextTask_ReturnsNewlyUnblockedTasks(t *testing.T) {
	mockClient := &MockBeadsClientForDynamic{
		tasks: []beads.Task{
			{ID: "task-1", Title: "First task"},
		},
		closed: make(map[string]bool),
	}

	o := &Orchestrator{
		config:       &Config{MaxPriority: -1},
		beadsClient:  mockClient,
		inFlight:     make(map[string]bool),
		inFlightTask: make(map[string]*beads.Task),
	}

	ctx := context.Background()

	// Get first task
	task1, _, err := o.getNextTask(ctx)
	if err != nil {
		t.Fatalf("getNextTask failed: %v", err)
	}
	if task1 == nil || task1.ID != "task-1" {
		t.Errorf("expected task-1, got %v", task1)
	}

	// Simulate task-1 completing and unblocking task-2
	o.unmarkInFlight("task-1")
	mockClient.mu.Lock()
	mockClient.tasks = append(mockClient.tasks, beads.Task{ID: "task-2", Title: "Second task (was blocked)"})
	mockClient.closed["task-1"] = true
	mockClient.mu.Unlock()

	// Should now get task-2
	task2, _, err := o.getNextTask(ctx)
	if err != nil {
		t.Fatalf("getNextTask failed: %v", err)
	}
	if task2 == nil || task2.ID != "task-2" {
		t.Errorf("expected task-2, got %v", task2)
	}
}

func TestGetNextTask_AppliesMaxPriorityFilter(t *testing.T) {
	mockClient := &MockBeadsClientForDynamic{
		tasks: []beads.Task{
			{ID: "task-1", Title: "P0 task", Priority: 0},
			{ID: "task-2", Title: "P2 task", Priority: 2},
		},
		closed: make(map[string]bool),
	}

	o := &Orchestrator{
		config:       &Config{MaxPriority: 1}, // Only P0 and P1
		beadsClient:  mockClient,
		inFlight:     make(map[string]bool),
		inFlightTask: make(map[string]*beads.Task),
	}

	ctx := context.Background()

	// Should only get task-1 (P0)
	task1, _, err := o.getNextTask(ctx)
	if err != nil {
		t.Fatalf("getNextTask failed: %v", err)
	}
	if task1 == nil || task1.ID != "task-1" {
		t.Errorf("expected task-1, got %v", task1)
	}

	// Should get no more tasks (task-2 is P2, filtered out)
	task2, _, err := o.getNextTask(ctx)
	if err != nil {
		t.Fatalf("getNextTask failed: %v", err)
	}
	if task2 != nil {
		t.Errorf("expected no task (P2 filtered out), got %v", task2)
	}
}
