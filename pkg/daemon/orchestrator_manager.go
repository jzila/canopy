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

// OrchestratorState represents the state of an always-running orchestrator.
type OrchestratorState string

const (
	// OrchestratorOff means the orchestrator has no instance and no goroutine running.
	// This is the initial state before Activate() and the state after Deactivate().
	OrchestratorOff OrchestratorState = "off"
	// OrchestratorIdle means the orchestrator is running but not processing tasks.
	OrchestratorIdle OrchestratorState = "idle"
	// OrchestratorActive means the orchestrator is actively processing tasks.
	OrchestratorActive OrchestratorState = "active"
	// OrchestratorPaused means the orchestrator is paused (tasks queued but not processed).
	OrchestratorPaused OrchestratorState = "paused"
)

// OrchestratorLifecycle represents an always-running orchestrator for a registered repository.
// The orchestrator goroutine starts when Activate() is called and stops when Deactivate() is called.
//
// State transitions:
//   - Off → Idle: Activate() creates orchestrator instance and starts Run() goroutine
//   - Idle/Active/Paused → Off: Deactivate() cancels context, waits for goroutine, destroys instance
//   - Idle → Active: When tasks are available (internal to orchestrator)
//   - Active → Idle: When no tasks are available (internal to orchestrator)
//   - Active ↔ Paused: Via pause/resume (internal to orchestrator)
type OrchestratorLifecycle struct {
	RepoPath string            `json:"repo_path"`
	RepoID   string            `json:"repo_id"`
	State    OrchestratorState `json:"state"`

	// Current run information (only valid when State != OrchestratorOff)
	RunID       string     `json:"run_id,omitempty"`
	RunConfig   RunConfig  `json:"run_config,omitempty"`
	StartTime   time.Time  `json:"start_time,omitempty"`
	TasksTotal  int        `json:"tasks_total,omitempty"`
	TasksDone   int        `json:"tasks_done,omitempty"`
	TasksFailed int        `json:"tasks_failed,omitempty"`

	// Internal: orchestrator instance and control
	orch        *orchestrator.Orchestrator
	rulesEngine *rules.Engine
	cancel      context.CancelFunc
	wg          sync.WaitGroup // Tracks the Run() goroutine
	mu          sync.RWMutex
}

// Activate transitions the orchestrator from Off → Idle state.
// It creates the Orchestrator instance and starts the Run() goroutine.
// A new RunID is generated for this activation period.
//
// Returns the generated RunID and an error if:
//   - The orchestrator is not in Off state
//   - The Orchestrator instance cannot be created
func (l *OrchestratorLifecycle) Activate(ctx context.Context, cfg RunConfig) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.State != OrchestratorOff {
		return "", fmt.Errorf("cannot activate: orchestrator is in %s state, expected off", l.State)
	}

	// Apply defaults
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = l.RepoPath
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = cfg.WorkDir
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.MaxPriority == 0 {
		cfg.MaxPriority = -1 // No filter by default
	}

	// Create orchestrator config
	orchConfig := &orchestrator.Config{
		WorkDir:         cfg.WorkDir,
		OutputDir:       cfg.OutputDir,
		Concurrency:     cfg.Concurrency,
		Verbose:         cfg.Verbose,
		DryRun:          cfg.DryRun,
		UseBwrap:        cfg.UseBwrap,
		MaxRetries:      cfg.MaxRetries,
		MaxPriority:     cfg.MaxPriority,
		ResolverTimeout: cfg.ResolverTimeout,
	}

	// Create orchestrator instance
	orch, err := orchestrator.New(orchConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create orchestrator: %w", err)
	}

	// Generate run ID for this activation period
	runID := uuid.New().String()

	// Create cancellable context for this activation
	runCtx, cancel := context.WithCancel(ctx)

	// Update lifecycle state
	l.State = OrchestratorIdle
	l.RunID = runID
	l.RunConfig = cfg
	l.StartTime = time.Now()
	l.TasksTotal = 0
	l.TasksDone = 0
	l.TasksFailed = 0
	l.orch = orch
	l.cancel = cancel

	// Update rules engine to use the orchestrator's engine for consistency
	if orchEngine := orch.GetRulesEngine(); orchEngine != nil {
		l.rulesEngine = orchEngine
	}

	// Set run and repo IDs on the orchestrator
	orch.SetRunID(runID)
	if l.RepoID != "" {
		orch.SetRepoID(l.RepoID)
	}

	// Start the Run() goroutine
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		_ = orch.Run(runCtx) // Error is logged internally
	}()

	logging.Info("activated orchestrator",
		"repo_path", l.RepoPath,
		"run_id", runID,
		"concurrency", cfg.Concurrency)

	return runID, nil
}

// Deactivate transitions the orchestrator from any state → Off.
// It cancels the orchestrator context, waits for the Run() goroutine to exit,
// and destroys the Orchestrator instance.
//
// Returns an error if:
//   - The orchestrator is already in Off state
func (l *OrchestratorLifecycle) Deactivate() error {
	l.mu.Lock()

	if l.State == OrchestratorOff {
		l.mu.Unlock()
		return fmt.Errorf("cannot deactivate: orchestrator is already off")
	}

	runID := l.RunID

	// Cancel the context to stop the orchestrator
	if l.cancel != nil {
		l.cancel()
	}

	// Release lock before waiting for goroutine to exit (avoids deadlock)
	l.mu.Unlock()

	// Wait for the Run() goroutine to exit
	l.wg.Wait()

	// Re-acquire lock to clean up state
	l.mu.Lock()
	defer l.mu.Unlock()

	// Clean up resources
	l.State = OrchestratorOff
	l.orch = nil
	l.cancel = nil
	l.RunID = ""
	l.RunConfig = RunConfig{}
	l.StartTime = time.Time{}

	// Keep rulesEngine for potential continued rules access
	// Reload from config to ensure fresh state
	if cfg, err := config.LoadConfig(l.RepoPath); err == nil {
		l.rulesEngine = rules.NewEngine(&cfg.Rules)
	}

	logging.Info("deactivated orchestrator",
		"repo_path", l.RepoPath,
		"run_id", runID)

	return nil
}

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
//
// The manager implements an always-running orchestrator model:
// - Orchestrators start when a repo is registered with the daemon
// - Orchestrators stop when a repo is unregistered
// - "Starting a run" = state transition (idle → active), not object creation
// - "Stopping a run" = state transition (active → idle), not object destruction
type OrchestratorManager struct {
	// Active runs indexed by run ID (for backwards compatibility during transition)
	runs sync.Map // map[string]*RunState

	// Active runs indexed by repo path (for single-run-per-repo enforcement)
	runsByRepo sync.Map // map[string]string (repo path -> run ID)

	// Always-running orchestrators indexed by repo path
	// These are created when a repo is registered and destroyed when unregistered
	orchestrators sync.Map // map[string]*OrchestratorLifecycle (repo path -> orchestrator)

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

// RegisterRepo creates an OrchestratorLifecycle for a repository.
// The orchestrator starts in OFF state and waits for activation via Activate().
// If repoID is empty, it will be derived from the path.
// Returns the OrchestratorLifecycle or an error if registration fails.
func (m *OrchestratorManager) RegisterRepo(repoPath string, repoID string) (*OrchestratorLifecycle, error) {
	// Check if already registered
	if existing, ok := m.orchestrators.Load(repoPath); ok {
		return existing.(*OrchestratorLifecycle), nil
	}

	// Load config to create rules engine
	cfg, err := config.LoadConfig(repoPath)
	if err != nil {
		// Log warning but continue with defaults - config loading should not fail registration
		logging.Warn("failed to load config.toml for repo, using defaults",
			"repo_path", repoPath, "error", err)
		cfg = config.DefaultConfig()
	}

	// Create rules engine from config
	rulesEngine := rules.NewEngine(&cfg.Rules)

	// Create the OrchestratorLifecycle in OFF state
	repoOrch := &OrchestratorLifecycle{
		RepoPath:    repoPath,
		RepoID:      repoID,
		State:       OrchestratorOff,
		rulesEngine: rulesEngine,
	}

	// Store atomically - another goroutine may have registered in parallel
	if existing, loaded := m.orchestrators.LoadOrStore(repoPath, repoOrch); loaded {
		return existing.(*OrchestratorLifecycle), nil
	}

	logging.Info("registered repo orchestrator", "repo_path", repoPath, "state", OrchestratorOff)

	return repoOrch, nil
}

// UnregisterRepo stops and removes the orchestrator for a repository.
// If the orchestrator is active, it will be stopped first.
// Returns an error if the repo is not registered.
func (m *OrchestratorManager) UnregisterRepo(repoPath string) error {
	repoOrchI, ok := m.orchestrators.Load(repoPath)
	if !ok {
		return fmt.Errorf("repo not registered: %s", repoPath)
	}

	repoOrch := repoOrchI.(*OrchestratorLifecycle)

	repoOrch.mu.Lock()
	defer repoOrch.mu.Unlock()

	// Cancel any running orchestrator
	if repoOrch.cancel != nil {
		repoOrch.cancel()
		repoOrch.cancel = nil
	}

	// Remove from registry
	m.orchestrators.Delete(repoPath)

	// Also clean up any active run tracking
	if repoOrch.RunID != "" {
		m.runs.Delete(repoOrch.RunID)
		m.runsByRepo.Delete(repoPath)
	}

	logging.Info("unregistered repo orchestrator", "repo_path", repoPath)

	return nil
}

// GetOrchestratorLifecycle returns the orchestrator for a repository, if registered.
// Returns nil if the repo is not registered.
func (m *OrchestratorManager) GetOrchestratorLifecycle(repoPath string) *OrchestratorLifecycle {
	if repoOrchI, ok := m.orchestrators.Load(repoPath); ok {
		return repoOrchI.(*OrchestratorLifecycle)
	}
	return nil
}

// GetOrchestratorState returns the current state of the orchestrator for a repo.
// Returns empty string if the repo is not registered.
func (m *OrchestratorManager) GetOrchestratorState(repoPath string) OrchestratorState {
	if repoOrch := m.GetOrchestratorLifecycle(repoPath); repoOrch != nil {
		repoOrch.mu.RLock()
		defer repoOrch.mu.RUnlock()
		return repoOrch.State
	}
	return ""
}

// ListRegisteredRepos returns the paths of all registered repositories.
func (m *OrchestratorManager) ListRegisteredRepos() []string {
	var repos []string
	m.orchestrators.Range(func(key, _ interface{}) bool {
		repos = append(repos, key.(string))
		return true
	})
	return repos
}

// GetRulesEngineForRepo returns the rules engine for a repository.
// First checks for an always-running orchestrator, then falls back to active run.
// Returns nil if no orchestrator exists for the repository.
func (m *OrchestratorManager) GetRulesEngineForRepo(repoPath string) *rules.Engine {
	// Check for always-running orchestrator first (preferred path)
	if repoOrchI, ok := m.orchestrators.Load(repoPath); ok {
		repoOrch := repoOrchI.(*OrchestratorLifecycle)
		repoOrch.mu.RLock()
		engine := repoOrch.rulesEngine
		repoOrch.mu.RUnlock()
		if engine != nil {
			return engine
		}
	}

	// Fall back to active run (backwards compatibility during transition)
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

// GetOrCreateRulesEngineForRepo returns a rules engine for a repository.
// If an always-running orchestrator or active run exists, returns that engine.
// Otherwise, creates/returns an engine loaded from config for the repo.
// Returns the engine and nil error on success, or nil and an error on failure.
func (m *OrchestratorManager) GetOrCreateRulesEngineForRepo(repoPath string) (*rules.Engine, error) {
	// First, check if there's an always-running orchestrator or active run
	if engine := m.GetRulesEngineForRepo(repoPath); engine != nil {
		return engine, nil
	}

	// No orchestrator yet - check if we have one registered but without an engine
	if repoOrchI, ok := m.orchestrators.Load(repoPath); ok {
		repoOrch := repoOrchI.(*OrchestratorLifecycle)
		repoOrch.mu.Lock()
		defer repoOrch.mu.Unlock()

		// Double-check after acquiring lock
		if repoOrch.rulesEngine != nil {
			return repoOrch.rulesEngine, nil
		}

		// Create engine for the registered orchestrator
		cfg, err := config.LoadConfig(repoPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config for %s: %w", repoPath, err)
		}

		repoOrch.rulesEngine = rules.NewEngine(&cfg.Rules)
		return repoOrch.rulesEngine, nil
	}

	// No orchestrator registered - auto-register one for this repo (lazy registration)
	// This provides backwards compatibility: rules can be accessed without explicit registration
	repoOrch, err := m.RegisterRepo(repoPath, "")
	if err != nil {
		return nil, fmt.Errorf("failed to register repo %s: %w", repoPath, err)
	}

	return repoOrch.rulesEngine, nil
}

// InvalidateRulesEngine reloads the rules engine for a repo from config.
// This should be called when the repo's config changes.
// For backwards compatibility, this is also aliased as InvalidateStandaloneEngine.
func (m *OrchestratorManager) InvalidateRulesEngine(repoPath string) {
	if repoOrchI, ok := m.orchestrators.Load(repoPath); ok {
		repoOrch := repoOrchI.(*OrchestratorLifecycle)
		repoOrch.mu.Lock()
		defer repoOrch.mu.Unlock()

		// Reload config and create new engine
		cfg, err := config.LoadConfig(repoPath)
		if err != nil {
			logging.Warn("failed to reload config for rules invalidation",
				"repo_path", repoPath, "error", err)
			return
		}

		repoOrch.rulesEngine = rules.NewEngine(&cfg.Rules)
	}
}

// InvalidateStandaloneEngine is deprecated. Use InvalidateRulesEngine instead.
// Kept for backwards compatibility.
func (m *OrchestratorManager) InvalidateStandaloneEngine(repoPath string) {
	m.InvalidateRulesEngine(repoPath)
}

// GetRepoAPI returns a RepoAPI for a repository.
// If an active run exists, returns a RepoAPI backed by the run's orchestrator.
// Otherwise, creates/returns a RepoAPI backed by a standalone rules engine.
// Returns the RepoAPI and nil error on success, or nil and an error on failure.
func (m *OrchestratorManager) GetRepoAPI(repoPath string) (orchestrator.RepoAPI, error) {
	// First, check if there's an active run for this repo
	runIDI, ok := m.runsByRepo.Load(repoPath)
	if ok {
		runStateI, ok := m.runs.Load(runIDI.(string))
		if ok {
			runState := runStateI.(*RunState)
			if runState.orch != nil {
				return orchestrator.NewRepoAPI(runState.orch, repoPath), nil
			}
		}
	}

	// No active run - get or create a standalone rules engine
	engine, err := m.GetOrCreateRulesEngineForRepo(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get rules engine for %s: %w", repoPath, err)
	}

	return orchestrator.NewStandaloneRepoAPI(engine, repoPath), nil
}

// GetRepoAPIForRun returns a RepoAPI for a specific run.
// Returns nil and an error if the run doesn't exist or has no orchestrator.
func (m *OrchestratorManager) GetRepoAPIForRun(runID string) (orchestrator.RepoAPI, error) {
	runStateI, ok := m.runs.Load(runID)
	if !ok {
		return nil, fmt.Errorf("run not found: %s", runID)
	}

	runState := runStateI.(*RunState)
	if runState.orch == nil {
		return nil, fmt.Errorf("run %s has no orchestrator", runID)
	}

	return orchestrator.NewRepoAPI(runState.orch, runState.RepoPath), nil
}

// StartRun activates the orchestrator for a repository.
// This is an alias for Activate() during the transition period, with additional
// backwards-compatible RunState tracking and event publishing.
//
// Returns the run ID or an error if the run could not be started.
// Only one run per repository is allowed at a time.
func (m *OrchestratorManager) StartRun(ctx context.Context, config RunConfig) (string, error) {
	// Validate config
	if config.WorkDir == "" {
		return "", fmt.Errorf("work_dir is required")
	}

	// Get or create the OrchestratorLifecycle for this repo
	repoOrch, err := m.RegisterRepo(config.WorkDir, config.RepoID)
	if err != nil {
		return "", fmt.Errorf("failed to register repo: %w", err)
	}

	// Check state under lock - Activate expects Off state
	repoOrch.mu.RLock()
	currentState := repoOrch.State
	currentRunID := repoOrch.RunID
	startTime := repoOrch.StartTime
	repoOrch.mu.RUnlock()

	// Return error if not in Off state (already activated)
	if currentState != OrchestratorOff {
		return "", &canopyerrors.RunActiveError{
			RepoPath:  config.WorkDir,
			RunID:     currentRunID,
			StartedAt: startTime,
		}
	}

	// Try to claim the repo in runsByRepo for backwards compatibility
	// Use a placeholder until we get the real runID from Activate
	placeholder := "pending-" + config.WorkDir
	if existingRunID, loaded := m.runsByRepo.LoadOrStore(config.WorkDir, placeholder); loaded {
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

	// Call Activate to do the core work
	runID, err := repoOrch.Activate(ctx, config)
	if err != nil {
		return "", err
	}

	// Update the runsByRepo with the real runID
	m.runsByRepo.Store(config.WorkDir, runID)

	// Get orchestrator and cancel from lifecycle for RunState
	repoOrch.mu.RLock()
	orch := repoOrch.orch
	cancel := repoOrch.cancel
	runStartTime := repoOrch.StartTime
	repoOrch.mu.RUnlock()

	// Create run state for backwards compatibility
	runState := &RunState{
		ID:        runID,
		RepoPath:  config.WorkDir,
		RepoID:    config.RepoID,
		Status:    RunStatusPending,
		Config:    config,
		StartTime: runStartTime,
		orch:      orch,
		cancel:    cancel,
	}

	// Register callbacks that publish directly to EventBus
	callbacks := m.createEventCallbacks(runID, config.RepoID)
	orch.SetCallbacks(callbacks)

	// Store run state for backwards compatibility
	m.runs.Store(runID, runState)

	// Get initial ready tasks count
	beadsClient, err := beads.NewClient(config.WorkDir)
	if err == nil {
		tasks, err := beadsClient.Ready(ctx)
		if err == nil {
			runState.mu.Lock()
			runState.TasksTotal = len(tasks)
			runState.mu.Unlock()

			repoOrch.mu.Lock()
			repoOrch.TasksTotal = len(tasks)
			repoOrch.mu.Unlock()
		}
	}

	// Publish run started event
	m.publishRunStarted(runID, runState.TasksTotal, config)

	// Start a monitoring goroutine that waits for the lifecycle's Run goroutine
	// and does cleanup when it completes
	go m.monitorActivation(repoOrch, runState)

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
// This is an alias for Deactivate() during the transition period.
// It transitions the orchestrator state from any running state → Off.
// Returns an error if the run is not found or already completed.
func (m *OrchestratorManager) StopRun(runID string) error {
	runStateI, ok := m.runs.Load(runID)
	if !ok {
		return fmt.Errorf("run not found: %s", runID)
	}

	runState := runStateI.(*RunState)

	runState.mu.Lock()
	if runState.Status != RunStatusRunning && runState.Status != RunStatusPending {
		runState.mu.Unlock()
		return fmt.Errorf("run %s is not active (status: %s)", runID, runState.Status)
	}
	repoPath := runState.RepoPath
	runState.mu.Unlock()

	// Get the lifecycle and call Deactivate
	repoOrch := m.GetOrchestratorLifecycle(repoPath)
	if repoOrch == nil {
		return fmt.Errorf("orchestrator lifecycle not found for repo: %s", repoPath)
	}

	// Call Deactivate - this cancels context, waits for goroutine, and cleans up
	if err := repoOrch.Deactivate(); err != nil {
		// If already off, that's fine - the run may have completed
		if repoOrch.State != OrchestratorOff {
			return fmt.Errorf("failed to deactivate: %w", err)
		}
	}

	// Update RunState for backwards compatibility
	runState.mu.Lock()
	runState.Status = RunStatusCancelled
	now := time.Now()
	runState.EndTime = &now
	runState.mu.Unlock()

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
// It updates both the RunState (for backwards compatibility) and the OrchestratorLifecycle.
// Deprecated: Use monitorActivation instead when using Activate().
func (m *OrchestratorManager) runOrchestrator(ctx context.Context, runState *RunState, repoOrch *OrchestratorLifecycle) {
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

	// Transition OrchestratorLifecycle back to IDLE state
	if repoOrch != nil {
		repoOrch.mu.Lock()
		repoOrch.State = OrchestratorIdle
		repoOrch.orch = nil
		repoOrch.cancel = nil
		// Keep rulesEngine for continued rules access in IDLE state
		// Reload from config to ensure fresh state
		if cfg, err := config.LoadConfig(repoOrch.RepoPath); err == nil {
			repoOrch.rulesEngine = rules.NewEngine(&cfg.Rules)
		}
		repoOrch.mu.Unlock()
	}

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

// monitorActivation monitors an activated orchestrator and handles completion.
// It waits for the lifecycle's Run goroutine to complete and updates RunState accordingly.
// This is used by StartRun when delegating to Activate().
func (m *OrchestratorManager) monitorActivation(repoOrch *OrchestratorLifecycle, runState *RunState) {
	// Update status to running
	runState.mu.Lock()
	runState.Status = RunStatusRunning
	runState.mu.Unlock()

	// Wait for the lifecycle's Run goroutine to complete
	repoOrch.wg.Wait()

	// Determine final status based on lifecycle state
	repoOrch.mu.RLock()
	lifecycleState := repoOrch.State
	repoOrch.mu.RUnlock()

	// Update run state on completion
	runState.mu.Lock()
	now := time.Now()
	runState.EndTime = &now

	// The lifecycle transitions to Off after completion via its own cleanup
	// or stays in Idle if it completed normally. Check what we got.
	if lifecycleState == OrchestratorOff {
		// Normal completion or cancellation - Deactivate was called
		if runState.cancel != nil {
			// Check if context was cancelled
			runState.Status = RunStatusCancelled
		} else {
			runState.Status = RunStatusCompleted
		}
	} else {
		// Lifecycle is still in some other state - run completed normally
		runState.Status = RunStatusCompleted
	}
	runState.mu.Unlock()

	// Note: The lifecycle's Deactivate() or the orchestrator's Run() completion
	// already handles the state transition. We just clean up the manager's tracking.

	// If the lifecycle didn't transition to Off, we need to clean it up
	// This happens when Run() completes normally without explicit Deactivate()
	repoOrch.mu.Lock()
	if repoOrch.State != OrchestratorOff {
		repoOrch.State = OrchestratorOff
		repoOrch.orch = nil
		repoOrch.cancel = nil
		// Keep rulesEngine for continued rules access
		if cfg, err := config.LoadConfig(repoOrch.RepoPath); err == nil {
			repoOrch.rulesEngine = rules.NewEngine(&cfg.Rules)
		}
	}
	repoOrch.mu.Unlock()

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

// KillAgent terminates a running agent by its ID.
// It searches all active orchestrators to find the one running this agent.
// Returns an error if the agent is not found or cannot be killed.
func (m *OrchestratorManager) KillAgent(agentID string) error {
	// Search through all active runs for the agent
	var killErr error
	found := false

	m.runs.Range(func(_, value interface{}) bool {
		runState := value.(*RunState)
		runState.mu.RLock()
		orch := runState.orch
		runState.mu.RUnlock()

		if orch != nil {
			sched := orch.GetScheduler()
			if sched != nil {
				// Try to kill via this scheduler
				err := sched.Kill(agentID)
				if err == nil {
					found = true
					return false // Stop iterating
				}
				// Agent not found in this scheduler, continue
			}
		}
		return true
	})

	if !found {
		// Also try orchestrator lifecycles
		m.orchestrators.Range(func(_, value interface{}) bool {
			lifecycle := value.(*OrchestratorLifecycle)
			lifecycle.mu.RLock()
			orch := lifecycle.orch
			lifecycle.mu.RUnlock()

			if orch != nil {
				sched := orch.GetScheduler()
				if sched != nil {
					err := sched.Kill(agentID)
					if err == nil {
						found = true
						return false
					}
				}
			}
			return true
		})
	}

	if !found {
		return fmt.Errorf("agent %s not found in any active orchestrator", agentID)
	}

	return killErr
}
