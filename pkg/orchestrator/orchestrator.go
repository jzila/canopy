package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	UseBwrap    bool // Use bubblewrap sandbox for agent isolation
	MaxRetries  int  // Maximum number of times to retry failed tasks (0 = no retries, -1 = infinite)
}

// Orchestrator coordinates the execution of tasks from beads
type Orchestrator struct {
	config       *Config
	beadsClient  *beads.Client
	scheduler    *scheduler.Scheduler
	merger       *merge.SequentialMerger
	tempDir      string
	callbacks    *EventCallbacks
	failureCounts map[string]int // Tracks how many times each task has failed
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

	return &Orchestrator{
		config:        config,
		beadsClient:   beadsClient,
		scheduler:     sched,
		merger:        merger,
		tempDir:       tempDir,
		callbacks:     nil, // Set via SetCallbacks
		failureCounts: make(map[string]int),
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

		// Get ready tasks from beads
		tasks, err := o.beadsClient.Ready()
		if err != nil {
			return fmt.Errorf("failed to get ready tasks: %w", err)
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

// commitMergedChanges creates a git commit for merged changes from completed tasks
func (o *Orchestrator) commitMergedChanges(results []*agent.Result) error {
	// Check if there are any uncommitted changes
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = o.config.OutputDir
	statusOut, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("git status failed: %w", err)
	}

	// If no changes, don't create an empty commit
	if len(bytes.TrimSpace(statusOut)) == 0 {
		if o.config.Verbose {
			fmt.Println("No uncommitted changes to commit")
		}
		return nil
	}

	// Stage all changes
	addCmd := exec.Command("git", "add", ".")
	addCmd.Dir = o.config.OutputDir
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add failed: %w", err)
	}

	// Build commit message with task IDs
	var taskIDs []string
	for _, r := range results {
		if r.Success {
			taskIDs = append(taskIDs, r.TaskID)
		}
	}

	commitMsg := fmt.Sprintf("canopy: merge results from %s", strings.Join(taskIDs, ", "))

	// Create commit
	commitCmd := exec.Command("git", "commit", "-m", commitMsg)
	commitCmd.Dir = o.config.OutputDir
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}

	if o.config.Verbose {
		fmt.Printf("Created commit for merged changes from %d tasks\n", len(taskIDs))
	}

	return nil
}
