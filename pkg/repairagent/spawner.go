// Package repairagent provides repair agent functionality for fixing validation failures.
package repairagent

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/sandbox"
)

// Config holds repair agent configuration.
type Config struct {
	WorkDir       string                 // Base working directory (the actual repo, not overlay)
	TempDir       string                 // Temp directory for overlays (if needed)
	Verbose       bool                   // Verbose logging
	UseBwrap      bool                   // Use bubblewrap sandbox
	SandboxConfig *sandbox.SandboxConfig // Sandbox configuration
	RepoID        string                 // Repository ID for tracking
	RunID         string                 // Run ID for unique agent ID generation
	Model         string                 // Model to use (empty = use Claude CLI default)
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

// AgentCallback is called for agent lifecycle events (start, done, fail).
type AgentCallback func(event AgentEvent)

// AgentEvent contains data for agent lifecycle callbacks.
// This allows the daemon to receive agent events from repair agents
// without requiring an IPC client.
type AgentEvent struct {
	AgentID         string
	RunID           string
	TaskID          string
	TaskTitle       string
	TaskDescription string
	ParentAgentID   string
	RepoID          string
	EventType       string // "started", "completed", "failed"
	// Completion fields (only set for completed/failed events)
	ExitCode        int
	DurationSeconds float64
	FilesChanged    int
	InputTokens     int
	OutputTokens    int
	CostUSD         float64
	DurationMS      int64
	DurationAPIMS   int64
	NumTurns        int
	CommitsCreated  int
	Error           string // Only for failed events
}

// RepairAgent manages repair agents for fixing validation failures.
type RepairAgent struct {
	config        *Config
	executor      *agent.Executor
	agentCallback AgentCallback
}

// New creates a new RepairAgent.
func New(config *Config) *RepairAgent {
	executor := agent.NewExecutor(&agent.Config{
		Verbose:       config.Verbose,
		UseBwrap:      config.UseBwrap,
		SandboxConfig: config.SandboxConfig,
		Model:         config.Model,
	})

	return &RepairAgent{
		config:   config,
		executor: executor,
	}
}

// SetAgentCallback sets the callback for agent lifecycle events.
// The callback is invoked for agent start, done, and fail events.
func (r *RepairAgent) SetAgentCallback(callback AgentCallback) {
	r.agentCallback = callback
}

// SetRepoID sets the repository ID for tracking.
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

	// Send event for repair agent start (child of original agent)
	if r.agentCallback != nil {
		r.agentCallback(AgentEvent{
			AgentID:         repairAgentID,
			RunID:           r.config.RunID,
			TaskID:          repairCtx.TaskID,
			TaskTitle:       repairTask.Title,
			TaskDescription: repairCtx.TaskTitle,
			ParentAgentID:   parentAgentID,
			RepoID:          r.config.RepoID,
			EventType:       "started",
		})
	}

	// Create a direct overlay that wraps the working directory without isolation.
	// This allows the repair agent to commit directly to the repo.
	// Unlike the resolver, we don't need isolation since the merge is already applied.
	overlay := sandbox.NewDirectOverlay(r.config.WorkDir)

	// Execute the repair agent
	agentResult := r.executor.Execute(ctx, repairTask, overlay, nil, nil)

	// Set the original bead ID for commit messages (TaskID is the synthetic repair agent ID)
	agentResult.BeadID = repairCtx.TaskID

	result.AgentResult = agentResult
	result.Success = agentResult.Success
	result.Duration = time.Since(start)

	if !agentResult.Success {
		result.Error = agentResult.Error
	}

	// Send event for repair agent completion
	if r.agentCallback != nil {
		event := AgentEvent{
			AgentID:         repairAgentID,
			RunID:           r.config.RunID,
			TaskID:          repairCtx.TaskID,
			TaskTitle:       repairTask.Title,
			TaskDescription: repairCtx.TaskTitle,
			ParentAgentID:   parentAgentID,
			RepoID:          r.config.RepoID,
			ExitCode:        agentResult.ExitCode,
			DurationSeconds: result.Duration.Seconds(),
			FilesChanged:    len(agentResult.Changes),
		}

		// Add token usage if available
		if agentResult.Output != nil {
			event.InputTokens = agentResult.Output.TotalInputTokens
			event.OutputTokens = agentResult.Output.TotalOutputTokens
			event.CostUSD = agentResult.Output.CostUSD
			event.DurationMS = agentResult.Output.DurationMS
			event.DurationAPIMS = agentResult.Output.DurationAPIMS
			event.NumTurns = agentResult.Output.NumTurns
		}

		if agentResult.GitState != nil {
			event.CommitsCreated = len(agentResult.GitState.NewCommits)
		}

		if result.Success {
			event.EventType = "completed"
		} else {
			event.EventType = "failed"
			event.Error = result.Error
		}

		r.agentCallback(event)
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

// RepairPreCommit spawns a repair agent to fix pre-commit hook failures.
// Unlike Repair which handles post-merge validation failures, this handles failures
// that occur at commit time when the pre-commit hook rejects the changes.
//
// The repair agent:
//   - Runs directly on the working directory (changes are already applied)
//   - Receives pre-commit hook failure context
//   - Has access to the staged changes that failed to commit
//   - Should fix the issue so the commit can be retried
//   - Is flagged as a child of the implementor agent via ParentAgentID
func (r *RepairAgent) RepairPreCommit(ctx context.Context, repairCtx *PreCommitRepairContext, parentAgentID string) (*Result, error) {
	start := time.Now()

	result := &Result{}

	// Generate unique repair agent ID
	repairAgentID := r.makeAgentID(repairCtx.TaskID, repairCtx.RepairAttempt)
	result.RepairAgentID = repairAgentID

	// Write repair context files to the working directory
	if err := WritePreCommitRepairContext(r.config.WorkDir, repairCtx); err != nil {
		result.Error = fmt.Sprintf("failed to write pre-commit repair context: %v", err)
		result.Duration = time.Since(start)
		return result, nil
	}

	// Build the repair prompt
	prompt := BuildPreCommitRepairPrompt(repairCtx)

	// Create a synthetic beads task for the repair agent
	repairTask := &beads.Task{
		ID:          repairAgentID,
		Title:       fmt.Sprintf("Repair pre-commit hook failure for %s", repairCtx.TaskID),
		Description: prompt,
	}

	// Send event for repair agent start (child of original agent)
	if r.agentCallback != nil {
		r.agentCallback(AgentEvent{
			AgentID:         repairAgentID,
			RunID:           r.config.RunID,
			TaskID:          repairCtx.TaskID,
			TaskTitle:       repairTask.Title,
			TaskDescription: repairCtx.TaskDescription,
			ParentAgentID:   parentAgentID,
			RepoID:          r.config.RepoID,
			EventType:       "started",
		})
	}

	// Create a direct overlay that wraps the working directory without isolation.
	// This allows the repair agent to commit directly to the repo.
	overlay := sandbox.NewDirectOverlay(r.config.WorkDir)

	// Execute the repair agent
	agentResult := r.executor.Execute(ctx, repairTask, overlay, nil, nil)

	// Set the original bead ID for commit messages (TaskID is the synthetic repair agent ID)
	agentResult.BeadID = repairCtx.TaskID

	result.AgentResult = agentResult
	result.Success = agentResult.Success
	result.Duration = time.Since(start)

	if !agentResult.Success {
		result.Error = agentResult.Error
	}

	// Send event for repair agent completion
	if r.agentCallback != nil {
		event := AgentEvent{
			AgentID:         repairAgentID,
			RunID:           r.config.RunID,
			TaskID:          repairCtx.TaskID,
			TaskTitle:       repairTask.Title,
			TaskDescription: repairCtx.TaskDescription,
			ParentAgentID:   parentAgentID,
			RepoID:          r.config.RepoID,
			ExitCode:        agentResult.ExitCode,
			DurationSeconds: result.Duration.Seconds(),
			FilesChanged:    len(agentResult.Changes),
		}

		// Add token usage if available
		if agentResult.Output != nil {
			event.InputTokens = agentResult.Output.TotalInputTokens
			event.OutputTokens = agentResult.Output.TotalOutputTokens
			event.CostUSD = agentResult.Output.CostUSD
			event.DurationMS = agentResult.Output.DurationMS
			event.DurationAPIMS = agentResult.Output.DurationAPIMS
			event.NumTurns = agentResult.Output.NumTurns
		}

		if agentResult.GitState != nil {
			event.CommitsCreated = len(agentResult.GitState.NewCommits)
		}

		if result.Success {
			event.EventType = "completed"
		} else {
			event.EventType = "failed"
			event.Error = result.Error
		}

		r.agentCallback(event)
	}

	return result, nil
}
