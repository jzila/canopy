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

// RepositoryStoreInterface abstracts persistence store operations for repository stats
type RepositoryStoreInterface interface {
	GetStatsByRepo(repoID string, since *time.Time) (*persistence.AggregateStats, error)
	GetRunsByRepo(repoID string) ([]persistence.Run, error)
}

// RepoHandler handles HTTP requests for repository management
type RepoHandler struct {
	daemon *Daemon
	store  RepositoryStoreInterface
}

// NewRepoHandler creates a new repository handler
func NewRepoHandler(daemon *Daemon, store RepositoryStoreInterface) *RepoHandler {
	return &RepoHandler{
		daemon: daemon,
		store:  store,
	}
}

// RepositoryResponse represents a repository in API responses
type RepositoryResponse struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	IsActive  bool      `json:"is_active"`
}

// RepositoryListResponse represents the response for listing repositories
type RepositoryListResponse struct {
	Repositories []RepositoryResponse `json:"repositories"`
	ActiveRepoID string               `json:"active_repo_id"`
}

// RepositoryDetailResponse represents detailed repository info with stats
type RepositoryDetailResponse struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Stats     *RepoStats `json:"stats,omitempty"`
}

// RepoStats contains statistics for a repository
type RepoStats struct {
	TotalRuns    int     `json:"total_runs"`
	TotalTasks   int     `json:"total_tasks"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// HandleListRepositories handles GET /api/repositories
func (h *RepoHandler) HandleListRepositories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// List all repositories from the registry
	repos, err := repository.List()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list repositories: %v", err), http.StatusInternalServerError)
		return
	}

	activeRepoID := h.daemon.GetActiveRepositoryID()

	// Convert to response format
	response := RepositoryListResponse{
		Repositories: make([]RepositoryResponse, len(repos)),
		ActiveRepoID: activeRepoID,
	}

	for i, repo := range repos {
		response.Repositories[i] = RepositoryResponse{
			ID:        repo.ID,
			Path:      repo.Path,
			Name:      repo.Name,
			CreatedAt: repo.CreatedAt,
			IsActive:  repo.ID == activeRepoID,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// HandleGetRepository handles GET /api/repositories/:id
func (h *RepoHandler) HandleGetRepository(w http.ResponseWriter, r *http.Request, repoID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Look up the repository
	repo, err := repository.FromID(repoID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to lookup repository: %v", err), http.StatusInternalServerError)
		return
	}
	if repo == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	response := RepositoryDetailResponse{
		ID:        repo.ID,
		Path:      repo.Path,
		Name:      repo.Name,
		CreatedAt: repo.CreatedAt,
	}

	// Get stats if persistence is enabled
	if h.store != nil {
		stats, err := h.store.GetStatsByRepo(repoID, nil)
		if err == nil && stats != nil {
			response.Stats = &RepoStats{
				TotalRuns:    stats.TotalRuns,
				TotalTasks:   stats.TotalAgents, // agents represent task executions
				TotalCostUSD: stats.TotalCostUSD,
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// HandleActivateRepository handles POST /api/repositories/:id/activate
func (h *RepoHandler) HandleActivateRepository(w http.ResponseWriter, r *http.Request, repoID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set the active repository
	if err := h.daemon.SetActiveRepository(repoID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to activate repository: %v", err), http.StatusInternalServerError)
		return
	}

	// Get the repository details
	repo, err := repository.FromID(repoID)
	if err != nil || repo == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	// Broadcast full state sync event to notify WebSocket clients
	// This includes the newly loaded tasks from the activated repository
	if h.daemon.eventBus != nil {
		snapshot := h.daemon.GetState()
		h.daemon.eventBus.Publish(Event{
			Type:      EventStateSync,
			Timestamp: time.Now(),
			Payload:   snapshot,
		})
	}

	// Return the activated repository
	response := RepositoryResponse{
		ID:        repo.ID,
		Path:      repo.Path,
		Name:      repo.Name,
		CreatedAt: repo.CreatedAt,
		IsActive:  true,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}
