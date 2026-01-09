package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/john/canopy/pkg/agent"
	"github.com/john/canopy/pkg/beads"
	"github.com/john/canopy/pkg/merge"
	"github.com/john/canopy/pkg/scheduler"
)

// Config holds orchestrator configuration
type Config struct {
	WorkDir     string
	OutputDir   string
	Concurrency int
	Verbose     bool
	DryRun      bool
}

// Orchestrator coordinates the execution of tasks from beads
type Orchestrator struct {
	config      *Config
	beadsClient *beads.Client
	scheduler   *scheduler.Scheduler
	merger      *merge.SequentialMerger
	tempDir     string
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
		Verbose: config.Verbose,
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

	return &Orchestrator{
		config:      config,
		beadsClient: beadsClient,
		scheduler:   sched,
		merger:      merger,
		tempDir:     tempDir,
	}, nil
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

		// Report merge results
		if o.config.Verbose {
			fmt.Printf("Merged %d changes", len(mergeResult.Applied))
			if len(mergeResult.Conflicts) > 0 {
				fmt.Printf(" (%d conflicts resolved by last-writer-wins)", len(mergeResult.Conflicts))
			}
			fmt.Println()
		}

		// Report errors
		for _, errMsg := range mergeResult.Errors {
			fmt.Fprintf(os.Stderr, "merge error: %s\n", errMsg)
		}

		// Summary of this iteration
		succeeded := 0
		failed := 0
		for _, r := range results {
			if r.Success {
				succeeded++
			} else {
				failed++
			}
		}

		if o.config.Verbose {
			fmt.Printf("Iteration %d complete: %d succeeded, %d failed\n", iteration, succeeded, failed)
		}
	}
}

// Cleanup removes temporary files
func (o *Orchestrator) Cleanup() error {
	return os.RemoveAll(o.tempDir)
}
