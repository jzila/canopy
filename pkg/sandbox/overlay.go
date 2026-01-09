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

	return overlay, nil
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
