//go:build !linux

package sandbox

import "fmt"

// Mount is not supported on non-Linux platforms
func (o *Overlay) Mount() error {
	return fmt.Errorf("OverlayFS is only supported on Linux")
}

// Unmount is not supported on non-Linux platforms
func (o *Overlay) Unmount() error {
	return fmt.Errorf("OverlayFS is only supported on Linux")
}
