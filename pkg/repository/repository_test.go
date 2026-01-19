package repository

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestGetOrCreate(t *testing.T) {
	// Set up temp directory for registry
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Create a test repository directory
	repoDir := filepath.Join(tmpDir, "test-repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("failed to create test repo dir: %v", err)
	}

	// First call should create a new repository
	repo1, err := GetOrCreate(repoDir)
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	if repo1 == nil {
		t.Fatal("GetOrCreate returned nil repository")
	}
	if repo1.ID == "" {
		t.Error("repository ID is empty")
	}
	if repo1.Name != "test-repo" {
		t.Errorf("expected name 'test-repo', got %q", repo1.Name)
	}
	if repo1.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}

	// Second call should return the same repository
	repo2, err := GetOrCreate(repoDir)
	if err != nil {
		t.Fatalf("second GetOrCreate failed: %v", err)
	}
	if repo2.ID != repo1.ID {
		t.Errorf("expected same ID %q, got %q", repo1.ID, repo2.ID)
	}
}

func TestGetOrCreateDifferentPaths(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Create two test repository directories
	repoDir1 := filepath.Join(tmpDir, "repo1")
	repoDir2 := filepath.Join(tmpDir, "repo2")
	_ = os.MkdirAll(repoDir1, 0755)
	_ = os.MkdirAll(repoDir2, 0755)

	repo1, err := GetOrCreate(repoDir1)
	if err != nil {
		t.Fatalf("GetOrCreate for repo1 failed: %v", err)
	}

	repo2, err := GetOrCreate(repoDir2)
	if err != nil {
		t.Fatalf("GetOrCreate for repo2 failed: %v", err)
	}

	if repo1.ID == repo2.ID {
		t.Error("different repositories should have different IDs")
	}
}

func TestFromPath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	repoDir := filepath.Join(tmpDir, "test-repo")
	_ = os.MkdirAll(repoDir, 0755)

	// FromPath on non-existent repo should return nil
	repo, err := FromPath(repoDir)
	if err != nil {
		t.Fatalf("FromPath failed: %v", err)
	}
	if repo != nil {
		t.Error("expected nil for non-registered repo")
	}

	// Create the repo
	created, err := GetOrCreate(repoDir)
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	// Now FromPath should find it
	found, err := FromPath(repoDir)
	if err != nil {
		t.Fatalf("FromPath after create failed: %v", err)
	}
	if found == nil {
		t.Fatal("FromPath returned nil after create")
	}
	if found.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, found.ID)
	}
}

func TestFromID(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	repoDir := filepath.Join(tmpDir, "test-repo")
	_ = os.MkdirAll(repoDir, 0755)

	// FromID on non-existent ID should return nil
	repo, err := FromID("nonexistent-id")
	if err != nil {
		t.Fatalf("FromID failed: %v", err)
	}
	if repo != nil {
		t.Error("expected nil for non-existent ID")
	}

	// Create the repo
	created, err := GetOrCreate(repoDir)
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	// Now FromID should find it
	found, err := FromID(created.ID)
	if err != nil {
		t.Fatalf("FromID after create failed: %v", err)
	}
	if found == nil {
		t.Fatal("FromID returned nil after create")
	}
	if found.Path != created.Path {
		t.Errorf("expected path %q, got %q", created.Path, found.Path)
	}
}

func TestList(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// List should return empty slice initially
	repos, err := List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("expected empty list, got %d repos", len(repos))
	}

	// Create some repos
	for i := 0; i < 3; i++ {
		repoDir := filepath.Join(tmpDir, "repo"+string(rune('a'+i)))
		_ = os.MkdirAll(repoDir, 0755)
		if _, err := GetOrCreate(repoDir); err != nil {
			t.Fatalf("GetOrCreate failed: %v", err)
		}
	}

	// List should now return 3 repos
	repos, err = List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(repos) != 3 {
		t.Errorf("expected 3 repos, got %d", len(repos))
	}
}

func TestPathNormalization(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Create actual directory
	repoDir := filepath.Join(tmpDir, "actual-repo")
	_ = os.MkdirAll(repoDir, 0755)

	// Create symlink to it
	symlinkDir := filepath.Join(tmpDir, "symlink-repo")
	if err := os.Symlink(repoDir, symlinkDir); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	// GetOrCreate via actual path
	repo1, err := GetOrCreate(repoDir)
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	// GetOrCreate via symlink should return the same repo
	repo2, err := GetOrCreate(symlinkDir)
	if err != nil {
		t.Fatalf("GetOrCreate via symlink failed: %v", err)
	}

	if repo1.ID != repo2.ID {
		t.Errorf("symlink should resolve to same repo: got IDs %q and %q", repo1.ID, repo2.ID)
	}
}

func TestRelativePathNormalization(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Create actual directory
	repoDir := filepath.Join(tmpDir, "test-repo")
	_ = os.MkdirAll(repoDir, 0755)

	// Change to temp dir and use relative path
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	// GetOrCreate with relative path
	repo1, err := GetOrCreate("test-repo")
	if err != nil {
		t.Fatalf("GetOrCreate with relative path failed: %v", err)
	}

	// GetOrCreate with absolute path should return the same repo
	repo2, err := GetOrCreate(repoDir)
	if err != nil {
		t.Fatalf("GetOrCreate with absolute path failed: %v", err)
	}

	if repo1.ID != repo2.ID {
		t.Errorf("relative and absolute paths should resolve to same repo: got IDs %q and %q", repo1.ID, repo2.ID)
	}
}

func TestConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Create test repo directory
	repoDir := filepath.Join(tmpDir, "concurrent-repo")
	_ = os.MkdirAll(repoDir, 0755)

	// Run multiple goroutines trying to GetOrCreate the same repo
	const numGoroutines = 10
	var wg sync.WaitGroup
	results := make(chan string, numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			repo, err := GetOrCreate(repoDir)
			if err != nil {
				errors <- err
				return
			}
			results <- repo.ID
		}()
	}

	wg.Wait()
	close(results)
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("concurrent GetOrCreate failed: %v", err)
	}

	// All goroutines should have gotten the same ID
	var firstID string
	for id := range results {
		if firstID == "" {
			firstID = id
		} else if id != firstID {
			t.Errorf("got different IDs in concurrent access: %q vs %q", firstID, id)
		}
	}
}

func TestConcurrentDifferentPaths(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Create multiple test repo directories
	const numRepos = 5
	repoDirs := make([]string, numRepos)
	for i := 0; i < numRepos; i++ {
		repoDirs[i] = filepath.Join(tmpDir, "repo"+string(rune('a'+i)))
		_ = os.MkdirAll(repoDirs[i], 0755)
	}

	// Run goroutines for each repo concurrently
	const iterations = 5
	var wg sync.WaitGroup
	results := make(map[string][]string)
	var mu sync.Mutex
	errors := make(chan error, numRepos*iterations)

	for _, dir := range repoDirs {
		for j := 0; j < iterations; j++ {
			wg.Add(1)
			go func(d string) {
				defer wg.Done()
				repo, err := GetOrCreate(d)
				if err != nil {
					errors <- err
					return
				}
				mu.Lock()
				results[d] = append(results[d], repo.ID)
				mu.Unlock()
			}(dir)
		}
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("concurrent GetOrCreate failed: %v", err)
	}

	// Each path should have consistent IDs
	for path, ids := range results {
		if len(ids) == 0 {
			t.Errorf("no results for path %s", path)
			continue
		}
		firstID := ids[0]
		for _, id := range ids {
			if id != firstID {
				t.Errorf("inconsistent IDs for path %s: %q vs %q", path, firstID, id)
			}
		}
	}

	// Different paths should have different IDs
	allIDs := make(map[string]string)
	for path, ids := range results {
		if len(ids) > 0 {
			if existingPath, exists := allIDs[ids[0]]; exists && existingPath != path {
				t.Errorf("same ID %s for different paths: %s and %s", ids[0], existingPath, path)
			}
			allIDs[ids[0]] = path
		}
	}
}
