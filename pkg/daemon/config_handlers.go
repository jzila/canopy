package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jzila/canopy/pkg/config"
	"github.com/jzila/canopy/pkg/orchestrator"
	"github.com/jzila/canopy/pkg/sandbox"
	"github.com/jzila/canopy/pkg/validation"
)

// ConfigHandler handles HTTP requests for configuration queries.
// These endpoints expose the RepoAPI config query methods.
type ConfigHandler struct {
	daemon             *Daemon
	agentConfigHandler *AgentConfigHandler
}

// NewConfigHandler creates a new config handler.
func NewConfigHandler(daemon *Daemon) *ConfigHandler {
	return &ConfigHandler{
		daemon:             daemon,
		agentConfigHandler: NewAgentConfigHandler(daemon),
	}
}

// RulesSettingsResponse is the response for GET /api/config/rules.
type RulesSettingsResponse struct {
	Settings *config.RulesSettings `json:"settings"`
	Error    string                `json:"error,omitempty"`
}

// SandboxConfigResponse is the response for GET /api/config/sandbox.
type SandboxConfigResponse struct {
	Config *sandbox.SandboxConfig `json:"config"`
	Error  string                 `json:"error,omitempty"`
}

// ValidationConfigResponse is the response for GET /api/config/validation.
type ValidationConfigResponse struct {
	Config *validation.ValidationConfig `json:"config"`
	Error  string                       `json:"error,omitempty"`
}

// RouteConfig routes config-related requests to the appropriate handler (legacy /api/config/* routes).
func (h *ConfigHandler) RouteConfig(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Only GET requests are supported for config queries
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch path {
	case "/api/config/rules":
		h.HandleGetRulesSettings(w, r)
	case "/api/config/sandbox":
		h.HandleGetSandboxConfig(w, r)
	case "/api/config/validation":
		h.HandleGetValidationConfig(w, r)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// RouteRepoConfig routes repo-scoped config requests.
// Handles /api/repos/:repo_id/config/* where repo_id is already extracted.
// The suffix contains remaining path parts after /api/repos/:repo_id/config
func (h *ConfigHandler) RouteRepoConfig(w http.ResponseWriter, r *http.Request, suffix []string) {
	if len(suffix) == 0 {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	switch suffix[0] {
	case "rules":
		// Only GET requests are supported for rules config queries
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if len(suffix) != 1 {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		h.HandleGetRulesSettings(w, r)
	case "sandbox":
		// Only GET requests are supported for sandbox config queries
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if len(suffix) != 1 {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		h.HandleGetSandboxConfig(w, r)
	case "validation":
		// Only GET requests are supported for validation config queries
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if len(suffix) != 1 {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		h.HandleGetValidationConfig(w, r)
	case "agents":
		// Agent config supports GET, POST, and POST /persist
		h.RouteAgentConfig(w, r, suffix[1:])
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// HandleGetRulesSettings handles GET /api/config/rules.
// Returns the rules filter settings (priority, types, labels, etc.).
func (h *ConfigHandler) HandleGetRulesSettings(w http.ResponseWriter, r *http.Request) {
	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		h.writeJSON(w, http.StatusBadRequest, RulesSettingsResponse{Error: errMsg})
		return
	}

	ctx := context.Background()

	// Get rules snapshot which includes settings
	snapshot, err := api.ListRules(ctx)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, RulesSettingsResponse{
			Error: fmt.Sprintf("failed to get rules settings: %v", err),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, RulesSettingsResponse{
		Settings: snapshot.Settings,
	})
}

// HandleGetSandboxConfig handles GET /api/config/sandbox.
// Returns the sandbox configuration for the repository.
func (h *ConfigHandler) HandleGetSandboxConfig(w http.ResponseWriter, r *http.Request) {
	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		h.writeJSON(w, http.StatusBadRequest, SandboxConfigResponse{Error: errMsg})
		return
	}

	ctx := context.Background()

	cfg, err := api.GetSandboxConfig(ctx)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, SandboxConfigResponse{
			Error: fmt.Sprintf("failed to get sandbox config: %v", err),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, SandboxConfigResponse{
		Config: cfg,
	})
}

// HandleGetValidationConfig handles GET /api/config/validation.
// Returns the validation configuration for the repository.
func (h *ConfigHandler) HandleGetValidationConfig(w http.ResponseWriter, r *http.Request) {
	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		h.writeJSON(w, http.StatusBadRequest, ValidationConfigResponse{Error: errMsg})
		return
	}

	ctx := context.Background()

	cfg, err := api.GetValidationConfig(ctx)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, ValidationConfigResponse{
			Error: fmt.Sprintf("failed to get validation config: %v", err),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, ValidationConfigResponse{
		Config: cfg,
	})
}

// getRepoAPI returns the RepoAPI for the specified repo or run.
// It extracts repo_id, repo_path, or run_id from the query parameters.
func (h *ConfigHandler) getRepoAPI(r *http.Request) (orchestrator.RepoAPI, string) {
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
	// repo_id is the repo path for now (from /api/repos/:repo_id/...)
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

// writeJSON writes a JSON response with the given status code.
func (h *ConfigHandler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		fmt.Printf("Error encoding JSON response: %v\n", err)
	}
}

// RouteAgentConfig routes agent config requests to the AgentConfigHandler.
// Handles /api/repos/:repo_id/config/agents and /api/repos/:repo_id/config/agents/persist
func (h *ConfigHandler) RouteAgentConfig(w http.ResponseWriter, r *http.Request, suffix []string) {
	if h.agentConfigHandler == nil {
		http.Error(w, "Agent config management not available", http.StatusServiceUnavailable)
		return
	}
	h.agentConfigHandler.RouteAgentConfig(w, r, suffix)
}
