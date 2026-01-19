//go:build !linux && !darwin

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

// IsMountPoint always returns false on non-Linux platforms
func IsMountPoint(_ string) bool {
	return false
}

// DetectStaleMounts is a no-op on non-Linux platforms
func DetectStaleMounts(_ string) ([]StaleMountInfo, error) {
	return nil, nil
}

// CleanupStaleMounts is a no-op on non-Linux platforms
func CleanupStaleMounts(_ string) (int, []error) {
	return 0, nil
}

// RecoverFromCrash is a no-op on non-Linux platforms
func RecoverFromCrash(_ string) (cleaned int, stale int, errors []error) {
	return 0, 0, nil
}
