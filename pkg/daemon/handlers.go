package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/persistence"
)

// DaemonInterface abstracts daemon operations for handlers
type DaemonInterface interface {
	GetActiveRepositoryID() string
}

// MergeQueueInterface abstracts merge queue operations for handlers
type MergeQueueInterface interface {
	Pause()
	Resume()
	IsPaused() bool
	IsPausedByUser() bool
	PauseStateString() string
}

// Handler wraps RuntimeState and provides HTTP handlers
type Handler struct {
	state            *RuntimeState
	scheduler        SchedulerInterface
	beadsClient      BeadsClientInterface
	eventBus         *EventBus
	daemon           DaemonInterface
	persistenceStore PersistenceStoreInterface
	mergeQueue       MergeQueueInterface
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
	Create(ctx context.Context, title string, priority int) (string, error)
	Start(ctx context.Context, taskID string) error
	Done(ctx context.Context, taskID string) error
	Fail(ctx context.Context, taskID string, reason string) error
	AddDep(ctx context.Context, child, parent string) error
	List(ctx context.Context) ([]beads.Task, error)
	Show(ctx context.Context, taskID string) (*beads.Task, error)
}

// NewHandler creates a new handler with the given state
func NewHandler(state *RuntimeState, scheduler SchedulerInterface, beadsClient BeadsClientInterface, eventBus *EventBus) *Handler {
	return &Handler{
		state:       state,
		scheduler:   scheduler,
		beadsClient: beadsClient,
		eventBus:    eventBus,
	}
}

// SetDaemon sets the daemon reference for handlers that need access to daemon state
func (h *Handler) SetDaemon(daemon DaemonInterface) {
	h.daemon = daemon
}

// SetPersistenceStore sets the persistence store for handlers that need to persist state
func (h *Handler) SetPersistenceStore(store PersistenceStoreInterface) {
	h.persistenceStore = store
}

// SetMergeQueue sets the merge queue reference for pause/resume operations
func (h *Handler) SetMergeQueue(mq MergeQueueInterface) {
	h.mergeQueue = mq
}

// StateResponse wraps RuntimeState with additional daemon-level information
type StateResponse struct {
	RuntimeState
	ActiveRepoID string `json:"active_repo_id,omitempty"`
}

// HandleGetState returns the current RuntimeState as JSON
func (h *Handler) HandleGetState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get active repository ID if available
	var activeRepoID string
	if h.daemon != nil {
		activeRepoID = h.daemon.GetActiveRepositoryID()
	}

	// Get full snapshot (don't filter tasks by repo - tasks may have been loaded
	// with a different or empty RepoID, and filtering would hide them from the UI)
	snapshot := h.state.GetSnapshot()

	// Wrap snapshot with additional daemon-level state
	response := StateResponse{
		RuntimeState: snapshot,
	}

	// Include active_repo_id in response
	if activeRepoID != "" {
		response.ActiveRepoID = activeRepoID
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
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

	// Check if scheduler is available
	if h.scheduler == nil {
		http.Error(w, "Scheduler not available", http.StatusNotImplemented)
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

	// Check if beads client is available
	if h.beadsClient == nil {
		http.Error(w, "Beads client not available", http.StatusNotImplemented)
		return
	}

	// Create task via beads client
	taskID, err := h.beadsClient.Create(r.Context(), req.Title, req.Priority)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create task: %v", err), http.StatusInternalServerError)
		return
	}

	// Add dependencies if specified
	for _, depID := range req.Dependencies {
		if err := h.beadsClient.AddDep(r.Context(), taskID, depID); err != nil {
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
	Status   string `json:"status,omitempty"`
	Archived *bool  `json:"archived,omitempty"`
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

	// Update archived status if provided
	if req.Archived != nil {
		h.state.SetTaskArchived(taskID, *req.Archived)
	}

	// Also update in beads if applicable (skip if beads client unavailable)
	if h.beadsClient == nil {
		response := map[string]interface{}{
			"id": taskID,
		}
		if req.Status != "" {
			response["status"] = req.Status
		}
		if req.Archived != nil {
			response["archived"] = *req.Archived
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}

	if req.Status == "in_progress" {
		if err := h.beadsClient.Start(r.Context(), taskID); err != nil {
			http.Error(w, fmt.Sprintf("Failed to update task in beads: %v", err), http.StatusInternalServerError)
			return
		}
	} else if req.Status == "completed" {
		if err := h.beadsClient.Done(r.Context(), taskID); err != nil {
			http.Error(w, fmt.Sprintf("Failed to update task in beads: %v", err), http.StatusInternalServerError)
			return
		}
	} else if req.Status == "failed" {
		if err := h.beadsClient.Fail(r.Context(), taskID, "Task failed"); err != nil {
			http.Error(w, fmt.Sprintf("Failed to update task in beads: %v", err), http.StatusInternalServerError)
			return
		}
	}

	response := map[string]interface{}{
		"id": taskID,
	}
	if req.Status != "" {
		response["status"] = req.Status
	}
	if req.Archived != nil {
		response["archived"] = *req.Archived
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// HandlePauseOrch pauses the orchestrator
func (h *Handler) HandlePauseOrch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.scheduler == nil {
		http.Error(w, "Scheduler not available", http.StatusNotImplemented)
		return
	}

	h.scheduler.Pause()
	h.state.Pause()

	// Also pause the merge queue if available (user-initiated pause)
	if h.mergeQueue != nil {
		h.mergeQueue.Pause()
	}

	// Build event payload with detailed pause state
	payload := map[string]interface{}{
		"paused":         true,
		"paused_by_user": true,
	}
	if h.mergeQueue != nil {
		payload["pause_state"] = h.mergeQueue.PauseStateString()
	}

	// Publish pause event to notify WebSocket clients
	if h.eventBus != nil {
		h.eventBus.Publish(Event{
			Type:      EventOrchPaused,
			Timestamp: time.Now(),
			Payload:   payload,
		})
	}

	response := map[string]interface{}{
		"status": "paused",
	}
	if h.mergeQueue != nil {
		response["pause_state"] = h.mergeQueue.PauseStateString()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// HandleResumeOrch resumes the orchestrator
func (h *Handler) HandleResumeOrch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.scheduler == nil {
		http.Error(w, "Scheduler not available", http.StatusNotImplemented)
		return
	}

	h.scheduler.Resume()
	h.state.Resume()

	// Also resume the merge queue if available (user-initiated resume)
	if h.mergeQueue != nil {
		h.mergeQueue.Resume()
	}

	// Build event payload with detailed pause state
	// After user resume, we may still be paused by resolver
	isPaused := false
	pausedByResolver := false
	if h.mergeQueue != nil {
		isPaused = h.mergeQueue.IsPaused()
		pausedByResolver = isPaused && !h.mergeQueue.IsPausedByUser()
	}

	payload := map[string]interface{}{
		"paused":             isPaused,
		"paused_by_user":     false,
		"paused_by_resolver": pausedByResolver,
	}
	if h.mergeQueue != nil {
		payload["pause_state"] = h.mergeQueue.PauseStateString()
	}

	// Publish resume event to notify WebSocket clients
	if h.eventBus != nil {
		h.eventBus.Publish(Event{
			Type:      EventOrchResumed,
			Timestamp: time.Now(),
			Payload:   payload,
		})
	}

	response := map[string]interface{}{
		"status": "resumed",
	}
	if h.mergeQueue != nil {
		response["pause_state"] = h.mergeQueue.PauseStateString()
		if isPaused {
			response["status"] = "partially_resumed"
			response["still_paused_by_resolver"] = true
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
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

// AgentUpdateRequest represents a request to update an agent
type AgentUpdateRequest struct {
	Archived *bool `json:"archived,omitempty"`
}

// HandleUpdateAgent updates an existing agent (archive/unarchive)
func (h *Handler) HandleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract agent ID from query parameter
	agentID := r.URL.Query().Get("id")
	if agentID == "" {
		http.Error(w, "Agent ID required", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req AgentUpdateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Check if agent exists
	agent := h.state.GetAgent(agentID)
	if agent == nil {
		http.Error(w, fmt.Sprintf("Agent %s not found", agentID), http.StatusNotFound)
		return
	}

	// Update archived status if provided
	if req.Archived != nil {
		h.state.SetAgentArchived(agentID, *req.Archived)

		// Persist to database if persistence store is available
		if h.persistenceStore != nil {
			if err := h.persistenceStore.SetAgentArchived(agentID, *req.Archived); err != nil {
				// Log error but don't fail the request - runtime state was updated
				// This allows archiving to work even if persistence fails
				fmt.Printf("Warning: failed to persist agent archive status: %v\n", err)
			}
		}

		// Clean up live feed JSONL file when archiving
		if *req.Archived {
			if err := persistence.DeleteLiveFeedFile(agentID); err != nil {
				// Log error but don't fail - cleanup is best-effort
				fmt.Printf("Warning: failed to delete live feed file for archived agent: %v\n", err)
			}
		}
	}

	response := map[string]interface{}{
		"id": agentID,
	}
	if req.Archived != nil {
		response["archived"] = *req.Archived
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// MergeQueueState represents the current state of the merge queue
type MergeQueueState struct {
	Completed          []MergeCompletedItem `json:"completed"`
	Resolvers          []MergeResolverItem  `json:"resolvers"`
	Pending            []MergePendingItem   `json:"pending"`
	ActiveWorkers      []MergeWorkerItem    `json:"active_workers"`
	IsPaused           bool                 `json:"is_paused"`
	IsPausedByUser     bool                 `json:"is_paused_by_user"`
	IsPausedByResolver bool                 `json:"is_paused_by_resolver"`
	PauseState         string               `json:"pause_state"`
	QueueLength        int                  `json:"queue_length"`
}

// MergeCompletedItem represents a completed merge
type MergeCompletedItem struct {
	TaskID    string    `json:"task_id"`
	AgentID   string    `json:"agent_id"`
	Timestamp time.Time `json:"timestamp"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
}

// MergeResolverItem represents an active resolver for merge conflicts
type MergeResolverItem struct {
	ParentTaskID    string `json:"parent_task_id"`
	ResolverTaskID  string `json:"resolver_task_id"`
	ParentAgentID   string `json:"parent_agent_id"`
	ResolverAgentID string `json:"resolver_agent_id"`
	Status          string `json:"status"`
}

// MergePendingItem represents a task waiting in the merge queue
type MergePendingItem struct {
	TaskID   string `json:"task_id"`
	AgentID  string `json:"agent_id"`
	Position int    `json:"position"`
}

// MergeWorkerItem represents an agent actively working on a task
type MergeWorkerItem struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
	Status  string `json:"status"`
}

// HandleGetMergeQueue returns the current merge queue state
func (h *Handler) HandleGetMergeQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot := h.state.GetSnapshot()

	// Build merge queue state from agent states
	state := MergeQueueState{
		Completed:     make([]MergeCompletedItem, 0),
		Resolvers:     make([]MergeResolverItem, 0),
		Pending:       make([]MergePendingItem, 0),
		ActiveWorkers: make([]MergeWorkerItem, 0),
		IsPaused:      snapshot.IsPaused,
		PauseState:    "running", // default
	}

	// Get detailed pause state from merge queue if available
	if h.mergeQueue != nil {
		state.IsPaused = h.mergeQueue.IsPaused()
		state.IsPausedByUser = h.mergeQueue.IsPausedByUser()
		state.IsPausedByResolver = h.mergeQueue.IsPaused() && !h.mergeQueue.IsPausedByUser()
		state.PauseState = h.mergeQueue.PauseStateString()
	}

	// Track resolver relationships for building resolver items
	resolverAgents := make(map[string]*AgentState) // parentAgentID -> resolver agent

	// Categorize agents by their merge status
	for _, agent := range snapshot.Agents {
		switch agent.MergeStatus {
		case MergeStatusMerged:
			// Successfully merged
			var timestamp time.Time
			if agent.EndTime != nil {
				timestamp = *agent.EndTime
			}
			state.Completed = append(state.Completed, MergeCompletedItem{
				TaskID:    agent.TaskID,
				AgentID:   agent.ID,
				Timestamp: timestamp,
				Success:   true,
			})
		case MergeStatusFailed:
			// Failed merge
			var timestamp time.Time
			if agent.EndTime != nil {
				timestamp = *agent.EndTime
			}
			state.Completed = append(state.Completed, MergeCompletedItem{
				TaskID:    agent.TaskID,
				AgentID:   agent.ID,
				Timestamp: timestamp,
				Success:   false,
				Error:     agent.MergeError,
			})
		case MergeStatusResolving:
			// Agent is waiting for resolver - track for resolver items
			// Find the resolver child agent
			for _, childID := range agent.ChildAgentIDs {
				if child, exists := snapshot.Agents[childID]; exists {
					resolverAgents[agent.ID] = child
					break
				}
			}
		case MergeStatusPending, MergeStatusAcquiring:
			// Waiting in queue
			state.Pending = append(state.Pending, MergePendingItem{
				TaskID:   agent.TaskID,
				AgentID:  agent.ID,
				Position: agent.MergeQueuePos,
			})
		case MergeStatusMerging:
			// Currently merging - this counts as active work
			state.ActiveWorkers = append(state.ActiveWorkers, MergeWorkerItem{
				AgentID: agent.ID,
				TaskID:  agent.TaskID,
				Status:  string(agent.MergeStatus),
			})
		}

		// Also track running agents as active workers
		if agent.Status == AgentStatusRunning && agent.MergeStatus == MergeStatusNone {
			state.ActiveWorkers = append(state.ActiveWorkers, MergeWorkerItem{
				AgentID: agent.ID,
				TaskID:  agent.TaskID,
				Status:  string(agent.Status),
			})
		}
	}

	// Build resolver items from tracked relationships
	for parentID, resolver := range resolverAgents {
		parent := snapshot.Agents[parentID]
		if parent != nil {
			status := "running"
			if resolver.Status == AgentStatusCompleted {
				status = "completed"
			} else if resolver.Status == AgentStatusFailed {
				status = "failed"
			}
			state.Resolvers = append(state.Resolvers, MergeResolverItem{
				ParentTaskID:    parent.TaskID,
				ResolverTaskID:  resolver.TaskID,
				ParentAgentID:   parent.ID,
				ResolverAgentID: resolver.ID,
				Status:          status,
			})
		}
	}

	// Calculate queue length from pending items
	state.QueueLength = len(state.Pending)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(state); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode merge queue state: %v", err), http.StatusInternalServerError)
		return
	}
}
