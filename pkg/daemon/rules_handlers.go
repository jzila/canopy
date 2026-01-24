package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jzila/canopy/pkg/config"
	"github.com/jzila/canopy/pkg/orchestrator"
	"github.com/jzila/canopy/pkg/rules"
)

// RulesHandler handles HTTP requests for runtime rule management
type RulesHandler struct {
	daemon   *Daemon
	eventBus *EventBus
}

// NewRulesHandler creates a new rules handler
func NewRulesHandler(daemon *Daemon) *RulesHandler {
	var eventBus *EventBus
	if daemon != nil {
		eventBus = daemon.GetEventBus()
	}
	return &RulesHandler{
		daemon:   daemon,
		eventBus: eventBus,
	}
}

// RulesResponse is the response for GET /api/rules
type RulesResponse struct {
	Settings  *config.RulesSettings `json:"settings"`  // Filter settings
	Rules     []rules.RuntimeRule   `json:"rules"`     // Unified rules list with per-rule persistence status
	Persisted bool                  `json:"persisted"` // true if entire list matches config (no additions, deletions, or reorders)
	// Deprecated: use Settings/Rules instead. Kept for API backwards compatibility.
	ConfigRules  *config.RulesSettings `json:"config_rules,omitempty"`
	CustomRules  []rules.RuntimeRule   `json:"custom_rules,omitempty"`
	RuntimeRules []rules.RuntimeRule   `json:"runtime_rules,omitempty"`
}

// AddRuleRequest is the request body for POST /api/rules
type AddRuleRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Condition   string `json:"condition"`
	Action      string `json:"action"`
	Reason      string `json:"reason,omitempty"`
}

// AddRuleResponse is the response for POST /api/rules
type AddRuleResponse struct {
	Success bool              `json:"success"`
	Rule    rules.RuntimeRule `json:"rule,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// UpdateRuleRequest is the request body for PATCH /api/rules/:name
type UpdateRuleRequest struct {
	Enabled *bool `json:"enabled"`
}

// UpdateRuleResponse is the response for PATCH /api/rules/:name
type UpdateRuleResponse struct {
	Success bool              `json:"success"`
	Rule    rules.RuntimeRule `json:"rule,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// DeleteRuleResponse is the response for DELETE /api/rules/:name
type DeleteRuleResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// PersistRuleResponse is the response for POST /api/rules/:name/persist
type PersistRuleResponse struct {
	Success    bool              `json:"success"`
	Rule       rules.RuntimeRule `json:"rule,omitempty"`
	ConfigPath string            `json:"config_path,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// PersistAllRulesResponse is the response for POST /api/rules/persist-all
type PersistAllRulesResponse struct {
	Success    bool     `json:"success"`
	Persisted  []string `json:"persisted,omitempty"`
	ConfigPath string   `json:"config_path,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// UpdateConfigResponse is the response for PATCH /api/rules/config
type UpdateConfigResponse struct {
	Success     bool                  `json:"success"`
	ConfigRules *config.RulesSettings `json:"config_rules,omitempty"`
	Error       string                `json:"error,omitempty"`
}

// ReorderRuleRequest is the request body for POST /api/rules/:name/reorder
type ReorderRuleRequest struct {
	Position int `json:"position"` // New 0-indexed position
}

// ReorderRuleResponse is the response for POST /api/rules/:name/reorder
type ReorderRuleResponse struct {
	Success   bool                `json:"success"`
	Rules     []rules.RuntimeRule `json:"rules,omitempty"`     // Updated rules list
	Persisted bool                `json:"persisted,omitempty"` // List-level persistence status
	Error     string              `json:"error,omitempty"`
}

// RouteRules routes rules-related requests to the appropriate handler (legacy /api/rules/* routes)
func (h *RulesHandler) RouteRules(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// GET /api/rules - list all rules
	if path == "/api/rules" {
		switch r.Method {
		case http.MethodGet:
			h.HandleListRules(w, r)
		case http.MethodPost:
			h.HandleAddRule(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// PATCH /api/rules/config - update config settings
	if path == "/api/rules/config" && r.Method == http.MethodPatch {
		h.HandleUpdateConfig(w, r)
		return
	}

	// POST /api/rules/persist-all - persist all runtime rules
	if path == "/api/rules/persist-all" && r.Method == http.MethodPost {
		h.HandlePersistAllRules(w, r)
		return
	}

	// Parse rule name from path: /api/rules/:name or /api/rules/:name/persist
	parts := strings.Split(strings.Trim(path, "/"), "/")

	// Handle /api/rules/:name/persist
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "rules" && parts[3] == "persist" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandlePersistRule(w, r, parts[2])
		return
	}

	// Handle /api/rules/:name/reorder
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "rules" && parts[3] == "reorder" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleReorderRule(w, r, parts[2])
		return
	}

	// Handle /api/rules/:name
	if len(parts) != 3 || parts[0] != "api" || parts[1] != "rules" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	ruleName := parts[2]
	if ruleName == "" {
		http.Error(w, "Rule name required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPatch:
		h.HandleUpdateRule(w, r, ruleName)
	case http.MethodDelete:
		h.HandleDeleteRule(w, r, ruleName)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// RouteRepoRules routes repo-scoped rules requests
// Handles /api/repos/:repo_id/rules/* where repo_id is already extracted
// The suffix contains remaining path parts after /api/repos/:repo_id/rules
func (h *RulesHandler) RouteRepoRules(w http.ResponseWriter, r *http.Request, suffix []string) {
	// /api/repos/:repo_id/rules (no suffix)
	if len(suffix) == 0 {
		switch r.Method {
		case http.MethodGet:
			h.HandleListRules(w, r)
		case http.MethodPost:
			h.HandleAddRule(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/repos/:repo_id/rules/config
	if len(suffix) == 1 && suffix[0] == "config" && r.Method == http.MethodPatch {
		h.HandleUpdateConfig(w, r)
		return
	}

	// /api/repos/:repo_id/rules/persist-all
	if len(suffix) == 1 && suffix[0] == "persist-all" && r.Method == http.MethodPost {
		h.HandlePersistAllRules(w, r)
		return
	}

	// /api/repos/:repo_id/rules/:name
	if len(suffix) == 1 {
		ruleName := suffix[0]
		if ruleName == "" {
			http.Error(w, "Rule name required", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPatch:
			h.HandleUpdateRule(w, r, ruleName)
		case http.MethodDelete:
			h.HandleDeleteRule(w, r, ruleName)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/repos/:repo_id/rules/:name/persist
	if len(suffix) == 2 && suffix[1] == "persist" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandlePersistRule(w, r, suffix[0])
		return
	}

	// /api/repos/:repo_id/rules/:name/reorder
	if len(suffix) == 2 && suffix[1] == "reorder" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleReorderRule(w, r, suffix[0])
		return
	}

	http.Error(w, "Not found", http.StatusNotFound)
}

// HandleListRules handles GET /api/rules
func (h *RulesHandler) HandleListRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	snapshot, err := api.ListRules(context.Background())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list rules: %v", err), http.StatusInternalServerError)
		return
	}

	response := RulesResponse{
		Settings:  snapshot.Settings,
		Rules:     snapshot.Rules,
		Persisted: snapshot.Persisted,
		// Deprecated fields for backwards compatibility
		ConfigRules:  snapshot.ConfigRules,  //nolint:staticcheck // Intentionally using deprecated field for API compatibility
		CustomRules:  snapshot.CustomRules,  //nolint:staticcheck // Intentionally using deprecated field for API compatibility
		RuntimeRules: snapshot.RuntimeRules, //nolint:staticcheck // Intentionally using deprecated field for API compatibility
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// HandleAddRule handles POST /api/rules
func (h *RulesHandler) HandleAddRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	var req AddRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, AddRuleResponse{
			Success: false,
			Error:   fmt.Sprintf("Invalid JSON: %v", err),
		})
		return
	}

	// Convert request to CustomRule
	rule := config.CustomRule{
		Name:      req.Name,
		Condition: req.Condition,
		Action:    req.Action,
		Reason:    req.Reason,
	}

	ctx := context.Background()

	// Add the rule with validation
	if err := api.AddRule(ctx, rule); err != nil {
		h.writeJSON(w, http.StatusBadRequest, AddRuleResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Get the rule back to return with full metadata
	runtimeRule, err := api.GetRule(ctx, req.Name)
	if err != nil || runtimeRule == nil {
		// Should not happen, but handle gracefully
		runtimeRule = &rules.RuntimeRule{
			CustomRule: rule,
			Persisted:  false,
		}
	}

	// Broadcast rules:changed event
	h.broadcastRulesChanged("added", runtimeRule)

	h.writeJSON(w, http.StatusCreated, AddRuleResponse{
		Success: true,
		Rule:    *runtimeRule,
	})
}

// HandleUpdateRule handles PATCH /api/rules/:name
func (h *RulesHandler) HandleUpdateRule(w http.ResponseWriter, r *http.Request, ruleName string) {
	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	var req UpdateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, UpdateRuleResponse{
			Success: false,
			Error:   fmt.Sprintf("Invalid JSON: %v", err),
		})
		return
	}

	if req.Enabled == nil {
		h.writeJSON(w, http.StatusBadRequest, UpdateRuleResponse{
			Success: false,
			Error:   "enabled field is required",
		})
		return
	}

	ctx := context.Background()

	// Update the rule via RepoAPI
	update := orchestrator.RuleUpdate{Enabled: req.Enabled}
	if err := api.UpdateRule(ctx, ruleName, update); err != nil {
		h.writeJSON(w, http.StatusNotFound, UpdateRuleResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Get the updated rule
	runtimeRule, err := api.GetRule(ctx, ruleName)
	if err != nil || runtimeRule == nil {
		h.writeJSON(w, http.StatusInternalServerError, UpdateRuleResponse{
			Success: false,
			Error:   "Rule not found after update",
		})
		return
	}

	// Broadcast rules:changed event
	h.broadcastRulesChanged("updated", runtimeRule)

	h.writeJSON(w, http.StatusOK, UpdateRuleResponse{
		Success: true,
		Rule:    *runtimeRule,
	})
}

// HandleDeleteRule handles DELETE /api/rules/:name
func (h *RulesHandler) HandleDeleteRule(w http.ResponseWriter, r *http.Request, ruleName string) {
	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	ctx := context.Background()

	// Check if the rule exists
	rule, err := api.GetRule(ctx, ruleName)
	if err != nil || rule == nil {
		h.writeJSON(w, http.StatusNotFound, DeleteRuleResponse{
			Success: false,
			Error:   fmt.Sprintf("rule %q not found", ruleName),
		})
		return
	}

	// Cannot delete persisted rules
	if rule.Persisted {
		h.writeJSON(w, http.StatusBadRequest, DeleteRuleResponse{
			Success: false,
			Error:   fmt.Sprintf("cannot delete persisted rule %q; use PATCH to disable instead", ruleName),
		})
		return
	}

	// Remove the runtime rule via RepoAPI
	if err := api.DeleteRule(ctx, ruleName); err != nil {
		h.writeJSON(w, http.StatusInternalServerError, DeleteRuleResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to remove rule: %v", err),
		})
		return
	}

	// Broadcast rules:changed event
	h.broadcastRulesChanged("deleted", rule)

	h.writeJSON(w, http.StatusOK, DeleteRuleResponse{
		Success: true,
	})
}

// HandleUpdateConfig handles PATCH /api/rules/config
func (h *RulesHandler) HandleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	var update rules.ConfigSettingsUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		h.writeJSON(w, http.StatusBadRequest, UpdateConfigResponse{
			Success: false,
			Error:   fmt.Sprintf("Invalid JSON: %v", err),
		})
		return
	}

	ctx := context.Background()

	// Update the config settings via RepoAPI
	if err := api.UpdateConfigSettings(ctx, update); err != nil {
		h.writeJSON(w, http.StatusBadRequest, UpdateConfigResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Get the updated config via ListRules
	snapshot, err := api.ListRules(ctx)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, UpdateConfigResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to get updated config: %v", err),
		})
		return
	}

	// Broadcast rules:changed event for config update
	h.broadcastRulesChanged("config_updated", nil)

	h.writeJSON(w, http.StatusOK, UpdateConfigResponse{
		Success:     true,
		ConfigRules: snapshot.Settings,
	})
}

// getRepoAPI returns the RepoAPI for the specified repo or run.
// It extracts repo_id, repo_path, or run_id from the query parameters.
// If none is specified, it returns nil with an error message.
// When repo_path/repo_id is specified and no active run exists, a standalone RepoAPI
// is created from the repo's config.
func (h *RulesHandler) getRepoAPI(r *http.Request) (orchestrator.RepoAPI, string) {
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
	// This works whether or not an active run exists
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
func (h *RulesHandler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		// Log but don't try to write again
		fmt.Printf("Error encoding JSON response: %v\n", err)
	}
}

// broadcastRulesChanged sends a rules:changed event to all connected clients
func (h *RulesHandler) broadcastRulesChanged(action string, rule *rules.RuntimeRule) {
	if h.eventBus == nil {
		return
	}

	payload := map[string]interface{}{
		"action": action,
	}
	if rule != nil {
		payload["rule"] = rule
	}

	h.eventBus.Publish(Event{
		Type:      EventRulesChanged,
		Timestamp: time.Now(),
		Payload:   payload,
	})
}

// HandlePersistRule handles POST /api/rules/:name/persist
// Persists a single runtime rule to the config file
func (h *RulesHandler) HandlePersistRule(w http.ResponseWriter, r *http.Request, ruleName string) {
	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	// Get workDir from query parameters (repo_path is required for persist operations)
	workDir := r.URL.Query().Get("repo_path")
	if workDir == "" {
		h.writeJSON(w, http.StatusBadRequest, PersistRuleResponse{
			Success: false,
			Error:   "repo_path query parameter is required for persist operations",
		})
		return
	}

	ctx := context.Background()

	// Persist the rule via RepoAPI (handles both memory and disk persistence)
	runtimeRule, configPath, err := api.PersistRule(ctx, ruleName)
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, PersistRuleResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Broadcast rules:changed event
	h.broadcastRulesChanged("persisted", runtimeRule)

	h.writeJSON(w, http.StatusOK, PersistRuleResponse{
		Success:    true,
		Rule:       *runtimeRule,
		ConfigPath: configPath,
	})
}

// HandlePersistAllRules handles POST /api/rules/persist-all
// Persists all runtime rules to the config file
func (h *RulesHandler) HandlePersistAllRules(w http.ResponseWriter, r *http.Request) {
	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	// Get workDir from query parameters (repo_path is required for persist operations)
	workDir := r.URL.Query().Get("repo_path")
	if workDir == "" {
		h.writeJSON(w, http.StatusBadRequest, PersistAllRulesResponse{
			Success: false,
			Error:   "repo_path query parameter is required for persist operations",
		})
		return
	}

	ctx := context.Background()

	// Persist all rules via RepoAPI (handles both memory and disk persistence)
	persisted, configPath, err := api.PersistRules(ctx)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, PersistAllRulesResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Broadcast rules:changed event
	h.broadcastRulesChanged("persisted_all", nil)

	h.writeJSON(w, http.StatusOK, PersistAllRulesResponse{
		Success:    true,
		Persisted:  persisted,
		ConfigPath: configPath,
	})
}

// HandleReorderRule handles POST /api/rules/:name/reorder
// Moves a rule to a new position in the rules list.
// Reordering causes list-level persisted to become false since order changed.
func (h *RulesHandler) HandleReorderRule(w http.ResponseWriter, r *http.Request, ruleName string) {
	api, errMsg := h.getRepoAPI(r)
	if api == nil {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	var req ReorderRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, ReorderRuleResponse{
			Success: false,
			Error:   fmt.Sprintf("Invalid JSON: %v", err),
		})
		return
	}

	ctx := context.Background()

	// Reorder the rule via RepoAPI
	if err := api.ReorderRule(ctx, ruleName, req.Position); err != nil {
		// Determine status code based on error type
		statusCode := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			statusCode = http.StatusNotFound
		}
		h.writeJSON(w, statusCode, ReorderRuleResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Get updated snapshot via ListRules
	snapshot, err := api.ListRules(ctx)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, ReorderRuleResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to get updated rules: %v", err),
		})
		return
	}

	// Broadcast rules:changed event
	h.broadcastRulesChanged("reordered", nil)

	h.writeJSON(w, http.StatusOK, ReorderRuleResponse{
		Success:   true,
		Rules:     snapshot.Rules,
		Persisted: snapshot.Persisted,
	})
}
