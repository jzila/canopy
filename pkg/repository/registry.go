package repository

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// registryData is the JSON schema for repositories.json
type registryData struct {
	Repositories []Repository `json:"repositories"`
}

// getRegistryPath returns the path to the registry file.
func getRegistryPath() string {
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheDir, "canopy", "repositories.json")
}

// loadRegistry loads the registry from disk with file locking.
func loadRegistry() (*registryData, error) {
	path := getRegistryPath()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create registry directory: %w", err)
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &registryData{Repositories: []Repository{}}, nil
	}

	// Open file with shared lock for reading
	f, err := os.OpenFile(path, os.O_RDONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open registry: %w", err)
	}
	defer f.Close()

	// Acquire shared lock
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
		return nil, fmt.Errorf("failed to acquire read lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	// Read and parse
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read registry: %w", err)
	}

	var registry registryData
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("failed to parse registry: %w", err)
	}

	return &registry, nil
}

// saveRegistry saves the registry to disk with file locking.
func saveRegistry(registry *registryData) error {
	path := getRegistryPath()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create registry directory: %w", err)
	}

	// Open file with exclusive lock for writing
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to open registry for writing: %w", err)
	}
	defer f.Close()

	// Acquire exclusive lock
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("failed to acquire write lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	// Serialize with pretty formatting
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize registry: %w", err)
	}

	// Truncate and write
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("failed to truncate registry: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek registry: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write registry: %w", err)
	}

	return nil
}

// loadRegistryWithLock loads the registry and returns it along with the file handle.
// The caller must call the returned unlock function when done to release the exclusive lock.
// This is used for atomic read-modify-write operations.
func loadRegistryWithLock() (*registryData, func(), error) {
	path := getRegistryPath()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create registry directory: %w", err)
	}

	// Open file with exclusive lock
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open registry: %w", err)
	}

	// Acquire exclusive lock
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	unlock := func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}

	// Read file contents
	info, err := f.Stat()
	if err != nil {
		unlock()
		return nil, nil, fmt.Errorf("failed to stat registry: %w", err)
	}

	// Empty file means no repositories yet
	if info.Size() == 0 {
		return &registryData{Repositories: []Repository{}}, unlock, nil
	}

	data := make([]byte, info.Size())
	if _, err := f.Read(data); err != nil {
		unlock()
		return nil, nil, fmt.Errorf("failed to read registry: %w", err)
	}

	var registry registryData
	if err := json.Unmarshal(data, &registry); err != nil {
		unlock()
		return nil, nil, fmt.Errorf("failed to parse registry: %w", err)
	}

	return &registry, unlock, nil
}

// saveRegistryLocked saves the registry while holding an exclusive lock.
// The file should have been opened with loadRegistryWithLock.
func saveRegistryLocked(path string, registry *registryData) error {
	// Open file for writing (lock should already be held by caller)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open registry for writing: %w", err)
	}
	defer f.Close()

	// Serialize with pretty formatting
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize registry: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write registry: %w", err)
	}

	return nil
}
