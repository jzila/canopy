package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	cfgpkg "github.com/jzila/canopy/pkg/config"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/mergecoordinator"
	"github.com/jzila/canopy/pkg/mergequeue"
	"github.com/jzila/canopy/pkg/rules"
	"github.com/jzila/canopy/pkg/sandbox"
	"github.com/jzila/canopy/pkg/scheduler"
)

// EventCallbacks defines lifecycle callbacks for agent execution events.
// All callbacks receive a context.Context for proper cancellation and timeout propagation.
type EventCallbacks struct {
	// OnAgentStartFn is called when an agent begins execution
	OnAgentStartFn func(ctx context.Context, taskID string, task *beads.Task)

	// OnOutputFn is called when an agent produces output (stdout/stderr)
	OnOutputFn func(ctx context.Context, taskID string, output string, isError bool)

	// OnLiveFeedFn is called for real-time streaming events from agents
	OnLiveFeedFn func(ctx context.Context, taskID string, event *agent.LiveFeedEvent)

	// OnDoneFn is called when an agent completes successfully
	OnDoneFn func(ctx context.Context, taskID string, result *agent.Result)

	// OnFailFn is called when an agent fails
	OnFailFn func(ctx context.Context, taskID string, result *agent.Result)
}

// OnAgentStart implements scheduler.CallbackHandler
func (e *EventCallbacks) OnAgentStart(ctx context.Context, taskID string, task *beads.Task) {
	if e != nil && e.OnAgentStartFn != nil {
		e.OnAgentStartFn(ctx, taskID, task)
	}
}

// OnOutput implements scheduler.CallbackHandler
func (e *EventCallbacks) OnOutput(ctx context.Context, taskID string, output string, isError bool) {
	if e != nil && e.OnOutputFn != nil {
		e.OnOutputFn(ctx, taskID, output, isError)
	}
}

// OnLiveFeed implements scheduler.CallbackHandler
func (e *EventCallbacks) OnLiveFeed(ctx context.Context, taskID string, event *agent.LiveFeedEvent) {
	if e != nil && e.OnLiveFeedFn != nil {
		e.OnLiveFeedFn(ctx, taskID, event)
	}
}

// OnDone implements scheduler.CallbackHandler
func (e *EventCallbacks) OnDone(ctx context.Context, taskID string, result *agent.Result) {
	if e != nil && e.OnDoneFn != nil {
		e.OnDoneFn(ctx, taskID, result)
	}
}

// OnFail implements scheduler.CallbackHandler
func (e *EventCallbacks) OnFail(ctx context.Context, taskID string, result *agent.Result) {
	if e != nil && e.OnFailFn != nil {
		e.OnFailFn(ctx, taskID, result)
	}
}

// StateCallbacks defines callbacks for orchestrator state transitions.
// These are called when the orchestrator transitions between Idle and Active states.
type StateCallbacks struct {
	// OnIdle is called when the orchestrator enters idle state (no in-flight tasks)
	OnIdle func()

	// OnActive is called when the orchestrator enters active state (has in-flight tasks)
	OnActive func()
}

// Config holds orchestrator configuration
type Config struct {
	WorkDir         string
	OutputDir       string
	Concurrency     int
	Verbose         bool
	DryRun          bool
	UseBwrap        bool          // Use bubblewrap sandbox for agent isolation
	MaxRetries      int           // Maximum number of times to retry failed tasks (0 = no retries, -1 = infinite)
	ResolverTimeout time.Duration // Timeout for resolver agents (0 = use default 10m)
	Rules           *cfgpkg.RulesSettings // CLI overrides for rules (nil = use config.toml only)
	RulesOverrides  *RulesOverrides       // Tracks which rules fields were explicitly set via CLI
	PollInterval    time.Duration // Interval between polling for new tasks when idle (default: 5s)
	Model           string        // Model to use for agents (empty = use Claude CLI default). Overrides config.toml agent settings.
	AgentSettings   *cfgpkg.AgentSettings // Per-agent-type settings from config.toml (passed from daemon, or loaded automatically)
}

// RulesOverrides tracks which rules fields were explicitly set via CLI flags.
// This allows merging with config.toml while preserving explicit CLI values.
// The 3-tier precedence hierarchy is: default < config < runtime override.
type RulesOverrides struct {
	PriorityMin   bool
	PriorityMax   bool
	Types         bool
	ExcludeTypes  bool
	Labels        bool
	ExcludeLabels bool
	Assignee      bool
}

// Orchestrator coordinates the execution of tasks from beads
type Orchestrator struct {
	config           *Config
	repoConfig       *cfgpkg.Config // Loaded from .canopy/config.toml
	beadsClient      beads.BeadsClient
	scheduler        *scheduler.Scheduler
	mergeCoordinator *mergecoordinator.MergeCoordinator
	tempDir          string
	callbackManager  *CallbackManager
	failureCountsMu  sync.RWMutex       // Protects failureCounts
	failureCounts    map[string]int     // Tracks how many times each task has failed
	sandboxConfig    *sandbox.SandboxConfig
	rulesEngine      *rules.Engine      // Rules engine for per-repo task selection

	// Dynamic concurrency control - supports runtime adjustment via API
	slotManager *scheduler.SlotManager

	// In-flight task tracking for dynamic task assignment
	inFlightMu   sync.RWMutex
	inFlight     map[string]bool   // Tasks currently being executed
	inFlightTask map[string]*beads.Task // Task objects for in-flight tasks (for concurrency limiting)

	// State transition callbacks
	stateCallbacks *StateCallbacks
	wasActive      bool // Track previous state to detect transitions
}

// New creates a new orchestrator
func New(config *Config) (*Orchestrator, error) {
	// Create beads client
	beadsClient, err := beads.NewClient(config.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create beads client: %w", err)
	}

	// Load config.toml - this is the source of truth for rules
	repoConfig, err := cfgpkg.LoadConfig(config.WorkDir)
	if err != nil {
		// Log warning but continue with defaults - config loading should not fail orchestration
		if config.Verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to load config.toml: %v\n", err)
		}
		repoConfig = cfgpkg.DefaultConfig()
	} else if config.Verbose {
		fmt.Printf("Loaded config from %s/.canopy/config.toml\n", config.WorkDir)
	}

	// Set up overlay directory per persistence invariant (XDG_CACHE_HOME/canopy/overlays)
	tempDir, err := sandbox.GetOverlayBaseDir()
	if err != nil {
		tempDir = filepath.Join(os.TempDir(), "canopy")
	}
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create overlay directory: %w", err)
	}

	// Try to load sandbox config from .canopy/sandbox.toml
	var sandboxConfig *sandbox.SandboxConfig
	if config.UseBwrap {
		loadedConfig, err := sandbox.LoadConfig(config.WorkDir)
		if err != nil && config.Verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to load sandbox config: %v\n", err)
		}
		if loadedConfig != nil {
			sandboxConfig = loadedConfig
			if config.Verbose {
				fmt.Printf("Loaded sandbox config from %s/.canopy/sandbox.toml\n", config.WorkDir)
			}
		} else if config.Verbose {
			fmt.Println("No sandbox config found, using default bwrap settings")
		}
	}

	// Use AgentSettings from config if provided, otherwise use from repoConfig
	agentSettings := config.AgentSettings
	if agentSettings == nil {
		agentSettings = &repoConfig.Agents
	}

	// Determine worker model: CLI flag takes precedence, then config.toml agent settings
	workerModel := config.Model
	if workerModel == "" {
		workerModel = agentSettings.GetWorkerModel()
	}

	// Parse worker timeout from agent settings
	var workerTimeout time.Duration
	if agentSettings.Worker.Timeout != "" {
		if d, err := time.ParseDuration(agentSettings.Worker.Timeout); err != nil {
			fmt.Fprintf(os.Stderr, "warning: invalid worker timeout %q in config: %v\n", agentSettings.Worker.Timeout, err)
		} else {
			workerTimeout = d
		}
	}

	// Create agent executor
	executor := agent.NewExecutor(&agent.Config{
		Verbose:       config.Verbose,
		UseBwrap:      config.UseBwrap,
		SandboxConfig: sandboxConfig,
		Model:         workerModel,
		Timeout:       workerTimeout,
	})

	// Create scheduler
	sched := scheduler.NewScheduler(beadsClient, executor, &scheduler.Config{
		Concurrency:   config.Concurrency,
		TempDir:       tempDir,
		WorkDir:       config.WorkDir,
		Verbose:       config.Verbose,
		SandboxConfig: sandboxConfig,
	})

	// Set default MaxRetries if not specified (default: 3 retries)
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}

	// Determine resolver model: CLI flag takes precedence, then config.toml agent settings
	resolverModel := config.Model
	if resolverModel == "" {
		resolverModel = agentSettings.GetResolverModel()
	}

	// Determine repair model: CLI flag takes precedence, then config.toml agent settings
	repairModel := config.Model
	if repairModel == "" {
		repairModel = agentSettings.GetRepairModel()
	}

	// Create merge coordinator to handle all merge operations
	mc, err := mergecoordinator.New(&mergecoordinator.Config{
		WorkDir:         config.WorkDir,
		OutputDir:       config.OutputDir,
		TempDir:         tempDir,
		Concurrency:     config.Concurrency,
		Verbose:         config.Verbose,
		UseBwrap:        config.UseBwrap,
		SandboxConfig:   sandboxConfig,
		ResolverTimeout: config.ResolverTimeout,
		ResolverModel:   resolverModel,
		RepairModel:     repairModel,
	}, beadsClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create merge coordinator: %w", err)
	}

	// Build effective rules settings: start from config.toml, apply CLI overrides
	effectiveRules := repoConfig.Rules
	if config.Rules != nil {
		// CLI overrides take precedence - merge them in
		effectiveRules = mergeRulesSettings(&repoConfig.Rules, config.Rules, config.RulesOverrides)
	}

	// Create rules engine from effective rules with default rules
	rulesEngine := rules.NewEngineWithDefaults(&effectiveRules, cfgpkg.DefaultCustomRules)

	o := &Orchestrator{
		config:           config,
		repoConfig:       repoConfig,
		beadsClient:      beadsClient,
		scheduler:        sched,
		mergeCoordinator: mc,
		tempDir:          tempDir,
		callbackManager:  NewCallbackManager(),
		failureCounts:    make(map[string]int),
		sandboxConfig:    sandboxConfig,
		rulesEngine:      rulesEngine,
		slotManager:      scheduler.NewSlotManager(config.Concurrency),
		inFlight:         make(map[string]bool),
		inFlightTask:     make(map[string]*beads.Task),
	}

	// Set cleanup callback for merge coordinator
	mc.SetCleanupCallback(o.cleanupOverlay)

	// Set up internal callbacks for merge coordination
	o.setupInternalCallbacks()

	return o, nil
}

// SetCallbacks configures event callbacks for the orchestrator.
// The orchestrator invokes these callbacks alongside internal callbacks
// when events occur.
func (o *Orchestrator) SetCallbacks(callbacks *EventCallbacks) {
	o.callbackManager.Register(callbacks)
}

// setupInternalCallbacks registers the orchestrator's internal callbacks with the CallbackManager.
// These handle merge coordination and task caching. User callbacks registered via SetCallbacks
// will be invoked alongside these internal callbacks.
// CRITICAL: Task completion (beadsClient.Done/Fail) now happens in the merge coordinator,
// not the callback. This ensures dependent agents see merged changes from predecessors.
func (o *Orchestrator) setupInternalCallbacks() {
	// Register internal callbacks as a single EventCallbacks struct
	internalCallbacks := &EventCallbacks{
		OnAgentStartFn: func(ctx context.Context, taskID string, task *beads.Task) {
			// Store task in merge coordinator for later use in OnDone
			o.mergeCoordinator.CacheTask(taskID, task)

			// Send task status update to mark as in_progress
			o.mergeCoordinator.SendTaskUpdated(taskID, task.Title, "in_progress")
		},
		OnDoneFn: func(ctx context.Context, taskID string, result *agent.Result) {
			// Delegate merge to coordinator - it handles queueing, merge, and task completion
			resp := o.mergeCoordinator.EnqueueMerge(ctx, result)

			// If merge failed, mark result as failed so failure count is incremented
			// and retry limit applies. Without this, merge failures would retry forever.
			if resp != nil && !resp.Success {
				result.Success = false
				result.Error = resp.Error
				if o.config.Verbose {
					fmt.Fprintf(os.Stderr, "[%s] merge failed: %s\n", taskID, resp.Error)
				}
			}
		},
		OnFailFn: func(ctx context.Context, taskID string, result *agent.Result) {
			// Delegate failure handling to coordinator
			o.mergeCoordinator.HandleFailure(ctx, taskID, result, result.Error)
		},
	}
	o.callbackManager.Register(internalCallbacks)

	// Pass the callback manager to the scheduler
	if o.scheduler != nil {
		o.scheduler.SetCallbacks(o.callbackManager)
	}
}

// cleanupOverlay cleans up the overlay for a result.
// This is safe to call from concurrent goroutines.
// The context parameter is available for future use (e.g., logging with trace ID).
func (o *Orchestrator) cleanupOverlay(ctx context.Context, result *agent.Result) {
	if result.Overlay != nil {
		if err := result.Overlay.Cleanup(); err != nil && o.config.Verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to cleanup overlay for task %s: %v\n", result.TaskID, err)
		}
	}
}

// CleanupOverlayByTaskID explicitly cleans up the overlay for a task by its ID.
// This can be called as a safety net when a terminal state is reached, ensuring
// overlay cleanup even if the normal callback chain was bypassed.
// Safe to call even if no overlay exists for the task ID.
func (o *Orchestrator) CleanupOverlayByTaskID(taskID string) {
	if o.scheduler != nil {
		o.scheduler.CleanupOverlay(taskID)
	}
}

// WithCallbacks is a builder-style method to register callbacks
func (o *Orchestrator) WithCallbacks(callbacks *EventCallbacks) *Orchestrator {
	o.SetCallbacks(callbacks)
	return o
}

// SetIPCClient sets the IPC client for the orchestrator and merge coordinator.
// This enables sending merge status events and resolver agent tracking.
func (o *Orchestrator) SetIPCClient(client *ipc.Client) {
	o.mergeCoordinator.SetIPCClient(client)
}

// SetMergeStatusCallback sets a callback for merge status events.
// This is used when running in daemon mode where IPC is not available.
// The callback is invoked whenever merge status would be sent via IPC.
func (o *Orchestrator) SetMergeStatusCallback(callback mergequeue.MergeStatusCallback) {
	o.mergeCoordinator.SetMergeStatusCallback(callback)
}

// SetCommitCallback sets a callback for commit events.
// This is used when running in daemon mode where IPC is not available.
// The callback is invoked for each commit created during merge.
func (o *Orchestrator) SetCommitCallback(callback mergequeue.CommitCallback) {
	o.mergeCoordinator.SetCommitCallback(callback)
}

// SetAgentCallback sets a callback for agent lifecycle events from child agents.
// This is used when running in daemon mode where IPC is not available.
// The callback is invoked for resolver and repair agent start/done/fail events.
// The callback receives resolver.AgentEvent directly - callers must import the resolver package.
func (o *Orchestrator) SetAgentCallback(callback func(event interface{})) {
	o.mergeCoordinator.SetAgentCallback(callback)
}

// SetRepoID sets the repository ID for IPC tracking.
// This ID is passed to resolver agents for parent-child tracking.
func (o *Orchestrator) SetRepoID(repoID string) {
	o.mergeCoordinator.SetRepoID(repoID)
}

// SetRunID sets the run ID for unique agent ID generation.
// This ensures agent IDs are unique per run, even when retrying tasks.
func (o *Orchestrator) SetRunID(runID string) {
	o.mergeCoordinator.SetRunID(runID)
}

// SetConcurrency dynamically updates the concurrency setting.
// This takes effect immediately for the running orchestrator - new slots
// become available if increasing, or existing work drains naturally if decreasing.
func (o *Orchestrator) SetConcurrency(concurrency int) {
	o.config.Concurrency = concurrency
	if o.slotManager != nil {
		o.slotManager.SetConcurrency(concurrency)
	}
}

// SetRulesEngine sets the rules engine for this orchestrator instance.
// This enables per-repo rules isolation.
func (o *Orchestrator) SetRulesEngine(engine *rules.Engine) {
	o.rulesEngine = engine
}

// GetRulesEngine returns the rules engine for this orchestrator instance.
func (o *Orchestrator) GetRulesEngine() *rules.Engine {
	return o.rulesEngine
}

// GetRepoConfig returns the loaded repository config (.canopy/config.toml).
func (o *Orchestrator) GetRepoConfig() *cfgpkg.Config {
	return o.repoConfig
}

// UpdateAgentSettings propagates updated agent settings to the running executor,
// resolver, and repair agents. Only non-default (non-empty) model values are applied;
// timeout values from AgentTypeSettings are parsed and applied when valid.
// CLI model override (config.Model) takes precedence and is not overwritten.
func (o *Orchestrator) UpdateAgentSettings(settings *cfgpkg.AgentSettings) {
	if settings == nil {
		return
	}

	// Only apply config-based models when no CLI override is present.
	// Empty model values are applied as explicit clears (return to CLI default).
	if o.config.Model == "" {
		o.scheduler.GetExecutor().SetModel(settings.GetWorkerModel())
		o.mergeCoordinator.SetResolverModel(settings.GetResolverModel())
		o.mergeCoordinator.SetModel(settings.GetRepairModel())
	}

	// Apply worker timeout if specified
	if settings.Worker.Timeout != "" {
		if d, err := time.ParseDuration(settings.Worker.Timeout); err != nil {
			fmt.Fprintf(os.Stderr, "warning: invalid worker timeout %q: %v\n", settings.Worker.Timeout, err)
		} else if d < 0 {
			fmt.Fprintf(os.Stderr, "warning: negative worker timeout %q ignored\n", settings.Worker.Timeout)
		} else {
			o.scheduler.GetExecutor().SetTimeout(d)
		}
	}
}

// SetAgentID records the agentID for a taskID, enabling parent-child tracking for resolvers.
// This should be called when an agent starts execution.
func (o *Orchestrator) SetAgentID(taskID, agentID string) {
	o.mergeCoordinator.SetAgentID(taskID, agentID)
}

// GetAgentID retrieves the agentID for a taskID.
func (o *Orchestrator) GetAgentID(taskID string) string {
	return o.mergeCoordinator.GetAgentID(taskID)
}

// SetStateCallbacks sets callbacks for orchestrator state transitions (Idle/Active).
// These callbacks are invoked when the orchestrator transitions between having
// in-flight tasks (Active) and having no work to do (Idle).
func (o *Orchestrator) SetStateCallbacks(callbacks *StateCallbacks) {
	o.stateCallbacks = callbacks
}

// GetTaskAttempt returns the current attempt number for a task (1-indexed).
// Returns 1 for first attempt, 2 for first retry, etc.
// The attempt is calculated as: number of previous failures + 1.
func (o *Orchestrator) GetTaskAttempt(taskID string) int {
	o.failureCountsMu.RLock()
	defer o.failureCountsMu.RUnlock()
	// Attempt = previous failures + 1
	// If no failures, this is attempt 1 (first try)
	return o.failureCounts[taskID] + 1
}

// GetMaxRetries returns the configured maximum retry attempts.
// Returns 0 = no retries, -1 = infinite, positive = retry limit.
func (o *Orchestrator) GetMaxRetries() int {
	return o.config.MaxRetries
}

// notifyStateChange checks if state has changed and invokes appropriate callback.
func (o *Orchestrator) notifyStateChange(inFlightCount int) {
	if o.stateCallbacks == nil {
		return
	}

	isActive := inFlightCount > 0
	if isActive && !o.wasActive {
		// Transition to Active
		o.wasActive = true
		if o.stateCallbacks.OnActive != nil {
			o.stateCallbacks.OnActive()
		}
	} else if !isActive && o.wasActive {
		// Transition to Idle
		o.wasActive = false
		if o.stateCallbacks.OnIdle != nil {
			o.stateCallbacks.OnIdle()
		}
	}
}

// Run executes the orchestration loop, polling continuously for work until context is cancelled.
// This uses dynamic task assignment: each worker calls bd ready to get
// fresh tasks, ensuring newly-unblocked tasks are picked up immediately.
// The orchestrator polls continuously: when work is available it processes tasks (Active state),
// when no work is available it sleeps for PollInterval (Idle state).
func (o *Orchestrator) Run(ctx context.Context) error {
	// Start the merge coordinator's processor goroutine
	// It will process merge requests from the queue until context is cancelled
	o.mergeCoordinator.Start(ctx)

	// Dry run: show what would execute and exit
	if o.config.DryRun {
		return o.dryRun(ctx)
	}

	// Set default poll interval
	pollInterval := o.config.PollInterval
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}

	if o.config.Verbose {
		fmt.Printf("Orchestrator started, polling every %v when idle\n", pollInterval)
	}

	// Track active workers and results
	var wg sync.WaitGroup
	var resultsMu sync.Mutex
	var results []*agent.Result

	// Create a channel to signal when workers should check for new tasks
	// Workers send on this channel when they complete a task
	taskComplete := make(chan struct{}, o.config.Concurrency)

	// Completion callback removes task from in-flight and tracks results
	completionCallback := func(taskID string, result *agent.Result) {
		o.unmarkInFlight(taskID)

		resultsMu.Lock()
		results = append(results, result)
		resultsMu.Unlock()

		// Track success/failure counts (using dedicated mutex)
		o.failureCountsMu.Lock()
		if result.Success {
			delete(o.failureCounts, taskID)
		} else {
			o.failureCounts[taskID]++
			failureCount := o.failureCounts[taskID]

			// Check if retries exhausted
			if o.config.MaxRetries != -1 && failureCount > o.config.MaxRetries {
				// Mark as permanently failed in beads - closes task and adds needs-investigation label
				if err := o.beadsClient.FailPermanently(ctx, taskID, fmt.Sprintf("Task failed after %d attempts", failureCount)); err != nil {
					fmt.Fprintf(os.Stderr, "warning: could not mark task %s as permanently failed in beads: %v\n", taskID, err)
				}
				fmt.Fprintf(os.Stderr, "ERROR: Task %s has failed %d times and will not be retried\n", taskID, failureCount)
			} else if o.config.MaxRetries == -1 || failureCount <= o.config.MaxRetries {
				// Retry will occur - log retry info
				maxAttempts := o.config.MaxRetries + 1
				if o.config.MaxRetries == -1 {
					fmt.Fprintf(os.Stderr, "[%s] Merge failed (attempt %d/∞): %s. Retrying...\n",
						taskID, failureCount, result.Error)
				} else {
					fmt.Fprintf(os.Stderr, "[%s] Merge failed (attempt %d/%d): %s. Retrying...\n",
						taskID, failureCount, maxAttempts, result.Error)
				}
			}
		}
		o.failureCountsMu.Unlock()

		// Signal that a task completed (non-blocking)
		select {
		case taskComplete <- struct{}{}:
		default:
		}
	}

	// Main loop: poll continuously until context is cancelled
	for {
		select {
		case <-ctx.Done():
			// Wait for in-flight tasks to complete
			wg.Wait()
			return ctx.Err()
		default:
		}

		// Try to acquire a slot
		if err := o.slotManager.Acquire(ctx); err != nil {
			// Context cancelled
			wg.Wait()
			return err
		}

		// Got a slot, get the next task
		task, _, err := o.getNextTask(ctx)
		if err != nil {
			o.slotManager.Release()
			wg.Wait()
			return err
		}

		if task == nil {
			// No task available right now
			o.slotManager.Release()

			// Check if there are any in-flight tasks
			o.inFlightMu.RLock()
			inFlightCount := len(o.inFlight)
			o.inFlightMu.RUnlock()

			if inFlightCount == 0 {
				// No in-flight tasks and no ready tasks - idle state
				// Poll for new tasks after interval
				if o.config.Verbose {
					fmt.Println("Idle, polling for new tasks...")
				}

				select {
				case <-ctx.Done():
					wg.Wait()
					return ctx.Err()
				case <-time.After(pollInterval):
					// Poll again
					continue
				}
			}

			// Wait for a task to complete before trying again
			select {
			case <-ctx.Done():
				wg.Wait()
				return ctx.Err()
			case <-taskComplete:
				// A task completed, loop back to try again
				continue
			}
		}

		// Start worker for this task
		if o.config.Verbose {
			fmt.Printf("Starting task: %s: %s\n", task.ID, task.Title)
		}

		wg.Add(1)
		go func(t *beads.Task) {
			defer wg.Done()
			defer o.slotManager.Release()

			o.scheduler.ExecuteTask(ctx, t, completionCallback)
		}(task)
	}
}

// dryRun shows what tasks would execute without actually running them
func (o *Orchestrator) dryRun(ctx context.Context) error {
	// Get ready tasks from beads
	tasks, err := o.beadsClient.Ready(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ready tasks: %w", err)
	}

	beforeCount := len(tasks)

	// Apply task filtering rules
	tasks = o.filterTasks(tasks)

	if o.config.Verbose && len(tasks) != beforeCount {
		fmt.Printf("After rules filter: %d tasks (from %d ready)\n", len(tasks), beforeCount)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks would execute")
		return nil
	}

	fmt.Printf("Would execute %d tasks:\n", len(tasks))
	for _, t := range tasks {
		fmt.Printf("  - %s: %s\n", t.ID, t.Title)
	}
	return nil
}

// Cleanup removes temporary files
func (o *Orchestrator) Cleanup() error {
	return os.RemoveAll(o.tempDir)
}

// GetScheduler returns the underlying scheduler for advanced operations like signal cleanup
func (o *Orchestrator) GetScheduler() *scheduler.Scheduler {
	return o.scheduler
}

// Shutdown gracefully stops the orchestrator and waits for cleanup to complete.
// This ensures all active overlays are unmounted before returning.
// The timeout specifies how long to wait for overlay cleanup (not task completion).
// Returns the number of overlays cleaned and any errors encountered.
func (o *Orchestrator) Shutdown(timeout time.Duration) (int, error) {
	// Clean up overlays via the scheduler
	if o.scheduler != nil {
		return o.scheduler.CleanupAll(timeout)
	}
	return 0, nil
}


// filterTasks applies configured rules to filter tasks using the rules engine.
func (o *Orchestrator) filterTasks(tasks []beads.Task) []beads.Task {
	// Use the rules engine for all task filtering
	if o.rulesEngine != nil {
		return o.filterTasksWithEngine(tasks)
	}
	// No rules engine - return all tasks unfiltered
	return tasks
}

// filterTasksWithEngine applies the rules engine to filter tasks.
// This considers in-flight tasks for concurrency limit rules.
func (o *Orchestrator) filterTasksWithEngine(tasks []beads.Task) []beads.Task {
	o.inFlightMu.RLock()
	// Make copies of in-flight state for the rules engine
	inFlight := make(map[string]bool, len(o.inFlight))
	for k, v := range o.inFlight {
		inFlight[k] = v
	}
	inFlightTasks := make(map[string]*beads.Task, len(o.inFlightTask))
	for k, v := range o.inFlightTask {
		inFlightTasks[k] = v
	}
	o.inFlightMu.RUnlock()

	filtered := make([]beads.Task, 0, len(tasks))
	for i := range tasks {
		task := &tasks[i]
		result := o.rulesEngine.Evaluate(task, inFlight, inFlightTasks)
		if result.Allow && !result.Skip {
			filtered = append(filtered, tasks[i])
		}
	}
	return filtered
}

// markInFlight marks a task as currently in-flight
func (o *Orchestrator) markInFlight(taskID string) {
	o.inFlightMu.Lock()
	wasEmpty := len(o.inFlight) == 0
	o.inFlight[taskID] = true
	count := len(o.inFlight)
	o.inFlightMu.Unlock()

	// Notify state change if we transitioned from empty to non-empty
	if wasEmpty {
		o.notifyStateChange(count)
	}
}

// unmarkInFlight removes a task from the in-flight set
func (o *Orchestrator) unmarkInFlight(taskID string) {
	o.inFlightMu.Lock()
	delete(o.inFlight, taskID)
	delete(o.inFlightTask, taskID)
	count := len(o.inFlight)
	o.inFlightMu.Unlock()

	// Notify state change (might transition to idle if count is 0)
	o.notifyStateChange(count)
}

// isInFlight returns whether a task is currently in-flight
func (o *Orchestrator) isInFlight(taskID string) bool {
	o.inFlightMu.RLock()
	defer o.inFlightMu.RUnlock()
	return o.inFlight[taskID]
}

// getNextTask fetches fresh ready tasks from beads and returns the first one
// that is not currently in-flight. Returns nil if no available task is found.
// Also returns whether we should stop (always false now, stop conditions removed).
func (o *Orchestrator) getNextTask(ctx context.Context) (*beads.Task, bool, error) {
	// Get fresh ready tasks from beads
	tasks, err := o.beadsClient.Ready(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get ready tasks: %w", err)
	}

	// Apply task filtering rules
	tasks = o.filterTasks(tasks)

	// Filter out in-flight tasks and find the first available one
	o.inFlightMu.Lock()
	defer o.inFlightMu.Unlock()

	for i := range tasks {
		if !o.inFlight[tasks[i].ID] {
			// Mark this task as in-flight (store task object for concurrency limiting)
			o.inFlight[tasks[i].ID] = true
			o.inFlightTask[tasks[i].ID] = &tasks[i]
			return &tasks[i], false, nil
		}
	}

	// No available tasks (either no ready tasks, or all are in-flight)
	// Return nil but don't signal stop - there may be in-flight tasks that will complete
	return nil, false, nil
}

// mergeRulesSettings merges CLI override settings into base config settings.
// The overrides parameter tracks which fields were explicitly set via CLI.
func mergeRulesSettings(base, cliRules *cfgpkg.RulesSettings, overrides *RulesOverrides) cfgpkg.RulesSettings {
	result := *base

	// If no CLI rules provided, return base as-is
	if cliRules == nil {
		return result
	}

	if overrides == nil {
		// No overrides tracking - use simple heuristics (legacy behavior)
		// Only apply if value differs from default
		if cliRules.PriorityMin > 0 {
			result.PriorityMin = cliRules.PriorityMin
		}
		if cliRules.PriorityMax != -1 && cliRules.PriorityMax != 0 {
			result.PriorityMax = cliRules.PriorityMax
		}
		if len(cliRules.Types) > 0 {
			result.Types = cliRules.Types
		}
		if len(cliRules.ExcludeTypes) > 0 {
			result.ExcludeTypes = cliRules.ExcludeTypes
		}
		if len(cliRules.Labels) > 0 {
			result.Labels = cliRules.Labels
		}
		if len(cliRules.ExcludeLabels) > 0 {
			result.ExcludeLabels = cliRules.ExcludeLabels
		}
		if cliRules.Assignee != "*" && cliRules.Assignee != "" {
			result.Assignee = cliRules.Assignee
		}
		return result
	}

	// Apply only fields that were explicitly set via CLI flags (3-tier precedence)
	// Runtime overrides take precedence over config.toml settings
	if overrides.PriorityMin {
		result.PriorityMin = cliRules.PriorityMin
	}
	if overrides.PriorityMax {
		result.PriorityMax = cliRules.PriorityMax
	}
	if overrides.Types {
		result.Types = cliRules.Types
	}
	if overrides.ExcludeTypes {
		result.ExcludeTypes = cliRules.ExcludeTypes
	}
	if overrides.Labels {
		result.Labels = cliRules.Labels
	}
	if overrides.ExcludeLabels {
		result.ExcludeLabels = cliRules.ExcludeLabels
	}
	if overrides.Assignee {
		result.Assignee = cliRules.Assignee
	}

	return result
}