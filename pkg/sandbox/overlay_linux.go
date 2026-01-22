//go:build linux

package sandbox

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// IsStub returns false on Linux where overlay is supported
func IsStub() bool {
	return false
}

// Mount mounts the overlay filesystem.
// This method is thread-safe.
// Tries kernel overlayfs first, falls back to fuse-overlayfs for rootless operation.
func (o *Overlay) Mount() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.mounted {
		return fmt.Errorf("overlay %s already mounted (flag)", o.ID)
	}

	// Check actual mount state to handle stale state from crashes
	if o.isMounted() {
		return fmt.Errorf("overlay %s already mounted (detected in /proc/mounts)", o.ID)
	}

	// Setup passthrough symlinks in upperdir BEFORE mounting
	// This way they'll appear in the merged view when overlay is mounted
	if err := o.setupPassthroughSymlinks(); err != nil {
		return err
	}

	// Build mount options
	// Format: lowerdir=X,upperdir=Y,workdir=Z
	mountOpts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
		o.LowerDir, o.UpperDir, o.WorkDir)

	// Try kernel overlayfs first (requires privileges or user namespace)
	err := syscall.Mount("overlay", o.MergedDir, "overlay", 0, mountOpts)
	if err == nil {
		o.mounted = true
		o.useFuse = false
		return nil
	}

	// Try with userxattr for rootless (kernel >= 5.11)
	mountOptsUserXattr := mountOpts + ",userxattr"
	err = syscall.Mount("overlay", o.MergedDir, "overlay", 0, mountOptsUserXattr)
	if err == nil {
		o.mounted = true
		o.useFuse = false
		return nil
	}

	// Fall back to fuse-overlayfs
	if err := o.mountFuseLocked(); err != nil {
		return err
	}
	return nil
}

// setupPassthroughSymlinks creates symlinks in upperdir for paths that should
// bypass the overlay. Must be called BEFORE mounting the overlay.
func (o *Overlay) setupPassthroughSymlinks() error {
	for _, relPath := range DefaultPassthroughPaths {
		srcPath := filepath.Join(o.LowerDir, relPath)
		upperPath := filepath.Join(o.UpperDir, relPath)

		// Only setup if source exists in lowerdir
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			continue
		}

		// Create symlink in upperdir pointing to original location
		// When overlay is mounted, upperdir takes precedence over lowerdir
		if err := os.Symlink(srcPath, upperPath); err != nil {
			return fmt.Errorf("failed to create passthrough symlink for %s: %w", relPath, err)
		}
	}
	return nil
}

// unmountPassthroughs removes passthrough symlinks
func (o *Overlay) unmountPassthroughs() {
	// Remove symlinks (they'll be cleaned up with the overlay anyway,
	// but this keeps the slice consistent)
	o.bindMounts = nil
}

// mountFuseLocked uses fuse-overlayfs for rootless operation.
// Caller must hold o.mu.
func (o *Overlay) mountFuseLocked() error {
	fusePath, err := exec.LookPath("fuse-overlayfs")
	if err != nil {
		return fmt.Errorf("fuse-overlayfs not found and kernel overlay failed: %w", err)
	}

	args := []string{
		"-o", fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
			o.LowerDir, o.UpperDir, o.WorkDir),
		o.MergedDir,
	}

	cmd := exec.Command(fusePath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("fuse-overlayfs mount failed: %w: %s", err, out)
	}

	o.mounted = true
	o.useFuse = true
	return nil
}

// Unmount unmounts the overlay filesystem.
// This method is idempotent and thread-safe - it's safe to call even if not mounted
// or if called concurrently from multiple goroutines (e.g., defer and signal handler).
// It checks actual mount state rather than relying solely on the mounted flag,
// which may be stale after crashes or inconsistent state.
func (o *Overlay) Unmount() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Check actual mount state - don't rely solely on the flag
	// The flag may be stale after crashes or state inconsistency
	actuallyMounted := o.isMounted()

	if !actuallyMounted && !o.mounted {
		// Neither flag nor reality says mounted - nothing to do
		return nil
	}

	// Clean up passthrough symlinks (they'll be removed with overlay anyway)
	o.unmountPassthroughs()

	// If flag says mounted but it's actually not, just clear the flag
	if !actuallyMounted {
		o.mounted = false
		return nil
	}

	// Actually mounted - perform unmount with exponential backoff
	const maxRetries = 5
	baseDelay := 50 * time.Millisecond

	// Capture useFuse value before unlocking for sleep
	useFuse := o.useFuse

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 50ms, 100ms, 200ms, 400ms, 800ms
			// Release lock during sleep to avoid blocking other operations
			o.mu.Unlock()
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			time.Sleep(delay)
			o.mu.Lock()

			// Re-check if another goroutine unmounted while we were sleeping
			if !o.isMounted() {
				o.mounted = false
				return nil
			}
		}

		// Try primary unmount method
		var err error
		if useFuse {
			err = exec.Command("fusermount", "-u", o.MergedDir).Run()
		} else {
			err = syscall.Unmount(o.MergedDir, syscall.MNT_DETACH)
		}

		if err == nil {
			// Verify unmount succeeded
			if !o.isMounted() {
				o.mounted = false
				return nil
			}
			lastErr = fmt.Errorf("unmount appeared successful but %s is still mounted", o.MergedDir)
			continue
		}

		lastErr = err

		// If primary method failed, try umount -l as fallback
		if fallbackErr := exec.Command("umount", "-l", o.MergedDir).Run(); fallbackErr == nil {
			// Verify fallback succeeded
			if !o.isMounted() {
				o.mounted = false
				return nil
			}
			lastErr = fmt.Errorf("umount -l appeared successful but %s is still mounted", o.MergedDir)
			continue
		}
	}

	// All attempts failed
	return fmt.Errorf("failed to unmount %s after %d attempts: %w", o.MergedDir, maxRetries, lastErr)
}

// isMounted checks if the overlay is currently mounted by checking /proc/mounts
func (o *Overlay) isMounted() bool {
	return IsMountPoint(o.MergedDir)
}

// IsMountPoint checks if a path is currently a mount point
func IsMountPoint(path string) bool {
	// Use findmnt to check if the path is a mount point
	cmd := exec.Command("findmnt", "-n", "-o", "TARGET", path)
	output, err := cmd.Output()
	if err != nil {
		// findmnt returns non-zero if not found, which means not mounted
		return false
	}
	// If findmnt found it, it's mounted
	return len(output) > 0
}

// DetectStaleMounts finds overlay mounts under baseDir that may be leftover from crashes.
// Returns a list of stale mount info that can be cleaned up.
func DetectStaleMounts(baseDir string) ([]StaleMountInfo, error) {
	var stale []StaleMountInfo

	// List potential overlay directories
	entries, err := os.ReadDir(baseDir)
	if os.IsNotExist(err) {
		return nil, nil // No base directory, no stale mounts
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

		// Check if this merged directory is a mount point
		if IsMountPoint(mergedDir) {
			info := StaleMountInfo{
				MergedDir: mergedDir,
				ID:        id,
			}
			// Check if it's a FUSE mount by looking at mount type
			cmd := exec.Command("findmnt", "-n", "-o", "FSTYPE", mergedDir)
			output, err := cmd.Output()
			if err == nil {
				fsType := strings.TrimSpace(string(output))
				info.IsFuse = fsType == "fuse.fuse-overlayfs"
			}
			stale = append(stale, info)
		}
	}

	return stale, nil
}

// CleanupStaleMounts unmounts and removes stale overlay mounts found by DetectStaleMounts.
// Returns the number of successfully cleaned mounts and any errors encountered.
func CleanupStaleMounts(baseDir string) (int, []error) {
	stale, err := DetectStaleMounts(baseDir)
	if err != nil {
		return 0, []error{err}
	}

	var errors []error
	cleaned := 0

	for _, info := range stale {
		// Try to unmount
		var unmountErr error
		if info.IsFuse {
			unmountErr = exec.Command("fusermount", "-u", info.MergedDir).Run()
		} else {
			unmountErr = syscall.Unmount(info.MergedDir, syscall.MNT_DETACH)
		}

		if unmountErr != nil {
			// Try lazy unmount as fallback
			unmountErr = exec.Command("umount", "-l", info.MergedDir).Run()
		}

		if unmountErr != nil {
			errors = append(errors, fmt.Errorf("unmount %s: %w", info.MergedDir, unmountErr))
			continue
		}

		// Verify unmount succeeded
		if IsMountPoint(info.MergedDir) {
			errors = append(errors, fmt.Errorf("%s still mounted after unmount attempt", info.MergedDir))
			continue
		}

		// Remove the overlay directory tree
		overlayDir := filepath.Join(baseDir, info.ID)
		if err := os.RemoveAll(overlayDir); err != nil {
			errors = append(errors, fmt.Errorf("remove %s: %w", overlayDir, err))
			continue
		}

		cleaned++
	}

	return cleaned, errors
}

// RemountOverlay recreates an Overlay struct from persisted paths and remounts it.
// This is used for resuming interrupted agents after daemon restart.
// The overlay directories must still exist on disk.
func RemountOverlay(lowerDir, upperDir, workDir, mergedDir string) (*Overlay, error) {
	// Verify all directories exist
	for name, dir := range map[string]string{
		"lower":  lowerDir,
		"upper":  upperDir,
		"work":   workDir,
		"merged": mergedDir,
	} {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return nil, fmt.Errorf("%s directory does not exist: %s", name, dir)
		}
	}

	// Extract ID from the directory structure (baseDir/ID/upper)
	// upperDir is like /path/to/base/abc123/upper
	id := filepath.Base(filepath.Dir(upperDir))

	overlay := &Overlay{
		ID:        id,
		LowerDir:  lowerDir,
		UpperDir:  upperDir,
		WorkDir:   workDir,
		MergedDir: mergedDir,
	}

	// Check if already mounted (from before crash)
	if IsMountPoint(mergedDir) {
		overlay.mounted = true
		// Check if it's a FUSE mount
		cmd := exec.Command("findmnt", "-n", "-o", "FSTYPE", mergedDir)
		output, err := cmd.Output()
		if err == nil {
			fsType := strings.TrimSpace(string(output))
			overlay.useFuse = fsType == "fuse.fuse-overlayfs"
		}
		return overlay, nil
	}

	// Mount the overlay
	if err := overlay.Mount(); err != nil {
		return nil, fmt.Errorf("failed to mount overlay: %w", err)
	}

	return overlay, nil
}

// RecoverFromCrash performs cleanup operations needed after a crash.
// It detects stale mounts, cleans them up, and returns a summary.
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

// GetChanges extracts all file changes from the overlay's upper directory.
// This is the Linux-specific implementation that reads from OverlayFS upper directory.
func (o *Overlay) GetChanges() ([]FileChange, error) {
	// For direct overlays (no UpperDir), return empty list.
	// Changes should be tracked via git status instead.
	if o.UpperDir == "" {
		return nil, nil
	}

	var changes []FileChange

	err := filepath.WalkDir(o.UpperDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory
		if path == o.UpperDir {
			return nil
		}

		// Get relative path
		relPath, _ := filepath.Rel(o.UpperDir, path)

		// Skip .git directory entirely - git changes are handled separately via commit extraction
		if relPath == ".git" || strings.HasPrefix(relPath, ".git"+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip passthrough paths (they're bind-mounted, not overlayed)
		for _, passthrough := range DefaultPassthroughPaths {
			if relPath == passthrough || strings.HasPrefix(relPath, passthrough+string(filepath.Separator)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip hidden paths (they're whiteouts we created, not real changes)
		for _, hidden := range DefaultHiddenPaths {
			if relPath == hidden || strings.HasPrefix(relPath, hidden+string(filepath.Separator)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip HOME-related paths (created when HOME=merged, not project files)
		for _, excluded := range HomeExcludedPaths {
			if relPath == excluded || strings.HasPrefix(relPath, excluded+string(filepath.Separator)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Check for whiteout files (deleted files in overlay)
		// Whiteout files have names prefixed with .wh.
		if strings.HasPrefix(d.Name(), ".wh.") {
			originalName := strings.TrimPrefix(d.Name(), ".wh.")
			changes = append(changes, FileChange{
				Path: filepath.Join(filepath.Dir(relPath), originalName),
				Type: ChangeDeleted,
			})
			return nil
		}

		// Skip directories (we track file contents)
		if d.IsDir() {
			return nil
		}

		// Check if file exists in lower (original)
		lowerPath := filepath.Join(o.LowerDir, relPath)
		_, lowerErr := os.Stat(lowerPath)

		// Compute hash of upper file
		upperHash, hashErr := hashFile(path)
		if hashErr != nil {
			// Log but continue - file change is still recorded, just without hash
			fmt.Fprintf(os.Stderr, "warning: failed to hash file %s: %v\n", relPath, hashErr)
		}

		change := FileChange{Path: relPath, NewHash: upperHash}

		if os.IsNotExist(lowerErr) {
			change.Type = ChangeCreated
		} else {
			// File exists in lower - compare content hashes
			lowerHash, lowerHashErr := hashFile(lowerPath)
			if lowerHashErr != nil {
				// Can't compare, assume modified to be safe
				fmt.Fprintf(os.Stderr, "warning: failed to hash lower file %s: %v\n", relPath, lowerHashErr)
				change.Type = ChangeModified
			} else if upperHash == lowerHash {
				// Content identical - skip this file (copy-up without actual change)
				return nil
			} else {
				change.Type = ChangeModified
			}
		}

		changes = append(changes, change)

		return nil
	})

	return changes, err
}
