//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// IsStub returns false on Linux where overlay is supported
func IsStub() bool {
	return false
}

// Mount mounts the overlay filesystem
// Tries kernel overlayfs first, falls back to fuse-overlayfs for rootless operation
func (o *Overlay) Mount() error {
	if o.mounted {
		return fmt.Errorf("overlay %s already mounted", o.ID)
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
	if err := o.mountFuse(); err != nil {
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

// mountFuse uses fuse-overlayfs for rootless operation
func (o *Overlay) mountFuse() error {
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

// Unmount unmounts the overlay filesystem
func (o *Overlay) Unmount() error {
	if !o.mounted {
		return nil
	}

	// Unmount bind mounts first
	o.unmountPassthroughs()

	// Try unmounting with retries
	const maxRetries = 3
	const retryDelay = 100 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}

		// Try primary unmount method
		var err error
		if o.useFuse {
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
	// Use findmnt to check if the path is a mount point
	cmd := exec.Command("findmnt", "-n", "-o", "TARGET", o.MergedDir)
	output, err := cmd.Output()
	if err != nil {
		// findmnt returns non-zero if not found, which means not mounted
		return false
	}
	// If findmnt found it, it's mounted
	return len(output) > 0
}
