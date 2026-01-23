package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSlotManager_BasicAcquireRelease(t *testing.T) {
	sm := NewSlotManager(2)

	// Acquire both slots
	ctx := context.Background()
	if err := sm.Acquire(ctx); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if err := sm.Acquire(ctx); err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}

	if sm.Acquired() != 2 {
		t.Errorf("expected 2 acquired, got %d", sm.Acquired())
	}
	if sm.Available() != 0 {
		t.Errorf("expected 0 available, got %d", sm.Available())
	}

	// Release one slot
	sm.Release()
	if sm.Acquired() != 1 {
		t.Errorf("expected 1 acquired after release, got %d", sm.Acquired())
	}
	if sm.Available() != 1 {
		t.Errorf("expected 1 available after release, got %d", sm.Available())
	}

	// Release the other
	sm.Release()
	if sm.Acquired() != 0 {
		t.Errorf("expected 0 acquired after both released, got %d", sm.Acquired())
	}
}

func TestSlotManager_TryAcquire(t *testing.T) {
	sm := NewSlotManager(1)

	// First try should succeed
	if !sm.TryAcquire() {
		t.Error("first TryAcquire should succeed")
	}

	// Second try should fail (no blocking)
	if sm.TryAcquire() {
		t.Error("second TryAcquire should fail when at capacity")
	}

	// After release, try should succeed
	sm.Release()
	if !sm.TryAcquire() {
		t.Error("TryAcquire should succeed after release")
	}
}

func TestSlotManager_AcquireBlocks(t *testing.T) {
	sm := NewSlotManager(1)
	ctx := context.Background()

	// Acquire the only slot
	if err := sm.Acquire(ctx); err != nil {
		t.Fatalf("initial acquire failed: %v", err)
	}

	// Start a goroutine that will block
	var acquired atomic.Bool
	go func() {
		if err := sm.Acquire(ctx); err == nil {
			acquired.Store(true)
		}
	}()

	// Give the goroutine time to block
	time.Sleep(50 * time.Millisecond)

	// Should not have acquired yet
	if acquired.Load() {
		t.Error("second acquire should be blocked")
	}

	// Release the slot
	sm.Release()

	// Wait for the blocked goroutine to acquire
	time.Sleep(50 * time.Millisecond)
	if !acquired.Load() {
		t.Error("second acquire should have succeeded after release")
	}
}

func TestSlotManager_ContextCancellation(t *testing.T) {
	sm := NewSlotManager(1)

	// Acquire the only slot
	if err := sm.Acquire(context.Background()); err != nil {
		t.Fatalf("initial acquire failed: %v", err)
	}

	// Try to acquire with a cancellable context
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := sm.Acquire(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestSlotManager_SetConcurrencyIncrease(t *testing.T) {
	sm := NewSlotManager(1)
	ctx := context.Background()

	// Acquire the only slot
	if err := sm.Acquire(ctx); err != nil {
		t.Fatalf("initial acquire failed: %v", err)
	}

	// Start a goroutine that will block
	var acquired atomic.Bool
	go func() {
		if err := sm.Acquire(ctx); err == nil {
			acquired.Store(true)
		}
	}()

	// Give the goroutine time to block
	time.Sleep(50 * time.Millisecond)

	// Should not have acquired yet
	if acquired.Load() {
		t.Error("second acquire should be blocked")
	}

	// Increase concurrency - this should allow the blocked goroutine to proceed
	sm.SetConcurrency(2)

	// Wait for the blocked goroutine to acquire
	time.Sleep(50 * time.Millisecond)
	if !acquired.Load() {
		t.Error("second acquire should have succeeded after concurrency increase")
	}

	if sm.Acquired() != 2 {
		t.Errorf("expected 2 acquired, got %d", sm.Acquired())
	}
}

func TestSlotManager_SetConcurrencyDecrease(t *testing.T) {
	sm := NewSlotManager(3)
	ctx := context.Background()

	// Acquire all slots
	for i := 0; i < 3; i++ {
		if err := sm.Acquire(ctx); err != nil {
			t.Fatalf("acquire %d failed: %v", i, err)
		}
	}

	// Decrease concurrency - existing slots should not be affected
	sm.SetConcurrency(1)

	if sm.Acquired() != 3 {
		t.Errorf("decreasing concurrency should not affect acquired slots, got %d", sm.Acquired())
	}
	if sm.Concurrency() != 1 {
		t.Errorf("expected concurrency 1, got %d", sm.Concurrency())
	}

	// New acquires should block until we're back below the new limit
	var acquired atomic.Bool
	go func() {
		if err := sm.Acquire(ctx); err == nil {
			acquired.Store(true)
		}
	}()

	time.Sleep(50 * time.Millisecond)
	if acquired.Load() {
		t.Error("new acquire should be blocked when over new limit")
	}

	// Release 3 slots to get below the new limit of 1
	sm.Release()
	sm.Release()
	sm.Release()

	time.Sleep(50 * time.Millisecond)
	if !acquired.Load() {
		t.Error("acquire should succeed once below new limit")
	}
}

func TestSlotManager_ConcurrentAccess(t *testing.T) {
	sm := NewSlotManager(5)
	ctx := context.Background()

	var wg sync.WaitGroup
	var maxConcurrent atomic.Int32
	var current atomic.Int32

	// Run 50 goroutines, each doing acquire/work/release
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := sm.Acquire(ctx); err != nil {
				t.Errorf("acquire failed: %v", err)
				return
			}

			// Track current concurrency
			cur := current.Add(1)
			for {
				max := maxConcurrent.Load()
				if cur <= max || maxConcurrent.CompareAndSwap(max, cur) {
					break
				}
			}

			// Simulate work
			time.Sleep(10 * time.Millisecond)

			current.Add(-1)
			sm.Release()
		}()
	}

	wg.Wait()

	if max := maxConcurrent.Load(); max > 5 {
		t.Errorf("max concurrent exceeded limit: got %d, want <= 5", max)
	}
}

func TestSlotManager_ZeroConcurrency(t *testing.T) {
	sm := NewSlotManager(0)
	if sm.Concurrency() != 1 {
		t.Errorf("zero concurrency should default to 1, got %d", sm.Concurrency())
	}
}

func TestSlotManager_NegativeConcurrency(t *testing.T) {
	sm := NewSlotManager(-5)
	if sm.Concurrency() != 1 {
		t.Errorf("negative concurrency should default to 1, got %d", sm.Concurrency())
	}
}

func TestSlotManager_SetConcurrencyZero(t *testing.T) {
	sm := NewSlotManager(5)
	sm.SetConcurrency(0)
	if sm.Concurrency() != 1 {
		t.Errorf("SetConcurrency(0) should default to 1, got %d", sm.Concurrency())
	}
}

func TestSlotManager_ReleaseWithoutAcquire(t *testing.T) {
	sm := NewSlotManager(2)

	// Release without acquire should not go negative
	sm.Release()
	sm.Release()
	sm.Release()

	if sm.Acquired() != 0 {
		t.Errorf("acquired should not go negative, got %d", sm.Acquired())
	}
}

func TestSlotManager_Available(t *testing.T) {
	sm := NewSlotManager(3)

	if sm.Available() != 3 {
		t.Errorf("expected 3 available initially, got %d", sm.Available())
	}

	_ = sm.Acquire(context.Background()) // Error intentionally ignored for test
	if sm.Available() != 2 {
		t.Errorf("expected 2 available after 1 acquire, got %d", sm.Available())
	}

	// Decrease concurrency below current acquired
	sm.SetConcurrency(1)
	if sm.Available() != 0 {
		t.Errorf("expected 0 available when acquired > max, got %d", sm.Available())
	}
}
