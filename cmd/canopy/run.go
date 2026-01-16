package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/orchestrator"
	"github.com/jzila/canopy/pkg/repository"
	sandboxpkg "github.com/jzila/canopy/pkg/sandbox"
)

var (
	concurrency int
	outputDir   string
	dryRun      bool
	useSandbox  bool
	maxRetries  int
	prompt      string
	maxPriority int
	stopAtGate  bool
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute ready tasks from beads",
	Long: `Executes all ready (unblocked) tasks from beads in parallel.

Each task runs in an isolated OverlayFS sandbox with its own copy
of the working directory. Changes are merged back after completion.

DAEMON CONNECTION
  The canopy daemon is required for monitoring and real-time updates.
  If the daemon is not running, it will be started automatically.

  The run command will fail if the daemon cannot be started or connected.

RETRY BEHAVIOR
  By default, failed tasks are retried up to 3 times. This prevents
  infinite retry loops when tasks consistently fail.

  Use --max-retries to customize:
  - --max-retries 0: No retries, fail immediately
  - --max-retries 3: Default, retry up to 3 times
  - --max-retries -1: Infinite retries (original behavior)

SECURITY
  By default, agents run with:
  - Filtered environment (only ANTHROPIC_*, PATH, LANG, LC_*, TERM, TMPDIR, TZ)
  - Hidden .claude/ directory (parent session settings not visible)
  - Process group isolation (clean termination on cancel)

  With --sandbox (requires bwrap):
  - Full namespace isolation (user, PID, IPC, UTS)
  - All capabilities dropped
  - Resource limits: 4GB memory, 100 processes, 1024 file descriptors
  - Read-only system mounts (/nix, /usr, /lib, /bin, /etc/ssl)
  - Only /workspace writable

Example:
  # Run with default settings (auto-starts daemon if needed)
  canopy run

  # Run with 8 concurrent agents
  canopy run --concurrency 8

  # Run with full sandbox isolation
  canopy run --sandbox

  # Run with no retries (fail immediately)
  canopy run --max-retries 0

  # Dry run to see what would execute
  canopy run --dry-run

  # Filter work by prompt (soft filter - agents may ignore)
  canopy run --prompt "Only work on P0 issues"
  canopy run --prompt "Focus on tasks only"
  canopy run --prompt "Stop after completing all P1s"

  # Hard filter by maximum priority (P0-P4, only tasks at or below this priority)
  canopy run --max-priority 2   # Only P0, P1, P2 tasks (excludes P3, P4)
  canopy run --max-priority 0   # Only P0 tasks (critical only)

  # Stop at gate tasks (tasks marked with gate=true)
  canopy run --stop-at-gate     # Stop before executing any gate task`,
	RunE: runOrchestrator,
}

func init() {
	runCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 4, "Maximum concurrent agents")
	runCmd.Flags().StringVarP(&outputDir, "output", "o", "", "Output directory for merged results (default: workdir)")
	runCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show execution plan without running")
	runCmd.Flags().BoolVar(&useSandbox, "sandbox", false, "Use bubblewrap (bwrap) for full process/filesystem isolation")
	runCmd.Flags().IntVar(&maxRetries, "max-retries", 3, "Maximum retry attempts for failed tasks (0=no retries, -1=infinite)")
	runCmd.Flags().StringVar(&prompt, "prompt", "", "Prompt to filter/direct work selection (e.g., 'Only work on P0 issues', 'Stop after completing all P1s')")
	runCmd.Flags().IntVar(&maxPriority, "max-priority", -1, "Hard filter: only run tasks with priority <= this value (0-4, -1=no filter)")
	runCmd.Flags().BoolVar(&stopAtGate, "stop-at-gate", false, "Stop orchestration when encountering a task marked as a gate")

	rootCmd.AddCommand(runCmd)
}

func runOrchestrator(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Orchestrator will be set once created, for signal cleanup
	var orch *orchestrator.Orchestrator

	// Handle interrupt signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nInterrupted, cleaning up overlays...")

		// Cancel context to stop agents
		cancel()

		// Synchronously cleanup all active overlays to prevent orphans
		if orch != nil {
			sched := orch.GetScheduler()
			if sched != nil {
				count, err := sched.CleanupAll(5 * time.Second)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: overlay cleanup encountered errors: %v\n", err)
				} else if count > 0 {
					fmt.Fprintf(os.Stderr, "Cleaned up %d active overlay(s)\n", count)
				}
			}
		}
	}()

	// Resolve working directory
	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return fmt.Errorf("invalid workdir: %w", err)
	}

	// Default output to workdir
	if outputDir == "" {
		outputDir = absWorkdir
	}

	// Initialize repository for tracking
	repo, err := repository.GetOrCreate(absWorkdir)
	if err != nil {
		// Non-fatal: repository tracking is optional
		if verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to initialize repository: %v\n", err)
		}
		repo = nil
	}

	// Connect to daemon (required for canopy run)
	ipcClient, err := ipc.GetClient()
	if err != nil {
		return fmt.Errorf("daemon required: %w\n\nThe canopy daemon is required for orchestration. "+
			"If the daemon failed to start, check the logs at ~/.cache/canopy/daemon.log", err)
	}
	defer ipcClient.Close()
	ipcClient.SetVerbose(verbose) // Enable verbose logging for reconnection events
	if verbose {
		fmt.Println("Connected to canopy daemon")
	}

	// Create and run orchestrator
	orch, err = orchestrator.New(&orchestrator.Config{
		WorkDir:     absWorkdir,
		OutputDir:   outputDir,
		Concurrency: concurrency,
		Verbose:     verbose,
		DryRun:      dryRun,
		UseBwrap:    useSandbox,
		MaxRetries:  maxRetries,
		Prompt:      prompt,
		MaxPriority: maxPriority,
		StopAtGate:  stopAtGate,
	})
	if err != nil {
		return fmt.Errorf("failed to create orchestrator: %w", err)
	}

	// Always create stats collector for history tracking
	runID := uuid.New().String()
	runStats := &runStatsCollector{
		runID:     runID,
		workDir:   absWorkdir,
		startTime: time.Now(),
	}

	// Get repo ID for IPC calls (empty string if repo is nil)
	repoID := ""
	if repo != nil {
		repoID = repo.ID
	}

	// Set up callbacks for history tracking and IPC
	// Note: parentAgentID is empty for top-level orchestrated agents
	// Child agents (e.g., resolvers) will populate this when spawned
	callbacks := &orchestrator.EventCallbacks{
		OnAgentStartFn: func(_ context.Context, taskID string, task *beads.Task) {
			runStats.recordTaskStart(taskID, task)
			agentID := makeAgentID(runID, taskID)
			// Record agent ID for parent-child tracking (resolver agents need this)
			orch.SetAgentID(taskID, agentID)
			parentAgentID := "" // Top-level agents have no parent
			if err := ipcClient.SendAgentStart(agentID, taskID, task.Title, parentAgentID, repoID); err != nil && verbose {
				fmt.Fprintf(os.Stderr, "warning: failed to send agent start: %v\n", err)
			}
		},
		OnOutputFn: func(_ context.Context, taskID string, output string, isError bool) {
			agentID := makeAgentID(runID, taskID)
			if err := ipcClient.SendAgentOutput(agentID, output, isError); err != nil && verbose {
				fmt.Fprintf(os.Stderr, "warning: failed to send agent output: %v\n", err)
			}
		},
		OnLiveFeedFn: func(_ context.Context, taskID string, event *agent.LiveFeedEvent) {
			agentID := makeAgentID(runID, taskID)
			if err := ipcClient.SendAgentLiveFeed(agentID, string(event.EventType), event.RawData); err != nil && verbose {
				fmt.Fprintf(os.Stderr, "warning: failed to send agent live feed: %v\n", err)
			}
		},
		OnDoneFn: func(_ context.Context, taskID string, result *agent.Result) {
			runStats.recordResult(taskID, result, true)
			agentID := makeAgentID(runID, taskID)
			parentAgentID := "" // Top-level agents have no parent
			// Send individual commit events before completion
			sendAgentCommits(ipcClient, agentID, result, verbose)
			ipcResult := convertToIPCResult(result)
			if err := ipcClient.SendAgentDone(agentID, parentAgentID, ipcResult); err != nil && verbose {
				fmt.Fprintf(os.Stderr, "warning: failed to send agent done: %v\n", err)
			}
		},
		OnFailFn: func(_ context.Context, taskID string, result *agent.Result) {
			runStats.recordResult(taskID, result, false)
			agentID := makeAgentID(runID, taskID)
			parentAgentID := "" // Top-level agents have no parent
			// Send individual commit events before failure (agent may have committed before failing)
			sendAgentCommits(ipcClient, agentID, result, verbose)
			ipcResult := convertToIPCResult(result)
			execErr := fmt.Errorf("%s", result.Error)
			if err := ipcClient.SendAgentFail(agentID, parentAgentID, execErr, ipcResult); err != nil && verbose {
				fmt.Fprintf(os.Stderr, "warning: failed to send agent fail: %v\n", err)
			}
		},
	}

	orch.SetCallbacks(callbacks)

	// Pass IPC client to orchestrator for resolver agent events
	orch.SetIPCClient(ipcClient)

	// Pass repo ID to orchestrator for resolver agent tracking
	if repo != nil {
		orch.SetRepoID(repo.ID)
	}

	// Pass run ID to orchestrator for unique agent ID generation
	orch.SetRunID(runID)

	// Get initial ready tasks to send task count
	beadsClient, err := beads.NewClient(absWorkdir)
	if err == nil {
		tasks, err := beadsClient.Ready(ctx)
		if err == nil && len(tasks) > 0 {
			if err := ipcClient.SendRunStarted(runID, len(tasks), repo); err != nil && verbose {
				fmt.Fprintf(os.Stderr, "warning: failed to send run started: %v\n", err)
			}
		}
	}

	// Send IPC completion after orchestration finishes
	// Note: Run persistence is handled by the daemon via the IPC events
	// (run_started creates the run, run_completed updates it with final stats)
	defer func() {
		if dryRun {
			return
		}

		// Send IPC completion - daemon will persist the run stats
		stats := runStats.getStats()
		if err := ipcClient.SendRunCompleted(runID, stats); err != nil && verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to send run completed: %v\n", err)
		}
	}()

	return orch.Run(ctx)
}

// convertToIPCResult converts agent.Result to ipc.AgentResult
func convertToIPCResult(result *agent.Result) *ipc.AgentResult {
	ipcResult := &ipc.AgentResult{
		ExitCode:        result.ExitCode,
		DurationSeconds: result.Duration.Seconds(),
		FilesChanged:    len(result.Changes),
	}

	if result.Output != nil {
		ipcResult.InputTokens = result.Output.TotalInputTokens
		ipcResult.OutputTokens = result.Output.TotalOutputTokens
		ipcResult.CacheCreationInputTokens = result.Output.CacheCreationInputTokens
		ipcResult.CacheReadInputTokens = result.Output.CacheReadInputTokens
		ipcResult.CostUSD = result.Output.CostUSD
		ipcResult.DurationMS = result.Output.DurationMS
		ipcResult.DurationAPIMS = result.Output.DurationAPIMS
		ipcResult.NumTurns = result.Output.NumTurns
		ipcResult.ResultMessage = result.Output.ResultMessage

		// Convert model usage
		if result.Output.ModelUsage != nil {
			ipcResult.ModelUsage = make(map[string]ipc.ModelUsage)
			for model, usage := range result.Output.ModelUsage {
				ipcResult.ModelUsage[model] = ipc.ModelUsage{
					InputTokens:              usage.InputTokens,
					OutputTokens:             usage.OutputTokens,
					CacheReadInputTokens:     usage.CacheReadInputTokens,
					CacheCreationInputTokens: usage.CacheCreationInputTokens,
					CostUSD:                  usage.CostUSD,
				}
			}
		}
	}

	if result.GitState != nil {
		ipcResult.CommitsCreated = len(result.GitState.NewCommits)
	}

	// Include stdout/stderr for persistence
	ipcResult.Stdout = result.Stdout
	ipcResult.Stderr = result.Stderr

	return ipcResult
}

// makeAgentID creates a unique agent ID by combining run ID prefix with task ID.
// Format: agent-{runID[:8]}-{taskID}
// This ensures agent IDs are unique per run, even when retrying the same task.
func makeAgentID(runID, taskID string) string {
	// Use first 8 characters of run ID as prefix for readability
	prefix := runID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return fmt.Sprintf("agent-%s-%s", prefix, taskID)
}

// sendAgentCommits sends individual commit events for each git commit made by an agent
func sendAgentCommits(client *ipc.Client, agentID string, result *agent.Result, verboseMode bool) {
	if client == nil || result == nil || result.GitState == nil {
		return
	}

	overlay := result.Overlay
	if overlay == nil {
		return
	}

	for _, commitHash := range result.GitState.NewCommits {
		// Get detailed commit info
		info, err := overlay.GetCommitInfo(commitHash)
		if err != nil {
			if verboseMode {
				fmt.Fprintf(os.Stderr, "warning: failed to get commit info for %s: %v\n", commitHash, err)
			}
			continue
		}

		commit := &ipc.AgentCommitPayload{
			Hash:         info.Hash,
			ShortHash:    info.ShortHash,
			Message:      info.Message,
			Author:       info.Author,
			AuthorEmail:  info.AuthorEmail,
			Timestamp:    info.Timestamp,
			FilesChanged: info.FilesChanged,
		}

		if err := client.SendAgentCommit(agentID, commit); err != nil && verboseMode {
			fmt.Fprintf(os.Stderr, "warning: failed to send agent commit: %v\n", err)
		}
	}
}

// sendAgentCommitsFromOverlay sends commits using the overlay directly (for when result.Overlay is nil)
func sendAgentCommitsFromOverlay(client *ipc.Client, agentID string, overlay *sandboxpkg.Overlay, gitState *sandboxpkg.GitState, verboseMode bool) {
	if client == nil || overlay == nil || gitState == nil {
		return
	}

	for _, commitHash := range gitState.NewCommits {
		info, err := overlay.GetCommitInfo(commitHash)
		if err != nil {
			if verboseMode {
				fmt.Fprintf(os.Stderr, "warning: failed to get commit info for %s: %v\n", commitHash, err)
			}
			continue
		}

		commit := &ipc.AgentCommitPayload{
			Hash:         info.Hash,
			ShortHash:    info.ShortHash,
			Message:      info.Message,
			Author:       info.Author,
			AuthorEmail:  info.AuthorEmail,
			Timestamp:    info.Timestamp,
			FilesChanged: info.FilesChanged,
		}

		if err := client.SendAgentCommit(agentID, commit); err != nil && verboseMode {
			fmt.Fprintf(os.Stderr, "warning: failed to send agent commit: %v\n", err)
		}
	}
}

// runStatsCollector tracks statistics across all agents in a run
type runStatsCollector struct {
	runID               string
	workDir             string
	startTime           time.Time
	totalTasks          int
	succeeded           int
	failed              int
	inputTokens         int
	outputTokens        int
	cacheCreationTokens int
	cacheReadTokens     int
	costUSD             float64
	totalTurns          int
	filesChanged        int
	gitCommits          int
	conflictsRes        int
	mu                  sync.Mutex
}

func (r *runStatsCollector) recordTaskStart(taskID string, task *beads.Task) {
	// Task start is tracked via IPC to daemon for real-time UI
	// Individual agent records are persisted by the daemon
}

func (r *runStatsCollector) recordResult(taskID string, result *agent.Result, success bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.totalTasks++
	if success {
		r.succeeded++
	} else {
		r.failed++
	}

	if result.Output != nil {
		r.inputTokens += result.Output.TotalInputTokens
		r.outputTokens += result.Output.TotalOutputTokens
		r.cacheCreationTokens += result.Output.CacheCreationInputTokens
		r.cacheReadTokens += result.Output.CacheReadInputTokens
		r.costUSD += result.Output.CostUSD
		r.totalTurns += result.Output.NumTurns
	}

	r.filesChanged += len(result.Changes)

	if result.GitState != nil {
		r.gitCommits += len(result.GitState.NewCommits)
	}
}

func (r *runStatsCollector) getStats() *ipc.RunStats {
	r.mu.Lock()
	defer r.mu.Unlock()

	return &ipc.RunStats{
		TotalTasks:                   r.totalTasks,
		SucceededTasks:               r.succeeded,
		FailedTasks:                  r.failed,
		TotalDuration:                time.Since(r.startTime).Seconds(),
		TotalInputTokens:             r.inputTokens,
		TotalOutputTokens:            r.outputTokens,
		TotalCacheCreationInputToken: r.cacheCreationTokens,
		TotalCacheReadInputTokens:    r.cacheReadTokens,
		TotalCostUSD:                 r.costUSD,
		TotalTurns:                   r.totalTurns,
		FilesChanged:                 r.filesChanged,
		GitCommits:                   r.gitCommits,
		ConflictsResolved:            r.conflictsRes,
	}
}

