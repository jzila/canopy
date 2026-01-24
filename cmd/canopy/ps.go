package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jzila/canopy/pkg/daemon"
	"github.com/jzila/canopy/pkg/repository"
	"github.com/jzila/canopy/pkg/runtime"
)

var (
	psJSON        bool
	psDaemonOnly  bool
	psWorkersOnly bool
	psShowHistory bool
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "Show running canopy processes",
	Long: `Display running canopy processes including daemons, orchestrators, and workers.

Shows a summary of all canopy processes currently running on the system:
  - Daemons: The background HTTP/IPC server processes
  - Workers: Agent processes executing tasks

EXAMPLES
  # Show all running canopy processes
  canopy ps

  # Show only the daemon status
  canopy ps --daemon-only

  # Show only worker agents
  canopy ps --workers-only

  # Output as JSON for scripting
  canopy ps --json

  # Show state transition history for debugging
  canopy ps --show-history`,
	RunE: runPs,
}

func init() {
	psCmd.Flags().BoolVar(&psJSON, "json", false, "Output as JSON")
	psCmd.Flags().BoolVar(&psDaemonOnly, "daemon-only", false, "Show only daemon status")
	psCmd.Flags().BoolVar(&psWorkersOnly, "workers-only", false, "Show only worker agents")
	psCmd.Flags().BoolVar(&psShowHistory, "show-history", false, "Display state transition history for debugging")

	rootCmd.AddCommand(psCmd)
}

// ProcessInfo contains information about canopy processes
type ProcessInfo struct {
	Daemon     *DaemonInfo     `json:"daemon,omitempty"`
	Workers    []WorkerInfo    `json:"workers,omitempty"`
	MergeQueue *MergeQueueInfo `json:"merge_queue,omitempty"`
}

// DaemonInfo contains information about the daemon process
type DaemonInfo struct {
	Running           bool       `json:"running"`
	PID               int        `json:"pid,omitempty"`
	Port              int        `json:"port"`
	SocketPath        string     `json:"socketPath"`
	Uptime            string     `json:"uptime,omitempty"`
	StartTime         time.Time  `json:"startTime,omitempty"`
	IsPaused          bool       `json:"isPaused"`
	ActiveRepo        string     `json:"activeRepo,omitempty"`
	Stats             *StatsInfo `json:"stats,omitempty"`
	OrchestratorState string     `json:"orchestratorState,omitempty"`
	ActiveAgentCount  int        `json:"activeAgentCount,omitempty"`
}

// StatsInfo contains aggregate statistics from the daemon
type StatsInfo struct {
	TotalTasks     int     `json:"totalTasks"`
	RunningTasks   int     `json:"runningTasks"`
	CompletedTasks int     `json:"completedTasks"`
	FailedTasks    int     `json:"failedTasks"`
	TotalCostUSD   float64 `json:"totalCostUsd"`
}

// WorkerInfo contains information about a worker agent
type WorkerInfo struct {
	AgentID          string                    `json:"agentId"`
	TaskID           string                    `json:"taskId"`
	TaskTitle        string                    `json:"taskTitle,omitempty"`
	Status           string                    `json:"status"`
	MergeStatus      string                    `json:"mergeStatus,omitempty"`
	ValidationStatus string                    `json:"validationStatus,omitempty"`
	RepairAttempts   int                       `json:"repairAttempts,omitempty"`
	LifecycleState   string                    `json:"lifecycleState,omitempty"`
	LifecycleHistory []LifecycleHistoryEntry   `json:"lifecycleHistory,omitempty"`
	MergeQueuePos    int                       `json:"mergeQueuePos,omitempty"`
	Duration         string                    `json:"duration"`
	StartTime        string                    `json:"startTime,omitempty"`
}

// LifecycleHistoryEntry represents a single state transition
type LifecycleHistoryEntry struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Event     string `json:"event"`
	Timestamp string `json:"timestamp"`
}

// MergeQueueInfo contains the merge queue state
type MergeQueueInfo struct {
	Entries     []MergeQueueEntry `json:"entries"`
	QueueLength int               `json:"queue_length"`
	IsPaused    bool              `json:"is_paused"`
	PauseState  string            `json:"pause_state"`
}

// MergeQueueEntry represents an item in the merge queue
type MergeQueueEntry struct {
	AgentID   string `json:"agent_id"`
	TaskID    string `json:"task_id"`
	TaskTitle string `json:"task_title,omitempty"`
	Status    string `json:"status"` // merging, waiting
	Position  int    `json:"position"`
}

func runPs(cmd *cobra.Command, args []string) error {
	info := ProcessInfo{}

	// Get daemon info unless workers-only is specified
	if !psWorkersOnly {
		daemonInfo, err := getDaemonInfo()
		if err != nil {
			return fmt.Errorf("failed to get daemon info: %w", err)
		}
		info.Daemon = daemonInfo
	}

	// Get worker info unless daemon-only is specified
	if !psDaemonOnly {
		workers, mergeQueue, err := getWorkerInfo()
		if err != nil && info.Daemon != nil && info.Daemon.Running {
			// Only report error if daemon is running but we failed to get workers
			return fmt.Errorf("failed to get worker info: %w", err)
		}
		info.Workers = workers
		info.MergeQueue = mergeQueue
	}

	if psJSON {
		return outputPsJSON(info)
	}

	return outputPsTable(info)
}

func getDaemonInfo() (*DaemonInfo, error) {
	running, pid, err := daemon.IsRunning()
	if err != nil {
		return nil, fmt.Errorf("failed to check daemon status: %w", err)
	}

	info := &DaemonInfo{
		Running:    running,
		Port:       8080, // Default port
		SocketPath: runtime.SocketPath(""),
	}

	if !running {
		return info, nil
	}

	info.PID = pid

	// Query the daemon HTTP API for more details
	stateResp, err := queryDaemonState(info.Port)
	if err != nil {
		// Daemon is running but HTTP API not responding - still return basic info
		return info, nil
	}

	// Extract info from state response
	info.IsPaused = stateResp.IsPaused
	info.StartTime = stateResp.StartTime
	if !stateResp.StartTime.IsZero() {
		info.Uptime = formatDuration(time.Since(stateResp.StartTime))
	}

	// Get orchestrator state from response
	info.OrchestratorState = stateResp.OrchestratorState
	info.ActiveAgentCount = stateResp.ActiveAgentCount

	// Get active repo path from response (look up path from ID)
	if activeRepoID, ok := stateResp.Extra["active_repo_id"].(string); ok && activeRepoID != "" {
		if repo, err := repository.FromID(activeRepoID); err == nil && repo != nil {
			info.ActiveRepo = repo.Path
		} else {
			// Fallback to ID if lookup fails
			info.ActiveRepo = activeRepoID
		}
	}

	// Extract stats
	info.Stats = &StatsInfo{
		TotalTasks:     stateResp.Stats.TotalTasks,
		RunningTasks:   stateResp.Stats.RunningTasks,
		CompletedTasks: stateResp.Stats.CompletedTasks,
		FailedTasks:    stateResp.Stats.FailedTasks,
		TotalCostUSD:   stateResp.Stats.TotalCostUSD,
	}

	return info, nil
}

// StateResponse matches the daemon's state response structure
type StateResponse struct {
	Agents            map[string]AgentInfo   `json:"agents"`
	Tasks             map[string]TaskInfo    `json:"tasks"`
	Stats             StatsResponse          `json:"stats"`
	IsPaused          bool                   `json:"is_paused"`
	StartTime         time.Time              `json:"start_time"`
	OrchestratorState string                 `json:"orchestrator_state,omitempty"`
	ActiveAgentCount  int                    `json:"active_agent_count,omitempty"`
	MergeQueue        *MergeQueueInfo        `json:"merge_queue,omitempty"`
	Extra             map[string]interface{} `json:"-"` // For capturing additional fields like active_repo_id
}

// AgentInfo matches the daemon's agent state structure
type AgentInfo struct {
	ID               string                         `json:"id"`
	TaskID           string                         `json:"task_id"`
	TaskTitle        string                         `json:"task_title"`
	Status           string                         `json:"status"`
	MergeStatus      string                         `json:"merge_status"`
	ValidationStatus string                         `json:"validation_status"`
	RepairAttempts   int                            `json:"repair_attempts"`
	LifecycleState   string                         `json:"lifecycle_state"`
	LifecycleHistory []AgentLifecycleHistoryEntry   `json:"lifecycle_history"`
	MergeQueuePos    int                            `json:"merge_queue_pos"`
	StartTime        time.Time                      `json:"start_time"`
	Duration         float64                        `json:"duration"`
	Archived         bool                           `json:"archived"`
}

// AgentLifecycleHistoryEntry matches the daemon's lifecycle history structure
type AgentLifecycleHistoryEntry struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Event     string `json:"event"`
	Timestamp string `json:"timestamp"`
}

// TaskInfo matches the daemon's task state structure
type TaskInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	AgentID  string `json:"agent_id"`
	Archived bool   `json:"archived"`
}

// StatsResponse matches the daemon's stats structure
type StatsResponse struct {
	TotalTasks     int     `json:"total_tasks"`
	CompletedTasks int     `json:"completed_tasks"`
	FailedTasks    int     `json:"failed_tasks"`
	RunningTasks   int     `json:"running_tasks"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
}

func queryDaemonState(port int) (*StateResponse, error) {
	url := fmt.Sprintf("http://localhost:%d/api/state", port)

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var state StateResponse
	if err := json.Unmarshal(body, &state); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Parse extra fields (like active_repo_id) that aren't in the struct
	var extra map[string]interface{}
	if err := json.Unmarshal(body, &extra); err == nil {
		state.Extra = extra
	}

	return &state, nil
}

func getWorkerInfo() ([]WorkerInfo, *MergeQueueInfo, error) {
	// Check if daemon is running
	running, _, err := daemon.IsRunning()
	if err != nil || !running {
		return nil, nil, nil
	}

	// Query daemon for agent info
	state, err := queryDaemonState(8080)
	if err != nil {
		return nil, nil, err
	}

	var workers []WorkerInfo
	for _, agent := range state.Agents {
		// Skip archived agents
		if agent.Archived {
			continue
		}

		// Only show running agents by default
		if agent.Status != "running" && agent.Status != "starting" {
			continue
		}

		duration := ""
		if !agent.StartTime.IsZero() {
			duration = formatDuration(time.Since(agent.StartTime))
		} else if agent.Duration > 0 {
			duration = formatDuration(time.Duration(agent.Duration * float64(time.Second)))
		}

		// Convert lifecycle history entries
		var history []LifecycleHistoryEntry
		for _, h := range agent.LifecycleHistory {
			history = append(history, LifecycleHistoryEntry(h))
		}

		workers = append(workers, WorkerInfo{
			AgentID:          agent.ID,
			TaskID:           agent.TaskID,
			TaskTitle:        agent.TaskTitle,
			Status:           agent.Status,
			MergeStatus:      agent.MergeStatus,
			ValidationStatus: agent.ValidationStatus,
			RepairAttempts:   agent.RepairAttempts,
			LifecycleState:   agent.LifecycleState,
			LifecycleHistory: history,
			MergeQueuePos:    agent.MergeQueuePos,
			Duration:         duration,
			StartTime:        agent.StartTime.Format("15:04:05"),
		})
	}

	return workers, state.MergeQueue, nil
}

func outputPsJSON(info ProcessInfo) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(info)
}

func outputPsTable(info ProcessInfo) error {
	// Show daemon section unless workers-only
	if !psWorkersOnly && info.Daemon != nil {
		fmt.Println("DAEMON")
		if info.Daemon.Running {
			fmt.Printf("  Status: Running (PID %d)\n", info.Daemon.PID)
			fmt.Printf("  Port: %d\n", info.Daemon.Port)
			fmt.Printf("  Socket: %s\n", info.Daemon.SocketPath)
			if info.Daemon.Uptime != "" {
				fmt.Printf("  Uptime: %s\n", info.Daemon.Uptime)
			}
			// Display orchestrator state
			if info.Daemon.OrchestratorState != "" {
				orchState := info.Daemon.OrchestratorState
				if orchState == "active" && info.Daemon.ActiveAgentCount > 0 {
					fmt.Printf("  Orchestrator: %s (%d agents)\n", orchState, info.Daemon.ActiveAgentCount)
				} else {
					fmt.Printf("  Orchestrator: %s\n", orchState)
				}
			}
			if info.Daemon.IsPaused {
				fmt.Printf("  Paused: yes\n")
			}
			if info.Daemon.ActiveRepo != "" {
				fmt.Printf("  Active Repo: %s\n", info.Daemon.ActiveRepo)
			}
			if info.Daemon.Stats != nil {
				stats := info.Daemon.Stats
				fmt.Printf("  Tasks: %d total (%d running, %d completed, %d failed)\n",
					stats.TotalTasks, stats.RunningTasks, stats.CompletedTasks, stats.FailedTasks)
				if stats.TotalCostUSD > 0 {
					fmt.Printf("  Cost: %s\n", formatCost(stats.TotalCostUSD))
				}
			}
		} else {
			fmt.Println("  Status: Not running")
		}
		fmt.Println()
	}

	// Show workers section unless daemon-only
	if !psDaemonOnly {
		fmt.Println("WORKERS")
		if len(info.Workers) == 0 {
			fmt.Println("  No active workers")
		} else {
			// Print header - use full agent IDs since they're needed for canopy kill
			fmt.Printf("  %-30s  %-16s  %-18s  %-10s  %s\n",
				"AGENT ID", "TASK ID", "STATUS", "DURATION", "TITLE")

			for _, w := range info.Workers {
				// Show full agent ID (needed for canopy kill)
				// Truncate task ID for display
				taskID := truncateID(w.TaskID, 16)
				title := w.TaskTitle
				if len(title) > 40 {
					title = title[:37] + "..."
				}

				// Use lifecycle state directly if available
				status := formatLifecycleStatus(w)

				fmt.Printf("  %-30s  %-16s  %-18s  %-10s  %s\n",
					w.AgentID, taskID, status, w.Duration, title)

				// Show state transition history if requested
				if psShowHistory && len(w.LifecycleHistory) > 0 {
					fmt.Println("    State History:")
					for _, h := range w.LifecycleHistory {
						// Parse and format timestamp for display
						ts := h.Timestamp
						if t, err := time.Parse(time.RFC3339, h.Timestamp); err == nil {
							ts = t.Format("15:04:05")
						}
						fmt.Printf("      %s  %s -> %s  (%s)\n", ts, h.From, h.To, h.Event)
					}
				}
			}
		}
		fmt.Println()
	}

	// Show merge queue section unless daemon-only
	if !psDaemonOnly {
		outputMergeQueue(info.MergeQueue)
	}

	return nil
}

// outputMergeQueue displays the merge queue state
func outputMergeQueue(mq *MergeQueueInfo) {
	if mq == nil || len(mq.Entries) == 0 {
		fmt.Println("Merge Queue: empty")
		fmt.Println()
		return
	}

	fmt.Println("Merge Queue:")
	for _, entry := range mq.Entries {
		// Format: Position N: agent-id (task-id) - status
		fmt.Printf("  Position %d: %s (%s) - %s\n",
			entry.Position,
			truncateID(entry.AgentID, 20),
			truncateID(entry.TaskID, 16),
			entry.Status,
		)
	}
	fmt.Println()
}

// formatLifecycleStatus returns a display string for the agent's status.
// Prefers lifecycle_state when available, with additional context for specific states.
func formatLifecycleStatus(w WorkerInfo) string {
	// Use lifecycle state if available
	if w.LifecycleState != "" {
		switch w.LifecycleState {
		case "queued_for_merge":
			if w.MergeQueuePos > 0 {
				return fmt.Sprintf("queued (pos %d)", w.MergeQueuePos)
			}
			return "queued"
		case "validating":
			return "validating"
		case "repairing":
			if w.RepairAttempts > 0 {
				return fmt.Sprintf("repairing (%d/3)", w.RepairAttempts)
			}
			return "repairing"
		default:
			return w.LifecycleState
		}
	}

	// Fallback to legacy status derivation
	status := w.Status
	if w.ValidationStatus == "repairing" {
		if w.RepairAttempts > 0 {
			status = fmt.Sprintf("repairing (%d)", w.RepairAttempts)
		} else {
			status = "repairing"
		}
	} else if w.MergeStatus != "" && w.MergeStatus != "none" {
		status = w.MergeStatus
	}
	return status
}

// truncateID truncates an ID to the specified length, showing the first part
func truncateID(id string, maxLen int) string {
	if len(id) <= maxLen {
		return id
	}
	// For IDs like "agt-abc123def", keep prefix and truncate
	if idx := strings.Index(id, "-"); idx > 0 && idx < maxLen-3 {
		remaining := maxLen - idx - 1
		if remaining > 0 && idx+1+remaining < len(id) {
			return id[:idx+1+remaining]
		}
	}
	return id[:maxLen]
}
