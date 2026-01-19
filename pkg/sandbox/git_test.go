package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractFilesFromPatches(t *testing.T) {
	tests := []struct {
		name     string
		patches  []string
		expected []string
	}{
		{
			name:     "empty patches",
			patches:  []string{},
			expected: nil,
		},
		{
			name: "single file patch",
			patches: []string{
				`From abc123 Mon Sep 17 00:00:00 2001
From: Test User <test@example.com>
Subject: [PATCH] Add feature

---
diff --git a/pkg/foo/bar.go b/pkg/foo/bar.go
index 1234567..abcdefg 100644
--- a/pkg/foo/bar.go
+++ b/pkg/foo/bar.go
@@ -1,3 +1,4 @@
 package foo
+// new line
`,
			},
			expected: []string{"pkg/foo/bar.go"},
		},
		{
			name: "multiple files in single patch",
			patches: []string{
				`From abc123 Mon Sep 17 00:00:00 2001
From: Test User <test@example.com>
Subject: [PATCH] Multiple changes

---
diff --git a/cmd/main.go b/cmd/main.go
index 1234567..abcdefg 100644
--- a/cmd/main.go
+++ b/cmd/main.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
diff --git a/pkg/util/helper.go b/pkg/util/helper.go
index 1234567..abcdefg 100644
--- a/pkg/util/helper.go
+++ b/pkg/util/helper.go
@@ -1,3 +1,4 @@
 package util
+// comment
`,
			},
			expected: []string{"cmd/main.go", "pkg/util/helper.go"},
		},
		{
			name: "multiple patches",
			patches: []string{
				`diff --git a/file1.go b/file1.go
--- a/file1.go
+++ b/file1.go
`,
				`diff --git a/file2.go b/file2.go
--- a/file2.go
+++ b/file2.go
`,
			},
			expected: []string{"file1.go", "file2.go"},
		},
		{
			name: "same file in multiple patches (deduplication)",
			patches: []string{
				`diff --git a/shared.go b/shared.go
--- a/shared.go
+++ b/shared.go
`,
				`diff --git a/shared.go b/shared.go
--- a/shared.go
+++ b/shared.go
`,
			},
			expected: []string{"shared.go"},
		},
		{
			name: "new file (a/dev/null)",
			patches: []string{
				`diff --git a/dev/null b/newfile.go
new file mode 100644
index 0000000..1234567
--- /dev/null
+++ b/newfile.go
`,
			},
			expected: []string{"newfile.go"},
		},
		// Note: Paths with spaces are rare in codebases and git format-patch
		// may quote them differently. The current implementation handles
		// the common case of paths without spaces.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractFilesFromPatches(tt.patches)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d files, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("file %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

func TestExtractFilesFromPatchesWithRenames(t *testing.T) {
	// Git renames show both old and new paths, we extract the destination (b/ path)
	patches := []string{
		`diff --git a/old/path.go b/new/path.go
similarity index 95%
rename from old/path.go
rename to new/path.go
--- a/old/path.go
+++ b/new/path.go
`,
	}

	result := ExtractFilesFromPatches(patches)

	if len(result) != 1 {
		t.Errorf("expected 1 file, got %d: %v", len(result), result)
		return
	}

	// We extract the b/ path (destination)
	if result[0] != "new/path.go" {
		t.Errorf("expected 'new/path.go', got %q", result[0])
	}
}

// setupTestGitRepo creates a temporary git repository for testing.
// Returns the overlay, cleanup function, and any error.
func setupTestGitRepo(t *testing.T) (*Overlay, func()) {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		t.Fatalf("git init failed: %v: %s", err, out)
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		t.Fatalf("git config user.name failed: %v: %s", err, out)
	}

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		t.Fatalf("git config user.email failed: %v: %s", err, out)
	}

	// Create an initial commit so we have a valid HEAD
	testFile := filepath.Join(tempDir, "initial.txt")
	if err := os.WriteFile(testFile, []byte("initial content\n"), 0644); err != nil {
		cleanup()
		t.Fatalf("failed to write initial file: %v", err)
	}

	cmd = exec.Command("git", "add", "initial.txt")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		t.Fatalf("git add failed: %v: %s", err, out)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		t.Fatalf("git commit failed: %v: %s", err, out)
	}

	// Create a minimal overlay pointing to this repo
	overlay := &Overlay{
		ID:        "test",
		MergedDir: tempDir,
	}

	return overlay, cleanup
}

func TestExtractNewCommits_NormalCase(t *testing.T) {
	overlay, cleanup := setupTestGitRepo(t)
	defer cleanup()

	// Get the base commit (initial commit)
	baseCommit, err := overlay.GetBaseCommit()
	if err != nil {
		t.Fatalf("GetBaseCommit failed: %v", err)
	}

	// Create two new commits
	for i := 1; i <= 2; i++ {
		testFile := filepath.Join(overlay.MergedDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(testFile, []byte(fmt.Sprintf("content %d\n", i)), 0644); err != nil {
			t.Fatalf("failed to write file%d.txt: %v", i, err)
		}

		cmd := exec.Command("git", "add", fmt.Sprintf("file%d.txt", i))
		cmd.Dir = overlay.MergedDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add failed: %v: %s", err, out)
		}

		cmd = exec.Command("git", "commit", "-m", fmt.Sprintf("Add file %d", i))
		cmd.Dir = overlay.MergedDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit failed: %v: %s", err, out)
		}
	}

	// Extract new commits
	state, err := overlay.ExtractNewCommits(baseCommit)
	if err != nil {
		t.Fatalf("ExtractNewCommits failed: %v", err)
	}

	// Verify we got the right number of commits
	if len(state.NewCommits) != 2 {
		t.Errorf("expected 2 new commits, got %d", len(state.NewCommits))
	}

	// Verify we have patches for each commit
	if len(state.Patches) != 2 {
		t.Errorf("expected 2 patches, got %d", len(state.Patches))
	}

	// Verify patches contain the expected files
	files := ExtractFilesFromPatches(state.Patches)
	if len(files) != 2 {
		t.Errorf("expected 2 files in patches, got %d: %v", len(files), files)
	}

	// Verify commit messages were extracted
	if len(state.CommitMessages) != 2 {
		t.Errorf("expected 2 commit messages, got %d", len(state.CommitMessages))
	}

	// Verify base commit is preserved
	if state.BaseCommit != baseCommit {
		t.Errorf("expected BaseCommit=%q, got %q", baseCommit, state.BaseCommit)
	}
}

func TestExtractNewCommits_NoCommits(t *testing.T) {
	overlay, cleanup := setupTestGitRepo(t)
	defer cleanup()

	// Get the base commit - this will be HEAD (no new commits after this)
	baseCommit, err := overlay.GetBaseCommit()
	if err != nil {
		t.Fatalf("GetBaseCommit failed: %v", err)
	}

	// Extract new commits without making any
	state, err := overlay.ExtractNewCommits(baseCommit)
	if err != nil {
		t.Fatalf("ExtractNewCommits should not error for no commits: %v", err)
	}

	// Verify we got empty state, not nil
	if state == nil {
		t.Fatal("expected non-nil GitState")
	}

	// Verify no new commits
	if len(state.NewCommits) != 0 {
		t.Errorf("expected 0 new commits, got %d", len(state.NewCommits))
	}

	// Verify no patches
	if len(state.Patches) != 0 {
		t.Errorf("expected 0 patches, got %d", len(state.Patches))
	}

	// BaseCommit should still be set
	if state.BaseCommit != baseCommit {
		t.Errorf("expected BaseCommit=%q, got %q", baseCommit, state.BaseCommit)
	}
}

func TestExtractNewCommits_InvalidBaseCommit(t *testing.T) {
	overlay, cleanup := setupTestGitRepo(t)
	defer cleanup()

	// Use an invalid commit hash
	invalidCommit := "0000000000000000000000000000000000000000"

	state, err := overlay.ExtractNewCommits(invalidCommit)

	// Should return an error for invalid ref
	if err == nil {
		t.Fatal("expected error for invalid base commit, got nil")
	}

	// State may be nil or partially populated, but error is the key check
	if state != nil {
		t.Logf("state returned despite error: %+v", state)
	}

	// Verify error message mentions the failed command
	if !strings.Contains(err.Error(), "rev-list") {
		t.Errorf("expected error to mention 'rev-list', got: %v", err)
	}
}

func TestExtractNewCommits_GitError(t *testing.T) {
	// Create an overlay pointing to a non-git directory
	tempDir, err := os.MkdirTemp("", "not-a-git-repo-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	overlay := &Overlay{
		ID:        "test",
		MergedDir: tempDir,
	}

	// Attempt to extract commits from non-git directory
	state, err := overlay.ExtractNewCommits("HEAD~1")

	// Should return an error (not silently return empty)
	if err == nil {
		t.Fatal("expected error for non-git directory, got nil")
	}

	// State should be nil on error
	if state != nil {
		t.Errorf("expected nil state on error, got: %+v", state)
	}
}

func TestExtractNewCommits_VerboseLogging(t *testing.T) {
	overlay, cleanup := setupTestGitRepo(t)
	defer cleanup()

	// Enable verbose mode
	overlay.Verbose = true

	baseCommit, err := overlay.GetBaseCommit()
	if err != nil {
		t.Fatalf("GetBaseCommit failed: %v", err)
	}

	// Add a commit
	testFile := filepath.Join(overlay.MergedDir, "verbose-test.txt")
	if err := os.WriteFile(testFile, []byte("verbose test\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	cmd := exec.Command("git", "add", "verbose-test.txt")
	cmd.Dir = overlay.MergedDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v: %s", err, out)
	}

	cmd = exec.Command("git", "commit", "-m", "Verbose test commit")
	cmd.Dir = overlay.MergedDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v: %s", err, out)
	}

	// Should work correctly even with verbose mode on
	state, err := overlay.ExtractNewCommits(baseCommit)
	if err != nil {
		t.Fatalf("ExtractNewCommits with verbose failed: %v", err)
	}

	if len(state.NewCommits) != 1 {
		t.Errorf("expected 1 new commit, got %d", len(state.NewCommits))
	}
}

func TestGetCommitInfoFromDir(t *testing.T) {
	// Create a test git repo
	tempDir, err := os.MkdirTemp("", "git-commit-info-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}

	// Configure git user
	cmd = exec.Command("git", "config", "user.name", "Test Author")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config user.name failed: %v: %s", err, out)
	}

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config user.email failed: %v: %s", err, out)
	}

	// Create a commit with known content
	testFile := filepath.Join(tempDir, "test-file.txt")
	if err := os.WriteFile(testFile, []byte("test content\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cmd = exec.Command("git", "add", "test-file.txt")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v: %s", err, out)
	}

	cmd = exec.Command("git", "commit", "-m", "Test commit message")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v: %s", err, out)
	}

	// Get the commit hash
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = tempDir
	hashOutput, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD failed: %v", err)
	}
	commitHash := strings.TrimSpace(string(hashOutput))

	// Test GetCommitInfoFromDir (the new standalone function)
	info, err := GetCommitInfoFromDir(tempDir, commitHash)
	if err != nil {
		t.Fatalf("GetCommitInfoFromDir failed: %v", err)
	}

	// Verify the commit info
	if info.Hash != commitHash {
		t.Errorf("Hash: expected %q, got %q", commitHash, info.Hash)
	}

	if len(info.Hash) != 40 {
		t.Errorf("Expected full 40-char hash, got %d chars", len(info.Hash))
	}

	if len(info.ShortHash) != 7 {
		t.Errorf("Expected 7-char short hash, got %d chars: %s", len(info.ShortHash), info.ShortHash)
	}

	if info.ShortHash != commitHash[:7] {
		t.Errorf("ShortHash: expected %q, got %q", commitHash[:7], info.ShortHash)
	}

	if info.Message != "Test commit message" {
		t.Errorf("Message: expected %q, got %q", "Test commit message", info.Message)
	}

	if info.Author != "Test Author" {
		t.Errorf("Author: expected %q, got %q", "Test Author", info.Author)
	}

	if info.AuthorEmail != "test@example.com" {
		t.Errorf("AuthorEmail: expected %q, got %q", "test@example.com", info.AuthorEmail)
	}

	if info.Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}

	if len(info.FilesChanged) != 1 {
		t.Errorf("Expected 1 file changed, got %d", len(info.FilesChanged))
	} else if info.FilesChanged[0] != "test-file.txt" {
		t.Errorf("FilesChanged: expected [test-file.txt], got %v", info.FilesChanged)
	}
}

func TestGetCommitInfoFromDir_InvalidCommit(t *testing.T) {
	// Create a test git repo
	tempDir, err := os.MkdirTemp("", "git-invalid-commit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}

	// Test with invalid commit hash
	_, err = GetCommitInfoFromDir(tempDir, "0000000000000000000000000000000000000000")
	if err == nil {
		t.Error("Expected error for invalid commit hash, got nil")
	}
}
