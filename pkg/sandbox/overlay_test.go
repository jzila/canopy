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
