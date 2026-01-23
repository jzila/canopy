package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/jzila/canopy/pkg/config"
	"github.com/jzila/canopy/pkg/rules"
)

// testRunCounter ensures unique run IDs across parallel tests
var testRunCounter atomic.Int64

// testContext holds test-specific values to avoid global state conflicts
type testContext struct {
	repoPath string
	runID    string
}

func setupTestDaemonWithRulesT(t *testing.T) (*Daemon, *rules.Engine, *testContext) {
	t.Helper()

	// Use unique paths per test to avoid parallel test conflicts
	repoPath := t.TempDir()
	runID := fmt.Sprintf("test-run-%d", testRunCounter.Add(1))

	daemon := newDaemonForTest(Config{}, nil, nil)
	daemon.Init() // This initializes orchManager

	// Create a rules engine
	defaultRules := config.DefaultRulesSettings()
	rulesEngine := rules.NewEngine(&defaultRules)

	// Store the engine in the standalone engines cache.
	// This is simpler than creating a full orchestrator (which requires beads).
	// The handler's getEngine() calls GetOrCreateRulesEngineForRepo which will
	// find this cached engine.
	daemon.orchManager.standaloneEngines.Store(repoPath, rulesEngine)

	ctx := &testContext{
		repoPath: repoPath,
		runID:    runID,
	}

	return daemon, rulesEngine, ctx
}

// Helper to add query parameters to request URL
func addRepoQueryParamT(url string, ctx *testContext) string {
	return url + "?repo_path=" + ctx.repoPath
}

func TestHandleListRules_Success(t *testing.T) {
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	req := httptest.NewRequest(http.MethodGet, addRepoQueryParamT("/api/rules", ctx), nil)
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

func TestHandleListRules_MissingQueryParam(t *testing.T) {
	daemon, _, _ := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	// No query params - should fail
	req := httptest.NewRequest(http.MethodGet, "/api/rules", nil)
	w := httptest.NewRecorder()

	handler.HandleListRules(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for missing query param, got %d", w.Code)
	}
}

func TestHandleListRules_MethodNotAllowed(t *testing.T) {
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	req := httptest.NewRequest(http.MethodPost, addRepoQueryParamT("/api/rules", ctx), nil)
	w := httptest.NewRecorder()

	handler.HandleListRules(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandleAddRule_Success(t *testing.T) {
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	reqBody := AddRuleRequest{
		Name:      "test-rule",
		Condition: "priority > 2",
		Action:    "skip",
		Reason:    "test reason",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, addRepoQueryParamT("/api/rules", ctx), bytes.NewReader(body))
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

	if response.Rule.Persisted {
		t.Errorf("expected rule to not be persisted, got persisted=true")
	}
}

func TestHandleAddRule_MissingName(t *testing.T) {
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	reqBody := AddRuleRequest{
		Condition: "priority > 2",
		Action:    "skip",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, addRepoQueryParamT("/api/rules", ctx), bytes.NewReader(body))
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
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	// Add first rule
	reqBody := AddRuleRequest{
		Name:      "dup-rule",
		Condition: "priority > 2",
		Action:    "skip",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, addRepoQueryParamT("/api/rules", ctx), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.HandleAddRule(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("first rule creation failed: %d", w.Code)
	}

	// Try to add duplicate
	body, _ = json.Marshal(reqBody)
	req = httptest.NewRequest(http.MethodPost, addRepoQueryParamT("/api/rules", ctx), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.HandleAddRule(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for duplicate, got %d", w.Code)
	}
}

func TestHandleUpdateRule_Enable(t *testing.T) {
	daemon, engine, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	// First add a rule
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "test-rule",
		Condition: "priority > 2",
		Action:    "skip",
	})

	// Disable it
	reqBody := UpdateRuleRequest{Enabled: ptrBool(false)}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, addRepoQueryParamT("/api/rules/test-rule", ctx), bytes.NewReader(body))
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
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	reqBody := UpdateRuleRequest{Enabled: ptrBool(true)}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, addRepoQueryParamT("/api/rules/nonexistent", ctx), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleUpdateRule(w, req, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestHandleDeleteRule_Success(t *testing.T) {
	daemon, engine, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	// First add a runtime rule
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "to-delete",
		Condition: "priority > 2",
		Action:    "skip",
	})

	req := httptest.NewRequest(http.MethodDelete, addRepoQueryParamT("/api/rules/to-delete", ctx), nil)
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
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	req := httptest.NewRequest(http.MethodDelete, addRepoQueryParamT("/api/rules/nonexistent", ctx), nil)
	w := httptest.NewRecorder()

	handler.HandleDeleteRule(w, req, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestHandleUpdateConfig_Success(t *testing.T) {
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	update := rules.ConfigSettingsUpdate{
		PriorityMax:   ptrInt(2),
		ExcludeLabels: &[]string{"wip", "blocked"},
	}
	body, _ := json.Marshal(update)

	req := httptest.NewRequest(http.MethodPatch, addRepoQueryParamT("/api/rules/config", ctx), bytes.NewReader(body))
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
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	update := rules.ConfigSettingsUpdate{
		PriorityMax: ptrInt(10), // Invalid: max is 4
	}
	body, _ := json.Marshal(update)

	req := httptest.NewRequest(http.MethodPatch, addRepoQueryParamT("/api/rules/config", ctx), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleUpdateConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestRouteRules_ListRules(t *testing.T) {
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	req := httptest.NewRequest(http.MethodGet, addRepoQueryParamT("/api/rules", ctx), nil)
	w := httptest.NewRecorder()

	handler.RouteRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRouteRules_AddRule(t *testing.T) {
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	reqBody := AddRuleRequest{
		Name:      "routed-rule",
		Condition: "priority > 2",
		Action:    "skip",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, addRepoQueryParamT("/api/rules", ctx), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.RouteRules(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouteRules_UpdateRule(t *testing.T) {
	daemon, engine, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	// First add a rule
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "routed-update",
		Condition: "priority > 2",
		Action:    "skip",
	})

	reqBody := UpdateRuleRequest{Enabled: ptrBool(false)}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, addRepoQueryParamT("/api/rules/routed-update", ctx), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.RouteRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouteRules_DeleteRule(t *testing.T) {
	daemon, engine, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	// First add a rule
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "routed-delete",
		Condition: "priority > 2",
		Action:    "skip",
	})

	req := httptest.NewRequest(http.MethodDelete, addRepoQueryParamT("/api/rules/routed-delete", ctx), nil)
	w := httptest.NewRecorder()

	handler.RouteRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouteRules_ConfigUpdate(t *testing.T) {
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	update := rules.ConfigSettingsUpdate{
		PriorityMax: ptrInt(1),
	}
	body, _ := json.Marshal(update)

	req := httptest.NewRequest(http.MethodPatch, addRepoQueryParamT("/api/rules/config", ctx), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.RouteRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRulesHandler_BroadcastsEvent(t *testing.T) {
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
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

	req := httptest.NewRequest(http.MethodPost, addRepoQueryParamT("/api/rules", ctx), bytes.NewReader(body))
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

func TestHandleReorderRule_Success(t *testing.T) {
	daemon, engine, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	// Add three rules
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "rule1",
		Condition: "priority > 1",
		Action:    "deny",
	})
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "rule2",
		Condition: "type == bug",
		Action:    "allow",
	})
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "rule3",
		Condition: "type == feature",
		Action:    "allow",
	})

	// Move rule1 to position 2 (last)
	reqBody := ReorderRuleRequest{Position: 2}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, addRepoQueryParamT("/api/rules/rule1/reorder", ctx), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleReorderRule(w, req, "rule1")

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response ReorderRuleResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Success {
		t.Errorf("expected success=true, got %v (error: %s)", response.Success, response.Error)
	}

	// Verify the order changed
	if len(response.Rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(response.Rules))
	}
	if response.Rules[0].Name != "rule2" {
		t.Errorf("expected first rule to be 'rule2', got %q", response.Rules[0].Name)
	}
	if response.Rules[1].Name != "rule3" {
		t.Errorf("expected second rule to be 'rule3', got %q", response.Rules[1].Name)
	}
	if response.Rules[2].Name != "rule1" {
		t.Errorf("expected third rule to be 'rule1', got %q", response.Rules[2].Name)
	}

	// Persisted should be false (rules are runtime rules, not from config)
	if response.Persisted {
		t.Error("expected persisted=false for runtime rules")
	}
}

func TestHandleReorderRule_NotFound(t *testing.T) {
	daemon, _, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	reqBody := ReorderRuleRequest{Position: 0}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, addRepoQueryParamT("/api/rules/nonexistent/reorder", ctx), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleReorderRule(w, req, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}

	var response ReorderRuleResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Success {
		t.Error("expected success=false")
	}
}

func TestHandleReorderRule_InvalidPosition(t *testing.T) {
	daemon, engine, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	// Add a rule
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "rule1",
		Condition: "priority > 1",
		Action:    "deny",
	})

	reqBody := ReorderRuleRequest{Position: 5} // Invalid position
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, addRepoQueryParamT("/api/rules/rule1/reorder", ctx), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleReorderRule(w, req, "rule1")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}

	var response ReorderRuleResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Success {
		t.Error("expected success=false")
	}
}

func TestHandleReorderRule_InvalidJSON(t *testing.T) {
	daemon, engine, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	// Add a rule
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "rule1",
		Condition: "priority > 1",
		Action:    "deny",
	})

	req := httptest.NewRequest(http.MethodPost, addRepoQueryParamT("/api/rules/rule1/reorder", ctx), bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleReorderRule(w, req, "rule1")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouteRules_ReorderRule(t *testing.T) {
	daemon, engine, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	// Add rules
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "routed-reorder1",
		Condition: "priority > 1",
		Action:    "deny",
	})
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "routed-reorder2",
		Condition: "type == bug",
		Action:    "allow",
	})

	reqBody := ReorderRuleRequest{Position: 0}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, addRepoQueryParamT("/api/rules/routed-reorder2/reorder", ctx), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.RouteRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouteRules_ReorderRule_MethodNotAllowed(t *testing.T) {
	daemon, engine, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	// Add a rule
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "method-test",
		Condition: "priority > 1",
		Action:    "deny",
	})

	// Try GET instead of POST
	req := httptest.NewRequest(http.MethodGet, addRepoQueryParamT("/api/rules/method-test/reorder", ctx), nil)
	w := httptest.NewRecorder()

	handler.RouteRules(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleReorderRule_BroadcastsEvent(t *testing.T) {
	daemon, engine, ctx := setupTestDaemonWithRulesT(t)
	handler := NewRulesHandler(daemon)

	// Add rules
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "event-rule1",
		Condition: "priority > 1",
		Action:    "deny",
	})
	_ = engine.AddRuleWithValidation(config.CustomRule{
		Name:      "event-rule2",
		Condition: "type == bug",
		Action:    "allow",
	})

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

	reqBody := ReorderRuleRequest{Position: 0}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, addRepoQueryParamT("/api/rules/event-rule2/reorder", ctx), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleReorderRule(w, req, "event-rule2")

	if !eventReceived {
		t.Error("expected rules:changed event to be broadcast")
	}

	if receivedAction != "reordered" {
		t.Errorf("expected action 'reordered', got '%s'", receivedAction)
	}
}

// Test rules panel without active run - standalone engine case
func TestHandleListRules_NoActiveRun(t *testing.T) {
	// Create a temp directory to use as repo path
	tmpDir := t.TempDir()

	daemon := newDaemonForTest(Config{}, nil, nil)
	daemon.Init()
	handler := NewRulesHandler(daemon)

	// Request rules for a repo with no active run - should work
	req := httptest.NewRequest(http.MethodGet, "/api/rules?repo_path="+tmpDir, nil)
	w := httptest.NewRecorder()

	handler.HandleListRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response RulesResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should get default settings
	if response.Settings == nil {
		t.Error("expected Settings to be non-nil")
	}
}

func TestHandleAddRule_NoActiveRun(t *testing.T) {
	tmpDir := t.TempDir()

	daemon := newDaemonForTest(Config{}, nil, nil)
	daemon.Init()
	handler := NewRulesHandler(daemon)

	reqBody := AddRuleRequest{
		Name:      "standalone-rule",
		Condition: "priority > 2",
		Action:    "deny",
		Reason:    "test reason",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/rules?repo_path="+tmpDir, bytes.NewReader(body))
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

	if response.Rule.Name != "standalone-rule" {
		t.Errorf("expected rule name 'standalone-rule', got %s", response.Rule.Name)
	}
}

func TestStandaloneEngineReusedAcrossRequests(t *testing.T) {
	tmpDir := t.TempDir()

	daemon := newDaemonForTest(Config{}, nil, nil)
	daemon.Init()
	handler := NewRulesHandler(daemon)

	// First request: add a rule
	reqBody := AddRuleRequest{
		Name:      "persisted-across-requests",
		Condition: "priority > 1",
		Action:    "deny",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/rules?repo_path="+tmpDir, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.HandleAddRule(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	// Second request: list rules - should see the rule we added
	req = httptest.NewRequest(http.MethodGet, "/api/rules?repo_path="+tmpDir, nil)
	w = httptest.NewRecorder()
	handler.HandleListRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response RulesResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should see the rule we added
	found := false
	for _, rule := range response.Rules {
		if rule.Name == "persisted-across-requests" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'persisted-across-requests' rule in second request")
	}
}

func TestStandaloneEngineInvalidatedOnRunStart(t *testing.T) {
	tmpDir := t.TempDir()

	daemon := newDaemonForTest(Config{}, nil, nil)
	daemon.Init()

	// Create a standalone engine by accessing rules
	engine1, err := daemon.orchManager.GetOrCreateRulesEngineForRepo(tmpDir)
	if err != nil {
		t.Fatalf("failed to create standalone engine: %v", err)
	}

	// Add a rule to the standalone engine
	_ = engine1.AddRuleWithValidation(config.CustomRule{
		Name:      "standalone-only-rule",
		Condition: "priority > 0",
		Action:    "deny",
	})

	// Simulate invalidation (what happens when a run starts)
	daemon.orchManager.InvalidateStandaloneEngine(tmpDir)

	// Get engine again - should be a fresh one without our rule
	engine2, err := daemon.orchManager.GetOrCreateRulesEngineForRepo(tmpDir)
	if err != nil {
		t.Fatalf("failed to create engine after invalidation: %v", err)
	}

	// The new engine should not have the rule we added
	rule := engine2.GetRule("standalone-only-rule")
	if rule != nil {
		t.Error("expected new engine to not have the rule from invalidated engine")
	}

	// engine1 and engine2 should be different instances
	if engine1 == engine2 {
		t.Error("expected different engine instances after invalidation")
	}
}

// Helper functions
func ptrBool(b bool) *bool {
	return &b
}

func ptrInt(i int) *int {
	return &i
}
