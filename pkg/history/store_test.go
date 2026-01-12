package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_SaveAndGet(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "canopy-history-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStoreWithDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create a run record
	run := &RunRecord{
		ID:              "test-run-123",
		StartTime:       time.Now().Add(-10 * time.Minute),
		EndTime:         time.Now(),
		Status:          RunStatusCompleted,
		WorkDir:         "/tmp/test",
		TotalTasks:      5,
		CompletedTasks:  4,
		FailedTasks:     1,
		DurationSeconds: 600.0,
		TotalInputTokens:  100000,
		TotalOutputTokens: 50000,
		TotalCostUSD:      0.45,
		Tasks: []TaskRecord{
			{
				ID:           "task-1",
				Title:        "Test Task 1",
				Status:       "completed",
				DurationMS:   120000,
				InputTokens:  20000,
				OutputTokens: 10000,
				CostUSD:      0.09,
			},
		},
	}

	// Save
	if err := store.Save(run); err != nil {
		t.Fatalf("failed to save run: %v", err)
	}

	// Verify file exists
	expectedPath := filepath.Join(tmpDir, "history", "run-test-run-123.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("run file not created at %s", expectedPath)
	}

	// Get
	retrieved, err := store.Get("test-run-123")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}

	// Verify fields
	if retrieved.ID != run.ID {
		t.Errorf("ID mismatch: got %s, want %s", retrieved.ID, run.ID)
	}
	if retrieved.Status != run.Status {
		t.Errorf("Status mismatch: got %s, want %s", retrieved.Status, run.Status)
	}
	if retrieved.TotalTasks != run.TotalTasks {
		t.Errorf("TotalTasks mismatch: got %d, want %d", retrieved.TotalTasks, run.TotalTasks)
	}
	if retrieved.TotalCostUSD != run.TotalCostUSD {
		t.Errorf("TotalCostUSD mismatch: got %f, want %f", retrieved.TotalCostUSD, run.TotalCostUSD)
	}
	if len(retrieved.Tasks) != len(run.Tasks) {
		t.Errorf("Tasks count mismatch: got %d, want %d", len(retrieved.Tasks), len(run.Tasks))
	}
}

func TestStore_List(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "canopy-history-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStoreWithDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create multiple runs
	now := time.Now()
	runs := []*RunRecord{
		{
			ID:        "run-1",
			StartTime: now.Add(-3 * time.Hour),
			EndTime:   now.Add(-2 * time.Hour),
			Status:    RunStatusCompleted,
		},
		{
			ID:        "run-2",
			StartTime: now.Add(-2 * time.Hour),
			EndTime:   now.Add(-1 * time.Hour),
			Status:    RunStatusFailed,
		},
		{
			ID:        "run-3",
			StartTime: now.Add(-1 * time.Hour),
			EndTime:   now,
			Status:    RunStatusCompleted,
		},
	}

	for _, run := range runs {
		if err := store.Save(run); err != nil {
			t.Fatalf("failed to save run: %v", err)
		}
	}

	// List all
	allRuns, err := store.List(ListOptions{})
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}
	if len(allRuns) != 3 {
		t.Errorf("expected 3 runs, got %d", len(allRuns))
	}

	// Verify sorted by start time (newest first)
	if allRuns[0].ID != "run-3" {
		t.Errorf("expected newest run first, got %s", allRuns[0].ID)
	}

	// List with limit
	limitedRuns, err := store.List(ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("failed to list runs with limit: %v", err)
	}
	if len(limitedRuns) != 2 {
		t.Errorf("expected 2 runs, got %d", len(limitedRuns))
	}

	// List with status filter
	failedRuns, err := store.List(ListOptions{Status: RunStatusFailed})
	if err != nil {
		t.Fatalf("failed to list failed runs: %v", err)
	}
	if len(failedRuns) != 1 {
		t.Errorf("expected 1 failed run, got %d", len(failedRuns))
	}
	if failedRuns[0].ID != "run-2" {
		t.Errorf("expected run-2 as failed run, got %s", failedRuns[0].ID)
	}

	// List with since filter
	recentRuns, err := store.List(ListOptions{Since: now.Add(-90 * time.Minute)})
	if err != nil {
		t.Fatalf("failed to list recent runs: %v", err)
	}
	if len(recentRuns) != 1 {
		t.Errorf("expected 1 recent run, got %d", len(recentRuns))
	}
}

func TestStore_Stats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "canopy-history-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStoreWithDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create runs
	runs := []*RunRecord{
		{
			ID:                "run-1",
			StartTime:         time.Now().Add(-1 * time.Hour),
			Status:            RunStatusCompleted,
			TotalTasks:        3,
			CompletedTasks:    3,
			TotalInputTokens:  50000,
			TotalOutputTokens: 25000,
			TotalCostUSD:      0.20,
			DurationSeconds:   300,
		},
		{
			ID:                "run-2",
			StartTime:         time.Now().Add(-30 * time.Minute),
			Status:            RunStatusFailed,
			TotalTasks:        2,
			CompletedTasks:    0,
			FailedTasks:       2,
			TotalInputTokens:  30000,
			TotalOutputTokens: 15000,
			TotalCostUSD:      0.15,
			DurationSeconds:   200,
		},
		{
			ID:                "run-3",
			StartTime:         time.Now(),
			Status:            RunStatusPartial,
			TotalTasks:        4,
			CompletedTasks:    2,
			FailedTasks:       2,
			TotalInputTokens:  40000,
			TotalOutputTokens: 20000,
			TotalCostUSD:      0.18,
			DurationSeconds:   400,
		},
	}

	for _, run := range runs {
		if err := store.Save(run); err != nil {
			t.Fatalf("failed to save run: %v", err)
		}
	}

	// Get aggregate stats
	stats, err := store.Stats(ListOptions{})
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if stats.TotalRuns != 3 {
		t.Errorf("expected 3 total runs, got %d", stats.TotalRuns)
	}
	if stats.CompletedRuns != 1 {
		t.Errorf("expected 1 completed run, got %d", stats.CompletedRuns)
	}
	if stats.FailedRuns != 1 {
		t.Errorf("expected 1 failed run, got %d", stats.FailedRuns)
	}
	if stats.PartialRuns != 1 {
		t.Errorf("expected 1 partial run, got %d", stats.PartialRuns)
	}
	if stats.TotalTasks != 9 {
		t.Errorf("expected 9 total tasks, got %d", stats.TotalTasks)
	}
	if stats.CompletedTasks != 5 {
		t.Errorf("expected 5 completed tasks, got %d", stats.CompletedTasks)
	}
	if stats.FailedTasks != 4 {
		t.Errorf("expected 4 failed tasks, got %d", stats.FailedTasks)
	}
	if stats.TotalInputTokens != 120000 {
		t.Errorf("expected 120000 input tokens, got %d", stats.TotalInputTokens)
	}

	expectedCost := 0.53
	if stats.TotalCostUSD < expectedCost-0.01 || stats.TotalCostUSD > expectedCost+0.01 {
		t.Errorf("expected cost ~%f, got %f", expectedCost, stats.TotalCostUSD)
	}
}

func TestStore_FindByPrefix(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "canopy-history-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStoreWithDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create runs with similar IDs
	runs := []*RunRecord{
		{ID: "abc123def", StartTime: time.Now()},
		{ID: "abc456ghi", StartTime: time.Now()},
		{ID: "xyz789", StartTime: time.Now()},
	}

	for _, run := range runs {
		if err := store.Save(run); err != nil {
			t.Fatalf("failed to save run: %v", err)
		}
	}

	// Find by unique prefix
	run, err := store.FindByPrefix("xyz")
	if err != nil {
		t.Fatalf("failed to find by prefix: %v", err)
	}
	if run.ID != "xyz789" {
		t.Errorf("expected xyz789, got %s", run.ID)
	}

	// Find by ambiguous prefix
	_, err = store.FindByPrefix("abc")
	if err == nil {
		t.Error("expected error for ambiguous prefix")
	}

	// Find by non-existent prefix
	_, err = store.FindByPrefix("notfound")
	if err == nil {
		t.Error("expected error for non-existent prefix")
	}
}

func TestStore_Delete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "canopy-history-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStoreWithDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	run := &RunRecord{
		ID:        "delete-test",
		StartTime: time.Now(),
		Status:    RunStatusCompleted,
	}

	if err := store.Save(run); err != nil {
		t.Fatalf("failed to save run: %v", err)
	}

	// Verify exists
	if _, err := store.Get("delete-test"); err != nil {
		t.Fatalf("run should exist: %v", err)
	}

	// Delete
	if err := store.Delete("delete-test"); err != nil {
		t.Fatalf("failed to delete run: %v", err)
	}

	// Verify deleted
	if _, err := store.Get("delete-test"); err == nil {
		t.Error("run should not exist after deletion")
	}

	// Delete non-existent
	if err := store.Delete("nonexistent"); err == nil {
		t.Error("expected error when deleting non-existent run")
	}
}

func TestMigrateHistory(t *testing.T) {
	// Create temp directories for old and new locations
	oldDir, err := os.MkdirTemp("", "canopy-old-*")
	if err != nil {
		t.Fatalf("failed to create old temp dir: %v", err)
	}
	defer os.RemoveAll(oldDir)

	newDir, err := os.MkdirTemp("", "canopy-new-*")
	if err != nil {
		t.Fatalf("failed to create new temp dir: %v", err)
	}
	defer os.RemoveAll(newDir)

	// Create old history with some run files
	oldHistoryDir := filepath.Join(oldDir, "history")
	if err := os.MkdirAll(oldHistoryDir, 0755); err != nil {
		t.Fatalf("failed to create old history dir: %v", err)
	}

	// Create test run files in old location
	testRuns := []string{"run-abc123.json", "run-def456.json", "run-ghi789.json"}
	for _, name := range testRuns {
		content := []byte(`{"id": "test", "status": "completed"}`)
		if err := os.WriteFile(filepath.Join(oldHistoryDir, name), content, 0644); err != nil {
			t.Fatalf("failed to write old run file: %v", err)
		}
	}

	// Run migration
	if err := migrateHistory(oldDir, newDir); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Verify files were copied to new location
	newHistoryDir := filepath.Join(newDir, "history")
	for _, name := range testRuns {
		newPath := filepath.Join(newHistoryDir, name)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			t.Errorf("file %s not migrated", name)
		}
	}

	// Verify old files still exist (should be left for manual deletion)
	for _, name := range testRuns {
		oldPath := filepath.Join(oldHistoryDir, name)
		if _, err := os.Stat(oldPath); os.IsNotExist(err) {
			t.Errorf("old file %s should still exist", name)
		}
	}
}

func TestMigrateHistory_NoOldHistory(t *testing.T) {
	// Create temp directories
	oldDir, err := os.MkdirTemp("", "canopy-old-*")
	if err != nil {
		t.Fatalf("failed to create old temp dir: %v", err)
	}
	defer os.RemoveAll(oldDir)

	newDir, err := os.MkdirTemp("", "canopy-new-*")
	if err != nil {
		t.Fatalf("failed to create new temp dir: %v", err)
	}
	defer os.RemoveAll(newDir)

	// Don't create any old history directory
	// Migration should succeed silently
	if err := migrateHistory(oldDir, newDir); err != nil {
		t.Fatalf("migration should succeed when no old history exists: %v", err)
	}

	// New history directory should not be created
	newHistoryDir := filepath.Join(newDir, "history")
	if _, err := os.Stat(newHistoryDir); !os.IsNotExist(err) {
		t.Error("new history directory should not be created when no old history exists")
	}
}

func TestMigrateHistory_NewHistoryExists(t *testing.T) {
	// Create temp directories
	oldDir, err := os.MkdirTemp("", "canopy-old-*")
	if err != nil {
		t.Fatalf("failed to create old temp dir: %v", err)
	}
	defer os.RemoveAll(oldDir)

	newDir, err := os.MkdirTemp("", "canopy-new-*")
	if err != nil {
		t.Fatalf("failed to create new temp dir: %v", err)
	}
	defer os.RemoveAll(newDir)

	// Create old history with run files
	oldHistoryDir := filepath.Join(oldDir, "history")
	if err := os.MkdirAll(oldHistoryDir, 0755); err != nil {
		t.Fatalf("failed to create old history dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldHistoryDir, "run-old.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to write old run file: %v", err)
	}

	// Create new history with existing run files
	newHistoryDir := filepath.Join(newDir, "history")
	if err := os.MkdirAll(newHistoryDir, 0755); err != nil {
		t.Fatalf("failed to create new history dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newHistoryDir, "run-new.json"), []byte(`{"id": "new"}`), 0644); err != nil {
		t.Fatalf("failed to write new run file: %v", err)
	}

	// Migration should succeed but not overwrite
	if err := migrateHistory(oldDir, newDir); err != nil {
		t.Fatalf("migration should succeed when new history exists: %v", err)
	}

	// Old file should NOT be migrated (don't overwrite existing history)
	if _, err := os.Stat(filepath.Join(newHistoryDir, "run-old.json")); !os.IsNotExist(err) {
		t.Error("old file should not be migrated when new history already exists")
	}

	// New file should still exist unchanged
	if _, err := os.Stat(filepath.Join(newHistoryDir, "run-new.json")); os.IsNotExist(err) {
		t.Error("existing new file should not be affected")
	}
}

func TestDefaultDataDir(t *testing.T) {
	// Test with XDG_CACHE_HOME set
	origCache := os.Getenv("XDG_CACHE_HOME")
	defer os.Setenv("XDG_CACHE_HOME", origCache)

	testCacheDir := "/tmp/test-cache"
	os.Setenv("XDG_CACHE_HOME", testCacheDir)

	dir, err := defaultDataDir()
	if err != nil {
		t.Fatalf("defaultDataDir failed: %v", err)
	}

	expected := filepath.Join(testCacheDir, "canopy")
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}

	// Test with XDG_CACHE_HOME unset (falls back to ~/.cache)
	os.Unsetenv("XDG_CACHE_HOME")
	dir, err = defaultDataDir()
	if err != nil {
		t.Fatalf("defaultDataDir failed without XDG_CACHE_HOME: %v", err)
	}

	home, _ := os.UserHomeDir()
	expected = filepath.Join(home, ".cache", "canopy")
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}
}
