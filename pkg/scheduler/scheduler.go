package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/sandbox"
)

// CallbackHandler defines lifecycle callbacks for agent execution events.
// This allows the scheduler to be decoupled from orchestrator package.
type CallbackHandler interface {
	OnAgentStart(taskID string, task *beads.Task)
	OnOutput(taskID string, output string, isError bool)
	OnLiveFeed(taskID string, event *agent.LiveFeedEvent)
	OnDone(taskID string, result *agent.Result)
	OnFail(taskID string, result *agent.Result)
}

// Scheduler executes tasks from beads in parallel with bounded concurrency
type Scheduler struct {
	beadsClient beads.BeadsClient
	executor    *agent.Executor
	config      *Config
	results     sync.Map // map[string]*agent.Result
	callbacks   CallbackHandler

	// Pause/resume support
	pauseMu sync.Mutex
	pauseCond *sync.Cond
	paused  bool

	// Per-agent kill support
	agentContexts sync.Map // map[string]context.CancelFunc

	// Active overlay tracking for signal cleanup
	activeOverlays sync.Map // map[string]*sandbox.Overlay
}

// Config holds scheduler configuration
type Config struct {
	Concurrency   int
	TempDir       string
	WorkDir       string
	Verbose       bool
	SandboxConfig *sandbox.SandboxConfig // Sandbox configuration from .canopy/sandbox.toml
}

// NewScheduler creates a new task scheduler
func NewScheduler(beadsClient beads.BeadsClient, executor *agent.Executor, config *Config) *Scheduler {
	if config.Concurrency <= 0 {
		config.Concurrency = 4
	}
	if config.TempDir == "" {
		config.TempDir = filepath.Join(os.TempDir(), "canopy")
	}

	s := &Scheduler{
		beadsClient: beadsClient,
		executor:    executor,
		config:      config,
		callbacks:   nil, // Set via SetCallbacks
	}
	s.pauseCond = sync.NewCond(&s.pauseMu)
	return s
}

// SetCallbacks configures event callbacks for the scheduler
func (s *Scheduler) SetCallbacks(callbacks CallbackHandler) {
	s.callbacks = callbacks
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
			// Wait if scheduler is paused before acquiring semaphore
			// This uses a condition variable to avoid TOCTOU races
			s.pauseMu.Lock()
			for s.paused {
				// Check if context is cancelled while waiting
				select {
				case <-gctx.Done():
					s.pauseMu.Unlock()
					return gctx.Err()
				default:
				}
				// Atomically release lock and wait for resume signal
				s.pauseCond.Wait()
			}
			s.pauseMu.Unlock()

			// Acquire semaphore slot
			if err := sem.Acquire(gctx, 1); err != nil {
				return err
			}
			defer sem.Release(1)

			// Create cancellable context for this agent
			agentCtx, cancel := context.WithCancel(gctx)
			s.agentContexts.Store(task.ID, cancel)
			defer func() {
				s.agentContexts.Delete(task.ID)
				cancel()
			}()

			// Execute the task
			result := s.executeTask(agentCtx, &task)

			// Store result
			mu.Lock()
			results = append(results, result)
			s.results.Store(task.ID, result)
			mu.Unlock()

			// Invoke completion callbacks
			if s.callbacks != nil {
				if result.Success {
					s.callbacks.OnDone(task.ID, result)
				} else {
					s.callbacks.OnFail(task.ID, result)
				}
			}

			// NOTE: Beads status updates have been moved to orchestrator (after merge)
			// to prevent SQLite corruption from concurrent writes

			return nil // Don't fail the group for individual task failures
		})
	}

	if err := g.Wait(); err != nil {
		return results, err
	}

	return results, nil
}

func (s *Scheduler) executeTask(ctx context.Context, task *beads.Task) *agent.Result {
	// Invoke OnAgentStart callback
	if s.callbacks != nil {
		s.callbacks.OnAgentStart(task.ID, task)
	}

	// NOTE: beadsClient.Start() removed to prevent SQLite corruption
	// Task status updates now happen only in orchestrator after merge

	// Print agent start to console
	fmt.Printf("[%s] Starting: %s\n", task.ID, task.Title)

	// Gather dependency context from completed tasks
	deps := s.gatherDependencyContext(task)

	// Create overlay sandbox
	overlay, err := sandbox.NewOverlay(s.config.TempDir, s.config.WorkDir)
	if err != nil {
		errMsg := fmt.Sprintf("failed to create sandbox: %v", err)
		if s.config.Verbose {
			fmt.Printf("Task %s failed: %s\n", task.ID, errMsg)
		}
		return &agent.Result{
			TaskID:  task.ID,
			Success: false,
			Error:   errMsg,
		}
	}
	// NOTE: Don't cleanup overlay here - it must remain until after merge completes
	// The orchestrator is responsible for cleaning up overlays after merge

	// Copy additional config files from sandbox config if present
	if s.config.SandboxConfig != nil {
		configPaths := s.config.SandboxConfig.GetAllCopyConfigs()
		if len(configPaths) > 0 {
			if err := overlay.CopyConfigPaths(configPaths); err != nil && s.config.Verbose {
				fmt.Fprintf(os.Stderr, "warning: failed to copy config paths: %v\n", err)
			}
		}
	}

	// Mount the overlay
	if err := overlay.Mount(); err != nil {
		errMsg := fmt.Sprintf("failed to mount sandbox: %v", err)
		if s.config.Verbose {
			fmt.Printf("Task %s failed: %s\n", task.ID, errMsg)
		}
		// Clean up on mount failure since we won't return the overlay
		overlay.Cleanup()
		return &agent.Result{
			TaskID:  task.ID,
			Success: false,
			Error:   errMsg,
		}
	}

	// Register overlay in active list for signal cleanup
	s.activeOverlays.Store(task.ID, overlay)
	defer s.activeOverlays.Delete(task.ID)

	// Unmount when task completes, but don't delete directories yet
	defer overlay.Unmount()

	// Create per-task live feed callback if handler is configured
	var liveFeedCallback agent.LiveFeedCallback
	if s.callbacks != nil {
		liveFeedCallback = func(taskID string, event *agent.LiveFeedEvent) {
			s.callbacks.OnLiveFeed(taskID, event)
		}
	}

	// Execute the agent with dependency context and per-task callback
	result := s.executor.Execute(ctx, task, overlay, deps, liveFeedCallback)

	// Attach overlay to result so it can be cleaned up after merge
	result.Overlay = overlay

	// Invoke OnOutput callback for captured output
	if s.callbacks != nil {
		if result.Stdout != "" {
			s.callbacks.OnOutput(task.ID, result.Stdout, false)
		}
		if result.Stderr != "" {
			s.callbacks.OnOutput(task.ID, result.Stderr, true)
		}
	}

	// Print agent completion to console
	if result.Success {
		fmt.Printf("[%s] Completed: %s\n", task.ID, task.Title)
		// Print commit messages with indentation
		if result.GitState != nil && len(result.GitState.CommitMessages) > 0 {
			for _, msg := range result.GitState.CommitMessages {
				// Print only the first line of the commit message (the subject)
				firstLine := strings.Split(msg, "\n")[0]
				fmt.Printf("  → %s\n", firstLine)
			}
		}
	} else {
		fmt.Printf("[%s] Failed: %s\n", task.ID, task.Title)
		if s.config.Verbose {
			fmt.Fprintf(os.Stderr, "  Error: %s\n", result.Error)
		}
	}

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

		// Print output when task fails to help debug issues
		if !result.Success {
			if result.Stdout != "" {
				fmt.Fprintf(os.Stderr, "Task %s stdout:\n%s\n", task.ID, result.Stdout)
			}
			if result.Stderr != "" {
				fmt.Fprintf(os.Stderr, "Task %s stderr:\n%s\n", task.ID, result.Stderr)
			}
			if result.Stdout == "" && result.Stderr == "" {
				fmt.Fprintf(os.Stderr, "Task %s produced no output on stdout or stderr\n", task.ID)
			}
		}
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

// Pause pauses the scheduler, preventing new agents from starting
func (s *Scheduler) Pause() {
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	s.paused = true
}

// Resume resumes the scheduler, allowing new agents to start
func (s *Scheduler) Resume() {
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	s.paused = false
	// Wake up all waiting goroutines
	s.pauseCond.Broadcast()
}

// IsPaused returns whether the scheduler is currently paused
func (s *Scheduler) IsPaused() bool {
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	return s.paused
}

// Kill terminates a specific agent by its task ID
func (s *Scheduler) Kill(agentID string) error {
	if cancelFunc, ok := s.agentContexts.Load(agentID); ok {
		cancelFunc.(context.CancelFunc)()
		s.agentContexts.Delete(agentID)
		return nil
	}
	return fmt.Errorf("agent %s not found", agentID)
}

// CleanupAll synchronously unmounts all active overlays.
// This should be called on shutdown to ensure no orphaned FUSE mounts remain.
// Returns the number of overlays cleaned and any errors encountered.
func (s *Scheduler) CleanupAll(timeout time.Duration) (int, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []error
	count := 0

	// Create a channel to signal completion
	done := make(chan struct{})

	go func() {
		s.activeOverlays.Range(func(key, value interface{}) bool {
			wg.Add(1)
			count++

			go func(taskID string, overlay *sandbox.Overlay) {
				defer wg.Done()

				// Try to unmount (it may already be unmounted by defer, but that's ok)
				if err := overlay.Unmount(); err != nil {
					mu.Lock()
					errors = append(errors, fmt.Errorf("task %s unmount: %w", taskID, err))
					mu.Unlock()
				}

				// Try to cleanup directories
				if err := overlay.Cleanup(); err != nil {
					mu.Lock()
					errors = append(errors, fmt.Errorf("task %s cleanup: %w", taskID, err))
					mu.Unlock()
				}
			}(key.(string), value.(*sandbox.Overlay))

			return true
		})

		wg.Wait()
		close(done)
	}()

	// Wait with timeout
	select {
	case <-done:
		// All cleanups completed
		if len(errors) > 0 {
			return count, fmt.Errorf("cleanup completed with %d error(s): %v", len(errors), errors)
		}
		return count, nil
	case <-time.After(timeout):
		return count, fmt.Errorf("cleanup timed out after %v (attempted %d overlays)", timeout, count)
	}
}
