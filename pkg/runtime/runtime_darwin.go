//go:build darwin

package runtime

import (
	"fmt"
	"os"
)

// baseRuntimeDir returns the base runtime directory for macOS/Darwin.
// Uses $TMPDIR/canopy-$UID since Darwin has no XDG equivalent.
func baseRuntimeDir() string {
	tmpdir := os.Getenv("TMPDIR")
	if tmpdir == "" {
		tmpdir = "/tmp"
	}
	return fmt.Sprintf("%s/canopy-%d", tmpdir, os.Getuid())
}
