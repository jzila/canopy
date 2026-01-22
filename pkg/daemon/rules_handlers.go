package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jzila/canopy/pkg/config"
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

// RouteRules routes rules-related requests to the appropriate handler
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

// HandleListRules handles GET /api/rules
func (h *RulesHandler) HandleListRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	engine, errMsg := h.getEngine(r)
	if engine == nil {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	snapshot := engine.GetSnapshot()
	response := RulesResponse{
		Settings:  snapshot.Settings,
		Rules:     snapshot.Rules,
		Persisted: snapshot.Persisted,
		// Deprecated fields for backwards compatibility
		ConfigRules:  snapshot.ConfigRules,
		CustomRules:  snapshot.CustomRules,
		RuntimeRules: snapshot.RuntimeRules,
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

	engine, errMsg := h.getEngine(r)
	if engine == nil {
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

	// Add the rule with validation
	if err := engine.AddRuleWithValidation(rule); err != nil {
		h.writeJSON(w, http.StatusBadRequest, AddRuleResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Get the rule back to return with full metadata
	runtimeRule := engine.GetRule(req.Name)
	if runtimeRule == nil {
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
	engine, errMsg := h.getEngine(r)
	if engine == nil {
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

	// Update the rule
	if err := engine.UpdateRule(ruleName, *req.Enabled); err != nil {
		h.writeJSON(w, http.StatusNotFound, UpdateRuleResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Get the updated rule
	runtimeRule := engine.GetRule(ruleName)
	if runtimeRule == nil {
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
	engine, errMsg := h.getEngine(r)
	if engine == nil {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	// Check if the rule exists
	rule := engine.GetRule(ruleName)
	if rule == nil {
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

	// Remove the runtime rule
	if !engine.RemoveRule(ruleName) {
		h.writeJSON(w, http.StatusInternalServerError, DeleteRuleResponse{
			Success: false,
			Error:   "failed to remove rule",
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

	engine, errMsg := h.getEngine(r)
	if engine == nil {
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

	// Update the config settings
	if err := engine.UpdateConfigSettings(update); err != nil {
		h.writeJSON(w, http.StatusBadRequest, UpdateConfigResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Get the updated config
	configSettings := engine.GetConfigSettings()

	// Broadcast rules:changed event for config update
	h.broadcastRulesChanged("config_updated", nil)

	h.writeJSON(w, http.StatusOK, UpdateConfigResponse{
		Success:     true,
		ConfigRules: configSettings,
	})
}

// getEngine returns the rules engine for the specified repo or run.
// It extracts repo_path or run_id from the query parameters.
// If neither is specified, it returns nil with an error message.
func (h *RulesHandler) getEngine(r *http.Request) (*rules.Engine, string) {
	if h.daemon == nil {
		return nil, "daemon not available"
	}

	orchManager := h.daemon.GetOrchestratorManager()
	if orchManager == nil {
		return nil, "orchestrator manager not available"
	}

	// Try to get the engine by run_id first (more specific)
	runID := r.URL.Query().Get("run_id")
	if runID != "" {
		engine := orchManager.GetRulesEngineForRun(runID)
		if engine == nil {
			return nil, fmt.Sprintf("no active run found for run_id %q", runID)
		}
		return engine, ""
	}

	// Try to get the engine by repo_path
	repoPath := r.URL.Query().Get("repo_path")
	if repoPath != "" {
		engine := orchManager.GetRulesEngineForRepo(repoPath)
		if engine == nil {
			return nil, fmt.Sprintf("no active run found for repo %q", repoPath)
		}
		return engine, ""
	}

	return nil, "either repo_path or run_id query parameter is required"
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
	engine, errMsg := h.getEngine(r)
	if engine == nil {
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

	// Persist the rule in the engine (moves from runtime to config)
	persistedRule, err := engine.PersistRule(ruleName)
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, PersistRuleResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Save the config to disk
	configPath, err := h.saveConfig(workDir, engine)
	if err != nil {
		// Note: The rule is already moved in memory, but disk save failed
		// This is a partial failure state
		h.writeJSON(w, http.StatusInternalServerError, PersistRuleResponse{
			Success: false,
			Error:   fmt.Sprintf("Rule persisted in memory but failed to save config: %v", err),
		})
		return
	}

	// Create RuntimeRule for response
	runtimeRule := rules.RuntimeRule{
		CustomRule: *persistedRule,
		Persisted:  true,
	}

	// Broadcast rules:changed event
	h.broadcastRulesChanged("persisted", &runtimeRule)

	h.writeJSON(w, http.StatusOK, PersistRuleResponse{
		Success:    true,
		Rule:       runtimeRule,
		ConfigPath: configPath,
	})
}

// HandlePersistAllRules handles POST /api/rules/persist-all
// Persists all runtime rules to the config file
func (h *RulesHandler) HandlePersistAllRules(w http.ResponseWriter, r *http.Request) {
	engine, errMsg := h.getEngine(r)
	if engine == nil {
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

	// Persist all rules in the engine
	persisted, err := engine.PersistAllRules()
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, PersistAllRulesResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	if len(persisted) == 0 {
		h.writeJSON(w, http.StatusOK, PersistAllRulesResponse{
			Success:   true,
			Persisted: []string{},
		})
		return
	}

	// Save the config to disk
	configPath, err := h.saveConfig(workDir, engine)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, PersistAllRulesResponse{
			Success: false,
			Error:   fmt.Sprintf("Rules persisted in memory but failed to save config: %v", err),
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

// saveConfig saves the current rules configuration to disk
func (h *RulesHandler) saveConfig(workDir string, engine *rules.Engine) (string, error) {
	// Load existing config (or get default)
	cfg, err := config.LoadConfig(workDir)
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}

	// Update rules settings from engine
	rulesSettings := engine.GetConfigForPersistence()
	if rulesSettings != nil {
		cfg.Rules = *rulesSettings
	}

	// Save the config
	if err := config.SaveConfig(workDir, cfg); err != nil {
		return "", fmt.Errorf("save config: %w", err)
	}

	return fmt.Sprintf("%s/.canopy/config.toml", workDir), nil
}
