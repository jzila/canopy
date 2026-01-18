package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jzila/canopy/pkg/persistence"
	"github.com/jzila/canopy/pkg/repository"
)

// BeadsHandler handles HTTP requests for beads operations
type BeadsHandler struct {
	daemon *Daemon
}

// NewBeadsHandler creates a new beads handler
func NewBeadsHandler(daemon *Daemon) *BeadsHandler {
	return &BeadsHandler{
		daemon: daemon,
	}
}

// SyncBeadsResponse represents the response from syncing beads
type SyncBeadsResponse struct {
	Synced   int    `json:"synced"`
	RepoID   string `json:"repo_id"`
	RepoName string `json:"repo_name"`
}

// HandleSyncBeads handles POST /api/beads/sync
// It reloads tasks from the beads database into RuntimeState
// Query params:
//   - repo: optional repo ID or path to sync. If not provided, uses active repo.
func (h *BeadsHandler) HandleSyncBeads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse optional repo query param
	repoParam := r.URL.Query().Get("repo")

	var repoID string
	var repoName string

	if repoParam != "" {
		// Try to resolve repo param - could be ID or path
		repo, err := h.resolveRepo(repoParam)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid repo: %v", err), http.StatusBadRequest)
			return
		}
		repoID = repo.ID
		repoName = repo.Name
	} else {
		// Use active repository
		repoID = h.daemon.GetActiveRepositoryID()
		if repoID == "" {
			http.Error(w, "No active repository", http.StatusServiceUnavailable)
			return
		}
		repo := h.daemon.GetActiveRepository()
		if repo != nil {
			repoName = repo.Name
		}
	}

	// Get beads client for the specified repo
	client, err := h.daemon.getBeadsClient(repoID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get beads client: %v", err), http.StatusInternalServerError)
		return
	}
	if client == nil {
		http.Error(w, "Beads client not available for repository", http.StatusServiceUnavailable)
		return
	}

	// List tasks from beads
	tasks, err := client.List(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to sync beads: %v", err), http.StatusInternalServerError)
		return
	}

	// Get persistence store for persisting tasks to SQLite
	var store *persistence.Store
	if h.daemon.persistManager != nil {
		store = h.daemon.persistManager.GetStore()
	}

	// Add tasks to runtime state
	for i := range tasks {
		h.daemon.state.AddTaskWithRepo(&tasks[i], repoID)

		// Persist to SQLite so the dashboard can display tasks
		if store != nil {
			pTask := &persistence.Task{
				ID:       tasks[i].ID,
				RepoID:   repoID,
				Title:    tasks[i].Title,
				Status:   tasks[i].Status,
				Priority: tasks[i].Priority,
			}
			if err := store.UpsertTask(pTask); err != nil {
				// Log but don't fail - runtime state was updated
				fmt.Printf("Warning: failed to persist task to database: %v\n", err)
			}
		}
	}

	// Broadcast state sync event to notify WebSocket clients
	if h.daemon.eventBus != nil {
		snapshot := h.daemon.GetState()
		h.daemon.eventBus.Publish(Event{
			Type:      EventStateSync,
			Timestamp: time.Now(),
			Payload:   snapshot,
		})
	}

	// Return response
	response := SyncBeadsResponse{
		Synced:   len(tasks),
		RepoID:   repoID,
		RepoName: repoName,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// resolveRepo attempts to resolve a repo identifier (ID or path) to a Repository
func (h *BeadsHandler) resolveRepo(identifier string) (*repository.Repository, error) {
	// First try as ID
	repo, err := repository.FromID(identifier)
	if err == nil && repo != nil {
		return repo, nil
	}

	// Try as path - look through all repositories
	repos, err := repository.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list repositories: %w", err)
	}

	// Normalize the path for comparison
	normalizedInput := strings.TrimSuffix(identifier, "/")
	for i := range repos {
		normalizedPath := strings.TrimSuffix(repos[i].Path, "/")
		if normalizedPath == normalizedInput {
			return &repos[i], nil
		}
	}

	return nil, fmt.Errorf("repository not found: %s", identifier)
}
