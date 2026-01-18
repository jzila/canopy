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

	// Get list of new commits (oldest first)
	cmd := exec.Command("git", "rev-list", "--reverse", baseCommit+"..HEAD")
	cmd.Dir = o.MergedDir

	out, err := cmd.Output()
	if err != nil {
		// No new commits is not an error
		return state, nil
	}

	commits := strings.Fields(string(out))
	if len(commits) == 0 {
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
		abortCmd.Run()

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
	cmd.Dir = o.MergedDir

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
	cmd = exec.Command("git", "diff-tree", "--no-commit-id", "--name-only", "-r", commitHash)
	cmd.Dir = o.MergedDir

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
			if strings.HasPrefix(subject, "[PATCH] ") {
				subject = strings.TrimPrefix(subject, "[PATCH] ")
			}
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
