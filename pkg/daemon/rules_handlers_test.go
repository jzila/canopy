package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jzila/canopy/pkg/config"
	"github.com/jzila/canopy/pkg/rules"
)

func setupTestDaemonWithRules() *Daemon {
	daemon := newDaemonForTest(Config{}, nil, nil)
	daemon.Init() // This initializes orchManager with rulesEngine
	return daemon
}

func TestHandleListRules_Success(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	req := httptest.NewRequest(http.MethodGet, "/api/rules", nil)
	w := httptest.NewRecorder()

	handler.HandleListRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response RulesResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Check that we get a valid response with config rules
	if response.ConfigRules == nil {
		t.Error("expected ConfigRules to be non-nil")
	}
}

func TestHandleListRules_MethodNotAllowed(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	req := httptest.NewRequest(http.MethodPost, "/api/rules", nil)
	w := httptest.NewRecorder()

	handler.HandleListRules(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandleAddRule_Success(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	reqBody := AddRuleRequest{
		Name:      "test-rule",
		Condition: "priority > 2",
		Action:    "skip",
		Reason:    "test reason",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleAddRule(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var response AddRuleResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Success {
		t.Errorf("expected success=true, got %v (error: %s)", response.Success, response.Error)
	}

	if response.Rule.Name != "test-rule" {
		t.Errorf("expected rule name 'test-rule', got %s", response.Rule.Name)
	}

	if response.Rule.Source != "runtime" {
		t.Errorf("expected rule source 'runtime', got %s", response.Rule.Source)
	}
}

func TestHandleAddRule_MissingName(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	reqBody := AddRuleRequest{
		Condition: "priority > 2",
		Action:    "skip",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleAddRule(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var response AddRuleResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Success {
		t.Error("expected success=false for missing name")
	}
}

func TestHandleAddRule_DuplicateName(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	// Add first rule
	reqBody := AddRuleRequest{
		Name:      "dup-rule",
		Condition: "priority > 2",
		Action:    "skip",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.HandleAddRule(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("first rule creation failed: %d", w.Code)
	}

	// Try to add duplicate
	body, _ = json.Marshal(reqBody)
	req = httptest.NewRequest(http.MethodPost, "/api/rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.HandleAddRule(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for duplicate, got %d", w.Code)
	}
}

func TestHandleUpdateRule_Enable(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	// First add a rule
	engine := daemon.orchManager.GetRulesEngine()
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "test-rule",
		Condition: "priority > 2",
		Action:    "skip",
	})

	// Disable it
	reqBody := UpdateRuleRequest{Enabled: ptrBool(false)}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, "/api/rules/test-rule", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleUpdateRule(w, req, "test-rule")

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response UpdateRuleResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Success {
		t.Errorf("expected success=true, got %v", response.Success)
	}

	if response.Rule.Enabled == nil || *response.Rule.Enabled {
		t.Error("expected rule to be disabled")
	}
}

func TestHandleUpdateRule_NotFound(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	reqBody := UpdateRuleRequest{Enabled: ptrBool(true)}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, "/api/rules/nonexistent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleUpdateRule(w, req, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestHandleDeleteRule_Success(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	// First add a runtime rule
	engine := daemon.orchManager.GetRulesEngine()
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "to-delete",
		Condition: "priority > 2",
		Action:    "skip",
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/rules/to-delete", nil)
	w := httptest.NewRecorder()

	handler.HandleDeleteRule(w, req, "to-delete")

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response DeleteRuleResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Success {
		t.Errorf("expected success=true, got %v", response.Success)
	}

	// Verify rule is gone
	rule := engine.GetRule("to-delete")
	if rule != nil {
		t.Error("expected rule to be deleted")
	}
}

func TestHandleDeleteRule_NotFound(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	req := httptest.NewRequest(http.MethodDelete, "/api/rules/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.HandleDeleteRule(w, req, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestHandleUpdateConfig_Success(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	update := rules.ConfigSettingsUpdate{
		PriorityMax:   ptrInt(2),
		ExcludeLabels: &[]string{"wip", "blocked"},
	}
	body, _ := json.Marshal(update)

	req := httptest.NewRequest(http.MethodPatch, "/api/rules/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleUpdateConfig(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response UpdateConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Success {
		t.Errorf("expected success=true, got %v", response.Success)
	}

	if response.ConfigRules.PriorityMax != 2 {
		t.Errorf("expected priority_max=2, got %d", response.ConfigRules.PriorityMax)
	}

	if len(response.ConfigRules.ExcludeLabels) != 2 {
		t.Errorf("expected 2 exclude labels, got %d", len(response.ConfigRules.ExcludeLabels))
	}
}

func TestHandleUpdateConfig_InvalidPriority(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	update := rules.ConfigSettingsUpdate{
		PriorityMax: ptrInt(10), // Invalid: max is 4
	}
	body, _ := json.Marshal(update)

	req := httptest.NewRequest(http.MethodPatch, "/api/rules/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleUpdateConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestRouteRules_ListRules(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	req := httptest.NewRequest(http.MethodGet, "/api/rules", nil)
	w := httptest.NewRecorder()

	handler.RouteRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRouteRules_AddRule(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	reqBody := AddRuleRequest{
		Name:      "routed-rule",
		Condition: "priority > 2",
		Action:    "skip",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.RouteRules(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouteRules_UpdateRule(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	// First add a rule
	engine := daemon.orchManager.GetRulesEngine()
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "routed-update",
		Condition: "priority > 2",
		Action:    "skip",
	})

	reqBody := UpdateRuleRequest{Enabled: ptrBool(false)}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, "/api/rules/routed-update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.RouteRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouteRules_DeleteRule(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	// First add a rule
	engine := daemon.orchManager.GetRulesEngine()
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "routed-delete",
		Condition: "priority > 2",
		Action:    "skip",
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/rules/routed-delete", nil)
	w := httptest.NewRecorder()

	handler.RouteRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouteRules_ConfigUpdate(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	update := rules.ConfigSettingsUpdate{
		PriorityMax: ptrInt(1),
	}
	body, _ := json.Marshal(update)

	req := httptest.NewRequest(http.MethodPatch, "/api/rules/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.RouteRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRulesHandler_BroadcastsEvent(t *testing.T) {
	daemon := setupTestDaemonWithRules()
	handler := NewRulesHandler(daemon)

	// Subscribe to events
	eventReceived := false
	var receivedAction string
	daemon.eventBus.Subscribe(func(e Event) {
		if e.Type == EventRulesChanged {
			eventReceived = true
			payload := e.Payload.(map[string]interface{})
			receivedAction = payload["action"].(string)
		}
	})

	// Add a rule
	reqBody := AddRuleRequest{
		Name:      "event-test",
		Condition: "priority > 2",
		Action:    "skip",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleAddRule(w, req)

	if !eventReceived {
		t.Error("expected rules:changed event to be broadcast")
	}

	if receivedAction != "added" {
		t.Errorf("expected action 'added', got '%s'", receivedAction)
	}
}

// Helper functions
func ptrBool(b bool) *bool {
	return &b
}

func ptrInt(i int) *int {
	return &i
}
