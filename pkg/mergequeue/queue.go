package mergequeue

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/jzila/canopy/pkg/metrics"
)

// Queue manages sequential merge operations for concurrent agents.
// It ensures only one merge happens at a time while allowing the queue
// to be paused during conflict resolution.
type Queue struct {
	// requests is the buffered channel holding pending merge requests.
	requests chan *MergeRequest

	// paused indicates whether dequeue operations should block.
	// This is set during conflict resolution to prevent new merges.
	paused atomic.Bool

	// resolverActive indicates whether a resolver agent is running.
	resolverActive atomic.Bool

	// closed indicates the queue has been shut down.
	closed atomic.Bool

	// mu protects coordinated state changes.
	mu sync.Mutex

	// pauseCond is used to signal when the queue is resumed.
	pauseCond *sync.Cond

	// capacity is the maximum number of pending requests.
	capacity int
}

// NewQueue creates a new merge queue with the specified capacity.
func NewQueue(capacity int) *Queue {
	if capacity <= 0 {
		capacity = 100 // default capacity
	}
	q := &Queue{
		requests: make(chan *MergeRequest, capacity),
		capacity: capacity,
	}
	q.pauseCond = sync.NewCond(&q.mu)
	return q
}

// Enqueue adds a merge request to the queue.
// Returns false if the queue is closed or full.
func (q *Queue) Enqueue(req *MergeRequest) bool {
	if q.closed.Load() {
		return false
	}

	select {
	case q.requests <- req:
		metrics.SetMergeQueueDepth(len(q.requests))
		return true
	default:
		// Queue is full
		return false
	}
}

// Dequeue retrieves the next merge request from the queue.
// Blocks if the queue is paused, waiting until Resume() is called.
// Returns nil if the queue is closed.
func (q *Queue) Dequeue() *MergeRequest {
	return q.DequeueCtx(context.Background())
}

// DequeueCtx retrieves the next merge request from the queue with context support.
// Blocks if the queue is paused, waiting until Resume() is called.
// Returns nil if the queue is closed or the context is cancelled.
func (q *Queue) DequeueCtx(ctx context.Context) *MergeRequest {
	// Check context early
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	// Wait if paused, with context cancellation support
	for {
		q.mu.Lock()
		if !q.paused.Load() || q.closed.Load() {
			q.mu.Unlock()
			break
		}

		// Create a channel that will be closed when condition is signaled
		// This allows us to select on both the condition and the context
		waitCh := make(chan struct{})
		go func() {
			q.mu.Lock()
			q.pauseCond.Wait()
			q.mu.Unlock()
			close(waitCh)
		}()
		q.mu.Unlock()

		select {
		case <-ctx.Done():
			// Context cancelled - broadcast to wake the waiting goroutine
			q.mu.Lock()
			q.pauseCond.Broadcast()
			q.mu.Unlock()
			<-waitCh // Wait for goroutine to complete
			return nil
		case <-waitCh:
			// Condition was signaled, loop to check if still paused
		}
	}

	if q.closed.Load() {
		return nil
	}

	select {
	case <-ctx.Done():
		return nil
	case req, ok := <-q.requests:
		if !ok {
			return nil
		}
		metrics.SetMergeQueueDepth(len(q.requests))
		return req
	}
}

// TryDequeue attempts to retrieve a merge request without blocking.
// Returns nil immediately if no request is available or if paused.
func (q *Queue) TryDequeue() *MergeRequest {
	if q.paused.Load() || q.closed.Load() {
		return nil
	}

	select {
	case req := <-q.requests:
		return req
	default:
		return nil
	}
}

// Pause stops dequeue operations until Resume is called.
// This is used during conflict resolution to prevent concurrent merges.
func (q *Queue) Pause() {
	q.paused.Store(true)
}

// Resume allows dequeue operations to proceed.
// This is called after conflict resolution completes.
func (q *Queue) Resume() {
	q.mu.Lock()
	q.paused.Store(false)
	q.pauseCond.Broadcast()
	q.mu.Unlock()
}

// SetResolverActive marks whether a resolver agent is currently running.
func (q *Queue) SetResolverActive(active bool) {
	q.resolverActive.Store(active)
}

// IsResolverActive returns whether a resolver agent is running.
func (q *Queue) IsResolverActive() bool {
	return q.resolverActive.Load()
}

// IsPaused returns whether the queue is currently paused.
func (q *Queue) IsPaused() bool {
	return q.paused.Load()
}

// IsClosed returns whether the queue has been closed.
func (q *Queue) IsClosed() bool {
	return q.closed.Load()
}

// Len returns the current number of pending requests.
func (q *Queue) Len() int {
	return len(q.requests)
}

// Close shuts down the queue and wakes any blocked Dequeue calls.
// Pending requests in the queue are drained and their response channels
// receive error responses.
func (q *Queue) Close() {
	q.mu.Lock()
	if q.closed.Load() {
		q.mu.Unlock()
		return
	}
	q.closed.Store(true)
	close(q.requests)
	q.pauseCond.Broadcast()
	q.mu.Unlock()

	// Drain remaining requests and send error responses
	for req := range q.requests {
		if req.Response != nil {
			select {
			case req.Response <- &MergeResponse{
				Success: false,
				Error:   "merge queue closed",
			}:
			default:
			}
		}
	}
}
