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
	historySince  string
	historyStatus string
	historyJSON   bool
	historyLimit  int
)

var historyCmd = &cobra.Command{
	Use:   "history [run-id]",
	Short: "Show run history and details",
	Long: `Display canopy run history and detailed information about specific runs.

Without arguments, shows a list of recent runs with summary information.
With a run ID, shows detailed information about that specific run.

EXAMPLES
  # Show recent runs (default: last 20)
  canopy history

  # Show runs from the last 7 days
  canopy history --since=7d

  # Show only failed runs
  canopy history --status=failed

  # Show detailed info for a specific run
  canopy history run-abc123

  # Use a short ID prefix (if unambiguous)
  canopy history abc123

  # Output as JSON for scripting
  canopy history --json`,
	RunE: runHistory,
}

func init() {
	historyCmd.Flags().StringVar(&historySince, "since", "", "Show runs since duration (e.g., 7d, 24h, 1w)")
	historyCmd.Flags().StringVar(&historyStatus, "status", "", "Filter by status (completed, failed, partial)")
	historyCmd.Flags().BoolVar(&historyJSON, "json", false, "Output as JSON")
	historyCmd.Flags().IntVar(&historyLimit, "limit", 20, "Maximum number of runs to show")

	rootCmd.AddCommand(historyCmd)
}

func runHistory(cmd *cobra.Command, args []string) error {
	store, err := history.NewStore()
	if err != nil {
		return fmt.Errorf("failed to open history store: %w", err)
	}

	// If a run ID is provided, show details for that run
	if len(args) > 0 {
		return showRunDetails(store, args[0])
	}

	// Otherwise, list runs
	return listRuns(store)
}

func listRuns(store *history.Store) error {
	opts := history.ListOptions{
		Limit: historyLimit,
	}

	// Parse --since duration
	if historySince != "" {
		duration, err := parseDuration(historySince)
		if err != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}
		opts.Since = time.Now().Add(-duration)
	}

	// Parse --status filter
	if historyStatus != "" {
		switch strings.ToLower(historyStatus) {
		case "completed":
			opts.Status = history.RunStatusCompleted
		case "failed":
			opts.Status = history.RunStatusFailed
		case "partial":
			opts.Status = history.RunStatusPartial
		default:
			return fmt.Errorf("invalid status: %s (use: completed, failed, partial)", historyStatus)
		}
	}

	runs, err := store.List(opts)
	if err != nil {
		return fmt.Errorf("failed to list runs: %w", err)
	}

	if len(runs) == 0 {
		fmt.Println("No runs found.")
		return nil
	}

	if historyJSON {
		return outputJSON(runs)
	}

	return outputTable(runs)
}

func showRunDetails(store *history.Store, runID string) error {
	// Try exact match first, then prefix match
	run, err := store.Get(runID)
	if err != nil {
		// Try prefix match
		run, err = store.FindByPrefix(runID)
		if err != nil {
			return err
		}
	}

	if historyJSON {
		return outputJSON(run)
	}

	return outputRunDetails(run)
}

func outputTable(runs []*history.RunRecord) error {
	// Print header
	fmt.Printf("%-12s  %-20s  %-10s  %-7s  %-10s  %s\n",
		"ID", "STARTED", "STATUS", "TASKS", "DURATION", "COST")

	for _, run := range runs {
		// Format short ID (first 8 chars after "run-" prefix)
		shortID := run.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}

		// Format timestamp
		started := run.StartTime.Format("2006-01-02 15:04:05")

		// Format status with color hints
		status := string(run.Status)

		// Format tasks as completed/total
		tasks := fmt.Sprintf("%d/%d", run.CompletedTasks, run.TotalTasks)

		// Format duration
		duration := formatDuration(time.Duration(run.DurationSeconds * float64(time.Second)))

		// Format cost
		cost := formatCost(run.TotalCostUSD)

		fmt.Printf("%-12s  %-20s  %-10s  %-7s  %-10s  %s\n",
			shortID, started, status, tasks, duration, cost)
	}

	return nil
}

func outputRunDetails(run *history.RunRecord) error {
	fmt.Printf("Run: %s\n", run.ID)
	fmt.Printf("Status: %s\n", run.Status)
	fmt.Printf("Started: %s\n", run.StartTime.Format(time.RFC3339))
	fmt.Printf("Ended: %s\n", run.EndTime.Format(time.RFC3339))
	fmt.Printf("Duration: %s\n", formatDuration(time.Duration(run.DurationSeconds*float64(time.Second))))
	fmt.Printf("Working Directory: %s\n", run.WorkDir)
	fmt.Println()

	fmt.Println("Tasks:")
	fmt.Printf("  Total: %d\n", run.TotalTasks)
	fmt.Printf("  Completed: %d\n", run.CompletedTasks)
	fmt.Printf("  Failed: %d\n", run.FailedTasks)
	fmt.Println()

	fmt.Println("Tokens:")
	fmt.Printf("  Input: %s\n", formatTokens(run.TotalInputTokens))
	fmt.Printf("  Output: %s\n", formatTokens(run.TotalOutputTokens))
	fmt.Printf("  Cache Read: %s\n", formatTokens(run.TotalCacheReadTokens))
	fmt.Printf("  Cache Creation: %s\n", formatTokens(run.TotalCacheCreationTokens))
	fmt.Println()

	fmt.Printf("Cost: %s\n", formatCost(run.TotalCostUSD))
	fmt.Printf("Files Changed: %d\n", run.FilesChanged)
	fmt.Printf("Git Commits: %d\n", run.GitCommits)

	// Show individual tasks if available
	if len(run.Tasks) > 0 {
		fmt.Println()
		fmt.Println("Task Details:")
		fmt.Printf("%-20s  %-10s  %-10s  %-10s  %s\n",
			"ID", "STATUS", "DURATION", "COST", "TITLE")

		for _, task := range run.Tasks {
			taskID := task.ID
			if len(taskID) > 18 {
				taskID = taskID[:18]
			}

			status := task.Status
			duration := formatDuration(time.Duration(task.DurationMS) * time.Millisecond)
			cost := formatCost(task.CostUSD)

			title := task.Title
			if len(title) > 40 {
				title = title[:37] + "..."
			}

			fmt.Printf("%-20s  %-10s  %-10s  %-10s  %s\n",
				taskID, status, duration, cost, title)
		}
	}

	return nil
}

func outputJSON(v interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

// parseDuration parses a duration string with support for days and weeks
func parseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))

	// Handle special suffixes not supported by time.ParseDuration
	if strings.HasSuffix(s, "d") {
		s = strings.TrimSuffix(s, "d")
		var days float64
		if _, err := fmt.Sscanf(s, "%f", &days); err != nil {
			return 0, fmt.Errorf("invalid days: %s", s)
		}
		return time.Duration(days * 24 * float64(time.Hour)), nil
	}

	if strings.HasSuffix(s, "w") {
		s = strings.TrimSuffix(s, "w")
		var weeks float64
		if _, err := fmt.Sscanf(s, "%f", &weeks); err != nil {
			return 0, fmt.Errorf("invalid weeks: %s", s)
		}
		return time.Duration(weeks * 7 * 24 * float64(time.Hour)), nil
	}

	// Fall back to standard duration parsing
	return time.ParseDuration(s)
}

// formatDuration formats a duration as a human-readable string
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, mins)
}

// formatCost formats a cost in USD
func formatCost(cost float64) string {
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

// formatTokens formats token counts with K/M suffixes
func formatTokens(tokens int) string {
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	if tokens < 1000000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(tokens)/1000000)
}
