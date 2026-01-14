// Package daemon provides daemon control utilities including pidfile management.
package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// pidFileName is the name of the pidfile within the cache directory.
	pidFileName = "daemon.pid"
)

// ErrDaemonNotRunning indicates the daemon is not currently running.
var ErrDaemonNotRunning = errors.New("daemon is not running")

// getCacheDir returns the canopy cache directory path.
// Per the persistence invariant, ALL canopy persistence MUST live at
// $XDG_CACHE_HOME/canopy/ or ~/.cache/canopy/
func getCacheDir() (string, error) {
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		cacheDir = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheDir, "canopy"), nil
}

// PidFilePath returns the full path to the daemon pidfile.
func PidFilePath() (string, error) {
	cacheDir, err := getCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, pidFileName), nil
}

// WritePidFile writes the current process's PID to the pidfile.
// Creates the cache directory if it doesn't exist.
func WritePidFile() error {
	pidPath, err := PidFilePath()
	if err != nil {
		return fmt.Errorf("failed to get pidfile path: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(pidPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	pid := os.Getpid()
	content := fmt.Sprintf("%d\n", pid)
	if err := os.WriteFile(pidPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write pidfile: %w", err)
	}

	return nil
}

// RemovePidFile removes the pidfile.
// Returns nil if the file doesn't exist.
func RemovePidFile() error {
	pidPath, err := PidFilePath()
	if err != nil {
		return fmt.Errorf("failed to get pidfile path: %w", err)
	}

	if err := os.Remove(pidPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove pidfile: %w", err)
	}

	return nil
}

// ReadPidFile reads the PID from the pidfile.
// Returns 0 and ErrDaemonNotRunning if the file doesn't exist.
func ReadPidFile() (int, error) {
	pidPath, err := PidFilePath()
	if err != nil {
		return 0, fmt.Errorf("failed to get pidfile path: %w", err)
	}

	content, err := os.ReadFile(pidPath)
	if os.IsNotExist(err) {
		return 0, ErrDaemonNotRunning
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read pidfile: %w", err)
	}

	pidStr := strings.TrimSpace(string(content))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid pid in pidfile: %w", err)
	}

	return pid, nil
}

// processExists checks if a process with the given PID exists.
func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds, so we need to send signal 0
	// to check if the process actually exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// IsRunning checks if the daemon is running.
// Returns (running, pid, error).
// If the daemon is not running, returns (false, 0, nil).
// If there's a stale pidfile (process no longer exists), it's cleaned up
// and (false, 0, nil) is returned.
func IsRunning() (bool, int, error) {
	pid, err := ReadPidFile()
	if errors.Is(err, ErrDaemonNotRunning) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}

	// Check if process actually exists
	if !processExists(pid) {
		// Stale pidfile - clean it up
		if err := RemovePidFile(); err != nil {
			// Log but don't fail - the important thing is reporting not running
			return false, 0, nil
		}
		return false, 0, nil
	}

	return true, pid, nil
}

// CleanStalePidFile removes the pidfile if the process is no longer running.
// This is useful on startup to clean up from crashes.
// Returns true if a stale pidfile was cleaned up.
func CleanStalePidFile() (bool, error) {
	pid, err := ReadPidFile()
	if errors.Is(err, ErrDaemonNotRunning) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if !processExists(pid) {
		if err := RemovePidFile(); err != nil {
			return false, err
		}
		return true, nil
	}

	return false, nil
}

// StopDaemon sends SIGTERM to the running daemon and waits for it to exit.
// Returns ErrDaemonNotRunning if no daemon is running.
// The timeout specifies how long to wait for graceful shutdown before returning.
func StopDaemon(timeout time.Duration) error {
	running, pid, err := IsRunning()
	if err != nil {
		return fmt.Errorf("failed to check daemon status: %w", err)
	}
	if !running {
		return ErrDaemonNotRunning
	}

	// Find the process
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	// Send SIGTERM for graceful shutdown
	if err := process.Signal(syscall.SIGTERM); err != nil {
		// Process might have already exited
		if !processExists(pid) {
			// Clean up stale pidfile
			_ = RemovePidFile()
			return nil
		}
		return fmt.Errorf("failed to send SIGTERM to process %d: %w", pid, err)
	}

	// Wait for process to exit with timeout
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			// Process has exited, clean up pidfile if still present
			_ = RemovePidFile()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Process didn't exit within timeout
	return fmt.Errorf("daemon (pid %d) did not exit within %v", pid, timeout)
}
