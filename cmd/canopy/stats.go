package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jzila/canopy/pkg/history"
)

var (
	statsSince string
	statsJSON  bool
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show aggregate statistics across runs",
	Long: `Display aggregate statistics across all canopy runs.

Shows cumulative metrics including run counts, task completion rates,
token usage, costs, and other execution metrics.

EXAMPLES
  # Show all-time statistics
  canopy stats

  # Show statistics for the last 7 days
  canopy stats --since=7d

  # Show statistics for the last 30 days
  canopy stats --since=30d

  # Output as JSON for scripting
  canopy stats --json`,
	RunE: runStats,
}

func init() {
	statsCmd.Flags().StringVar(&statsSince, "since", "", "Calculate stats since duration (e.g., 7d, 24h, 1w)")
	statsCmd.Flags().BoolVar(&statsJSON, "json", false, "Output as JSON")

	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, args []string) error {
	store, err := history.NewStore()
	if err != nil {
		return fmt.Errorf("failed to open history store: %w", err)
	}

	opts := history.ListOptions{}

	// Parse --since duration
	if statsSince != "" {
		duration, err := parseDuration(statsSince)
		if err != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}
		opts.Since = time.Now().Add(-duration)
	}

	stats, err := store.Stats(opts)
	if err != nil {
		return fmt.Errorf("failed to calculate stats: %w", err)
	}

	if stats.TotalRuns == 0 {
		if statsSince != "" {
			fmt.Printf("No runs found in the last %s.\n", statsSince)
		} else {
			fmt.Println("No runs found.")
		}
		return nil
	}

	if statsJSON {
		return outputStatsJSON(stats)
	}

	return outputStatsTable(stats, statsSince)
}

func outputStatsJSON(stats *history.AggregateStats) error {
	// Create a JSON-friendly version with duration as seconds
	jsonStats := struct {
		TotalRuns                int     `json:"total_runs"`
		CompletedRuns            int     `json:"completed_runs"`
		FailedRuns               int     `json:"failed_runs"`
		PartialRuns              int     `json:"partial_runs"`
		TotalTasks               int     `json:"total_tasks"`
		CompletedTasks           int     `json:"completed_tasks"`
		FailedTasks              int     `json:"failed_tasks"`
		TotalInputTokens         int     `json:"total_input_tokens"`
		TotalOutputTokens        int     `json:"total_output_tokens"`
		TotalCacheCreationTokens int     `json:"total_cache_creation_tokens"`
		TotalCacheReadTokens     int     `json:"total_cache_read_tokens"`
		TotalCostUSD             float64 `json:"total_cost_usd"`
		TotalDurationSeconds     float64 `json:"total_duration_seconds"`
		FilesChanged             int     `json:"files_changed"`
		GitCommits               int     `json:"git_commits"`
	}{
		TotalRuns:                stats.TotalRuns,
		CompletedRuns:            stats.CompletedRuns,
		FailedRuns:               stats.FailedRuns,
		PartialRuns:              stats.PartialRuns,
		TotalTasks:               stats.TotalTasks,
		CompletedTasks:           stats.CompletedTasks,
		FailedTasks:              stats.FailedTasks,
		TotalInputTokens:         stats.TotalInputTokens,
		TotalOutputTokens:        stats.TotalOutputTokens,
		TotalCacheCreationTokens: stats.TotalCacheCreationTokens,
		TotalCacheReadTokens:     stats.TotalCacheReadTokens,
		TotalCostUSD:             stats.TotalCostUSD,
		TotalDurationSeconds:     stats.TotalDuration.Seconds(),
		FilesChanged:             stats.FilesChanged,
		GitCommits:               stats.GitCommits,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(jsonStats)
}

func outputStatsTable(stats *history.AggregateStats, since string) error {
	// Build period description
	period := "All time"
	if since != "" {
		period = fmt.Sprintf("Last %s", since)
	}
	fmt.Printf("Statistics (%s)\n", period)
	fmt.Println(strings.Repeat("=", 40))
	fmt.Println()

	// Runs section
	runDesc := fmt.Sprintf("%d completed", stats.CompletedRuns)
	if stats.FailedRuns > 0 {
		runDesc += fmt.Sprintf(", %d failed", stats.FailedRuns)
	}
	if stats.PartialRuns > 0 {
		runDesc += fmt.Sprintf(", %d partial", stats.PartialRuns)
	}
	fmt.Printf("Runs: %d (%s)\n", stats.TotalRuns, runDesc)

	// Tasks section
	taskDesc := fmt.Sprintf("%d completed", stats.CompletedTasks)
	if stats.FailedTasks > 0 {
		taskDesc += fmt.Sprintf(", %d failed", stats.FailedTasks)
	}
	fmt.Printf("Tasks: %d (%s)\n", stats.TotalTasks, taskDesc)

	// Tokens section
	fmt.Printf("Tokens: %s input, %s output",
		formatTokens(stats.TotalInputTokens),
		formatTokens(stats.TotalOutputTokens))
	if stats.TotalCacheReadTokens > 0 {
		fmt.Printf(" (%s cache)", formatTokens(stats.TotalCacheReadTokens))
	}
	fmt.Println()

	// Cost section
	fmt.Printf("Cost: %s\n", formatCost(stats.TotalCostUSD))

	// Duration section
	fmt.Printf("Total Duration: %s\n", formatDuration(stats.TotalDuration))

	// Files and commits section
	if stats.FilesChanged > 0 || stats.GitCommits > 0 {
		fmt.Printf("Files Changed: %d\n", stats.FilesChanged)
		fmt.Printf("Git Commits: %d\n", stats.GitCommits)
	}

	return nil
}
