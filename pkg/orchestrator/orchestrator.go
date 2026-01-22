package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	cfgpkg "github.com/jzila/canopy/pkg/config"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/mergecoordinator"
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

// Config holds orchestrator configuration
type Config struct {
	WorkDir         string
	OutputDir       string
	Concurrency     int
	Verbose         bool
	DryRun          bool
	UseBwrap        bool          // Use bubblewrap sandbox for agent isolation
	MaxRetries      int           // Maximum number of times to retry failed tasks (0 = no retries, -1 = infinite)
	MaxPriority     int           // Hard filter: only run tasks with priority <= this value (-1 = no filter)
	ResolverTimeout time.Duration // Timeout for resolver agents (0 = use default 10m)
	Rules           *cfgpkg.RulesSettings // Task selection rules from config (nil = use MaxPriority only)
	Watch           bool          // Watch mode: keep running and poll for new tasks instead of exiting when queue is empty
	PollInterval    time.Duration // Interval between polling for new tasks in watch mode (default: 5s)
}

// Orchestrator coordinates the execution of tasks from beads
type Orchestrator struct {
	config           *Config
	beadsClient      beads.BeadsClient
	scheduler        *scheduler.Scheduler
	mergeCoordinator *mergecoordinator.MergeCoordinator
	tempDir          string
	callbackManager  *CallbackManager
	failureCounts    map[string]int // Tracks how many times each task has failed
	sandboxConfig    *sandbox.SandboxConfig
	taskFilter       *cfgpkg.TaskFilter // Task filter from rules settings (legacy)
	rulesEngine      *rules.Engine      // Rules engine for per-repo task selection

	// In-flight task tracking for dynamic task assignment
	inFlightMu   sync.RWMutex
	inFlight     map[string]bool   // Tasks currently being executed
	inFlightTask map[string]*beads.Task // Task objects for in-flight tasks (for concurrency limiting)

	// Watch mode statistics
	watchStatsMu    sync.RWMutex
	watchIterations int       // Number of polling iterations in watch mode
	watchStartTime  time.Time // When watch mode started
	watchTasksTotal int       // Total tasks processed in watch mode
}

// New creates a new orchestrator
func New(config *Config) (*Orchestrator, error) {
	// Create beads client
	beadsClient, err := beads.NewClient(config.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create beads client: %w", err)
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

	// Create agent executor
	executor := agent.NewExecutor(&agent.Config{
		Verbose:       config.Verbose,
		UseBwrap:      config.UseBwrap,
		SandboxConfig: sandboxConfig,
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
	}, beadsClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create merge coordinator: %w", err)
	}

	// Create task filter from rules settings
	var taskFilter *cfgpkg.TaskFilter
	if config.Rules != nil {
		taskFilter = cfgpkg.NewTaskFilter(config.Rules)
	}

	o := &Orchestrator{
		config:           config,
		beadsClient:      beadsClient,
		scheduler:        sched,
		mergeCoordinator: mc,
		tempDir:          tempDir,
		callbackManager:  NewCallbackManager(),
		failureCounts:    make(map[string]int),
		sandboxConfig:    sandboxConfig,
		taskFilter:       taskFilter,
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

			// Log merge result if verbose and there was an error
			if resp != nil && !resp.Success && o.config.Verbose {
				fmt.Fprintf(os.Stderr, "[%s] merge failed: %s\n", taskID, resp.Error)
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

// SetConcurrency updates the concurrency setting.
// Note: This updates the config for future reference but doesn't affect
// currently running tasks as the scheduler's semaphore is fixed at creation.
func (o *Orchestrator) SetConcurrency(concurrency int) {
	o.config.Concurrency = concurrency
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

// SetAgentID records the agentID for a taskID, enabling parent-child tracking for resolvers.
// This should be called when an agent starts execution.
func (o *Orchestrator) SetAgentID(taskID, agentID string) {
	o.mergeCoordinator.SetAgentID(taskID, agentID)
}

// GetAgentID retrieves the agentID for a taskID.
func (o *Orchestrator) GetAgentID(taskID string) string {
	return o.mergeCoordinator.GetAgentID(taskID)
}

// Run executes the orchestration loop until no ready tasks remain.
// This uses dynamic task assignment: each worker calls bd ready to get
// fresh tasks, ensuring newly-unblocked tasks are picked up immediately.
// In watch mode, the orchestrator polls for new tasks instead of exiting
// when the queue is empty.
func (o *Orchestrator) Run(ctx context.Context) error {
	// Start the merge coordinator's processor goroutine
	// It will process merge requests from the queue until context is cancelled
	o.mergeCoordinator.Start(ctx)

	// Dry run: show what would execute and exit
	if o.config.DryRun {
		return o.dryRun(ctx)
	}

	// Set default poll interval for watch mode
	pollInterval := o.config.PollInterval
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}

	// Initialize watch mode statistics
	if o.config.Watch {
		o.watchStatsMu.Lock()
		o.watchStartTime = time.Now()
		o.watchIterations = 0
		o.watchTasksTotal = 0
		o.watchStatsMu.Unlock()

		if o.config.Verbose {
			fmt.Printf("Watch mode enabled, polling every %v\n", pollInterval)
		}
	}

	// Create semaphore for bounded concurrency
	sem := semaphore.NewWeighted(int64(o.config.Concurrency))

	// Track active workers and results
	var wg sync.WaitGroup
	var resultsMu sync.Mutex
	var results []*agent.Result
	var runError error
	var runErrorMu sync.Mutex

	// Create a channel to signal when workers should check for new tasks
	// Workers send on this channel when they complete a task
	taskComplete := make(chan struct{}, o.config.Concurrency)

	// Completion callback removes task from in-flight and tracks results
	completionCallback := func(taskID string, result *agent.Result) {
		o.unmarkInFlight(taskID)

		resultsMu.Lock()
		results = append(results, result)

		// Track success/failure counts
		if result.Success {
			delete(o.failureCounts, taskID)
		} else {
			o.failureCounts[taskID]++

			// Check if retries exhausted
			if o.config.MaxRetries != -1 && o.failureCounts[taskID] > o.config.MaxRetries {
				// Mark as permanently failed in beads
				if err := o.beadsClient.Fail(ctx, taskID, fmt.Sprintf("Task failed after %d attempts", o.failureCounts[taskID])); err != nil {
					fmt.Fprintf(os.Stderr, "warning: could not mark task %s as failed in beads: %v\n", taskID, err)
				}
				fmt.Fprintf(os.Stderr, "ERROR: Task %s has failed %d times and will not be retried\n", taskID, o.failureCounts[taskID])

				// Signal error but don't stop immediately - let other workers finish
				// Note: In watch mode, we continue watching even after task failures
				if !o.config.Watch {
					runErrorMu.Lock()
					if runError == nil {
						runError = fmt.Errorf("task %s failed after %d retry attempts", taskID, o.config.MaxRetries)
					}
					runErrorMu.Unlock()
				}
			}
		}
		resultsMu.Unlock()

		// Signal that a task completed (non-blocking)
		select {
		case taskComplete <- struct{}{}:
		default:
		}
	}

	// Initial check for tasks
	task, shouldStop, err := o.getNextTask(ctx)
	if err != nil {
		return err
	}

	// Handle initial state when no tasks are available
	if shouldStop || task == nil {
		if o.config.Watch {
			// In watch mode, wait for tasks to appear
			if o.config.Verbose {
				fmt.Println("No ready tasks, watching for new tasks...")
			}
		} else {
			// Normal mode: exit immediately
			if o.config.Verbose {
				if shouldStop {
					fmt.Println("No tasks to execute, orchestration complete")
				} else {
					fmt.Println("No ready tasks, orchestration complete")
				}
			}
			return nil
		}
	}

	// Start the first worker with the initial task (if we have one)
	if task != nil {
		if o.config.Verbose {
			fmt.Printf("Starting task: %s: %s\n", task.ID, task.Title)
		}

		// Track task in watch mode stats
		if o.config.Watch {
			o.watchStatsMu.Lock()
			o.watchTasksTotal++
			o.watchStatsMu.Unlock()
		}

		// Acquire semaphore before spawning worker
		if err := sem.Acquire(ctx, 1); err != nil {
			return err
		}
		wg.Add(1)
		go func(t *beads.Task) {
			defer wg.Done()
			defer sem.Release(1)

			o.scheduler.ExecuteTask(ctx, t, completionCallback)
		}(task)
	}

	// Main loop: keep spawning workers as slots become available
	for {
		select {
		case <-ctx.Done():
			// Wait for in-flight tasks to complete
			wg.Wait()
			if o.config.Watch && o.config.Verbose {
				o.printWatchStats()
			}
			return ctx.Err()

		case <-taskComplete:
			// A task completed, try to get more work

		default:
			// Try to acquire a semaphore slot (non-blocking check first)
		}

		// Check if we have an error that should stop us (only in non-watch mode)
		if !o.config.Watch {
			runErrorMu.Lock()
			if runError != nil {
				runErrorMu.Unlock()
				// Wait for in-flight tasks
				wg.Wait()
				return runError
			}
			runErrorMu.Unlock()
		}

		// Try to acquire a semaphore slot
		if err := sem.Acquire(ctx, 1); err != nil {
			// Context cancelled
			wg.Wait()
			if o.config.Watch && o.config.Verbose {
				o.printWatchStats()
			}
			return err
		}

		// Got a slot, get the next task
		task, shouldStop, err := o.getNextTask(ctx)
		if err != nil {
			sem.Release(1)
			wg.Wait()
			return err
		}

		if shouldStop {
			// Stop condition met - release slot and wait for in-flight tasks
			sem.Release(1)
			wg.Wait()
			if o.config.Verbose {
				fmt.Println("Stop condition met, orchestration complete")
				if o.config.Watch {
					o.printWatchStats()
				}
			}
			return nil
		}

		if task == nil {
			// No task available right now
			sem.Release(1)

			// Check if there are any in-flight tasks
			o.inFlightMu.RLock()
			inFlightCount := len(o.inFlight)
			o.inFlightMu.RUnlock()

			if inFlightCount == 0 {
				// No in-flight tasks and no ready tasks
				if o.config.Watch {
					// Watch mode: poll for new tasks
					o.watchStatsMu.Lock()
					o.watchIterations++
					o.watchStatsMu.Unlock()

					if o.config.Verbose {
						fmt.Printf("Watching for new tasks (iteration %d)...\n", o.watchIterations)
					}

					select {
					case <-ctx.Done():
						wg.Wait()
						if o.config.Verbose {
							o.printWatchStats()
						}
						return ctx.Err()
					case <-time.After(pollInterval):
						// Poll again
						continue
					}
				} else {
					// Normal mode: we're done
					wg.Wait()
					if o.config.Verbose {
						fmt.Println("No more tasks, orchestration complete")
					}
					return nil
				}
			}

			// Wait for a task to complete before trying again
			select {
			case <-ctx.Done():
				wg.Wait()
				if o.config.Watch && o.config.Verbose {
					o.printWatchStats()
				}
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

		// Track task in watch mode stats
		if o.config.Watch {
			o.watchStatsMu.Lock()
			o.watchTasksTotal++
			o.watchStatsMu.Unlock()
		}

		wg.Add(1)
		go func(t *beads.Task) {
			defer wg.Done()
			defer sem.Release(1)

			o.scheduler.ExecuteTask(ctx, t, completionCallback)
		}(task)
	}
}

// printWatchStats prints watch mode statistics to stderr.
func (o *Orchestrator) printWatchStats() {
	o.watchStatsMu.RLock()
	defer o.watchStatsMu.RUnlock()

	duration := time.Since(o.watchStartTime)
	fmt.Fprintf(os.Stderr, "Watch mode: ran for %v, processed %d tasks across %d iterations\n",
		duration.Round(time.Second), o.watchTasksTotal, o.watchIterations)
}

// WatchStats contains statistics for watch mode operation.
type WatchStats struct {
	Enabled     bool          `json:"enabled"`
	StartTime   time.Time     `json:"start_time,omitempty"`
	Duration    time.Duration `json:"duration,omitempty"`
	Iterations  int           `json:"iterations"`
	TasksTotal  int           `json:"tasks_total"`
}

// GetWatchStats returns current watch mode statistics.
// Returns nil if watch mode is not enabled.
func (o *Orchestrator) GetWatchStats() *WatchStats {
	if !o.config.Watch {
		return &WatchStats{Enabled: false}
	}

	o.watchStatsMu.RLock()
	defer o.watchStatsMu.RUnlock()

	return &WatchStats{
		Enabled:    true,
		StartTime:  o.watchStartTime,
		Duration:   time.Since(o.watchStartTime),
		Iterations: o.watchIterations,
		TasksTotal: o.watchTasksTotal,
	}
}

// IsWatchMode returns true if the orchestrator is running in watch mode.
func (o *Orchestrator) IsWatchMode() bool {
	return o.config.Watch
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

// filterTasksByMaxPriority filters tasks to only include those with priority <= maxPriority.
// This is a hard filter applied after fetching tasks from beads.
// Deprecated: Use TaskFilter.FilterTasks() instead for full rules support.
func filterTasksByMaxPriority(tasks []beads.Task, maxPriority int) []beads.Task {
	if maxPriority < 0 {
		return tasks
	}

	filtered := make([]beads.Task, 0, len(tasks))
	for _, task := range tasks {
		if task.Priority <= maxPriority {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

// filterTasks applies configured rules to filter tasks.
// Priority: rulesEngine > taskFilter > maxPriority fallback
func (o *Orchestrator) filterTasks(tasks []beads.Task) []beads.Task {
	// If we have a rules engine, use it (preferred for per-repo isolation)
	if o.rulesEngine != nil {
		return o.filterTasksWithEngine(tasks)
	}

	// If we have a task filter (legacy), use it
	if o.taskFilter != nil {
		return o.taskFilter.FilterTasks(tasks)
	}

	// Fall back to simple maxPriority filter for backward compatibility
	return filterTasksByMaxPriority(tasks, o.config.MaxPriority)
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
	defer o.inFlightMu.Unlock()
	o.inFlight[taskID] = true
}

// markInFlightWithTask marks a task as currently in-flight and stores the task object
// for use in concurrency limit calculations.
func (o *Orchestrator) markInFlightWithTask(task *beads.Task) {
	o.inFlightMu.Lock()
	defer o.inFlightMu.Unlock()
	o.inFlight[task.ID] = true
	o.inFlightTask[task.ID] = task
}

// unmarkInFlight removes a task from the in-flight set
func (o *Orchestrator) unmarkInFlight(taskID string) {
	o.inFlightMu.Lock()
	defer o.inFlightMu.Unlock()
	delete(o.inFlight, taskID)
	delete(o.inFlightTask, taskID)
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
