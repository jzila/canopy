package merge

import (
	"os"
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
