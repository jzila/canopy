package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestGetCacheDir(t *testing.T) {
	// Test with XDG_CACHE_HOME set
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	dir, err := getCacheDir()
	if err != nil {
		t.Fatalf("getCacheDir failed: %v", err)
	}

	expected := filepath.Join(tmpDir, "canopy")
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}

	// Test with XDG_CACHE_HOME unset (falls back to ~/.cache)
	t.Setenv("XDG_CACHE_HOME", "")
	dir, err = getCacheDir()
	if err != nil {
		t.Fatalf("getCacheDir failed without XDG_CACHE_HOME: %v", err)
	}

	home, _ := os.UserHomeDir()
	expected = filepath.Join(home, ".cache", "canopy")
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}
}

func TestPidFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	path, err := PidFilePath()
	if err != nil {
		t.Fatalf("PidFilePath failed: %v", err)
	}

	expected := filepath.Join(tmpDir, "canopy", "daemon.pid")
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestWriteAndReadPidFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Write pidfile
	if err := WritePidFile(); err != nil {
		t.Fatalf("WritePidFile failed: %v", err)
	}

	// Read it back
	pid, err := ReadPidFile()
	if err != nil {
		t.Fatalf("ReadPidFile failed: %v", err)
	}

	expectedPid := os.Getpid()
	if pid != expectedPid {
		t.Errorf("expected pid %d, got %d", expectedPid, pid)
	}

	// Verify file contents
	pidPath, _ := PidFilePath()
	content, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("failed to read pidfile directly: %v", err)
	}

	expectedContent := strconv.Itoa(expectedPid) + "\n"
	if string(content) != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, string(content))
	}
}

func TestRemovePidFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Write pidfile
	if err := WritePidFile(); err != nil {
		t.Fatalf("WritePidFile failed: %v", err)
	}

	// Verify it exists
	pidPath, _ := PidFilePath()
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Fatal("pidfile should exist after WritePidFile")
	}

	// Remove it
	if err := RemovePidFile(); err != nil {
		t.Fatalf("RemovePidFile failed: %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("pidfile should not exist after RemovePidFile")
	}

	// Remove again should not error
	if err := RemovePidFile(); err != nil {
		t.Errorf("RemovePidFile should not error when file doesn't exist: %v", err)
	}
}

func TestReadPidFileNotExists(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	pid, err := ReadPidFile()
	if err != ErrDaemonNotRunning {
		t.Errorf("expected ErrDaemonNotRunning, got %v", err)
	}
	if pid != 0 {
		t.Errorf("expected pid 0, got %d", pid)
	}
}

func TestReadPidFileInvalidContent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Create directory
	cacheDir := filepath.Join(tmpDir, "canopy")
	_ = os.MkdirAll(cacheDir, 0755)

	// Write invalid content
	pidPath := filepath.Join(cacheDir, "daemon.pid")
	_ = os.WriteFile(pidPath, []byte("not-a-number\n"), 0644)

	_, err := ReadPidFile()
	if err == nil {
		t.Error("expected error for invalid pid content")
	}
}

func TestProcessExists(t *testing.T) {
	// Current process should exist
	if !processExists(os.Getpid()) {
		t.Error("current process should exist")
	}

	// PID 0 should not be sendable to (it represents the kernel)
	if processExists(0) {
		t.Error("pid 0 should report as non-existent")
	}

	// Very high PID should not exist
	if processExists(999999999) {
		t.Error("very high pid should not exist")
	}
}

func TestIsRunning(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// No pidfile - not running
	running, pid, err := IsRunning()
	if err != nil {
		t.Fatalf("IsRunning failed: %v", err)
	}
	if running {
		t.Error("should not be running without pidfile")
	}
	if pid != 0 {
		t.Errorf("expected pid 0, got %d", pid)
	}

	// Write pidfile with current process - should be running
	if err := WritePidFile(); err != nil {
		t.Fatalf("WritePidFile failed: %v", err)
	}

	running, pid, err = IsRunning()
	if err != nil {
		t.Fatalf("IsRunning failed: %v", err)
	}
	if !running {
		t.Error("should be running with valid pidfile")
	}
	if pid != os.Getpid() {
		t.Errorf("expected pid %d, got %d", os.Getpid(), pid)
	}
}

func TestIsRunningStaleFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Create directory
	cacheDir := filepath.Join(tmpDir, "canopy")
	_ = os.MkdirAll(cacheDir, 0755)

	// Write stale pidfile (process doesn't exist)
	pidPath := filepath.Join(cacheDir, "daemon.pid")
	_ = os.WriteFile(pidPath, []byte("999999999\n"), 0644)

	// Should report not running and clean up
	running, pid, err := IsRunning()
	if err != nil {
		t.Fatalf("IsRunning failed: %v", err)
	}
	if running {
		t.Error("should not be running with stale pidfile")
	}
	if pid != 0 {
		t.Errorf("expected pid 0, got %d", pid)
	}

	// Pidfile should be cleaned up
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("stale pidfile should be cleaned up")
	}
}

func TestCleanStalePidFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// No pidfile - nothing to clean
	cleaned, err := CleanStalePidFile()
	if err != nil {
		t.Fatalf("CleanStalePidFile failed: %v", err)
	}
	if cleaned {
		t.Error("should not report cleaned when no pidfile exists")
	}

	// Create stale pidfile
	cacheDir := filepath.Join(tmpDir, "canopy")
	_ = os.MkdirAll(cacheDir, 0755)
	pidPath := filepath.Join(cacheDir, "daemon.pid")
	_ = os.WriteFile(pidPath, []byte("999999999\n"), 0644)

	// Clean it
	cleaned, err = CleanStalePidFile()
	if err != nil {
		t.Fatalf("CleanStalePidFile failed: %v", err)
	}
	if !cleaned {
		t.Error("should report cleaned for stale pidfile")
	}

	// File should be gone
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("stale pidfile should be removed")
	}

	// Current process pidfile should not be cleaned
	if err := WritePidFile(); err != nil {
		t.Fatalf("WritePidFile failed: %v", err)
	}

	cleaned, err = CleanStalePidFile()
	if err != nil {
		t.Fatalf("CleanStalePidFile failed: %v", err)
	}
	if cleaned {
		t.Error("should not clean current process pidfile")
	}

	// File should still exist
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Error("current process pidfile should not be removed")
	}
}

func TestWritePidFileCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Directory doesn't exist yet
	cacheDir := filepath.Join(tmpDir, "canopy")
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatal("cache dir should not exist before WritePidFile")
	}

	// Write should create it
	if err := WritePidFile(); err != nil {
		t.Fatalf("WritePidFile failed: %v", err)
	}

	// Now it should exist
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		t.Error("cache dir should exist after WritePidFile")
	}
}

func TestStopDaemonNotRunning(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// No daemon running - should return ErrDaemonNotRunning
	err := StopDaemon(1 * time.Second)
	if err != ErrDaemonNotRunning {
		t.Errorf("expected ErrDaemonNotRunning, got %v", err)
	}
}

func TestStopDaemonStaleProcess(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Create pidfile with non-existent process
	cacheDir := filepath.Join(tmpDir, "canopy")
	_ = os.MkdirAll(cacheDir, 0755)
	pidPath := filepath.Join(cacheDir, "daemon.pid")
	_ = os.WriteFile(pidPath, []byte("999999999\n"), 0644)

	// StopDaemon should handle the stale pidfile gracefully
	// IsRunning will clean up the stale file, so StopDaemon returns ErrDaemonNotRunning
	err := StopDaemon(1 * time.Second)
	if err != ErrDaemonNotRunning {
		t.Errorf("expected ErrDaemonNotRunning for stale pidfile, got %v", err)
	}

	// Pidfile should be cleaned up by IsRunning
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("stale pidfile should be cleaned up")
	}
}
