package daemon

import (
	"sync"
	"time"

	"github.com/jzila/canopy/pkg/logging"
	"github.com/jzila/canopy/pkg/persistence"
)

// PersistenceHandler subscribes to the EventBus and persists events to SQLite.
// It tracks run and agent state, writing to the persistence store as events occur.
type PersistenceHandler struct {
	store    *persistence.Store
	eventBus *EventBus

	// Track current run ID to associate agents with runs
	mu           sync.RWMutex
	currentRunID string
	runStartTime time.Time
}

// NewPersistenceHandler creates a new PersistenceHandler with the given store and EventBus.
func NewPersistenceHandler(store *persistence.Store, eventBus *EventBus) *PersistenceHandler {
	return &PersistenceHandler{
		store:    store,
		eventBus: eventBus,
	}
}

// Start subscribes to the EventBus and begins handling events.
// Returns an unsubscribe function that should be called to stop handling events.
func (h *PersistenceHandler) Start() func() {
	return h.eventBus.Subscribe(func(event Event) {
		h.handleEvent(event)
	})
}

// handleEvent processes an event and persists it to the store
func (h *PersistenceHandler) handleEvent(event Event) {
	switch event.Type {
	case EventRunStarted:
		h.handleRunStarted(event)
	case EventAgentStarted:
		h.handleAgentStarted(event)
	case EventAgentMergeStatus:
		h.handleAgentMergeStatus(event)
	case EventAgentCompleted:
		h.handleAgentCompleted(event)
	case EventRunCompleted:
		h.handleRunCompleted(event)
	case EventTaskUpdated:
		h.handleTaskUpdated(event)
	}
}

// handleRunStarted creates a new run record in the store
func (h *PersistenceHandler) handleRunStarted(event Event) {
	payload, ok := event.Payload.(map[string]interface{})
	if !ok {
		logging.Warn("invalid run started payload type", "component", "persistence")
		return
	}

	runID, _ := payload["run_id"].(string)
	if runID == "" {
		logging.Warn("missing run_id in run started event", "component", "persistence")
		return
	}

	taskCount, _ := getIntFromPayload(payload, "task_count")
	repoID, _ := payload["repo_id"].(string)
	repoPath, _ := payload["repo_path"].(string)
	repoName, _ := payload["repo_name"].(string)

	// Store current run ID for associating agents
	h.mu.Lock()
	h.currentRunID = runID
	h.runStartTime = event.Timestamp
	h.mu.Unlock()

	run := &persistence.Run{
		ID:         runID,
		StartedAt:  event.Timestamp,
		Status:     persistence.RunStatusRunning,
		TotalTasks: taskCount,
		RepoID:     repoID,
		RepoPath:   repoPath,
		RepoName:   repoName,
	}

	if err := h.store.CreateRun(run); err != nil {
		logging.Error("failed to create run", "run_id", runID, "error", err, "component", "persistence")
	} else {
		logging.Debug("persisted run", "run_id", runID, "task_count", taskCount, "component", "persistence")
	}
}

// handleAgentMergeStatus updates the merge result fields when merge completes.
// We only persist final merge statuses (merged or failed) to avoid noise from
// intermediate statuses (pending, merging, resolving).
func (h *PersistenceHandler) handleAgentMergeStatus(event Event) {
	payload, ok := event.Payload.(map[string]interface{})
	if !ok {
		logging.Warn("invalid agent merge status payload type", "component", "persistence")
		return
	}

	agentID, _ := payload["agent_id"].(string)
	if agentID == "" {
		logging.Warn("missing agent_id in agent merge status event", "component", "persistence")
		return
	}

	mergeStatus, _ := payload["merge_status"].(string)
	mergeErr, _ := payload["error"].(string)

	// Only persist final merge statuses
	if mergeStatus != "merged" && mergeStatus != "failed" {
		return
	}

	// Map IPC merge status to persistence merge status
	var persistMergeStatus persistence.MergeStatus
	switch mergeStatus {
	case "merged":
		persistMergeStatus = persistence.MergeStatusMerged
	case "failed":
		persistMergeStatus = persistence.MergeStatusFailed
	}

	// Extract merge result details from payload
	commitsApplied, _ := getIntFromPayload(payload, "commits_applied")
	hadConflict, _ := payload["had_conflict"].(bool)
	resolverSpawned, _ := payload["resolver_spawned"].(bool)

	if err := h.store.UpdateAgentMergeResult(agentID, persistMergeStatus, commitsApplied, hadConflict, resolverSpawned, mergeErr); err != nil {
		logging.Error("failed to update merge result",
			"agent_id", agentID,
			"error", err,
			"component", "persistence")
	} else {
		logging.Debug("updated merge result",
			"agent_id", agentID,
			"merge_status", mergeStatus,
			"commits_applied", commitsApplied,
			"had_conflict", hadConflict,
			"resolver_spawned", resolverSpawned,
			"component", "persistence")
	}
}

// handleAgentStarted creates a new agent record in the store
func (h *PersistenceHandler) handleAgentStarted(event Event) {
	payload, ok := event.Payload.(map[string]interface{})
	if !ok {
		logging.Warn("invalid agent started payload type", "component", "persistence")
		return
	}

	agentID, _ := payload["agent_id"].(string)
	if agentID == "" {
		logging.Warn("missing agent_id in agent started event", "component", "persistence")
		return
	}

	taskID, _ := payload["task_id"].(string)
	taskTitle, _ := payload["task_title"].(string)
	repoID, _ := payload["repo_id"].(string)
	parentAgentID, _ := payload["parent_agent_id"].(string)

	// Get current run ID
	h.mu.RLock()
	runID := h.currentRunID
	h.mu.RUnlock()

	agent := &persistence.Agent{
		ID:            agentID,
		RunID:         runID,
		TaskID:        taskID,
		TaskTitle:     taskTitle,
		Status:        persistence.AgentStatusRunning,
		StartedAt:     event.Timestamp,
		RepoID:        repoID,
		ParentAgentID: parentAgentID,
	}

	if err := h.store.CreateAgent(agent); err != nil {
		logging.Error("failed to create agent",
			"agent_id", agentID,
			"error", err,
			"component", "persistence")
	} else {
		logging.Debug("persisted agent",
			"agent_id", agentID,
			"task_id", taskID,
			"run_id", runID,
			"component", "persistence")
	}
}

// handleAgentCompleted updates an agent record with completion data
func (h *PersistenceHandler) handleAgentCompleted(event Event) {
	payload, ok := event.Payload.(map[string]interface{})
	if !ok {
		logging.Warn("invalid agent completed payload type", "component", "persistence")
		return
	}

	agentID, _ := payload["agent_id"].(string)
	if agentID == "" {
		logging.Warn("missing agent_id in agent completed event", "component", "persistence")
		return
	}

	// Get current run ID
	h.mu.RLock()
	runID := h.currentRunID
	h.mu.RUnlock()

	// Determine status based on error presence
	status := persistence.AgentStatusCompleted
	errorMsg, _ := payload["error"].(string)
	if errorMsg != "" {
		status = persistence.AgentStatusFailed
	}

	// Extract metrics
	exitCode, _ := getIntFromPayload(payload, "exit_code")
	duration, _ := payload["duration"].(float64)
	inputTokens, _ := getIntFromPayload(payload, "input_tokens")
	outputTokens, _ := getIntFromPayload(payload, "output_tokens")
	cacheCreationTokens, _ := getIntFromPayload(payload, "cache_creation_input_tokens")
	cacheReadTokens, _ := getIntFromPayload(payload, "cache_read_input_tokens")
	costUSD, _ := payload["cost_usd"].(float64)
	filesChanged, _ := getIntFromPayload(payload, "files_changed")
	commitsCreated, _ := getIntFromPayload(payload, "commits_created")
	numTurns, _ := getIntFromPayload(payload, "num_turns")
	resultMessage, _ := payload["result_message"].(string)
	stdout, _ := payload["stdout"].(string)
	stderr, _ := payload["stderr"].(string)

	finishedAt := event.Timestamp
	agent := &persistence.Agent{
		ID:                  agentID,
		RunID:               runID,
		Status:              status,
		FinishedAt:          &finishedAt,
		DurationSeconds:     duration,
		ExitCode:            &exitCode,
		ErrorMessage:        errorMsg,
		Stdout:              stdout,
		Stderr:              stderr,
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		TotalTokens:         inputTokens + outputTokens,
		CacheCreationTokens: cacheCreationTokens,
		CacheReadTokens:     cacheReadTokens,
		CostUSD:             costUSD,
		FilesChanged:        filesChanged,
		GitCommitsCreated:   commitsCreated,
		NumTurns:            numTurns,
		ResultMessage:       resultMessage,
	}

	if err := h.store.UpdateAgent(agent); err != nil {
		logging.Error("failed to update agent",
			"agent_id", agentID,
			"error", err,
			"component", "persistence")
	} else {
		logging.Debug("updated agent",
			"agent_id", agentID,
			"status", status,
			"component", "persistence")
	}
}

// handleRunCompleted updates a run record with completion data
func (h *PersistenceHandler) handleRunCompleted(event Event) {
	payload, ok := event.Payload.(map[string]interface{})
	if !ok {
		logging.Warn("invalid run completed payload type", "component", "persistence")
		return
	}

	runID, _ := payload["run_id"].(string)
	if runID == "" {
		logging.Warn("missing run_id in run completed event", "component", "persistence")
		return
	}

	// Extract stats from nested payload structure
	// The IPC protocol sends stats as a nested object under "stats"
	stats, _ := payload["stats"].(map[string]interface{})
	if stats == nil {
		// Fallback: stats might be at top level (backwards compatibility)
		stats = payload
	}

	totalTasks, _ := getIntFromPayload(stats, "total_tasks")
	succeededTasks, _ := getIntFromPayload(stats, "succeeded_tasks")
	failedTasks, _ := getIntFromPayload(stats, "failed_tasks")

	// Extract additional metrics from RunStats
	totalDuration, _ := stats["total_duration_seconds"].(float64)
	totalInputTokens, _ := getIntFromPayload(stats, "total_input_tokens")
	totalOutputTokens, _ := getIntFromPayload(stats, "total_output_tokens")
	cacheCreationTokens, _ := getIntFromPayload(stats, "total_cache_creation_input_tokens")
	cacheReadTokens, _ := getIntFromPayload(stats, "total_cache_read_input_tokens")
	totalCostUSD, _ := stats["total_cost_usd"].(float64)
	totalTurns, _ := getIntFromPayload(stats, "total_turns")
	filesChanged, _ := getIntFromPayload(stats, "files_changed")
	gitCommits, _ := getIntFromPayload(stats, "git_commits")

	// Determine overall status
	status := persistence.RunStatusCompleted
	if failedTasks > 0 && succeededTasks > 0 {
		status = persistence.RunStatusPartial
	} else if failedTasks > 0 {
		status = persistence.RunStatusFailed
	}

	finishedAt := event.Timestamp
	run := &persistence.Run{
		ID:                  runID,
		FinishedAt:          &finishedAt,
		Status:              status,
		TotalTasks:          totalTasks,
		CompletedTasks:      succeededTasks,
		FailedTasks:         failedTasks,
		DurationSeconds:     totalDuration,
		TotalInputTokens:    totalInputTokens,
		TotalOutputTokens:   totalOutputTokens,
		CacheCreationTokens: cacheCreationTokens,
		CacheReadTokens:     cacheReadTokens,
		TotalCostUSD:        totalCostUSD,
		TotalTurns:          totalTurns,
		FilesChanged:        filesChanged,
		GitCommits:          gitCommits,
	}

	if err := h.store.UpdateRun(run); err != nil {
		logging.Error("failed to update run",
			"run_id", runID,
			"error", err,
			"component", "persistence")
	} else {
		logging.Info("run completed",
			"run_id", runID,
			"status", status,
			"succeeded_tasks", succeededTasks,
			"failed_tasks", failedTasks,
			"cost_usd", totalCostUSD,
			"component", "persistence")
	}

	// Clear current run ID
	h.mu.Lock()
	h.currentRunID = ""
	h.mu.Unlock()
}

// handleTaskUpdated persists task state changes to the store
func (h *PersistenceHandler) handleTaskUpdated(event Event) {
	payload, ok := event.Payload.(map[string]interface{})
	if !ok {
		logging.Warn("invalid task updated payload type", "component", "persistence")
		return
	}

	taskID, _ := payload["id"].(string)
	if taskID == "" {
		logging.Warn("missing id in task updated event", "component", "persistence")
		return
	}

	title, _ := payload["title"].(string)
	status, _ := payload["status"].(string)
	taskType, _ := payload["type"].(string)
	priority, _ := getIntFromPayload(payload, "priority")
	agentID, _ := payload["agent_id"].(string)
	repoID, _ := payload["repo_id"].(string)

	task := &persistence.Task{
		ID:       taskID,
		RepoID:   repoID,
		Title:    title,
		Status:   status,
		Type:     taskType,
		Priority: priority,
		AgentID:  agentID,
	}

	if err := h.store.UpsertTask(task); err != nil {
		logging.Error("failed to upsert task",
			"task_id", taskID,
			"error", err,
			"component", "persistence")
	} else {
		logging.Debug("persisted task",
			"task_id", taskID,
			"status", status,
			"component", "persistence")
	}
}
