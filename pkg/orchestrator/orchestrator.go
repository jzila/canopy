package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/merge"
	"github.com/jzila/canopy/pkg/resolver"
	"github.com/jzila/canopy/pkg/sandbox"
	"github.com/jzila/canopy/pkg/scheduler"
)

// EventCallbacks defines lifecycle callbacks for agent execution events
type EventCallbacks struct {
	// OnAgentStartFn is called when an agent begins execution
	OnAgentStartFn func(taskID string, task *beads.Task)

	// OnOutputFn is called when an agent produces output (stdout/stderr)
	OnOutputFn func(taskID string, output string, isError bool)

	// OnLiveFeedFn is called for real-time streaming events from agents
	OnLiveFeedFn func(taskID string, event *agent.LiveFeedEvent)

	// OnDoneFn is called when an agent completes successfully
	OnDoneFn func(taskID string, result *agent.Result)

	// OnFailFn is called when an agent fails
	OnFailFn func(taskID string, result *agent.Result)
}

// OnAgentStart implements scheduler.CallbackHandler
func (e *EventCallbacks) OnAgentStart(taskID string, task *beads.Task) {
	if e != nil && e.OnAgentStartFn != nil {
		e.OnAgentStartFn(taskID, task)
	}
}

// OnOutput implements scheduler.CallbackHandler
func (e *EventCallbacks) OnOutput(taskID string, output string, isError bool) {
	if e != nil && e.OnOutputFn != nil {
		e.OnOutputFn(taskID, output, isError)
	}
}

// OnLiveFeed implements scheduler.CallbackHandler
func (e *EventCallbacks) OnLiveFeed(taskID string, event *agent.LiveFeedEvent) {
	if e != nil && e.OnLiveFeedFn != nil {
		e.OnLiveFeedFn(taskID, event)
	}
}

// OnDone implements scheduler.CallbackHandler
func (e *EventCallbacks) OnDone(taskID string, result *agent.Result) {
	if e != nil && e.OnDoneFn != nil {
		e.OnDoneFn(taskID, result)
	}
}

// OnFail implements scheduler.CallbackHandler
func (e *EventCallbacks) OnFail(taskID string, result *agent.Result) {
	if e != nil && e.OnFailFn != nil {
		e.OnFailFn(taskID, result)
	}
}

// Config holds orchestrator configuration
type Config struct {
	WorkDir     string
	OutputDir   string
	Concurrency int
	Verbose     bool
	DryRun      bool
	UseBwrap    bool   // Use bubblewrap sandbox for agent isolation
	MaxRetries  int    // Maximum number of times to retry failed tasks (0 = no retries, -1 = infinite)
	Prompt      string // Prompt to filter/direct work selection
	MaxPriority int    // Hard filter: only run tasks with priority <= this value (-1 = no filter)
}

// Orchestrator coordinates the execution of tasks from beads
type Orchestrator struct {
	config        *Config
	beadsClient   *beads.Client
	scheduler     *scheduler.Scheduler
	merger        *merge.SequentialMerger
	resolver      *resolver.Resolver
	tempDir       string
	callbacks     *EventCallbacks
	failureCounts map[string]int // Tracks how many times each task has failed
	promptFilter  *PromptFilter  // Parsed prompt for filtering tasks
	beadsMu       sync.Mutex     // Serializes beads updates to prevent corruption
	mergeMu       sync.Mutex     // Serializes merge operations to prevent race conditions
	sandboxConfig *sandbox.SandboxConfig
	agentIDMap    sync.Map       // Maps taskID -> agentID for parent-child tracking
}

// New creates a new orchestrator
func New(config *Config) (*Orchestrator, error) {
	// Create beads client
	beadsClient, err := beads.NewClient(config.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create beads client: %w", err)
	}

	// Set up temp directory for overlays
	tempDir := filepath.Join(os.TempDir(), "canopy")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
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

	// Create merger
	merger := merge.NewSequentialMerger(config.OutputDir, tempDir, config.Verbose)

	// Create resolver for handling merge conflicts
	resolverInst := resolver.New(&resolver.Config{
		WorkDir:       config.WorkDir,
		TempDir:       tempDir,
		Verbose:       config.Verbose,
		UseBwrap:      config.UseBwrap,
		SandboxConfig: sandboxConfig,
	})

	// Set default MaxRetries if not specified (default: 3 retries)
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}

	// Parse prompt for filtering
	promptFilter, err := ParsePrompt(config.Prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse prompt: %w", err)
	}

	if config.Verbose && config.Prompt != "" {
		fmt.Printf("Prompt filter: %+v\n", promptFilter)
	}

	o := &Orchestrator{
		config:        config,
		beadsClient:   beadsClient,
		scheduler:     sched,
		merger:        merger,
		resolver:      resolverInst,
		tempDir:       tempDir,
		callbacks:     nil, // Set via SetCallbacks
		failureCounts: make(map[string]int),
		promptFilter:  promptFilter,
		sandboxConfig: sandboxConfig,
	}

	// Set up default internal callbacks for beads updates.
	// These will be wrapped with user callbacks if SetCallbacks is called later.
	o.setupInternalCallbacks(nil)

	return o, nil
}

// SetCallbacks configures event callbacks for the orchestrator.
// The orchestrator wraps the provided callbacks to also update beads status
// immediately when each task completes, ensuring timely status updates.
func (o *Orchestrator) SetCallbacks(callbacks *EventCallbacks) {
	o.callbacks = callbacks
	o.setupInternalCallbacks(callbacks)
}

// setupInternalCallbacks creates wrapper callbacks that include beads status updates.
// If userCallbacks is provided, they are called after the internal beads updates.
func (o *Orchestrator) setupInternalCallbacks(userCallbacks *EventCallbacks) {
	wrappedCallbacks := &EventCallbacks{
		OnAgentStartFn: func(taskID string, task *beads.Task) {
			if userCallbacks != nil && userCallbacks.OnAgentStartFn != nil {
				userCallbacks.OnAgentStartFn(taskID, task)
			}
		},
		OnOutputFn: func(taskID string, output string, isError bool) {
			if userCallbacks != nil && userCallbacks.OnOutputFn != nil {
				userCallbacks.OnOutputFn(taskID, output, isError)
			}
		},
		OnLiveFeedFn: func(taskID string, event *agent.LiveFeedEvent) {
			if userCallbacks != nil && userCallbacks.OnLiveFeedFn != nil {
				userCallbacks.OnLiveFeedFn(taskID, event)
			}
		},
		OnDoneFn: func(taskID string, result *agent.Result) {
			// Merge result immediately while overlay is still mounted
			o.mergeAndCleanup(result)

			// Update beads immediately when task completes
			o.markTaskDone(taskID)

			// Then call user's callback
			if userCallbacks != nil && userCallbacks.OnDoneFn != nil {
				userCallbacks.OnDoneFn(taskID, result)
			}
		},
		OnFailFn: func(taskID string, result *agent.Result) {
			// Clean up overlay for failed task (no merge needed)
			o.cleanupOverlay(result)

			// Update beads immediately when task fails
			o.markTaskFailed(taskID, result.Error)

			// Then call user's callback
			if userCallbacks != nil && userCallbacks.OnFailFn != nil {
				userCallbacks.OnFailFn(taskID, result)
			}
		},
	}

	// Pass wrapped callbacks through to scheduler
	if o.scheduler != nil {
		o.scheduler.SetCallbacks(wrappedCallbacks)
	}
}

// markTaskDone marks a task as completed in beads with proper synchronization.
// This is safe to call from concurrent goroutines.
func (o *Orchestrator) markTaskDone(taskID string) {
	o.beadsMu.Lock()
	defer o.beadsMu.Unlock()

	if err := o.beadsClient.Done(taskID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark task %s done: %v\n", taskID, err)
	}
}

// markTaskFailed marks a task as failed in beads with proper synchronization.
// This is safe to call from concurrent goroutines.
func (o *Orchestrator) markTaskFailed(taskID string, reason string) {
	o.beadsMu.Lock()
	defer o.beadsMu.Unlock()

	if err := o.beadsClient.Fail(taskID, reason); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to mark task %s as failed: %v\n", taskID, err)
	}
}

// mergeAndCleanup merges a single agent result and cleans up its overlay.
// This is called immediately when each agent completes, while the overlay is still mounted.
// This is safe to call from concurrent goroutines.
//
// The merge flow:
// 1. Acquire merge slot (bd merge-slot acquire)
// 2. Auto-commit any dirty .beads/ changes to prevent git am failures
// 3. Apply git patches or file changes
// 4. If git am fails, spawn resolver agent (inherits merge slot)
// 5. Release merge slot (bd merge-slot release)
func (o *Orchestrator) mergeAndCleanup(result *agent.Result) {
	// Use context for resolver agent execution
	ctx := context.Background()

	o.mergeAndCleanupWithContext(ctx, result, nil)
}

// mergeAndCleanupWithContext performs merge with optional task context for resolver spawning.
// The originalTask parameter is used to provide context to resolver agents when git am fails.
func (o *Orchestrator) mergeAndCleanupWithContext(ctx context.Context, result *agent.Result, originalTask *beads.Task) {
	// Serialize merge operations to ensure atomic commits
	o.mergeMu.Lock()
	defer o.mergeMu.Unlock()

	// Acquire merge slot before attempting git operations
	// This prevents race conditions with other agents trying to merge simultaneously
	if err := o.acquireMergeSlot(result.TaskID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to acquire merge slot for task %s: %v\n", result.TaskID, err)
		// Continue with merge anyway - the slot mechanism is best-effort
	}

	// Ensure we release the merge slot when done
	defer func() {
		if err := o.releaseMergeSlot(result.TaskID); err != nil && o.config.Verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to release merge slot for task %s: %v\n", result.TaskID, err)
		}
	}()

	// Auto-commit any dirty .beads/ changes before git am
	// This fixes canopy-pja: git am fails if .beads/ has uncommitted changes
	// because 'git am' won't apply patches when local changes would be overwritten
	if err := o.commitDirtyBeadsChanges(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to commit .beads/ changes: %v\n", err)
		// Continue with merge - better to try than to fail completely
	}

	// Merge the result (applies patches or file changes and commits)
	mergeResult, err := o.merger.MergeSingle(result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to merge result for task %s: %v\n", result.TaskID, err)
	}

	// Check if git am failed - if so, spawn a resolver agent
	if mergeResult.PatchFailed[result.TaskID] && result.GitState != nil && len(result.GitState.Patches) > 0 {
		fmt.Printf("[%s] Git patch failed, spawning resolver agent...\n", result.TaskID)

		// Build conflict context for the resolver
		conflictCtx := &resolver.ConflictContext{
			TaskID:        result.TaskID,
			TaskTitle:     result.TaskID, // Will be overridden if originalTask is available
			FailedPatches: result.GitState.Patches,
			PatchErrors:   mergeResult.Errors,
			FileChanges:   result.Changes,
			ParentAgentID: o.GetAgentID(result.TaskID), // Get parent agent ID for IPC tracking
		}

		// Add original task info if available
		if originalTask != nil {
			conflictCtx.TaskTitle = originalTask.Title
			conflictCtx.TaskDescription = originalTask.Description
		} else {
			// Try to fetch task info from beads
			if task, err := o.beadsClient.Show(result.TaskID); err == nil && task != nil {
				conflictCtx.TaskTitle = task.Title
				conflictCtx.TaskDescription = task.Description
			}
		}

		// Spawn resolver agent (inherits the merge slot - no need to re-acquire)
		// The resolver runs within the same merge lock, so it has exclusive access
		resolverResult, err := o.resolver.Resolve(ctx, conflictCtx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "resolver error for %s: %v\n", result.TaskID, err)
		}

		if resolverResult != nil {
			if resolverResult.Success {
				fmt.Printf("[%s-resolver] Conflict resolved successfully (%.1fs)\n",
					result.TaskID, resolverResult.Duration.Seconds())

				// Merge the resolver's result
				if resolverResult.AgentResult != nil && resolverResult.AgentResult.Overlay != nil {
					resolverMergeResult, err := o.merger.MergeSingle(resolverResult.AgentResult)
					if err != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to merge resolver result for %s: %v\n",
							result.TaskID, err)
					}

					// Report resolver merge errors
					for _, errMsg := range resolverMergeResult.Errors {
						fmt.Fprintf(os.Stderr, "resolver merge error for %s: %s\n", result.TaskID, errMsg)
					}

					if o.config.Verbose && resolverMergeResult.CommitsApplied > 0 {
						fmt.Printf("Merged %d commit(s) from resolver for task %s\n",
							resolverMergeResult.CommitsApplied, result.TaskID)
					}

					// Clean up resolver overlay
					resolverResult.AgentResult.Overlay.Cleanup()
				}
			} else {
				fmt.Fprintf(os.Stderr, "[%s-resolver] Failed to resolve conflict: %s\n",
					result.TaskID, resolverResult.Error)
				// Mark the original task as failed since resolver couldn't fix it
				o.markTaskFailed(result.TaskID, fmt.Sprintf("resolver failed: %s", resolverResult.Error))
			}
		}
	} else {
		// Report merge errors for non-resolver cases
		for _, errMsg := range mergeResult.Errors {
			fmt.Fprintf(os.Stderr, "merge error for %s: %s\n", result.TaskID, errMsg)
		}
	}

	if o.config.Verbose && mergeResult.CommitsApplied > 0 {
		fmt.Printf("Merged %d commit(s) from task %s\n", mergeResult.CommitsApplied, result.TaskID)
	}

	// Clean up overlay now that merge is complete
	o.cleanupOverlay(result)
}

// acquireMergeSlot acquires the merge slot for exclusive merge access.
// If the slot is held, this will add to the waiters queue with the task's priority.
func (o *Orchestrator) acquireMergeSlot(taskID string) error {
	// Use taskID as the holder identifier
	result, err := o.beadsClient.MergeSlotAcquire(taskID, true) // wait=true to join queue
	if err != nil {
		return err
	}

	if result != nil && !result.Acquired {
		// We're in the waiters queue - poll until we get the slot
		// The priority queue is managed by bd merge-slot
		return o.waitForMergeSlot(taskID)
	}

	return nil
}

// waitForMergeSlot polls for the merge slot with exponential backoff.
// This is called when the initial acquire put us in the waiters queue.
func (o *Orchestrator) waitForMergeSlot(taskID string) error {
	// Poll with increasing intervals
	intervals := []int{10, 20, 50, 100, 200, 500} // milliseconds
	maxAttempts := 60                              // ~30 seconds total with backoff

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Calculate sleep duration
		intervalIdx := attempt
		if intervalIdx >= len(intervals) {
			intervalIdx = len(intervals) - 1
		}
		sleepDuration := intervals[intervalIdx]

		// Sleep before retry
		time.Sleep(time.Duration(sleepDuration) * time.Millisecond)

		// Try to acquire again
		result, err := o.beadsClient.MergeSlotAcquire(taskID, false) // don't re-add to queue
		if err == nil && result != nil && result.Acquired {
			return nil
		}
	}

	return fmt.Errorf("timeout waiting for merge slot after %d attempts", maxAttempts)
}

// releaseMergeSlot releases the merge slot after merge is complete.
func (o *Orchestrator) releaseMergeSlot(taskID string) error {
	return o.beadsClient.MergeSlotRelease(taskID)
}

// commitDirtyBeadsChanges commits any uncommitted changes in the .beads/ directory.
// This prevents git am from failing when .beads/ files have been modified.
func (o *Orchestrator) commitDirtyBeadsChanges() error {
	// Check if .beads/ directory has uncommitted changes
	statusCmd := exec.Command("git", "status", "--porcelain", ".beads/")
	statusCmd.Dir = o.config.OutputDir
	output, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("git status failed: %w", err)
	}

	// No changes to commit
	if len(output) == 0 {
		return nil
	}

	if o.config.Verbose {
		fmt.Printf("Auto-committing dirty .beads/ changes before merge\n")
	}

	// Stage .beads/ changes
	addCmd := exec.Command("git", "add", ".beads/")
	addCmd.Dir = o.config.OutputDir
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add .beads/ failed: %w", err)
	}

	// Commit with a clear message
	commitCmd := exec.Command("git", "commit", "-m", "canopy: auto-commit beads changes before merge")
	commitCmd.Dir = o.config.OutputDir
	if err := commitCmd.Run(); err != nil {
		// Check if there's actually nothing to commit (possible race with bd sync)
		return fmt.Errorf("git commit failed: %w", err)
	}

	return nil
}

// cleanupOverlay cleans up the overlay for a result.
// This is safe to call from concurrent goroutines.
func (o *Orchestrator) cleanupOverlay(result *agent.Result) {
	if result.Overlay != nil {
		if err := result.Overlay.Cleanup(); err != nil && o.config.Verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to cleanup overlay for task %s: %v\n", result.TaskID, err)
		}
	}
}

// WithCallbacks is a builder-style method to set callbacks
func (o *Orchestrator) WithCallbacks(callbacks *EventCallbacks) *Orchestrator {
	o.SetCallbacks(callbacks)
	return o
}

// SetIPCClient sets the IPC client for the resolver to send parent-child agent events.
// This enables resolver agents to be tracked as children of the implementor agents.
func (o *Orchestrator) SetIPCClient(client *ipc.Client) {
	if o.resolver != nil {
		o.resolver.SetIPCClient(client)
	}
}

// SetAgentID records the agentID for a taskID, enabling parent-child tracking for resolvers.
// This should be called when an agent starts execution.
func (o *Orchestrator) SetAgentID(taskID, agentID string) {
	o.agentIDMap.Store(taskID, agentID)
}

// GetAgentID retrieves the agentID for a taskID.
func (o *Orchestrator) GetAgentID(taskID string) string {
	if val, ok := o.agentIDMap.Load(taskID); ok {
		return val.(string)
	}
	return ""
}

// Run executes the orchestration loop until no ready tasks remain
func (o *Orchestrator) Run(ctx context.Context) error {
	iteration := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		iteration++

		// Get ready tasks from beads (with optional filtering from prompt)
		var tasks []beads.Task
		var err error
		if o.promptFilter != nil && o.config.Prompt != "" {
			args := o.promptFilter.BuildBdReadyArgs()
			tasks, err = o.beadsClient.ReadyWithArgs(args...)
		} else {
			tasks, err = o.beadsClient.Ready()
		}
		if err != nil {
			return fmt.Errorf("failed to get ready tasks: %w", err)
		}

		// Apply hard max-priority filter (this is a hard filter, not a soft prompt)
		if o.config.MaxPriority >= 0 {
			tasks = filterTasksByMaxPriority(tasks, o.config.MaxPriority)
			if o.config.Verbose {
				fmt.Printf("After max-priority filter (<= P%d): %d tasks\n", o.config.MaxPriority, len(tasks))
			}
		}

		// Check stop condition
		if o.promptFilter != nil && o.promptFilter.ShouldStop(len(tasks)) {
			if o.config.Verbose {
				fmt.Printf("Stop condition met: %s\n", o.promptFilter.StopCondition)
			}
			return nil
		}

		if len(tasks) == 0 {
			if o.config.Verbose {
				fmt.Println("No more ready tasks, orchestration complete")
			}
			return nil
		}

		if o.config.Verbose {
			fmt.Printf("\n=== Iteration %d: %d ready tasks ===\n", iteration, len(tasks))
			for _, t := range tasks {
				fmt.Printf("  %s: %s\n", t.ID, t.Title)
			}
		}

		// Dry run: just show what would execute
		if o.config.DryRun {
			fmt.Printf("Would execute %d tasks:\n", len(tasks))
			for _, t := range tasks {
				fmt.Printf("  - %s: %s\n", t.ID, t.Title)
			}
			// In dry run, we don't actually execute, so we need to break
			// to avoid infinite loop (tasks remain ready)
			return nil
		}

		// Execute batch
		// NOTE: Merging and cleanup happen in the OnDone/OnFail callbacks
		// as each agent completes, so we don't need to do batch merge here.
		results, err := o.scheduler.ExecuteBatch(ctx, tasks)
		if err != nil {
			return fmt.Errorf("batch execution failed: %w", err)
		}

		// Summary of this iteration
		succeeded := 0
		failed := 0
		retriesExhausted := []string{}

		for _, r := range results {
			if r.Success {
				succeeded++
				// Clear failure count on success
				delete(o.failureCounts, r.TaskID)
			} else {
				failed++
				// Track failure count
				o.failureCounts[r.TaskID]++

				// Check if we've exceeded max retries (unless MaxRetries is -1 for infinite)
				if o.config.MaxRetries != -1 && o.failureCounts[r.TaskID] > o.config.MaxRetries {
					retriesExhausted = append(retriesExhausted, r.TaskID)
				}
			}
		}

		if o.config.Verbose {
			fmt.Printf("Iteration %d complete: %d succeeded, %d failed\n", iteration, succeeded, failed)
		}

		// If tasks have exhausted retries, stop retrying them
		if len(retriesExhausted) > 0 {
			fmt.Fprintf(os.Stderr, "\nERROR: The following tasks have failed %d times and will not be retried:\n", o.config.MaxRetries)
			for _, taskID := range retriesExhausted {
				fmt.Fprintf(os.Stderr, "  - %s\n", taskID)
				// Try to mark as failed one more time
				if err := o.beadsClient.Fail(taskID, fmt.Sprintf("Task failed after %d attempts", o.failureCounts[taskID])); err != nil {
					fmt.Fprintf(os.Stderr, "    warning: could not mark task as failed in beads: %v\n", err)
				}
			}
			fmt.Fprintf(os.Stderr, "\nStopping orchestration due to exhausted retries.\n")
			return fmt.Errorf("%d task(s) failed after %d retry attempts", len(retriesExhausted), o.config.MaxRetries)
		}
	}
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
