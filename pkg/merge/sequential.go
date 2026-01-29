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

	// MergedCommits contains information about commits created in the repo during merge.
	// These are the actual commits after git am/commit, with hashes that exist in the repo.
	// Use this instead of GitState.NewCommits from agent results, as those are overlay
	// commits with different hashes that may no longer exist after merge.
	MergedCommits []*sandbox.CommitInfo

	// PreCommitHookFailed indicates that git commit failed due to a pre-commit hook.
	// When true, the staged changes are kept (not reset) so a repair agent can fix
	// the issue and retry the commit.
	PreCommitHookFailed bool

	// PreCommitHookOutput contains the output from the failed pre-commit hook.
	// This provides context for the repair agent about what needs to be fixed.
	PreCommitHookOutput string

	// StagedPaths contains the paths that were staged for commit when the pre-commit
	// hook failed. These are needed by the repair agent to retry the commit.
	StagedPaths []string
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
	defer func() { _ = srcFile.Close() }()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// MergeOptions configures the merge behavior
type MergeOptions struct {
	// TaskTitle is the task title from beads, used to generate commit messages
	// when the agent didn't make commits with messages.
	TaskTitle string
}

// commitResult holds the outcome of a commit attempt
type commitResult struct {
	// Committed is true if a commit was successfully created
	Committed bool
	// PreCommitHookFailed is true if the commit failed due to a pre-commit hook
	PreCommitHookFailed bool
	// HookOutput contains the output from the failed pre-commit hook
	HookOutput string
	// Error contains any error that occurred (nil if pre-commit hook failed, as that's handled specially)
	Error error
}

// MergeSingle merges and commits a single agent result atomically.
// This should be called immediately when each agent completes.
// The overlay must still be mounted when this is called.
//
// Always uses file-based merge to create a single commit per agent containing ALL changes.
// On failure, the working directory is reset to HEAD state to prevent partial changes.
// Exception: if a pre-commit hook fails, staged changes are kept so a repair agent can fix
// the issue (PreCommitHookFailed will be true in the result).
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

	// Record HEAD before merge so we can reset on failure
	headCommit, err := m.getCurrentHead()
	if err != nil {
		mergeResult.Errors = append(mergeResult.Errors,
			fmt.Sprintf("failed to get HEAD: %v", err))
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

		// Commit all changes in one commit
		if len(paths) > 0 {
			// Build commit message including agent's original commit messages if any
			commitMsg := m.buildMergeCommitMessage(result, opts)
			cr := m.commitFileChanges(result, paths, commitMsg)

			if cr.PreCommitHookFailed {
				// Pre-commit hook failed - DON'T reset staged changes
				// Signal this to the caller so they can spawn a repair agent
				mergeResult.PreCommitHookFailed = true
				mergeResult.PreCommitHookOutput = cr.HookOutput
				mergeResult.StagedPaths = paths
				// Don't add to Errors - this is a special case that should be handled differently
				if m.verbose {
					fmt.Printf("Pre-commit hook failed for task %s, staged changes preserved for repair agent\n", result.TaskID)
				}
			} else if cr.Error != nil {
				// Some other commit error - reset working directory to clean HEAD state
				// This prevents partial changes from affecting subsequent operations
				if resetErr := m.resetToHead(headCommit, paths); resetErr != nil && m.verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to reset working directory after merge failure: %v\n", resetErr)
				}
				mergeResult.Errors = append(mergeResult.Errors,
					fmt.Sprintf("failed to commit changes from %s: %v", result.TaskID, cr.Error))
			} else if cr.Committed {
				mergeResult.CommitsApplied++

				// Capture the new HEAD commit info for the dashboard
				// This is the actual merged commit with the hash that exists in the repo
				newHead, err := m.getCurrentHead()
				if err == nil {
					commitInfo, err := sandbox.GetCommitInfoFromDirWithOptions(m.outputDir, newHead, sandbox.CommitInfoOptions{IncludePatch: true})
					if err == nil {
						mergeResult.MergedCommits = append(mergeResult.MergedCommits, commitInfo)
					} else if m.verbose {
						fmt.Fprintf(os.Stderr, "warning: failed to get commit info for %s: %v\n", newHead, err)
					}
				} else if m.verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to get HEAD after commit: %v\n", err)
				}
			}
		}
	}

	return mergeResult, nil
}

// getCurrentHead returns the current HEAD commit hash
func (m *SequentialMerger) getCurrentHead() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = m.outputDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// resetToHead restores the working directory to a clean HEAD state.
// This is called when merge fails to ensure no partial changes remain.
func (m *SequentialMerger) resetToHead(headCommit string, paths []string) error {
	// First, reset any staged changes
	resetCmd := exec.Command("git", "reset", "HEAD", "--")
	resetCmd.Args = append(resetCmd.Args, paths...)
	resetCmd.Dir = m.outputDir
	var resetStderr bytes.Buffer
	resetCmd.Stderr = &resetStderr
	if err := resetCmd.Run(); err != nil {
		// Non-fatal: continue to checkout even if reset fails
		if m.verbose {
			fmt.Fprintf(os.Stderr, "warning: git reset failed: %v: %s\n", err, resetStderr.String())
		}
	}

	// Restore working directory files from HEAD
	// This ensures files are back to their pre-merge state
	checkoutCmd := exec.Command("git", "checkout", headCommit, "--")
	checkoutCmd.Args = append(checkoutCmd.Args, paths...)
	checkoutCmd.Dir = m.outputDir
	var checkoutStderr bytes.Buffer
	checkoutCmd.Stderr = &checkoutStderr
	if err := checkoutCmd.Run(); err != nil {
		return fmt.Errorf("git checkout failed: %w: %s", err, checkoutStderr.String())
	}

	return nil
}

// buildMergeCommitMessage creates a commit message for merged changes.
// Uses the agent's original commit message(s) with bead ID appended.
// Format: "<original message> (<bead-id>)" or combined messages if multiple commits.
// Falls back to generating a message from the task title if no agent commits exist.
func (m *SequentialMerger) buildMergeCommitMessage(result *agent.Result, opts *MergeOptions) string {
	// Use BeadID if set (repair/resolver agents set this to the original bead ID),
	// otherwise fall back to TaskID (for normal agents, TaskID is the bead ID).
	beadID := result.BeadID
	if beadID == "" {
		beadID = result.TaskID
	}

	// If agent made commits, use their message(s) as the primary content
	if result.GitState != nil && len(result.GitState.CommitMessages) > 0 {
		if len(result.GitState.CommitMessages) == 1 {
			// Single commit: use its message directly with bead ID
			msg := strings.TrimSpace(result.GitState.CommitMessages[0])
			return appendBeadID(msg, beadID)
		}

		// Multiple commits: combine with primary (first) in title, rest in body
		firstMsg := strings.TrimSpace(result.GitState.CommitMessages[0])
		title := appendBeadID(getFirstLine(firstMsg), beadID)

		var body strings.Builder
		body.WriteString("\n\n")
		// Include all commit messages for context
		for i, msg := range result.GitState.CommitMessages {
			body.WriteString(fmt.Sprintf("[%d] %s\n", i+1, strings.TrimSpace(msg)))
		}

		return title + body.String()
	}

	// No commits from agent - generate message from task title
	if opts != nil && opts.TaskTitle != "" {
		return generateCommitMessageFromTitle(opts.TaskTitle, beadID)
	}

	// Last resort fallback (should be rare - task title should always be available)
	return fmt.Sprintf("canopy: apply changes from %s", beadID)
}

// generateCommitMessageFromTitle creates a conventional commit message from a task title.
// It analyzes the title to determine the appropriate commit type (feat, fix, refactor, etc.)
// and formats it as "type: description (bead-id)".
func generateCommitMessageFromTitle(title string, beadID string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Sprintf("chore: apply changes (%s)", beadID)
	}

	// Normalize title to lowercase for pattern matching
	lowerTitle := strings.ToLower(title)

	// Determine commit type based on keywords in the title
	// Order matters: more specific types should be checked before general ones
	var commitType string
	switch {
	case strings.Contains(lowerTitle, "fix") || strings.Contains(lowerTitle, "bug") ||
		strings.Contains(lowerTitle, "error") || strings.Contains(lowerTitle, "issue") ||
		strings.Contains(lowerTitle, "broken") || strings.Contains(lowerTitle, "crash"):
		commitType = "fix"
	case strings.Contains(lowerTitle, "test"):
		commitType = "test"
	case strings.Contains(lowerTitle, "doc") || strings.Contains(lowerTitle, "readme") ||
		strings.Contains(lowerTitle, "comment"):
		commitType = "docs"
	case strings.Contains(lowerTitle, "refactor") || strings.Contains(lowerTitle, "restructure") ||
		strings.Contains(lowerTitle, "reorganize") || strings.Contains(lowerTitle, "clean"):
		commitType = "refactor"
	case strings.Contains(lowerTitle, "remove") || strings.Contains(lowerTitle, "delete"):
		commitType = "refactor"
	case strings.Contains(lowerTitle, "add") || strings.Contains(lowerTitle, "implement") ||
		strings.Contains(lowerTitle, "create") || strings.Contains(lowerTitle, "new") ||
		strings.Contains(lowerTitle, "feature") || strings.Contains(lowerTitle, "update") ||
		strings.Contains(lowerTitle, "change") || strings.Contains(lowerTitle, "modify") ||
		strings.Contains(lowerTitle, "improve"):
		commitType = "feat"
	default:
		commitType = "chore"
	}

	// Format the description: lowercase first letter, trim trailing punctuation
	description := title
	if len(description) > 0 {
		// Lowercase the first character for conventional commit style
		description = strings.ToLower(description[:1]) + description[1:]
	}
	// Trim common trailing punctuation
	description = strings.TrimRight(description, ".!?")

	return fmt.Sprintf("%s: %s (%s)", commitType, description, beadID)
}

// appendBeadID appends the bead ID to a commit message if not already present.
// Handles both single-line and multi-line messages.
func appendBeadID(msg string, beadID string) string {
	// Check if bead ID is already present anywhere in the message
	if strings.Contains(msg, "("+beadID+")") || strings.Contains(msg, beadID) {
		return msg
	}

	// For multi-line messages, append to first line only
	lines := strings.SplitN(msg, "\n", 2)
	firstLine := strings.TrimSpace(lines[0])

	// Append bead ID to first line
	firstLine = firstLine + " (" + beadID + ")"

	if len(lines) > 1 {
		return firstLine + "\n" + lines[1]
	}
	return firstLine
}

// getFirstLine returns the first line of a potentially multi-line string
func getFirstLine(s string) string {
	if idx := strings.Index(s, "\n"); idx != -1 {
		return s[:idx]
	}
	return s
}

// BuildCommitMessageForTask is a public wrapper around buildMergeCommitMessage.
// It allows the processor to get the commit message that would be used for a task
// without having to duplicate the message generation logic.
func (m *SequentialMerger) BuildCommitMessageForTask(result *agent.Result, opts *MergeOptions) string {
	return m.buildMergeCommitMessage(result, opts)
}

// filterGitignored filters out paths that are gitignored.
// Uses git check-ignore to respect .gitignore, .git/info/exclude, and global gitignore.
func (m *SequentialMerger) filterGitignored(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	// Use git check-ignore with stdin to check all paths efficiently in one call
	// --no-index: don't check if file is tracked (we want to know if pattern matches)
	// -n: show non-matching files (inverted behavior - we want to see what's NOT ignored)
	// Actually, simpler approach: check-ignore returns ignored paths, so we filter them out
	cmd := exec.Command("git", "check-ignore", "--stdin")
	cmd.Dir = m.outputDir

	var stdin bytes.Buffer
	for _, p := range paths {
		stdin.WriteString(p + "\n")
	}
	cmd.Stdin = &stdin

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// git check-ignore exits 0 if any paths are ignored, 1 if none are ignored
	// Both are valid outcomes, so we don't treat exit 1 as an error
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Exit code 1 means no paths are ignored - that's fine
			if exitErr.ExitCode() == 1 {
				return paths, nil
			}
		}
		// Exit code 128 or other means actual error
		return nil, fmt.Errorf("git check-ignore failed: %w: %s", err, stderr.String())
	}

	// Parse ignored paths from output (one per line)
	ignoredSet := make(map[string]bool)
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ignoredSet[line] = true
			if m.verbose {
				fmt.Printf("Skipping gitignored file: %s\n", line)
			}
		}
	}

	// Filter out ignored paths
	var filtered []string
	for _, p := range paths {
		if !ignoredSet[p] {
			filtered = append(filtered, p)
		}
	}

	return filtered, nil
}

// commitFileChanges stages and commits file changes for a single task.
// Returns a commitResult indicating:
//   - Committed=true: commit was successful
//   - Committed=false, PreCommitHookFailed=true: pre-commit hook rejected the commit
//   - Committed=false, Error!=nil: some other error occurred
//   - Committed=false, Error==nil, PreCommitHookFailed=false: no changes to commit
func (m *SequentialMerger) commitFileChanges(result *agent.Result, paths []string, commitMsg string) *commitResult {
	if len(paths) == 0 {
		return &commitResult{}
	}

	// Filter out gitignored files before staging
	// This is necessary because git add with explicit paths bypasses .gitignore
	filteredPaths, err := m.filterGitignored(paths)
	if err != nil {
		return &commitResult{Error: fmt.Errorf("failed to filter gitignored files: %w", err)}
	}

	if len(filteredPaths) == 0 {
		if m.verbose {
			fmt.Printf("No changes to commit for task %s (all files gitignored)\n", result.TaskID)
		}
		return &commitResult{}
	}

	// Stage the files
	addCmd := exec.Command("git", "add", "--")
	addCmd.Args = append(addCmd.Args, filteredPaths...)
	addCmd.Dir = m.outputDir
	var addStderr bytes.Buffer
	addCmd.Stderr = &addStderr
	if err := addCmd.Run(); err != nil {
		return &commitResult{Error: fmt.Errorf("git add failed: %w: %s", err, addStderr.String())}
	}

	// Check if there are staged changes
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = m.outputDir
	if err := diffCmd.Run(); err == nil {
		// No staged changes (exit code 0 means no diff)
		if m.verbose {
			fmt.Printf("No changes to commit for task %s (all changes already committed)\n", result.TaskID)
		}
		return &commitResult{}
	}

	// Capture both stdout and stderr for the commit command
	// Pre-commit hooks write to both, and we need the full context
	commitCmd := exec.Command("git", "commit", "-m", commitMsg)
	commitCmd.Dir = m.outputDir
	var commitOutput bytes.Buffer
	commitCmd.Stdout = &commitOutput
	commitCmd.Stderr = &commitOutput

	if err := commitCmd.Run(); err != nil {
		output := commitOutput.String()

		// Detect if this was a pre-commit hook failure
		// Pre-commit hook failures are characterized by:
		// 1. Exit code 1 (hooks can only return 0 or non-zero)
		// 2. Output that doesn't start with typical git errors
		// 3. Often contains build/test output
		if isPreCommitHookFailure(output) {
			if m.verbose {
				fmt.Printf("Pre-commit hook failed for task %s, keeping staged changes for repair\n", result.TaskID)
			}
			// DON'T reset staged changes - keep them for the repair agent
			return &commitResult{
				PreCommitHookFailed: true,
				HookOutput:          output,
			}
		}

		// Not a pre-commit hook failure - reset staged files and return error
		resetCmd := exec.Command("git", "reset", "HEAD", "--")
		resetCmd.Args = append(resetCmd.Args, paths...)
		resetCmd.Dir = m.outputDir
		if resetErr := resetCmd.Run(); resetErr != nil && m.verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to reset staged files after commit failure: %v\n", resetErr)
		}
		return &commitResult{Error: fmt.Errorf("git commit failed: %w: %s", err, output)}
	}

	if m.verbose {
		fmt.Printf("Created commit for changes from task %s\n", result.TaskID)
	}

	return &commitResult{Committed: true}
}

// isPreCommitHookFailure determines if a git commit failure was caused by a pre-commit hook.
// Pre-commit hooks reject commits by exiting with a non-zero status, causing git commit
// to fail. We detect this by looking for patterns in the output that indicate a hook failure
// rather than a git-internal error.
func isPreCommitHookFailure(output string) bool {
	// Common indicators that a pre-commit hook ran and failed:
	// 1. Build failures (go build, npm build, etc.)
	// 2. Test failures (go test, npm test, etc.)
	// 3. Linter failures (golint, eslint, etc.)
	// 4. Explicit "pre-commit" mentions

	hookIndicators := []string{
		// Build commands
		"go build",
		"npm run build",
		"make build",
		"cargo build",

		// Test commands
		"go test",
		"npm test",
		"pytest",
		"cargo test",

		// Linters
		"golint",
		"eslint",
		"prettier",
		"rustfmt",

		// Common error patterns from builds
		"undefined:",
		"cannot find",
		"syntax error",
		"compilation failed",
		"build failed",

		// Explicit hook mentions
		"pre-commit",
		"hook failed",
	}

	lowerOutput := strings.ToLower(output)
	for _, indicator := range hookIndicators {
		if strings.Contains(lowerOutput, strings.ToLower(indicator)) {
			return true
		}
	}

	// Git's own errors typically start with specific patterns
	// If the output doesn't look like a git error, assume it's from a hook
	gitErrorPrefixes := []string{
		"error: pathspec",
		"error: src refspec",
		"fatal: not a git repository",
		"fatal: unable to access",
		"fatal: refusing to merge",
		"hint:",
	}

	for _, prefix := range gitErrorPrefixes {
		if strings.HasPrefix(strings.ToLower(output), strings.ToLower(prefix)) {
			return false // This is a git error, not a hook failure
		}
	}

	// If output is non-empty and doesn't look like a git error, likely a hook failure
	return len(strings.TrimSpace(output)) > 0
}

// isWorkingDirectoryClean checks if the specified paths have any uncommitted changes
// (staged or unstaged). Returns true if all paths are clean, false otherwise.
func (m *SequentialMerger) isWorkingDirectoryClean(paths []string) (bool, error) {
	if len(paths) == 0 {
		return true, nil
	}

	// Use git status --porcelain to check for changes
	// This shows both staged and unstaged changes
	cmd := exec.Command("git", "status", "--porcelain", "--")
	cmd.Args = append(cmd.Args, paths...)
	cmd.Dir = m.outputDir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("git status failed: %w", err)
	}

	// If output is empty, all paths are clean
	return len(strings.TrimSpace(stdout.String())) == 0, nil
}

// hardResetToHead performs a hard reset to the specified commit.
// This discards all changes in the working directory and index.
func (m *SequentialMerger) hardResetToHead(headCommit string) error {
	cmd := exec.Command("git", "reset", "--hard", headCommit)
	cmd.Dir = m.outputDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git reset --hard failed: %w: %s", err, stderr.String())
	}
	return nil
}
