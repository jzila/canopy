package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jzila/canopy/pkg/daemon"
)

var (
	killForce bool
)

var killCmd = &cobra.Command{
	Use:   "kill <agent-id>",
	Short: "Kill a running worker agent",
	Long: `Terminate a running worker agent by its agent ID or task ID.

The agent ID can be found using 'canopy ps' which shows all running workers.
You can use either the full agent ID (e.g., agent-abc12345-beads-xyz) or
just the task ID portion (e.g., beads-xyz).

EXAMPLES
  # Kill an agent by its full agent ID
  canopy kill agent-abc12345-beads-xyz

  # Kill an agent by task ID (will match the running agent for that task)
  canopy kill beads-xyz

  # Force kill without confirmation
  canopy kill --force agent-abc12345-beads-xyz`,
	Args: cobra.ExactArgs(1),
	RunE: runKill,
}

func init() {
	killCmd.Flags().BoolVarP(&killForce, "force", "f", false, "Kill without confirmation")

	rootCmd.AddCommand(killCmd)
}

func runKill(cmd *cobra.Command, args []string) error {
	targetID := args[0]

	// Check if daemon is running
	running, _, err := daemon.IsRunning()
	if err != nil {
		return fmt.Errorf("failed to check daemon status: %w", err)
	}
	if !running {
		return fmt.Errorf("daemon is not running (start with 'canopy daemon')")
	}

	// Get current state to find the agent
	state, err := queryDaemonState(8080)
	if err != nil {
		return fmt.Errorf("failed to query daemon: %w", err)
	}

	// Find the agent by ID or task ID
	var agentID string
	var agentInfo *AgentInfo

	for _, agent := range state.Agents {
		if agent.ID == targetID {
			agentID = agent.ID
			agentInfo = &agent
			break
		}
		if agent.TaskID == targetID {
			agentID = agent.ID
			agentInfo = &agent
			break
		}
	}

	if agentID == "" {
		// List available agents if not found
		var activeAgents []string
		for _, agent := range state.Agents {
			if agent.Status == "running" || agent.Status == "starting" {
				activeAgents = append(activeAgents, fmt.Sprintf("  %s (task: %s)", agent.ID, agent.TaskID))
			}
		}
		if len(activeAgents) > 0 {
			return fmt.Errorf("agent '%s' not found. Active agents:\n%s", targetID, strings.Join(activeAgents, "\n"))
		}
		return fmt.Errorf("agent '%s' not found and no active agents", targetID)
	}

	// Check if agent is actually running
	if agentInfo.Status != "running" && agentInfo.Status != "starting" {
		return fmt.Errorf("agent %s is not running (status: %s)", agentID, agentInfo.Status)
	}

	// Confirm unless --force
	if !killForce {
		taskDesc := agentInfo.TaskID
		if agentInfo.TaskTitle != "" {
			taskDesc = agentInfo.TaskTitle
		}
		fmt.Printf("Kill agent %s?\n", agentID)
		fmt.Printf("  Task: %s\n", taskDesc)
		fmt.Printf("  Status: %s\n", agentInfo.Status)
		if !agentInfo.StartTime.IsZero() {
			fmt.Printf("  Running for: %s\n", formatDurationKill(time.Since(agentInfo.StartTime)))
		}
		fmt.Print("\nConfirm [y/N]: ")

		var response string
		_, _ = fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "y" && response != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Send kill request to daemon
	if err := killAgent(agentID); err != nil {
		return err
	}

	fmt.Printf("Killed agent %s\n", agentID)
	return nil
}

func killAgent(agentID string) error {
	url := fmt.Sprintf("http://localhost:8080/api/agents/%s/kill", agentID)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send kill request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("agent %s not found (may have already completed)", agentID)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("kill failed: %s", errResp.Error)
		}
		return fmt.Errorf("kill failed with status %d", resp.StatusCode)
	}

	return nil
}

// formatDurationKill formats a duration for display
func formatDurationKill(d time.Duration) string {
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
