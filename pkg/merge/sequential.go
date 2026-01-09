package merge

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/john/canopy/pkg/agent"
	"github.com/john/canopy/pkg/sandbox"
)

// AppliedChange records a change that was applied during merge
type AppliedChange struct {
	Path   string
	Source string // Task ID that made the change
	Type   sandbox.ChangeType
}

// Conflict records when multiple tasks modified the same file
type Conflict struct {
	Path    string
	Sources []string // Task IDs that modified this file
}

// Result holds the outcome of a merge operation
type Result struct {
	Applied   []AppliedChange
	Conflicts []Conflict
	Errors    []string
}

// SequentialMerger applies changes in order, later overwrites earlier
type SequentialMerger struct {
	outputDir string
	tempDir   string
	verbose   bool
}

// NewSequentialMerger creates a new sequential merger
func NewSequentialMerger(outputDir, tempDir string, verbose bool) *SequentialMerger {
	return &SequentialMerger{
		outputDir: outputDir,
		tempDir:   tempDir,
		verbose:   verbose,
	}
}

// Merge applies changes from multiple agent results to the output directory
// Results should be provided in completion order; later results take precedence
func (m *SequentialMerger) Merge(results []*agent.Result) (*Result, error) {
	mergeResult := &Result{}

	// Track which files have been modified and by whom (for conflict detection within same batch)
	fileModifiers := make(map[string][]string) // path -> list of task IDs

	// First pass: detect conflicts (same file modified by multiple tasks in this batch)
	for _, r := range results {
		if !r.Success || len(r.Changes) == 0 {
			continue
		}

		for _, change := range r.Changes {
			if change.Type == sandbox.ChangeDeleted {
				continue
			}
			fileModifiers[change.Path] = append(fileModifiers[change.Path], r.TaskID)
		}
	}

	// Report conflicts (but still apply using last-writer-wins)
	for path, modifiers := range fileModifiers {
		if len(modifiers) > 1 {
			mergeResult.Conflicts = append(mergeResult.Conflicts, Conflict{
				Path:    path,
				Sources: modifiers,
			})
			if m.verbose {
				fmt.Printf("Conflict: %s modified by %v (using last)\n", path, modifiers)
			}
		}
	}

	// Second pass: apply changes (later results overwrite earlier)
	for _, r := range results {
		if !r.Success || len(r.Changes) == 0 {
			continue
		}

		for _, change := range r.Changes {
			applied, err := m.applyChange(r.TaskID, change)
			if err != nil {
				mergeResult.Errors = append(mergeResult.Errors,
					fmt.Sprintf("failed to apply %s from %s: %v", change.Path, r.TaskID, err))
				continue
			}
			if applied != nil {
				mergeResult.Applied = append(mergeResult.Applied, *applied)
			}
		}
	}

	return mergeResult, nil
}

func (m *SequentialMerger) applyChange(taskID string, change sandbox.FileChange) (*AppliedChange, error) {
	dstPath := filepath.Join(m.outputDir, change.Path)

	switch change.Type {
	case sandbox.ChangeDeleted:
		if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		return &AppliedChange{
			Path:   change.Path,
			Source: taskID,
			Type:   sandbox.ChangeDeleted,
		}, nil

	case sandbox.ChangeCreated, sandbox.ChangeModified:
		// Source file is in the task's upper directory
		srcPath := filepath.Join(m.tempDir, taskID, "upper", change.Path)

		// Ensure destination directory exists
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return nil, err
		}

		// Copy the file
		if err := copyFile(srcPath, dstPath); err != nil {
			return nil, err
		}

		return &AppliedChange{
			Path:   change.Path,
			Source: taskID,
			Type:   change.Type,
		}, nil
	}

	return nil, nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

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
