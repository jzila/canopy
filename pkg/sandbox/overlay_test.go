package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetChanges_ContentComparison(t *testing.T) {
	// Create temporary directories to simulate lower and upper
	tmpDir := t.TempDir()
	lowerDir := filepath.Join(tmpDir, "lower")
	baseDir := filepath.Join(tmpDir, "overlays")

	// Create lower directory with test files
	if err := os.MkdirAll(lowerDir, 0755); err != nil {
		t.Fatalf("failed to create lower dir: %v", err)
	}

	// Create a test file in lower
	testContent := []byte("original content")
	testFile := filepath.Join(lowerDir, "testfile.txt")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Create overlay (this creates upper, work, merged directories)
	overlay, err := NewOverlay(baseDir, lowerDir)
	if err != nil {
		t.Fatalf("failed to create overlay: %v", err)
	}
	defer overlay.Cleanup()

	t.Run("identical content not reported as change", func(t *testing.T) {
		// Copy same content to upper (simulates copy-up without modification)
		upperFile := filepath.Join(overlay.UpperDir, "testfile.txt")
		if err := os.WriteFile(upperFile, testContent, 0644); err != nil {
			t.Fatalf("failed to write upper file: %v", err)
		}

		changes, err := overlay.GetChanges()
		if err != nil {
			t.Fatalf("GetChanges failed: %v", err)
		}

		// Should not report testfile.txt as a change since content is identical
		for _, change := range changes {
			if change.Path == "testfile.txt" {
				t.Errorf("file with identical content should not be reported as change, got: %+v", change)
			}
		}

		// Clean up for next test
		os.Remove(upperFile)
	})

	t.Run("modified content reported as change", func(t *testing.T) {
		// Write different content to upper
		upperFile := filepath.Join(overlay.UpperDir, "testfile.txt")
		modifiedContent := []byte("modified content")
		if err := os.WriteFile(upperFile, modifiedContent, 0644); err != nil {
			t.Fatalf("failed to write upper file: %v", err)
		}

		changes, err := overlay.GetChanges()
		if err != nil {
			t.Fatalf("GetChanges failed: %v", err)
		}

		// Should report testfile.txt as modified
		var found bool
		for _, change := range changes {
			if change.Path == "testfile.txt" {
				found = true
				if change.Type != ChangeModified {
					t.Errorf("expected ChangeModified, got %v", change.Type)
				}
				if change.NewHash == "" {
					t.Error("expected NewHash to be set")
				}
				break
			}
		}
		if !found {
			t.Error("modified file not found in changes")
		}

		// Clean up for next test
		os.Remove(upperFile)
	})

	t.Run("new file reported as created", func(t *testing.T) {
		// Create a file that doesn't exist in lower
		upperFile := filepath.Join(overlay.UpperDir, "newfile.txt")
		if err := os.WriteFile(upperFile, []byte("new content"), 0644); err != nil {
			t.Fatalf("failed to write new file: %v", err)
		}

		changes, err := overlay.GetChanges()
		if err != nil {
			t.Fatalf("GetChanges failed: %v", err)
		}

		var found bool
		for _, change := range changes {
			if change.Path == "newfile.txt" {
				found = true
				if change.Type != ChangeCreated {
					t.Errorf("expected ChangeCreated, got %v", change.Type)
				}
				break
			}
		}
		if !found {
			t.Error("new file not found in changes")
		}

		// Clean up
		os.Remove(upperFile)
	})
}

func TestNewDirectOverlay(t *testing.T) {
	tmpDir := t.TempDir()

	overlay := NewDirectOverlay(tmpDir)

	if overlay == nil {
		t.Fatal("NewDirectOverlay returned nil")
	}

	t.Run("fields set correctly", func(t *testing.T) {
		if overlay.ID != "direct" {
			t.Errorf("expected ID 'direct', got %q", overlay.ID)
		}
		if overlay.MergedDir != tmpDir {
			t.Errorf("expected MergedDir %q, got %q", tmpDir, overlay.MergedDir)
		}
		if overlay.LowerDir != tmpDir {
			t.Errorf("expected LowerDir %q, got %q", tmpDir, overlay.LowerDir)
		}
		if overlay.UpperDir != "" {
			t.Errorf("expected empty UpperDir, got %q", overlay.UpperDir)
		}
		if overlay.WorkDir != "" {
			t.Errorf("expected empty WorkDir, got %q", overlay.WorkDir)
		}
	})

	t.Run("GetChanges returns empty for direct overlay", func(t *testing.T) {
		// Create some files in the directory
		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		changes, err := overlay.GetChanges()
		if err != nil {
			t.Errorf("GetChanges() returned error: %v", err)
		}
		if len(changes) != 0 {
			t.Errorf("expected empty changes for direct overlay, got %d", len(changes))
		}
	})

	t.Run("HasGitRepo works on direct overlay", func(t *testing.T) {
		// Should return false since tmpDir doesn't have .git
		if overlay.HasGitRepo() {
			t.Error("expected HasGitRepo() to return false for non-git dir")
		}

		// Create .git dir
		gitDir := filepath.Join(tmpDir, ".git")
		if err := os.MkdirAll(gitDir, 0755); err != nil {
			t.Fatalf("failed to create .git: %v", err)
		}

		if !overlay.HasGitRepo() {
			t.Error("expected HasGitRepo() to return true after creating .git")
		}
	})
}
