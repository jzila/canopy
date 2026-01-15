package daemon

import (
	"log"
	"sync"
	"time"

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
	}
}

// handleRunStarted creates a new run record in the store
func (h *PersistenceHandler) handleRunStarted(event Event) {
	payload, ok := event.Payload.(map[string]interface{})
	if !ok {
		log.Printf("PersistenceHandler: invalid run started payload type")
		return
	}

	runID, _ := payload["run_id"].(string)
	if runID == "" {
		log.Printf("PersistenceHandler: missing run_id in run started event")
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
		log.Printf("PersistenceHandler: failed to create run %s: %v", runID, err)
	} else {
		log.Printf("PersistenceHandler: persisted run %s (tasks=%d)", runID, taskCount)
	}
}

// handleAgentMergeStatus updates the merge result fields when merge completes.
// We only persist final merge statuses (merged or failed) to avoid noise from
// intermediate statuses (pending, merging, resolving).
func (h *PersistenceHandler) handleAgentMergeStatus(event Event) {
	payload, ok := event.Payload.(map[string]interface{})
	if !ok {
		log.Printf("PersistenceHandler: invalid agent merge status payload type")
		return
	}

	agentID, _ := payload["agent_id"].(string)
	if agentID == "" {
		log.Printf("PersistenceHandler: missing agent_id in agent merge status event")
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
		log.Printf("PersistenceHandler: failed to update merge result for agent %s: %v", agentID, err)
	} else {
		log.Printf("PersistenceHandler: updated merge result for agent %s (status=%s, commits=%d, conflict=%v, resolver=%v)",
			agentID, mergeStatus, commitsApplied, hadConflict, resolverSpawned)
	}
}

// handleAgentStarted creates a new agent record in the store
func (h *PersistenceHandler) handleAgentStarted(event Event) {
	payload, ok := event.Payload.(map[string]interface{})
	if !ok {
		log.Printf("PersistenceHandler: invalid agent started payload type")
		return
	}

	agentID, _ := payload["agent_id"].(string)
	if agentID == "" {
		log.Printf("PersistenceHandler: missing agent_id in agent started event")
		return
	}

	taskID, _ := payload["task_id"].(string)
	taskTitle, _ := payload["task_title"].(string)
	repoID, _ := payload["repo_id"].(string)

	// Get current run ID
	h.mu.RLock()
	runID := h.currentRunID
	h.mu.RUnlock()

	agent := &persistence.Agent{
		ID:        agentID,
		RunID:     runID,
		TaskID:    taskID,
		TaskTitle: taskTitle,
		Status:    persistence.AgentStatusRunning,
		StartedAt: event.Timestamp,
		RepoID:    repoID,
	}

	if err := h.store.CreateAgent(agent); err != nil {
		log.Printf("PersistenceHandler: failed to create agent %s: %v", agentID, err)
	} else {
		log.Printf("PersistenceHandler: persisted agent %s (task=%s, run=%s)", agentID, taskID, runID)
	}
}

// handleAgentCompleted updates an agent record with completion data
func (h *PersistenceHandler) handleAgentCompleted(event Event) {
	payload, ok := event.Payload.(map[string]interface{})
	if !ok {
		log.Printf("PersistenceHandler: invalid agent completed payload type")
		return
	}

	agentID, _ := payload["agent_id"].(string)
	if agentID == "" {
		log.Printf("PersistenceHandler: missing agent_id in agent completed event")
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
	cacheCreationTokens, _ := getIntFromPayload(payload, "cache_creation_tokens")
	cacheReadTokens, _ := getIntFromPayload(payload, "cache_read_tokens")
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
		log.Printf("PersistenceHandler: failed to update agent %s: %v", agentID, err)
	} else {
		log.Printf("PersistenceHandler: updated agent %s (status=%s)", agentID, status)
	}
}

// handleRunCompleted updates a run record with completion data
func (h *PersistenceHandler) handleRunCompleted(event Event) {
	payload, ok := event.Payload.(map[string]interface{})
	if !ok {
		log.Printf("PersistenceHandler: invalid run completed payload type")
		return
	}

	runID, _ := payload["run_id"].(string)
	if runID == "" {
		log.Printf("PersistenceHandler: missing run_id in run completed event")
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
		log.Printf("PersistenceHandler: failed to update run %s: %v", runID, err)
	} else {
		log.Printf("PersistenceHandler: updated run %s (status=%s, succeeded=%d, failed=%d, cost=$%.4f)", runID, status, succeededTasks, failedTasks, totalCostUSD)
	}

	// Clear current run ID
	h.mu.Lock()
	h.currentRunID = ""
	h.mu.Unlock()
}
