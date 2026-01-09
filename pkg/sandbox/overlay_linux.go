//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Mount mounts the overlay filesystem
// Tries kernel overlayfs first, falls back to fuse-overlayfs for rootless operation
func (o *Overlay) Mount() error {
	if o.mounted {
		return fmt.Errorf("overlay %s already mounted", o.ID)
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
		return o.mountPassthroughs()
	}

	// Try with userxattr for rootless (kernel >= 5.11)
	mountOptsUserXattr := mountOpts + ",userxattr"
	err = syscall.Mount("overlay", o.MergedDir, "overlay", 0, mountOptsUserXattr)
	if err == nil {
		o.mounted = true
		o.useFuse = false
		return o.mountPassthroughs()
	}

	// Fall back to fuse-overlayfs
	if err := o.mountFuse(); err != nil {
		return err
	}
	return o.mountPassthroughs()
}

// mountPassthroughs bind-mounts directories that should bypass the overlay
func (o *Overlay) mountPassthroughs() error {
	for _, relPath := range DefaultPassthroughPaths {
		srcPath := filepath.Join(o.LowerDir, relPath)
		dstPath := filepath.Join(o.MergedDir, relPath)

		// Only mount if source exists
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			continue
		}

		// Ensure destination exists
		if err := os.MkdirAll(dstPath, 0755); err != nil {
			return fmt.Errorf("failed to create passthrough mount point %s: %w", dstPath, err)
		}

		// Bind mount
		if err := syscall.Mount(srcPath, dstPath, "", syscall.MS_BIND, ""); err != nil {
			return fmt.Errorf("failed to bind mount %s: %w", relPath, err)
		}

		o.bindMounts = append(o.bindMounts, dstPath)
	}
	return nil
}

// unmountPassthroughs unmounts all bind-mounted directories
func (o *Overlay) unmountPassthroughs() {
	// Unmount in reverse order
	for i := len(o.bindMounts) - 1; i >= 0; i-- {
		syscall.Unmount(o.bindMounts[i], syscall.MNT_DETACH)
	}
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

	var err error
	if o.useFuse {
		err = exec.Command("fusermount", "-u", o.MergedDir).Run()
	} else {
		// Try lazy unmount first
		err = syscall.Unmount(o.MergedDir, syscall.MNT_DETACH)
	}

	if err != nil {
		// Force unmount as last resort
		_ = exec.Command("umount", "-l", o.MergedDir).Run()
	}

	o.mounted = false
	return err
}
