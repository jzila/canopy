package sandbox

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTakeSnapshot(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test file structure
	createTestFile(t, tmpDir, "file1.txt", "content1")
	createTestFile(t, tmpDir, "file2.txt", "content2")
	createTestDir(t, tmpDir, "subdir")
	createTestFile(t, tmpDir, "subdir/nested.txt", "nested content")

	snapshot, err := TakeSnapshot(tmpDir, nil)
	if err != nil {
		t.Fatalf("TakeSnapshot failed: %v", err)
	}

	// Verify files are recorded
	expectedFiles := []string{"file1.txt", "file2.txt", "subdir", "subdir/nested.txt"}
	for _, f := range expectedFiles {
		if _, exists := snapshot.Files[f]; !exists {
			t.Errorf("expected file %q in snapshot", f)
		}
	}

	// Verify file state
	if state, ok := snapshot.Files["file1.txt"]; ok {
		if state.IsDir {
			t.Error("file1.txt should not be a directory")
		}
		if state.Size != int64(len("content1")) {
			t.Errorf("file1.txt size = %d, want %d", state.Size, len("content1"))
		}
	}

	// Verify directory state
	if state, ok := snapshot.Files["subdir"]; ok {
		if !state.IsDir {
			t.Error("subdir should be a directory")
		}
	}
}

func TestSnapshotCompareCreated(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "existing.txt", "content")

	before, err := TakeSnapshot(tmpDir, nil)
	if err != nil {
		t.Fatalf("TakeSnapshot failed: %v", err)
	}

	// Create new file
	createTestFile(t, tmpDir, "new.txt", "new content")

	after, err := TakeSnapshot(tmpDir, nil)
	if err != nil {
		t.Fatalf("TakeSnapshot failed: %v", err)
	}

	changes := before.Compare(after)

	// Should detect one created file
	created := FilterChanges(changes, ChangeCreated)
	if len(created) != 1 {
		t.Fatalf("expected 1 created file, got %d", len(created))
	}
	if created[0].Path != "new.txt" {
		t.Errorf("created file = %q, want %q", created[0].Path, "new.txt")
	}
}

func TestSnapshotCompareModified(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file.txt")
	createTestFile(t, tmpDir, "file.txt", "original")

	before, err := TakeSnapshot(tmpDir, nil)
	if err != nil {
		t.Fatalf("TakeSnapshot failed: %v", err)
	}

	// Modify file (change content and ensure mtime changes)
	time.Sleep(10 * time.Millisecond) // Ensure mtime differs
	if err := os.WriteFile(filePath, []byte("modified content"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	after, err := TakeSnapshot(tmpDir, nil)
	if err != nil {
		t.Fatalf("TakeSnapshot failed: %v", err)
	}

	changes := before.Compare(after)

	modified := FilterChanges(changes, ChangeModified)
	if len(modified) != 1 {
		t.Fatalf("expected 1 modified file, got %d", len(modified))
	}
	if modified[0].Path != "file.txt" {
		t.Errorf("modified file = %q, want %q", modified[0].Path, "file.txt")
	}
}

func TestSnapshotCompareDeleted(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "to-delete.txt")
	createTestFile(t, tmpDir, "to-delete.txt", "content")
	createTestFile(t, tmpDir, "keep.txt", "keep")

	before, err := TakeSnapshot(tmpDir, nil)
	if err != nil {
		t.Fatalf("TakeSnapshot failed: %v", err)
	}

	// Delete file
	if err := os.Remove(filePath); err != nil {
		t.Fatalf("failed to delete file: %v", err)
	}

	after, err := TakeSnapshot(tmpDir, nil)
	if err != nil {
		t.Fatalf("TakeSnapshot failed: %v", err)
	}

	changes := before.Compare(after)

	deleted := FilterChanges(changes, ChangeDeleted)
	if len(deleted) != 1 {
		t.Fatalf("expected 1 deleted file, got %d", len(deleted))
	}
	if deleted[0].Path != "to-delete.txt" {
		t.Errorf("deleted file = %q, want %q", deleted[0].Path, "to-delete.txt")
	}
}

func TestSnapshotExcludes(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files including ones that should be excluded
	createTestFile(t, tmpDir, "include.txt", "content")
	createTestDir(t, tmpDir, ".git")
	createTestFile(t, tmpDir, ".git/config", "git config")
	createTestDir(t, tmpDir, "node_modules")
	createTestFile(t, tmpDir, "node_modules/package.json", "pkg")
	createTestFile(t, tmpDir, "temp.log", "log content")

	snapshot, err := TakeSnapshot(tmpDir, []string{"node_modules", "*.log"})
	if err != nil {
		t.Fatalf("TakeSnapshot failed: %v", err)
	}

	// Should include
	if _, exists := snapshot.Files["include.txt"]; !exists {
		t.Error("include.txt should be in snapshot")
	}

	// Should exclude .git (built-in)
	if _, exists := snapshot.Files[".git"]; exists {
		t.Error(".git should be excluded")
	}
	if _, exists := snapshot.Files[".git/config"]; exists {
		t.Error(".git/config should be excluded")
	}

	// Should exclude node_modules (custom)
	if _, exists := snapshot.Files["node_modules"]; exists {
		t.Error("node_modules should be excluded")
	}
	if _, exists := snapshot.Files["node_modules/package.json"]; exists {
		t.Error("node_modules/package.json should be excluded")
	}

	// Should exclude *.log (glob pattern)
	if _, exists := snapshot.Files["temp.log"]; exists {
		t.Error("temp.log should be excluded")
	}
}

func TestSnapshotHomeExcludedPaths(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directories that match HomeExcludedPaths
	for _, excluded := range HomeExcludedPaths {
		createTestDir(t, tmpDir, excluded)
		createTestFile(t, tmpDir, excluded+"/file.txt", "content")
	}
	createTestFile(t, tmpDir, "regular.txt", "content")

	snapshot, err := TakeSnapshot(tmpDir, nil)
	if err != nil {
		t.Fatalf("TakeSnapshot failed: %v", err)
	}

	// Regular file should be included
	if _, exists := snapshot.Files["regular.txt"]; !exists {
		t.Error("regular.txt should be in snapshot")
	}

	// HomeExcludedPaths should be excluded
	for _, excluded := range HomeExcludedPaths {
		if _, exists := snapshot.Files[excluded]; exists {
			t.Errorf("%s should be excluded", excluded)
		}
		if _, exists := snapshot.Files[excluded+"/file.txt"]; exists {
			t.Errorf("%s/file.txt should be excluded", excluded)
		}
	}
}

func TestSnapshotRefresh(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "file.txt", "content")

	snapshot, err := TakeSnapshot(tmpDir, []string{"*.log"})
	if err != nil {
		t.Fatalf("TakeSnapshot failed: %v", err)
	}

	// Refresh should create a new snapshot with same settings
	refreshed, err := snapshot.Refresh()
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	if refreshed.RootDir != snapshot.RootDir {
		t.Errorf("RootDir = %q, want %q", refreshed.RootDir, snapshot.RootDir)
	}
	if len(refreshed.Excludes) != len(snapshot.Excludes) {
		t.Errorf("Excludes length = %d, want %d", len(refreshed.Excludes), len(snapshot.Excludes))
	}
}

func TestShouldExclude(t *testing.T) {
	tests := []struct {
		name     string
		relPath  string
		excludes []string
		want     bool
	}{
		{
			name:     "exact match",
			relPath:  "node_modules",
			excludes: []string{"node_modules"},
			want:     true,
		},
		{
			name:     "directory prefix",
			relPath:  "node_modules/package/index.js",
			excludes: []string{"node_modules"},
			want:     true,
		},
		{
			name:     "glob pattern",
			relPath:  "debug.log",
			excludes: []string{"*.log"},
			want:     true,
		},
		{
			name:     "nested glob pattern",
			relPath:  "logs/debug.log",
			excludes: []string{"*.log"},
			want:     true,
		},
		{
			name:     "no match",
			relPath:  "src/main.go",
			excludes: []string{"node_modules", "*.log"},
			want:     false,
		},
		{
			name:     ".git builtin",
			relPath:  ".git",
			excludes: nil,
			want:     true,
		},
		{
			name:     ".git nested",
			relPath:  ".git/objects/pack",
			excludes: nil,
			want:     true,
		},
		{
			name:     "partial name no match",
			relPath:  "node_modules_backup",
			excludes: []string{"node_modules"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldExclude(tt.relPath, tt.excludes)
			if got != tt.want {
				t.Errorf("shouldExclude(%q, %v) = %v, want %v", tt.relPath, tt.excludes, got, tt.want)
			}
		})
	}
}

func TestFilterChanges(t *testing.T) {
	changes := []Change{
		{Path: "new.txt", Type: ChangeCreated},
		{Path: "modified.txt", Type: ChangeModified},
		{Path: "deleted.txt", Type: ChangeDeleted},
		{Path: "another.txt", Type: ChangeCreated},
	}

	created := FilterChanges(changes, ChangeCreated)
	if len(created) != 2 {
		t.Errorf("FilterChanges(ChangeCreated) = %d items, want 2", len(created))
	}

	modified := FilterChanges(changes, ChangeModified)
	if len(modified) != 1 {
		t.Errorf("FilterChanges(ChangeModified) = %d items, want 1", len(modified))
	}

	createdOrModified := FilterChanges(changes, ChangeCreated, ChangeModified)
	if len(createdOrModified) != 3 {
		t.Errorf("FilterChanges(ChangeCreated, ChangeModified) = %d items, want 3", len(createdOrModified))
	}
}

func TestChangedPaths(t *testing.T) {
	changes := []Change{
		{Path: "a.txt", Type: ChangeCreated},
		{Path: "b.txt", Type: ChangeModified},
		{Path: "c.txt", Type: ChangeDeleted},
	}

	paths := ChangedPaths(changes)
	if len(paths) != 3 {
		t.Fatalf("ChangedPaths = %d items, want 3", len(paths))
	}

	expected := []string{"a.txt", "b.txt", "c.txt"}
	for i, want := range expected {
		if paths[i] != want {
			t.Errorf("paths[%d] = %q, want %q", i, paths[i], want)
		}
	}
}

func TestTakeSnapshotWithOptions(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "file.txt", "content")
	createTestFile(t, tmpDir, "excluded.log", "log")

	opts := SnapshotOptions{
		Excludes: []string{"*.log"},
	}

	snapshot, err := TakeSnapshotWithOptions(tmpDir, opts)
	if err != nil {
		t.Fatalf("TakeSnapshotWithOptions failed: %v", err)
	}

	if _, exists := snapshot.Files["file.txt"]; !exists {
		t.Error("file.txt should be in snapshot")
	}
	if _, exists := snapshot.Files["excluded.log"]; exists {
		t.Error("excluded.log should not be in snapshot")
	}
}

func TestSnapshotEmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	snapshot, err := TakeSnapshot(tmpDir, nil)
	if err != nil {
		t.Fatalf("TakeSnapshot failed: %v", err)
	}

	if len(snapshot.Files) != 0 {
		t.Errorf("empty directory should have 0 files, got %d", len(snapshot.Files))
	}
}

func TestSnapshotCompareNoChanges(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "file.txt", "content")

	before, err := TakeSnapshot(tmpDir, nil)
	if err != nil {
		t.Fatalf("TakeSnapshot failed: %v", err)
	}

	// Take another snapshot without changes
	after, err := TakeSnapshot(tmpDir, nil)
	if err != nil {
		t.Fatalf("TakeSnapshot failed: %v", err)
	}

	changes := before.Compare(after)
	if len(changes) != 0 {
		t.Errorf("expected no changes, got %d", len(changes))
	}
}

// Helper functions

func createTestFile(t *testing.T, tmpDir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(tmpDir, relPath)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create directory %s: %v", dir, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create file %s: %v", fullPath, err)
	}
}

func createTestDir(t *testing.T, tmpDir, relPath string) {
	t.Helper()
	fullPath := filepath.Join(tmpDir, relPath)
	if err := os.MkdirAll(fullPath, 0755); err != nil {
		t.Fatalf("failed to create directory %s: %v", fullPath, err)
	}
}
