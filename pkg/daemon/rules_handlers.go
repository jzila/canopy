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
	ConfigRules  *config.RulesSettings `json:"config_rules"`
	CustomRules  []rules.RuntimeRule   `json:"custom_rules"`
	RuntimeRules []rules.RuntimeRule   `json:"runtime_rules"`
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

	// Parse rule name from path: /api/rules/:name
	parts := strings.Split(strings.Trim(path, "/"), "/")
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

	engine := h.getEngine()
	if engine == nil {
		http.Error(w, "Rules engine not available", http.StatusServiceUnavailable)
		return
	}

	snapshot := engine.GetSnapshot()
	response := RulesResponse{
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

	engine := h.getEngine()
	if engine == nil {
		http.Error(w, "Rules engine not available", http.StatusServiceUnavailable)
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
			Source:     "runtime",
			CreatedAt:  time.Now(),
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
	engine := h.getEngine()
	if engine == nil {
		http.Error(w, "Rules engine not available", http.StatusServiceUnavailable)
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
	engine := h.getEngine()
	if engine == nil {
		http.Error(w, "Rules engine not available", http.StatusServiceUnavailable)
		return
	}

	// Check if the rule exists and is a runtime rule
	rule := engine.GetRule(ruleName)
	if rule == nil {
		h.writeJSON(w, http.StatusNotFound, DeleteRuleResponse{
			Success: false,
			Error:   fmt.Sprintf("rule %q not found", ruleName),
		})
		return
	}

	// Cannot delete config-sourced rules
	if rule.Source == "config" {
		h.writeJSON(w, http.StatusBadRequest, DeleteRuleResponse{
			Success: false,
			Error:   fmt.Sprintf("cannot delete config-sourced rule %q; use PATCH to disable instead", ruleName),
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

	engine := h.getEngine()
	if engine == nil {
		http.Error(w, "Rules engine not available", http.StatusServiceUnavailable)
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

// getEngine returns the rules engine from the daemon's orchestrator manager
func (h *RulesHandler) getEngine() *rules.Engine {
	if h.daemon == nil {
		return nil
	}

	orchManager := h.daemon.GetOrchestratorManager()
	if orchManager == nil {
		return nil
	}

	return orchManager.GetRulesEngine()
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
