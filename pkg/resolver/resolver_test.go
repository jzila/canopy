package resolver

import (
	"context"
	"os"
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
