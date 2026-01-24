package daemon

import (
	"encoding/json"
	"fmt"

	"github.com/jzila/canopy/pkg/lifecycle"
	"github.com/jzila/canopy/pkg/logging"
	"github.com/jzila/canopy/pkg/persistence"
)

// PersistenceManager coordinates the persistence layer including the store,
// event handler, and state restoration.
type PersistenceManager struct {
	store       *persistence.Store
	handler     *PersistenceHandler
	unsubscribe func()
	enabled     bool
}

// NewPersistenceManager creates a new PersistenceManager.
// If enabled is false, the manager will be a no-op.
func NewPersistenceManager(enabled bool) *PersistenceManager {
	return &PersistenceManager{
		enabled: enabled,
	}
}

// Initialize sets up the persistence store and handler.
// Returns an error if initialization fails.
func (m *PersistenceManager) Initialize(eventBus *EventBus) error {
	if !m.enabled {
		return nil
	}

	if m.store != nil {
		// Already initialized
		return nil
	}

	store, err := persistence.NewStore()
	if err != nil {
		return fmt.Errorf("failed to initialize persistence store: %w", err)
	}

	m.store = store
	m.handler = NewPersistenceHandler(store, eventBus)
	m.unsubscribe = m.handler.Start()

	logging.Info("persistence enabled - run history will be saved to SQLite")
	return nil
}

// Close shuts down the persistence layer.
func (m *PersistenceManager) Close() error {
	if m.unsubscribe != nil {
		m.unsubscribe()
		m.unsubscribe = nil
	}

	if m.store != nil {
		if err := m.store.Close(); err != nil {
			return fmt.Errorf("failed to close persistence store: %w", err)
		}
		m.store = nil
	}

	return nil
}

// GetStore returns the persistence store, or nil if persistence is disabled.
func (m *PersistenceManager) GetStore() *persistence.Store {
	return m.store
}

// IsEnabled returns whether persistence is enabled.
func (m *PersistenceManager) IsEnabled() bool {
	return m.enabled && m.store != nil
}

// MarkOrphanedStatesAsFailed marks any orphaned runs and agents as failed.
// This handles the case where the daemon was terminated while a run was in progress.
func (m *PersistenceManager) MarkOrphanedStatesAsFailed() error {
	if !m.IsEnabled() {
		return nil
	}

	// Mark orphaned agents first (agents in "starting" or "running" state)
	agentCount, err := m.store.MarkOrphanedAgentsFailed()
	if err != nil {
		return fmt.Errorf("failed to mark orphaned agents: %w", err)
	}
	if agentCount > 0 {
		logging.Warn("marked orphaned agents as failed (daemon terminated unexpectedly)", "count", agentCount)
	}

	// Mark orphaned runs (runs in "running" state)
	runCount, err := m.store.MarkOrphanedRunsFailed()
	if err != nil {
		return fmt.Errorf("failed to mark orphaned runs: %w", err)
	}
	if runCount > 0 {
		logging.Warn("marked orphaned runs as failed (daemon terminated unexpectedly)", "count", runCount)
	}

	return nil
}

// GetMostRecentRun returns the most recent run from the database.
func (m *PersistenceManager) GetMostRecentRun() (*persistence.Run, error) {
	if !m.IsEnabled() {
		return nil, nil
	}
	return m.store.GetMostRecentRun()
}

// GetAllNonArchivedAgents returns all non-archived agents from the database.
func (m *PersistenceManager) GetAllNonArchivedAgents() ([]persistence.Agent, error) {
	if !m.IsEnabled() {
		return nil, nil
	}
	return m.store.GetAllNonArchivedAgents()
}

// GetAllTasks returns all tasks from the database.
func (m *PersistenceManager) GetAllTasks() ([]persistence.Task, error) {
	if !m.IsEnabled() {
		return nil, nil
	}
	return m.store.GetAllTasks()
}

// RestoreState restores daemon state from the database.
// Returns the restored run (if any), agents, and tasks.
type RestoredState struct {
	Run    *persistence.Run
	Agents []persistence.Agent
	Tasks  []persistence.Task
}

// RestoreState loads state from the database for daemon initialization.
func (m *PersistenceManager) RestoreState() (*RestoredState, error) {
	if !m.IsEnabled() {
		logging.Debug("state restoration skipped - persistence not enabled")
		return nil, nil
	}

	logging.Debug("beginning database state restoration")

	// First, mark any orphaned runs/agents as failed
	if err := m.MarkOrphanedStatesAsFailed(); err != nil {
		return nil, fmt.Errorf("failed to mark orphaned states: %w", err)
	}

	state := &RestoredState{}

	// Get the most recent run
	run, err := m.store.GetMostRecentRun()
	if err != nil {
		return nil, fmt.Errorf("failed to get most recent run: %w", err)
	}
	state.Run = run

	if run != nil {
		logging.Debug("most recent run found",
			"run_id", run.ID,
			"status", run.Status,
			"started_at", run.StartedAt.Format("2006-01-02 15:04:05"))
	} else {
		logging.Debug("no previous runs found in database")
	}

	// Load all agents (including archived) so they can be displayed and un-archived
	agents, err := m.store.GetAllAgents()
	if err != nil {
		return nil, fmt.Errorf("failed to get agents: %w", err)
	}
	state.Agents = agents

	if len(agents) > 0 {
		logging.Debug("found agents to restore", "count", len(agents))
	}

	// Load tasks
	tasks, err := m.store.GetAllTasks()
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks: %w", err)
	}
	state.Tasks = tasks

	if len(tasks) > 0 {
		logging.Debug("found tasks to restore", "count", len(tasks))
	}

	return state, nil
}

// ConvertPersistenceAgentToState converts a persistence.Agent to a daemon.AgentState
func ConvertPersistenceAgentToState(pAgent *persistence.Agent) *AgentState {
	agent := &AgentState{
		ID:            pAgent.ID,
		RunID:         pAgent.RunID,
		TaskID:        pAgent.TaskID,
		TaskTitle:     pAgent.TaskTitle,
		RepoID:        pAgent.RepoID,
		ParentAgentID: pAgent.ParentAgentID,
		Status:        ConvertPersistenceStatus(pAgent.Status),
		StartTime:     pAgent.StartedAt,
		Duration:      pAgent.DurationSeconds,
		TokenUsage: TokenUsage{
			InputTokens:              pAgent.InputTokens,
			OutputTokens:             pAgent.OutputTokens,
			CacheCreationInputTokens: pAgent.CacheCreationTokens,
			CacheReadInputTokens:     pAgent.CacheReadTokens,
			TotalTokens:              pAgent.TotalTokens,
			CostUSD:                  pAgent.CostUSD,
		},
		Changes:       pAgent.FilesChanged,
		Commits:       pAgent.GitCommitsCreated,
		NumTurns:      pAgent.NumTurns,
		ResultMessage: pAgent.ResultMessage,
		Error:         pAgent.ErrorMessage,
		Archived:      pAgent.Archived,
	}

	if pAgent.FinishedAt != nil {
		agent.EndTime = pAgent.FinishedAt
	}

	if pAgent.ExitCode != nil {
		agent.ExitCode = *pAgent.ExitCode
	}

	// Restore stdout/stderr if available
	if pAgent.Stdout != "" || pAgent.Stderr != "" {
		agent.Output.Stdout = pAgent.Stdout
		agent.Output.Stderr = pAgent.Stderr
	}

	// Try to load live feed events from JSONL file
	agent.LiveFeedEvents = LoadLiveFeedEventsFromFile(pAgent.ID)
	if len(agent.LiveFeedEvents) == 0 {
		// No persisted events found, fall back to synthetic events for historical data
		agent.LiveFeedEvents = GenerateHistoricalLiveFeedEvents(pAgent)
	}

	// Restore merge status fields
	if pAgent.MergeStatus != "" {
		agent.MergeStatus = MergeStatus(pAgent.MergeStatus)
	}
	if pAgent.MergeError != "" {
		agent.MergeError = pAgent.MergeError
	}

	// Restore validation and repair agent state fields
	agent.RepairAttempts = pAgent.RepairAttempts
	agent.LastRepairOutput = pAgent.LastRepairOutput
	agent.ValidationStatus = pAgent.ValidationStatus
	agent.ValidationDuration = pAgent.ValidationDuration
	agent.ValidationError = pAgent.ValidationError

	// Parse validation steps from JSON if present
	if pAgent.ValidationSteps != "" {
		var steps []ValidationStep
		if err := json.Unmarshal([]byte(pAgent.ValidationSteps), &steps); err != nil {
			logging.Debug("failed to parse validation steps", "agent_id", pAgent.ID, "error", err)
		} else {
			agent.ValidationSteps = steps
		}
	}

	// Restore session ID for claude --resume support
	agent.SessionID = pAgent.SessionID

	// Restore lifecycle state - use persisted state if available, otherwise derive from legacy fields
	lifecycleState := pAgent.LifecycleState
	if lifecycleState == "" {
		// Migration: derive lifecycle state from legacy fields for agents without lifecycle_state
		lifecycleState = string(deriveLifecycleStateFromLegacy(pAgent))
	}
	agent.LifecycleState = lifecycleState

	// Create the lifecycle state machine with the restored state
	// For restored agents, we initialize directly into the restored state without transitions
	if lifecycleState != "" {
		agent.Lifecycle = lifecycle.New(
			lifecycle.WithInitialState(lifecycle.AgentLifecycleState(lifecycleState)),
		)
	}

	return agent
}

// deriveLifecycleStateFromLegacy derives a lifecycle state from the legacy status fields.
// This is used for migrating existing agents that don't have lifecycle_state persisted.
// Note: Persistence only stores final merge statuses (merged, failed, skipped, merged_needs_repair),
// not intermediate states (pending, merging, resolving) which are runtime-only.
func deriveLifecycleStateFromLegacy(pAgent *persistence.Agent) lifecycle.AgentLifecycleState {
	// Check validation status first (most specific states)
	switch pAgent.ValidationStatus {
	case "running":
		return lifecycle.StateValidating
	case "repairing":
		return lifecycle.StateRepairing
	case "failed":
		// Validation failed - could be failed or needs_attention
		if pAgent.MergeStatus == persistence.MergeStatusMergedNeedsRepair {
			return lifecycle.StateNeedsAttention
		}
		return lifecycle.StateFailed
	case "passed":
		return lifecycle.StateCompleted
	}

	// Check merge status - persistence only stores final statuses
	switch pAgent.MergeStatus {
	case persistence.MergeStatusMerged, persistence.MergeStatusResolved:
		return lifecycle.StateCompleted
	case persistence.MergeStatusFailed:
		return lifecycle.StateMergeFailed
	case persistence.MergeStatusMergedNeedsRepair:
		return lifecycle.StateNeedsAttention
	case persistence.MergeStatusSkipped:
		return lifecycle.StateCompleted
	}

	// Fall back to agent status
	switch pAgent.Status {
	case persistence.AgentStatusStarting:
		return lifecycle.StateStarting
	case persistence.AgentStatusRunning:
		return lifecycle.StateRunning
	case persistence.AgentStatusCompleted:
		return lifecycle.StateCompleted
	case persistence.AgentStatusFailed:
		return lifecycle.StateFailed
	case persistence.AgentStatusCancelled:
		return lifecycle.StateCancelled
	case persistence.AgentStatusTimedOut:
		return lifecycle.StateTimedOut
	}

	// Default to running for unknown states
	return lifecycle.StateRunning
}

// ConvertPersistenceStatus converts persistence.AgentStatus to daemon.AgentStatus
func ConvertPersistenceStatus(status persistence.AgentStatus) AgentStatus {
	switch status {
	case persistence.AgentStatusStarting:
		return AgentStatusStarting
	case persistence.AgentStatusRunning:
		return AgentStatusRunning
	case persistence.AgentStatusCompleted:
		return AgentStatusCompleted
	case persistence.AgentStatusFailed:
		return AgentStatusFailed
	case persistence.AgentStatusTimedOut:
		return AgentStatusTimedOut
	case persistence.AgentStatusCancelled:
		return AgentStatusCancelled
	default:
		return AgentStatusRunning
	}
}

// GenerateHistoricalLiveFeedEvents creates synthetic live feed events from persisted agent data.
// Since live feed events aren't persisted to the database, we reconstruct meaningful events
// from the available data to provide visibility into historical agent executions.
// This is a fallback for historical data before live feed persistence was implemented.
func GenerateHistoricalLiveFeedEvents(pAgent *persistence.Agent) []LiveFeedEvent {
	events := []LiveFeedEvent{}

	// Add a "historical" marker event so the UI knows these are reconstructed
	events = append(events, NewTextEvent("[Historical session - live feed events were not recorded]", true))

	// If we have a result message, add it as a text event
	if pAgent.ResultMessage != "" {
		events = append(events, NewTextEvent(pAgent.ResultMessage, true))
	}

	// Add an agent_completed event with available metrics
	events = append(events, NewAgentCompletedEvent(
		pAgent.FilesChanged,
		pAgent.GitCommitsCreated,
		pAgent.ErrorMessage,
		pAgent.ResultMessage,
		true, // isHistoric
	))

	return events
}

// LoadLiveFeedEventsFromFile loads live feed events from a JSONL file and converts
// them to daemon.LiveFeedEvent structs with typed data.
func LoadLiveFeedEventsFromFile(agentID string) []LiveFeedEvent {
	persistedEvents, err := persistence.LoadLiveFeedEvents(agentID)
	if err != nil {
		logging.Debug("failed to load live feed events", "agent_id", agentID, "error", err)
		return nil
	}

	if len(persistedEvents) == 0 {
		return nil
	}

	events := make([]LiveFeedEvent, 0, len(persistedEvents))
	for _, pe := range persistedEvents {
		// Convert persisted event back to typed daemon event
		event := convertPersistedEventToLiveFeed(pe)
		events = append(events, event)
	}

	return events
}

// convertPersistedEventToLiveFeed converts a persistence.LiveFeedEvent to daemon.LiveFeedEvent
// with properly typed data based on the event type.
func convertPersistedEventToLiveFeed(pe persistence.LiveFeedEvent) LiveFeedEvent {
	eventType := LiveFeedEventType(pe.EventType)
	data := pe.RawData

	switch eventType {
	case LiveFeedEventToolUse:
		tool := getStringFromMap(data, "tool")
		filePath := getStringFromMap(data, "file_path")
		command := getStringFromMap(data, "command")
		pattern := getStringFromMap(data, "pattern")
		return NewToolUseEvent(tool, filePath, command, pattern)

	case LiveFeedEventText:
		text := getStringFromMap(data, "text")
		isHistoric := getBoolFromMap(data, "is_historic")
		return NewTextEvent(text, isHistoric)

	case LiveFeedEventFileChange:
		action := getStringFromMap(data, "action")
		filePath := getStringFromMap(data, "file_path")
		return NewFileChangeEvent(action, filePath)

	case LiveFeedEventAgentCompleted:
		filesChanged, _ := getIntFromPayload(data, "files_changed")
		commitsCreated, _ := getIntFromPayload(data, "commits_created")
		errMsg := getStringFromMap(data, "error")
		resultMsg := getStringFromMap(data, "result_message")
		isHistoric := getBoolFromMap(data, "is_historic")
		return NewAgentCompletedEvent(filesChanged, commitsCreated, errMsg, resultMsg, isHistoric)

	default:
		// Unknown event type - preserve raw data
		return LiveFeedEvent{
			EventType: eventType,
			RawData:   data,
		}
	}
}

// ConvertPersistenceTaskToState converts a persistence.Task to a daemon.TaskState
func ConvertPersistenceTaskToState(pTask *persistence.Task) *TaskState {
	return &TaskState{
		ID:       pTask.ID,
		Title:    pTask.Title,
		Status:   pTask.Status,
		Type:     pTask.Type,
		AgentID:  pTask.AgentID,
		Priority: pTask.Priority,
		RepoID:   pTask.RepoID,
	}
}

// RebuildAgentChildLinks rebuilds the ChildAgentIDs lists from ParentAgentID relationships.
func RebuildAgentChildLinks(agents map[string]*AgentState) {
	// First pass: clear existing ChildAgentIDs to avoid duplicates
	for _, agent := range agents {
		agent.ChildAgentIDs = nil
	}

	// Second pass: rebuild ChildAgentIDs from ParentAgentID relationships
	for _, agent := range agents {
		if agent.ParentAgentID != "" {
			if parent, exists := agents[agent.ParentAgentID]; exists {
				parent.ChildAgentIDs = append(parent.ChildAgentIDs, agent.ID)
			}
		}
	}
}

// ApplyRestoredState applies restored state to RuntimeState.
func ApplyRestoredState(state *RuntimeState, restored *RestoredState) {
	if restored == nil {
		return
	}

	// Update start time from most recent run
	if restored.Run != nil {
		state.StartTime = restored.Run.StartedAt
	}

	// Restore agents
	for i := range restored.Agents {
		agentState := ConvertPersistenceAgentToState(&restored.Agents[i])

		// Wire up lifecycle callbacks for non-terminal agents so any future
		// transitions publish events to the EventBus for real-time UI updates.
		if agentState.Lifecycle != nil && !agentState.Lifecycle.IsTerminal() {
			// Create a new lifecycle with the callback and same initial state
			agentState.Lifecycle = lifecycle.New(
				lifecycle.WithInitialState(agentState.Lifecycle.State()),
				lifecycle.WithTransitionCallback(state.makeLifecycleCallback(agentState.ID)),
			)
		}

		state.AddAgent(agentState)
		logging.Debug("restored agent",
			"agent_id", restored.Agents[i].ID,
			"task_id", restored.Agents[i].TaskID,
			"status", restored.Agents[i].Status,
			"run_id", restored.Agents[i].RunID,
			"parent_agent_id", restored.Agents[i].ParentAgentID)
	}

	// Rebuild parent-child links after all agents are added
	state.mu.Lock()
	RebuildAgentChildLinks(state.Agents)
	state.mu.Unlock()

	// Restore tasks
	state.mu.Lock()
	for i := range restored.Tasks {
		state.Tasks[restored.Tasks[i].ID] = ConvertPersistenceTaskToState(&restored.Tasks[i])
	}
	state.mu.Unlock()

	// Update stats
	state.UpdateStats()

	if len(restored.Agents) > 0 {
		logging.Info("restored agents from database", "count", len(restored.Agents))
	}
	if len(restored.Tasks) > 0 {
		logging.Info("restored tasks from database", "count", len(restored.Tasks))
	}
}

// StartTim