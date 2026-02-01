package beads

import (
	"context"
	"errors"
	"testing"
)

func TestMockClient_ImplementsInterface(t *testing.T) {
	// Verify MockClient implements BeadsClient at compile time
	var _ BeadsClient = (*MockClient)(nil)
	var _ BeadsClient = NewMockClient()
}

func TestMockClient_Ready(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()
	mock.SetReadyTasks([]Task{
		{ID: "task-1", Title: "Task 1"},
		{ID: "task-2", Title: "Task 2"},
	})

	tasks, err := mock.Ready(ctx)
	if err != nil {
		t.Fatalf("Ready() error = %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("Ready() got %d tasks, want 2", len(tasks))
	}

	if mock.Calls.Ready != 1 {
		t.Errorf("Ready() call count = %d, want 1", mock.Calls.Ready)
	}
}

func TestMockClient_ReadyError(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()
	expectedErr := errors.New("test error")
	mock.Errors.Ready = expectedErr

	_, err := mock.Ready(ctx)
	if err != expectedErr {
		t.Errorf("Ready() error = %v, want %v", err, expectedErr)
	}
}

func TestMockClient_Show(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()
	mock.SetTask(&Task{ID: "task-1", Title: "Test Task", Priority: 2})

	task, err := mock.Show(ctx, "task-1")
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}

	if task.ID != "task-1" {
		t.Errorf("Show() task.ID = %s, want task-1", task.ID)
	}
	if task.Title != "Test Task" {
		t.Errorf("Show() task.Title = %s, want Test Task", task.Title)
	}

	if len(mock.Calls.Show) != 1 || mock.Calls.Show[0] != "task-1" {
		t.Errorf("Show() calls = %v, want [task-1]", mock.Calls.Show)
	}
}

func TestMockClient_ShowNotFound(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()

	_, err := mock.Show(ctx, "nonexistent")
	if err == nil {
		t.Error("Show() expected error for nonexistent task")
	}
}

func TestMockClient_Done(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()
	mock.SetTask(&Task{ID: "task-1", Title: "Test", Status: "open"})

	err := mock.Done(ctx, "task-1")
	if err != nil {
		t.Fatalf("Done() error = %v", err)
	}

	if len(mock.Calls.Done) != 1 || mock.Calls.Done[0] != "task-1" {
		t.Errorf("Done() calls = %v, want [task-1]", mock.Calls.Done)
	}

	// Verify status changed
	if mock.Tasks["task-1"].Status != "closed" {
		t.Errorf("Task status = %s, want closed", mock.Tasks["task-1"].Status)
	}
}

func TestMockClient_Fail(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()
	mock.SetTask(&Task{ID: "task-1", Title: "Test", Status: "in_progress"})

	err := mock.Fail(ctx, "task-1", "something went wrong")
	if err != nil {
		t.Fatalf("Fail() error = %v", err)
	}

	if len(mock.Calls.Fail) != 1 {
		t.Fatalf("Fail() calls count = %d, want 1", len(mock.Calls.Fail))
	}
	if mock.Calls.Fail[0].TaskID != "task-1" {
		t.Errorf("Fail() taskID = %s, want task-1", mock.Calls.Fail[0].TaskID)
	}
	if mock.Calls.Fail[0].Reason != "something went wrong" {
		t.Errorf("Fail() reason = %s, want 'something went wrong'", mock.Calls.Fail[0].Reason)
	}

	// Verify status is reset to "open" so task can be retried
	if mock.Tasks["task-1"].Status != "open" {
		t.Errorf("Task status = %s, want open (failed tasks should be reopened for retry)", mock.Tasks["task-1"].Status)
	}
}

func TestMockClient_FailPermanently(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()
	mock.SetTask(&Task{ID: "task-1", Title: "Test", Status: "in_progress", Labels: []string{"bug"}})

	err := mock.FailPermanently(ctx, "task-1", "max retries exceeded")
	if err != nil {
		t.Fatalf("FailPermanently() error = %v", err)
	}

	if len(mock.Calls.FailPermanently) != 1 {
		t.Fatalf("FailPermanently() calls count = %d, want 1", len(mock.Calls.FailPermanently))
	}
	if mock.Calls.FailPermanently[0].TaskID != "task-1" {
		t.Errorf("FailPermanently() taskID = %s, want task-1", mock.Calls.FailPermanently[0].TaskID)
	}
	if mock.Calls.FailPermanently[0].Reason != "max retries exceeded" {
		t.Errorf("FailPermanently() reason = %s, want 'max retries exceeded'", mock.Calls.FailPermanently[0].Reason)
	}

	// Verify status remains unchanged (task stays open for investigation)
	if mock.Tasks["task-1"].Status != "in_progress" {
		t.Errorf("Task status = %s, want in_progress (permanently failed tasks should stay open)", mock.Tasks["task-1"].Status)
	}

	// Verify needs-investigation label was added
	hasLabel := false
	for _, label := range mock.Tasks["task-1"].Labels {
		if label == "needs-investigation" {
			hasLabel = true
			break
		}
	}
	if !hasLabel {
		t.Error("FailPermanently() should add needs-investigation label")
	}
}

func TestMockClient_Create(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()
	mock.NextCreateID = "created-task-1"

	id, err := mock.Create(ctx, "New Task", 1)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if id != "created-task-1" {
		t.Errorf("Create() id = %s, want created-task-1", id)
	}

	if len(mock.Calls.Create) != 1 {
		t.Fatalf("Create() calls count = %d, want 1", len(mock.Calls.Create))
	}
	if mock.Calls.Create[0].Title != "New Task" {
		t.Errorf("Create() title = %s, want 'New Task'", mock.Calls.Create[0].Title)
	}
	if mock.Calls.Create[0].Priority != 1 {
		t.Errorf("Create() priority = %d, want 1", mock.Calls.Create[0].Priority)
	}

	// Verify task was added
	if task, ok := mock.Tasks[id]; !ok || task.Title != "New Task" {
		t.Error("Created task not found in Tasks map")
	}
}

func TestMockClient_AddDep(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()
	mock.SetTask(&Task{ID: "child", Title: "Child"})
	mock.SetTask(&Task{ID: "parent", Title: "Parent"})

	err := mock.AddDep(ctx, "child", "parent")
	if err != nil {
		t.Fatalf("AddDep() error = %v", err)
	}

	if len(mock.Calls.AddDep) != 1 {
		t.Fatalf("AddDep() calls count = %d, want 1", len(mock.Calls.AddDep))
	}

	// Verify dependency was added
	if len(mock.Tasks["child"].Blockers) != 1 || mock.Tasks["child"].Blockers[0] != "parent" {
		t.Errorf("AddDep() blockers = %v, want [parent]", mock.Tasks["child"].Blockers)
	}
}

func TestMockClient_GetDeps(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()
	mock.SetTask(&Task{ID: "task-1", Title: "Task", Blockers: []string{"dep-1", "dep-2"}})

	deps, err := mock.GetDeps(ctx, "task-1")
	if err != nil {
		t.Fatalf("GetDeps() error = %v", err)
	}

	if len(deps) != 2 {
		t.Errorf("GetDeps() got %d deps, want 2", len(deps))
	}
}

func TestMockClient_Sync(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()

	err := mock.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if mock.Calls.Sync != 1 {
		t.Errorf("Sync() call count = %d, want 1", mock.Calls.Sync)
	}
}

func TestMockClient_Reset(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()
	mock.SetTask(&Task{ID: "task-1"})
	mock.SetReadyTasks([]Task{{ID: "task-1"}})

	// Make some calls
	_, _ = mock.Ready(ctx)
	_, _ = mock.Show(ctx, "task-1")
	_ = mock.Done(ctx, "task-1")
	mock.Errors.Ready = errors.New("test")

	// Reset
	mock.Reset()

	// Verify calls are cleared
	if mock.Calls.Ready != 0 {
		t.Errorf("Reset() Ready calls = %d, want 0", mock.Calls.Ready)
	}
	if len(mock.Calls.Show) != 0 {
		t.Errorf("Reset() Show calls = %d, want 0", len(mock.Calls.Show))
	}
	if len(mock.Calls.Done) != 0 {
		t.Errorf("Reset() Done calls = %d, want 0", len(mock.Calls.Done))
	}

	// Verify errors are cleared
	if mock.Errors.Ready != nil {
		t.Error("Reset() Ready error should be nil")
	}
}

func TestMockClient_AddComment(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()

	err := mock.AddComment(ctx, "task-1", "This is a test comment")
	if err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	if len(mock.Calls.AddComment) != 1 {
		t.Fatalf("AddComment() calls count = %d, want 1", len(mock.Calls.AddComment))
	}
	if mock.Calls.AddComment[0].TaskID != "task-1" {
		t.Errorf("AddComment() taskID = %s, want task-1", mock.Calls.AddComment[0].TaskID)
	}
	if mock.Calls.AddComment[0].Comment != "This is a test comment" {
		t.Errorf("AddComment() comment = %s, want 'This is a test comment'", mock.Calls.AddComment[0].Comment)
	}
}

func TestMockClient_AddCommentError(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()
	expectedErr := errors.New("comment error")
	mock.Errors.AddComment = expectedErr

	err := mock.AddComment(ctx, "task-1", "comment")
	if err != expectedErr {
		t.Errorf("AddComment() error = %v, want %v", err, expectedErr)
	}
}

func TestMockClient_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()
	mock.SetReadyTasks([]Task{{ID: "task-1"}})
	mock.SetTask(&Task{ID: "task-1", Title: "Task 1"})

	done := make(chan bool)

	// Run concurrent operations
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = mock.Ready(ctx)
			_, _ = mock.Show(ctx, "task-1")
			_ = mock.Done(ctx, "task-1")
			_ = mock.Sync(ctx)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify call counts
	if mock.Calls.Ready != 10 {
		t.Errorf("Ready() call count = %d, want 10", mock.Calls.Ready)
	}
	if mock.Calls.Sync != 10 {
		t.Errorf("Sync() call count = %d, want 10", mock.Calls.Sync)
	}
}
