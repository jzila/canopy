//go:build linux

package sandbox

import (
	"fmt"
	"os/exec"
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
	return o.mountFuse()
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
