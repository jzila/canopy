package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// createTestOrchestratorManager creates an OrchestratorManager for testing
func createTestOrchestratorManager() *OrchestratorManager {
	state := NewRuntimeState()
	eventBus := NewEventBus()
	return NewOrchestratorManager(eventBus, state)
}

// TestHandleListActiveRuns tests the GET /api/orchestrator/runs/active endpoint
func TestHandleListActiveRuns(t *testing.T) {
	manager := createTestOrchestratorManager()
	handler := NewOrchestrationHandler(manager)

	// Add some test runs directly to the manager
	manager.runs.Store("run-1", &RunState{
		ID:        "run-1",
		RepoPath:  "/path/to/repo1",
		Status:    RunStatusRunning,
		StartTime: time.Now(),
	})
	manager.runs.Store("run-2", &RunState{
		ID:        "run-2",
		RepoPath:  "/path/to/repo2",
		Status:    RunStatusCompleted,
		StartTime: time.Now().Add(-1 * time.Hour),
	})
	manager.runs.Store("run-3", &RunState{
		ID:        "run-3",
		RepoPath:  "/path/to/repo3",
		Status:    RunStatusPending,
		StartTime: time.Now(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/orchestrator/runs/active", nil)
	w := httptest.NewRecorder()

	handler.HandleListActiveRuns(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var response RunStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Success {
		t.Errorf("expected success=true, got false: %s", response.Error)
	}

	// Should only return active runs (running or pending)
	if len(response.Runs) != 2 {
		t.Errorf("expected 2 active runs, got %d", len(response.Runs))
	}

	// Verify the runs are the correct ones
	runIDs := make(map[string]bool)
	for _, run := range response.Runs {
		runIDs[run.ID] = true
	}

	if !runIDs["run-1"] {
		t.Error("expected run-1 (running) to be in active runs")
	}
	if !runIDs["run-3"] {
		t.Error("expected run-3 (pending) to be in active runs")
	}
	if runIDs["run-2"] {
		t.Error("did not expect run-2 (completed) to be in active runs")
	}
}

// TestHandleListActiveRuns_Empty tests behavior when there are no active runs
func TestHandleListActiveRuns_Empty(t *testing.T) {
	manager := createTestOrchestratorManager()
	handler := NewOrchestrationHandler(manager)

	req := httptest.NewRequest(http.MethodGet, "/api/orchestrator/runs/active", nil)
	w := httptest.NewRecorder()

	handler.HandleListActiveRuns(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var response RunStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Success {
		t.Errorf("expected success=true, got false: %s", response.Error)
	}

	if len(response.Runs) != 0 {
		t.Errorf("expected 0 active runs, got %d", len(response.Runs))
	}
}

// TestHandleListActiveRuns_NoManager tests behavior when manager is nil
func TestHandleListActiveRuns_NoManager(t *testing.T) {
	handler := NewOrchestrationHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/orchestrator/runs/active", nil)
	w := httptest.NewRecorder()

	handler.HandleListActiveRuns(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", resp.StatusCode)
	}
}

// TestHandleListActiveRuns_WrongMethod tests that non-GET methods are rejected
func TestHandleListActiveRuns_WrongMethod(t *testing.T) {
	manager := createTestOrchestratorManager()
	handler := NewOrchestrationHandler(manager)

	req := httptest.NewRequest(http.MethodPost, "/api/orchestrator/runs/active", nil)
	w := httptest.NewRecorder()

	handler.HandleListActiveRuns(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

// TestHandleUpdateRunConfig tests the PATCH /api/orchestrator/runs/:id/config endpoint
func TestHandleUpdateRunConfig(t *testing.T) {
	manager := createTestOrchestratorManager()
	handler := NewOrchestrationHandler(manager)

	// Add a test run directly to the manager
	manager.runs.Store("run-1", &RunState{
		ID:        "run-1",
		RepoPath:  "/path/to/repo1",
		Status:    RunStatusRunning,
		StartTime: time.Now(),
		Config: RunConfig{
			Concurrency: 4,
		},
	})

	concurrency := 8
	reqBody := UpdateRunConfigRequest{
		Concurrency: &concurrency,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, "/api/orchestrator/runs/run-1/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleUpdateRunConfig(w, req, "run-1")

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var response UpdateRunConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Success {
		t.Errorf("expected success=true, got false: %s", response.Error)
	}

	// Verify the config was updated
	runStateI, _ := manager.runs.Load("run-1")
	runState := runStateI.(*RunState)
	if runState.Config.Concurrency != 8 {
		t.Errorf("expected concurrency=8, got %d", runState.Config.Concurrency)
	}
}

// TestHandleUpdateRunConfig_NotFound tests behavior when run is not found
func TestHandleUpdateRunConfig_NotFound(t *testing.T) {
	manager := createTestOrchestratorManager()
	handler := NewOrchestrationHandler(manager)

	reqBody := UpdateRunConfigRequest{}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, "/api/orchestrator/runs/nonexistent/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleUpdateRunConfig(w, req, "nonexistent")

	resp := w.Result()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}
}

// TestHandleUpdateRunConfig_InvalidJSON tests behavior with invalid JSON body
func TestHandleUpdateRunConfig_InvalidJSON(t *testing.T) {
	manager := createTestOrchestratorManager()
	handler := NewOrchestrationHandler(manager)

	manager.runs.Store("run-1", &RunState{
		ID:        "run-1",
		RepoPath:  "/path/to/repo1",
		Status:    RunStatusRunning,
		StartTime: time.Now(),
	})

	req := httptest.NewRequest(http.MethodPatch, "/api/orchestrator/runs/run-1/config", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleUpdateRunConfig(w, req, "run-1")

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

// TestHandleUpdateRunConfig_WrongMethod tests that non-PATCH methods are rejected
func TestHandleUpdateRunConfig_WrongMethod(t *testing.T) {
	manager := createTestOrchestratorManager()
	handler := NewOrchestrationHandler(manager)

	req := httptest.NewRequest(http.MethodGet, "/api/orchestrator/runs/run-1/config", nil)
	w := httptest.NewRecorder()

	handler.HandleUpdateRunConfig(w, req, "run-1")

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

// TestRouteOrchestrator_ActiveRuns tests routing to the active runs endpoint
func TestRouteOrchestrator_ActiveRuns(t *testing.T) {
	manager := createTestOrchestratorManager()
	handler := NewOrchestrationHandler(manager)

	req := httptest.NewRequest(http.MethodGet, "/api/orchestrator/runs/active", nil)
	w := httptest.NewRecorder()

	handler.RouteOrchestrator(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestRouteOrchestrator_UpdateConfig tests routing to the config update endpoint
func TestRouteOrchestrator_UpdateConfig(t *testing.T) {
	manager := createTestOrchestratorManager()
	handler := NewOrchestrationHandler(manager)

	manager.runs.Store("run-1", &RunState{
		ID:        "run-1",
		RepoPath:  "/path/to/repo1",
		Status:    RunStatusRunning,
		StartTime: time.Now(),
	})

	reqBody := UpdateRunConfigRequest{}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, "/api/orchestrator/runs/run-1/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.RouteOrchestrator(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestRouteOrchestrator_NotFound tests 404 for unknown routes
func TestRouteOrchestrator_NotFound(t *testing.T) {
	manager := createTestOrchestratorManager()
	handler := NewOrchestrationHandler(manager)

	req := httptest.NewRequest(http.MethodGet, "/api/orchestrator/unknown", nil)
	w := httptest.NewRecorder()

	handler.RouteOrchestrator(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}
