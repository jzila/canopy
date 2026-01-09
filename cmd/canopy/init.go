package canopy

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize beads in the current directory",
	Long: `Initializes beads (bd init) in the current directory if not already initialized.
This creates the .beads directory for task tracking.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	// Check if beads is already initialized
	if _, err := os.Stat(".beads"); err == nil {
		fmt.Println("Beads already initialized in this directory")
		return nil
	}

	// Run bd init
	bdCmd := exec.Command("bd", "init")
	bdCmd.Stdout = os.Stdout
	bdCmd.Stderr = os.Stderr

	if err := bdCmd.Run(); err != nil {
		return fmt.Errorf("bd init failed: %w", err)
	}

	fmt.Println("Beads initialized. Create tasks with: bd create \"Task title\" -p 0")
	return nil
}
