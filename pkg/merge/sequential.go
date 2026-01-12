package merge

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/sandbox"
)

// isCanopyFile returns true if the path is a .canopy/ file that should be filtered out
func isCanopyFile(path string) bool {
	return path == ".canopy" || strings.HasPrefix(path, ".canopy/")
}

// isOverlayWhiteout returns true if the path is an overlayfs whiteout marker
// Whiteout files have format ".wh.<filename>" and mark deleted files in overlayfs
func isOverlayWhiteout(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, ".wh.")
}

// isTransientFile returns true if the path is a transient file that shouldn't be committed
func isTransientFile(path string) bool {
	base := filepath.Base(path)
	// Claude Code lock files
	if base == ".claude.json.lock" || base == ".claude.json.backup" {
		return true
	}
	return false
}

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
	Applied        []AppliedChange
	Conflicts      []Conflict
	Errors         []string
	CommitsApplied int             // Number of git commits applied
	BeadsSynced    bool            // Whether bd sync was run for beads updates
	PatchFailed    map[string]bool // Task IDs where git patch application failed
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
//
// Strategy:
// - If a worker made git commits, apply them via git am (preserves commit history)
// - If a worker only made file changes, apply them directly
// - Skip .beads directory (agents don't have access to it, orchestrator updates it)
func (m *SequentialMerger) Merge(results []*agent.Result) (*Result, error) {
	mergeResult := &Result{
		PatchFailed: make(map[string]bool),
	}

	// Separate results into those with commits and those with file changes
	var withCommits, withFileChanges []*agent.Result
	for _, r := range results {
		if !r.Success {
			continue
		}
		if r.GitState != nil && len(r.GitState.Patches) > 0 {
			withCommits = append(withCommits, r)
		}
		// Include all results with file changes, even if they also have commits
		// (commits may not include all file changes if agent made uncommitted edits)
		if len(r.Changes) > 0 {
			withFileChanges = append(withFileChanges, r)
		}
	}

	// Apply git patches first (these are the "proper" changes with commit history)
	patchedTasks := make(map[string]bool) // Track which tasks had patches applied successfully
	for _, r := range withCommits {
		if err := sandbox.ApplyPatches(m.outputDir, r.GitState.Patches); err != nil {
			mergeResult.Errors = append(mergeResult.Errors,
				fmt.Sprintf("failed to apply commits from %s: %v", r.TaskID, err))
			// Mark as patch failed so orchestrator can commit file changes instead
			mergeResult.PatchFailed[r.TaskID] = true
		} else {
			mergeResult.CommitsApplied += len(r.GitState.Patches)
			patchedTasks[r.TaskID] = true
			if m.verbose {
				fmt.Printf("Applied %d commits from task %s\n", len(r.GitState.Patches), r.TaskID)
			}
		}
	}

	// Track which files have been modified and by whom (for conflict detection)
	fileModifiers := make(map[string][]string) // path -> list of task IDs

	// First pass: detect conflicts and filter changes
	// NOTE: .beads changes are skipped since agents don't have access to .beads
	for _, r := range withFileChanges {
		for _, change := range r.Changes {
			// Skip .beads files (agents don't have access to .beads directory)
			if isBeadsFile(change.Path) {
				continue
			}
			// Skip .canopy files (resolver artifacts, conflict data)
			if isCanopyFile(change.Path) {
				continue
			}
			// Skip overlayfs whiteout markers
			if isOverlayWhiteout(change.Path) {
				continue
			}
			// Skip transient files (lock files, backups)
			if isTransientFile(change.Path) {
				continue
			}

			if change.Type != sandbox.ChangeDeleted {
				fileModifiers[change.Path] = append(fileModifiers[change.Path], r.TaskID)
			}
		}
	}

	// Report conflicts (but still apply using last-writer-wins)
	var userConflicts, vendorConflicts []Conflict
	for path, modifiers := range fileModifiers {
		if len(modifiers) > 1 {
			conflict := Conflict{
				Path:    path,
				Sources: modifiers,
			}
			mergeResult.Conflicts = append(mergeResult.Conflicts, conflict)

			if isVendorPath(path) {
				vendorConflicts = append(vendorConflicts, conflict)
			} else {
				userConflicts = append(userConflicts, conflict)
			}
		}
	}

	// Print conflict summary if verbose
	if m.verbose {
		// Show user conflicts (max 5)
		if len(userConflicts) > 0 {
			maxShow := 5
			for i, c := range userConflicts {
				if i >= maxShow {
					fmt.Printf("... and %d more user conflicts\n", len(userConflicts)-maxShow)
					break
				}
				fmt.Printf("Conflict: %s modified by %v (using last)\n", c.Path, c.Sources)
			}
		}

		// Show vendor conflicts as one-line summary
		if len(vendorConflicts) > 0 {
			fmt.Printf("%d vendor/module conflicts (expected when agents add dependencies)\n", len(vendorConflicts))
		}
	}

	// Second pass: apply file changes (later results overwrite earlier)
	for _, r := range withFileChanges {
		for _, change := range r.Changes {
			// Skip .beads files (agents don't have access to .beads)
			if isBeadsFile(change.Path) {
				continue
			}
			// Skip .canopy files (resolver artifacts, conflict data)
			if isCanopyFile(change.Path) {
				continue
			}
			// Skip overlayfs whiteout markers
			if isOverlayWhiteout(change.Path) {
				continue
			}
			// Skip transient files (lock files, backups)
			if isTransientFile(change.Path) {
				continue
			}

			applied, err := m.applyChange(r, change)
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

// isBeadsFile checks if a path is within the .beads directory
func isBeadsFile(path string) bool {
	return path == ".beads" || strings.HasPrefix(path, ".beads/")
}

// isVendorPath checks if a path is within vendor/dependency directories
func isVendorPath(path string) bool {
	// Normalize path separators for cross-platform compatibility
	normalizedPath := filepath.ToSlash(path)

	vendorPrefixes := []string{
		"go/pkg/mod/",
		"vendor/",
		"node_modules/",
	}

	for _, prefix := range vendorPrefixes {
		if strings.HasPrefix(normalizedPath, prefix) {
			return true
		}
	}

	return false
}

func (m *SequentialMerger) applyChange(result *agent.Result, change sandbox.FileChange) (*AppliedChange, error) {
	dstPath := filepath.Join(m.outputDir, change.Path)

	switch change.Type {
	case sandbox.ChangeDeleted:
		if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		return &AppliedChange{
			Path:   change.Path,
			Source: result.TaskID,
			Type:   sandbox.ChangeDeleted,
		}, nil

	case sandbox.ChangeCreated, sandbox.ChangeModified:
		// Source file is in the overlay's upper directory
		// Use the overlay from the result to get the correct path
		if result.Overlay == nil {
			return nil, fmt.Errorf("result missing overlay reference")
		}
		srcPath := result.Overlay.UpperPath(change.Path)

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
			Source: result.TaskID,
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

// MergeOptions configures the merge behavior
type MergeOptions struct {
	// SkipFileFallback skips the file-based fallback when git am fails.
	// Use this when a resolver agent will handle the conflict instead.
	SkipFileFallback bool
}

// MergeSingle merges and commits a single agent result atomically.
// This should be called immediately when each agent completes.
// The overlay must still be mounted when this is called.
func (m *SequentialMerger) MergeSingle(result *agent.Result, opts *MergeOptions) (*Result, error) {
	if opts == nil {
		opts = &MergeOptions{}
	}
	mergeResult := &Result{
		PatchFailed: make(map[string]bool),
	}

	if !result.Success {
		return mergeResult, nil
	}

	// Apply git patches first (these preserve the agent's commit history)
	if result.GitState != nil && len(result.GitState.Patches) > 0 {
		if err := sandbox.ApplyPatches(m.outputDir, result.GitState.Patches); err != nil {
			mergeResult.Errors = append(mergeResult.Errors,
				fmt.Sprintf("failed to apply commits from %s: %v", result.TaskID, err))
			mergeResult.PatchFailed[result.TaskID] = true
		} else {
			mergeResult.CommitsApplied += len(result.GitState.Patches)
			if m.verbose {
				fmt.Printf("Applied %d commits from task %s\n", len(result.GitState.Patches), result.TaskID)
			}
			// Patches applied successfully - no need to commit file changes
			// since they're already committed via git am
			return mergeResult, nil
		}
	}

	// If we get here, either there were no git patches, or patch application failed
	// Skip file-based fallback if requested (resolver will handle it)
	if opts.SkipFileFallback && mergeResult.PatchFailed[result.TaskID] {
		if m.verbose {
			fmt.Printf("Skipping file-based fallback for task %s (resolver will handle)\n", result.TaskID)
		}
		return mergeResult, nil
	}

	// Apply file changes from the overlay
	if len(result.Changes) > 0 {
		var paths []string
		for _, change := range result.Changes {
			// Skip .beads files
			if isBeadsFile(change.Path) {
				continue
			}
			// Skip .canopy files (resolver artifacts, conflict data)
			if isCanopyFile(change.Path) {
				continue
			}
			// Skip overlayfs whiteout markers
			if isOverlayWhiteout(change.Path) {
				continue
			}
			// Skip transient files (lock files, backups)
			if isTransientFile(change.Path) {
				continue
			}

			applied, err := m.applyChange(result, change)
			if err != nil {
				mergeResult.Errors = append(mergeResult.Errors,
					fmt.Sprintf("failed to apply %s from %s: %v", change.Path, result.TaskID, err))
				continue
			}
			if applied != nil {
				mergeResult.Applied = append(mergeResult.Applied, *applied)
				paths = append(paths, change.Path)
			}
		}

		// Commit the file changes
		if len(paths) > 0 {
			if err := m.commitFileChanges(result, paths, mergeResult.PatchFailed[result.TaskID]); err != nil {
				mergeResult.Errors = append(mergeResult.Errors,
					fmt.Sprintf("failed to commit changes from %s: %v", result.TaskID, err))
			} else {
				mergeResult.CommitsApplied++
			}
		}
	}

	return mergeResult, nil
}

// commitFileChanges stages and commits file changes for a single task
func (m *SequentialMerger) commitFileChanges(result *agent.Result, paths []string, patchFailed bool) error {
	if len(paths) == 0 {
		return nil
	}

	// Stage the files
	addCmd := exec.Command("git", "add", "--")
	addCmd.Args = append(addCmd.Args, paths...)
	addCmd.Dir = m.outputDir
	var addStderr bytes.Buffer
	addCmd.Stderr = &addStderr
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add failed: %w: %s", err, addStderr.String())
	}

	// Check if there are staged changes
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = m.outputDir
	if err := diffCmd.Run(); err == nil {
		// No staged changes (exit code 0 means no diff)
		if m.verbose {
			fmt.Printf("No changes to commit for task %s (files may have been committed via git am)\n", result.TaskID)
		}
		return nil
	}

	// Create commit message
	commitMsg := fmt.Sprintf("canopy: apply changes from %s", result.TaskID)
	if patchFailed {
		commitMsg = fmt.Sprintf("canopy: apply changes from %s (git patch failed, using file-based merge)", result.TaskID)
	}

	commitCmd := exec.Command("git", "commit", "-m", commitMsg)
	commitCmd.Dir = m.outputDir
	var commitStderr bytes.Buffer
	commitCmd.Stderr = &commitStderr
	if err := commitCmd.Run(); err != nil {
		// Commit failed - reset staged files to prevent leaving dirty state
		resetCmd := exec.Command("git", "reset", "HEAD", "--")
		resetCmd.Args = append(resetCmd.Args, paths...)
		resetCmd.Dir = m.outputDir
		if resetErr := resetCmd.Run(); resetErr != nil && m.verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to reset staged files after commit failure: %v\n", resetErr)
		}
		return fmt.Errorf("git commit failed: %w: %s", err, commitStderr.String())
	}

	if m.verbose {
		if patchFailed {
			fmt.Printf("Created commit for file changes from task %s (patch application failed)\n", result.TaskID)
		} else {
			fmt.Printf("Created commit for file-only changes from task %s\n", result.TaskID)
		}
	}

	return nil
}
