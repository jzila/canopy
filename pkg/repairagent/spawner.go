// Package repairagent provides repair agent functionality for fixing validation failures.
package repairagent

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/sandbox"
)

// Config holds repair agent configuration.
type Config struct {
	WorkDir       string                 // Base working directory (the actual repo, not overlay)
	TempDir       string                 // Temp directory for overlays (if needed)
	Verbose       bool                   // Verbose logging
	UseBwrap      bool                   // Use bubblewrap sandbox
	SandboxConfig *sandbox.SandboxConfig // Sandbox configuration
	RepoID        string                 // Repository ID for IPC tracking
	RunID         string                 // Run ID for unique agent ID generation
}

// Result holds the outcome of a repair agent execution.
type Result struct {
	// Success indicates whether the repair agent successfully fixed the issue.
	Success bool
	// Error contains an error message if repair failed.
	Error string
	// AgentResult contains the full agent execution result.
	AgentResult *agent.Result
	// Duration is how long the repair agent took.
	Duration time.Duration
	// RepairAgentID is the unique ID assigned to the repair agent.
	RepairAgentID string
}

// RepairAgent manages repair agents for fixing validation failures.
type RepairAgent struct {
	config    *Config
	executor  *agent.Executor
	ipcClient *ipc.Client
}

// New creates a new RepairAgent.
func New(config *Config) *RepairAgent {
	executor := agent.NewExecutor(&agent.Config{
		Verbose:       config.Verbose,
		UseBwrap:      config.UseBwrap,
		SandboxConfig: config.SandboxConfig,
	})

	return &RepairAgent{
		config:   config,
		executor: executor,
	}
}

// SetIPCClient sets the IPC client for sending repair agent events to the daemon.
// This enables parent-child agent tracking in the UI.
func (r *RepairAgent) SetIPCClient(client *ipc.Client) {
	r.ipcClient = client
}

// SetRepoID sets the repository ID for IPC tracking.
func (r *RepairAgent) SetRepoID(repoID string) {
	r.config.RepoID = repoID
}

// SetRunID sets the run ID for unique agent ID generation.
func (r *RepairAgent) SetRunID(runID string) {
	r.config.RunID = runID
}

// makeAgentID creates a unique repair agent ID by combining run ID prefix with task ID.
// Format: agent-{runID[:8]}-{taskID}-repair-{attemptNum}
func (r *RepairAgent) makeAgentID(taskID string, attemptNum int) string {
	prefix := r.config.RunID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	if prefix == "" {
		// Fallback for when runID is not set (shouldn't happen in normal flow)
		return fmt.Sprintf("%s-repair-%d", taskID, attemptNum)
	}
	return fmt.Sprintf("agent-%s-%s-repair-%d", prefix, taskID, attemptNum)
}

// Repair spawns a repair agent to fix validation failures.
// Unlike the resolver which runs in an overlay, the repair agent runs directly
// on the working directory since the merge has already been applied.
//
// The repair agent:
//   - Runs directly on the working directory (already merged)
//   - Receives validation failure context
//   - Has access to the merged diff
//   - Should fix the issue and commit directly
//   - Is flagged as a child of the implementor agent via ParentAgentID
func (r *RepairAgent) Repair(ctx context.Context, repairCtx *RepairContext, parentAgentID string) (*Result, error) {
	start := time.Now()

	result := &Result{}

	// Generate unique repair agent ID
	repairAgentID := r.makeAgentID(repairCtx.TaskID, repairCtx.RepairAttempt)
	result.RepairAgentID = repairAgentID

	// Write repair context files to the working directory
	if err := WriteRepairContext(r.config.WorkDir, repairCtx); err != nil {
		result.Error = fmt.Sprintf("failed to write repair context: %v", err)
		result.Duration = time.Since(start)
		return result, nil
	}

	// Build the repair prompt
	prompt := BuildRepairPrompt(repairCtx)

	// Create a synthetic beads task for the repair agent
	repairTask := &beads.Task{
		ID:          repairAgentID,
		Title:       fmt.Sprintf("Repair validation failure for %s", repairCtx.TaskID),
		Description: prompt,
	}

	// Send IPC event for repair agent start (child of original agent)
	if r.ipcClient != nil {
		if err := r.ipcClient.SendAgentStart(
			repairAgentID,
			r.config.RunID,            // Run ID for historical filtering
			repairCtx.TaskID,          // TaskID is the original task
			repairTask.Title,
			repairCtx.TaskTitle,       // Task description from original task
			parentAgentID,             // Parent is the implementor agent
			r.config.RepoID,           // Repository ID for tracking
		); err != nil && r.config.Verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to send repair start event: %v\n", err)
		}
	}

	// Create a direct overlay that wraps the working directory without isolation.
	// This allows the repair agent to commit directly to the repo.
	// Unlike the resolver, we don't need isolation since the merge is already applied.
	overlay := sandbox.NewDirectOverlay(r.config.WorkDir)

	// Execute the repair agent
	agentResult := r.executor.Execute(ctx, repairTask, overlay, nil, nil)

	result.AgentResult = agentResult
	result.Success = agentResult.Success
	result.Duration = time.Since(start)

	if !agentResult.Success {
		result.Error = agentResult.Error
	}

	// Send IPC event for repair agent completion
	if r.ipcClient != nil {
		ipcResult := &ipc.AgentResult{
			ExitCode:        agentResult.ExitCode,
			DurationSeconds: result.Duration.Seconds(),
			FilesChanged:    len(agentResult.Changes),
		}

		// Add token usage if available
		if agentResult.Output != nil {
			ipcResult.InputTokens = agentResult.Output.TotalInputTokens
			ipcResult.OutputTokens = agentResult.Output.TotalOutputTokens
			ipcResult.CostUSD = agentResult.Output.CostUSD
			ipcResult.DurationMS = agentResult.Output.DurationMS
			ipcResult.DurationAPIMS = agentResult.Output.DurationAPIMS
			ipcResult.NumTurns = agentResult.Output.NumTurns
		}

		if agentResult.GitState != nil {
			ipcResult.CommitsCreated = len(agentResult.GitState.NewCommits)
		}

		if result.Success {
			if err := r.ipcClient.SendAgentDone(
				repairAgentID,
				parentAgentID,
				ipcResult,
			); err != nil && r.config.Verbose {
				fmt.Fprintf(os.Stderr, "warning: failed to send repair done event: %v\n", err)
			}
		} else {
			if err := r.ipcClient.SendAgentFail(
				repairAgentID,
				parentAgentID,
				fmt.Errorf("%s", result.Error),
				ipcResult,
			); err != nil && r.config.Verbose {
				fmt.Fprintf(os.Stderr, "warning: failed to send repair fail event: %v\n", err)
			}
		}
	}

	return result, nil
}

// CleanupRepairContext removes the repair context files from the working directory.
// This should be called after repair completes (success or failure).
func CleanupRepairContext(workDir string) error {
	repairDir := workDir + "/.canopy/repair"
	if err := os.RemoveAll(repairDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to cleanup repair context: %w", err)
	}
	return nil
}
