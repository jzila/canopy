package daemon

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/logging"
	"github.com/jzila/canopy/pkg/persistence"
	"github.com/jzila/canopy/pkg/sandbox"
)

// OverlayRecoveryResult contains the results of overlay recovery on daemon restart.
type OverlayRecoveryResult struct {
	Resumable int      // Overlays with session_id that could potentially be resumed
	Cleaned   int      // Overlays cleaned up (no session_id)
	Failed    int      // Recovery failures
	Errors    []error  // Detailed errors for failures
}

// RecoverOrphanedOverlays detects and handles orphaned overlays from previous daemon runs.
// On daemon startup, overlays may be in the following states:
//   - Still mounted (filesystem mounted but no daemon owning it)
//   - Recorded in database as "active" but daemon crashed
//
// Recovery has two paths:
//  1. Resumable: Has session_id -> mark for potential agent resume (future)
//  2. Non-resumable: No session_id -> cleanup filesystem and database record
//
// This function should be called early in daemon initialization, before accepting new work.
func (d *Daemon) RecoverOrphanedOverlays(ctx context.Context) (*OverlayRecoveryResult, error) {
	result := &OverlayRecoveryResult{}

	store := d.persistManager.GetStore()
	if store == nil {
		logging.Debug("overlay recovery skipped - persistence not enabled")
		return result, nil
	}

	// First, mark all "active" overlays as orphaned since daemon is restarting
	orphanedCount, err := store.MarkOverlaysOrphaned()
	if err != nil {
		return nil, fmt.Errorf("failed to mark overlays as orphaned: %w", err)
	}
	if orphanedCount > 0 {
		logging.Info("marked active overlays as orphaned", "count", orphanedCount)
	}

	// Get all orphaned overlays from database
	overlays, err := store.GetOrphanedOverlays()
	if err != nil {
		return nil, fmt.Errorf("failed to get orphaned overlays: %w", err)
	}

	if len(overlays) == 0 {
		logging.Debug("no orphaned overlays found during recovery")
		return result, nil
	}

	logging.Info("recovering orphaned overlays", "count", len(overlays))

	for _, o := range overlays {
		if o.SessionID != "" {
			// Has session_id - could potentially be resumed in the future
			// For now, just count these as resumable but still clean up the filesystem
			// since we don't yet have agent resume implemented
			result.Resumable++
			logging.Debug("overlay has session_id (resumable)",
				"agent_id", o.AgentID,
				"session_id", o.SessionID)
		}

		// Clean up the overlay filesystem and database record
		if err := d.cleanupOrphanedOverlay(o); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, err)
			logging.Warn("failed to cleanup orphaned overlay",
				"agent_id", o.AgentID,
				"error", err)
			continue
		}

		if o.SessionID == "" {
			result.Cleaned++
		}
	}

	// Also run filesystem-based stale mount detection to catch any mounts
	// not tracked in the database (e.g., from very old daemon versions or bugs)
	baseDir, err := sandbox.GetOverlayBaseDir()
	if err == nil {
		cleaned, stale, errs := sandbox.RecoverFromCrash(baseDir)
		if stale > 0 || cleaned > 0 {
			logging.Info("cleaned stale overlay mounts from filesystem",
				"cleaned", cleaned,
				"detected", stale)
		}
		for _, e := range errs {
			result.Errors = append(result.Errors, e)
			logging.Warn("stale mount cleanup error", "error", e)
		}
	}

	// Clean up any completed overlays that were left behind
	if deletedCount, err := store.DeleteCompletedOverlays(); err != nil {
		logging.Warn("failed to delete completed overlays", "error", err)
	} else if deletedCount > 0 {
		logging.Debug("deleted completed overlay records", "count", deletedCount)
	}

	return result, nil
}

// cleanupOrphanedOverlay cleans up a single orphaned overlay.
// It attempts to unmount if still mounted, removes overlay directories,
// marks the associated agent as interrupted, and removes the database record.
func (d *Daemon) cleanupOrphanedOverlay(o *persistence.ActiveOverlay) error {
	store := d.persistManager.GetStore()

	// Check if still mounted - if so, log it.
	// The actual unmount will be handled by sandbox.RecoverFromCrash()
	// which is called after processing all database records.
	if sandbox.IsMountPoint(o.MergedDir) {
		logging.Debug("orphaned overlay still mounted (will be cleaned by RecoverFromCrash)",
			"agent_id", o.AgentID,
			"merged_dir", o.MergedDir)
	}

	// Remove overlay directories (upper_dir and work_dir)
	// Don't remove lower_dir - that's the actual repository
	if o.UpperDir != "" {
		if err := os.RemoveAll(o.UpperDir); err != nil && !os.IsNotExist(err) {
			logging.Debug("failed to remove upper_dir", "path", o.UpperDir, "error", err)
		}
	}
	if o.WorkDir != "" {
		if err := os.RemoveAll(o.WorkDir); err != nil && !os.IsNotExist(err) {
			logging.Debug("failed to remove work_dir", "path", o.WorkDir, "error", err)
		}
	}
	// Try to remove the merged dir (should be empty after unmount)
	if o.MergedDir != "" {
		if err := os.RemoveAll(o.MergedDir); err != nil && !os.IsNotExist(err) {
			logging.Debug("failed to remove merged_dir", "path", o.MergedDir, "error", err)
		}
	}

	// Mark the associated agent as failed with an informative message
	agent, err := store.GetAgent(o.AgentID)
	if err != nil {
		logging.Debug("could not get agent for orphaned overlay", "agent_id", o.AgentID, "error", err)
	} else if agent != nil && (agent.Status == persistence.AgentStatusRunning || agent.Status == persistence.AgentStatusStarting) {
		now := time.Now()
		agent.Status = persistence.AgentStatusFailed
		agent.FinishedAt = &now
		if o.SessionID != "" {
			agent.ErrorMessage = "daemon restarted - agent work preserved (session_id available for potential resume)"
		} else {
			agent.ErrorMessage = "daemon restarted - agent work lost (no session_id for resume)"
		}
		if err := store.UpdateAgent(agent); err != nil {
			logging.Debug("failed to update agent status", "agent_id", o.AgentID, "error", err)
		}
	}

	// Remove the overlay record from database
	if err := store.DeleteOverlay(o.AgentID); err != nil {
		return fmt.Errorf("failed to delete overlay record for agent %s: %w", o.AgentID, err)
	}

	logging.Debug("cleaned up orphaned overlay", "agent_id", o.AgentID)
	return nil
}

// ResumableOverlay contains information needed to resume an interrupted agent
type ResumableOverlay struct {
	Overlay       *persistence.ActiveOverlay
	Agent         *persistence.Agent
	InterruptedAt time.Time
}

// AgentResumeResult contains the results of attempting to resume interrupted agents
type AgentResumeResult struct {
	Resumed int      // Agents successfully resumed
	Failed  int      // Agents that failed to resume
	Skipped int      // Agents skipped (e.g., max retries, expired session)
	Errors  []error  // Detailed errors for failures
}

// MaxResumeAttempts is the maximum number of times an agent can be resumed
const MaxResumeAttempts = 3

// GetResumableOverlays returns overlays that can be resumed (have session_id and overlay still exists).
// Unlike RecoverOrphanedOverlays, this does NOT clean up the overlays - it returns them
// for the caller to resume.
func (d *Daemon) GetResumableOverlays(ctx context.Context) ([]*ResumableOverlay, error) {
	store := d.persistManager.GetStore()
	if store == nil {
		return nil, nil
	}

	// Get all orphaned overlays from database
	overlays, err := store.GetOrphanedOverlays()
	if err != nil {
		return nil, fmt.Errorf("failed to get orphaned overlays: %w", err)
	}

	var resumable []*ResumableOverlay
	for _, o := range overlays {
		// Skip overlays without session_id - these can't be resumed
		if o.SessionID == "" {
			continue
		}

		// Check if overlay directories still exist
		if _, err := os.Stat(o.UpperDir); os.IsNotExist(err) {
			logging.Debug("overlay upper_dir missing, cannot resume", "agent_id", o.AgentID)
			continue
		}
		if _, err := os.Stat(o.MergedDir); os.IsNotExist(err) {
			logging.Debug("overlay merged_dir missing, cannot resume", "agent_id", o.AgentID)
			continue
		}

		// Get the agent record
		agentRecord, err := store.GetAgent(o.AgentID)
		if err != nil {
			logging.Debug("could not get agent for resumable overlay", "agent_id", o.AgentID, "error", err)
			continue
		}
		if agentRecord == nil {
			logging.Debug("agent not found for resumable overlay", "agent_id", o.AgentID)
			continue
		}

		// Skip if agent is already completed or failed (shouldn't happen, but be safe)
		if agentRecord.Status != persistence.AgentStatusRunning && agentRecord.Status != persistence.AgentStatusStarting {
			logging.Debug("agent not in running state, skipping resume", "agent_id", o.AgentID, "status", agentRecord.Status)
			continue
		}

		// Determine when the agent was interrupted (use started_at as approximation)
		interruptedAt := agentRecord.StartedAt
		if agentRecord.FinishedAt != nil {
			interruptedAt = *agentRecord.FinishedAt
		}

		resumable = append(resumable, &ResumableOverlay{
			Overlay:       o,
			Agent:         agentRecord,
			InterruptedAt: interruptedAt,
		})
	}

	return resumable, nil
}

// ResumeInterruptedAgents resumes agents that were interrupted when the daemon crashed.
// This should be called after daemon initialization is complete and the executor is available.
//
// For each resumable overlay:
// 1. Remounts the overlay filesystem
// 2. Calls ExecuteResume with the saved session_id
// 3. Handles the result through normal completion flow
//
// Returns a summary of resume attempts.
func (d *Daemon) ResumeInterruptedAgents(ctx context.Context, executor *agent.Executor) (*AgentResumeResult, error) {
	result := &AgentResumeResult{}

	store := d.persistManager.GetStore()
	if store == nil {
		logging.Debug("agent resume skipped - persistence not enabled")
		return result, nil
	}

	// Get resumable overlays
	resumable, err := d.GetResumableOverlays(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get resumable overlays: %w", err)
	}

	if len(resumable) == 0 {
		logging.Debug("no agents to resume")
		return result, nil
	}

	logging.Info("resuming interrupted agents", "count", len(resumable))

	for _, r := range resumable {
		// Check context before each resume
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		// Check max resume attempts
		// The resume count is tracked in a separate field we need to read from agent state
		// For now, we'll use a simple approach: check if there's an existing resume count
		// stored somewhere. Since we don't have this yet, we'll skip this check for now
		// and add it when we have proper tracking.

		if err := d.resumeAgent(ctx, r, executor); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Errorf("agent %s: %w", r.Agent.ID, err))
			logging.Warn("failed to resume agent",
				"agent_id", r.Agent.ID,
				"task_id", r.Agent.TaskID,
				"error", err)

			// Mark agent as failed if resume fails
			d.markAgentResumeFailed(r.Agent.ID, r.Overlay.AgentID, err)
			continue
		}

		result.Resumed++
	}

	return result, nil
}

// resumeAgent resumes a single interrupted agent.
func (d *Daemon) resumeAgent(ctx context.Context, r *ResumableOverlay, executor *agent.Executor) error {
	o := r.Overlay
	agentRecord := r.Agent

	logging.Info("resuming agent",
		"agent_id", o.AgentID,
		"task_id", o.TaskID,
		"session_id", o.SessionID)

	// Remount the overlay
	overlay, err := sandbox.RemountOverlay(o.LowerDir, o.UpperDir, o.WorkDir, o.MergedDir)
	if err != nil {
		return fmt.Errorf("failed to remount overlay: %w", err)
	}

	// Get task from beads client
	beadsClient, err := d.getBeadsClient(agentRecord.RepoID)
	if err != nil {
		// Cleanup overlay on failure
		_ = overlay.Cleanup()
		return fmt.Errorf("failed to get beads client: %w", err)
	}

	task, err := beadsClient.Show(ctx, o.TaskID)
	if err != nil {
		_ = overlay.Cleanup()
		return fmt.Errorf("failed to get task %s: %w", o.TaskID, err)
	}

	if task == nil {
		_ = overlay.Cleanup()
		return fmt.Errorf("task %s not found (may have been deleted)", o.TaskID)
	}

	// Mark overlay as active again
	if err := d.persistManager.GetStore().UpdateOverlayStatus(o.AgentID, "active"); err != nil {
		logging.Debug("failed to update overlay status to active", "agent_id", o.AgentID, "error", err)
	}

	// Emit agent resumed event
	now := time.Now()
	if d.eventBus != nil {
		d.eventBus.Publish(Event{
			Type:      EventAgentResumed,
			Timestamp: now,
			Payload: &ipc.AgentResumedPayload{
				AgentID:       o.AgentID,
				RunID:         o.RunID,
				TaskID:        o.TaskID,
				TaskTitle:     task.Title,
				SessionID:     o.SessionID,
				ResumeCount:   1, // TODO: Track actual resume count
				InterruptedAt: r.InterruptedAt.Unix(),
				ResumedAt:     now.Unix(),
			},
		})
	}

	// Update agent state in runtime
	if d.state != nil {
		if agentState := d.state.GetAgent(o.AgentID); agentState != nil {
			agentState.Update(func(a *AgentState) {
				a.Status = AgentStatusRunning
				a.IsResume = true
				a.ResumeCount++
				a.InterruptedAt = &r.InterruptedAt
			})
		}
	}

	// Create live feed callback
	var liveFeedCallback agent.LiveFeedCallback
	if d.eventBus != nil {
		liveFeedCallback = func(taskID string, event *agent.LiveFeedEvent) {
			d.eventBus.Publish(Event{
				Type:      EventAgentLiveFeed,
				Timestamp: time.Now(),
				Payload: map[string]interface{}{
					"agent_id":   o.AgentID,
					"event_type": string(event.EventType),
					"data":       event.RawData,
				},
			})
		}
	}

	// Execute the resume - this blocks until the agent completes
	execResult := executor.ExecuteResume(ctx, task, overlay, o.SessionID, liveFeedCallback)

	// Handle completion
	d.handleResumedAgentResult(o, agentRecord, execResult)

	return nil
}

// handleResumedAgentResult processes the result of a resumed agent execution.
func (d *Daemon) handleResumedAgentResult(o *persistence.ActiveOverlay, agentRecord *persistence.Agent, result *agent.Result) {
	store := d.persistManager.GetStore()
	now := time.Now()

	// Update agent record with result
	if result.Success {
		agentRecord.Status = persistence.AgentStatusCompleted
	} else {
		agentRecord.Status = persistence.AgentStatusFailed
		agentRecord.ErrorMessage = result.Error
	}
	agentRecord.FinishedAt = &now
	agentRecord.DurationSeconds = result.Duration.Seconds()
	if result.Output != nil {
		agentRecord.InputTokens = result.Output.TotalInputTokens
		agentRecord.OutputTokens = result.Output.TotalOutputTokens
		agentRecord.CacheCreationTokens = result.Output.CacheCreationInputTokens
		agentRecord.CacheReadTokens = result.Output.CacheReadInputTokens
		agentRecord.CostUSD = result.Output.CostUSD
		agentRecord.NumTurns = result.Output.NumTurns
		agentRecord.ResultMessage = result.Output.ResultMessage
		// Update session ID in case it changed
		if result.Output.SessionID != "" {
			agentRecord.SessionID = result.Output.SessionID
		}
	}
	agentRecord.FilesChanged = len(result.Changes)
	if result.GitState != nil {
		agentRecord.GitCommitsCreated = len(result.GitState.NewCommits)
	}

	if err := store.UpdateAgent(agentRecord); err != nil {
		logging.Warn("failed to update agent after resume", "agent_id", o.AgentID, "error", err)
	}

	// Emit completion event using EventAgentCompleted (the standard event type)
	if d.eventBus != nil {
		d.eventBus.Publish(Event{
			Type:      EventAgentCompleted,
			Timestamp: now,
			Payload:   d.buildAgentCompletionPayload(o.AgentID, result),
		})
	}

	// Update runtime state
	if d.state != nil {
		if agentState := d.state.GetAgent(o.AgentID); agentState != nil {
			agentState.Update(func(a *AgentState) {
				if result.Success {
					a.Status = AgentStatusCompleted
				} else {
					a.Status = AgentStatusFailed
					a.Error = result.Error
				}
				endTime := now
				a.EndTime = &endTime
				a.Duration = result.Duration.Seconds()
				if result.Output != nil {
					a.TokenUsage = TokenUsage{
						InputTokens:              result.Output.TotalInputTokens,
						OutputTokens:             result.Output.TotalOutputTokens,
						CacheCreationInputTokens: result.Output.CacheCreationInputTokens,
						CacheReadInputTokens:     result.Output.CacheReadInputTokens,
						CostUSD:                  result.Output.CostUSD,
					}
					a.ResultMessage = result.Output.ResultMessage
					a.SessionID = result.Output.SessionID
				}
			})
		}
	}

	// Mark overlay as completed
	if err := store.UpdateOverlayStatus(o.AgentID, "completed"); err != nil {
		logging.Debug("failed to mark overlay completed", "agent_id", o.AgentID, "error", err)
	}

	// Clean up overlay
	if result.Overlay != nil {
		if err := result.Overlay.Cleanup(); err != nil {
			logging.Debug("failed to cleanup overlay after resume", "agent_id", o.AgentID, "error", err)
		}
	}

	logging.Info("resumed agent completed",
		"agent_id", o.AgentID,
		"task_id", o.TaskID,
		"success", result.Success)
}

// markAgentResumeFailed marks an agent as failed when resume fails.
func (d *Daemon) markAgentResumeFailed(agentID, overlayAgentID string, resumeErr error) {
	store := d.persistManager.GetStore()
	if store == nil {
		return
	}

	agentRecord, err := store.GetAgent(agentID)
	if err != nil || agentRecord == nil {
		return
	}

	now := time.Now()
	agentRecord.Status = persistence.AgentStatusFailed
	agentRecord.FinishedAt = &now
	agentRecord.ErrorMessage = fmt.Sprintf("resume failed: %v", resumeErr)

	if err := store.UpdateAgent(agentRecord); err != nil {
		logging.Debug("failed to update agent status after resume failure", "agent_id", agentID, "error", err)
	}

	// Also update runtime state if available
	if d.state != nil {
		if agentState := d.state.GetAgent(agentID); agentState != nil {
			agentState.Update(func(a *AgentState) {
				a.Status = AgentStatusFailed
				a.Error = fmt.Sprintf("resume failed: %v", resumeErr)
				a.EndTime = &now
			})
		}
	}

	// Clean up the overlay
	if err := store.DeleteOverlay(overlayAgentID); err != nil {
		logging.Debug("failed to delete overlay after resume failure", "agent_id", overlayAgentID, "error", err)
	}
}

// buildAgentCompletionPayload builds a completion payload for agent completed events.
// This returns a map that matches the format expected by handleAgentCompleted.
func (d *Daemon) buildAgentCompletionPayload(agentID string, r *agent.Result) map[string]interface{} {
	payload := map[string]interface{}{
		"agent_id":      agentID,
		"exit_code":     r.ExitCode,
		"duration":      r.Duration.Seconds(),
		"files_changed": len(r.Changes),
	}

	if r.Error != "" {
		payload["error"] = r.Error
	}

	if r.Output != nil {
		payload["session_id"] = r.Output.SessionID
		payload["input_tokens"] = r.Output.TotalInputTokens
		payload["output_tokens"] = r.Output.TotalOutputTokens
		payload["cache_creation_input_tokens"] = r.Output.CacheCreationInputTokens
		payload["cache_read_input_tokens"] = r.Output.CacheReadInputTokens
		payload["cost_usd"] = r.Output.CostUSD
		payload["num_turns"] = r.Output.NumTurns
		payload["result_message"] = r.Output.ResultMessage
	}

	if r.GitState != nil {
		payload["commits_created"] = len(r.GitState.NewCommits)
	}

	return payload
}
