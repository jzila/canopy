//go:build !linux

package sandbox

import "fmt"

// IsStub returns true on non-Linux platforms where overlay is not supported
func IsStub() bool {
	return true
}

// Mount is not supported on non-Linux platforms
func (o *Overlay) Mount() error {
	return fmt.Errorf("OverlayFS is only supported on Linux")
}

// Unmount is not supported on non-Linux platforms
func (o *Overlay) Unmount() error {
	return fmt.Errorf("OverlayFS is only supported on Linux")
}

// isMounted always returns false on non-Linux platforms
func (o *Overlay) isMounted() bool {
	return false
}
