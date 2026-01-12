package repository

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
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

// registryLock holds an exclusive lock on the registry file for atomic read-modify-write.
type registryLock struct {
	file *os.File
	data *registryData
}

// loadRegistryWithLock loads the registry and returns a lock handle.
// The caller must call Save or Close to release the exclusive lock.
// This is used for atomic read-modify-write operations.
// Retries with exponential backoff on lock contention.
func loadRegistryWithLock() (*registryLock, error) {
	path := getRegistryPath()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create registry directory: %w", err)
	}

	// Retry parameters for lock acquisition
	const maxRetries = 50
	const initialBackoff = 5 * time.Millisecond
	const maxBackoff = 100 * time.Millisecond

	var f *os.File
	var err error

	// Open file with retry logic
	backoff := initialBackoff
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2 // exponential backoff
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}

		// Open file with exclusive lock
		f, err = os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open registry: %w", err)
		}

		// Try to acquire exclusive lock with non-blocking mode first
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break // Lock acquired successfully
		}

		// If lock is held by another process, retry
		if err == syscall.EWOULDBLOCK {
			f.Close()
			if attempt == maxRetries-1 {
				return nil, fmt.Errorf("failed to acquire lock after %d attempts: lock held by another process", maxRetries)
			}
			continue
		}

		// Other errors are fatal
		f.Close()
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	// Read file contents
	info, err := f.Stat()
	if err != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		return nil, fmt.Errorf("failed to stat registry: %w", err)
	}

	var registry registryData

	// Empty file means no repositories yet
	if info.Size() == 0 {
		registry.Repositories = []Repository{}
	} else {
		data := make([]byte, info.Size())
		if _, err := f.Read(data); err != nil {
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
			return nil, fmt.Errorf("failed to read registry: %w", err)
		}

		if err := json.Unmarshal(data, &registry); err != nil {
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
			return nil, fmt.Errorf("failed to parse registry: %w", err)
		}
	}

	return &registryLock{file: f, data: &registry}, nil
}

// Save writes the registry data to disk and releases the lock.
func (rl *registryLock) Save() error {
	defer rl.Close()

	// Serialize with pretty formatting
	data, err := json.MarshalIndent(rl.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize registry: %w", err)
	}

	// Truncate and rewind
	if err := rl.file.Truncate(0); err != nil {
		return fmt.Errorf("failed to truncate registry: %w", err)
	}
	if _, err := rl.file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek registry: %w", err)
	}

	// Write data
	if _, err := rl.file.Write(data); err != nil {
		return fmt.Errorf("failed to write registry: %w", err)
	}

	// Sync to ensure data is written to disk
	if err := rl.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync registry: %w", err)
	}

	return nil
}

// Close releases the lock without saving (for read-only operations or error cases).
func (rl *registryLock) Close() error {
	if rl.file != nil {
		syscall.Flock(int(rl.file.Fd()), syscall.LOCK_UN)
		return rl.file.Close()
	}
	return nil
}
