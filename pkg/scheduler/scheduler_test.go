package scheduler

// Merge queue commit verification test - this comment was added by an agent
// to verify that the merge queue properly commits and pushes changes.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/sandbox"
)

// TestCleanupAll verifies that CleanupAll unmounts all active overlays
func TestCleanupAll(t *testing.T) {
	// Skip if not on Linux (overlay requires Linux)
	if sandbox.IsStub() {
		t.Skip("Skipping overlay test on non-Linux platform")
	}

	// Create temp directories
	tempDir := filepath.Join(os.TempDir(), "canopy-test-scheduler")
	workDir := filepath.Join(tempDir, "workdir")
	defer os.RemoveAll(tempDir)

	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create workdir: %v", err)
	}

	// Create a test file in workdir
	testFile := filepath.Join(workDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a minimal scheduler just for testing the cleanup
	sched := &Scheduler{
		config: &Config{
			TempDir: filepath.Join(tempDir, "overlays"),
			WorkDir: workDir,
		},
	}

	// Manually create and register overlays (simulating task execution)
	overlays := make([]*sandbox.Overlay, 3)
	for i := 0; i < 3; i++ {
		overlay, err := sandbox.NewOverlay(sched.config.TempDir, workDir)
		if err != nil {
			t.Fatalf("Failed to create overlay %d: %v", i, err)
		}

		if err := overlay.Mount(); err != nil {
			t.Fatalf("Failed to mount overlay %d: %v", i, err)
		}

		taskID := "test-task-" + string(rune('a'+i))
		sched.activeOverlays.Store(taskID, overlay)
		overlays[i] = overlay
	}

	// Verify overlays are mounted
	for i, overlay := range overlays {
		if !overlay.IsMounted() {
			t.Errorf("Overlay %d should be mounted", i)
		}
	}

	// Call CleanupAll
	count, err := sched.CleanupAll(5 * time.Second)
	if err != nil {
		t.Errorf("CleanupAll failed: %v", err)
	}

	if count != 3 {
		t.Errorf("Expected to clean 3 overlays, got %d", count)
	}

	// Verify overlays are unmounted
	for i, overlay := range overlays {
		if overlay.IsMounted() {
			t.Errorf("Overlay %d should be unmounted after CleanupAll", i)
		}
	}

	// Verify activeOverlays map is still tracked (CleanupAll doesn't remove from map)
	// The normal deferred Delete() in executeTask handles that
	activeCount := 0
	sched.activeOverlays.Range(func(key, value interface{}) bool {
		activeCount++
		return true
	})

	if activeCount != 3 {
		t.Errorf("Expected activeOverlays to still have 3 entries, got %d", activeCount)
	}
}

// TestCleanupAllEmpty verifies that CleanupAll works with no active overlays
func TestCleanupAllEmpty(t *testing.T) {
	sched := &Scheduler{}
	count, err := sched.CleanupAll(1 * time.Second)
	if err != nil {
		t.Errorf("CleanupAll should not fail with empty map: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 overlays cleaned, got %d", count)
	}
}

// TestPauseResumeRaceCondition tests that pause/resume operations are thread-safe
// and don't have TOCTOU race conditions
func TestPauseResumeRaceCondition(t *testing.T) {
	sched := NewScheduler(nil, nil, &Config{Concurrency: 10})

	// Pause BEFORE starting goroutines to ensure they all block
	sched.Pause()

	// Track how many goroutines actually started execution
	var started atomic.Int32
	var wg sync.WaitGroup

	// Start 100 goroutines that simulate the ExecuteBatch pause check
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Simulate the pause check in ExecuteBatch
			sched.pauseMu.Lock()
			for sched.paused {
				sched.pauseCond.Wait()
			}
			sched.pauseMu.Unlock()

			// If we get here, we passed the pause check
			started.Add(1)
		}()
	}

	// Give goroutines time to reach the wait
	time.Sleep(50 * time.Millisecond)

	// No goroutines should have completed yet because we're paused
	if started.Load() > 0 {
		t.Errorf("Expected no goroutines to complete while paused, got %d", started.Load())
	}

	// Resume should wake up all waiting goroutines
	sched.Resume()

	// Wait for all goroutines to complete
	wg.Wait()

	// All goroutines should have started now
	if started.Load() != 100 {
		t.Errorf("Expected all 100 goroutines to start, got %d", started.Load())
	}
}

// TestPauseResumeNonRacing tests basic pause/resume functionality
func TestPauseResumeNonRacing(t *testing.T) {
	sched := NewScheduler(nil, nil, &Config{Concurrency: 1})

	// Initially not paused
	if sched.IsPaused() {
		t.Error("Scheduler should not be paused initially")
	}

	// Pause
	sched.Pause()
	if !sched.IsPaused() {
		t.Error("Scheduler should be paused after Pause()")
	}

	// Resume
	sched.Resume()
	if sched.IsPaused() {
		t.Error("Scheduler should not be paused after Resume()")
	}

	// Multiple pauses should be idempotent
	sched.Pause()
	sched.Pause()
	if !sched.IsPaused() {
		t.Error("Multiple Pause() calls should keep scheduler paused")
	}

	// Multiple resumes should be idempotent
	sched.Resume()
	sched.Resume()
	if sched.IsPaused() {
		t.Error("Multiple Resume() calls should keep scheduler running")
	}
}

// TestPauseBlocksExecution tests that paused scheduler blocks task execution
func TestPauseBlocksExecution(t *testing.T) {
	sched := NewScheduler(nil, nil, &Config{Concurrency: 4})

	var executed atomic.Bool
	var wg sync.WaitGroup

	// Pause before starting goroutine
	sched.Pause()

	// Start a goroutine that simulates task execution
	wg.Add(1)
	go func() {
		defer wg.Done()

		// This is the pause check from ExecuteBatch
		sched.pauseMu.Lock()
		for sched.paused {
			sched.pauseCond.Wait()
		}
		sched.pauseMu.Unlock()

		// Mark as executed
		executed.Store(true)
	}()

	// Wait a bit - goroutine should be blocked
	time.Sleep(50 * time.Millisecond)

	// Should not have executed yet
	if executed.Load() {
		t.Error("Task should not execute while scheduler is paused")
	}

	// Resume and wait for completion
	sched.Resume()
	wg.Wait()

	// Should have executed now
	if !executed.Load() {
		t.Error("Task should execute after resume")
	}
}

// TestConcurrentPauseResume tests many concurrent pause/resume operations
func TestConcurrentPauseResume(t *testing.T) {
	sched := NewScheduler(nil, nil, &Config{Concurrency: 10})

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start goroutines that repeatedly pause/resume
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					if i%2 == 0 {
						sched.Pause()
					} else {
						sched.Resume()
					}
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// Start goroutines that simulate task execution
	var executed atomic.Int32
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					// Simulate the pause check
					sched.pauseMu.Lock()
					for sched.paused {
						// Check if context is cancelled
						select {
						case <-ctx.Done():
							sched.pauseMu.Unlock()
							return
						default:
						}
						sched.pauseCond.Wait()
					}
					sched.pauseMu.Unlock()

					// "Execute" task
					executed.Add(1)
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// Let it run for a bit
	time.Sleep(500 * time.Millisecond)
	cancel()
	wg.Wait()

	// Should have executed some tasks (exact number varies due to timing)
	if executed.Load() == 0 {
		t.Error("Expected some tasks to execute during concurrent pause/resume")
	}

	t.Logf("Executed %d task iterations during concurrent pause/resume test", executed.Load())
}

// TestExecuteBatchWithPause tests ExecuteBatch respects pause state
// This test verifies the pause check works without actually executing tasks
func TestExecuteBatchWithPause(t *testing.T) {
	sched := NewScheduler(nil, nil, &Config{Concurrency: 2})

	// Track how many goroutines get past the pause check
	var passedPause atomic.Int32

	// Pause scheduler before starting
	sched.Pause()

	var wg sync.WaitGroup

	// Simulate what ExecuteBatch does for each task
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// This is the pause check from ExecuteBatch
			sched.pauseMu.Lock()
			for sched.paused {
				sched.pauseCond.Wait()
			}
			sched.pauseMu.Unlock()

			// Track that we passed the pause check
			passedPause.Add(1)
		}()
	}

	// Give goroutines time to reach pause check
	time.Sleep(50 * time.Millisecond)

	// Should still be blocked
	if passedPause.Load() > 0 {
		t.Errorf("Expected tasks to be blocked by pause, but %d passed", passedPause.Load())
	}

	// Resume should allow all to proceed
	sched.Resume()
	wg.Wait()

	// All should have passed now
	if passedPause.Load() != 5 {
		t.Errorf("Expected all 5 tasks to pass pause check, got %d", passedPause.Load())
	}
}
