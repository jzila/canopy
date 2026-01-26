package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	cfgpkg "github.com/jzila/canopy/pkg/config"
	canopyerrors "github.com/jzila/canopy/pkg/errors"
	"github.com/jzila/canopy/pkg/events"
	"github.com/jzila/canopy/pkg/lifecycle"
	"github.com/jzila/canopy/pkg/logging"
	"github.com/jzila/canopy/pkg/mergequeue"
	"github.com/jzila/canopy/pkg/orchestrator"
	"github.com/jzila/canopy/pkg/resolver"
	"github.com/jzila/canopy/pkg/rules"
	"github.com/jzila/canopy/pkg/types"
)

// RunConfig holds configuration for starting a new orchestration run.
// This mirrors orchestrator.Config but is used at the IPC/daemon boundary.
// Task selection parameters (priority, type, labels, assignee) are handled via
// RuleOverrides or direct RulesSettings fields, not as top-level config.
type RunConfig struct {
	WorkDir         string        `json:"work_dir"`
	OutputDir       string        `json:"output_dir,omitempty"`
	Concurrency     int           `json:"concurrency,omitempty"`
	Verbose         bool          `json:"verbose,omitempty"`
	DryRun          bool          `json:"dry_run,omitempty"`
	UseBwrap        bool          `json:"use_bwrap,omitempty"`
	MaxRetries      int           `json:"max_retries,omitempty"`
	ResolverTimeout time.Duration `json:"resolver_timeout,omitempty"`
	PollInterval    time.Duration `json:"poll_interval,omitempty"`
	RepoID          string        `json:"repo_id,omitempty"`
	// Task selection settings (applied to RulesSettings)
	PriorityMax   int      `json:"priority_max,omitempty"` // Max priority filter (-1 = no filter)
	Types         []string `json:"types,omitempty"`
	ExcludeTypes  []string `json:"exclude_types,omitempty"`
	Labels        []string `json:"labels,omitempty"`
	ExcludeLabels []string `json:"exclude_labels,omitempty"`
	Assignee      string   `json:"assignee,omitempty"`
	// RuleOverrides contains custom rules to apply for this run only.
	// These rules are applied with the highest precedence (above config.toml rules).
	// When the run ends, these overrides are discarded unless persisted.
	RuleOverrides []cfgpkg.CustomRule `json:"rule_overrides,omitempty"`
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
	// OrchestratorOff means no orchestrator instance exists (not activated, not watching).
	OrchestratorOff OrchestratorState = "off"
	// OrchestratorIdle means the orchestrator is running, polling for work, but none available.
	OrchestratorIdle OrchestratorState = "idle"
	// OrchestratorActive means the orchestrator is actively processing tasks.
	OrchestratorActive OrchestratorState = "active"
	// OrchestratorPaused means the orchestrator is running but scheduler is paused.
	OrchestratorPaused OrchestratorState = "paused"
)

// OrchestratorLifecycle manages the lifecycle of an always-running orchestrator for a repository.
// It wraps orchestrator instances and manages state transitions between off, idle, active, and paused.
//
// State machine:
//   - Off: Not activated, no orchestrator instance exists
//   - Idle: Activated and watching for work, but no tasks currently available
//   - Active: Processing tasks
//   - Paused: Temporarily stopped by user or agent, will resume when unpaused
//
// The orchestrator starts when Activate() is called and stops when Deactivate() is called.
// State transitions between Idle and Active happen automatically based on work availability.
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
	mu          sync.RWMutex
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

// RegisterRepo creates an always-running orchestrator for a repository.
// The orchestrator starts in IDLE state and waits for activation (StartRun).
// If repoID is empty, it will be derived from the path.
// Returns the OrchestratorLifecycle or an error if registration fails.
func (m *OrchestratorManager) RegisterRepo(repoPath string, repoID string) (*OrchestratorLifecycle, error) {
	// Check if already registered
	if existing, ok := m.orchestrators.Load(repoPath); ok {
		return existing.(*OrchestratorLifecycle), nil
	}

	// Load config to create rules engine
	cfg, err := cfgpkg.LoadConfig(repoPath)
	if err != nil {
		// Log warning but continue with defaults - config loading should not fail registration
		logging.Warn("failed to load config.toml for repo, using defaults",
			"repo_path", repoPath, "error", err)
		cfg = cfgpkg.DefaultConfig()
	}

	// Create rules engine from config
	rulesEngine := rules.NewEngine(&cfg.Rules)

	// Create the OrchestratorLifecycle in OFF state (registered but not activated)
	lifecycle := &OrchestratorLifecycle{
		RepoPath:    repoPath,
		RepoID:      repoID,
		State:       OrchestratorOff,
		rulesEngine: rulesEngine,
	}

	// Store atomically - another goroutine may have registered in parallel
	if existing, loaded := m.orchestrators.LoadOrStore(repoPath, lifecycle); loaded {
		return existing.(*OrchestratorLifecycle), nil
	}

	logging.Info("registered repo orchestrator", "repo_path", repoPath, "state", OrchestratorOff)

	return lifecycle, nil
}

// UnregisterRepo stops and removes the orchestrator for a repository.
// If the orchestrator is active, it will be stopped first with graceful cleanup.
// This waits for active overlays to be cleaned up before returning.
// Returns an error if the repo is not registered.
func (m *OrchestratorManager) UnregisterRepo(repoPath string) error {
	lifecycleI, ok := m.orchestrators.Load(repoPath)
	if !ok {
		return fmt.Errorf("repo not registered: %s", repoPath)
	}

	lifecycle := lifecycleI.(*OrchestratorLifecycle)

	lifecycle.mu.Lock()

	// Get orchestrator reference before cancelling
	orch := lifecycle.orch

	// Cancel any running orchestrator
	if lifecycle.cancel != nil {
		lifecycle.cancel()
		lifecycle.cancel = nil
	}

	lifecycle.mu.Unlock()

	// Wait for orchestrator cleanup outside the lock to avoid blocking other operations
	if orch != nil {
		const shutdownTimeout = 30 * time.Second
		if cleaned, err := orch.Shutdown(shutdownTimeout); err != nil {
			logging.Warn("overlay cleanup during unregister had errors",
				"repo_path", repoPath,
				"overlays_cleaned", cleaned,
				"error", err,
			)
		} else if cleaned > 0 {
			logging.Info("cleaned up overlays during unregister",
				"repo_path", repoPath,
				"overlays_cleaned", cleaned,
			)
		}
	}

	// Remove from registry
	m.orchestrators.Delete(repoPath)

	// Also clean up any active run tracking
	lifecycle.mu.Lock()
	runID := lifecycle.RunID
	lifecycle.mu.Unlock()

	if runID != "" {
		m.runs.Delete(runID)
		m.runsByRepo.Delete(repoPath)
	}

	logging.Info("unregistered repo orchestrator", "repo_path", repoPath)

	return nil
}

// GetOrchestratorLifecycle returns the orchestrator for a repository, if registered.
// Returns nil if the repo is not registered.
func (m *OrchestratorManager) GetOrchestratorLifecycle(repoPath string) *OrchestratorLifecycle {
	if lifecycleI, ok := m.orchestrators.Load(repoPath); ok {
		return lifecycleI.(*OrchestratorLifecycle)
	}
	return nil
}

// GetOrchestratorState returns the current state of the orchestrator for a repo.
// Returns empty string if the repo is not registered.
func (m *OrchestratorManager) GetOrchestratorState(repoPath string) OrchestratorState {
	if lifecycle := m.GetOrchestratorLifecycle(repoPath); lifecycle != nil {
		lifecycle.mu.RLock()
		defer lifecycle.mu.RUnlock()
		return lifecycle.State
	}
	return ""
}

// SetOrchestratorPaused transitions the orchestrator to paused or unpaused state.
// When paused=true, state transitions to OrchestratorPaused.
// When paused=false, state transitions to OrchestratorActive (or OrchestratorIdle if no agents).
// Returns false if the repo is not registered or not in an active state.
func (m *OrchestratorManager) SetOrchestratorPaused(repoPath string, paused bool) bool {
	lifecycle := m.GetOrchestratorLifecycle(repoPath)
	if lifecycle == nil {
		return false
	}

	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()

	// Only allow pausing/resuming when orchestrator is active, idle, or already paused
	if lifecycle.State == OrchestratorOff {
		return false
	}

	if paused {
		lifecycle.State = OrchestratorPaused
	} else {
		// When resuming, determine state based on active agents
		activeCount := m.countActiveAgents()
		if activeCount > 0 {
			lifecycle.State = OrchestratorActive
		} else {
			lifecycle.State = OrchestratorIdle
		}
	}

	return true
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
	if lifecycleI, ok := m.orchestrators.Load(repoPath); ok {
		lifecycle := lifecycleI.(*OrchestratorLifecycle)
		lifecycle.mu.RLock()
		engine := lifecycle.rulesEngine
		lifecycle.mu.RUnlock()
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
	if lifecycleI, ok := m.orchestrators.Load(repoPath); ok {
		lifecycle := lifecycleI.(*OrchestratorLifecycle)
		lifecycle.mu.Lock()
		defer lifecycle.mu.Unlock()

		// Double-check after acquiring lock
		if lifecycle.rulesEngine != nil {
			return lifecycle.rulesEngine, nil
		}

		// Create engine for the registered orchestrator
		cfg, err := cfgpkg.LoadConfig(repoPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config for %s: %w", repoPath, err)
		}

		lifecycle.rulesEngine = rules.NewEngine(&cfg.Rules)
		return lifecycle.rulesEngine, nil
	}

	// No orchestrator registered - auto-register one for this repo (lazy registration)
	// This provides backwards compatibility: rules can be accessed without explicit registration
	lifecycle, err := m.RegisterRepo(repoPath, "")
	if err != nil {
		return nil, fmt.Errorf("failed to register repo %s: %w", repoPath, err)
	}

	return lifecycle.rulesEngine, nil
}

// InvalidateRulesEngine reloads the rules engine for a repo from config.
// This should be called when the repo's config changes.
// For backwards compatibility, this is also aliased as InvalidateStandaloneEngine.
func (m *OrchestratorManager) InvalidateRulesEngine(repoPath string) {
	if lifecycleI, ok := m.orchestrators.Load(repoPath); ok {
		lifecycle := lifecycleI.(*OrchestratorLifecycle)
		lifecycle.mu.Lock()
		defer lifecycle.mu.Unlock()

		// Reload config and create new engine
		cfg, err := cfgpkg.LoadConfig(repoPath)
		if err != nil {
			logging.Warn("failed to reload config for rules invalidation",
				"repo_path", repoPath, "error", err)
			return
		}

		lifecycle.rulesEngine = rules.NewEngine(&cfg.Rules)
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
// This is a state transition (idle → active), not object creation.
// Returns the run ID or an error if the run could not be started.
// Only one run per repository is allowed at a time.
func (m *OrchestratorManager) StartRun(ctx context.Context, config RunConfig) (string, error) {
	// Validate config
	if config.WorkDir == "" {
		return "", fmt.Errorf("work_dir is required")
	}

	// Get or create the OrchestratorLifecycle for this repo
	lifecycle, err := m.RegisterRepo(config.WorkDir, config.RepoID)
	if err != nil {
		return "", fmt.Errorf("failed to register repo: %w", err)
	}

	// Lock the orchestrator for state transition
	lifecycle.mu.Lock()

	// Check if already active
	if lifecycle.State == OrchestratorActive {
		lifecycle.mu.Unlock()
		return "", &canopyerrors.RunActiveError{
			RepoPath:  config.WorkDir,
			RunID:     lifecycle.RunID,
			StartedAt: lifecycle.StartTime,
		}
	}

	// Generate run ID
	runID := uuid.New().String()

	// Also check the legacy runsByRepo for backwards compatibility
	if existingRunID, loaded := m.runsByRepo.LoadOrStore(config.WorkDir, runID); loaded {
		lifecycle.mu.Unlock()
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
			lifecycle.mu.Lock()
			lifecycle.State = OrchestratorOff
			lifecycle.RunID = ""
			lifecycle.mu.Unlock()
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

	// Update OrchestratorLifecycle state before releasing lock
	lifecycle.State = OrchestratorActive
	lifecycle.RunID = runID
	lifecycle.RunConfig = config
	lifecycle.StartTime = time.Now()
	lifecycle.TasksTotal = 0
	lifecycle.TasksDone = 0
	lifecycle.TasksFailed = 0

	// Release lock before creating orchestrator (may block)
	lifecycle.mu.Unlock()

	// Create orchestrator config
	orchConfig := &orchestrator.Config{
		WorkDir:         config.WorkDir,
		OutputDir:       config.OutputDir,
		Concurrency:     config.Concurrency,
		Verbose:         config.Verbose,
		DryRun:          config.DryRun,
		UseBwrap:        config.UseBwrap,
		MaxRetries:      config.MaxRetries,
		ResolverTimeout: config.ResolverTimeout,
		PollInterval:    config.PollInterval,
	}

	// Apply CLI rules overrides if any were provided
	// All task selection parameters go through the rules system
	if hasRulesOverrides(&config) {
		orchConfig.Rules = &cfgpkg.RulesSettings{
			PriorityMax:   config.PriorityMax,
			Types:         config.Types,
			ExcludeTypes:  config.ExcludeTypes,
			Labels:        config.Labels,
			ExcludeLabels: config.ExcludeLabels,
			Assignee:      config.Assignee,
		}
		orchConfig.RulesOverrides = &orchestrator.RulesOverrides{
			PriorityMax:   config.PriorityMax != 0,
			Types:         len(config.Types) > 0,
			ExcludeTypes:  len(config.ExcludeTypes) > 0,
			Labels:        len(config.Labels) > 0,
			ExcludeLabels: len(config.ExcludeLabels) > 0,
			Assignee:      config.Assignee != "",
		}
	}

	// Create orchestrator
	// Note: The orchestrator loads config.toml and creates its own rules engine in New().
	orch, err := orchestrator.New(orchConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create orchestrator: %w", err)
	}

	// Apply run-configured rule overrides if provided
	// These rules are applied with the highest precedence (above config.toml rules)
	if len(config.RuleOverrides) > 0 {
		if orchEngine := orch.GetRulesEngine(); orchEngine != nil {
			if err := orchEngine.ApplyOverrides(config.RuleOverrides); err != nil {
				return "", fmt.Errorf("failed to apply rule overrides: %w", err)
			}
		}
	}

	// Create cancellable context for this run
	runCtx, cancel := context.WithCancel(ctx)

	// Update OrchestratorLifecycle with orchestrator instance
	lifecycle.mu.Lock()
	lifecycle.orch = orch
	lifecycle.cancel = cancel
	// Update rules engine to use the orchestrator's engine for consistency
	if orchEngine := orch.GetRulesEngine(); orchEngine != nil {
		lifecycle.rulesEngine = orchEngine
	}
	lifecycle.mu.Unlock()

	// Create run state for backwards compatibility
	runState := &RunState{
		ID:        runID,
		RepoPath:  config.WorkDir,
		RepoID:    config.RepoID,
		Status:    RunStatusPending,
		Config:    config,
		StartTime: lifecycle.StartTime,
		orch:      orch,
		cancel:    cancel,
	}

	// Register callbacks that publish directly to EventBus
	callbacks := m.createEventCallbacks(runID, config.RepoID, orch)
	orch.SetCallbacks(callbacks)

	// Register merge status callback to publish merge events to EventBus
	// This replaces the IPC-based merge status when running in daemon mode
	orch.SetMergeStatusCallback(m.createMergeStatusCallback())

	// Register commit callback to publish commit events to EventBus
	// This replaces the IPC-based commit events when running in daemon mode
	orch.SetCommitCallback(m.createCommitCallback())

	// Register agent callback to publish resolver/repair agent events to EventBus
	// This replaces the IPC-based agent events when running in daemon mode
	orch.SetAgentCallback(m.createAgentCallback(runID, config.RepoID))

	// Register state callbacks to track Idle/Active transitions
	orch.SetStateCallbacks(m.createStateCallbacks(lifecycle))

	// Set run and repo IDs on the orchestrator for agent tracking
	orch.SetRunID(runID)
	if config.RepoID != "" {
		orch.SetRepoID(config.RepoID)
	}

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

			lifecycle.mu.Lock()
			lifecycle.TasksTotal = len(tasks)
			lifecycle.mu.Unlock()
		}
	}

	// Publish run started event
	m.publishRunStarted(runID, runState.TasksTotal, config)

	// Publish initial state change (orchestrator now idle/active)
	m.publishStateChange(OrchestratorIdle, 0)

	// Start orchestrator in background goroutine
	go m.runOrchestrator(runCtx, runState, lifecycle)

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
// This transitions the orchestrator state from ACTIVE → IDLE.
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

	// Also update the OrchestratorLifecycle state (back to off since orchestrator stopped)
	if lifecycle := m.GetOrchestratorLifecycle(runState.RepoPath); lifecycle != nil {
		lifecycle.mu.Lock()
		lifecycle.State = OrchestratorOff
		lifecycle.orch = nil
		lifecycle.cancel = nil
		lifecycle.mu.Unlock()
	}

	logging.Info("cancelled orchestration run", "run_id", runID)

	return nil
}

// Activate starts the orchestrator for a repository.
// This is the preferred method for the always-active model. It wraps StartRun
// and returns the run ID for backwards compatibility.
//
// State transition: Off → Idle (or Active if work is immediately available)
func (m *OrchestratorManager) Activate(ctx context.Context, config RunConfig) (string, error) {
	return m.StartRun(ctx, config)
}

// Deactivate stops the orchestrator for a repository by run ID.
// This is the preferred method for the always-active model. It wraps StopRun.
//
// State transition: * → Off
func (m *OrchestratorManager) Deactivate(runID string) error {
	return m.StopRun(runID)
}

// DeactivateByRepo stops the orchestrator for a repository by repo path.
// Returns an error if no active orchestrator exists for the repo.
//
// State transition: * → Off
func (m *OrchestratorManager) DeactivateByRepo(repoPath string) error {
	lifecycle := m.GetOrchestratorLifecycle(repoPath)
	if lifecycle == nil {
		return fmt.Errorf("no orchestrator registered for repo: %s", repoPath)
	}

	lifecycle.mu.RLock()
	runID := lifecycle.RunID
	state := lifecycle.State
	lifecycle.mu.RUnlock()

	if state == OrchestratorOff {
		return fmt.Errorf("orchestrator is not active for repo: %s", repoPath)
	}

	if runID == "" {
		return fmt.Errorf("no run ID found for active orchestrator: %s", repoPath)
	}

	return m.StopRun(runID)
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
// Supports modifying concurrency. Task selection parameters (priority, types, labels)
// are updated through the rules engine via the rules API, not this method.
// Returns an error if the run is not found or not active.
func (m *OrchestratorManager) UpdateRunConfig(runID string, concurrency *int) error {
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

	return nil
}

// runOrchestrator executes the orchestrator and handles completion.
// It updates both the RunState (for backwards compatibility) and the OrchestratorLifecycle.
func (m *OrchestratorManager) runOrchestrator(ctx context.Context, runState *RunState, lifecycle *OrchestratorLifecycle) {
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

	// Transition OrchestratorLifecycle back to OFF state (orchestrator stopped)
	if lifecycle != nil {
		lifecycle.mu.Lock()
		lifecycle.State = OrchestratorOff
		lifecycle.orch = nil
		lifecycle.cancel = nil
		// Keep rulesEngine for continued rules access in OFF state
		// Reload from config to ensure fresh state
		if cfg, err := cfgpkg.LoadConfig(lifecycle.RepoPath); err == nil {
			lifecycle.rulesEngine = rules.NewEngine(&cfg.Rules)
		}
		lifecycle.mu.Unlock()
	}

	// Remove from active runs by repo
	m.runsByRepo.Delete(runState.RepoPath)

	// Publish state change to Off
	m.publishStateChange(OrchestratorOff, 0)

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
func (m *OrchestratorManager) createEventCallbacks(runID, repoID string, orch *orchestrator.Orchestrator) *orchestrator.EventCallbacks {
	return &orchestrator.EventCallbacks{
		OnAgentStartFn: func(ctx context.Context, taskID string, task *beads.Task) {
			agentID := makeAgentID(runID, taskID)

			// Get retry information from orchestrator
			attempt := orch.GetTaskAttempt(taskID)
			maxRetries := orch.GetMaxRetries()

			// Record agent ID for parent-child tracking
			// The orchestrator instance manages this via SetAgentID

			// Publish agent started event - this synchronously creates the agent in RuntimeState
			payload := map[string]interface{}{
				"agent_id":         agentID,
				"run_id":           runID,
				"task_id":          taskID,
				"task_title":       task.Title,
				"task_description": task.Description,
				"parent_agent_id":  "", // Top-level agents have no parent
				"repo_id":          repoID,
			}
			// Include retry information for UI display
			if attempt > 0 {
				payload["attempt"] = attempt
			}
			if maxRetries != 0 {
				payload["max_retries"] = maxRetries
			}
			m.eventBus.Publish(events.Event{
				Type:      events.EventAgentStarted,
				Timestamp: time.Now(),
				Payload:   payload,
			})

			// Transition lifecycle to running state (agent was created by event handler above)
			// This is the authoritative state change - the event handler only initializes the lifecycle
			if m.state != nil {
				if agent := m.state.GetAgent(agentID); agent != nil && agent.Lifecycle != nil {
					if err := agent.Lifecycle.Transition(lifecycle.EventAgentSpawned, lifecycle.TransitionContext{}); err != nil {
						logging.Warn("lifecycle transition failed on agent start",
							"agent_id", agentID,
							"event", lifecycle.EventAgentSpawned,
							"error", err,
						)
					}
				}
			}

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

			// Transition lifecycle to queued_for_merge state (work complete, awaiting merge)
			// This is the authoritative state change - the merge processor will handle subsequent transitions
			if m.state != nil {
				if agent := m.state.GetAgent(agentID); agent != nil && agent.Lifecycle != nil {
					if err := agent.Lifecycle.Transition(lifecycle.EventWorkComplete, lifecycle.TransitionContext{}); err != nil {
						logging.Warn("lifecycle transition failed on agent done",
							"agent_id", agentID,
							"event", lifecycle.EventWorkComplete,
							"error", err,
						)
					}
				}
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

			// Transition lifecycle to failed state (work failed, no retries)
			// This is the authoritative state change
			if m.state != nil {
				if agent := m.state.GetAgent(agentID); agent != nil && agent.Lifecycle != nil {
					// AttemptsRemaining = 0 means no retries, transition directly to failed
					if err := agent.Lifecycle.Transition(lifecycle.EventWorkFailed, lifecycle.TransitionContext{
						AttemptsRemaining: 0,
						Error:             result.Error,
					}); err != nil {
						logging.Warn("lifecycle transition failed on agent fail",
							"agent_id", agentID,
							"event", lifecycle.EventWorkFailed,
							"error", err,
						)
					}
				}
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

// createStateCallbacks creates state transition callbacks for a OrchestratorLifecycle.
// These callbacks update the OrchestratorLifecycle's State field when the underlying
// Orchestrator transitions between Idle and Active states.
func (m *OrchestratorManager) createStateCallbacks(lifecycle *OrchestratorLifecycle) *orchestrator.StateCallbacks {
	return &orchestrator.StateCallbacks{
		OnIdle: func() {
			lifecycle.mu.Lock()
			lifecycle.State = OrchestratorIdle
			lifecycle.mu.Unlock()
			logging.Debug("orchestrator entered idle state", "repo_path", lifecycle.RepoPath)

			// Publish state change event
			m.publishStateChange(OrchestratorIdle, 0)
		},
		OnActive: func() {
			lifecycle.mu.Lock()
			lifecycle.State = OrchestratorActive
			lifecycle.mu.Unlock()
			logging.Debug("orchestrator entered active state", "repo_path", lifecycle.RepoPath)

			// Count active agents
			activeCount := m.countActiveAgents()
			m.publishStateChange(OrchestratorActive, activeCount)
		},
	}
}

// createMergeStatusCallback creates a callback that publishes merge status events to the EventBus.
// This replaces the IPC-based merge status updates when the orchestrator runs in daemon mode.
func (m *OrchestratorManager) createMergeStatusCallback() mergequeue.MergeStatusCallback {
	return func(event mergequeue.MergeStatusEvent) {
		if m.eventBus == nil {
			return
		}

		payload := map[string]interface{}{
			"agent_id":     event.AgentID,
			"merge_status": string(event.Status),
		}

		// Include optional fields only when set
		if event.QueuePos > 0 {
			payload["queue_pos"] = event.QueuePos
		}
		if event.Error != "" {
			payload["error"] = event.Error
		}
		if event.CommitsApplied > 0 {
			payload["commits_applied"] = event.CommitsApplied
		}
		if event.HadConflict {
			payload["had_conflict"] = event.HadConflict
		}
		if event.ResolverSpawned {
			payload["resolver_spawned"] = event.ResolverSpawned
		}

		m.eventBus.Publish(events.Event{
			Type:      events.EventAgentMergeStatus,
			Timestamp: time.Now(),
			Payload:   payload,
		})

		// Also transition lifecycle state if this is a final merge status
		if m.state != nil {
			if agent := m.state.GetAgent(event.AgentID); agent != nil && agent.Lifecycle != nil {
				switch event.Status {
				case types.MergeStatusMerging:
					// Transition to merging state
					if err := agent.Lifecycle.Transition(lifecycle.EventMergeStarted, lifecycle.TransitionContext{}); err != nil {
						logging.Debug("lifecycle transition failed on merge started",
							"agent_id", event.AgentID,
							"event", lifecycle.EventMergeStarted,
							"error", err,
						)
					}
				case types.MergeStatusMerged, types.MergeStatusMergedNeedsRepair:
					// Transition to merged state
					if err := agent.Lifecycle.Transition(lifecycle.EventMergeSuccess, lifecycle.TransitionContext{}); err != nil {
						logging.Debug("lifecycle transition failed on merge success",
							"agent_id", event.AgentID,
							"event", lifecycle.EventMergeSuccess,
							"error", err,
						)
					}
				case types.MergeStatusFailed:
					// Transition to merge failed state
					if err := agent.Lifecycle.Transition(lifecycle.EventMergeFailed, lifecycle.TransitionContext{
						Error: event.Error,
					}); err != nil {
						logging.Debug("lifecycle transition failed on merge failed",
							"agent_id", event.AgentID,
							"event", lifecycle.EventMergeFailed,
							"error", err,
						)
					}
				}
			}
		}
	}
}

// createCommitCallback creates a callback that publishes commit events to the EventBus.
// This replaces the IPC-based commit events when the orchestrator runs in daemon mode.
func (m *OrchestratorManager) createCommitCallback() mergequeue.CommitCallback {
	return func(event mergequeue.CommitEvent) {
		if m.eventBus == nil {
			return
		}

		payload := map[string]interface{}{
			"agent_id":      event.AgentID,
			"hash":          event.Hash,
			"short_hash":    event.ShortHash,
			"message":       event.Message,
			"author":        event.Author,
			"author_email":  event.AuthorEmail,
			"timestamp":     event.Timestamp,
			"files_changed": event.FilesChanged,
		}

		m.eventBus.Publish(events.Event{
			Type:      events.EventAgentCommit,
			Timestamp: time.Now(),
			Payload:   payload,
		})
	}
}

// createAgentCallback creates a callback that publishes resolver/repair agent events to the EventBus.
// This replaces the IPC-based agent events when the orchestrator runs in daemon mode.
// It handles agent started, completed, and failed events from child agents (resolvers, repair agents).
func (m *OrchestratorManager) createAgentCallback(runID, repoID string) func(event interface{}) {
	return func(eventI interface{}) {
		if m.eventBus == nil {
			return
		}

		// Type-assert to resolver.AgentEvent
		event, ok := eventI.(resolver.AgentEvent)
		if !ok {
			logging.Warn("createAgentCallback: unexpected event type", "type", fmt.Sprintf("%T", eventI))
			return
		}

		switch event.EventType {
		case "started":
			// Publish agent started event
			payload := map[string]interface{}{
				"agent_id":         event.AgentID,
				"run_id":           event.RunID,
				"task_id":          event.TaskID,
				"task_title":       event.TaskTitle,
				"task_description": event.TaskDescription,
				"parent_agent_id":  event.ParentAgentID,
				"repo_id":          event.RepoID,
			}
			m.eventBus.Publish(events.Event{
				Type:      events.EventAgentStarted,
				Timestamp: time.Now(),
				Payload:   payload,
			})

			// Transition lifecycle to running state
			if m.state != nil {
				if agent := m.state.GetAgent(event.AgentID); agent != nil && agent.Lifecycle != nil {
					if err := agent.Lifecycle.Transition(lifecycle.EventAgentSpawned, lifecycle.TransitionContext{}); err != nil {
						logging.Warn("lifecycle transition failed on resolver agent start",
							"agent_id", event.AgentID,
							"event", lifecycle.EventAgentSpawned,
							"error", err,
						)
					}
				}
			}

		case "completed":
			// Build completion payload
			payload := map[string]interface{}{
				"agent_id":        event.AgentID,
				"parent_agent_id": event.ParentAgentID,
				"exit_code":       event.ExitCode,
				"duration":        event.DurationSeconds,
				"files_changed":   event.FilesChanged,
				"input_tokens":    event.InputTokens,
				"output_tokens":   event.OutputTokens,
				"cost_usd":        event.CostUSD,
				"duration_ms":     event.DurationMS,
				"duration_api_ms": event.DurationAPIMS,
				"num_turns":       event.NumTurns,
				"commits_created": event.CommitsCreated,
			}

			m.eventBus.Publish(events.Event{
				Type:      events.EventAgentCompleted,
				Timestamp: time.Now(),
				Payload:   payload,
			})

			// Transition lifecycle to queued_for_merge state
			if m.state != nil {
				if agent := m.state.GetAgent(event.AgentID); agent != nil && agent.Lifecycle != nil {
					if err := agent.Lifecycle.Transition(lifecycle.EventWorkComplete, lifecycle.TransitionContext{}); err != nil {
						logging.Warn("lifecycle transition failed on resolver agent done",
							"agent_id", event.AgentID,
							"event", lifecycle.EventWorkComplete,
							"error", err,
						)
					}
				}
			}

		case "failed":
			// Build failure payload
			payload := map[string]interface{}{
				"agent_id":        event.AgentID,
				"parent_agent_id": event.ParentAgentID,
				"exit_code":       event.ExitCode,
				"duration":        event.DurationSeconds,
				"files_changed":   event.FilesChanged,
				"input_tokens":    event.InputTokens,
				"output_tokens":   event.OutputTokens,
				"cost_usd":        event.CostUSD,
				"duration_ms":     event.DurationMS,
				"duration_api_ms": event.DurationAPIMS,
				"num_turns":       event.NumTurns,
				"commits_created": event.CommitsCreated,
				"error":           event.Error,
			}

			m.eventBus.Publish(events.Event{
				Type:      events.EventAgentFailed,
				Timestamp: time.Now(),
				Payload:   payload,
			})

			// Transition lifecycle to failed state
			if m.state != nil {
				if agent := m.state.GetAgent(event.AgentID); agent != nil && agent.Lifecycle != nil {
					if err := agent.Lifecycle.Transition(lifecycle.EventWorkFailed, lifecycle.TransitionContext{
						AttemptsRemaining: 0,
						Error:             event.Error,
					}); err != nil {
						logging.Warn("lifecycle transition failed on resolver agent fail",
							"agent_id", event.AgentID,
							"event", lifecycle.EventWorkFailed,
							"error", err,
						)
					}
				}
			}
		}
	}
}

// publishStateChange publishes an orchestrator state change event to the event bus.
func (m *OrchestratorManager) publishStateChange(state OrchestratorState, activeAgentCount int) {
	if m.eventBus == nil {
		return
	}

	m.eventBus.Publish(events.Event{
		Type:      events.EventOrchStateChanged,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"state":              string(state),
			"active_agent_count": activeAgentCount,
		},
	})
}

// countActiveAgents counts the number of currently running agents.
func (m *OrchestratorManager) countActiveAgents() int {
	count := 0
	if m.state != nil {
		snapshot := m.state.GetSnapshot()
		for _, agent := range snapshot.Agents {
			if agent.Status == "running" {
				count++
			}
		}
	}
	return count
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
	payload := map[string]interface{}{
		"run_id":     runID,
		"task_count": taskCount,
		"repo_id":    config.RepoID,
		"repo_path":  config.WorkDir,
	}

	// Include rule overrides if present
	if len(config.RuleOverrides) > 0 {
		payload["rule_overrides"] = config.RuleOverrides
	}

	m.eventBus.Publish(events.Event{
		Type:      events.EventRunStarted,
		Timestamp: time.Now(),
		Payload:   payload,
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

// KillAgent terminates a specific agent by its ID.
// It finds the run that owns the agent and delegates to the scheduler.
// Agent ID format: agent-{runID[:8]}-{taskID}
func (m *OrchestratorManager) KillAgent(agentID string) error {
	// Parse agent ID to extract run ID prefix
	// Format: agent-{runID[:8]}-{taskID}
	parts := strings.SplitN(agentID, "-", 3)
	if len(parts) < 3 || parts[0] != "agent" {
		return fmt.Errorf("invalid agent ID format: %s", agentID)
	}
	runPrefix := parts[1]

	// Find the run with matching ID prefix
	var foundRun *RunState
	m.runs.Range(func(key, value interface{}) bool {
		runID := key.(string)
		// Check if run ID starts with the prefix
		if len(runID) >= len(runPrefix) && runID[:len(runPrefix)] == runPrefix {
			foundRun = value.(*RunState)
			return false // stop iteration
		}
		return true
	})

	if foundRun == nil {
		return fmt.Errorf("run not found for agent %s", agentID)
	}

	// Get the scheduler from the orchestrator
	foundRun.mu.RLock()
	orch := foundRun.orch
	foundRun.mu.RUnlock()

	if orch == nil {
		return fmt.Errorf("orchestrator not available for agent %s", agentID)
	}

	sched := orch.GetScheduler()
	if sched == nil {
		return fmt.Errorf("scheduler not available for agent %s", agentID)
	}

	// Extract task ID from parts (taskID is parts[2])
	taskID := parts[2]
	return sched.Kill(taskID)
}

// hasRulesOverrides checks if any CLI rules overrides were provided in the RunConfig.
func hasRulesOverrides(cfg *RunConfig) bool {
	return cfg.PriorityMax != 0 ||
		len(cfg.Types) > 0 ||
		len(cfg.ExcludeTypes) > 0 ||
		len(cfg.Labels) > 0 ||
		len(cfg.ExcludeLabels) > 0 ||
		cfg.Assignee != ""
}

// CleanupAllOverlays cleans up active overlays from all running orchestrators.
// This should be called during daemon shutdown to prevent orphaned FUSE mounts.
// The timeout specifies how long to wait for cleanup to complete.
// Returns the number of overlays cleaned and any errors encountered.
func (m *OrchestratorManager) CleanupAllOverlays(timeout time.Duration) (int, error) {
	var totalCleaned int
	var cleanupErrors []error

	// Iterate through all active runs to find their schedulers
	m.runs.Range(func(key, value interface{}) bool {
		runState := value.(*RunState)
		runState.mu.RLock()
		orch := runState.orch
		runState.mu.RUnlock()

		if orch == nil {
			return true // continue iteration
		}

		sched := orch.GetScheduler()
		if sched == nil {
			return true // continue iteration
		}

		// Cleanup overlays for this scheduler
		count, err := sched.CleanupAll(timeout)
		totalCleaned += count
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("run %s: %w", key.(string), err))
		}

		return true // continue iteration
	})

	// If there were errors, return them
	if len(cleanupErrors) > 0 {
		return totalCleaned, fmt.Errorf("cleanup completed with %d error(s): %v", len(cleanupErrors), cleanupErrors)
	}

	return totalCleaned, nil
}