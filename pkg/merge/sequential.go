package merge

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/sandbox"
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
	Applied        []AppliedChange
	Conflicts      []Conflict
	Errors         []string
	CommitsApplied int  // Number of git commits applied
	BeadsSynced    bool // Whether bd sync was run for beads updates
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
// - Handle .beads directory specially: apply changes sequentially and run bd sync
func (m *SequentialMerger) Merge(results []*agent.Result) (*Result, error) {
	mergeResult := &Result{}

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
			// Don't mark as patched - will fall back to file-based merge
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

	// Separate .beads changes from regular file changes
	var beadsChanges []beadsChange
	var regularChanges []*agent.Result

	// First pass: detect conflicts and separate .beads changes
	for _, r := range withFileChanges {
		hasBeadsChanges := false
		hasRegularChanges := false

		for _, change := range r.Changes {
			// Check if this is a .beads file
			if isBeadsFile(change.Path) {
				hasBeadsChanges = true
				beadsChanges = append(beadsChanges, beadsChange{
					result: r,
					change: change,
				})
			} else {
				hasRegularChanges = true
				if change.Type != sandbox.ChangeDeleted {
					fileModifiers[change.Path] = append(fileModifiers[change.Path], r.TaskID)
				}
			}
		}

		// Track results with regular (non-beads) changes
		if hasRegularChanges {
			regularChanges = append(regularChanges, r)
		}

		// Log if worker made both kinds of changes
		if hasBeadsChanges && m.verbose {
			fmt.Printf("Task %s made changes to .beads directory\n", r.TaskID)
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

	// Second pass: apply regular file changes (later results overwrite earlier)
	for _, r := range regularChanges {
		for _, change := range r.Changes {
			// Skip .beads files - they're handled separately
			if isBeadsFile(change.Path) {
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

	// Apply .beads changes sequentially and sync
	if len(beadsChanges) > 0 {
		if err := m.mergeBeadsChanges(beadsChanges, mergeResult); err != nil {
			mergeResult.Errors = append(mergeResult.Errors,
				fmt.Sprintf("failed to merge beads changes: %v", err))
		}
	}

	return mergeResult, nil
}

// beadsChange pairs a result with a specific .beads file change
type beadsChange struct {
	result *agent.Result
	change sandbox.FileChange
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

// mergeBeadsChanges applies .beads changes sequentially and runs bd sync
func (m *SequentialMerger) mergeBeadsChanges(changes []beadsChange, mergeResult *Result) error {
	if m.verbose {
		fmt.Printf("Merging %d .beads file changes from %d tasks\n",
			len(changes), countUniqueResults(changes))
	}

	// Apply each .beads change in order
	for _, bc := range changes {
		applied, err := m.applyChange(bc.result, bc.change)
		if err != nil {
			return fmt.Errorf("failed to apply %s from %s: %w", bc.change.Path, bc.result.TaskID, err)
		}
		if applied != nil {
			mergeResult.Applied = append(mergeResult.Applied, *applied)
		}
	}

	// Run bd sync to merge beads state and ensure consistency
	if err := m.runBeadsSync(); err != nil {
		return fmt.Errorf("bd sync failed: %w", err)
	}

	mergeResult.BeadsSynced = true
	if m.verbose {
		fmt.Println("Successfully synced beads state")
	}

	return nil
}

// runBeadsSync executes bd sync to merge beads state
func (m *SequentialMerger) runBeadsSync() error {
	cmd := exec.Command("bd", "sync", "--import-only")
	cmd.Dir = m.outputDir

	// Capture output for debugging
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(output))
	}

	if m.verbose && len(output) > 0 {
		fmt.Printf("bd sync output: %s\n", string(output))
	}

	// Check for conflict markers in .beads files and resolve them
	if err := m.resolveBeadsConflicts(); err != nil {
		return fmt.Errorf("failed to resolve beads conflicts: %w", err)
	}

	return nil
}

// resolveBeadsConflicts checks for conflict markers in .beads files and resolves them
func (m *SequentialMerger) resolveBeadsConflicts() error {
	beadsDir := filepath.Join(m.outputDir, ".beads")

	// Check if .beads directory exists
	if _, err := os.Stat(beadsDir); os.IsNotExist(err) {
		return nil // No .beads directory, nothing to do
	}

	// Files that could have conflicts
	filesToCheck := []string{
		filepath.Join(beadsDir, "issues.jsonl"),
		filepath.Join(beadsDir, "beads.jsonl"),
		filepath.Join(beadsDir, "interactions.jsonl"),
	}

	// Check each file for conflict markers
	for _, filePath := range filesToCheck {
		hasConflicts, err := hasConflictMarkers(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // File doesn't exist, skip it
			}
			return fmt.Errorf("failed to check %s for conflicts: %w", filePath, err)
		}

		if hasConflicts {
			if m.verbose {
				fmt.Printf("Detected conflict markers in %s, running bd resolve-conflicts\n", filePath)
			}

			// Run bd resolve-conflicts to fix the file
			cmd := exec.Command("bd", "resolve-conflicts", filePath)
			cmd.Dir = m.outputDir

			output, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("bd resolve-conflicts failed for %s: %w: %s", filePath, err, string(output))
			}

			if m.verbose && len(output) > 0 {
				fmt.Printf("bd resolve-conflicts output: %s\n", string(output))
			}
		}
	}

	return nil
}

// hasConflictMarkers checks if a file contains git conflict markers
func hasConflictMarkers(filePath string) (bool, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return false, err
	}

	// Check for standard git conflict markers
	markers := []string{"<<<<<<<", "=======", ">>>>>>>"}
	for _, marker := range markers {
		if strings.Contains(string(content), marker) {
			return true, nil
		}
	}

	return false, nil
}

// countUniqueResults counts unique results in beadsChanges
func countUniqueResults(changes []beadsChange) int {
	seen := make(map[string]bool)
	for _, bc := range changes {
		seen[bc.result.TaskID] = true
	}
	return len(seen)
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
