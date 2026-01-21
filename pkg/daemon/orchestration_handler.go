package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jzila/canopy/pkg/logging"
)

// OrchestrationHandler handles HTTP requests for orchestration control.
type OrchestrationHandler struct {
	manager *OrchestratorManager
}

// NewOrchestrationHandler creates a new OrchestrationHandler.
func NewOrchestrationHandler(manager *OrchestratorManager) *OrchestrationHandler {
	return &OrchestrationHandler{
		manager: manager,
	}
}

// ExecuteRunRequest is the JSON request body for starting a run.
type ExecuteRunRequest struct {
	WorkDir           string `json:"work_dir"`
	OutputDir         string `json:"output_dir,omitempty"`
	Concurrency       int    `json:"concurrency,omitempty"`
	Verbose           bool   `json:"verbose,omitempty"`
	DryRun            bool   `json:"dry_run,omitempty"`
	UseBwrap          bool   `json:"use_bwrap,omitempty"`
	MaxRetries        int    `json:"max_retries,omitempty"`
	MaxPriority       int    `json:"max_priority,omitempty"`
	ResolverTimeoutMS int64  `json:"resolver_timeout_ms,omitempty"`
	RepoID            string `json:"repo_id,omitempty"`
}

// ExecuteRunResponse is the JSON response for starting a run.
type ExecuteRunResponse struct {
	Success bool   `json:"success"`
	RunID   string `json:"run_id,omitempty"`
	Error   string `json:"error,omitempty"`
}

// StopRunRequest is the JSON request body for stopping a run.
type StopRunRequest struct {
	RunID string `json:"run_id"`
}

// StopRunResponse is the JSON response for stopping a run.
type StopRunResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// RunStatusResponse is the JSON response for run status queries.
type RunStatusResponse struct {
	Success bool             `json:"success"`
	Run     *RunStatusWire   `json:"run,omitempty"`
	Runs    []RunStatusWire  `json:"runs,omitempty"`
	Error   string           `json:"error,omitempty"`
}

// RunStatusWire is the wire format for run status.
type RunStatusWire struct {
	ID          string `json:"id"`
	RepoPath    string `json:"repo_path"`
	RepoID      string `json:"repo_id,omitempty"`
	Status      string `json:"status"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time,omitempty"`
	Error       string `json:"error,omitempty"`
	TasksTotal  int    `json:"tasks_total"`
	TasksDone   int    `json:"tasks_done"`
	TasksFailed int    `json:"tasks_failed"`
}

// HandleExecuteRun handles POST /api/orchestrator/run requests.
func (h *OrchestrationHandler) HandleExecuteRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.manager == nil {
		writeJSON(w, http.StatusServiceUnavailable, ExecuteRunResponse{
			Success: false,
			Error:   "orchestration not available",
		})
		return
	}

	var req ExecuteRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ExecuteRunResponse{
			Success: false,
			Error:   "invalid request body: " + err.Error(),
		})
		return
	}

	// Convert request to RunConfig
	config := RunConfig{
		WorkDir:         req.WorkDir,
		OutputDir:       req.OutputDir,
		Concurrency:     req.Concurrency,
		Verbose:         req.Verbose,
		DryRun:          req.DryRun,
		UseBwrap:        req.UseBwrap,
		MaxRetries:      req.MaxRetries,
		MaxPriority:     req.MaxPriority,
		ResolverTimeout: time.Duration(req.ResolverTimeoutMS) * time.Millisecond,
		RepoID:          req.RepoID,
	}

	// Start the run with a background context (not tied to request)
	runID, err := h.manager.StartRun(context.Background(), config)
	if err != nil {
		logging.Warn("failed to start orchestration run",
			"work_dir", req.WorkDir,
			"error", err)

		writeJSON(w, http.StatusInternalServerError, ExecuteRunResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	logging.Info("started orchestration run via HTTP",
		"run_id", runID,
		"work_dir", req.WorkDir)

	writeJSON(w, http.StatusOK, ExecuteRunResponse{
		Success: true,
		RunID:   runID,
	})
}

// HandleStopRun handles POST /api/orchestrator/run/stop requests.
func (h *OrchestrationHandler) HandleStopRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.manager == nil {
		writeJSON(w, http.StatusServiceUnavailable, StopRunResponse{
			Success: false,
			Error:   "orchestration not available",
		})
		return
	}

	var req StopRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, StopRunResponse{
			Success: false,
			Error:   "invalid request body: " + err.Error(),
		})
		return
	}

	if req.RunID == "" {
		writeJSON(w, http.StatusBadRequest, StopRunResponse{
			Success: false,
			Error:   "run_id is required",
		})
		return
	}

	err := h.manager.StopRun(req.RunID)
	if err != nil {
		logging.Warn("failed to stop orchestration run",
			"run_id", req.RunID,
			"error", err)

		writeJSON(w, http.StatusInternalServerError, StopRunResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	logging.Info("stopped orchestration run via HTTP", "run_id", req.RunID)

	writeJSON(w, http.StatusOK, StopRunResponse{
		Success: true,
	})
}

// HandleGetRunStatus handles GET /api/orchestrator/run/:id requests.
func (h *OrchestrationHandler) HandleGetRunStatus(w http.ResponseWriter, r *http.Request, runID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.manager == nil {
		writeJSON(w, http.StatusServiceUnavailable, RunStatusResponse{
			Success: false,
			Error:   "orchestration not available",
		})
		return
	}

	runState, err := h.manager.GetRunStatus(runID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, RunStatusResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, RunStatusResponse{
		Success: true,
		Run:     runStateToWire(runState),
	})
}

// HandleListRuns handles GET /api/orchestrator/runs requests.
func (h *OrchestrationHandler) HandleListRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.manager == nil {
		writeJSON(w, http.StatusServiceUnavailable, RunStatusResponse{
			Success: false,
			Error:   "orchestration not available",
		})
		return
	}

	runs := h.manager.ListRuns()
	wireRuns := make([]RunStatusWire, 0, len(runs))
	for _, run := range runs {
		wireRuns = append(wireRuns, *runStateToWire(run))
	}

	writeJSON(w, http.StatusOK, RunStatusResponse{
		Success: true,
		Runs:    wireRuns,
	})
}

// runStateToWire converts RunState to wire format.
func runStateToWire(rs *RunState) *RunStatusWire {
	if rs == nil {
		return nil
	}

	wire := &RunStatusWire{
		ID:          rs.ID,
		RepoPath:    rs.RepoPath,
		RepoID:      rs.RepoID,
		Status:      string(rs.Status),
		StartTime:   rs.StartTime.Unix(),
		Error:       rs.Error,
		TasksTotal:  rs.TasksTotal,
		TasksDone:   rs.TasksDone,
		TasksFailed: rs.TasksFailed,
	}

	if rs.EndTime != nil {
		wire.EndTime = rs.EndTime.Unix()
	}

	return wire
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logging.Error("failed to encode JSON response", "error", err)
	}
}

// RouteOrchestrator routes orchestrator-related HTTP requests.
// Called from server.go for /api/orchestrator/* paths.
func (h *OrchestrationHandler) RouteOrchestrator(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// POST /api/orchestrator/run - start a new run
	if path == "/api/orchestrator/run" && r.Method == http.MethodPost {
		h.HandleExecuteRun(w, r)
		return
	}

	// POST /api/orchestrator/run/stop - stop a run
	if path == "/api/orchestrator/run/stop" && r.Method == http.MethodPost {
		h.HandleStopRun(w, r)
		return
	}

	// GET /api/orchestrator/runs - list all runs
	if path == "/api/orchestrator/runs" && r.Method == http.MethodGet {
		h.HandleListRuns(w, r)
		return
	}

	// GET /api/orchestrator/runs/:id - get specific run status
	if strings.HasPrefix(path, "/api/orchestrator/runs/") && r.Method == http.MethodGet {
		parts := strings.Split(strings.TrimPrefix(path, "/api/orchestrator/runs/"), "/")
		if len(parts) >= 1 && parts[0] != "" {
			h.HandleGetRunStatus(w, r, parts[0])
			return
		}
	}

	http.Error(w, "Not found", http.StatusNotFound)
}
