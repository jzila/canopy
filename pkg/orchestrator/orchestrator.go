package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/merge"
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
	tempDir       string
	callbacks     *EventCallbacks
	failureCounts map[string]int // Tracks how many times each task has failed
	promptFilter  *PromptFilter  // Parsed prompt for filtering tasks
	beadsMu       sync.Mutex     // Serializes beads updates to prevent corruption
	mergeMu       sync.Mutex     // Serializes merge operations to prevent race conditions
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
		tempDir:       tempDir,
		callbacks:     nil, // Set via SetCallbacks
		failureCounts: make(map[string]int),
		promptFilter:  promptFilter,
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
func (o *Orchestrator) mergeAndCleanup(result *agent.Result) {
	// Serialize merge operations to ensure atomic commits
	o.mergeMu.Lock()
	defer o.mergeMu.Unlock()

	// Merge the result (applies patches or file changes and commits)
	mergeResult, err := o.merger.MergeSingle(result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to merge result for task %s: %v\n", result.TaskID, err)
	}

	// Report merge errors
	for _, errMsg := range mergeResult.Errors {
		fmt.Fprintf(os.Stderr, "merge error for %s: %s\n", result.TaskID, errMsg)
	}

	if o.config.Verbose && mergeResult.CommitsApplied > 0 {
		fmt.Printf("Merged %d commit(s) from task %s\n", mergeResult.CommitsApplied, result.TaskID)
	}

	// Clean up overlay now that merge is complete
	o.cleanupOverlay(result)
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
