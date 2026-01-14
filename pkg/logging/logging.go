// Package logging provides file-based logging for the canopy daemon.
// Logs are written to $XDG_CACHE_HOME/canopy/daemon.log with simple
// size-based rotation (max 10MB, keeping 3 backup files).
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

const (
	// LogFileName is the daemon log file name
	LogFileName = "daemon.log"

	// MaxLogSize is the maximum log file size before rotation (10MB)
	MaxLogSize = 10 * 1024 * 1024

	// MaxBackups is the number of backup log files to keep
	MaxBackups = 3
)

// LogPath returns the full path to the daemon log file.
func LogPath() string {
	return filepath.Join(getCacheDir(), LogFileName)
}

// getCacheDir returns the cache directory for canopy, respecting XDG_CACHE_HOME
func getCacheDir() string {
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheDir, "canopy")
}

// EnsureLogDir creates the log directory if it doesn't exist.
func EnsureLogDir() error {
	return os.MkdirAll(getCacheDir(), 0755)
}

// OpenLogFile opens the daemon log file for appending.
// Creates the file if it doesn't exist.
// The caller is responsible for closing the returned file.
func OpenLogFile() (*os.File, error) {
	if err := EnsureLogDir(); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	logPath := LogPath()

	// Check if rotation is needed before opening
	if err := rotateIfNeeded(logPath); err != nil {
		// Log rotation failure is non-fatal; we can still write to the existing file
		fmt.Fprintf(os.Stderr, "warning: log rotation failed: %v\n", err)
	}

	return os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
}

// SetupFileLogging configures the standard logger to write to both
// stderr and the daemon log file. Returns a cleanup function that
// should be called to close the log file.
func SetupFileLogging() (cleanup func(), err error) {
	logFile, err := OpenLogFile()
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Write to both stderr and the log file
	log.SetOutput(io.MultiWriter(os.Stderr, logFile))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cleanup = func() {
		logFile.Close()
	}

	return cleanup, nil
}

// rotateIfNeeded checks if the log file exceeds MaxLogSize and rotates if needed.
func rotateIfNeeded(logPath string) error {
	info, err := os.Stat(logPath)
	if os.IsNotExist(err) {
		return nil // No file to rotate
	}
	if err != nil {
		return err
	}

	if info.Size() < MaxLogSize {
		return nil // No rotation needed
	}

	return rotate(logPath)
}

// rotate performs log rotation:
// daemon.log.3 -> deleted
// daemon.log.2 -> daemon.log.3
// daemon.log.1 -> daemon.log.2
// daemon.log   -> daemon.log.1
func rotate(logPath string) error {
	// Remove oldest backup if it exists
	oldestBackup := fmt.Sprintf("%s.%d", logPath, MaxBackups)
	os.Remove(oldestBackup)

	// Shift existing backups
	for i := MaxBackups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", logPath, i)
		newPath := fmt.Sprintf("%s.%d", logPath, i+1)
		if _, err := os.Stat(oldPath); err == nil {
			if err := os.Rename(oldPath, newPath); err != nil {
				return fmt.Errorf("failed to rename %s to %s: %w", oldPath, newPath, err)
			}
		}
	}

	// Rename current log to .1
	backup1 := fmt.Sprintf("%s.1", logPath)
	if err := os.Rename(logPath, backup1); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %w", logPath, backup1, err)
	}

	return nil
}
