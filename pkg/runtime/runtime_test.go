package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeDir(t *testing.T) {
	dir := RuntimeDir()

	// Should always end with /canopy
	if !strings.HasSuffix(dir, "/canopy") {
		t.Errorf("RuntimeDir() = %q, want suffix /canopy", dir)
	}

	// Should be an absolute path
	if !filepath.IsAbs(dir) {
		t.Errorf("RuntimeDir() = %q, want absolute path", dir)
	}
}

func TestRuntimeDir_Linux_XDG(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}

	// Save and restore XDG_RUNTIME_DIR
	orig := os.Getenv("XDG_RUNTIME_DIR")
	defer os.Setenv("XDG_RUNTIME_DIR", orig)

	os.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	dir := RuntimeDir()

	want := "/run/user/1000/canopy"
	if dir != want {
		t.Errorf("RuntimeDir() with XDG_RUNTIME_DIR = %q, want %q", dir, want)
	}
}

func TestRuntimeDir_Fallback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux fallback test")
	}

	// Save and restore environment
	origXDG := os.Getenv("XDG_RUNTIME_DIR")
	origTMP := os.Getenv("TMPDIR")
	defer func() {
		os.Setenv("XDG_RUNTIME_DIR", origXDG)
		os.Setenv("TMPDIR", origTMP)
	}()

	os.Unsetenv("XDG_RUNTIME_DIR")
	os.Setenv("TMPDIR", "/custom/tmp")

	dir := RuntimeDir()

	// Should use TMPDIR fallback
	if !strings.HasPrefix(dir, "/custom/tmp/canopy-") {
		t.Errorf("RuntimeDir() fallback = %q, want prefix /custom/tmp/canopy-", dir)
	}
}

func TestSocketPath_Default(t *testing.T) {
	path := SocketPath("")
	want := filepath.Join(RuntimeDir(), "canopy.sock")

	if path != want {
		t.Errorf("SocketPath(\"\") = %q, want %q", path, want)
	}
}

func TestSocketPath_Named(t *testing.T) {
	path := SocketPath("agent.sock")
	want := filepath.Join(RuntimeDir(), "agent.sock")

	if path != want {
		t.Errorf("SocketPath(\"agent.sock\") = %q, want %q", path, want)
	}
}

func TestEnsureDir(t *testing.T) {
	// Use a temporary directory for testing
	tmpdir := t.TempDir()

	// Save and restore environment
	origXDG := os.Getenv("XDG_RUNTIME_DIR")
	defer os.Setenv("XDG_RUNTIME_DIR", origXDG)

	os.Setenv("XDG_RUNTIME_DIR", tmpdir)

	err := EnsureDir()
	if err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}

	dir := RuntimeDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("os.Stat(%q) error = %v", dir, err)
	}

	if !info.IsDir() {
		t.Errorf("%q is not a directory", dir)
	}

	// Check permissions (0700)
	perm := info.Mode().Perm()
	if perm != 0700 {
		t.Errorf("directory permissions = %o, want 0700", perm)
	}
}

func TestEnsureDir_Idempotent(t *testing.T) {
	tmpdir := t.TempDir()

	origXDG := os.Getenv("XDG_RUNTIME_DIR")
	defer os.Setenv("XDG_RUNTIME_DIR", origXDG)

	os.Setenv("XDG_RUNTIME_DIR", tmpdir)

	// Call twice - should succeed both times
	if err := EnsureDir(); err != nil {
		t.Fatalf("first EnsureDir() error = %v", err)
	}
	if err := EnsureDir(); err != nil {
		t.Fatalf("second EnsureDir() error = %v", err)
	}
}
