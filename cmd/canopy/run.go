package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/orchestrator"
)

var (
	concurrency int
	outputDir   string
	dryRun      bool
	sandbox     bool
	daemonAddr  string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute ready tasks from beads",
	Long: `Executes all ready (unblocked) tasks from beads in parallel.

Each task runs in an isolated OverlayFS sandbox with its own copy
of the working directory. Changes are merged back after completion.

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
  # Run with default settings
  canopy run

  # Run with 8 concurrent agents
  canopy run --concurrency 8

  # Run with full sandbox isolation
  canopy run --sandbox

  # Dry run to see what would execute
  canopy run --dry-run`,
	RunE: runOrchestrator,
}

func init() {
	runCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 4, "Maximum concurrent agents")
	runCmd.Flags().StringVarP(&outputDir, "output", "o", "", "Output directory for merged results (default: workdir)")
	runCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show execution plan without running")
	runCmd.Flags().BoolVar(&sandbox, "sandbox", false, "Use bubblewrap (bwrap) for full process/filesystem isolation")
	runCmd.Flags().StringVar(&daemonAddr, "daemon", "", "Connect to canopyd at this address (e.g., /tmp/canopyd.sock)")

	rootCmd.AddCommand(runCmd)
}

func runOrchestrator(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nInterrupted, cleaning up...")
		cancel()
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

	// Set up IPC client if daemon address is provided
	var ipcClient *ipc.Client
	if daemonAddr != "" {
		client, err := ipc.NewClient(daemonAddr)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "warning: failed to connect to daemon at %s: %v\n", daemonAddr, err)
			}
		} else {
			ipcClient = client
			defer ipcClient.Close()
			if verbose {
				fmt.Printf("Connected to canopyd at %s\n", daemonAddr)
			}
		}
	}

	// Create and run orchestrator
	orch, err := orchestrator.New(&orchestrator.Config{
		WorkDir:     absWorkdir,
		OutputDir:   outputDir,
		Concurrency: concurrency,
		Verbose:     verbose,
		DryRun:      dryRun,
		UseBwrap:    sandbox,
	})
	if err != nil {
		return fmt.Errorf("failed to create orchestrator: %w", err)
	}

	// Set up callbacks if IPC client is connected
	if ipcClient != nil {
		runID := uuid.New().String()
		runStats := &runStatsCollector{
			runID:     runID,
			startTime: time.Now(),
		}

		callbacks := &orchestrator.EventCallbacks{
			OnAgentStartFn: func(taskID string, task *beads.Task) {
				agentID := fmt.Sprintf("agent-%s", taskID)
				if err := ipcClient.SendAgentStart(agentID, taskID, task.Title); err != nil && verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to send agent start: %v\n", err)
				}
			},
			OnOutputFn: func(taskID string, output string, isError bool) {
				agentID := fmt.Sprintf("agent-%s", taskID)
				if err := ipcClient.SendAgentOutput(agentID, output, isError); err != nil && verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to send agent output: %v\n", err)
				}
			},
			OnDoneFn: func(taskID string, result *agent.Result) {
				agentID := fmt.Sprintf("agent-%s", taskID)
				ipcResult := convertToIPCResult(result)
				if err := ipcClient.SendAgentDone(agentID, ipcResult); err != nil && verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to send agent done: %v\n", err)
				}
				runStats.recordResult(result, true)
			},
			OnFailFn: func(taskID string, result *agent.Result) {
				agentID := fmt.Sprintf("agent-%s", taskID)
				ipcResult := convertToIPCResult(result)
				execErr := fmt.Errorf("%s", result.Error)
				if err := ipcClient.SendAgentFail(agentID, execErr, ipcResult); err != nil && verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to send agent fail: %v\n", err)
				}
				runStats.recordResult(result, false)
			},
		}

		orch.SetCallbacks(callbacks)

		// Get initial ready tasks to send task count
		beadsClient, err := beads.NewClient(absWorkdir)
		if err == nil {
			tasks, err := beadsClient.Ready()
			if err == nil && len(tasks) > 0 {
				if err := ipcClient.SendRunStarted(runID, len(tasks)); err != nil && verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to send run started: %v\n", err)
				}
			}
		}

		// Send run completed after orchestration finishes
		defer func() {
			stats := runStats.getStats()
			if err := ipcClient.SendRunCompleted(runID, stats); err != nil && verbose {
				fmt.Fprintf(os.Stderr, "warning: failed to send run completed: %v\n", err)
			}
		}()
	}

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
		ipcResult.CostUSD = result.Output.CostUSD
	}

	if result.GitState != nil {
		ipcResult.CommitsCreated = len(result.GitState.NewCommits)
	}

	return ipcResult
}

// runStatsCollector tracks statistics across all agents in a run
type runStatsCollector struct {
	runID         string
	startTime     time.Time
	totalTasks    int
	succeeded     int
	failed        int
	inputTokens   int
	outputTokens  int
	costUSD       float64
	filesChanged  int
	conflictsRes  int
}

func (r *runStatsCollector) recordResult(result *agent.Result, success bool) {
	r.totalTasks++
	if success {
		r.succeeded++
	} else {
		r.failed++
	}

	if result.Output != nil {
		r.inputTokens += result.Output.TotalInputTokens
		r.outputTokens += result.Output.TotalOutputTokens
		r.costUSD += result.Output.CostUSD
	}

	r.filesChanged += len(result.Changes)
}

func (r *runStatsCollector) getStats() *ipc.RunStats {
	return &ipc.RunStats{
		TotalTasks:        r.totalTasks,
		SucceededTasks:    r.succeeded,
		FailedTasks:       r.failed,
		TotalDuration:     time.Since(r.startTime).Seconds(),
		TotalInputTokens:  r.inputTokens,
		TotalOutputTokens: r.outputTokens,
		TotalCostUSD:      r.costUSD,
		FilesChanged:      r.filesChanged,
		ConflictsResolved: r.conflictsRes,
	}
}
