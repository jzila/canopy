package merge

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/sandbox"
)

func TestIsBeadsFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{".beads", true},
		{".beads/issues.jsonl", true},
		{".beads/config.yaml", true},
		{"src/main.go", false},
		{"beads.txt", false},
		{".beads.txt", false},
	}

	for _, tt := range tests {
		result := isBeadsFile(tt.path)
		if result != tt.expected {
			t.Errorf("isBeadsFile(%q) = %v, expected %v", tt.path, result, tt.expected)
		}
	}
}

func TestIsCanopyFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{".canopy", true},
		{".canopy/conflict/errors.txt", true},
		{".canopy/conflict/original-task.txt", true},
		{".canopy/conflict/patch-0.patch", true},
		{"src/main.go", false},
		{"canopy.txt", false},
		{".canopy.txt", false},
	}

	for _, tt := range tests {
		result := isCanopyFile(tt.path)
		if result != tt.expected {
			t.Errorf("isCanopyFile(%q) = %v, expected %v", tt.path, result, tt.expected)
		}
	}
}

func TestMergeWithBeadsChanges(t *testing.T) {
	// Create temporary directories for testing
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	overlayDir := filepath.Join(tempDir, "overlay")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a mock overlay with .beads changes
	overlay := &sandbox.Overlay{
		UpperDir: filepath.Join(overlayDir, "upper"),
	}
	if err := os.MkdirAll(overlay.UpperDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create mock .beads directory structure
	beadsDir := filepath.Join(overlay.UpperDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a mock issues.jsonl file
	issuesFile := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(issuesFile, []byte("test data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a regular file change
	regularFile := filepath.Join(overlay.UpperDir, "test.txt")
	if err := os.WriteFile(regularFile, []byte("regular content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create results with both beads and regular changes
	results := []*agent.Result{
		{
			TaskID:  "test-1",
			Success: true,
			Overlay: overlay,
			Changes: []sandbox.FileChange{
				{Path: ".beads/issues.jsonl", Type: sandbox.ChangeModified},
				{Path: "test.txt", Type: sandbox.ChangeCreated},
			},
		},
	}

	// Create merger
	merger := NewSequentialMerger(outputDir, tempDir, true)

	// Merge should skip .beads files since agents don't have access to them
	result, err := merger.Merge(results)
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Verify that .beads changes were skipped
	for _, applied := range result.Applied {
		if strings.HasPrefix(applied.Path, ".beads/") {
			t.Errorf("Expected .beads files to be skipped, but found: %s", applied.Path)
		}
	}

	// Verify that regular file was applied
	foundRegular := false
	for _, applied := range result.Applied {
		if applied.Path == "test.txt" {
			foundRegular = true
			break
		}
	}
	if !foundRegular {
		t.Error("Expected test.txt to be applied")
	}
}

func TestMergeWithCanopyChanges(t *testing.T) {
	// Create temporary directories for testing
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	overlayDir := filepath.Join(tempDir, "overlay")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a mock overlay with .canopy changes
	overlay := &sandbox.Overlay{
		UpperDir: filepath.Join(overlayDir, "upper"),
	}
	if err := os.MkdirAll(overlay.UpperDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create mock .canopy/conflict directory structure
	canopyDir := filepath.Join(overlay.UpperDir, ".canopy", "conflict")
	if err := os.MkdirAll(canopyDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create mock resolver conflict files
	errorsFile := filepath.Join(canopyDir, "errors.txt")
	if err := os.WriteFile(errorsFile, []byte("error details"), 0644); err != nil {
		t.Fatal(err)
	}
	patchFile := filepath.Join(canopyDir, "patch-0.patch")
	if err := os.WriteFile(patchFile, []byte("patch content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a regular file change
	regularFile := filepath.Join(overlay.UpperDir, "test.txt")
	if err := os.WriteFile(regularFile, []byte("regular content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create results with both canopy and regular changes
	results := []*agent.Result{
		{
			TaskID:  "test-1",
			Success: true,
			Overlay: overlay,
			Changes: []sandbox.FileChange{
				{Path: ".canopy/conflict/errors.txt", Type: sandbox.ChangeCreated},
				{Path: ".canopy/conflict/patch-0.patch", Type: sandbox.ChangeCreated},
				{Path: "test.txt", Type: sandbox.ChangeCreated},
			},
		},
	}

	// Create merger
	merger := NewSequentialMerger(outputDir, tempDir, true)

	// Merge should skip .canopy files to avoid leaking resolver context
	result, err := merger.Merge(results)
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Verify that .canopy changes were skipped
	for _, applied := range result.Applied {
		if strings.HasPrefix(applied.Path, ".canopy/") {
			t.Errorf("Expected .canopy files to be skipped, but found: %s", applied.Path)
		}
	}

	// Verify that regular file was applied
	foundRegular := false
	for _, applied := range result.Applied {
		if applied.Path == "test.txt" {
			foundRegular = true
			break
		}
	}
	if !foundRegular {
		t.Error("Expected test.txt to be applied")
	}

	// Verify that .canopy files were not created in output
	canopyOutputDir := filepath.Join(outputDir, ".canopy")
	if _, err := os.Stat(canopyOutputDir); !os.IsNotExist(err) {
		t.Errorf("Expected .canopy directory to not exist in output, but it does")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestGetCurrentHead(t *testing.T) {
	// Create a temp git repo
	tempDir := t.TempDir()
	initGitRepo(t, tempDir)

	merger := NewSequentialMerger(tempDir, tempDir, true)
	head, err := merger.getCurrentHead()
	if err != nil {
		t.Fatalf("getCurrentHead failed: %v", err)
	}

	if len(head) != 40 {
		t.Errorf("Expected 40-char hash, got %d chars: %s", len(head), head)
	}
}

func TestResetToHead(t *testing.T) {
	// Create a temp git repo
	tempDir := t.TempDir()
	initGitRepo(t, tempDir)

	// Create a file and commit it
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, tempDir, "add", "test.txt")
	runGit(t, tempDir, "commit", "-m", "add test file")

	// Get HEAD
	merger := NewSequentialMerger(tempDir, tempDir, true)
	head, err := merger.getCurrentHead()
	if err != nil {
		t.Fatal(err)
	}

	// Modify the file (simulating failed merge)
	if err := os.WriteFile(testFile, []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify file was modified
	content, _ := os.ReadFile(testFile)
	if string(content) != "modified" {
		t.Fatalf("File should be modified, got: %s", content)
	}

	// Reset to HEAD
	err = merger.resetToHead(head, []string{"test.txt"})
	if err != nil {
		t.Fatalf("resetToHead failed: %v", err)
	}

	// Verify file was restored
	content, _ = os.ReadFile(testFile)
	if string(content) != "original" {
		t.Errorf("File should be restored to 'original', got: %s", content)
	}
}

func TestMergeSingleResetsOnCommitFailure(t *testing.T) {
	// Create a temp git repo
	tempDir := t.TempDir()
	overlayDir := filepath.Join(tempDir, "overlay")
	initGitRepo(t, tempDir)

	// Create a file and commit it
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, tempDir, "add", "test.txt")
	runGit(t, tempDir, "commit", "-m", "add test file")

	// Create overlay with a change to same file
	overlay := &sandbox.Overlay{
		UpperDir: filepath.Join(overlayDir, "upper"),
	}
	if err := os.MkdirAll(overlay.UpperDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create modified file in overlay
	overlayFile := filepath.Join(overlay.UpperDir, "test.txt")
	if err := os.WriteFile(overlayFile, []byte("modified by agent"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create agent result
	result := &agent.Result{
		TaskID:  "test-task",
		Success: true,
		Overlay: overlay,
		Changes: []sandbox.FileChange{
			{Path: "test.txt", Type: sandbox.ChangeModified},
		},
	}

	// Create merger
	merger := NewSequentialMerger(tempDir, tempDir, true)

	// MergeSingle should apply changes and commit successfully
	mergeResult, err := merger.MergeSingle(result, nil)
	if err != nil {
		t.Fatalf("MergeSingle failed: %v", err)
	}

	// Check that commit was applied
	if mergeResult.CommitsApplied != 1 {
		t.Errorf("Expected 1 commit, got %d", mergeResult.CommitsApplied)
	}

	// Verify file was changed
	content, _ := os.ReadFile(testFile)
	if string(content) != "modified by agent" {
		t.Errorf("File should be modified, got: %s", content)
	}
}

// initGitRepo initializes a git repo in the given directory
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test User")
	// Create initial commit so HEAD exists
	readmeFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmeFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "initial")
}

// runGit runs a git command in the given directory
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
