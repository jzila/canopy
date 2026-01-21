package daemon

import (
	"context"
	"fmt"
	"os"
	"time"

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
