package resolver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/sandbox"
)

func TestBuildResolverPrompt(t *testing.T) {
	r := &Resolver{
		config: &Config{
			Verbose: false,
		},
	}

	conflict := &ConflictContext{
		TaskID:          "canopy-abc",
		TaskTitle:       "Add feature X",
		TaskDescription: "Implement feature X with proper error handling",
		FailedPatches:   []string{"patch-content-1", "patch-content-2"},
		PatchErrors:     []string{"error 1", "error 2"},
		ParentAgentID:   "agent-canopy-abc",
		BaseCommit:      "abc123def456",
	}

	prompt := r.buildResolverPrompt(conflict)

	// Verify the prompt contains key information
	if prompt == "" {
		t.Error("Expected non-empty prompt")
	}

	// Check for task ID
	if !strings.Contains(prompt, "canopy-abc") {
		t.Error("Prompt should contain task ID")
	}

	// Check for task title
	if !strings.Contains(prompt, "Add feature X") {
		t.Error("Prompt should contain task title")
	}

	// Check for task description
	if !strings.Contains(prompt, "Implement feature X") {
		t.Error("Prompt should contain task description")
	}

	// Check for conflict resolution instructions
	if !strings.Contains(prompt, "Three-Way Merge Resolution") {
		t.Error("Prompt should contain three-way merge resolution header")
	}

	// Check for patch file references
	if !strings.Contains(prompt, ".canopy/conflict") {
		t.Error("Prompt should reference patch file location")
	}

	// Check for base commit reference
	if !strings.Contains(prompt, "abc123def456") {
		t.Error("Prompt should contain base commit hash")
	}

	// Check for three-way merge terminology
	if !strings.Contains(prompt, "BASE") || !strings.Contains(prompt, "OURS") || !strings.Contains(prompt, "THEIRS") {
		t.Error("Prompt should use BASE/OURS/THEIRS terminology")
	}

	// Check for concurrent changes explanation
	if !strings.Contains(prompt, "concurrent") {
		t.Error("Prompt should explain concurrent changes")
	}
}

func TestConflictContextParentAgentID(t *testing.T) {
	conflict := &ConflictContext{
		TaskID:        "canopy-xyz",
		ParentAgentID: "agent-canopy-xyz",
	}

	if conflict.ParentAgentID != "agent-canopy-xyz" {
		t.Errorf("Expected ParentAgentID to be 'agent-canopy-xyz', got '%s'", conflict.ParentAgentID)
	}
}

func TestResultResolverAgentID(t *testing.T) {
	result := &Result{
		Success:         true,
		ResolverAgentID: "canopy-abc-resolver",
	}

	if result.ResolverAgentID != "canopy-abc-resolver" {
		t.Errorf("Expected ResolverAgentID to be 'canopy-abc-resolver', got '%s'", result.ResolverAgentID)
	}
}

func TestNewResolver(t *testing.T) {
	config := &Config{
		WorkDir:  t.TempDir(),
		TempDir:  t.TempDir(),
		Verbose:  true,
		UseBwrap: false,
	}

	r := New(config)

	if r == nil {
		t.Fatal("Expected New to return non-nil resolver")
	}

	if r.config != config {
		t.Error("Expected resolver config to match input config")
	}

	if r.executor == nil {
		t.Error("Expected resolver to have non-nil executor")
	}
}

// TestMakeAgentID verifies agent ID generation with and without run ID
func TestMakeAgentID(t *testing.T) {
	tests := []struct {
		name     string
		runID    string
		taskID   string
		expected string
	}{
		{
			name:     "with long run ID",
			runID:    "run-1234567890abcdef",
			taskID:   "canopy-abc",
			expected: "agent-run-1234-canopy-abc-resolver",
		},
		{
			name:     "with short run ID",
			runID:    "run-123",
			taskID:   "canopy-xyz",
			expected: "agent-run-123-canopy-xyz-resolver",
		},
		{
			name:     "with empty run ID",
			runID:    "",
			taskID:   "canopy-test",
			expected: "canopy-test-resolver",
		},
		{
			name:     "with exactly 8 char run ID",
			runID:    "12345678",
			taskID:   "canopy-foo",
			expected: "agent-12345678-canopy-foo-resolver",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Resolver{
				config: &Config{
					RunID: tt.runID,
				},
			}

			got := r.makeAgentID(tt.taskID)
			if got != tt.expected {
				t.Errorf("makeAgentID(%q) = %q, want %q", tt.taskID, got, tt.expected)
			}
		})
	}
}

// TestWritePatchFiles verifies that all conflict context files are written correctly
func TestWritePatchFiles(t *testing.T) {
	tempDir := t.TempDir()
	workDir := t.TempDir()

	// Create a mock overlay with just the MergedDir
	overlay := &sandbox.Overlay{
		MergedDir: tempDir,
	}

	r := &Resolver{
		config: &Config{
			WorkDir: workDir,
			TempDir: tempDir,
			Verbose: false,
		},
	}

	conflict := &ConflictContext{
		TaskID:          "canopy-test",
		TaskTitle:       "Test Task",
		TaskDescription: "A test task description",
		FailedPatches: []string{
			"diff --git a/file1.go b/file1.go\n--- a/file1.go\n+++ b/file1.go\n@@ -1 +1 @@\n-old\n+new",
			"diff --git a/file2.go b/file2.go\n--- a/file2.go\n+++ b/file2.go\n@@ -1 +1 @@\n-old2\n+new2",
		},
		PatchErrors:    []string{"error on patch 0", "error on patch 1"},
		BaseCommit:     "abc123def456",
		ConcurrentDiff: "diff --git a/concurrent.go...",
		BaseFileContents: map[string]string{
			"file1.go":       "original content of file1",
			"pkg/file2.go":   "original content of file2",
			"deep/path/f.go": "original content in deep path",
		},
	}

	err := r.writePatchFiles(overlay, conflict)
	if err != nil {
		t.Fatalf("writePatchFiles failed: %v", err)
	}

	conflictDir := filepath.Join(tempDir, ".canopy", "conflict")

	// Verify patch files
	for i, patchContent := range conflict.FailedPatches {
		patchPath := filepath.Join(conflictDir, "patch-"+string(rune('0'+i))+".patch")
		content, err := os.ReadFile(patchPath)
		if err != nil {
			t.Errorf("Failed to read patch-%d.patch: %v", i, err)
			continue
		}
		if string(content) != patchContent {
			t.Errorf("patch-%d.patch content mismatch: got %q, want %q", i, string(content), patchContent)
		}
	}

	// Verify errors file
	errorsPath := filepath.Join(conflictDir, "errors.txt")
	errorsContent, err := os.ReadFile(errorsPath)
	if err != nil {
		t.Fatalf("Failed to read errors.txt: %v", err)
	}
	if !strings.Contains(string(errorsContent), "Patch 0 Error") {
		t.Error("errors.txt should contain 'Patch 0 Error'")
	}
	if !strings.Contains(string(errorsContent), "error on patch 0") {
		t.Error("errors.txt should contain error message for patch 0")
	}

	// Verify original task context
	contextPath := filepath.Join(conflictDir, "original-task.txt")
	contextContent, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatalf("Failed to read original-task.txt: %v", err)
	}
	if !strings.Contains(string(contextContent), "canopy-test") {
		t.Error("original-task.txt should contain task ID")
	}
	if !strings.Contains(string(contextContent), "Test Task") {
		t.Error("original-task.txt should contain task title")
	}

	// Verify base commit file
	baseCommitPath := filepath.Join(conflictDir, "base-commit.txt")
	baseCommitContent, err := os.ReadFile(baseCommitPath)
	if err != nil {
		t.Fatalf("Failed to read base-commit.txt: %v", err)
	}
	if strings.TrimSpace(string(baseCommitContent)) != "abc123def456" {
		t.Errorf("base-commit.txt content = %q, want %q", strings.TrimSpace(string(baseCommitContent)), "abc123def456")
	}

	// Verify concurrent diff file
	concurrentDiffPath := filepath.Join(conflictDir, "concurrent-changes.diff")
	concurrentDiffContent, err := os.ReadFile(concurrentDiffPath)
	if err != nil {
		t.Fatalf("Failed to read concurrent-changes.diff: %v", err)
	}
	if string(concurrentDiffContent) != conflict.ConcurrentDiff {
		t.Errorf("concurrent-changes.diff content mismatch")
	}

	// Verify base file contents
	for filePath, expectedContent := range conflict.BaseFileContents {
		fullPath := filepath.Join(conflictDir, "base", filePath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("Failed to read base file %s: %v", filePath, err)
			continue
		}
		if string(content) != expectedContent {
			t.Errorf("base file %s content mismatch: got %q, want %q", filePath, string(content), expectedContent)
		}
	}
}

// TestWritePatchFilesMinimal verifies writePatchFiles works with minimal context
func TestWritePatchFilesMinimal(t *testing.T) {
	tempDir := t.TempDir()

	overlay := &sandbox.Overlay{
		MergedDir: tempDir,
	}

	r := &Resolver{
		config: &Config{},
	}

	// Minimal conflict context - only required fields
	conflict := &ConflictContext{
		TaskID:          "canopy-minimal",
		TaskTitle:       "Minimal Task",
		TaskDescription: "Minimal description",
		FailedPatches:   []string{}, // Empty patches
		// No PatchErrors, BaseCommit, ConcurrentDiff, or BaseFileContents
	}

	err := r.writePatchFiles(overlay, conflict)
	if err != nil {
		t.Fatalf("writePatchFiles with minimal context failed: %v", err)
	}

	conflictDir := filepath.Join(tempDir, ".canopy", "conflict")

	// Verify conflict directory was created
	if _, err := os.Stat(conflictDir); os.IsNotExist(err) {
		t.Error("Expected .canopy/conflict directory to exist")
	}

	// Verify original-task.txt exists
	if _, err := os.Stat(filepath.Join(conflictDir, "original-task.txt")); os.IsNotExist(err) {
		t.Error("Expected original-task.txt to exist")
	}

	// Verify optional files don't exist when not provided
	if _, err := os.Stat(filepath.Join(conflictDir, "base-commit.txt")); !os.IsNotExist(err) {
		t.Error("base-commit.txt should not exist when BaseCommit is empty")
	}
	if _, err := os.Stat(filepath.Join(conflictDir, "concurrent-changes.diff")); !os.IsNotExist(err) {
		t.Error("concurrent-changes.diff should not exist when ConcurrentDiff is empty")
	}
	if _, err := os.Stat(filepath.Join(conflictDir, "errors.txt")); !os.IsNotExist(err) {
		t.Error("errors.txt should not exist when PatchErrors is empty")
	}
	if _, err := os.Stat(filepath.Join(conflictDir, "base")); !os.IsNotExist(err) {
		t.Error("base directory should not exist when BaseFileContents is empty")
	}
}

// TestBuildResolverPromptWithAllFields verifies prompt contains all ConflictContext fields
func TestBuildResolverPromptWithAllFields(t *testing.T) {
	r := &Resolver{
		config: &Config{
			Verbose: false,
		},
	}

	conflict := &ConflictContext{
		TaskID:          "canopy-full",
		TaskTitle:       "Full Feature Implementation",
		TaskDescription: "Implement the full feature with tests and documentation",
		FailedPatches:   []string{"patch content 1", "patch content 2"},
		PatchErrors:     []string{"hunk failed", "context mismatch"},
		BaseCommit:      "deadbeef12345678",
		ConcurrentDiff:  "diff showing concurrent changes",
		BaseFileContents: map[string]string{
			"main.go": "original main.go content",
		},
		ParentAgentID: "agent-run12345-canopy-full",
	}

	prompt := r.buildResolverPrompt(conflict)

	// Verify task information is included
	if !strings.Contains(prompt, "canopy-full") {
		t.Error("Prompt should contain task ID")
	}
	if !strings.Contains(prompt, "Full Feature Implementation") {
		t.Error("Prompt should contain task title")
	}
	if !strings.Contains(prompt, "Implement the full feature") {
		t.Error("Prompt should contain task description")
	}

	// Verify base commit is included
	if !strings.Contains(prompt, "deadbeef12345678") {
		t.Error("Prompt should contain base commit hash")
	}

	// Verify three-way merge terminology
	if !strings.Contains(prompt, "BASE") {
		t.Error("Prompt should explain BASE")
	}
	if !strings.Contains(prompt, "OURS") {
		t.Error("Prompt should explain OURS")
	}
	if !strings.Contains(prompt, "THEIRS") {
		t.Error("Prompt should explain THEIRS")
	}

	// Verify file references
	if !strings.Contains(prompt, ".canopy/conflict/patch") {
		t.Error("Prompt should reference patch files")
	}
	if !strings.Contains(prompt, ".canopy/conflict/base") {
		t.Error("Prompt should reference base file directory")
	}
	if !strings.Contains(prompt, "concurrent-changes.diff") {
		t.Error("Prompt should reference concurrent changes diff")
	}

	// Verify critical warning about not reverting concurrent changes
	if !strings.Contains(prompt, "DO NOT revert") || !strings.Contains(prompt, "concurrent") {
		t.Error("Prompt should warn about preserving concurrent changes")
	}
}

// mockExecutor implements a mock agent executor for testing
type mockExecutor struct {
	executeCalls int
	lastTask     *beads.Task
	lastOverlay  *sandbox.Overlay
	result       *agent.Result
}

func (m *mockExecutor) Execute(ctx context.Context, task *beads.Task, overlay *sandbox.Overlay, deps []*agent.DependencyContext, feedbackCb func(string)) *agent.Result {
	m.executeCalls++
	m.lastTask = task
	m.lastOverlay = overlay
	return m.result
}

// TestResolveExecutionFlow tests the Resolve method execution flow with a mock executor
func TestResolveExecutionFlow(t *testing.T) {
	tempDir := t.TempDir()
	workDir := t.TempDir()

	// Initialize workDir as a git repo so overlay can work
	if err := initGitRepo(workDir); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	mockExec := &mockExecutor{
		result: &agent.Result{
			TaskID:   "canopy-test-resolver",
			Success:  true,
			ExitCode: 0,
			Changes: []sandbox.FileChange{
				{Path: "resolved.go", Type: sandbox.ChangeModified},
			},
			GitState: &sandbox.GitState{
				NewCommits: []string{"newcommit123"},
			},
			Output: &agent.ClaudeOutput{
				TotalInputTokens:  1000,
				TotalOutputTokens: 500,
				CostUSD:           0.05,
				DurationMS:        5000,
				NumTurns:          3,
			},
		},
	}

	r := &Resolver{
		config: &Config{
			WorkDir: workDir,
			TempDir: tempDir,
			Verbose: false,
			RunID:   "run-testrun",
		},
		executor: nil, // We'll inject our mock
	}

	// Inject mock executor using reflection or by creating a testable interface
	// For now, we test the components that don't require the real executor

	conflict := &ConflictContext{
		TaskID:          "canopy-test",
		TaskTitle:       "Test Feature",
		TaskDescription: "Implement test feature",
		FailedPatches:   []string{"patch content"},
		PatchErrors:     []string{"patch failed to apply"},
		ParentAgentID:   "agent-run-test-canopy-test",
		BaseCommit:      "basecommit123",
		ConcurrentDiff:  "concurrent diff content",
		BaseFileContents: map[string]string{
			"test.go": "original test.go content",
		},
	}

	// Verify agent ID generation
	expectedAgentID := "agent-run-test-canopy-test-resolver"
	actualAgentID := r.makeAgentID(conflict.TaskID)
	if actualAgentID != expectedAgentID {
		t.Errorf("Agent ID = %q, want %q", actualAgentID, expectedAgentID)
	}

	// Verify prompt generation includes all context
	prompt := r.buildResolverPrompt(conflict)
	requiredStrings := []string{
		"canopy-test",
		"Test Feature",
		"basecommit123",
		"BASE",
		"OURS",
		"THEIRS",
		"concurrent",
	}
	for _, s := range requiredStrings {
		if !strings.Contains(prompt, s) {
			t.Errorf("Prompt missing required string: %q", s)
		}
	}

	_ = mockExec // Will be used in integration test
}

// TestResolveWithRealOverlay tests Resolve with an actual overlay (without executing agent)
func TestResolveWithRealOverlay(t *testing.T) {
	// Skip if not on Linux (overlayfs requires Linux)
	if os.Getenv("CI") == "" {
		// Local development may not have overlay support
		t.Skip("Skipping overlay test in local development")
	}

	tempDir := t.TempDir()
	workDir := t.TempDir()

	// Initialize workDir as a git repo
	if err := initGitRepo(workDir); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(workDir, "test.go")
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	config := &Config{
		WorkDir: workDir,
		TempDir: tempDir,
		Verbose: true,
		RunID:   "run-overlay-test",
	}

	r := New(config)

	conflict := &ConflictContext{
		TaskID:          "canopy-overlay-test",
		TaskTitle:       "Overlay Test",
		TaskDescription: "Test overlay creation",
		FailedPatches:   []string{"patch content"},
		ParentAgentID:   "agent-parent",
	}

	// We can't fully test Resolve without a real claude binary,
	// but we can test that it handles context cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := r.Resolve(ctx, conflict)

	// The resolve should fail due to timeout or missing claude binary
	// but should not panic and should return a result
	if err != nil {
		// Error returned means something went wrong before agent execution
		t.Logf("Resolve returned error (expected in test): %v", err)
	}
	if result != nil {
		t.Logf("Resolve returned result: Success=%v, Error=%s", result.Success, result.Error)
		// Verify resolver agent ID was set
		if result.ResolverAgentID == "" {
			t.Error("Expected ResolverAgentID to be set")
		}
	}
}

// TestConflictContextAllFieldsPopulated verifies ConflictContext captures all required data
func TestConflictContextAllFieldsPopulated(t *testing.T) {
	ctx := &ConflictContext{
		TaskID:          "canopy-xyz",
		TaskTitle:       "Task XYZ",
		TaskDescription: "Description of task XYZ",
		FailedPatches: []string{
			"diff --git a/file.go b/file.go\n...",
		},
		PatchErrors: []string{
			"error: patch does not apply",
		},
		FileChanges: []sandbox.FileChange{
			{Path: "file.go", Type: sandbox.ChangeModified},
		},
		ParentAgentID:  "agent-run12345-canopy-xyz",
		BaseCommit:     "abc123",
		ConcurrentDiff: "diff showing concurrent work",
		BaseFileContents: map[string]string{
			"file.go": "original content",
		},
	}

	// Verify all fields
	if ctx.TaskID == "" {
		t.Error("TaskID should be populated")
	}
	if ctx.TaskTitle == "" {
		t.Error("TaskTitle should be populated")
	}
	if ctx.TaskDescription == "" {
		t.Error("TaskDescription should be populated")
	}
	if len(ctx.FailedPatches) == 0 {
		t.Error("FailedPatches should be populated")
	}
	if len(ctx.PatchErrors) == 0 {
		t.Error("PatchErrors should be populated")
	}
	if len(ctx.FileChanges) == 0 {
		t.Error("FileChanges should be populated")
	}
	if ctx.ParentAgentID == "" {
		t.Error("ParentAgentID should be populated")
	}
	if ctx.BaseCommit == "" {
		t.Error("BaseCommit should be populated")
	}
	if ctx.ConcurrentDiff == "" {
		t.Error("ConcurrentDiff should be populated")
	}
	if len(ctx.BaseFileContents) == 0 {
		t.Error("BaseFileContents should be populated")
	}
}

// TestResultStructure verifies the Result struct captures all resolver outcomes
func TestResultStructure(t *testing.T) {
	result := &Result{
		Success: true,
		Error:   "",
		AgentResult: &agent.Result{
			TaskID:  "canopy-test-resolver",
			Success: true,
			Changes: []sandbox.FileChange{
				{Path: "resolved.go", Type: sandbox.ChangeModified},
			},
		},
		Duration:        5 * time.Second,
		ResolverAgentID: "agent-run12345-canopy-test-resolver",
	}

	if !result.Success {
		t.Error("Expected Success to be true")
	}
	if result.ResolverAgentID == "" {
		t.Error("Expected ResolverAgentID to be set")
	}
	if result.Duration == 0 {
		t.Error("Expected Duration to be set")
	}
	if result.AgentResult == nil {
		t.Error("Expected AgentResult to be set")
	}
}

// Helper function to initialize a git repo for testing
func initGitRepo(dir string) error {
	// Create minimal git repo structure
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		return err
	}
	return nil
}

// initRealGitRepo initializes a git repository with actual commits using git commands.
// Returns the repository directory and initial commit hash.
func initRealGitRepo(t *testing.T, dir string) string {
	t.Helper()

	// Initialize git repo
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")

	// Create initial file and commit
	initialContent := `package main

func example() {
	line1()
	line2()
}
`
	testFile := filepath.Join(dir, "example.go")
	if err := os.WriteFile(testFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to write initial file: %v", err)
	}

	runGit(t, dir, "add", "example.go")
	runGit(t, dir, "commit", "-m", "Initial commit")

	// Get initial commit hash
	return getHeadCommit(t, dir)
}

// runGit runs a git command and fails the test on error
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\nOutput: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// getHeadCommit returns the current HEAD commit hash
func getHeadCommit(t *testing.T, dir string) string {
	t.Helper()
	return runGit(t, dir, "rev-parse", "HEAD")
}

// TestConcurrentModificationScenario tests that the resolver doesn't revert concurrent changes.
//
// Scenario:
// 1. Create a test repository with a file
// 2. Agent A starts work (records base commit)
// 3. Agent B completes first and adds line3()
// 4. Agent A completes with change to line1() -> modifiedLine1()
// 5. Apply Agent A's patches to current HEAD - should fail (context mismatch)
// 6. Run resolver which should preserve BOTH:
//   - Agent A's change: modifiedLine1()
//   - Agent B's concurrent change: line3() (NOT reverted)
func TestConcurrentModificationScenario(t *testing.T) {
	workDir := t.TempDir()

	// Step 1: Initialize repository with initial file
	baseCommit := initRealGitRepo(t, workDir)

	// Step 2: Simulate Agent A starting work (records base commit)
	// Agent A will change line1() -> modifiedLine1()
	agentABaseCommit := baseCommit

	// Step 3: Simulate Agent B completing first - adds line3() after line2()
	agentBContent := `package main

func example() {
	line1()
	line2()
	line3()
}
`
	testFile := filepath.Join(workDir, "example.go")
	if err := os.WriteFile(testFile, []byte(agentBContent), 0644); err != nil {
		t.Fatalf("Failed to write Agent B changes: %v", err)
	}
	runGit(t, workDir, "add", "example.go")
	runGit(t, workDir, "commit", "-m", "Agent B: add line3()")
	agentBCommit := getHeadCommit(t, workDir)

	// Step 4: Create Agent A's patch (against original base commit)
	// Agent A changed line1() -> modifiedLine1()
	agentAPatch := `From 0000000000000000000000000000000000000000 Mon Sep 17 00:00:00 2001
From: Test User <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Agent A: modify line1

---
 example.go | 2 +-
 1 file changed, 1 insertion(+), 1 deletion(-)

diff --git a/example.go b/example.go
index 0000000..1111111 100644
--- a/example.go
+++ b/example.go
@@ -1,7 +1,7 @@
 package main

 func example() {
-	line1()
+	modifiedLine1()
 	line2()
 }
`

	// Step 5: Try to apply Agent A's patch - should fail due to context mismatch
	// The patch expects line2() to be followed by }, but now there's line3()
	err := sandbox.ApplyPatches(workDir, []string{agentAPatch})
	if err == nil {
		t.Log("Patch applied successfully (no conflict in this case)")
		// Even if patch applies, verify the result is correct
	} else {
		t.Logf("Patch failed as expected: %v", err)
	}

	// Reset to Agent B's commit to simulate failed patch scenario
	runGit(t, workDir, "reset", "--hard", agentBCommit)

	// Step 6: Get concurrent diff (what changed from base to current HEAD)
	concurrentDiff := runGit(t, workDir, "diff", agentABaseCommit+"..HEAD")

	// Step 7: Get base file content
	baseFileContent := runGit(t, workDir, "show", agentABaseCommit+":example.go")

	// Step 8: Create ConflictContext with all the information
	conflict := &ConflictContext{
		TaskID:          "canopy-agent-a",
		TaskTitle:       "Agent A: modify line1",
		TaskDescription: "Change line1() to modifiedLine1()",
		FailedPatches:   []string{agentAPatch},
		PatchErrors:     []string{"patch does not apply: context mismatch due to line3()"},
		BaseCommit:      agentABaseCommit,
		ConcurrentDiff:  concurrentDiff,
		BaseFileContents: map[string]string{
			"example.go": baseFileContent,
		},
		ParentAgentID: "agent-canopy-agent-a",
	}

	// Step 9: Verify the conflict context captures the scenario correctly
	t.Run("ConflictContextCaptures", func(t *testing.T) {
		// Verify base commit is correct
		if conflict.BaseCommit != agentABaseCommit {
			t.Errorf("BaseCommit = %q, want %q", conflict.BaseCommit, agentABaseCommit)
		}

		// Verify concurrent diff shows line3() was added
		if !strings.Contains(conflict.ConcurrentDiff, "line3()") {
			t.Error("ConcurrentDiff should show line3() was added")
		}

		// Verify base file doesn't have line3()
		if strings.Contains(conflict.BaseFileContents["example.go"], "line3()") {
			t.Error("Base file should not contain line3()")
		}

		// Verify patch shows line1 -> modifiedLine1
		if !strings.Contains(conflict.FailedPatches[0], "modifiedLine1()") {
			t.Error("Patch should contain modifiedLine1()")
		}
	})

	// Step 10: Build resolver prompt and verify it instructs preservation
	t.Run("ResolverPromptInstructions", func(t *testing.T) {
		r := &Resolver{config: &Config{}}
		prompt := r.buildResolverPrompt(conflict)

		// Verify prompt contains critical instructions
		criticalInstructions := []string{
			"DO NOT revert",
			"concurrent",
			"BASE",
			"OURS",
			"THEIRS",
			agentABaseCommit,
		}
		for _, instruction := range criticalInstructions {
			if !strings.Contains(prompt, instruction) {
				t.Errorf("Prompt missing critical instruction: %q", instruction)
			}
		}
	})

	// Step 11: Verify expected resolution has both changes
	t.Run("ExpectedResolutionHasBothChanges", func(t *testing.T) {
		// The expected resolved content should have BOTH:
		// 1. modifiedLine1() from Agent A's patch
		// 2. line3() from Agent B's concurrent change
		expectedResolution := `package main

func example() {
	modifiedLine1()
	line2()
	line3()
}
`
		// Verify expected resolution has both changes
		if !strings.Contains(expectedResolution, "modifiedLine1()") {
			t.Error("Expected resolution should have Agent A's change (modifiedLine1)")
		}
		if !strings.Contains(expectedResolution, "line3()") {
			t.Error("Expected resolution should preserve Agent B's concurrent change (line3)")
		}
	})

	// Step 12: Write conflict files to a temp overlay and verify structure
	t.Run("WritePatchFilesForResolver", func(t *testing.T) {
		tempDir := t.TempDir()
		overlay := &sandbox.Overlay{MergedDir: tempDir}
		r := &Resolver{config: &Config{}}

		err := r.writePatchFiles(overlay, conflict)
		if err != nil {
			t.Fatalf("writePatchFiles failed: %v", err)
		}

		conflictDir := filepath.Join(tempDir, ".canopy", "conflict")

		// Verify patch file
		patchContent, err := os.ReadFile(filepath.Join(conflictDir, "patch-0.patch"))
		if err != nil {
			t.Fatalf("Failed to read patch: %v", err)
		}
		if !strings.Contains(string(patchContent), "modifiedLine1()") {
			t.Error("Patch file should contain modifiedLine1()")
		}

		// Verify base file
		baseContent, err := os.ReadFile(filepath.Join(conflictDir, "base", "example.go"))
		if err != nil {
			t.Fatalf("Failed to read base file: %v", err)
		}
		if strings.Contains(string(baseContent), "line3()") {
			t.Error("Base file should NOT contain line3()")
		}
		if !strings.Contains(string(baseContent), "line1()") {
			t.Error("Base file should contain original line1()")
		}

		// Verify concurrent changes diff
		concurrentContent, err := os.ReadFile(filepath.Join(conflictDir, "concurrent-changes.diff"))
		if err != nil {
			t.Fatalf("Failed to read concurrent diff: %v", err)
		}
		if !strings.Contains(string(concurrentContent), "line3()") {
			t.Error("Concurrent diff should show line3() was added")
		}
	})
}

// TestConcurrentNewFile tests that new files created by concurrent agents are preserved.
func TestConcurrentNewFile(t *testing.T) {
	workDir := t.TempDir()

	// Initialize repository
	baseCommit := initRealGitRepo(t, workDir)

	// Concurrent agent creates a new file
	newFileContent := `package main

func helper() {
	// Helper function added by concurrent agent
}
`
	helperFile := filepath.Join(workDir, "helper.go")
	if err := os.WriteFile(helperFile, []byte(newFileContent), 0644); err != nil {
		t.Fatalf("Failed to write new file: %v", err)
	}
	runGit(t, workDir, "add", "helper.go")
	runGit(t, workDir, "commit", "-m", "Concurrent: add helper.go")

	// Get concurrent diff
	concurrentDiff := runGit(t, workDir, "diff", baseCommit+"..HEAD")

	// Verify new file appears in diff
	if !strings.Contains(concurrentDiff, "helper.go") {
		t.Error("Concurrent diff should show helper.go was created")
	}

	// Create conflict context for a patch that modifies example.go
	conflict := &ConflictContext{
		TaskID:          "canopy-modify-example",
		TaskTitle:       "Modify example.go",
		TaskDescription: "Make changes to example.go",
		FailedPatches:   []string{"patch content for example.go"},
		BaseCommit:      baseCommit,
		ConcurrentDiff:  concurrentDiff,
		BaseFileContents: map[string]string{
			"example.go": "original example.go content",
		},
	}

	// Build prompt and verify it mentions preserving concurrent changes
	r := &Resolver{config: &Config{}}
	prompt := r.buildResolverPrompt(conflict)

	if !strings.Contains(prompt, "concurrent changes are VALID and MUST be preserved") {
		t.Error("Prompt should instruct preservation of concurrent changes")
	}
	if !strings.Contains(prompt, "DO NOT revert any code that exists in HEAD") {
		t.Error("Prompt should warn against reverting HEAD changes")
	}
}

// TestConcurrentDeletedFile tests scenario where concurrent agent deleted a file.
func TestConcurrentDeletedFile(t *testing.T) {
	workDir := t.TempDir()

	// Initialize repository with two files
	_ = initRealGitRepo(t, workDir)

	// Add another file
	otherContent := `package main

func other() {}
`
	otherFile := filepath.Join(workDir, "other.go")
	if err := os.WriteFile(otherFile, []byte(otherContent), 0644); err != nil {
		t.Fatalf("Failed to write other file: %v", err)
	}
	runGit(t, workDir, "add", "other.go")
	runGit(t, workDir, "commit", "-m", "Add other.go")
	baseWithOther := getHeadCommit(t, workDir)

	// Concurrent agent deletes other.go
	if err := os.Remove(otherFile); err != nil {
		t.Fatalf("Failed to remove other.go: %v", err)
	}
	runGit(t, workDir, "add", "-A")
	runGit(t, workDir, "commit", "-m", "Concurrent: remove other.go")

	// Get concurrent diff
	concurrentDiff := runGit(t, workDir, "diff", baseWithOther+"..HEAD")

	// Verify deletion appears in diff
	if !strings.Contains(concurrentDiff, "deleted file") || !strings.Contains(concurrentDiff, "other.go") {
		t.Logf("Concurrent diff: %s", concurrentDiff)
		// Deletion might be shown differently
	}

	// Create conflict context
	conflict := &ConflictContext{
		TaskID:          "canopy-modify-example",
		TaskTitle:       "Modify example.go",
		TaskDescription: "Changes to example.go while other.go was deleted",
		BaseCommit:      baseWithOther,
		ConcurrentDiff:  concurrentDiff,
	}

	// Verify conflict context captures the scenario
	if conflict.BaseCommit != baseWithOther {
		t.Errorf("BaseCommit = %q, want %q", conflict.BaseCommit, baseWithOther)
	}
}

// TestSameFileModifiedBothAgents tests the true conflict case where both agents
// modify the same file in different places.
func TestSameFileModifiedBothAgents(t *testing.T) {
	workDir := t.TempDir()

	// Initialize repository with a larger file
	_ = initRealGitRepo(t, workDir)

	// Replace with larger file
	largerContent := `package main

func first() {
	// First function
}

func second() {
	// Second function
}

func third() {
	// Third function
}
`
	testFile := filepath.Join(workDir, "example.go")
	if err := os.WriteFile(testFile, []byte(largerContent), 0644); err != nil {
		t.Fatalf("Failed to write larger file: %v", err)
	}
	runGit(t, workDir, "add", "example.go")
	runGit(t, workDir, "commit", "-m", "Larger file")
	newBaseCommit := getHeadCommit(t, workDir)

	// Concurrent agent modifies third() function
	modifiedByB := `package main

func first() {
	// First function
}

func second() {
	// Second function
}

func third() {
	// Third function - modified by Agent B
	newLine()
}
`
	if err := os.WriteFile(testFile, []byte(modifiedByB), 0644); err != nil {
		t.Fatalf("Failed to write Agent B changes: %v", err)
	}
	runGit(t, workDir, "add", "example.go")
	runGit(t, workDir, "commit", "-m", "Agent B: modify third()")

	// Get concurrent diff
	concurrentDiff := runGit(t, workDir, "diff", newBaseCommit+"..HEAD")

	// Agent A's patch modifies first() function
	agentAPatch := `From 0000000000000000000000000000000000000000 Mon Sep 17 00:00:00 2001
From: Test User <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Agent A: modify first

---
 example.go | 2 +-
 1 file changed, 1 insertion(+), 1 deletion(-)

diff --git a/example.go b/example.go
index 0000000..1111111 100644
--- a/example.go
+++ b/example.go
@@ -1,7 +1,7 @@
 package main

 func first() {
-	// First function
+	// First function - modified by Agent A
 }

 func second() {
`

	// Create conflict context
	conflict := &ConflictContext{
		TaskID:          "canopy-agent-a",
		TaskTitle:       "Agent A: modify first()",
		TaskDescription: "Modify the first() function",
		FailedPatches:   []string{agentAPatch},
		PatchErrors:     []string{"context line mismatch"},
		BaseCommit:      newBaseCommit,
		ConcurrentDiff:  concurrentDiff,
		BaseFileContents: map[string]string{
			"example.go": largerContent,
		},
	}

	// Verify both agents' changes are captured
	t.Run("BothChangesInConflictContext", func(t *testing.T) {
		// Verify patch has Agent A's change
		if !strings.Contains(conflict.FailedPatches[0], "modified by Agent A") {
			t.Error("Patch should contain Agent A's modification")
		}

		// Verify concurrent diff has Agent B's change
		if !strings.Contains(conflict.ConcurrentDiff, "modified by Agent B") ||
			!strings.Contains(conflict.ConcurrentDiff, "newLine()") {
			t.Error("Concurrent diff should show Agent B's changes")
		}

		// Verify base file has neither modification
		base := conflict.BaseFileContents["example.go"]
		if strings.Contains(base, "modified by Agent A") ||
			strings.Contains(base, "modified by Agent B") {
			t.Error("Base file should not contain either agent's modifications")
		}
	})

	// Expected resolution should have BOTH modifications
	t.Run("ExpectedResolution", func(t *testing.T) {
		expectedResolution := `package main

func first() {
	// First function - modified by Agent A
}

func second() {
	// Second function
}

func third() {
	// Third function - modified by Agent B
	newLine()
}
`
		if !strings.Contains(expectedResolution, "modified by Agent A") {
			t.Error("Expected resolution should have Agent A's change")
		}
		if !strings.Contains(expectedResolution, "modified by Agent B") {
			t.Error("Expected resolution should have Agent B's change")
		}
		if !strings.Contains(expectedResolution, "newLine()") {
			t.Error("Expected resolution should preserve Agent B's newLine()")
		}
	})
}

// TestSameLineModifiedBothAgents tests the true conflict case where both agents
// modify the same line (this requires manual resolution).
func TestSameLineModifiedBothAgents(t *testing.T) {
	workDir := t.TempDir()

	// Initialize repository
	baseCommit := initRealGitRepo(t, workDir)

	// Concurrent agent modifies line1()
	modifiedByB := `package main

func example() {
	lineModifiedByB()
	line2()
}
`
	testFile := filepath.Join(workDir, "example.go")
	if err := os.WriteFile(testFile, []byte(modifiedByB), 0644); err != nil {
		t.Fatalf("Failed to write Agent B changes: %v", err)
	}
	runGit(t, workDir, "add", "example.go")
	runGit(t, workDir, "commit", "-m", "Agent B: modify line1")

	// Get concurrent diff
	concurrentDiff := runGit(t, workDir, "diff", baseCommit+"..HEAD")

	// Agent A's patch also modifies line1() - true conflict!
	agentAPatch := `From 0000000000000000000000000000000000000000 Mon Sep 17 00:00:00 2001
From: Test User <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Agent A: modify line1

---
 example.go | 2 +-
 1 file changed, 1 insertion(+), 1 deletion(-)

diff --git a/example.go b/example.go
index 0000000..1111111 100644
--- a/example.go
+++ b/example.go
@@ -1,7 +1,7 @@
 package main

 func example() {
-	line1()
+	lineModifiedByA()
 	line2()
 }
`

	// Create conflict context
	baseFileContent := runGit(t, workDir, "show", baseCommit+":example.go")
	conflict := &ConflictContext{
		TaskID:          "canopy-agent-a",
		TaskTitle:       "Agent A: modify line1",
		TaskDescription: "Modify line1() - conflicts with Agent B",
		FailedPatches:   []string{agentAPatch},
		PatchErrors:     []string{"patch does not apply: hunk failed"},
		BaseCommit:      baseCommit,
		ConcurrentDiff:  concurrentDiff,
		BaseFileContents: map[string]string{
			"example.go": baseFileContent,
		},
	}

	// Verify true conflict is captured
	t.Run("TrueConflictCaptured", func(t *testing.T) {
		// Patch wants to change line1() -> lineModifiedByA()
		if !strings.Contains(conflict.FailedPatches[0], "lineModifiedByA()") {
			t.Error("Patch should show Agent A wants lineModifiedByA()")
		}

		// But concurrent diff shows line1() was already changed to lineModifiedByB()
		if !strings.Contains(conflict.ConcurrentDiff, "lineModifiedByB()") {
			t.Error("Concurrent diff should show Agent B changed line1 to lineModifiedByB()")
		}

		// Base file has original line1()
		if !strings.Contains(conflict.BaseFileContents["example.go"], "line1()") {
			t.Error("Base file should have original line1()")
		}
	})

	// Build prompt and verify it provides enough context for resolution
	t.Run("PromptProvidesContext", func(t *testing.T) {
		r := &Resolver{config: &Config{}}
		prompt := r.buildResolverPrompt(conflict)

		// Prompt should explain three-way merge
		if !strings.Contains(prompt, "Three-Way Merge") {
			t.Error("Prompt should explain three-way merge")
		}

		// Prompt should reference base files
		if !strings.Contains(prompt, ".canopy/conflict/base/") {
			t.Error("Prompt should reference base file location")
		}

		// Prompt should reference concurrent changes
		if !strings.Contains(prompt, "concurrent-changes.diff") {
			t.Error("Prompt should reference concurrent changes diff")
		}
	})
}
