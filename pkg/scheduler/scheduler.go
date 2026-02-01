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
	"github.com/jzila/canopy/pkg/metrics"
	"github.com/jzila/canopy/pkg/sandbox"
)

// CallbackHandler defines lifecycle callbacks for agent execution events.
// This allows the scheduler to be decoupled from orchestrator package.
// All callbacks receive a context.Context for proper cancellation and timeout propagation.
type CallbackHandler interface {
	OnAgentStart(ctx context.Context, taskID string, task *beads.Task)
	OnOutput(ctx context.Context, taskID string, output string, isError bool)
	OnLiveFeed(ctx context.Context, taskID string, event *agent.LiveFeedEvent)
	OnDone(ctx context.Context, taskID string, result *agent.Result)
	OnFail(ctx context.Context, taskID string, result *agent.Result)
}

// Scheduler executes tasks from beads in parallel with bounded concurrency
type Scheduler struct {
	beadsClient beads.BeadsClient
	executor    *agent.Executor
	config      *Config
	callbacks   CallbackHandler

	// Results storage - uses RWMutex because AllResults() requires iteration
	resultsMu sync.RWMutex
	results   map[string]*agent.Result

	// Pause/resume support
	pauseMu   sync.Mutex
	pauseCond *sync.Cond
	paused    bool

	// Per-agent kill support - sync.Map appropriate for store-then-delete pattern
	agentContexts sync.Map // map[string]context.CancelFunc

	// Active overlay tracking - uses RWMutex because CleanupAll() requires iteration
	overlaysMu     sync.RWMutex
	activeOverlays map[string]*sandbox.Overlay
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
		// Use XDG cache directory per persistence invariant
		// Fallback to os.TempDir() only if home directory lookup fails
		overlayDir, err := sandbox.GetOverlayBaseDir()
		if err != nil {
			overlayDir = filepath.Join(os.TempDir(), "canopy")
		}
		config.TempDir = overlayDir
	}

	s := &Scheduler{
		beadsClient:    beadsClient,
		executor:       executor,
		config:         config,
		callbacks:      nil, // Set via SetCallbacks
		results:        make(map[string]*agent.Result),
		activeOverlays: make(map[string]*sandbox.Overlay),
	}
	s.pauseCond = sync.NewCond(&s.pauseMu)
	return s
}

// SetCallbacks configures event callbacks for the scheduler
func (s *Scheduler) SetCallbacks(callbacks CallbackHandler) {
	s.callbacks = callbacks
}

// GetExecutor returns the underlying agent executor for runtime configuration updates.
func (s *Scheduler) GetExecutor() *agent.Executor {
	return s.executor
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
			mu.Unlock()

			s.resultsMu.Lock()
			s.results[task.ID] = result
			s.resultsMu.Unlock()

			// Invoke completion callbacks
			if s.callbacks != nil {
				if result.Success {
					s.callbacks.OnDone(agentCtx, task.ID, result)
				} else {
					s.callbacks.OnFail(agentCtx, task.ID, result)
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
		s.callbacks.OnAgentStart(ctx, task.ID, task)
	}

	// NOTE: beadsClient.Start() removed to prevent SQLite corruption
	// Task status updates now happen only in orchestrator after merge

	// Print agent start to console
	fmt.Printf("[%s] Starting: %s\n", task.ID, task.Title)

	// Gather dependency context from completed tasks
	deps := s.gatherDependencyContext(ctx, task)

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
		_ = overlay.Cleanup()
		return &agent.Result{
			TaskID:  task.ID,
			Success: false,
			Error:   errMsg,
		}
	}

	// Register overlay in active list for signal cleanup
	s.overlaysMu.Lock()
	s.activeOverlays[task.ID] = overlay
	s.overlaysMu.Unlock()
	metrics.IncOverlayMounts()
	defer func() {
		s.overlaysMu.Lock()
		delete(s.activeOverlays, task.ID)
		s.overlaysMu.Unlock()
		metrics.DecOverlayMounts()
	}()

	// Unmount when task completes, but don't delete directories yet
	defer func() { _ = overlay.Unmount() }()

	// Create per-task live feed callback if handler is configured
	var liveFeedCallback agent.LiveFeedCallback
	if s.callbacks != nil {
		liveFeedCallback = func(taskID string, event *agent.LiveFeedEvent) {
			s.callbacks.OnLiveFeed(ctx, taskID, event)
		}
	}

	// Execute the agent with dependency context and per-task callback
	result := s.executor.Execute(ctx, task, overlay, deps, liveFeedCallback)

	// Attach overlay to result so it can be cleaned up after merge
	result.Overlay = overlay

	// Invoke OnOutput callback for captured output
	if s.callbacks != nil {
		if result.Stdout != "" {
			s.callbacks.OnOutput(ctx, task.ID, result.Stdout, false)
		}
		if result.Stderr != "" {
			s.callbacks.OnOutput(ctx, task.ID, result.Stderr, true)
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
func (s *Scheduler) gatherDependencyContext(ctx context.Context, task *beads.Task) []agent.DependencyContext {
	var deps []agent.DependencyContext

	// Get dependency task IDs
	depIDs := task.GetDependencies()
	if len(depIDs) == 0 {
		// Try fetching from beads if not in task struct
		var err error
		depIDs, err = s.beadsClient.GetDeps(ctx, task.ID)
		if err != nil && s.config.Verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to get dependencies for task %s: %v\n", task.ID, err)
		}
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

// ExecuteTask executes a single task and returns its result.
// This is the building block for dynamic task assignment - the orchestrator
// calls this for each worker slot to execute individual tasks.
// The completion callback is invoked after the task completes to allow
// the orchestrator to update its in-flight tracking.
func (s *Scheduler) ExecuteTask(ctx context.Context, task *beads.Task, completionCallback func(taskID string, result *agent.Result)) *agent.Result {
	// Wait if scheduler is paused
	s.pauseMu.Lock()
	for s.paused {
		select {
		case <-ctx.Done():
			s.pauseMu.Unlock()
			return &agent.Result{
				TaskID:  task.ID,
				Success: false,
				Error:   ctx.Err().Error(),
			}
		default:
		}
		s.pauseCond.Wait()
	}
	s.pauseMu.Unlock()

	// Create cancellable context for this agent
	agentCtx, cancel := context.WithCancel(ctx)
	s.agentContexts.Store(task.ID, cancel)
	defer func() {
		s.agentContexts.Delete(task.ID)
		cancel()
	}()

	// Execute the task
	result := s.executeTask(agentCtx, task)

	// Store result
	s.resultsMu.Lock()
	s.results[task.ID] = result
	s.resultsMu.Unlock()

	// Invoke completion callbacks
	if s.callbacks != nil {
		if result.Success {
			s.callbacks.OnDone(agentCtx, task.ID, result)
		} else {
			s.callbacks.OnFail(agentCtx, task.ID, result)
		}
	}

	// Invoke the orchestrator's completion callback
	if completionCallback != nil {
		completionCallback(task.ID, result)
	}

	return result
}

// GetResult returns the result for a specific task
func (s *Scheduler) GetResult(taskID string) *agent.Result {
	s.resultsMu.RLock()
	defer s.resultsMu.RUnlock()
	return s.results[taskID]
}

// AllResults returns all stored results
func (s *Scheduler) AllResults() map[string]*agent.Result {
	s.resultsMu.RLock()
	defer s.resultsMu.RUnlock()
	results := make(map[string]*agent.Result, len(s.results))
	for k, v := range s.results {
		results[k] = v
	}
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

// CleanupOverlay cleans up the overlay for a specific task.
// This is safe to call even if the overlay doesn't exist or has already been cleaned up.
// Returns true if an overlay was found and cleaned up, false otherwise.
func (s *Scheduler) CleanupOverlay(taskID string) bool {
	s.overlaysMu.Lock()
	overlay, exists := s.activeOverlays[taskID]
	if exists {
		delete(s.activeOverlays, taskID)
	}
	s.overlaysMu.Unlock()

	if !exists {
		return false
	}

	// Unmount and cleanup - ignore errors as this is best-effort
	_ = overlay.Unmount()
	_ = overlay.Cleanup()
	return true
}

// CleanupAll synchronously unmounts all active overlays.
// This should be called on shutdown to ensure no orphaned FUSE mounts remain.
// Returns the number of overlays cleaned and any errors encountered.
func (s *Scheduler) CleanupAll(timeout time.Duration) (int, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []error

	// Create a channel to signal completion
	done := make(chan struct{})

	// Take a snapshot of active overlays under lock
	s.overlaysMu.RLock()
	overlays := make(map[string]*sandbox.Overlay, len(s.activeOverlays))
	for k, v := range s.activeOverlays {
		overlays[k] = v
	}
	s.overlaysMu.RUnlock()

	count := len(overlays)

	go func() {
		for taskID, overlay := range overlays {
			wg.Add(1)

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
			}(taskID, overlay)
		}

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
