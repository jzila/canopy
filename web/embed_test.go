package web

import (
	"io/fs"
	"os"
	"testing"
)

func TestGetFS(t *testing.T) {
	// Change to project root for dev mode
	if err := os.Chdir(".."); err != nil {
		t.Fatalf("Failed to change to project root: %v", err)
	}

	fsys, err := GetFS()
	if err != nil {
		t.Fatalf("GetFS() failed: %v", err)
	}

	// Check that we can read the root directory
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		t.Fatalf("ReadDir('.') failed: %v", err)
	}

	if len(entries) == 0 {
		t.Error("Expected at least one entry in the dist directory")
	}

	// Verify index.html exists
	_, err = fs.Stat(fsys, "index.html")
	if err != nil {
		t.Errorf("index.html not found: %v", err)
	}
}
