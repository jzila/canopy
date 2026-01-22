package orchestrator

import (
	"context"
	"sync"
	"testing"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/scheduler"
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

func TestSetConcurrency_UpdatesSlotManager(t *testing.T) {
	o := &Orchestrator{
		config:       &Config{Concurrency: 4},
		slotManager:  scheduler.NewSlotManager(4),
		inFlight:     make(map[string]bool),
		inFlightTask: make(map[string]*beads.Task),
	}

	// Initial concurrency
	if got := o.slotManager.Concurrency(); got != 4 {
		t.Errorf("initial concurrency = %d, want 4", got)
	}

	// Increase concurrency
	o.SetConcurrency(8)
	if got := o.slotManager.Concurrency(); got != 8 {
		t.Errorf("after increase, concurrency = %d, want 8", got)
	}
	if got := o.config.Concurrency; got != 8 {
		t.Errorf("config.Concurrency = %d, want 8", got)
	}

	// Decrease concurrency
	o.SetConcurrency(2)
	if got := o.slotManager.Concurrency(); got != 2 {
		t.Errorf("after decrease, concurrency = %d, want 2", got)
	}
}

func TestSetConcurrency_NilSlotManager(t *testing.T) {
	// Test that SetConcurrency doesn't panic with nil slotManager
	o := &Orchestrator{
		config:       &Config{Concurrency: 4},
		slotManager:  nil, // Might happen in tests or edge cases
		inFlight:     make(map[string]bool),
		inFlightTask: make(map[string]*beads.Task),
	}

	// Should not panic
	o.SetConcurrency(8)

	// Config should still be updated
	if got := o.config.Concurrency; got != 8 {
		t.Errorf("config.Concurrency = %d, want 8", got)
	}
}

func TestSetConcurrency_DynamicAdjustmentUnblocksWaiters(t *testing.T) {
	o := &Orchestrator{
		config:       &Config{Concurrency: 1},
		slotManager:  scheduler.NewSlotManager(1),
		inFlight:     make(map[string]bool),
		inFlightTask: make(map[string]*beads.Task),
	}

	ctx := context.Background()

	// Acquire the only slot
	if err := o.slotManager.Acquire(ctx); err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Try to acquire another slot in a goroutine (will block)
	var acquired bool
	var acquireErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		acquireErr = o.slotManager.Acquire(ctx)
		if acquireErr == nil {
			acquired = true
		}
	}()

	// Give the goroutine time to block
	// Use a channel to synchronize
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Wait a bit to ensure the goroutine is blocked
	select {
	case <-done:
		// If we get here immediately, something went wrong
		if acquired && acquireErr == nil {
			t.Error("second acquire succeeded before concurrency increase")
		}
	default:
		// Good, goroutine is blocking as expected
	}

	// Increase concurrency - this should unblock the waiting goroutine
	o.SetConcurrency(2)

	// Wait for the goroutine to complete
	wg.Wait()

	if acquireErr != nil {
		t.Errorf("second acquire failed after concurrency increase: %v", acquireErr)
	}
	if !acquired {
		t.Error("second acquire did not succeed after concurrency increase")
	}
}
