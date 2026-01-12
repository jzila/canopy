// Package failedpatches provides functionality to preserve git patches
// when both merge and resolution fail, preventing data loss.
package failedpatches

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PreserveResult contains information about where patches were preserved
type PreserveResult struct {
	// Dir is the directory where patches were saved
	Dir string
	// PatchCount is the number of patches preserved
	PatchCount int
}

// PreservePatches saves failed patches to a persistent location in the cache directory.
// This is called when both merge and resolver fail, to prevent data loss.
//
// The patches are saved to: ~/.cache/canopy/failed-patches/<task-id>/
// Each patch is saved as patch-<n>.patch (same format as git format-patch)
//
// Returns a PreserveResult with the directory path and patch count, or an error.
func PreservePatches(taskID string, patches []string, errors []string) (*PreserveResult, error) {
	if len(patches) == 0 {
		return nil, fmt.Errorf("no patches to preserve")
	}

	// Get cache directory
	cacheDir := getCacheDir()

	// Create unique directory for this task's failed patches
	// Include timestamp with nanoseconds to allow multiple preservation attempts for same task
	timestamp := time.Now().Format("20060102-150405.000000000")
	patchDir := filepath.Join(cacheDir, "failed-patches", fmt.Sprintf("%s-%s", taskID, timestamp))

	if err := os.MkdirAll(patchDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create patch directory: %w", err)
	}

	// Write each patch
	for i, patch := range patches {
		patchPath := filepath.Join(patchDir, fmt.Sprintf("patch-%d.patch", i))
		if err := os.WriteFile(patchPath, []byte(patch), 0644); err != nil {
			return nil, fmt.Errorf("failed to write patch %d: %w", i, err)
		}
	}

	// Write errors file if there are errors
	if len(errors) > 0 {
		errorsPath := filepath.Join(patchDir, "errors.txt")
		var content string
		for i, errMsg := range errors {
			content += fmt.Sprintf("=== Patch %d Error ===\n%s\n\n", i, errMsg)
		}
		if err := os.WriteFile(errorsPath, []byte(content), 0644); err != nil {
			return nil, fmt.Errorf("failed to write errors file: %w", err)
		}
	}

	// Write README with instructions
	readme := fmt.Sprintf(`Failed Patches for Task: %s
Preserved: %s

These patches failed to apply during merge, and the resolver agent also failed.
The patches can be manually applied using git am:

  cd /path/to/repo
  git am --3way %s/patch-*.patch

Or apply them one at a time:

  git am --3way %s/patch-0.patch
  # resolve conflicts if needed, then:
  git am --continue

To view the original errors, see errors.txt in this directory.
`, taskID, time.Now().Format(time.RFC3339), patchDir, patchDir)

	readmePath := filepath.Join(patchDir, "README.txt")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		// Non-fatal: patches are preserved, just log the error
		fmt.Fprintf(os.Stderr, "warning: failed to write README.txt: %v\n", err)
	}

	return &PreserveResult{
		Dir:        patchDir,
		PatchCount: len(patches),
	}, nil
}

// getCacheDir returns the cache directory for canopy, respecting XDG_CACHE_HOME
func getCacheDir() string {
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheDir, "canopy")
}

// ListPreservedPatches returns a list of all preserved patch directories
func ListPreservedPatches() ([]string, error) {
	cacheDir := getCacheDir()
	patchesDir := filepath.Join(cacheDir, "failed-patches")

	entries, err := os.ReadDir(patchesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read patches directory: %w", err)
	}

	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(patchesDir, entry.Name()))
		}
	}
	return dirs, nil
}

// CleanupPreservedPatches removes all preserved patch directories
func CleanupPreservedPatches() error {
	cacheDir := getCacheDir()
	patchesDir := filepath.Join(cacheDir, "failed-patches")

	if err := os.RemoveAll(patchesDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to cleanup patches directory: %w", err)
	}
	return nil
}
