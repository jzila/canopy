package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Handler wraps RuntimeState and provides HTTP handlers
type Handler struct {
	state      *RuntimeState
	scheduler  SchedulerInterface
	beadsClient BeadsClientInterface
}

// SchedulerInterface abstracts scheduler operations for handlers
type SchedulerInterface interface {
	Pause()
	Resume()
	IsPaused() bool
	Kill(agentID string) error
}

// BeadsClientInterface abstracts beads client operations for handlers
type BeadsClientInterface interface {
	Create(title string, priority int) (string, error)
	Start(taskID string) error
	Done(taskID string) error
	Fail(taskID string, reason string) error
	AddDep(child, parent string) error
}

// NewHandler creates a new handler with the given state
func NewHandler(state *RuntimeState, scheduler SchedulerInterface, beadsClient BeadsClientInterface) *Handler {
	return &Handler{
		state:      state,
		scheduler:  scheduler,
		beadsClient: beadsClient,
	}
}

// HandleGetState returns the current RuntimeState as JSON
func (h *Handler) HandleGetState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot := h.state.GetSnapshot()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snapshot); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode state: %v", err), http.StatusInternalServerError)
		return
	}
}

// HandleGetAgents returns the list of agents
func (h *Handler) HandleGetAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot := h.state.GetSnapshot()

	// Convert map to slice for easier consumption
	agents := make([]*AgentState, 0, len(snapshot.Agents))
	for _, agent := range snapshot.Agents {
		agents = append(agents, agent)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(agents); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode agents: %v", err), http.StatusInternalServerError)
		return
	}
}

// HandlePostAgents creates a new agent (placeholder for future implementation)
func (h *Handler) HandlePostAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Future: Parse request body and create agent
	http.Error(w, "Agent creation not yet implemented", http.StatusNotImplemented)
}

// HandleKillAgent terminates a specific agent by ID
func (h *Handler) HandleKillAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract agent ID from path: /api/agents/:id/kill
	path := r.URL.Path
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "agents" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	agentID := parts[2]
	if agentID == "" {
		http.Error(w, "Agent ID required", http.StatusBadRequest)
		return
	}

	// Check if agent exists
	agent := h.state.GetAgent(agentID)
	if agent == nil {
		http.Error(w, fmt.Sprintf("Agent %s not found", agentID), http.StatusNotFound)
		return
	}

	// Kill the agent via scheduler
	if err := h.scheduler.Kill(agentID); err != nil {
		http.Error(w, fmt.Sprintf("Failed to kill agent: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "killed",
		"agent_id": agentID,
	})
}

// HandleGetTasks returns the list of tasks
func (h *Handler) HandleGetTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot := h.state.GetSnapshot()

	// Convert map to slice for easier consumption
	tasks := make([]*TaskState, 0, len(snapshot.Tasks))
	for _, task := range snapshot.Tasks {
		tasks = append(tasks, task)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tasks); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode tasks: %v", err), http.StatusInternalServerError)
		return
	}
}

// TaskCreateRequest represents a request to create a new task
type TaskCreateRequest struct {
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	Priority     int      `json:"priority,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// HandleCreateTask creates a new task
func (h *Handler) HandleCreateTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req TaskCreateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if req.Title == "" {
		http.Error(w, "Title is required", http.StatusBadRequest)
		return
	}

	// Create task via beads client
	taskID, err := h.beadsClient.Create(req.Title, req.Priority)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create task: %v", err), http.StatusInternalServerError)
		return
	}

	// Add dependencies if specified
	for _, depID := range req.Dependencies {
		if err := h.beadsClient.AddDep(taskID, depID); err != nil {
			http.Error(w, fmt.Sprintf("Failed to add dependency: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Return the created task
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"id": taskID,
		"title": req.Title,
		"status": "created",
	})
}

// TaskUpdateRequest represents a request to update a task
type TaskUpdateRequest struct {
	Status string `json:"status,omitempty"`
}

// HandleUpdateTask updates an existing task
func (h *Handler) HandleUpdateTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract task ID from query parameter
	taskID := r.URL.Query().Get("id")
	if taskID == "" {
		http.Error(w, "Task ID required", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req TaskUpdateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Update task status in runtime state
	if req.Status != "" {
		h.state.UpdateTaskStatus(taskID, req.Status, "")
	}

	// Also update in beads if applicable
	if req.Status == "in_progress" {
		if err := h.beadsClient.Start(taskID); err != nil {
			http.Error(w, fmt.Sprintf("Failed to update task in beads: %v", err), http.StatusInternalServerError)
			return
		}
	} else if req.Status == "completed" {
		if err := h.beadsClient.Done(taskID); err != nil {
			http.Error(w, fmt.Sprintf("Failed to update task in beads: %v", err), http.StatusInternalServerError)
			return
		}
	} else if req.Status == "failed" {
		if err := h.beadsClient.Fail(taskID, "Task failed"); err != nil {
			http.Error(w, fmt.Sprintf("Failed to update task in beads: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"id": taskID,
		"status": req.Status,
	})
}

// HandlePauseOrch pauses the orchestrator
func (h *Handler) HandlePauseOrch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.scheduler.Pause()
	h.state.Pause()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "paused",
	})
}

// HandleResumeOrch resumes the orchestrator
func (h *Handler) HandleResumeOrch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.scheduler.Resume()
	h.state.Resume()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "resumed",
	})
}

// HandleGetStats returns aggregate statistics
func (h *Handler) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Update stats before returning
	h.state.UpdateStats()

	snapshot := h.state.GetSnapshot()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snapshot.Stats); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode stats: %v", err), http.StatusInternalServerError)
		return
	}
}
