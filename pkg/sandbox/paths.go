// Package sandbox provides overlay filesystem utilities for agent isolation.
package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
)

// GetOverlayBaseDir returns the base directory for overlay mounts.
// Per the persistence invariant in AGENTS.md, ALL canopy state must live at
// $XDG_CACHE_HOME/canopy/ or ~/.cache/canopy/
//
// This returns the "overlays" subdirectory for overlay filesystem mounts:
// - $XDG_CACHE_HOME/canopy/overlays/ if XDG_CACHE_HOME is set
// - ~/.cache/canopy/overlays/ otherwise
func GetOverlayBaseDir() (string, error) {
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		cacheDir = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheDir, "canopy", "overlays"), nil
}
