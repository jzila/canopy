package canopy

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	verbose bool
	workdir string
)

var rootCmd = &cobra.Command{
	Use:   "canopy",
	Short: "Coding agent orchestrator with parallel execution",
	Long: `Canopy orchestrates multiple Claude Code agents in parallel,
each running in isolated OverlayFS sandboxes. Tasks are tracked
via beads (bd) and executed based on dependency order.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().StringVarP(&workdir, "workdir", "w", ".", "Working directory for agents")
}
