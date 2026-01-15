package daemon

import (
	"fmt"
	"sync"

	"github.com/jzila/canopy/pkg/logging"
	"github.com/jzila/canopy/pkg/repository"
)

// RepositoryManager handles repository and beads client management.
// It provides lazy creation of beads clients per repository and tracks
// the currently active repository.
type RepositoryManager struct {
	defaultClient  BeadsClientInterface
	clientFactory  BeadsClientFactory
	clients        map[string]BeadsClientInterface
	activeRepoID   string
	mu             sync.RWMutex
}

// NewRepositoryManager creates a new RepositoryManager with an optional default beads client.
func NewRepositoryManager(defaultClient BeadsClientInterface) *RepositoryManager {
	return &RepositoryManager{
		defaultClient: defaultClient,
		clients:       make(map[string]BeadsClientInterface),
	}
}

// SetClientFactory sets the factory function for creating beads clients.
// This allows lazy creation of beads clients for different repositories.
func (m *RepositoryManager) SetClientFactory(factory BeadsClientFactory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clientFactory = factory
}

// SetActiveRepository sets the currently active repository by ID.
// Returns an error if the repository ID is not found in the registry.
func (m *RepositoryManager) SetActiveRepository(repoID string) error {
	// Verify the repository exists
	repo, err := repository.FromID(repoID)
	if err != nil {
		return fmt.Errorf("failed to lookup repository: %w", err)
	}
	if repo == nil {
		return fmt.Errorf("repository not found: %s", repoID)
	}

	m.mu.Lock()
	m.activeRepoID = repoID
	m.mu.Unlock()

	logging.Info("active repository set", "repo_name", repo.Name, "repo_id", repoID)
	return nil
}

// SetActiveRepositoryDirect sets the active repository ID without validation.
// This is used during state restoration when the repo may already be validated.
func (m *RepositoryManager) SetActiveRepositoryDirect(repoID string) {
	m.mu.Lock()
	m.activeRepoID = repoID
	m.mu.Unlock()
}

// GetActiveRepository returns the currently active repository.
// Returns nil if no repository is active.
func (m *RepositoryManager) GetActiveRepository() *repository.Repository {
	m.mu.RLock()
	repoID := m.activeRepoID
	m.mu.RUnlock()

	if repoID == "" {
		return nil
	}

	repo, err := repository.FromID(repoID)
	if err != nil {
		logging.Warn("failed to lookup active repository", "repo_id", repoID, "error", err)
		return nil
	}
	return repo
}

// GetActiveRepositoryID returns the ID of the currently active repository.
// Returns empty string if no repository is active.
func (m *RepositoryManager) GetActiveRepositoryID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeRepoID
}

// ListRepositories returns all registered repositories.
func (m *RepositoryManager) ListRepositories() ([]repository.Repository, error) {
	return repository.List()
}

// GetClient returns a beads client for the specified repository ID.
// If no client exists for that repo, it creates one lazily using the factory.
// Returns the default beads client if repoID is empty.
func (m *RepositoryManager) GetClient(repoID string) (BeadsClientInterface, error) {
	// Return default client if no repo specified
	if repoID == "" {
		return m.defaultClient, nil
	}

	// Check if we already have a client for this repo
	m.mu.RLock()
	client, exists := m.clients[repoID]
	m.mu.RUnlock()

	if exists {
		return client, nil
	}

	// Need to create a new client - lookup repo path
	repo, err := repository.FromID(repoID)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup repository: %w", err)
	}
	if repo == nil {
		return nil, fmt.Errorf("repository not found: %s", repoID)
	}

	// Create client using factory
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if client, exists := m.clients[repoID]; exists {
		return client, nil
	}

	// No factory available - return nil (no beads support for this repo)
	if m.clientFactory == nil {
		logging.Warn("no beads client factory configured", "repo_id", repoID)
		return nil, nil
	}

	client, err = m.clientFactory(repo.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to create beads client for repo %s: %w", repoID, err)
	}

	m.clients[repoID] = client
	logging.Info("created beads client for repository", "repo_name", repo.Name, "repo_id", repoID)
	return client, nil
}

// GetActiveClient returns the beads client for the currently active repository.
// Falls back to the default beads client if no repository is active.
func (m *RepositoryManager) GetActiveClient() (BeadsClientInterface, error) {
	m.mu.RLock()
	repoID := m.activeRepoID
	m.mu.RUnlock()

	if repoID == "" {
		return m.defaultClient, nil
	}

	return m.GetClient(repoID)
}

// GetDefaultClient returns the default beads client.
func (m *RepositoryManager) GetDefaultClient() BeadsClientInterface {
	return m.defaultClient
}
