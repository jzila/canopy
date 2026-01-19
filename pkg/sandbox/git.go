package sandbox

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitState captures the git state before/after worker execution
type GitState struct {
	BaseCommit     string   // HEAD before worker started
	NewCommits     []string // Commits made by worker (oldest first)
	Patches        []string // Patch content for each new commit
	CommitMessages []string // Commit messages (one per patch, oldest first)
}

// GetBaseCommit returns the current HEAD commit in the overlay
func (o *Overlay) GetBaseCommit() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = o.MergedDir

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// ExtractNewCommits extracts commits made since baseCommit as patches
func (o *Overlay) ExtractNewCommits(baseCommit string) (*GitState, error) {
	state := &GitState{
		BaseCommit: baseCommit,
	}

	if o.Verbose {
		fmt.Fprintf(os.Stderr, "[ExtractNewCommits] baseCommit=%q, MergedDir=%q\n", baseCommit, o.MergedDir)
	}

	// Get list of new commits (oldest first)
	cmdArgs := []string{"rev-list", "--reverse", baseCommit + "..HEAD"}
	cmd := exec.Command("git", cmdArgs...)
	cmd.Dir = o.MergedDir

	if o.Verbose {
		fmt.Fprintf(os.Stderr, "[ExtractNewCommits] running: git %s (in %s)\n", strings.Join(cmdArgs, " "), o.MergedDir)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if o.Verbose {
		fmt.Fprintf(os.Stderr, "[ExtractNewCommits] git rev-list output=%q, stderr=%q, err=%v\n",
			strings.TrimSpace(string(out)), strings.TrimSpace(stderr.String()), err)
	}

	if err != nil {
		// Check if this is an actual error vs empty output
		// git rev-list returns exit 0 with empty output when there are no commits
		// git rev-list returns exit 128 for invalid refs or other errors
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Non-zero exit code means actual error (e.g., invalid ref)
			return nil, fmt.Errorf("git rev-list %s..HEAD failed (exit %d): %s",
				baseCommit, exitErr.ExitCode(), strings.TrimSpace(stderr.String()))
		}
		// Other errors (e.g., command not found)
		return nil, fmt.Errorf("git rev-list failed: %w", err)
	}

	commits := strings.Fields(string(out))
	if o.Verbose {
		fmt.Fprintf(os.Stderr, "[ExtractNewCommits] found %d commits: %v\n", len(commits), commits)
	}

	if len(commits) == 0 {
		if o.Verbose {
			fmt.Fprintf(os.Stderr, "[ExtractNewCommits] returning GitState with 0 commits\n")
		}
		return state, nil
	}

	state.NewCommits = commits

	// Extract each commit as a patch
	for _, commit := range commits {
		patch, err := o.formatPatch(commit)
		if err != nil {
			return state, fmt.Errorf("failed to format patch for %s: %w", commit, err)
		}
		state.Patches = append(state.Patches, patch)

		// Extract commit message from patch
		msg := extractCommitMessageFromPatch(patch)
		state.CommitMessages = append(state.CommitMessages, msg)
	}

	if o.Verbose {
		fmt.Fprintf(os.Stderr, "[ExtractNewCommits] returning GitState with %d commits, %d patches\n",
			len(state.NewCommits), len(state.Patches))
	}

	return state, nil
}

// formatPatch extracts a single commit as a patch
func (o *Overlay) formatPatch(commit string) (string, error) {
	cmd := exec.Command("git", "format-patch", "-1", "--stdout", commit)
	cmd.Dir = o.MergedDir

	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return string(out), nil
}

// ApplyPatches applies patches to a target directory
func ApplyPatches(targetDir string, patches []string) error {
	for i, patch := range patches {
		if err := applyPatch(targetDir, patch); err != nil {
			return fmt.Errorf("failed to apply patch %d: %w", i, err)
		}
	}
	return nil
}

func applyPatch(targetDir, patch string) error {
	cmd := exec.Command("git", "am", "--3way")
	cmd.Dir = targetDir
	cmd.Stdin = strings.NewReader(patch)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Try to abort the failed am
		abortCmd := exec.Command("git", "am", "--abort")
		abortCmd.Dir = targetDir
		_ = abortCmd.Run()

		return fmt.Errorf("%w: %s", err, stderr.String())
	}

	return nil
}

// WriteContextFile writes dependency context to a file in the sandbox
func (o *Overlay) WriteContextFile(context map[string]string) error {
	if len(context) == 0 {
		return nil
	}

	contextDir := filepath.Join(o.MergedDir, ".canopy")
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		return err
	}

	// Write each dependency's output to a separate file
	for taskID, content := range context {
		filename := filepath.Join(contextDir, fmt.Sprintf("dep-%s.txt", taskID))
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			return err
		}
	}

	// Write a summary file
	summaryPath := filepath.Join(contextDir, "context.txt")
	var summary strings.Builder
	summary.WriteString("# Dependency Context\n\n")
	summary.WriteString("This directory contains outputs from upstream tasks.\n\n")

	for taskID := range context {
		summary.WriteString(fmt.Sprintf("- dep-%s.txt: Output from task %s\n", taskID, taskID))
	}

	return os.WriteFile(summaryPath, []byte(summary.String()), 0644)
}

// HasGitRepo checks if the overlay contains a git repository
func (o *Overlay) HasGitRepo() bool {
	gitDir := filepath.Join(o.MergedDir, ".git")
	info, err := os.Stat(gitDir)
	return err == nil && info.IsDir()
}

// GetDiffBetween generates a diff between two commits.
// This is useful for showing what changed between an agent's starting point
// and current HEAD, so a resolver can understand concurrent modifications.
func GetDiffBetween(repoDir, fromCommit, toCommit string) (string, error) {
	cmd := exec.Command("git", "diff", fromCommit+".."+toCommit)
	cmd.Dir = repoDir

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %w", err)
	}

	return string(out), nil
}

// CommitInfo holds detailed information about a git commit
type CommitInfo struct {
	Hash         string   // Full commit hash
	ShortHash    string   // Short (7-char) hash
	Message      string   // Commit message (first line)
	Author       string   // Author name
	AuthorEmail  string   // Author email
	Timestamp    string   // ISO 8601 timestamp
	FilesChanged []string // Files modified in this commit
}

// GetCommitInfo extracts detailed information about a commit
func (o *Overlay) GetCommitInfo(commitHash string) (*CommitInfo, error) {
	return GetCommitInfoFromDir(o.MergedDir, commitHash)
}

// GetCommitInfoFromDir extracts detailed information about a commit from any git directory.
// This is a standalone function that doesn't require an Overlay, useful for getting
// commit info from the main repository after merge (when overlays are destroyed).
func GetCommitInfoFromDir(gitDir, commitHash string) (*CommitInfo, error) {
	info := &CommitInfo{
		Hash:      commitHash,
		ShortHash: commitHash,
	}
	if len(commitHash) >= 7 {
		info.ShortHash = commitHash[:7]
	}

	// Get commit metadata using git show with format
	// Format: %s (subject), %an (author name), %ae (author email), %aI (ISO timestamp)
	cmd := exec.Command("git", "show", "-s", "--format=%s%n%an%n%ae%n%aI", commitHash)
	cmd.Dir = gitDir

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git show failed: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) >= 4 {
		info.Message = lines[0]
		info.Author = lines[1]
		info.AuthorEmail = lines[2]
		info.Timestamp = lines[3]
	}

	// Get files changed in this commit
	// Use --root to handle the initial commit (which has no parent)
	cmd = exec.Command("git", "diff-tree", "--no-commit-id", "--name-only", "-r", "--root", commitHash)
	cmd.Dir = gitDir

	out, err = cmd.Output()
	if err == nil {
		files := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, f := range files {
			if f != "" {
				info.FilesChanged = append(info.FilesChanged, f)
			}
		}
	}

	return info, nil
}

// ExtractFilesFromPatches returns unique file paths mentioned in git format-patch output.
// It parses 'diff --git a/path b/path' lines to extract the paths.
func ExtractFilesFromPatches(patches []string) []string {
	seen := make(map[string]bool)
	var files []string

	for _, patch := range patches {
		lines := strings.Split(patch, "\n")
		for _, line := range lines {
			// Parse 'diff --git a/path b/path' lines
			if strings.HasPrefix(line, "diff --git ") {
				// Format: diff --git a/path/to/file b/path/to/file
				// We extract the b/ path as it represents the destination
				parts := strings.Split(line, " ")
				if len(parts) >= 4 {
					// Get the b/path part (last element) and strip "b/" prefix
					bPath := parts[len(parts)-1]
					if strings.HasPrefix(bPath, "b/") {
						filePath := strings.TrimPrefix(bPath, "b/")
						if !seen[filePath] {
							seen[filePath] = true
							files = append(files, filePath)
						}
					}
				}
			}
		}
	}

	return files
}

// GetFileAtCommit returns the content of a file at a specific commit.
// Returns empty string and nil error if the file doesn't exist at that commit (e.g., new files).
// Returns error for actual git failures.
func GetFileAtCommit(repoDir, commitHash, filePath string) (string, error) {
	cmd := exec.Command("git", "show", commitHash+":"+filePath)
	cmd.Dir = repoDir

	out, err := cmd.Output()
	if err != nil {
		// Check if it's an exit error (file doesn't exist at that commit)
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			// Git returns error when file doesn't exist at that commit
			// This is not an error condition for our use case (new files)
			if strings.Contains(stderr, "does not exist") ||
				strings.Contains(stderr, "exists on disk, but not in") ||
				strings.Contains(stderr, "fatal: path") {
				return "", nil
			}
		}
		return "", fmt.Errorf("git show %s:%s failed: %w", commitHash, filePath, err)
	}

	return string(out), nil
}

// GetBaseFileContents retrieves the content of files at a specific commit.
// Only retrieves content for files that exist at that commit.
// Returns a map from file path to file content.
func GetBaseFileContents(repoDir, commitHash string, filePaths []string) (map[string]string, error) {
	result := make(map[string]string)

	for _, filePath := range filePaths {
		content, err := GetFileAtCommit(repoDir, commitHash, filePath)
		if err != nil {
			// Log warning but continue - some files may not exist
			continue
		}
		// Only include files that had content (existed at base commit)
		if content != "" {
			result[filePath] = content
		}
	}

	return result, nil
}

// extractCommitMessageFromPatch extracts the commit message from a git format-patch output
// The patch format includes "Subject: [PATCH] <commit message>" followed by the commit body
func extractCommitMessageFromPatch(patch string) string {
	lines := strings.Split(patch, "\n")

	var subject string
	var bodyLines []string
	inBody := false

	for _, line := range lines {
		// Look for Subject line
		if strings.HasPrefix(line, "Subject: ") {
			// Extract subject, removing "[PATCH]" prefix if present
			subject = strings.TrimPrefix(line, "Subject: ")
			subject = strings.TrimSpace(subject)
			subject = strings.TrimPrefix(subject, "[PATCH] ")
			inBody = true
			continue
		}

		// After subject, collect body until we hit "---" or "diff"
		if inBody {
			if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "diff ") {
				break
			}
			// Skip the empty line immediately after subject (it's metadata separator)
			if len(bodyLines) == 0 && line == "" {
				continue
			}
			// Collect body lines
			bodyLines = append(bodyLines, line)
		}
	}

	// If there's a body, combine subject and body with blank line separator
	if len(bodyLines) > 0 {
		body := strings.Join(bodyLines, "\n")
		body = strings.TrimSpace(body)
		if body != "" {
			return subject + "\n\n" + body
		}
	}

	return subject
}
