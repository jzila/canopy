package scheduler

import (
	"context"
	"sync"
)

// SlotManager provides bounded concurrency with dynamic resizing.
// Unlike golang.org/x/sync/semaphore, SlotManager supports changing the
// concurrency limit at runtime without dropping existing work.
type SlotManager struct {
	mu       sync.Mutex
	cond     *sync.Cond
	max      int // Maximum concurrent slots
	acquired int // Currently acquired slots
}

// NewSlotManager creates a new slot manager with the given initial concurrency.
func NewSlotManager(concurrency int) *SlotManager {
	if concurrency <= 0 {
		concurrency = 1
	}
	sm := &SlotManager{
		max:      concurrency,
		acquired: 0,
	}
	sm.cond = sync.NewCond(&sm.mu)
	return sm
}

// Acquire blocks until a slot is available or the context is cancelled.
// Returns nil on success, or the context error if cancelled while waiting.
func (sm *SlotManager) Acquire(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Wait until a slot is available or context is cancelled
	for sm.acquired >= sm.max {
		// Check context before waiting
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Use a done channel to wake up when context is cancelled
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				sm.cond.Broadcast() // Wake up all waiters so they can check context
			case <-done:
			}
		}()

		sm.cond.Wait()
		close(done)

		// Check context again after waking up
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	sm.acquired++
	return nil
}

// TryAcquire attempts to acquire a slot without blocking.
// Returns true if a slot was acquired, false otherwise.
func (sm *SlotManager) TryAcquire() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.acquired < sm.max {
		sm.acquired++
		return true
	}
	return false
}

// Release releases a slot back to the pool.
// Must be called exactly once for each successful Acquire/TryAcquire.
func (sm *SlotManager) Release() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.acquired > 0 {
		sm.acquired--
		sm.cond.Signal() // Wake one waiter
	}
}

// SetConcurrency dynamically updates the maximum concurrent slots.
// If the new limit is higher, waiting goroutines will be woken up.
// If the new limit is lower, currently acquired slots will continue
// until released (no preemption).
func (sm *SlotManager) SetConcurrency(concurrency int) {
	if concurrency <= 0 {
		concurrency = 1
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	oldMax := sm.max
	sm.max = concurrency

	// If we increased the limit, wake up all waiters so they can try to acquire
	if concurrency > oldMax {
		sm.cond.Broadcast()
	}
	// If we decreased, existing acquired slots will naturally drain
}

// Concurrency returns the current maximum concurrency setting.
func (sm *SlotManager) Concurrency() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.max
}

// Acquired returns the number of currently acquired slots.
func (sm *SlotManager) Acquired() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.acquired
}

// Available returns the number of available slots.
func (sm *SlotManager) Available() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	available := sm.max - sm.acquired
	if available < 0 {
		return 0
	}
	return available
}
