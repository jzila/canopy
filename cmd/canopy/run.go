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
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute ready tasks from beads",
	Long: `Executes all ready (unblocked) tasks from beads in parallel.

Each task runs in an isolated OverlayFS sandbox with its own copy
of the working directory. Changes are merged back after completion.

Example:
  # Run with default settings
  canopy run

  # Run with 8 concurrent agents
  canopy run --concurrency 8

  # Dry run to see what would execute
  canopy run --dry-run`,
	RunE: runOrchestrator,
}

func init() {
	runCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 4, "Maximum concurrent agents")
	runCmd.Flags().StringVarP(&outputDir, "output", "o", "", "Output directory for merged results (default: workdir)")
	runCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show execution plan without running")

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
	})
	if err != nil {
		return fmt.Errorf("failed to create orchestrator: %w", err)
	}

	return orch.Run(ctx)
}
