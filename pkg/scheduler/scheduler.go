package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/john/canopy/pkg/agent"
	"github.com/john/canopy/pkg/beads"
	"github.com/john/canopy/pkg/sandbox"
)

// Scheduler executes tasks from beads in parallel with bounded concurrency
type Scheduler struct {
	beadsClient *beads.Client
	executor    *agent.Executor
	config      *Config
	results     sync.Map // map[string]*agent.Result
}

// Config holds scheduler configuration
type Config struct {
	Concurrency int
	TempDir     string
	WorkDir     string
	Verbose     bool
}

// NewScheduler creates a new task scheduler
func NewScheduler(beadsClient *beads.Client, executor *agent.Executor, config *Config) *Scheduler {
	if config.Concurrency <= 0 {
		config.Concurrency = 4
	}
	if config.TempDir == "" {
		config.TempDir = filepath.Join(os.TempDir(), "canopy")
	}

	return &Scheduler{
		beadsClient: beadsClient,
		executor:    executor,
		config:      config,
	}
}

// ExecuteBatch runs a batch of tasks in parallel
// Returns the results for all tasks in the batch
func (s *Scheduler) ExecuteBatch(ctx context.Context, tasks []beads.Task) ([]*agent.Result, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	sem := semaphore.NewWeighted(int64(s.config.Concurrency))
	g, gctx := errgroup.WithContext(ctx)

	var mu sync.Mutex
	var results []*agent.Result

	for _, task := range tasks {
		task := task // capture for goroutine

		g.Go(func() error {
			// Acquire semaphore slot
			if err := sem.Acquire(gctx, 1); err != nil {
				return err
			}
			defer sem.Release(1)

			// Execute the task
			result := s.executeTask(gctx, &task)

			// Store result
			mu.Lock()
			results = append(results, result)
			s.results.Store(task.ID, result)
			mu.Unlock()

			// Update beads status
			if result.Success {
				if err := s.beadsClient.Done(task.ID); err != nil && s.config.Verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to mark task %s done: %v\n", task.ID, err)
				}
			} else {
				if err := s.beadsClient.Fail(task.ID, result.Error); err != nil && s.config.Verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to mark task %s failed: %v\n", task.ID, err)
				}
			}

			return nil // Don't fail the group for individual task failures
		})
	}

	if err := g.Wait(); err != nil {
		return results, err
	}

	return results, nil
}

func (s *Scheduler) executeTask(ctx context.Context, task *beads.Task) *agent.Result {
	// Mark task as started
	if err := s.beadsClient.Start(task.ID); err != nil && s.config.Verbose {
		fmt.Fprintf(os.Stderr, "warning: failed to mark task %s started: %v\n", task.ID, err)
	}

	if s.config.Verbose {
		fmt.Printf("Starting task %s: %s\n", task.ID, task.Title)
	}

	// Gather dependency context from completed tasks
	deps := s.gatherDependencyContext(task)

	// Create overlay sandbox
	overlay, err := sandbox.NewOverlay(s.config.TempDir, s.config.WorkDir)
	if err != nil {
		return &agent.Result{
			TaskID:  task.ID,
			Success: false,
			Error:   fmt.Sprintf("failed to create sandbox: %v", err),
		}
	}
	defer overlay.Cleanup()

	// Mount the overlay
	if err := overlay.Mount(); err != nil {
		return &agent.Result{
			TaskID:  task.ID,
			Success: false,
			Error:   fmt.Sprintf("failed to mount sandbox: %v", err),
		}
	}
	defer overlay.Unmount()

	// Execute the agent with dependency context
	result := s.executor.Execute(ctx, task, overlay, deps)

	if s.config.Verbose {
		status := "completed"
		if !result.Success {
			status = fmt.Sprintf("failed: %s", result.Error)
		}
		commitInfo := ""
		if result.GitState != nil && len(result.GitState.NewCommits) > 0 {
			commitInfo = fmt.Sprintf(", %d commits", len(result.GitState.NewCommits))
		}
		fmt.Printf("Task %s %s (%.1fs, %d files changed%s)\n",
			task.ID, status, result.Duration.Seconds(), len(result.Changes), commitInfo)
	}

	return result
}

// gatherDependencyContext collects outputs from tasks this task depends on
func (s *Scheduler) gatherDependencyContext(task *beads.Task) []agent.DependencyContext {
	var deps []agent.DependencyContext

	// Get dependency task IDs
	depIDs := task.GetDependencies()
	if len(depIDs) == 0 {
		// Try fetching from beads if not in task struct
		depIDs, _ = s.beadsClient.GetDeps(task.ID)
	}

	for _, depID := range depIDs {
		// Look up the result from our completed tasks
		if result := s.GetResult(depID); result != nil && result.Success {
			dep := agent.DependencyContext{
				TaskID: depID,
			}

			// Extract summary from Claude output if available
			if result.Output != nil && len(result.Output.Messages) > 0 {
				// Get the last assistant message as summary
				for i := len(result.Output.Messages) - 1; i >= 0; i-- {
					if result.Output.Messages[i].Role == "assistant" {
						content := result.Output.Messages[i].Content
						// Truncate for summary (first 500 chars)
						if len(content) > 500 {
							dep.Summary = content[:500] + "..."
						} else {
							dep.Summary = content
						}
						dep.Output = content
						break
					}
				}
			}

			deps = append(deps, dep)
		}
	}

	return deps
}

// GetResult returns the result for a specific task
func (s *Scheduler) GetResult(taskID string) *agent.Result {
	if result, ok := s.results.Load(taskID); ok {
		return result.(*agent.Result)
	}
	return nil
}

// AllResults returns all stored results
func (s *Scheduler) AllResults() map[string]*agent.Result {
	results := make(map[string]*agent.Result)
	s.results.Range(func(key, value interface{}) bool {
		results[key.(string)] = value.(*agent.Result)
		return true
	})
	return results
}
