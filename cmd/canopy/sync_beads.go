package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/jzila/canopy/pkg/daemon"
)

var (
	syncBeadsJSON  bool
	syncBeadsQuiet bool
)

var syncBeadsCmd = &cobra.Command{
	Use:   "sync-beads [repo-path]",
	Short: "Sync beads tasks with the daemon",
	Long: `Reload tasks from the beads database into the daemon's runtime state.

This command triggers the daemon to re-read tasks from the beads (.beads/)
database and update its internal state. Useful after manually modifying
beads files or to ensure the daemon has the latest task information.

EXAMPLES
  # Sync beads for the current directory
  canopy sync-beads

  # Sync beads for a specific repository
  canopy sync-beads /path/to/repo

  # Output as JSON for scripting
  canopy sync-beads --json

  # Quiet mode - only output errors
  canopy sync-beads --quiet`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSyncBeads,
}

func init() {
	syncBeadsCmd.Flags().BoolVar(&syncBeadsJSON, "json", false, "Output as JSON")
	syncBeadsCmd.Flags().BoolVar(&syncBeadsQuiet, "quiet", false, "Only output errors")

	rootCmd.AddCommand(syncBeadsCmd)
}

// SyncBeadsResult represents the result of a sync operation
type SyncBeadsResult struct {
	Success  bool   `json:"success"`
	Synced   int    `json:"synced"`
	RepoID   string `json:"repo_id,omitempty"`
	RepoName string `json:"repo_name,omitempty"`
	Error    string `json:"error,omitempty"`
}

func runSyncBeads(cmd *cobra.Command, args []string) error {
	// Check if daemon is running
	running, _, err := daemon.IsRunning()
	if err != nil {
		return fmt.Errorf("failed to check daemon status: %w", err)
	}
	if !running {
		result := SyncBeadsResult{
			Success: false,
			Error:   "daemon is not running",
		}
		return outputSyncBeadsResult(result)
	}

	// Build the API URL
	apiURL := "http://localhost:8080/api/beads/sync"
	if len(args) > 0 {
		params := url.Values{}
		params.Set("repo", args[0])
		apiURL = apiURL + "?" + params.Encode()
	}

	// Make the POST request
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Post(apiURL, "application/json", nil)
	if err != nil {
		result := SyncBeadsResult{
			Success: false,
			Error:   fmt.Sprintf("failed to connect to daemon: %v", err),
		}
		return outputSyncBeadsResult(result)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result := SyncBeadsResult{
			Success: false,
			Error:   fmt.Sprintf("failed to read response: %v", err),
		}
		return outputSyncBeadsResult(result)
	}

	if resp.StatusCode != http.StatusOK {
		result := SyncBeadsResult{
			Success: false,
			Error:   fmt.Sprintf("daemon returned error: %s", string(body)),
		}
		return outputSyncBeadsResult(result)
	}

	// Parse response
	var syncResp struct {
		Synced   int    `json:"synced"`
		RepoID   string `json:"repo_id"`
		RepoName string `json:"repo_name"`
	}
	if err := json.Unmarshal(body, &syncResp); err != nil {
		result := SyncBeadsResult{
			Success: false,
			Error:   fmt.Sprintf("failed to parse response: %v", err),
		}
		return outputSyncBeadsResult(result)
	}

	result := SyncBeadsResult{
		Success:  true,
		Synced:   syncResp.Synced,
		RepoID:   syncResp.RepoID,
		RepoName: syncResp.RepoName,
	}

	return outputSyncBeadsResult(result)
}

func outputSyncBeadsResult(result SyncBeadsResult) error {
	if syncBeadsJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return fmt.Errorf("failed to encode JSON: %w", err)
		}
		if !result.Success {
			os.Exit(1)
		}
		return nil
	}

	// Non-JSON output
	if !result.Success {
		if !syncBeadsQuiet {
			fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
		}
		os.Exit(1)
	}

	if !syncBeadsQuiet {
		repoName := result.RepoName
		if repoName == "" {
			repoName = result.RepoID
		}
		fmt.Printf("Synced %d tasks from %s\n", result.Synced, repoName)
	}

	return nil
}
