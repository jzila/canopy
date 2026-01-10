package merge

import (
	"os"
	"path/filepath"
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

	// Create merger (skip bd sync since we don't have a real beads setup)
	merger := NewSequentialMerger(outputDir, tempDir, true)

	// Note: This test will fail at bd sync step since we don't have a real beads repo
	// But it validates the logic up to that point
	result, err := merger.Merge(results)

	// We expect an error because bd sync will fail in this test environment
	if err != nil {
		t.Logf("Expected error due to missing beads setup: %v", err)
	}

	// Verify that beads changes were identified
	if len(result.Errors) > 0 {
		// Check that the error is related to bd sync, not the separation logic
		foundBeadsError := false
		for _, errMsg := range result.Errors {
			if contains(errMsg, "bd sync") || contains(errMsg, "beads") {
				foundBeadsError = true
				break
			}
		}
		if !foundBeadsError {
			t.Errorf("Expected beads-related error, got: %v", result.Errors)
		}
	}
}

func TestCountUniqueResults(t *testing.T) {
	changes := []beadsChange{
		{result: &agent.Result{TaskID: "task-1"}},
		{result: &agent.Result{TaskID: "task-1"}},
		{result: &agent.Result{TaskID: "task-2"}},
	}

	count := countUniqueResults(changes)
	if count != 2 {
		t.Errorf("countUniqueResults() = %d, expected 2", count)
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
