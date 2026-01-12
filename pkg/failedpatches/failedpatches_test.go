package failedpatches

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreservePatches(t *testing.T) {
	// Use a temp directory for testing
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)

	patches := []string{
		"From abc123\nSubject: Test patch 1\n\ndiff --git a/file.go b/file.go\n--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n-old\n+new\n",
		"From def456\nSubject: Test patch 2\n\ndiff --git a/other.go b/other.go\n--- a/other.go\n+++ b/other.go\n@@ -1 +1 @@\n-foo\n+bar\n",
	}
	errors := []string{
		"patch 0 failed: conflict in file.go",
		"patch 1 failed: conflict in other.go",
	}

	result, err := PreservePatches("beads-test123", patches, errors)
	if err != nil {
		t.Fatalf("PreservePatches failed: %v", err)
	}

	if result.PatchCount != 2 {
		t.Errorf("expected PatchCount=2, got %d", result.PatchCount)
	}

	// Verify directory was created under XDG_CACHE_HOME
	if !strings.HasPrefix(result.Dir, filepath.Join(tempDir, "canopy", "failed-patches")) {
		t.Errorf("expected Dir to start with %s, got %s", filepath.Join(tempDir, "canopy", "failed-patches"), result.Dir)
	}

	// Verify patches were written
	for i, expectedPatch := range patches {
		patchPath := filepath.Join(result.Dir, "patch-"+string(rune('0'+i))+".patch")
		content, err := os.ReadFile(patchPath)
		if err != nil {
			t.Errorf("failed to read patch-%d.patch: %v", i, err)
			continue
		}
		if string(content) != expectedPatch {
			t.Errorf("patch-%d.patch content mismatch", i)
		}
	}

	// Verify errors.txt was written
	errorsPath := filepath.Join(result.Dir, "errors.txt")
	errorsContent, err := os.ReadFile(errorsPath)
	if err != nil {
		t.Errorf("failed to read errors.txt: %v", err)
	} else {
		for _, errMsg := range errors {
			if !strings.Contains(string(errorsContent), errMsg) {
				t.Errorf("errors.txt missing error message: %s", errMsg)
			}
		}
	}

	// Verify README.txt was written
	readmePath := filepath.Join(result.Dir, "README.txt")
	readmeContent, err := os.ReadFile(readmePath)
	if err != nil {
		t.Errorf("failed to read README.txt: %v", err)
	} else {
		if !strings.Contains(string(readmeContent), "beads-test123") {
			t.Errorf("README.txt missing task ID")
		}
		if !strings.Contains(string(readmeContent), "git am --3way") {
			t.Errorf("README.txt missing git am instructions")
		}
	}
}

func TestPreservePatchesNoPatches(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)

	_, err := PreservePatches("beads-test", nil, nil)
	if err == nil {
		t.Error("expected error for empty patches, got nil")
	}
	if !strings.Contains(err.Error(), "no patches to preserve") {
		t.Errorf("expected 'no patches to preserve' error, got: %v", err)
	}
}

func TestPreservePatchesNoErrors(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)

	patches := []string{"From abc\nSubject: Test\n\ndiff content"}

	result, err := PreservePatches("beads-test", patches, nil)
	if err != nil {
		t.Fatalf("PreservePatches failed: %v", err)
	}

	// Verify no errors.txt was written
	errorsPath := filepath.Join(result.Dir, "errors.txt")
	if _, err := os.Stat(errorsPath); !os.IsNotExist(err) {
		t.Error("errors.txt should not exist when no errors provided")
	}
}

func TestListPreservedPatches(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)

	// Initially empty
	dirs, err := ListPreservedPatches()
	if err != nil {
		t.Fatalf("ListPreservedPatches failed: %v", err)
	}
	if len(dirs) != 0 {
		t.Errorf("expected 0 dirs initially, got %d", len(dirs))
	}

	// Preserve some patches
	patches := []string{"patch content"}
	_, err = PreservePatches("beads-task1", patches, nil)
	if err != nil {
		t.Fatalf("PreservePatches failed: %v", err)
	}
	_, err = PreservePatches("beads-task2", patches, nil)
	if err != nil {
		t.Fatalf("PreservePatches failed: %v", err)
	}

	// Should now list 2 directories
	dirs, err = ListPreservedPatches()
	if err != nil {
		t.Fatalf("ListPreservedPatches failed: %v", err)
	}
	if len(dirs) != 2 {
		t.Errorf("expected 2 dirs, got %d", len(dirs))
	}
}

func TestCleanupPreservedPatches(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)

	// Preserve some patches
	patches := []string{"patch content"}
	_, err := PreservePatches("beads-task1", patches, nil)
	if err != nil {
		t.Fatalf("PreservePatches failed: %v", err)
	}

	// Cleanup
	err = CleanupPreservedPatches()
	if err != nil {
		t.Fatalf("CleanupPreservedPatches failed: %v", err)
	}

	// Verify directory is gone
	dirs, err := ListPreservedPatches()
	if err != nil {
		t.Fatalf("ListPreservedPatches failed: %v", err)
	}
	if len(dirs) != 0 {
		t.Errorf("expected 0 dirs after cleanup, got %d", len(dirs))
	}
}

func TestCleanupPreservedPatchesEmpty(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)

	// Cleanup when nothing exists should not error
	err := CleanupPreservedPatches()
	if err != nil {
		t.Errorf("CleanupPreservedPatches failed on empty dir: %v", err)
	}
}

func TestMultiplePreservationsForSameTask(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)

	patches := []string{"patch content"}

	// Preserve same task twice (simulating retries)
	result1, err := PreservePatches("beads-sametask", patches, nil)
	if err != nil {
		t.Fatalf("PreservePatches (1) failed: %v", err)
	}
	result2, err := PreservePatches("beads-sametask", patches, nil)
	if err != nil {
		t.Fatalf("PreservePatches (2) failed: %v", err)
	}

	// Both should succeed with different directories (due to timestamp)
	if result1.Dir == result2.Dir {
		t.Error("expected different directories for multiple preservations of same task")
	}

	// Both directories should exist
	if _, err := os.Stat(result1.Dir); os.IsNotExist(err) {
		t.Error("first preservation directory should exist")
	}
	if _, err := os.Stat(result2.Dir); os.IsNotExist(err) {
		t.Error("second preservation directory should exist")
	}
}
