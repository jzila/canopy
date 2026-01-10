package sandbox

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileChange represents a modification in the overlay
type FileChange struct {
	Path    string
	Type    ChangeType
	NewHash string
}

// ChangeType indicates the type of file change
type ChangeType string

const (
	ChangeCreated  ChangeType = "created"
	ChangeModified ChangeType = "modified"
	ChangeDeleted  ChangeType = "deleted"
)

// Overlay represents an OverlayFS mount for agent isolation
type Overlay struct {
	ID         string
	LowerDir   string   // Read-only base (original working directory)
	UpperDir   string   // Writable layer (per-agent changes)
	WorkDir    string   // OverlayFS internal workdir
	MergedDir  string   // Combined view where agent operates
	mounted    bool
	useFuse    bool
	bindMounts []string // Paths that are bind-mounted through the overlay
}

// DefaultPassthroughPaths are directories that should bypass the overlay
// and write directly to the original filesystem
var DefaultPassthroughPaths = []string{
	".beads",
}

// DefaultHiddenPaths are paths that should be hidden from agents via whiteout
// These won't be visible in the merged view even if they exist in lowerdir
var DefaultHiddenPaths = []string{
	".claude", // Hide parent session settings and approved commands
}

// NewOverlay creates a new overlay filesystem structure
func NewOverlay(baseDir, lowerDir string) (*Overlay, error) {
	id := generateID()

	overlay := &Overlay{
		ID:        id,
		LowerDir:  lowerDir,
		UpperDir:  filepath.Join(baseDir, id, "upper"),
		WorkDir:   filepath.Join(baseDir, id, "work"),
		MergedDir: filepath.Join(baseDir, id, "merged"),
	}

	// Create directory structure
	for _, dir := range []string{overlay.UpperDir, overlay.WorkDir, overlay.MergedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			overlay.Cleanup()
			return nil, fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	// Create whiteouts for hidden paths
	if err := overlay.createWhiteouts(); err != nil {
		overlay.Cleanup()
		return nil, fmt.Errorf("failed to create whiteouts: %w", err)
	}

	// Copy Claude credentials to upper dir so agents can authenticate
	if err := overlay.copyClaudeCredentials(); err != nil {
		// Non-fatal - agent might work with env vars
		// Just log the error if verbose mode is enabled elsewhere
		_ = err
	}

	return overlay, nil
}

// createWhiteouts creates whiteout entries in the upper directory to hide
// sensitive paths from agents. Uses OverlayFS whiteout convention.
func (o *Overlay) createWhiteouts() error {
	for _, hiddenPath := range DefaultHiddenPaths {
		lowerPath := filepath.Join(o.LowerDir, hiddenPath)

		// Only create whiteout if path exists in lower
		info, err := os.Stat(lowerPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat %s: %w", hiddenPath, err)
		}

		if info.IsDir() {
			// For directories: create opaque directory with .wh..wh..opq marker
			upperDir := filepath.Join(o.UpperDir, hiddenPath)
			if err := os.MkdirAll(upperDir, 0755); err != nil {
				return fmt.Errorf("create upper dir %s: %w", hiddenPath, err)
			}
			opaquePath := filepath.Join(upperDir, ".wh..wh..opq")
			if err := os.WriteFile(opaquePath, nil, 0644); err != nil {
				return fmt.Errorf("create opaque marker for %s: %w", hiddenPath, err)
			}
		} else {
			// For files: create .wh.<filename> whiteout
			dir := filepath.Dir(hiddenPath)
			if dir != "." {
				if err := os.MkdirAll(filepath.Join(o.UpperDir, dir), 0755); err != nil {
					return fmt.Errorf("create parent dir for whiteout: %w", err)
				}
			}
			whiteoutPath := filepath.Join(o.UpperDir, dir, ".wh."+filepath.Base(hiddenPath))
			if err := os.WriteFile(whiteoutPath, nil, 0644); err != nil {
				return fmt.Errorf("create whiteout for %s: %w", hiddenPath, err)
			}
		}
	}
	return nil
}

// copyClaudeCredentials copies Claude CLI credentials from the user's home
// directory to the overlay's upper directory so agents can authenticate.
// This is necessary because .claude is whiteout'd from the lowerdir.
func (o *Overlay) copyClaudeCredentials() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get user home dir: %w", err)
	}

	// Claude credentials can be in multiple locations
	credentialPaths := []string{
		".claude.json",
		".claude/.credentials.json",
		".claude/settings.json",
	}

	claudeDir := filepath.Join(o.UpperDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("create .claude dir in upper: %w", err)
	}

	// Copy each credential file if it exists
	for _, credPath := range credentialPaths {
		srcPath := filepath.Join(homeDir, credPath)
		dstPath := filepath.Join(o.UpperDir, credPath)

		// Check if source file exists
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			continue
		}

		// Create parent directory in upper if needed
		dstDir := filepath.Dir(dstPath)
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return fmt.Errorf("create parent dir for %s: %w", credPath, err)
		}

		// Copy the file
		if err := copyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("copy %s: %w", credPath, err)
		}
	}

	// Also copy the entire statsig directory if it exists
	statsigSrc := filepath.Join(homeDir, ".claude", "statsig")
	statsigDst := filepath.Join(claudeDir, "statsig")
	if _, err := os.Stat(statsigSrc); err == nil {
		if err := copyDir(statsigSrc, statsigDst); err != nil {
			// Non-fatal, statsig is just for telemetry
			_ = err
		}
	}

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Get source file mode
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// copyDir recursively copies a directory
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path from src
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return copyFile(path, dstPath)
	})
}

// generateID creates a random hex ID
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// GetChanges extracts all file changes from the overlay's upper directory
func (o *Overlay) GetChanges() ([]FileChange, error) {
	var changes []FileChange

	err := filepath.WalkDir(o.UpperDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory
		if path == o.UpperDir {
			return nil
		}

		// Get relative path
		relPath, _ := filepath.Rel(o.UpperDir, path)

		// Skip passthrough paths (they're bind-mounted, not overlayed)
		for _, passthrough := range DefaultPassthroughPaths {
			if relPath == passthrough || strings.HasPrefix(relPath, passthrough+string(filepath.Separator)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip hidden paths (they're whiteouts we created, not real changes)
		for _, hidden := range DefaultHiddenPaths {
			if relPath == hidden || strings.HasPrefix(relPath, hidden+string(filepath.Separator)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Check for whiteout files (deleted files in overlay)
		// Whiteout files have names prefixed with .wh.
		if strings.HasPrefix(d.Name(), ".wh.") {
			originalName := strings.TrimPrefix(d.Name(), ".wh.")
			changes = append(changes, FileChange{
				Path: filepath.Join(filepath.Dir(relPath), originalName),
				Type: ChangeDeleted,
			})
			return nil
		}

		// Skip directories (we track file contents)
		if d.IsDir() {
			return nil
		}

		// Check if file exists in lower (original)
		lowerPath := filepath.Join(o.LowerDir, relPath)
		_, lowerErr := os.Stat(lowerPath)

		change := FileChange{Path: relPath}

		if os.IsNotExist(lowerErr) {
			change.Type = ChangeCreated
		} else {
			change.Type = ChangeModified
		}

		change.NewHash, _ = hashFile(path)
		changes = append(changes, change)

		return nil
	})

	return changes, err
}

// hashFile computes SHA256 hash of a file
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// Cleanup removes all overlay directories
func (o *Overlay) Cleanup() error {
	if o.mounted {
		if err := o.Unmount(); err != nil {
			return err
		}
	}

	// Remove the overlay directory tree
	baseDir := filepath.Dir(o.UpperDir)
	return os.RemoveAll(baseDir)
}

// IsMounted returns whether the overlay is currently mounted
func (o *Overlay) IsMounted() bool {
	return o.mounted
}

// UpperPath returns the full path to a file in the upper directory
func (o *Overlay) UpperPath(relPath string) string {
	return filepath.Join(o.UpperDir, relPath)
}
