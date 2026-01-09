package canopy

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/john/canopy/pkg/orchestrator"
)

var (
	concurrency int
	outputDir   string
	dryRun      bool
	sandbox     bool
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

	return orch.Run(ctx)
}
