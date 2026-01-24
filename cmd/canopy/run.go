package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/repository"
	"github.com/jzila/canopy/pkg/runtime"
	"github.com/jzila/canopy/pkg/sandbox"
)

var (
	concurrency      int
	outputDir        string
	dryRun           bool
	useSandbox       bool
	maxRetries       int
	maxPriority      int
	resolverTimeout  time.Duration
	resumeAgents     bool
	filterTypes      []string
	excludeTypes     []string
	filterLabels     []string
	excludeLabels    []string
	filterAssignee   string
	pollInterval     time.Duration
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute ready tasks from beads",
	Long: `Executes ready (unblocked) tasks from beads in parallel, polling continuously.

The orchestrator runs continuously until cancelled (Ctrl+C):
- When work is available: processes tasks (Active state)
- When no work is available: sleeps for poll interval (Idle state)

Each task runs in an isolated sandbox with its own copy of the working
directory. Changes are merged back after completion.

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

TASK SELECTION
  Canopy uses explicit filters for task selection (no natural language parsing).

  CLI flags override settings in .canopy/config.toml [rules] section:
  - --max-priority: Only run tasks with priority <= this value (0-4)
  - --type: Only run tasks of these types (comma-separated: bug,task,feature,chore)
  - --exclude-type: Exclude tasks of these types (comma-separated)
  - --label: Only run tasks with these labels (comma-separated)
  - --exclude-label: Exclude tasks with these labels (comma-separated)
  - --assignee: Filter by assignee ("" = unassigned, "*" = any, name = exact match)

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

  # Hard filter by maximum priority (P0-P4, only tasks at or below this priority)
  canopy run --max-priority 2   # Only P0, P1, P2 tasks (excludes P3, P4)
  canopy run --max-priority 0   # Only P0 tasks (critical only)

  # Filter by type
  canopy run --type bug,task        # Only bugs and tasks
  canopy run --exclude-type epic    # Exclude epics

  # Filter by labels
  canopy run --label frontend       # Only tasks with frontend label
  canopy run --exclude-label wip    # Exclude work-in-progress tasks

  # Filter by assignee
  canopy run --assignee john        # Only tasks assigned to john
  canopy run --assignee ""          # Only unassigned tasks

  # Set resolver timeout for conflict resolution
  canopy run --resolver-timeout 15m   # 15 minute timeout (default: 10m)
  canopy run --resolver-timeout 30m   # 30 minute timeout for complex conflicts

  # Resume interrupted agents after daemon restart
  canopy run --resume                 # Resume agents that were interrupted

  # Set poll interval for idle state
  canopy run --poll-interval 10s      # Poll every 10 seconds (default: 5s)`,
	RunE: runOrchestrator,
}

func init() {
	runCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 4, "Maximum concurrent agents")
	runCmd.Flags().StringVarP(&outputDir, "output", "o", "", "Output directory for merged results (default: workdir)")
	runCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show execution plan without running")
	runCmd.Flags().BoolVar(&useSandbox, "sandbox", false, "Use bubblewrap (bwrap) for full process/filesystem isolation")
	runCmd.Flags().IntVar(&maxRetries, "max-retries", 3, "Maximum retry attempts for failed tasks (0=no retries, -1=infinite)")
	runCmd.Flags().IntVar(&maxPriority, "max-priority", -1, "Hard filter: only run tasks with priority <= this value (0-4, -1=no filter)")
	runCmd.Flags().DurationVar(&resolverTimeout, "resolver-timeout", 0, "Timeout for resolver agents when resolving merge conflicts (e.g., 10m, 15m, 1h). Default: 10m. Set from CANOPY_RESOLVER_TIMEOUT env var if not specified.")
	runCmd.Flags().BoolVar(&resumeAgents, "resume", false, "Resume agents that were interrupted by daemon restart")

	// Task selection filters (override config.toml [rules] section)
	runCmd.Flags().StringSliceVar(&filterTypes, "type", nil, "Only run tasks of these types (comma-separated: bug,task,feature,chore,epic)")
	runCmd.Flags().StringSliceVar(&excludeTypes, "exclude-type", nil, "Exclude tasks of these types (comma-separated)")
	runCmd.Flags().StringSliceVar(&filterLabels, "label", nil, "Only run tasks with these labels (comma-separated)")
	runCmd.Flags().StringSliceVar(&excludeLabels, "exclude-label", nil, "Exclude tasks with these labels (comma-separated)")
	runCmd.Flags().StringVar(&filterAssignee, "assignee", "", "Filter by assignee (\"\" = unassigned only, \"*\" = any, name = exact match)")

	// Poll interval for idle state
	runCmd.Flags().DurationVar(&pollInterval, "poll-interval", 5*time.Second, "Interval between polling for new tasks when idle")

	rootCmd.AddCommand(runCmd)
}

func runOrchestrator(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Clean up any stale mounts from previous crashes before starting
	// This prevents "permission denied" errors from orphaned FUSE mounts
	// Use XDG cache directory per persistence invariant
	tempDir, err := sandbox.GetOverlayBaseDir()
	if err != nil {
		// Fallback to os.TempDir() if home directory lookup fails
		tempDir = filepath.Join(os.TempDir(), "canopy")
	}
	if cleaned, stale, errs := sandbox.RecoverFromCrash(tempDir); stale > 0 {
		if verbose {
			fmt.Fprintf(os.Stderr, "Recovered %d/%d stale overlay mounts from previous run\n", cleaned, stale)
			for _, err := range errs {
				fmt.Fprintf(os.Stderr, "  warning: %v\n", err)
			}
		}
	}

	// Resolve working directory
	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return fmt.Errorf("invalid workdir: %w", err)
	}

	// Default output to workdir
	if outputDir == "" {
		outputDir = absWorkdir
	}

	// Resolve resolver timeout with precedence: CLI flag > env var > default
	effectiveResolverTimeout := resolverTimeout
	if effectiveResolverTimeout == 0 {
		// Try environment variable
		if envTimeout := os.Getenv("CANOPY_RESOLVER_TIMEOUT"); envTimeout != "" {
			parsed, err := time.ParseDuration(envTimeout)
			if err != nil {
				return fmt.Errorf("invalid CANOPY_RESOLVER_TIMEOUT %q: %w", envTimeout, err)
			}
			effectiveResolverTimeout = parsed
		}
	}
	// Note: if still 0, the daemon will use its default of 10 minutes

	// Initialize repository for tracking
	repo, err := repository.GetOrCreate(absWorkdir)
	if err != nil {
		// Non-fatal: repository tracking is optional
		if verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to initialize repository: %v\n", err)
		}
		repo = nil
	}

	// Get repo ID for daemon calls
	repoID := ""
	if repo != nil {
		repoID = repo.ID
	}

	// Connect to daemon (required for canopy run)
	ipcClient, err := ipc.GetClient()
	if err != nil {
		return fmt.Errorf("daemon required: %w\n\nThe canopy daemon is required for orchestration. "+
			"If the daemon failed to start, check the logs at ~/.cache/canopy/daemon.log", err)
	}
	defer func() { _ = ipcClient.Close() }()
	ipcClient.SetVerbose(verbose) // Enable verbose logging for reconnection events
	if verbose {
		fmt.Println("Connected to canopy daemon")
	}

	// If --resume flag is set, resume interrupted agents first
	if resumeAgents {
		resumed, errs := resumeInterruptedAgents(ctx, absWorkdir, useSandbox, verbose, ipcClient)
		if resumed > 0 {
			fmt.Printf("Resumed %d interrupted agent(s)\n", resumed)
		}
		for _, err := range errs {
			fmt.Fprintf(os.Stderr, "warning: resume error: %v\n", err)
		}
		// Exit after resuming if --resume was the primary operation
		if dryRun {
			return nil
		}
	}

	// Start the orchestrator run via daemon HTTP API
	runID, err := startDaemonRun(ctx, startRunRequest{
		WorkDir:           absWorkdir,
		OutputDir:         outputDir,
		Concurrency:       concurrency,
		Verbose:           verbose,
		DryRun:            dryRun,
		UseBwrap:          useSandbox,
		MaxRetries:        maxRetries,
		MaxPriority:       maxPriority,
		ResolverTimeoutMS: effectiveResolverTimeout.Milliseconds(),
		RepoID:            repoID,
		PollIntervalMS:    pollInterval.Milliseconds(),
		Types:             filterTypes,
		ExcludeTypes:      excludeTypes,
		Labels:            filterLabels,
		ExcludeLabels:     excludeLabels,
		Assignee:          filterAssignee,
	})
	if err != nil {
		return fmt.Errorf("failed to start orchestrator run: %w", err)
	}

	if verbose {
		fmt.Printf("Started orchestrator run: %s\n", runID)
	}

	// Set up signal handler to stop the run on Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nInterrupted, stopping orchestrator run...")
		if err := stopDaemonRun(runID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to stop run: %v\n", err)
		}
		cancel()
	}()

	// Stream events via WebSocket until the run completes
	return streamRunEvents(ctx, runID, verbose)
}

// startRunRequest matches the daemon's ExecuteRunRequest
type startRunRequest struct {
	WorkDir           string   `json:"work_dir"`
	OutputDir         string   `json:"output_dir,omitempty"`
	Concurrency       int      `json:"concurrency,omitempty"`
	Verbose           bool     `json:"verbose,omitempty"`
	DryRun            bool     `json:"dry_run,omitempty"`
	UseBwrap          bool     `json:"use_bwrap,omitempty"`
	MaxRetries        int      `json:"max_retries,omitempty"`
	MaxPriority       int      `json:"max_priority,omitempty"`
	ResolverTimeoutMS int64    `json:"resolver_timeout_ms,omitempty"`
	RepoID            string   `json:"repo_id,omitempty"`
	PollIntervalMS    int64    `json:"poll_interval_ms,omitempty"`
	Types             []string `json:"types,omitempty"`
	ExcludeTypes      []string `json:"exclude_types,omitempty"`
	Labels            []string `json:"labels,omitempty"`
	ExcludeLabels     []string `json:"exclude_labels,omitempty"`
	Assignee          string   `json:"assignee,omitempty"`
}

// startRunResponse matches the daemon's ExecuteRunResponse
type startRunResponse struct {
	Success bool   `json:"success"`
	RunID   string `json:"run_id,omitempty"`
	Error   string `json:"error,omitempty"`
}

// stopRunRequest matches the daemon's StopRunRequest
type stopRunRequest struct {
	RunID string `json:"run_id"`
}

// stopRunResponse matches the daemon's StopRunResponse
type stopRunResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// startDaemonRun calls the daemon's /api/orchestrator/run endpoint to start a run
func startDaemonRun(ctx context.Context, req startRunRequest) (string, error) {
	url := fmt.Sprintf("http://localhost:%d/api/orchestrator/run", runtime.DefaultDaemonPort)

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &bodyReader{data: body})
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.ContentLength = int64(len(body))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to call daemon: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result startRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return "", fmt.Errorf("daemon returned error: %s", result.Error)
	}

	return result.RunID, nil
}

// bodyReader wraps a byte slice for use as io.ReadCloser
type bodyReader struct {
	data []byte
	pos  int
}

func (r *bodyReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *bodyReader) Close() error {
	return nil
}

// stopDaemonRun calls the daemon's /api/orchestrator/run/stop endpoint
func stopDaemonRun(runID string) error {
	url := fmt.Sprintf("http://localhost:%d/api/orchestrator/run/stop", runtime.DefaultDaemonPort)

	req := stopRunRequest{RunID: runID}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, url, &bodyReader{data: body})
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.ContentLength = int64(len(body))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to call daemon: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result stopRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("daemon returned error: %s", result.Error)
	}

	return nil
}

// streamRunEvents connects to the WebSocket and streams events until the run completes
func streamRunEvents(ctx context.Context, runID string, verbose bool) error {
	// For now, just poll the run status until it completes
	// TODO: Implement WebSocket streaming for real-time output
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := getRunStatus(runID)
			if err != nil {
				if verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to get run status: %v\n", err)
				}
				continue
			}

			if verbose {
				fmt.Printf("Run status: %s (tasks: %d/%d, failed: %d)\n",
					status.Status, status.TasksDone, status.TasksTotal, status.TasksFailed)
			}

			// Check if run is terminal
			switch status.Status {
			case "completed":
				fmt.Printf("Run completed: %d/%d tasks succeeded\n", status.TasksDone, status.TasksTotal)
				return nil
			case "failed":
				return fmt.Errorf("run failed: %s", status.Error)
			case "cancelled":
				return fmt.Errorf("run cancelled")
			}
		}
	}
}

// runStatus matches the daemon's RunStatusWire
type runStatus struct {
	ID          string `json:"id"`
	RepoPath    string `json:"repo_path"`
	RepoID      string `json:"repo_id,omitempty"`
	Status      string `json:"status"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time,omitempty"`
	Error       string `json:"error,omitempty"`
	TasksTotal  int    `json:"tasks_total"`
	TasksDone   int    `json:"tasks_done"`
	TasksFailed int    `json:"tasks_failed"`
}

// runStatusResponse matches the daemon's RunStatusResponse
type runStatusResponse struct {
	Success bool       `json:"success"`
	Run     *runStatus `json:"run,omitempty"`
	Error   string     `json:"error,omitempty"`
}

// getRunStatus queries the daemon for run status
func getRunStatus(runID string) (*runStatus, error) {
	url := fmt.Sprintf("http://localhost:%d/api/orchestrator/runs/%s", runtime.DefaultDaemonPort, runID)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to call daemon: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result runStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("daemon returned error: %s", result.Error)
	}

	return result.Run, nil
}

// convertToIPCResult converts agent.Result to ipc.AgentResult
// (kept for resumeInterruptedAgents)
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
		ipcResult.SessionID = result.Output.SessionID

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

// resumableAgentInfo is the wire format for resumable agent data from the daemon
type resumableAgentInfo struct {
	AgentID       string `json:"agent_id"`
	TaskID        string `json:"task_id"`
	TaskTitle     string `json:"task_title,omitempty"`
	RunID         string `json:"run_id"`
	SessionID     string `json:"session_id"`
	UpperDir      string `json:"upper_dir"`
	LowerDir      string `json:"lower_dir"`
	WorkDir       string `json:"work_dir"`
	MergedDir     string `json:"merged_dir"`
	InterruptedAt int64  `json:"interrupted_at"`
}

// resumeInterruptedAgents queries the daemon for resumable agents and resumes them.
// Returns the number of agents successfully resumed and any errors encountered.
func resumeInterruptedAgents(ctx context.Context, workDir string, useBwrap, verbose bool, ipcClient *ipc.Client) (int, []error) {
	// Get daemon HTTP endpoint (use default port - could be made configurable)
	url := fmt.Sprintf("http://localhost:%d/api/agents/resumable", runtime.DefaultDaemonPort)

	// Query daemon for resumable agents
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, []error{fmt.Errorf("failed to create request: %w", err)}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, []error{fmt.Errorf("failed to query daemon: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, []error{fmt.Errorf("daemon returned %d: %s", resp.StatusCode, string(body))}
	}

	// Parse response
	var agents []resumableAgentInfo
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return 0, []error{fmt.Errorf("failed to decode response: %w", err)}
	}

	if len(agents) == 0 {
		if verbose {
			fmt.Println("No interrupted agents to resume")
		}
		return 0, nil
	}

	if verbose {
		fmt.Printf("Found %d interrupted agent(s) to resume\n", len(agents))
	}

	// Create executor for resuming agents
	executor := agent.NewExecutor(&agent.Config{
		UseBwrap: useBwrap,
		Verbose:  verbose,
	})

	var resumed int
	var errors []error

	// Resume each agent
	for _, a := range agents {
		if verbose {
			fmt.Printf("Resuming agent %s (task: %s, session: %s)\n", a.AgentID, a.TaskID, a.SessionID)
		}

		// Remount the overlay
		overlay, err := sandbox.RemountOverlay(a.LowerDir, a.UpperDir, a.WorkDir, a.MergedDir)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to remount overlay for %s: %w", a.AgentID, err))
			continue
		}

		// Create task object for executor
		task := &beads.Task{
			ID:    a.TaskID,
			Title: a.TaskTitle,
		}

		// Create live feed callback to send events to daemon
		liveFeedCallback := func(taskID string, event *agent.LiveFeedEvent) {
			if ipcClient != nil {
				_ = ipcClient.SendAgentLiveFeed(a.AgentID, string(event.EventType), event.RawData)
			}
		}

		// Resume the agent using claude --resume
		result := executor.ExecuteResume(ctx, task, overlay, a.SessionID, liveFeedCallback)

		// Send result to daemon via IPC
		if result != nil && ipcClient != nil {
			ipcResult := convertToIPCResult(result)
			if result.ExitCode == 0 && result.Error == "" {
				_ = ipcClient.SendAgentDone(a.AgentID, "", ipcResult)
			} else {
				execErr := fmt.Errorf("%s", result.Error)
				if result.Error == "" {
					execErr = fmt.Errorf("agent exited with code %d", result.ExitCode)
				}
				_ = ipcClient.SendAgentFail(a.AgentID, "", execErr, ipcResult)
			}
		}

		// Cleanup overlay after execution
		if err := overlay.Cleanup(); err != nil {
			errors = append(errors, fmt.Errorf("failed to cleanup overlay for %s: %w", a.AgentID, err))
		}

		resumed++
	}

	return resumed, errors
}

