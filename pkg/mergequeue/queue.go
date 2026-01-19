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

	// pauseState manages pause state from user and resolver sources.
	// The queue only processes when in the Running state.
	pauseState *PauseStateMachine

	// closed indicates the queue has been shut down.
	closed atomic.Bool

	// mu protects coordinated state changes (used for Close).
	mu sync.Mutex

	// capacity is the maximum number of pending requests.
	capacity int
}

// NewQueue creates a new merge queue with the specified capacity.
func NewQueue(capacity int) *Queue {
	if capacity <= 0 {
		capacity = 100 // default capacity
	}
	return &Queue{
		requests:   make(chan *MergeRequest, capacity),
		pauseState: NewPauseStateMachine(),
		capacity:   capacity,
	}
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
// Blocks if the queue is paused (by user or agent), waiting until running.
// Returns nil if the queue is closed or the context is cancelled.
func (q *Queue) DequeueCtx(ctx context.Context) *MergeRequest {
	// Check context early
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	// Wait until queue is running (not paused by user or resolver)
	if err := q.pauseState.WaitUntilRunning(ctx); err != nil {
		return nil
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
	if !q.pauseState.IsRunning() || q.closed.Load() {
		return nil
	}

	select {
	case req := <-q.requests:
		return req
	default:
		return nil
	}
}

// Pause stops dequeue operations until Resume is called (user-initiated pause).
// This is used during conflict resolution to prevent concurrent merges.
func (q *Queue) Pause() {
	q.pauseState.UserPause()
}

// Resume allows dequeue operations to proceed (user-initiated resume).
// This is called after conflict resolution completes.
func (q *Queue) Resume() {
	q.pauseState.UserResume()
}

// AgentPause pauses the queue for agent operations (resolver, repair, etc.).
// This should be called before spawning an agent that needs exclusive access.
func (q *Queue) AgentPause() {
	q.pauseState.AgentPause()
}

// AgentResume resumes the queue after agent operations.
// This should be called after the agent completes.
func (q *Queue) AgentResume() {
	q.pauseState.AgentResume()
}

// IsAgentActive returns whether an agent is running
// (i.e., the queue is paused by an agent).
func (q *Queue) IsAgentActive() bool {
	return q.pauseState.IsPausedByAgent()
}

// IsPaused returns whether the queue is currently paused (by user or agent).
func (q *Queue) IsPaused() bool {
	return !q.pauseState.IsRunning()
}

// IsPausedByUser returns whether the queue is paused by a user request.
func (q *Queue) IsPausedByUser() bool {
	return q.pauseState.IsPausedByUser()
}

// PauseState returns the current pause state for detailed status reporting.
func (q *Queue) PauseState() PauseState {
	return q.pauseState.State()
}

// PauseStateString returns a string representation of the current pause state.
func (q *Queue) PauseStateString() string {
	return q.pauseState.State().String()
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
	// Resume to wake any goroutines blocked in WaitUntilRunning.
	// This ensures DequeueCtx calls return promptly on shutdown.
	q.pauseState.UserResume()
	q.pauseState.AgentResume()
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
