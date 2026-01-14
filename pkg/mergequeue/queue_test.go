package mergequeue

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
)

func TestNewQueue(t *testing.T) {
	q := NewQueue(10)
	if q == nil {
		t.Fatal("NewQueue returned nil")
	}
	if q.Len() != 0 {
		t.Errorf("expected empty queue, got len=%d", q.Len())
	}
	if q.IsPaused() {
		t.Error("new queue should not be paused")
	}
	if q.IsClosed() {
		t.Error("new queue should not be closed")
	}
}

func TestNewQueueDefaultCapacity(t *testing.T) {
	q := NewQueue(0)
	if q.capacity != 100 {
		t.Errorf("expected default capacity 100, got %d", q.capacity)
	}

	q = NewQueue(-1)
	if q.capacity != 100 {
		t.Errorf("expected default capacity 100 for negative, got %d", q.capacity)
	}
}

func TestEnqueueDequeue(t *testing.T) {
	q := NewQueue(10)

	req := NewMergeRequest(&agent.Result{TaskID: "task-1"}, &beads.Task{ID: "task-1"})
	if !q.Enqueue(req) {
		t.Fatal("Enqueue failed")
	}

	if q.Len() != 1 {
		t.Errorf("expected len=1, got %d", q.Len())
	}

	got := q.TryDequeue()
	if got == nil {
		t.Fatal("TryDequeue returned nil")
	}
	if got.Result.TaskID != "task-1" {
		t.Errorf("expected task-1, got %s", got.Result.TaskID)
	}

	if q.Len() != 0 {
		t.Errorf("expected empty queue after dequeue, got len=%d", q.Len())
	}
}

func TestFIFOOrder(t *testing.T) {
	q := NewQueue(10)

	// Enqueue in order
	for i := 0; i < 5; i++ {
		taskID := string(rune('a' + i))
		req := NewMergeRequest(&agent.Result{TaskID: taskID}, &beads.Task{ID: taskID})
		if !q.Enqueue(req) {
			t.Fatalf("Enqueue failed for task %s", taskID)
		}
	}

	// Dequeue should be FIFO
	expected := []string{"a", "b", "c", "d", "e"}
	for _, exp := range expected {
		got := q.TryDequeue()
		if got == nil {
			t.Fatal("TryDequeue returned nil")
		}
		if got.Result.TaskID != exp {
			t.Errorf("expected %s, got %s", exp, got.Result.TaskID)
		}
	}
}

func TestEnqueueFullQueue(t *testing.T) {
	q := NewQueue(2)

	req1 := NewMergeRequest(&agent.Result{TaskID: "1"}, nil)
	req2 := NewMergeRequest(&agent.Result{TaskID: "2"}, nil)
	req3 := NewMergeRequest(&agent.Result{TaskID: "3"}, nil)

	if !q.Enqueue(req1) {
		t.Fatal("first enqueue should succeed")
	}
	if !q.Enqueue(req2) {
		t.Fatal("second enqueue should succeed")
	}
	if q.Enqueue(req3) {
		t.Error("third enqueue should fail - queue full")
	}
}

func TestEnqueueClosedQueue(t *testing.T) {
	q := NewQueue(10)
	q.Close()

	req := NewMergeRequest(&agent.Result{TaskID: "1"}, nil)
	if q.Enqueue(req) {
		t.Error("enqueue to closed queue should fail")
	}
}

func TestTryDequeueEmpty(t *testing.T) {
	q := NewQueue(10)
	if q.TryDequeue() != nil {
		t.Error("TryDequeue on empty queue should return nil")
	}
}

func TestTryDequeuePaused(t *testing.T) {
	q := NewQueue(10)
	req := NewMergeRequest(&agent.Result{TaskID: "1"}, nil)
	q.Enqueue(req)

	q.Pause()
	if q.TryDequeue() != nil {
		t.Error("TryDequeue on paused queue should return nil")
	}

	q.Resume()
	got := q.TryDequeue()
	if got == nil {
		t.Error("TryDequeue after resume should succeed")
	}
}

func TestPauseResume(t *testing.T) {
	q := NewQueue(10)

	if q.IsPaused() {
		t.Error("new queue should not be paused")
	}

	q.Pause()
	if !q.IsPaused() {
		t.Error("queue should be paused after Pause()")
	}

	q.Resume()
	if q.IsPaused() {
		t.Error("queue should not be paused after Resume()")
	}
}

func TestDequeueBlocksWhenPaused(t *testing.T) {
	q := NewQueue(10)
	req := NewMergeRequest(&agent.Result{TaskID: "1"}, nil)
	q.Enqueue(req)

	q.Pause()

	done := make(chan *MergeRequest)
	go func() {
		done <- q.Dequeue()
	}()

	// Give time for goroutine to block
	time.Sleep(50 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("Dequeue should block when paused")
	default:
		// Expected: still blocked
	}

	q.Resume()

	select {
	case got := <-done:
		if got == nil || got.Result.TaskID != "1" {
			t.Errorf("expected task-1, got %v", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Dequeue should unblock after Resume")
	}
}

func TestDequeueReturnsNilWhenClosed(t *testing.T) {
	q := NewQueue(10)

	done := make(chan *MergeRequest)
	go func() {
		done <- q.Dequeue()
	}()

	// Give time for goroutine to start blocking
	time.Sleep(50 * time.Millisecond)

	q.Close()

	select {
	case got := <-done:
		if got != nil {
			t.Error("Dequeue on closed queue should return nil")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Dequeue should return after Close")
	}
}

func TestCloseUnblocksPausedDequeue(t *testing.T) {
	q := NewQueue(10)
	q.Pause()

	done := make(chan *MergeRequest)
	go func() {
		done <- q.Dequeue()
	}()

	time.Sleep(50 * time.Millisecond)

	q.Close()

	select {
	case got := <-done:
		if got != nil {
			t.Error("Dequeue should return nil after Close")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Dequeue should unblock after Close")
	}
}

func TestCloseDrainsPendingRequests(t *testing.T) {
	q := NewQueue(10)

	req1 := NewMergeRequest(&agent.Result{TaskID: "1"}, nil)
	req2 := NewMergeRequest(&agent.Result{TaskID: "2"}, nil)
	q.Enqueue(req1)
	q.Enqueue(req2)

	q.Close()

	// Both requests should receive error responses
	select {
	case resp := <-req1.Response:
		if resp.Success {
			t.Error("drained request should have Success=false")
		}
		if resp.Error != "merge queue closed" {
			t.Errorf("expected 'merge queue closed', got %s", resp.Error)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("req1 should receive response on close")
	}

	select {
	case resp := <-req2.Response:
		if resp.Success {
			t.Error("drained request should have Success=false")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("req2 should receive response on close")
	}
}

func TestDoubleClose(t *testing.T) {
	q := NewQueue(10)
	q.Close()
	q.Close() // Should not panic
}

func TestResolverActive(t *testing.T) {
	q := NewQueue(10)

	if q.IsResolverActive() {
		t.Error("resolver should not be active initially")
	}

	q.SetResolverActive(true)
	if !q.IsResolverActive() {
		t.Error("resolver should be active after SetResolverActive(true)")
	}

	q.SetResolverActive(false)
	if q.IsResolverActive() {
		t.Error("resolver should not be active after SetResolverActive(false)")
	}
}

func TestConcurrentEnqueue(t *testing.T) {
	q := NewQueue(100)
	var wg sync.WaitGroup

	// Concurrent enqueues
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			taskID := string(rune('0' + id%10))
			req := NewMergeRequest(&agent.Result{TaskID: taskID}, nil)
			q.Enqueue(req)
		}(i)
	}

	wg.Wait()

	if q.Len() != 50 {
		t.Errorf("expected 50 items, got %d", q.Len())
	}
}

func TestConcurrentEnqueueDequeue(t *testing.T) {
	q := NewQueue(100)
	var wg sync.WaitGroup
	var dequeueCount int32

	// Enqueue items first
	for i := 0; i < 20; i++ {
		req := NewMergeRequest(&agent.Result{TaskID: "task"}, nil)
		if !q.Enqueue(req) {
			t.Fatal("Enqueue failed")
		}
	}

	// Start concurrent dequeuers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if req := q.TryDequeue(); req != nil {
					atomic.AddInt32(&dequeueCount, 1)
				} else {
					return
				}
			}
		}()
	}

	wg.Wait()

	if dequeueCount != 20 {
		t.Errorf("expected 20 dequeued items, got %d", dequeueCount)
	}
}

func TestNewMergeRequest(t *testing.T) {
	result := &agent.Result{TaskID: "test-task"}
	task := &beads.Task{ID: "test-task", Title: "Test"}

	before := time.Now()
	req := NewMergeRequest(result, task)
	after := time.Now()

	if req.Result != result {
		t.Error("Result not set correctly")
	}
	if req.Task != task {
		t.Error("Task not set correctly")
	}
	if req.Response == nil {
		t.Error("Response channel should be initialized")
	}
	if req.EnqueuedAt.Before(before) || req.EnqueuedAt.After(after) {
		t.Error("EnqueuedAt should be set to current time")
	}
}

func TestDequeueCtxCancellation(t *testing.T) {
	q := NewQueue(10)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *MergeRequest)

	go func() {
		done <- q.DequeueCtx(ctx)
	}()

	// Give time for goroutine to start blocking
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	select {
	case got := <-done:
		if got != nil {
			t.Error("DequeueCtx should return nil when context is cancelled")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("DequeueCtx should return after context cancellation")
	}
}

func TestDequeueCtxCancelledBeforeCall(t *testing.T) {
	q := NewQueue(10)
	req := NewMergeRequest(&agent.Result{TaskID: "1"}, nil)
	q.Enqueue(req)

	// Cancel context before calling DequeueCtx
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := q.DequeueCtx(ctx)
	if got != nil {
		t.Error("DequeueCtx should return nil for already-cancelled context")
	}

	// Original request should still be in queue
	if q.Len() != 1 {
		t.Errorf("expected queue len=1, got %d", q.Len())
	}
}

func TestDequeueCtxSuccessBeforeCancellation(t *testing.T) {
	q := NewQueue(10)
	req := NewMergeRequest(&agent.Result{TaskID: "test"}, nil)
	q.Enqueue(req)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := q.DequeueCtx(ctx)
	if got == nil {
		t.Fatal("DequeueCtx should return the request")
	}
	if got.Result.TaskID != "test" {
		t.Errorf("expected task 'test', got %s", got.Result.TaskID)
	}
}

func TestDequeueCtxCancelWhilePaused(t *testing.T) {
	q := NewQueue(10)
	req := NewMergeRequest(&agent.Result{TaskID: "1"}, nil)
	q.Enqueue(req)

	q.Pause()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *MergeRequest)

	go func() {
		done <- q.DequeueCtx(ctx)
	}()

	// Give time for goroutine to start blocking on pause
	time.Sleep(50 * time.Millisecond)

	// Cancel while paused
	cancel()

	select {
	case got := <-done:
		if got != nil {
			t.Error("DequeueCtx should return nil when cancelled while paused")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("DequeueCtx should return after cancellation while paused")
	}

	// Request should still be in queue
	if q.Len() != 1 {
		t.Errorf("expected queue len=1, got %d", q.Len())
	}
}

func TestDequeueCtxWithTimeout(t *testing.T) {
	q := NewQueue(10)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	got := q.DequeueCtx(ctx)
	elapsed := time.Since(start)

	if got != nil {
		t.Error("DequeueCtx should return nil on timeout")
	}

	// Should have waited approximately the timeout duration
	if elapsed < 40*time.Millisecond || elapsed > 150*time.Millisecond {
		t.Errorf("expected ~50ms wait, got %v", elapsed)
	}
}
