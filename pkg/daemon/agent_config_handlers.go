package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jzila/canopy/pkg/config"
	"github.com/jzila/canopy/pkg/orchestrator"
)

// AgentConfigHandler handles HTTP requests for agent settings management
type AgentConfigHandler struct {
	daemon   *Daemon
	eventBus *EventBus
}

// NewAgentConfigHandler creates a new agent config handler
func NewAgentConfigHandler(daemon *Daemon) *AgentConfigHandler {
	var eventBus *EventBus
	if daemon != nil {
		eventBus = daemon.GetEventBus()
	}
	return &AgentConfigHandler{
		daemon:   daemon,
		eventBus: eventBus,
	}
}

// AgentConfigResponse is the response for GET /api/config/agents
type AgentConfigResponse struct {
	DefaultModel string                      `json:"default_model"`
	Worker       config.AgentTypeSettings    `json:"worker"`
	Resolver     config.AgentTypeSettings    `json:"resolver"`
	Repair       config.AgentTypeSettings    `json:"repair"`
	Persisted    bool                        `json:"persisted"` // true if runtime matches config on disk
	Error        string                      `json:"error,omitempty"`
}

// AgentConfigUpdateRequest is the request body for POST /api/config/agents
// All fields are optional; only provided fields are updated
type AgentConfigUpdateRequest struct {
	DefaultModel *string                  `json:"default_model,omitempty"`
	Worker       *AgentTypeSettingsUpdate `json:"worker,omitempty"`
	Resolver     *AgentTypeSettingsUpdate `json:"resolver,omitempty"`
	Repair       *AgentTypeSettingsUpdate `json:"repair,omitempty"`
}

// AgentTypeSettingsUpdate allows partial updates to agent type settings
type AgentTypeSettingsUpdate struct {
	Model   *string `json:"model,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
	Timeout *string `json:"timeout,omitempty"`
}

// AgentConfigUpdateResponse is the response for POST /api/config/agents
type AgentConfigUpdateResponse struct {
	Success   bool                `json:"success"`
	Settings  *AgentConfigResponse `json:"settings,omitempty"`
	Error     string              `json:"error,omitempty"`
}

// AgentConfigPersistResponse is the response for POST /api/config/agents/persist
type AgentConfigPersistResponse struct {
	Success    bool   `json:"success"`
	ConfigPath string `json:"config_path,omitempty"`
	Error      string `json:"error,omitempty"`
}

// RouteAgentConfig routes agent config requests to the appropriate handler
// Handles /api/repos/:repo_id/config/agents and /api/repos/:repo_id/config/agents/persist
func (h *AgentConfigHandler) RouteAgentConfig(w http.ResponseWriter, r *http.Request, suffix []string) {
	// /api/repos/:repo_id/config/agents (no suffix beyond "agents")
	if len(suffix) == 0 {
		switch r.Method {
		case http.MethodGet:
			h.HandleGetAgentConfig(w, r)
		case http.MethodPost:
			h.HandleUpdateAgentConfig(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/repos/:repo_id/config/agents/persist
	if len(suffix) == 1 && suffix[0] == "persist" && r.Method == http.MethodPost {
		h.HandlePersistAgentConfig(w, r)
		return
	}

	http.Error(w, "Not found", http.StatusNotFound)
}

// HandleGetAgentConfig handles GET /api/config/agents
func (h *AgentConfigHandler) HandleGetAgentConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		h.writeJSON(w, http.StatusBadRequest, AgentConfigResponse{Error: errMsg})
		return
	}

	ctx := context.Background()

	snapshot, err := api.GetAgentConfig(ctx)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, AgentConfigResponse{
			Error: fmt.Sprintf("failed to get agent config: %v", err),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, AgentConfigResponse{
		DefaultModel: snapshot.Settings.DefaultModel,
		Worker:       snapshot.Settings.Worker,
		Resolver:     snapshot.Settings.Resolver,
		Repair:       snapshot.Settings.Repair,
		Persisted:    snapshot.Persisted,
	})
}

// HandleUpdateAgentConfig handles POST /api/config/agents
func (h *AgentConfigHandler) HandleUpdateAgentConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		h.writeJSON(w, http.StatusBadRequest, AgentConfigUpdateResponse{
			Success: false,
			Error:   errMsg,
		})
		return
	}

	var req AgentConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, AgentConfigUpdateResponse{
			Success: false,
			Error:   fmt.Sprintf("Invalid JSON: %v", err),
		})
		return
	}

	ctx := context.Background()

	// Build the update struct for RepoAPI
	update := orchestrator.AgentConfigUpdate{
		DefaultModel: req.DefaultModel,
	}
	if req.Worker != nil {
		update.Worker = &orchestrator.AgentTypeSettingsUpdate{
			Model:   req.Worker.Model,
			Enabled: req.Worker.Enabled,
			Timeout: req.Worker.Timeout,
		}
	}
	if req.Resolver != nil {
		update.Resolver = &orchestrator.AgentTypeSettingsUpdate{
			Model:   req.Resolver.Model,
			Enabled: req.Resolver.Enabled,
			Timeout: req.Resolver.Timeout,
		}
	}
	if req.Repair != nil {
		update.Repair = &orchestrator.AgentTypeSettingsUpdate{
			Model:   req.Repair.Model,
			Enabled: req.Repair.Enabled,
			Timeout: req.Repair.Timeout,
		}
	}

	// Apply the update
	if err := api.UpdateAgentConfig(ctx, update); err != nil {
		h.writeJSON(w, http.StatusBadRequest, AgentConfigUpdateResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Get updated snapshot
	snapshot, err := api.GetAgentConfig(ctx)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, AgentConfigUpdateResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to get updated config: %v", err),
		})
		return
	}

	// Broadcast agent_config:changed event
	h.broadcastAgentConfigChanged("updated")

	h.writeJSON(w, http.StatusOK, AgentConfigUpdateResponse{
		Success: true,
		Settings: &AgentConfigResponse{
			DefaultModel: snapshot.Settings.DefaultModel,
			Worker:       snapshot.Settings.Worker,
			Resolver:     snapshot.Settings.Resolver,
			Repair:       snapshot.Settings.Repair,
			Persisted:    snapshot.Persisted,
		},
	})
}

// HandlePersistAgentConfig handles POST /api/config/agents/persist
func (h *AgentConfigHandler) HandlePersistAgentConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		h.writeJSON(w, http.StatusBadRequest, AgentConfigPersistResponse{
			Success: false,
			Error:   errMsg,
		})
		return
	}

	ctx := context.Background()

	configPath, err := api.PersistAgentConfig(ctx)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, AgentConfigPersistResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Broadcast agent_config:changed event
	h.broadcastAgentConfigChanged("persisted")

	h.writeJSON(w, http.StatusOK, AgentConfigPersistResponse{
		Success:    true,
		ConfigPath: configPath,
	})
}

// getRepoAPI returns the RepoAPI for the specified repo or run.
// It extracts repo_id, repo_path, or run_id from the query parameters.
func (h *AgentConfigHandler) getRepoAPI(r *http.Request) (orchestrator.RepoAPI, string) {
	if h.daemon == nil {
		return nil, "daemon not available"
	}

	orchManager := h.daemon.GetOrchestratorManager()
	if orchManager == nil {
		return nil, "orchestrator manager not available"
	}

	// Try to get the RepoAPI by run_id first (more specific)
	runID := r.URL.Query().Get("run_id")
	if runID != "" {
		api, err := orchManager.GetRepoAPIForRun(runID)
		if err != nil {
			return nil, fmt.Sprintf("no active run found for run_id %q: %v", runID, err)
		}
		return api, ""
	}

	// Try to get or create the RepoAPI by repo_id (new URL scheme)
	repoID := r.URL.Query().Get("repo_id")
	if repoID != "" {
		api, err := orchManager.GetRepoAPI(repoID)
		if err != nil {
			return nil, fmt.Sprintf("failed to get RepoAPI for repo %q: %v", repoID, err)
		}
		return api, ""
	}

	// Try to get or create the RepoAPI by repo_path (legacy)
	repoPath := r.URL.Query().Get("repo_path")
	if repoPath != "" {
		api, err := orchManager.GetRepoAPI(repoPath)
		if err != nil {
			return nil, fmt.Sprintf("failed to get RepoAPI for repo %q: %v", repoPath, err)
		}
		return api, ""
	}

	return nil, "either repo_id, repo_path, or run_id query parameter is required"
}

// writeJSON writes a JSON response with the given status code
func (h *AgentConfigHandler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		fmt.Printf("Error encoding JSON response: %v\n", err)
	}
}

// broadcastAgentConfigChanged sends an agent_config:changed event to all connected clients
func (h *AgentConfigHandler) broadcastAgentConfigChanged(action string) {
	if h.eventBus == nil {
		return
	}

	payload := map[string]interface{}{
		"action": action,
	}

	h.eventBus.Publish(Event{
		Type:      EventAgentConfigChanged,
		Timestamp: time.Now(),
		Payload:   payload,
	})
}
