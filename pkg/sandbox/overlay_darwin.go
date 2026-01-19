//go:build darwin

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// DarwinOverlayState stores state needed for Darwin clone-based overlay.
// Since Darwin doesn't use a real mount, we track the clone directory and
// use mtime-based change detection instead of OverlayFS upper directory.
type DarwinOverlayState struct {
	// Snapshot taken immediately after cloning, used for change detection
	BaseSnapshot *Snapshot
	// CloneTime records when the clone was created for stale detection
	CloneTime time.Time
}

// darwinState holds Darwin-specific state for each overlay.
// This is stored separately since the Overlay struct is defined in overlay.go.
var darwinState = make(map[string]*DarwinOverlayState)

// IsStub returns false on Darwin where APFS clone-based overlay is supported
func IsStub() bool {
	return false
}

// Mount creates an APFS clone of the source directory using cp -c (copy-on-write).
// On Darwin, this doesn't create a real mount - instead it:
// 1. Clones the source directory to MergedDir using APFS COW clones
// 2. Takes an mtime snapshot for later change detection
// 3. Deletes hidden paths (equivalent to whiteouts on Linux)
//
// This approach provides similar isolation semantics to OverlayFS:
// - Changes are isolated to the clone
// - Original directory is preserved
// - Changes can be detected via snapshot comparison
func (o *Overlay) Mount() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.mounted {
		return fmt.Errorf("overlay %s already mounted (flag)", o.ID)
	}

	// Check if MergedDir already has content (stale state)
	if o.isMountedLocked() {
		return fmt.Errorf("overlay %s already has cloned content", o.ID)
	}

	// Remove MergedDir if it exists (it was created empty by NewOverlay)
	if err := os.RemoveAll(o.MergedDir); err != nil {
		return fmt.Errorf("remove empty merged dir: %w", err)
	}

	// Clone using cp -c -R for APFS copy-on-write
	// -c: Use clonefile(2) for COW copies (APFS-specific)
	// -R: Recursive
	// -P: Don't follow symlinks (preserve them)
	cmd := exec.Command("cp", "-c", "-R", "-P", o.LowerDir, o.MergedDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cp -c clone failed: %w: %s", err, output)
	}

	// Delete hidden paths in the clone (equivalent to whiteouts)
	if err := o.deleteHiddenPaths(); err != nil {
		// Clean up on failure
		os.RemoveAll(o.MergedDir)
		return fmt.Errorf("delete hidden paths: %w", err)
	}

	// Copy passthrough symlinks (pointing back to original locations)
	if err := o.setupPassthroughSymlinks(); err != nil {
		os.RemoveAll(o.MergedDir)
		return fmt.Errorf("setup passthrough symlinks: %w", err)
	}

	// Take baseline snapshot for change detection
	// Exclude the same paths as GetChanges() would
	excludes := append([]string{}, DefaultPassthroughPaths...)
	excludes = append(excludes, DefaultHiddenPaths...)
	excludes = append(excludes, HomeExcludedPaths...)

	snapshot, err := TakeSnapshot(o.MergedDir, excludes)
	if err != nil {
		os.RemoveAll(o.MergedDir)
		return fmt.Errorf("take baseline snapshot: %w", err)
	}

	// Store Darwin-specific state
	darwinState[o.ID] = &DarwinOverlayState{
		BaseSnapshot: snapshot,
		CloneTime:    time.Now(),
	}

	o.mounted = true
	o.useFuse = false // Not using FUSE
	return nil
}

// deleteHiddenPaths removes paths that should be hidden from agents.
// This is the Darwin equivalent of OverlayFS whiteouts.
func (o *Overlay) deleteHiddenPaths() error {
	for _, hiddenPath := range DefaultHiddenPaths {
		fullPath := filepath.Join(o.MergedDir, hiddenPath)

		// Check if path exists
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			continue
		}

		// Remove the path (file or directory)
		if err := os.RemoveAll(fullPath); err != nil {
			return fmt.Errorf("remove hidden path %s: %w", hiddenPath, err)
		}
	}
	return nil
}

// setupPassthroughSymlinks creates symlinks for paths that should bypass the clone
// and write directly to the original filesystem.
func (o *Overlay) setupPassthroughSymlinks() error {
	for _, relPath := range DefaultPassthroughPaths {
		srcPath := filepath.Join(o.LowerDir, relPath)
		clonePath := filepath.Join(o.MergedDir, relPath)

		// Only setup if source exists in original directory
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			continue
		}

		// Remove the cloned copy
		if err := os.RemoveAll(clonePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove cloned %s for passthrough: %w", relPath, err)
		}

		// Create parent directory if needed
		parentDir := filepath.Dir(clonePath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("create parent for passthrough symlink: %w", err)
		}

		// Create symlink pointing to original location
		if err := os.Symlink(srcPath, clonePath); err != nil {
			return fmt.Errorf("create passthrough symlink for %s: %w", relPath, err)
		}
	}
	return nil
}

// unmountPassthroughs is a no-op on Darwin since we use symlinks that get
// cleaned up with the clone directory.
func (o *Overlay) unmountPassthroughs() {
	o.bindMounts = nil
}

// Unmount cleans up the Darwin overlay state.
// Since Darwin doesn't use a real mount, this just clears internal state.
// The actual clone directory is removed by Cleanup().
//
// This method is idempotent and thread-safe.
func (o *Overlay) Unmount() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Clean up Darwin state
	delete(darwinState, o.ID)

	// Clear passthrough tracking
	o.unmountPassthroughs()

	// Mark as unmounted
	o.mounted = false
	return nil
}

// isMounted checks if the overlay clone exists and has content.
// On Darwin, we check if MergedDir exists and is non-empty.
func (o *Overlay) isMounted() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.isMountedLocked()
}

// isMountedLocked checks mount state without acquiring the lock.
// Caller must hold o.mu.
func (o *Overlay) isMountedLocked() bool {
	// Check if MergedDir exists and has content
	entries, err := os.ReadDir(o.MergedDir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// IsMountPoint checks if a path appears to be a Darwin overlay clone.
// Since Darwin doesn't use real mounts, we check if the path exists and
// is within the expected overlay directory structure.
func IsMountPoint(path string) bool {
	// Check if path exists
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	// Must be a directory
	if !info.IsDir() {
		return false
	}

	// Check if it has content (empty directories aren't "mounted")
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}

	return len(entries) > 0
}

// DetectStaleMounts finds overlay clones under baseDir that may be leftover from crashes.
// On Darwin, we detect stale clones by checking for directories with the expected
// structure (upper/, work/, merged/ with content) that aren't tracked in darwinState.
func DetectStaleMounts(baseDir string) ([]StaleMountInfo, error) {
	var stale []StaleMountInfo

	entries, err := os.ReadDir(baseDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read base dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		id := entry.Name()
		mergedDir := filepath.Join(baseDir, id, "merged")

		// Check if merged directory exists and has content
		if IsMountPoint(mergedDir) {
			// Check if this overlay is tracked in our state
			if _, tracked := darwinState[id]; !tracked {
				stale = append(stale, StaleMountInfo{
					MergedDir: mergedDir,
					ID:        id,
					IsFuse:    false, // Darwin doesn't use FUSE
				})
			}
		}
	}

	return stale, nil
}

// CleanupStaleMounts removes stale overlay clones found by DetectStaleMounts.
// On Darwin, this simply removes the entire overlay directory tree.
func CleanupStaleMounts(baseDir string) (int, []error) {
	stale, err := DetectStaleMounts(baseDir)
	if err != nil {
		return 0, []error{err}
	}

	var errors []error
	cleaned := 0

	for _, info := range stale {
		// Remove the entire overlay directory tree
		overlayDir := filepath.Join(baseDir, info.ID)
		if err := os.RemoveAll(overlayDir); err != nil {
			errors = append(errors, fmt.Errorf("remove %s: %w", overlayDir, err))
			continue
		}
		cleaned++
	}

	return cleaned, errors
}

// RecoverFromCrash performs cleanup operations needed after a crash.
// On Darwin, this removes stale clone directories.
func RecoverFromCrash(baseDir string) (cleaned int, stale int, errors []error) {
	staleMounts, err := DetectStaleMounts(baseDir)
	if err != nil {
		return 0, 0, []error{err}
	}

	stale = len(staleMounts)
	if stale == 0 {
		return 0, 0, nil
	}

	cleaned, errors = CleanupStaleMounts(baseDir)
	return cleaned, stale, errors
}

// GetDarwinSnapshot returns the baseline snapshot for change detection.
// This is Darwin-specific and used by the snapshot-based change detection.
func (o *Overlay) GetDarwinSnapshot() *Snapshot {
	if state, ok := darwinState[o.ID]; ok {
		return state.BaseSnapshot
	}
	return nil
}

// GetChangesDarwin detects changes by comparing the current state against
// the baseline snapshot taken at clone time. This is the Darwin equivalent
// of reading the OverlayFS upper directory.
func (o *Overlay) GetChangesDarwin() ([]Change, error) {
	state, ok := darwinState[o.ID]
	if !ok {
		return nil, fmt.Errorf("no Darwin state for overlay %s", o.ID)
	}

	// Take a new snapshot of the current state
	currentSnapshot, err := state.BaseSnapshot.Refresh()
	if err != nil {
		return nil, fmt.Errorf("take current snapshot: %w", err)
	}

	// Compare against baseline
	return state.BaseSnapshot.Compare(currentSnapshot), nil
}
