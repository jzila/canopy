// Package runtime provides cross-platform runtime directory utilities.
//
// On Linux, uses $XDG_RUNTIME_DIR (typically /run/user/$UID).
// On macOS/Darwin, uses $TMPDIR/canopy-$UID (no XDG equivalent).
// Falls back to $TMPDIR/canopy-$UID on Linux if XDG_RUNTIME_DIR is unset.
package runtime

import (
	"os"
	"path/filepath"
)

const (
	// dirName is the canopy subdirectory name within the runtime dir
	dirName = "canopy"
	// socketName is the default socket filename
	socketName = "canopy.sock"
	// DefaultDaemonPort is the default HTTP port for the canopy daemon
	DefaultDaemonPort = 8080
)

// RuntimeDir returns the platform-appropriate runtime directory for canopy.
// The returned path includes the canopy subdirectory.
func RuntimeDir() string {
	return filepath.Join(baseRuntimeDir(), dirName)
}

// SocketPath returns the full path to a named socket within the runtime directory.
// If name is empty, returns the default socket path (canopy.sock).
func SocketPath(name string) string {
	if name == "" {
		name = socketName
	}
	return filepath.Join(RuntimeDir(), name)
}

// EnsureDir creates the runtime directory with 0700 permissions if it doesn't exist.
// Returns an error if the directory cannot be created or has incorrect permissions.
func EnsureDir() error {
	dir := RuntimeDir()
	return os.MkdirAll(dir, 0700)
}
