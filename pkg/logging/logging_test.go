package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogPath(t *testing.T) {
	// Set up temp directory
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	logPath := LogPath()
	expected := filepath.Join(tmpDir, "canopy", "daemon.log")
	if logPath != expected {
		t.Errorf("LogPath() = %q, want %q", logPath, expected)
	}
}

func TestLogPathFallback(t *testing.T) {
	// Unset XDG_CACHE_HOME to test fallback
	t.Setenv("XDG_CACHE_HOME", "")

	logPath := LogPath()
	if !strings.Contains(logPath, ".cache/canopy/daemon.log") {
		t.Errorf("LogPath() = %q, expected to contain .cache/canopy/daemon.log", logPath)
	}
}

func TestEnsureLogDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	err := EnsureLogDir()
	if err != nil {
		t.Fatalf("EnsureLogDir() error = %v", err)
	}

	// Verify directory was created
	canopyDir := filepath.Join(tmpDir, "canopy")
	info, err := os.Stat(canopyDir)
	if err != nil {
		t.Fatalf("canopy directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("canopy is not a directory")
	}
}

func TestOpenLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	f, err := OpenLogFile()
	if err != nil {
		t.Fatalf("OpenLogFile() error = %v", err)
	}
	defer f.Close()

	// Write something to verify it works
	_, err = f.WriteString("test log entry\n")
	if err != nil {
		t.Fatalf("failed to write to log file: %v", err)
	}

	// Verify file exists
	logPath := filepath.Join(tmpDir, "canopy", "daemon.log")
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log file not created: %v", err)
	}

	// Verify content
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if string(content) != "test log entry\n" {
		t.Errorf("log file content = %q, want %q", string(content), "test log entry\n")
	}
}

func TestRotation(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	// Create initial log file with some content
	if err := os.WriteFile(logPath, []byte("original"), 0644); err != nil {
		t.Fatalf("failed to create test log: %v", err)
	}

	// Perform rotation
	if err := rotate(logPath); err != nil {
		t.Fatalf("rotate() error = %v", err)
	}

	// Verify original was renamed to .1
	backup1 := logPath + ".1"
	content, err := os.ReadFile(backup1)
	if err != nil {
		t.Fatalf("failed to read backup: %v", err)
	}
	if string(content) != "original" {
		t.Errorf("backup content = %q, want %q", string(content), "original")
	}

	// Verify original no longer exists
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Error("original log file should not exist after rotation")
	}
}

func TestRotationChain(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	// Create initial backups to test chain rotation
	for i := 1; i <= 3; i++ {
		backup := filepath.Join(tmpDir, "test.log."+string(rune('0'+i)))
		if err := os.WriteFile(backup, []byte("backup"+string(rune('0'+i))), 0644); err != nil {
			t.Fatalf("failed to create backup %d: %v", i, err)
		}
	}

	// Create current log
	if err := os.WriteFile(logPath, []byte("current"), 0644); err != nil {
		t.Fatalf("failed to create test log: %v", err)
	}

	// Perform rotation
	if err := rotate(logPath); err != nil {
		t.Fatalf("rotate() error = %v", err)
	}

	// Verify chain: current -> .1, .1 -> .2, .2 -> .3, .3 deleted
	backup1Content, _ := os.ReadFile(logPath + ".1")
	if string(backup1Content) != "current" {
		t.Errorf(".1 content = %q, want %q", string(backup1Content), "current")
	}

	backup2Content, _ := os.ReadFile(logPath + ".2")
	if string(backup2Content) != "backup1" {
		t.Errorf(".2 content = %q, want %q", string(backup2Content), "backup1")
	}

	backup3Content, _ := os.ReadFile(logPath + ".3")
	if string(backup3Content) != "backup2" {
		t.Errorf(".3 content = %q, want %q", string(backup3Content), "backup2")
	}

	// .4 should not exist (MaxBackups = 3)
	if _, err := os.Stat(logPath + ".4"); !os.IsNotExist(err) {
		t.Error(".4 should not exist (exceeded MaxBackups)")
	}
}

func TestRotateIfNeeded_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "nonexistent.log")

	// Should not error when file doesn't exist
	if err := rotateIfNeeded(logPath); err != nil {
		t.Fatalf("rotateIfNeeded() error = %v", err)
	}
}

func TestRotateIfNeeded_SmallFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "small.log")

	// Create a small file (less than MaxLogSize)
	if err := os.WriteFile(logPath, []byte("small content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Should not rotate
	if err := rotateIfNeeded(logPath); err != nil {
		t.Fatalf("rotateIfNeeded() error = %v", err)
	}

	// Original file should still exist
	if _, err := os.Stat(logPath); err != nil {
		t.Error("original file should still exist for small files")
	}

	// Backup should not exist
	if _, err := os.Stat(logPath + ".1"); !os.IsNotExist(err) {
		t.Error("backup should not exist for small files")
	}
}
