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
