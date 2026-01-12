package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jzila/canopy/pkg/persistence"
)

// PersistenceStoreInterface abstracts persistence store operations for handlers
type PersistenceStoreInterface interface {
	GetRun(id string) (*persistence.Run, error)
	ListRuns(filter persistence.RunFilter) (*persistence.RunListResult, error)
	GetAgentsByRun(runID string) ([]persistence.Agent, error)
	GetStats(since *time.Time) (*persistence.AggregateStats, error)
	GetStatsByRepo(repoID string, since *time.Time) (*persistence.AggregateStats, error)
	SetAgentArchived(agentID string, archived bool) error
}

// RunsHandler handles HTTP requests for run history queries
type RunsHandler struct {
	store PersistenceStoreInterface
}

// NewRunsHandler creates a new runs handler with the given persistence store
func NewRunsHandler(store PersistenceStoreInterface) *RunsHandler {
	return &RunsHandler{
		store: store,
	}
}

// HandleListRuns handles GET /api/runs with filtering and pagination
func (h *RunsHandler) HandleListRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.store == nil {
		http.Error(w, "Persistence not enabled", http.StatusNotImplemented)
		return
	}

	// Parse query parameters
	filter, err := parseRunFilter(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid query parameter: %v", err), http.StatusBadRequest)
		return
	}

	// Query runs
	result, err := h.store.ListRuns(filter)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to query runs: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// HandleGetRun handles GET /api/runs/:id
func (h *RunsHandler) HandleGetRun(w http.ResponseWriter, r *http.Request, runID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.store == nil {
		http.Error(w, "Persistence not enabled", http.StatusNotImplemented)
		return
	}

	// Get run
	run, err := h.store.GetRun(runID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get run: %v", err), http.StatusInternalServerError)
		return
	}
	if run == nil {
		http.Error(w, "Run not found", http.StatusNotFound)
		return
	}

	// Also get agents for this run
	agents, err := h.store.GetAgentsByRun(runID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get agents: %v", err), http.StatusInternalServerError)
		return
	}

	// Return run with agents
	response := struct {
		*persistence.Run
		Agents []persistence.Agent `json:"agents"`
	}{
		Run:    run,
		Agents: agents,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// HandleGetRunAgents handles GET /api/runs/:id/agents
func (h *RunsHandler) HandleGetRunAgents(w http.ResponseWriter, r *http.Request, runID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.store == nil {
		http.Error(w, "Persistence not enabled", http.StatusNotImplemented)
		return
	}

	// Check if run exists
	run, err := h.store.GetRun(runID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get run: %v", err), http.StatusInternalServerError)
		return
	}
	if run == nil {
		http.Error(w, "Run not found", http.StatusNotFound)
		return
	}

	// Get agents
	agents, err := h.store.GetAgentsByRun(runID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get agents: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(agents); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// HandleGetHistoricalStats handles GET /api/stats/history for aggregate statistics
func (h *RunsHandler) HandleGetHistoricalStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.store == nil {
		http.Error(w, "Persistence not enabled", http.StatusNotImplemented)
		return
	}

	// Parse since parameter
	var since *time.Time
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		t, err := parseTimeOrDuration(sinceStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid 'since' parameter: %v", err), http.StatusBadRequest)
			return
		}
		since = &t
	}

	// Parse repo_id parameter for filtering by repository
	repoID := r.URL.Query().Get("repo_id")

	// Get stats - filtered by repo if specified
	var stats *persistence.AggregateStats
	var err error
	if repoID != "" {
		stats, err = h.store.GetStatsByRepo(repoID, since)
	} else {
		stats, err = h.store.GetStats(since)
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get stats: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// parseRunFilter parses query parameters into a RunFilter
func parseRunFilter(r *http.Request) (persistence.RunFilter, error) {
	filter := persistence.RunFilter{}

	// Parse 'since' - can be ISO timestamp or duration (e.g., "7d", "24h")
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		t, err := parseTimeOrDuration(sinceStr)
		if err != nil {
			return filter, fmt.Errorf("invalid 'since': %w", err)
		}
		filter.Since = &t
	}

	// Parse 'before' - ISO timestamp or duration
	if beforeStr := r.URL.Query().Get("before"); beforeStr != "" {
		t, err := parseTimeOrDuration(beforeStr)
		if err != nil {
			return filter, fmt.Errorf("invalid 'before': %w", err)
		}
		filter.Before = &t
	}

	// Parse 'status'
	if status := r.URL.Query().Get("status"); status != "" {
		switch status {
		case "running", "completed", "failed", "cancelled":
			filter.Status = persistence.RunStatus(status)
		default:
			return filter, fmt.Errorf("invalid 'status': must be running, completed, failed, or cancelled")
		}
	}

	// Parse 'limit'
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			return filter, fmt.Errorf("invalid 'limit': must be a positive integer")
		}
		filter.Limit = limit
	}

	// Parse 'offset'
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			return filter, fmt.Errorf("invalid 'offset': must be a non-negative integer")
		}
		filter.Offset = offset
	}

	// Parse 'repo_id' - filter by repository
	if repoID := r.URL.Query().Get("repo_id"); repoID != "" {
		filter.RepoID = repoID
	}

	return filter, nil
}

// parseTimeOrDuration parses a string as either an ISO timestamp or a duration like "7d", "24h"
func parseTimeOrDuration(s string) (time.Time, error) {
	// Try ISO format first
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Try duration format (e.g., "7d", "24h", "30m")
	durationRegex := regexp.MustCompile(`^(\d+)([dhms])$`)
	matches := durationRegex.FindStringSubmatch(strings.ToLower(s))
	if matches != nil {
		value, _ := strconv.Atoi(matches[1])
		unit := matches[2]

		var duration time.Duration
		switch unit {
		case "d":
			duration = time.Duration(value) * 24 * time.Hour
		case "h":
			duration = time.Duration(value) * time.Hour
		case "m":
			duration = time.Duration(value) * time.Minute
		case "s":
			duration = time.Duration(value) * time.Second
		}

		return time.Now().Add(-duration), nil
	}

	return time.Time{}, fmt.Errorf("must be ISO timestamp (RFC3339) or duration (e.g., '7d', '24h')")
}
