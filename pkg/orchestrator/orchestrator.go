package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/merge"
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

	// Create agent executor
	executor := agent.NewExecutor(&agent.Config{
		Verbose:  config.Verbose,
		UseBwrap: config.UseBwrap,
	})

	// Create scheduler
	sched := scheduler.NewScheduler(beadsClient, executor, &scheduler.Config{
		Concurrency: config.Concurrency,
		TempDir:     tempDir,
		WorkDir:     config.WorkDir,
		Verbose:     config.Verbose,
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

	return &Orchestrator{
		config:        config,
		beadsClient:   beadsClient,
		scheduler:     sched,
		merger:        merger,
		tempDir:       tempDir,
		callbacks:     nil, // Set via SetCallbacks
		failureCounts: make(map[string]int),
		promptFilter:  promptFilter,
	}, nil
}

// SetCallbacks configures event callbacks for the orchestrator
func (o *Orchestrator) SetCallbacks(callbacks *EventCallbacks) {
	o.callbacks = callbacks
	// Pass callbacks through to scheduler
	if o.scheduler != nil {
		o.scheduler.SetCallbacks(callbacks)
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
		results, err := o.scheduler.ExecuteBatch(ctx, tasks)
		if err != nil {
			return fmt.Errorf("batch execution failed: %w", err)
		}

		// Merge results
		mergeResult, err := o.merger.Merge(results)
		if err != nil {
			return fmt.Errorf("merge failed: %w", err)
		}

		// Clean up overlays now that merge is complete
		for _, r := range results {
			if r.Overlay != nil {
				if cleanupErr := r.Overlay.Cleanup(); cleanupErr != nil && o.config.Verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to cleanup overlay for task %s: %v\n", r.TaskID, cleanupErr)
				}
			}
		}

		// Report merge results
		if o.config.Verbose {
			fmt.Printf("Merged %d changes", len(mergeResult.Applied))
			if len(mergeResult.Conflicts) > 0 {
				fmt.Printf(" (%d conflicts resolved by last-writer-wins)", len(mergeResult.Conflicts))
			}
			if mergeResult.BeadsSynced {
				fmt.Printf(" (beads state synced)")
			}
			fmt.Println()
		}

		// Commit merged changes
		if err := o.commitMergedChanges(results); err != nil {
			// Log but don't fail - the changes are already merged
			fmt.Fprintf(os.Stderr, "warning: failed to commit merged changes: %v\n", err)
		}

		// Report errors
		for _, errMsg := range mergeResult.Errors {
			fmt.Fprintf(os.Stderr, "merge error: %s\n", errMsg)
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

// commitMergedChanges creates individual git commits for each task's file-only changes.
// Note: Tasks that made git commits have already been applied via git am with their
// original commit messages. This function only commits file-only changes (if any).
func (o *Orchestrator) commitMergedChanges(results []*agent.Result) error {
	// Identify tasks with file-only changes (no git commits)
	var fileOnlyTasks []*agent.Result
	for _, r := range results {
		if !r.Success {
			continue
		}
		// Task has file changes but no git commits
		hasFileChanges := len(r.Changes) > 0
		hasGitCommits := r.GitState != nil && len(r.GitState.Patches) > 0

		if hasFileChanges && !hasGitCommits {
			fileOnlyTasks = append(fileOnlyTasks, r)
		}
	}

	if len(fileOnlyTasks) == 0 {
		if o.config.Verbose {
			fmt.Println("No file-only changes to commit")
		}
		return nil
	}

	// Commit each task's file-only changes separately
	for _, task := range fileOnlyTasks {
		// Collect paths for this task
		var paths []string
		for _, change := range task.Changes {
			paths = append(paths, change.Path)
		}

		if len(paths) == 0 {
			continue
		}

		// Stage this task's files
		addCmd := exec.Command("git", "add", "--")
		addCmd.Args = append(addCmd.Args, paths...)
		addCmd.Dir = o.config.OutputDir
		if err := addCmd.Run(); err != nil {
			return fmt.Errorf("git add failed for task %s: %w", task.TaskID, err)
		}

		// Check if there are staged changes for this task
		diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
		diffCmd.Dir = o.config.OutputDir
		if err := diffCmd.Run(); err == nil {
			// No staged changes (exit code 0 means no diff)
			if o.config.Verbose {
				fmt.Printf("No changes to commit for task %s (files may have been committed via git am)\n", task.TaskID)
			}
			continue
		}

		// Create commit for this task
		commitMsg := fmt.Sprintf("canopy: apply changes from %s", task.TaskID)
		commitCmd := exec.Command("git", "commit", "-m", commitMsg)
		commitCmd.Dir = o.config.OutputDir
		if err := commitCmd.Run(); err != nil {
			return fmt.Errorf("git commit failed for task %s: %w", task.TaskID, err)
		}

		if o.config.Verbose {
			fmt.Printf("Created commit for file-only changes from task %s\n", task.TaskID)
		}
	}

	return nil
}
