package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jzila/canopy/pkg/config"
	"github.com/jzila/canopy/pkg/orchestrator"
)

// mockRepoAPI implements the subset of orchestrator.RepoAPI needed for agent config tests.
type mockRepoAPIForConfig struct {
	orchestrator.RepoAPI // embed to satisfy interface; panics on unimplemented methods

	snapshot    *orchestrator.AgentConfigSnapshot
	getErr     error
	updateErr  error
	persistErr error
	configPath string

	lastUpdate orchestrator.AgentConfigUpdate
}

func (m *mockRepoAPIForConfig) GetAgentConfig(_ context.Context) (*orchestrator.AgentConfigSnapshot, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.snapshot, nil
}

func (m *mockRepoAPIForConfig) UpdateAgentConfig(_ context.Context, update orchestrator.AgentConfigUpdate) error {
	m.lastUpdate = update
	if m.updateErr != nil {
		return m.updateErr
	}
	if m.snapshot == nil {
		return fmt.Errorf("snapshot is nil")
	}
	// Apply update to snapshot so subsequent GetAgentConfig reflects changes
	if update.DefaultModel != nil {
		m.snapshot.Settings.DefaultModel = *update.DefaultModel
	}
	if update.Worker != nil {
		applyUpdate(&m.snapshot.Settings.Worker, update.Worker)
	}
	if update.Resolver != nil {
		applyUpdate(&m.snapshot.Settings.Resolver, update.Resolver)
	}
	if update.Repair != nil {
		applyUpdate(&m.snapshot.Settings.Repair, update.Repair)
	}
	m.snapshot.Persisted = false
	return nil
}

func (m *mockRepoAPIForConfig) PersistAgentConfig(_ context.Context) (string, error) {
	if m.persistErr != nil {
		return "", m.persistErr
	}
	m.snapshot.Persisted = true
	return m.configPath, nil
}

func applyUpdate(s *config.AgentTypeSettings, u *orchestrator.AgentTypeSettingsUpdate) {
	if u.Model != nil {
		s.Model = *u.Model
	}
	if u.Enabled != nil {
		s.Enabled = u.Enabled
	}
	if u.Timeout != nil {
		s.Timeout = *u.Timeout
	}
}

// newTestHandlerWithMock creates an AgentConfigHandler with a mock RepoAPI injected.
func newTestHandlerWithMock(mock *mockRepoAPIForConfig) *AgentConfigHandler {
	return &AgentConfigHandler{
		repoAPIOverride: mock,
	}
}

func TestAgentConfigJSONCasing(t *testing.T) {
	t.Run("GET response uses snake_case keys", func(t *testing.T) {
		resp := AgentConfigResponse{
			DefaultModel: "claude-3-opus",
			Worker: config.AgentTypeSettings{
				Model:   "claude-3-haiku",
				Timeout: "10m",
			},
			Resolver: config.AgentTypeSettings{
				Model: "claude-3-sonnet",
			},
			Repair: config.AgentTypeSettings{},
			Persisted: true,
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("Failed to unmarshal to map: %v", err)
		}

		expectedKeys := []string{"default_model", "worker", "resolver", "repair", "persisted"}
		for _, key := range expectedKeys {
			if _, ok := raw[key]; !ok {
				t.Errorf("Expected snake_case key %q in JSON, got keys: %v", key, keys(raw))
			}
		}

		// Verify no camelCase keys
		badKeys := []string{"defaultModel", "DefaultModel"}
		for _, key := range badKeys {
			if _, ok := raw[key]; ok {
				t.Errorf("Unexpected camelCase key %q in JSON response", key)
			}
		}
	})

	t.Run("POST request accepts snake_case keys", func(t *testing.T) {
		jsonBody := `{
			"default_model": "claude-3-opus",
			"worker": {"model": "claude-3-haiku", "enabled": true, "timeout": "15m"},
			"resolver": {"model": "claude-3-sonnet"},
			"repair": {"enabled": false}
		}`

		var req AgentConfigUpdateRequest
		if err := json.Unmarshal([]byte(jsonBody), &req); err != nil {
			t.Fatalf("Failed to unmarshal snake_case request: %v", err)
		}

		if req.DefaultModel == nil || *req.DefaultModel != "claude-3-opus" {
			t.Errorf("default_model not parsed: got %v", req.DefaultModel)
		}
		if req.Worker == nil {
			t.Fatal("worker not parsed")
		}
		if req.Worker.Model == nil || *req.Worker.Model != "claude-3-haiku" {
			t.Errorf("worker.model not parsed: got %v", req.Worker.Model)
		}
		if req.Worker.Enabled == nil || *req.Worker.Enabled != true {
			t.Errorf("worker.enabled not parsed: got %v", req.Worker.Enabled)
		}
		if req.Worker.Timeout == nil || *req.Worker.Timeout != "15m" {
			t.Errorf("worker.timeout not parsed: got %v", req.Worker.Timeout)
		}
		if req.Resolver == nil || req.Resolver.Model == nil || *req.Resolver.Model != "claude-3-sonnet" {
			t.Errorf("resolver.model not parsed")
		}
		if req.Repair == nil || req.Repair.Enabled == nil || *req.Repair.Enabled != false {
			t.Errorf("repair.enabled not parsed")
		}
	})

	t.Run("POST response uses snake_case keys", func(t *testing.T) {
		resp := AgentConfigUpdateResponse{
			Success: true,
			Settings: &AgentConfigResponse{
				DefaultModel: "claude-3-opus",
				Worker:       config.AgentTypeSettings{Model: "claude-3-haiku"},
				Persisted:    false,
			},
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if _, ok := raw["success"]; !ok {
			t.Error("Expected 'success' key")
		}
		if _, ok := raw["settings"]; !ok {
			t.Error("Expected 'settings' key")
		}

		// Check nested settings
		var settings map[string]json.RawMessage
		if err := json.Unmarshal(raw["settings"], &settings); err != nil {
			t.Fatalf("Failed to unmarshal settings: %v", err)
		}
		if _, ok := settings["default_model"]; !ok {
			t.Errorf("Expected 'default_model' in settings, got keys: %v", keys(settings))
		}
	})

	t.Run("persist response uses snake_case keys", func(t *testing.T) {
		resp := AgentConfigPersistResponse{
			Success:    true,
			ConfigPath: "/path/to/config.toml",
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if _, ok := raw["config_path"]; !ok {
			t.Errorf("Expected 'config_path' key, got: %v", keys(raw))
		}
		if _, ok := raw["configPath"]; ok {
			t.Error("Unexpected camelCase 'configPath' key")
		}
	})
}

func TestAgentConfigPartialUpdate(t *testing.T) {
	t.Run("only default_model field is applied", func(t *testing.T) {
		jsonBody := `{"default_model": "claude-3-opus"}`
		var req AgentConfigUpdateRequest
		if err := json.Unmarshal([]byte(jsonBody), &req); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if req.DefaultModel == nil || *req.DefaultModel != "claude-3-opus" {
			t.Error("default_model should be set")
		}
		if req.Worker != nil {
			t.Error("worker should be nil for partial update")
		}
		if req.Resolver != nil {
			t.Error("resolver should be nil for partial update")
		}
		if req.Repair != nil {
			t.Error("repair should be nil for partial update")
		}
	})

	t.Run("only worker model is applied", func(t *testing.T) {
		jsonBody := `{"worker": {"model": "claude-3-haiku"}}`
		var req AgentConfigUpdateRequest
		if err := json.Unmarshal([]byte(jsonBody), &req); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if req.DefaultModel != nil {
			t.Error("default_model should be nil")
		}
		if req.Worker == nil {
			t.Fatal("worker should not be nil")
		}
		if req.Worker.Model == nil || *req.Worker.Model != "claude-3-haiku" {
			t.Error("worker.model should be set")
		}
		if req.Worker.Enabled != nil {
			t.Error("worker.enabled should be nil for partial update")
		}
		if req.Worker.Timeout != nil {
			t.Error("worker.timeout should be nil for partial update")
		}
	})

	t.Run("empty body applies no changes", func(t *testing.T) {
		jsonBody := `{}`
		var req AgentConfigUpdateRequest
		if err := json.Unmarshal([]byte(jsonBody), &req); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if req.DefaultModel != nil || req.Worker != nil || req.Resolver != nil || req.Repair != nil {
			t.Error("empty body should result in all nil fields")
		}
	})

	t.Run("enabled false is preserved (not treated as nil)", func(t *testing.T) {
		jsonBody := `{"worker": {"enabled": false}}`
		var req AgentConfigUpdateRequest
		if err := json.Unmarshal([]byte(jsonBody), &req); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if req.Worker == nil {
			t.Fatal("worker should not be nil")
		}
		if req.Worker.Enabled == nil {
			t.Fatal("worker.enabled should not be nil - false must be preserved")
		}
		if *req.Worker.Enabled != false {
			t.Error("worker.enabled should be false")
		}
	})
}

func TestAgentConfigRouting(t *testing.T) {
	handler := &AgentConfigHandler{}

	t.Run("GET agents routes correctly", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/repos/r/config/agents", nil)
		w := httptest.NewRecorder()
		handler.RouteAgentConfig(w, req, nil)
		// Without a daemon, we expect a 400 (bad request) not 404/405
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for GET without daemon, got %d", w.Code)
		}
	})

	t.Run("POST agents routes correctly", func(t *testing.T) {
		body := bytes.NewBufferString(`{}`)
		req := httptest.NewRequest(http.MethodPost, "/api/repos/r/config/agents", body)
		w := httptest.NewRecorder()
		handler.RouteAgentConfig(w, req, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for POST without daemon, got %d", w.Code)
		}
	})

	t.Run("DELETE agents returns method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/repos/r/config/agents", nil)
		w := httptest.NewRecorder()
		handler.RouteAgentConfig(w, req, nil)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected 405, got %d", w.Code)
		}
	})

	t.Run("POST persist routes correctly", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/repos/r/config/agents/persist", nil)
		w := httptest.NewRecorder()
		handler.RouteAgentConfig(w, req, []string{"persist"})
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for POST persist without daemon, got %d", w.Code)
		}
	})

	t.Run("GET persist returns not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/repos/r/config/agents/persist", nil)
		w := httptest.NewRecorder()
		handler.RouteAgentConfig(w, req, []string{"persist"})
		if w.Code != http.StatusNotFound {
			t.Errorf("Expected 404, got %d", w.Code)
		}
	})

	t.Run("unknown suffix returns not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/repos/r/config/agents/unknown", nil)
		w := httptest.NewRecorder()
		handler.RouteAgentConfig(w, req, []string{"unknown"})
		if w.Code != http.StatusNotFound {
			t.Errorf("Expected 404, got %d", w.Code)
		}
	})
}

func TestAgentConfigRequestResponseRoundtrip(t *testing.T) {
	t.Run("request and response use matching key names", func(t *testing.T) {
		// Simulate what a frontend would send
		requestJSON := `{
			"default_model": "claude-3-opus",
			"worker": {"model": "claude-3-haiku", "enabled": true, "timeout": "10m"}
		}`

		// Parse request
		var req AgentConfigUpdateRequest
		if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
			t.Fatalf("Failed to parse request: %v", err)
		}

		// Build response as the handler would
		resp := AgentConfigUpdateResponse{
			Success: true,
			Settings: &AgentConfigResponse{
				DefaultModel: *req.DefaultModel,
				Worker: config.AgentTypeSettings{
					Model:   *req.Worker.Model,
					Enabled: req.Worker.Enabled,
					Timeout: *req.Worker.Timeout,
				},
				Persisted: false,
			},
		}

		// Marshal response
		respData, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("Failed to marshal response: %v", err)
		}

		// Verify the response can be parsed with the same key names as the request
		var respMap map[string]json.RawMessage
		if err := json.Unmarshal(respData, &respMap); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		var settingsMap map[string]json.RawMessage
		if err := json.Unmarshal(respMap["settings"], &settingsMap); err != nil {
			t.Fatalf("Failed to unmarshal settings: %v", err)
		}

		// The response key "default_model" must match request key "default_model"
		if _, ok := settingsMap["default_model"]; !ok {
			t.Error("Response settings missing 'default_model' - key mismatch with request")
		}

		// Verify worker settings match
		var workerMap map[string]json.RawMessage
		if err := json.Unmarshal(settingsMap["worker"], &workerMap); err != nil {
			t.Fatalf("Failed to unmarshal worker: %v", err)
		}

		for _, key := range []string{"model", "enabled", "timeout"} {
			if _, ok := workerMap[key]; !ok {
				t.Errorf("Response worker missing key %q", key)
			}
		}
	})
}

func TestWriteJSON(t *testing.T) {
	handler := &AgentConfigHandler{}

	t.Run("sets content type and status", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.writeJSON(w, http.StatusOK, map[string]string{"key": "value"})

		if w.Header().Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
		}
		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", w.Code)
		}
	})

	t.Run("error status codes work", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.writeJSON(w, http.StatusInternalServerError, AgentConfigResponse{Error: "something broke"})

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected 500, got %d", w.Code)
		}

		var resp AgentConfigResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode: %v", err)
		}
		if resp.Error != "something broke" {
			t.Errorf("Expected error message, got %q", resp.Error)
		}
	})
}

func TestBroadcastAgentConfigChanged(t *testing.T) {
	t.Run("nil eventBus does not panic", func(t *testing.T) {
		handler := &AgentConfigHandler{eventBus: nil}
		// Should not panic
		handler.broadcastAgentConfigChanged("updated")
	})
}

func TestHandlerGetAgentConfig(t *testing.T) {
	t.Run("success returns 200 with config", func(t *testing.T) {
		mock := &mockRepoAPIForConfig{
			snapshot: &orchestrator.AgentConfigSnapshot{
				Settings: config.AgentSettings{
					DefaultModel: "claude-3-opus",
					Worker:       config.AgentTypeSettings{Model: "claude-3-haiku"},
				},
				Persisted: true,
			},
		}
		handler := newTestHandlerWithMock(mock)

		req := httptest.NewRequest(http.MethodGet, "/api/repos/r/config/agents", nil)
		w := httptest.NewRecorder()
		handler.HandleGetAgentConfig(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp AgentConfigResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if resp.DefaultModel != "claude-3-opus" {
			t.Errorf("Expected default_model claude-3-opus, got %q", resp.DefaultModel)
		}
		if !resp.Persisted {
			t.Error("Expected persisted=true")
		}
	})

	t.Run("GetAgentConfig error returns 500", func(t *testing.T) {
		mock := &mockRepoAPIForConfig{
			getErr: fmt.Errorf("database unavailable"),
		}
		handler := newTestHandlerWithMock(mock)

		req := httptest.NewRequest(http.MethodGet, "/api/repos/r/config/agents", nil)
		w := httptest.NewRecorder()
		handler.HandleGetAgentConfig(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected 500, got %d", w.Code)
		}
	})
}

func TestHandlerUpdateAgentConfig(t *testing.T) {
	t.Run("success returns 200 with updated settings", func(t *testing.T) {
		mock := &mockRepoAPIForConfig{
			snapshot: &orchestrator.AgentConfigSnapshot{
				Settings: config.AgentSettings{
					DefaultModel: "claude-3-sonnet",
				},
				Persisted: true,
			},
		}
		handler := newTestHandlerWithMock(mock)

		body := bytes.NewBufferString(`{"default_model": "claude-3-opus"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/repos/r/config/agents", body)
		w := httptest.NewRecorder()
		handler.HandleUpdateAgentConfig(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp AgentConfigUpdateResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode: %v", err)
		}
		if !resp.Success {
			t.Error("Expected success=true")
		}
		if resp.Settings == nil || resp.Settings.DefaultModel != "claude-3-opus" {
			t.Errorf("Expected updated default_model, got %+v", resp.Settings)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		mock := &mockRepoAPIForConfig{
			snapshot: &orchestrator.AgentConfigSnapshot{},
		}
		handler := newTestHandlerWithMock(mock)

		body := bytes.NewBufferString(`{invalid json}`)
		req := httptest.NewRequest(http.MethodPost, "/api/repos/r/config/agents", body)
		w := httptest.NewRecorder()
		handler.HandleUpdateAgentConfig(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400, got %d", w.Code)
		}
	})

	t.Run("UpdateAgentConfig error returns 400", func(t *testing.T) {
		mock := &mockRepoAPIForConfig{
			snapshot:  &orchestrator.AgentConfigSnapshot{},
			updateErr: fmt.Errorf("invalid model"),
		}
		handler := newTestHandlerWithMock(mock)

		body := bytes.NewBufferString(`{"default_model": "bad"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/repos/r/config/agents", body)
		w := httptest.NewRecorder()
		handler.HandleUpdateAgentConfig(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400, got %d", w.Code)
		}
	})
}

func TestHandlerPersistAgentConfig(t *testing.T) {
	t.Run("success returns 200 with config path", func(t *testing.T) {
		mock := &mockRepoAPIForConfig{
			snapshot:   &orchestrator.AgentConfigSnapshot{},
			configPath: "/repo/.canopy/config.toml",
		}
		handler := newTestHandlerWithMock(mock)

		req := httptest.NewRequest(http.MethodPost, "/api/repos/r/config/agents/persist", nil)
		w := httptest.NewRecorder()
		handler.HandlePersistAgentConfig(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp AgentConfigPersistResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode: %v", err)
		}
		if !resp.Success {
			t.Error("Expected success=true")
		}
		if resp.ConfigPath != "/repo/.canopy/config.toml" {
			t.Errorf("Expected config path, got %q", resp.ConfigPath)
		}
	})

	t.Run("persist error returns 500", func(t *testing.T) {
		mock := &mockRepoAPIForConfig{
			snapshot:   &orchestrator.AgentConfigSnapshot{},
			persistErr: fmt.Errorf("write failed"),
		}
		handler := newTestHandlerWithMock(mock)

		req := httptest.NewRequest(http.MethodPost, "/api/repos/r/config/agents/persist", nil)
		w := httptest.NewRecorder()
		handler.HandlePersistAgentConfig(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected 500, got %d", w.Code)
		}
	})
}

func keys[V any](m map[string]V) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}
