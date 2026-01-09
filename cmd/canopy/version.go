package canopy

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("canopy %s (commit: %s, built: %s)\n", Version, Commit, BuildDate)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
