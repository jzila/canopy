package sandbox

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileState represents the state of a file at snapshot time.
type FileState struct {
	ModTime time.Time
	Size    int64
	IsDir   bool
}

// Snapshot holds the state of a directory tree for change detection.
type Snapshot struct {
	Files    map[string]FileState
	RootDir  string
	TakenAt  time.Time
	Excludes []string
}

// Change represents a detected file change between snapshots.
// Uses ChangeType constants from overlay.go (ChangeCreated, ChangeModified, ChangeDeleted).
type Change struct {
	Path string
	Type ChangeType
}

// TakeSnapshot walks the directory tree and records mtime/size for each file.
// It respects the provided exclude patterns and built-in exclusions.
func TakeSnapshot(rootDir string, excludes []string) (*Snapshot, error) {
	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root directory: %w", err)
	}

	snapshot := &Snapshot{
		Files:    make(map[string]FileState),
		RootDir:  rootDir,
		TakenAt:  time.Now(),
		Excludes: excludes,
	}

	err = filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Skip inaccessible files/directories
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}

		// Skip root directory itself
		if relPath == "." {
			return nil
		}

		// Check if path should be excluded
		if shouldExclude(relPath, excludes) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			// Skip files we can't stat
			return nil
		}

		snapshot.Files[relPath] = FileState{
			ModTime: info.ModTime(),
			Size:    info.Size(),
			IsDir:   d.IsDir(),
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}

	return snapshot, nil
}

// Compare detects changes between two snapshots.
// Returns changes from the perspective of moving from s (before) to other (after).
func (s *Snapshot) Compare(other *Snapshot) []Change {
	var changes []Change

	// Detect created and modified files
	for path, afterState := range other.Files {
		beforeState, existed := s.Files[path]
		if !existed {
			changes = append(changes, Change{
				Path: path,
				Type: ChangeCreated,
			})
			continue
		}

		// Check for modification (mtime or size changed)
		if !afterState.ModTime.Equal(beforeState.ModTime) || afterState.Size != beforeState.Size {
			// Skip directories - we care about file content changes
			if !afterState.IsDir {
				changes = append(changes, Change{
					Path: path,
					Type: ChangeModified,
				})
			}
		}
	}

	// Detect deleted files
	for path := range s.Files {
		if _, exists := other.Files[path]; !exists {
			changes = append(changes, Change{
				Path: path,
				Type: ChangeDeleted,
			})
		}
	}

	return changes
}

// Refresh takes a new snapshot of the same directory with the same excludes.
func (s *Snapshot) Refresh() (*Snapshot, error) {
	return TakeSnapshot(s.RootDir, s.Excludes)
}

// shouldExclude checks if a relative path matches any exclude pattern.
// Supports exact matches, directory prefixes, and glob patterns.
func shouldExclude(relPath string, excludes []string) bool {
	// Always exclude .git directory
	if relPath == ".git" || strings.HasPrefix(relPath, ".git"+string(filepath.Separator)) {
		return true
	}

	// Check built-in home exclusions
	for _, excluded := range HomeExcludedPaths {
		if relPath == excluded || strings.HasPrefix(relPath, excluded+string(filepath.Separator)) {
			return true
		}
	}

	// Check built-in hidden paths
	for _, hidden := range DefaultHiddenPaths {
		if relPath == hidden || strings.HasPrefix(relPath, hidden+string(filepath.Separator)) {
			return true
		}
	}

	// Check custom excludes
	for _, pattern := range excludes {
		// Exact match
		if relPath == pattern {
			return true
		}

		// Directory prefix match
		if strings.HasPrefix(relPath, pattern+string(filepath.Separator)) {
			return true
		}

		// Glob pattern match
		if matched, _ := filepath.Match(pattern, relPath); matched {
			return true
		}

		// Also check just the filename for glob patterns
		if matched, _ := filepath.Match(pattern, filepath.Base(relPath)); matched {
			return true
		}
	}

	return false
}

// SnapshotOptions configures snapshot behavior.
type SnapshotOptions struct {
	// Excludes are additional patterns to exclude from the snapshot.
	Excludes []string

	// IncludeHidden, when true, includes hidden files (starting with .).
	// Default false excludes common hidden directories.
	IncludeHidden bool

	// FollowSymlinks, when true, follows symbolic links.
	// Default false records the symlink itself, not its target.
	FollowSymlinks bool
}

// TakeSnapshotWithOptions takes a snapshot with configurable options.
func TakeSnapshotWithOptions(rootDir string, opts SnapshotOptions) (*Snapshot, error) {
	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root directory: %w", err)
	}

	snapshot := &Snapshot{
		Files:    make(map[string]FileState),
		RootDir:  rootDir,
		TakenAt:  time.Now(),
		Excludes: opts.Excludes,
	}

	err = filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}

		if relPath == "." {
			return nil
		}

		if shouldExclude(relPath, opts.Excludes) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		var info fs.FileInfo
		if opts.FollowSymlinks && d.Type()&os.ModeSymlink != 0 {
			info, err = os.Stat(path)
			if err != nil {
				// Broken symlink, skip it
				return nil
			}
		} else {
			info, err = d.Info()
			if err != nil {
				return nil
			}
		}

		snapshot.Files[relPath] = FileState{
			ModTime: info.ModTime(),
			Size:    info.Size(),
			IsDir:   info.IsDir(),
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}

	return snapshot, nil
}

// FilterChanges returns only changes matching the given types.
func FilterChanges(changes []Change, types ...ChangeType) []Change {
	typeSet := make(map[ChangeType]bool)
	for _, t := range types {
		typeSet[t] = true
	}

	var filtered []Change
	for _, c := range changes {
		if typeSet[c.Type] {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// ChangedPaths extracts just the paths from a slice of changes.
func ChangedPaths(changes []Change) []string {
	paths := make([]string, len(changes))
	for i, c := range changes {
		paths[i] = c.Path
	}
	return paths
}
