package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/config"
	canopyerrors "github.com/jzila/canopy/pkg/errors"
	"github.com/jzila/canopy/pkg/events"
	"github.com/jzila/canopy/pkg/logging"
	"github.com/jzila/canopy/pkg/orchestrator"
	"github.com/jzila/canopy/pkg/rules"
)

// RunConfig holds configuration for starting a new orchestration run.
// This mirrors orchestrator.Config but is used at the IPC/daemon boundary.
type RunConfig struct {
	WorkDir         string        `json:"work_dir"`
	OutputDir       string        `json:"output_dir,omitempty"`
	Concurrency     int           `json:"concurrency,omitempty"`
	Verbose         bool          `json:"verbose,omitempty"`
	DryRun          bool          `json:"dry_run,omitempty"`
	UseBwrap        bool          `json:"use_bwrap,omitempty"`
	MaxRetries      int           `json:"max_retries,omitempty"`
	MaxPriority     int           `json:"max_priority,omitempty"`
	ResolverTimeout time.Duration `json:"resolver_timeout,omitempty"`
	RepoID          string        `json:"repo_id,omitempty"`
}

// RunStatus represents the current status of an orchestration run.
type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

// RunState tracks the state of a single orchestration run.
type RunState struct {
	ID           string       `json:"id"`
	RepoPath     string       `json:"repo_path"`
	RepoID       string       `json:"repo_id,omitempty"`
	Status       RunStatus    `json:"status"`
	Config       RunConfig    `json:"config"`
	StartTime    time.Time    `json:"start_time"`
	EndTime      *time.Time   `json:"end_time,omitempty"`
	Error        string       `json:"error,omitempty"`
	TasksTotal   int          `json:"tasks_total"`
	TasksDone    int          `json:"tasks_done"`
	TasksFailed  int          `json:"tasks_failed"`

	// Internal: orchestrator instance and cancellation
	orch       *orchestrator.Orchestrator
	cancel     context.CancelFunc
	mu         sync.RWMutex
}

// OrchestratorManager manages orchestrator instances within the daemon.
// It owns orchestrator lifecycle (create, run, stop) and tracks active runs.
type OrchestratorManager struct {
	// Active runs indexed by run ID
	runs sync.Map // map[string]*RunState

	// Active runs indexed by repo path (for single-run-per-repo enforcement)
	runsByRepo sync.Map // map[string]string (repo path -> run ID)

	// Event bus for publishing orchestration events directly
	eventBus *events.EventBus

	// Runtime state for direct state updates (no IPC needed)
	state *RuntimeState
}

// NewOrchestratorManager creates a new OrchestratorManager instance.
func NewOrchestratorManager(eventBus *events.EventBus, state *RuntimeState) *OrchestratorManager {
	return &OrchestratorManager{
		eventBus: eventBus,
		state:    state,
	}
}

// GetRulesEngineForRepo returns the rules engine for a repository's active run, if any.
// Returns nil if no active run exists for the repository.
func (m *OrchestratorManager) GetRulesEngineForRepo(repoPath string) *rules.Engine {
	runIDI, ok := m.runsByRepo.Load(repoPath)
	if !ok {
		return nil
	}

	runStateI, ok := m.runs.Load(runIDI.(string))
	if !ok {
		return nil
	}

	runState := runStateI.(*RunState)
	if runState.orch == nil {
		return nil
	}

	return runState.orch.GetRulesEngine()
}

// GetRulesEngineForRun returns the rules engine for a specific run.
// Returns nil if the run doesn't exist or has no orchestrator.
func (m *OrchestratorManager) GetRulesEngineForRun(runID string) *rules.Engine {
	runStateI, ok := m.runs.Load(runID)
	if !ok {
		return nil
	}

	runState := runStateI.(*RunState)
	if runState.orch == nil {
		return nil
	}

	return runState.orch.GetRulesEngine()
}

// StartRun creates and starts a new orchestration run.
// Returns the run ID or an error if the run could not be started.
// Only one run per repository is allowed at a time.
func (m *OrchestratorManager) StartRun(ctx context.Context, config RunConfig) (string, error) {
	// Validate config
	if config.WorkDir == "" {
		return "", fmt.Errorf("work_dir is required")
	}

	// Generate run ID first so we can do an atomic check-and-set
	runID := uuid.New().String()

	// Atomically try to claim this repo for our run.
	// If another run already claimed it, we fail immediately.
	if existingRunID, loaded := m.runsByRepo.LoadOrStore(config.WorkDir, runID); loaded {
		existingID := existingRunID.(string)
		var startedAt time.Time
		if runStateI, ok := m.runs.Load(existingID); ok {
			startedAt = runStateI.(*RunState).StartTime
		}
		return "", &canopyerrors.RunActiveError{
			RepoPath:  config.WorkDir,
			RunID:     existingID,
			StartedAt: startedAt,
		}
	}

	// We've claimed the repo. Clean up if we fail before completing setup.
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			m.runsByRepo.Delete(config.WorkDir)
		}
	}()

	// Apply defaults
	if config.Concurrency <= 0 {
		config.Concurrency = 4
	}
	if config.OutputDir == "" {
		config.OutputDir = config.WorkDir
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.MaxPriority == 0 {
		config.MaxPriority = -1 // No filter by default
	}

	// Create orchestrator config
	orchConfig := &orchestrator.Config{
		WorkDir:         config.WorkDir,
		OutputDir:       config.OutputDir,
		Concurrency:     config.Concurrency,
		Verbose:         config.Verbose,
		DryRun:          config.DryRun,
		UseBwrap:        config.UseBwrap,
		MaxRetries:      config.MaxRetries,
		MaxPriority:     config.MaxPriority,
		ResolverTimeout: config.ResolverTimeout,
	}

	// Create orchestrator
	orch, err := orchestrator.New(orchConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create orchestrator: %w", err)
	}

	// Create per-repo rules engine from config
	rulesConfig, err := loadRulesConfig(config.WorkDir)
	if err != nil {
		logging.Warn("failed to load rules config, using defaults", "error", err)
		rulesConfig = nil
	}
	rulesEngine := rules.NewEngine(rulesConfig)
	orch.SetRulesEngine(rulesEngine)

	// Create cancellable context for this run
	runCtx, cancel := context.WithCancel(ctx)

	// Create run state
	runState := &RunState{
		ID:        runID,
		RepoPath:  config.WorkDir,
		RepoID:    config.RepoID,
		Status:    RunStatusPending,
		Config:    config,
		StartTime: time.Now(),
		orch:      orch,
		cancel:    cancel,
	}

	// Register callbacks that publish directly to EventBus
	callbacks := m.createEventCallbacks(runID, config.RepoID)
	orch.SetCallbacks(callbacks)

	// Set run and repo IDs on the orchestrator for agent tracking
	orch.SetRunID(runID)
	if config.RepoID != "" {
		orch.SetRepoID(config.RepoID)
	}

	// Store run state
	m.runs.Store(runID, runState)

	// Get initial ready tasks count
	beadsClient, err := beads.NewClient(config.WorkDir)
	if err == nil {
		tasks, err := beadsClient.Ready(ctx)
		if err == nil {
			runState.mu.Lock()
			runState.TasksTotal = len(tasks)
			runState.mu.Unlock()
		}
	}

	// Publish run started event
	m.publishRunStarted(runID, runState.TasksTotal, config)

	// Start orchestrator in background goroutine
	go m.runOrchestrator(runCtx, runState)

	logging.Info("started orchestration run",
		"run_id", runID,
		"repo", config.WorkDir,
		"concurrency", config.Concurrency,
		"tasks", runState.TasksTotal)

	// Success - don't clean up the repo mapping
	cleanupOnError = false
	return runID, nil
}

// StopRun cancels a running orchestration.
// Returns an error if the run is not found or already completed.
func (m *OrchestratorManager) StopRun(runID string) error {
	runStateI, ok := m.runs.Load(runID)
	if !ok {
		return fmt.Errorf("run not found: %s", runID)
	}

	runState := runStateI.(*RunState)

	runState.mu.Lock()
	defer runState.mu.Unlock()

	if runState.Status != RunStatusRunning && runState.Status != RunStatusPending {
		return fmt.Errorf("run %s is not active (status: %s)", runID, runState.Status)
	}

	// Cancel the context to stop the orchestrator
	if runState.cancel != nil {
		runState.cancel()
	}

	runState.Status = RunStatusCancelled
	now := time.Now()
	runState.EndTime = &now

	logging.Info("cancelled orchestration run", "run_id", runID)

	return nil
}

// GetRunStatus returns the current status of a run.
func (m *OrchestratorManager) GetRunStatus(runID string) (*RunState, error) {
	runStateI, ok := m.runs.Load(runID)
	if !ok {
		return nil, fmt.Errorf("run not found: %s", runID)
	}

	runState := runStateI.(*RunState)

	runState.mu.RLock()
	defer runState.mu.RUnlock()

	// Return a copy without internal fields
	return &RunState{
		ID:          runState.ID,
		RepoPath:    runState.RepoPath,
		RepoID:      runState.RepoID,
		Status:      runState.Status,
		Config:      runState.Config,
		StartTime:   runState.StartTime,
		EndTime:     runState.EndTime,
		Error:       runState.Error,
		TasksTotal:  runState.TasksTotal,
		TasksDone:   runState.TasksDone,
		TasksFailed: runState.TasksFailed,
	}, nil
}

// ListRuns returns all run states (both active and completed).
func (m *OrchestratorManager) ListRuns() []*RunState {
	var runs []*RunState

	m.runs.Range(func(_, value interface{}) bool {
		runState := value.(*RunState)

		runState.mu.RLock()
		runs = append(runs, &RunState{
			ID:          runState.ID,
			RepoPath:    runState.RepoPath,
			RepoID:      runState.RepoID,
			Status:      runState.Status,
			Config:      runState.Config,
			StartTime:   runState.StartTime,
			EndTime:     runState.EndTime,
			Error:       runState.Error,
			TasksTotal:  runState.TasksTotal,
			TasksDone:   runState.TasksDone,
			TasksFailed: runState.TasksFailed,
		})
		runState.mu.RUnlock()

		return true
	})

	return runs
}

// GetActiveRunForRepo returns the active run for a repository, if any.
func (m *OrchestratorManager) GetActiveRunForRepo(repoPath string) (*RunState, error) {
	runIDI, ok := m.runsByRepo.Load(repoPath)
	if !ok {
		return nil, nil // No active run
	}

	return m.GetRunStatus(runIDI.(string))
}

// UpdateRunConfig updates the configuration of a running orchestration.
// Supports modifying concurrency, max priority filter, and pause state.
// Returns an error if the run is not found or not active.
func (m *OrchestratorManager) UpdateRunConfig(runID string, concurrency *int, maxPriority *int, paused *bool) error {
	runStateI, ok := m.runs.Load(runID)
	if !ok {
		return fmt.Errorf("run not found: %s", runID)
	}

	runState := runStateI.(*RunState)

	runState.mu.Lock()
	defer runState.mu.Unlock()

	if runState.Status != RunStatusRunning && runState.Status != RunStatusPending {
		return fmt.Errorf("run %s is not active (status: %s)", runID, runState.Status)
	}

	// Update concurrency if specified
	if concurrency != nil && *concurrency > 0 {
		runState.Config.Concurrency = *concurrency
		// Note: Runtime concurrency changes are recorded in config but cannot
		// be applied to active scheduler (semaphore doesn't support resizing).
		// The new concurrency will take effect on the next run.
		// TODO: Implement SetConcurrency on scheduler if dynamic resizing is needed.
		logging.Info("updated run concurrency", "run_id", runID, "concurrency", *concurrency)
	}

	// Update max priority filter if specified
	if maxPriority != nil {
		runState.Config.MaxPriority = *maxPriority
		// Note: MaxPriority changes will take effect on next task selection
		logging.Info("updated run max priority", "run_id", runID, "max_priority", *maxPriority)
	}

	// Update pause state if specified
	if paused != nil {
		if runState.orch != nil {
			sched := runState.orch.GetScheduler()
			if sched != nil {
				if *paused {
					sched.Pause()
					logging.Info("paused run", "run_id", runID)
				} else {
					sched.Resume()
					logging.Info("resumed run", "run_id", runID)
				}
			}
		}
	}

	return nil
}

// runOrchestrator executes the orchestrator and handles completion.
func (m *OrchestratorManager) runOrchestrator(ctx context.Context, runState *RunState) {
	// Update status to running
	runState.mu.Lock()
	runState.Status = RunStatusRunning
	runState.mu.Unlock()

	// Run the orchestrator
	err := runState.orch.Run(ctx)

	// Update run state on completion
	runState.mu.Lock()
	now := time.Now()
	runState.EndTime = &now

	if ctx.Err() == context.Canceled {
		runState.Status = RunStatusCancelled
	} else if err != nil {
		runState.Status = RunStatusFailed
		runState.Error = err.Error()
	} else {
		runState.Status = RunStatusCompleted
	}
	runState.mu.Unlock()

	// Remove from active runs by repo
	m.runsByRepo.Delete(runState.RepoPath)

	// Publish run completed event
	m.publishRunCompleted(runState)

	logging.Info("orchestration run completed",
		"run_id", runState.ID,
		"status", runState.Status,
		"tasks_done", runState.TasksDone,
		"tasks_failed", runState.TasksFailed)
}

// createEventCallbacks creates orchestrator event callbacks that publish to the EventBus.
// These callbacks replace the IPC-based callbacks when the orchestrator runs in the daemon.
func (m *OrchestratorManager) createEventCallbacks(runID, repoID string) *orchestrator.EventCallbacks {
	return &orchestrator.EventCallbacks{
		OnAgentStartFn: func(ctx context.Context, taskID string, task *beads.Task) {
			agentID := makeAgentID(runID, taskID)

			// Record agent ID for parent-child tracking
			// The orchestrator instance manages this via SetAgentID

			// Publish agent started event
			m.eventBus.Publish(events.Event{
				Type:      events.EventAgentStarted,
				Timestamp: time.Now(),
				Payload: map[string]interface{}{
					"agent_id":         agentID,
					"run_id":           runID,
					"task_id":          taskID,
					"task_title":       task.Title,
					"task_description": task.Description,
					"parent_agent_id":  "", // Top-level agents have no parent
					"repo_id":          repoID,
				},
			})

			// Update task status
			m.eventBus.Publish(events.Event{
				Type:      events.EventTaskUpdated,
				Timestamp: time.Now(),
				Payload: map[string]interface{}{
					"id":       taskID,
					"title":    task.Title,
					"status":   "in_progress",
					"agent_id": agentID,
					"repo_id":  repoID,
				},
			})
		},

		OnOutputFn: func(ctx context.Context, taskID string, output string, isError bool) {
			agentID := makeAgentID(runID, taskID)

			m.eventBus.Publish(events.Event{
				Type:      events.EventAgentOutput,
				Timestamp: time.Now(),
				Payload: map[string]interface{}{
					"agent_id": agentID,
					"output":   output,
					"is_error": isError,
				},
			})
		},

		OnLiveFeedFn: func(ctx context.Context, taskID string, event *agent.LiveFeedEvent) {
			agentID := makeAgentID(runID, taskID)

			m.eventBus.Publish(events.Event{
				Type:      events.EventAgentLiveFeed,
				Timestamp: time.Now(),
				Payload: map[string]interface{}{
					"agent_id":   agentID,
					"event_type": string(event.EventType),
					"data":       event.RawData,
				},
			})
		},

		OnDoneFn: func(ctx context.Context, taskID string, result *agent.Result) {
			agentID := makeAgentID(runID, taskID)

			// Update run state counters
			if runStateI, ok := m.runs.Load(runID); ok {
				runState := runStateI.(*RunState)
				runState.mu.Lock()
				runState.TasksDone++
				runState.mu.Unlock()
			}

			// Build completion payload
			payload := m.buildCompletionPayload(agentID, result)

			m.eventBus.Publish(events.Event{
				Type:      events.EventAgentCompleted,
				Timestamp: time.Now(),
				Payload:   payload,
			})

			// Update task status
			m.eventBus.Publish(events.Event{
				Type:      events.EventTaskUpdated,
				Timestamp: time.Now(),
				Payload: map[string]interface{}{
					"id":       taskID,
					"status":   "completed",
					"agent_id": agentID,
					"repo_id":  repoID,
				},
			})
		},

		OnFailFn: func(ctx context.Context, taskID string, result *agent.Result) {
			agentID := makeAgentID(runID, taskID)

			// Update run state counters
			if runStateI, ok := m.runs.Load(runID); ok {
				runState := runStateI.(*RunState)
				runState.mu.Lock()
				runState.TasksFailed++
				runState.mu.Unlock()
			}

			// Build failure payload
			payload := m.buildCompletionPayload(agentID, result)
			payload["error"] = result.Error

			m.eventBus.Publish(events.Event{
				Type:      events.EventAgentFailed,
				Timestamp: time.Now(),
				Payload:   payload,
			})

			// Update task status
			m.eventBus.Publish(events.Event{
				Type:      events.EventTaskUpdated,
				Timestamp: time.Now(),
				Payload: map[string]interface{}{
					"id":       taskID,
					"status":   "failed",
					"agent_id": agentID,
					"repo_id":  repoID,
				},
			})
		},
	}
}

// buildCompletionPayload creates the payload for agent completion events.
func (m *OrchestratorManager) buildCompletionPayload(agentID string, result *agent.Result) map[string]interface{} {
	payload := map[string]interface{}{
		"agent_id":      agentID,
		"exit_code":     result.ExitCode,
		"duration":      result.Duration.Seconds(),
		"files_changed": len(result.Changes),
	}

	if result.Output != nil {
		payload["input_tokens"] = result.Output.TotalInputTokens
		payload["output_tokens"] = result.Output.TotalOutputTokens
		payload["cache_creation_input_tokens"] = result.Output.CacheCreationInputTokens
		payload["cache_read_input_tokens"] = result.Output.CacheReadInputTokens
		payload["cost_usd"] = result.Output.CostUSD
		payload["duration_ms"] = result.Output.DurationMS
		payload["duration_api_ms"] = result.Output.DurationAPIMS
		payload["num_turns"] = result.Output.NumTurns
		payload["result_message"] = result.Output.ResultMessage
		payload["session_id"] = result.Output.SessionID

		// Convert model usage
		if result.Output.ModelUsage != nil {
			modelUsage := make(map[string]interface{})
			for model, usage := range result.Output.ModelUsage {
				modelUsage[model] = map[string]interface{}{
					"input_tokens":               usage.InputTokens,
					"output_tokens":              usage.OutputTokens,
					"cache_read_input_tokens":    usage.CacheReadInputTokens,
					"cache_creation_input_tokens": usage.CacheCreationInputTokens,
					"cost_usd":                   usage.CostUSD,
				}
			}
			payload["model_usage"] = modelUsage
		}
	}

	if result.GitState != nil {
		payload["commits_created"] = len(result.GitState.NewCommits)
	}

	return payload
}

// publishRunStarted publishes a run started event.
func (m *OrchestratorManager) publishRunStarted(runID string, taskCount int, config RunConfig) {
	m.eventBus.Publish(events.Event{
		Type:      events.EventRunStarted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id":     runID,
			"task_count": taskCount,
			"repo_id":    config.RepoID,
			"repo_path":  config.WorkDir,
		},
	})
}

// publishRunCompleted publishes a run completed event.
func (m *OrchestratorManager) publishRunCompleted(runState *RunState) {
	runState.mu.RLock()
	defer runState.mu.RUnlock()

	var duration float64
	if runState.EndTime != nil {
		duration = runState.EndTime.Sub(runState.StartTime).Seconds()
	}

	m.eventBus.Publish(events.Event{
		Type:      events.EventRunCompleted,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"run_id": runState.ID,
			"stats": map[string]interface{}{
				"total_tasks":     runState.TasksTotal,
				"succeeded_tasks": runState.TasksDone,
				"failed_tasks":    runState.TasksFailed,
				"total_duration":  duration,
			},
		},
	})
}

// makeAgentID creates a unique agent ID by combining run ID prefix with task ID.
// Format: agent-{runID[:8]}-{taskID}
func makeAgentID(runID, taskID string) string {
	prefix := runID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return fmt.Sprintf("agent-%s-%s", prefix, taskID)
}

// loadRulesConfig loads the rules configuration from the repository's config file.
// Returns nil (no rules) if no config exists or if rules are not configured.
func loadRulesConfig(workDir string) (*config.RulesSettings, error) {
	cfg, err := config.LoadConfig(workDir)
	if err != nil {
		// No config file is OK - just use no rules
		return nil, nil
	}

	// Return a copy of the rules settings
	rules := cfg.Rules
	return &rules, nil
}
