//go:build linux

package runtime

import (
	"fmt"
	"os"
)

// baseRuntimeDir returns the base runtime directory for Linux.
// Uses $XDG_RUNTIME_DIR if set, otherwise falls back to $TMPDIR/canopy-$UID.
func baseRuntimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir
	}
	return fallbackDir()
}

// fallbackDir returns $TMPDIR/canopy-$UID (or /tmp/canopy-$UID if TMPDIR unset)
func fallbackDir() string {
	tmpdir := os.Getenv("TMPDIR")
	if tmpdir == "" {
		tmpdir = "/tmp"
	}
	return fmt.Sprintf("%s/canopy-%d", tmpdir, os.Getuid())
}
