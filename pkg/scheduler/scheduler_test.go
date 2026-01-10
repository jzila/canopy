package scheduler

import (
	"os"
	"path/filepath"
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
