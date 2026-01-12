// Package repository provides repository identity management for canopy.
// Repositories are identified by UUID and registered in a central registry
// stored at $XDG_CACHE_HOME/canopy/repositories.json.
package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// Repository represents a tracked repository with a stable identity.
type Repository struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// GetOrCreate retrieves an existing repository by path or creates a new one.
// The path is normalized (symlinks resolved, converted to absolute) before lookup.
// This function is thread-safe and uses file locking for atomic read-modify-write.
func GetOrCreate(path string) (*Repository, error) {
	normalizedPath, err := normalizePath(path)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize path: %w", err)
	}

	// Use atomic read-modify-write with exclusive lock
	registry, unlock, err := loadRegistryWithLock()
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}
	defer unlock()

	// Check if repository already exists
	for i := range registry.Repositories {
		if registry.Repositories[i].Path == normalizedPath {
			return &registry.Repositories[i], nil
		}
	}

	// Create new repository
	repo := Repository{
		ID:        uuid.New().String(),
		Path:      normalizedPath,
		Name:      filepath.Base(normalizedPath),
		CreatedAt: time.Now().UTC(),
	}

	registry.Repositories = append(registry.Repositories, repo)
	if err := saveRegistryLocked(getRegistryPath(), registry); err != nil {
		return nil, fmt.Errorf("failed to save registry: %w", err)
	}

	return &repo, nil
}

// FromPath loads an existing repository by path.
// Returns nil if the repository is not registered.
func FromPath(path string) (*Repository, error) {
	normalizedPath, err := normalizePath(path)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize path: %w", err)
	}

	registry, err := loadRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}

	for i := range registry.Repositories {
		if registry.Repositories[i].Path == normalizedPath {
			return &registry.Repositories[i], nil
		}
	}

	return nil, nil
}

// FromID loads an existing repository by ID.
// Returns nil if the repository is not registered.
func FromID(id string) (*Repository, error) {
	registry, err := loadRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}

	for i := range registry.Repositories {
		if registry.Repositories[i].ID == id {
			return &registry.Repositories[i], nil
		}
	}

	return nil, nil
}

// List returns all registered repositories.
func List() ([]Repository, error) {
	registry, err := loadRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}

	// Return a copy to prevent mutation
	result := make([]Repository, len(registry.Repositories))
	copy(result, registry.Repositories)
	return result, nil
}

// normalizePath converts a path to an absolute path with symlinks resolved.
func normalizePath(path string) (string, error) {
	// First make it absolute
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Resolve symlinks - if EvalSymlinks fails (e.g., path doesn't exist),
	// fall back to the absolute path
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// Path might not exist yet, use the absolute path
		if os.IsNotExist(err) {
			return absPath, nil
		}
		return "", fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	return resolved, nil
}
