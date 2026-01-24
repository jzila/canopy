package mergequeue

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/merge"
	"github.com/jzila/canopy/pkg/metrics"
	"github.com/jzila/canopy/pkg/repairagent"
	"github.com/jzila/canopy/pkg/resolver"
	"github.com/jzila/canopy/pkg/sandbox"
	"github.com/jzila/canopy/pkg/validation"
)

// MergeStatusEvent contains data for merge status callbacks.
// This allows the daemon to receive merge status updates without requiring an IPC client.
type MergeStatusEvent struct {
	AgentID         string
	Status          ipc.MergeStatus
	QueuePos        int
	Error           string
	CommitsApplied  int
	HadConflict     bool
	ResolverSpawned bool
}

// MergeStatusCallback is called when merge status changes.
// This provides an alternative to IPC for daemon-mode operation.
type MergeStatusCallback func(event MergeStatusEvent)

// CommitEvent contains git commit information from a merge.
// This allows the daemon to receive commit events without requiring an IPC client.
type CommitEvent struct {
	AgentID      string
	Hash         string
	ShortHash    string
	Message      string
	Author       string
	AuthorEmail  string
	Timestamp    string
	FilesChanged []string
}

// CommitCallback is called when a commit is created during merge.
// This provides an alternative to IPC for daemon-mode operation.
type CommitCallback func(event CommitEvent)

// Processor handles merge operations from the queue.
// It processes merge requests sequentially, spawning resolver agents when conflicts occur.
type Processor struct {
	queue           *Queue
	merger          *merge.SequentialMerger
	resolver        *resolver.Resolver
	beadsClient     beads.BeadsClient
	outputDir       string
	ipcClient       *ipc.Client
	verbose         bool
	resolverTimeout time.Duration // Timeout for resolver operations (0 = no timeout)
	runID           string        // Run ID for unique agent ID generation
	repoID          string        // Repository ID for IPC tracking

	// Callback for merge status events (alternative to IPC for daemon mode)
	mergeStatusCallback MergeStatusCallback

	// Callback for commit events (alternative to IPC for daemon mode)
	commitCallback CommitCallback

	// Validation and repair fields
	validationConfig *validation.ValidationConfig // Validation configuration (nil = disabled)
	repairAgent      *repairagent.RepairAgent     // Repair agent for fixing validation failures
	historyRecorder  *HistoryRecorder             // History recorder for audit trail
	sandboxConfig    *sandbox.SandboxConfig       // Sandbox configuration for repair agents
}

// NewProcessor creates a new merge processor.
// If resolverTimeout is 0, a default timeout of 10 minutes is used.
func NewProcessor(
	queue *Queue,
	merger *merge.SequentialMerger,
	resolver *resolver.Resolver,
	beadsClient beads.BeadsClient,
	outputDir string,
	ipcClient *ipc.Client,
	verbose bool,
	resolverTimeout time.Duration,
) *Processor {
	// Default timeout of 10 minutes if not specified
	if resolverTimeout == 0 {
		resolverTimeout = 10 * time.Minute
	}

	return &Processor{
		queue:           queue,
		merger:          merger,
		resolver:        resolver,
		beadsClient:     beadsClient,
		outputDir:       outputDir,
		ipcClient:       ipcClient,
		verbose:         verbose,
		resolverTimeout: resolverTimeout,
	}
}

// SetRunID sets the run ID for unique agent ID generation.
func (p *Processor) SetRunID(runID string) {
	p.runID = runID
}

// SetIPCClient sets the IPC client for sending status updates.
func (p *Processor) SetIPCClient(client *ipc.Client) {
	p.ipcClient = client
}

// SetRepoID sets the repository ID for IPC tracking.
func (p *Processor) SetRepoID(repoID string) {
	p.repoID = repoID
}

// SetMergeStatusCallback sets a callback for merge status events.
// This is used when running in daemon mode where IPC is not available.
// The callback is invoked whenever merge status would be sent via IPC.
func (p *Processor) SetMergeStatusCallback(callback MergeStatusCallback) {
	p.mergeStatusCallback = callback
}

// SetCommitCallback sets a callback for commit events.
// This is used when running in daemon mode where IPC is not available.
// The callback is invoked for each commit created during merge.
func (p *Processor) SetCommitCallback(callback CommitCallback) {
	p.commitCallback = callback
}

// SetValidationConfig sets the validation configuration.
// If nil, validation is disabled.
func (p *Processor) SetValidationConfig(config *validation.ValidationConfig) {
	p.validationConfig = config
}

// SetSandboxConfig sets the sandbox configuration for repair agents.
func (p *Processor) SetSandboxConfig(config *sandbox.SandboxConfig) {
	p.sandboxConfig = config
}

// InitializeRepairAgent creates the repair agent with current configuration.
// Must be called after SetRunID, SetRepoID, and SetSandboxConfig.
func (p *Processor) InitializeRepairAgent() {
	if p.validationConfig == nil || !p.validationConfig.IsEnabled() {
		return
	}

	p.repairAgent = repairagent.New(&repairagent.Config{
		WorkDir:       p.outputDir,
		Verbose:       p.verbose,
		SandboxConfig: p.sandboxConfig,
		RepoID:        p.repoID,
		RunID:         p.runID,
	})

	if p.ipcClient != nil {
		p.repairAgent.SetIPCClient(p.ipcClient)
	}

	p.historyRecorder = NewHistoryRecorder(p.beadsClient, p.verbose)
}

// makeAgentID creates a unique agent ID by combining run ID prefix with task ID.
// Format: agent-{runID[:8]}-{taskID}
func (p *Processor) makeAgentID(taskID string) string {
	prefix := p.runID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	if prefix == "" {
		// Fallback for when runID is not set (shouldn't happen in normal flow)
		return fmt.Sprintf("agent-%s", taskID)
	}
	return fmt.Sprintf("agent-%s-%s", prefix, taskID)
}

// Start begins the merge processing loop.
// It dequeues merge requests and processes them sequentially.
// The loop runs until the context is cancelled or the queue is closed.
func (p *Processor) Start(ctx context.Context) {
	for {
		// DequeueCtx respects context cancellation
		req := p.queue.DequeueCtx(ctx)
		if req == nil {
			// Queue is closed or context cancelled
			return
		}

		// Check context again after dequeue (in case it was cancelled while processing previous request)
		select {
		case <-ctx.Done():
			// Send cancellation response and return
			if req.Response != nil {
				select {
				case req.Response <- &MergeResponse{
					Success: false,
					Error:   "processor shutdown: " + ctx.Err().Error(),
				}:
				default:
				}
			}
			return
		default:
		}

		// Process the merge request
		resp := p.processMerge(ctx, req)

		// Send response back to the caller
		if req.Response != nil {
			select {
			case req.Response <- resp:
			default:
				// Response channel full or closed, log warning
				if p.verbose {
					fmt.Fprintf(os.Stderr, "warning: could not send merge response for task %s\n", req.Task.ID)
				}
			}
		}
	}
}

// processMerge handles a single merge request.
// It commits dirty beads changes, applies the merge, spawns resolver on conflict,
// marks the task as done/failed, and sends IPC status updates.
// Returns early with an error response if the context is cancelled.
func (p *Processor) processMerge(ctx context.Context, req *MergeRequest) *MergeResponse {
	resp := &MergeResponse{}
	taskID := req.Task.ID

	// Check context at start
	if err := ctx.Err(); err != nil {
		resp.Error = fmt.Sprintf("cancelled: %v", err)
		return resp
	}

	// Send initial pending status
	p.sendMergeStatus(taskID, ipc.MergeStatusPending, 0, "")

	// Auto-commit any dirty .beads/ changes before merge
	// This prevents git am from failing when .beads/ files have been modified
	if err := p.commitDirtyBeadsChanges(); err != nil {
		if p.verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to commit .beads/ changes: %v\n", err)
		}
		// Continue with merge - better to try than to fail completely
	}

	// Check context before merge operation
	if err := ctx.Err(); err != nil {
		resp.Error = fmt.Sprintf("cancelled before merge: %v", err)
		return resp
	}

	// Send merging status
	p.sendMergeStatus(taskID, ipc.MergeStatusMerging, 0, "")

	// Record pre-merge HEAD for potential rollback in strict validation mode
	preMergeCommit, err := p.getCurrentHead()
	if err != nil {
		if p.verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to get pre-merge HEAD: %v\n", err)
		}
		// Continue anyway - we just won't be able to revert in strict mode
	}

	// Check if HEAD has moved ahead of the overlay's base commit
	// If so, we need a 3-way merge via resolver instead of direct patch application
	needsResolver := false
	resolverReason := ""
	if req.Result.GitState != nil && req.Result.GitState.BaseCommit != "" {
		baseCommit := req.Result.GitState.BaseCommit
		if preMergeCommit != "" && baseCommit != preMergeCommit {
			// HEAD has moved - check if it's ahead of base
			mergeBase, err := p.getMergeBase(baseCommit, preMergeCommit)
			if err != nil {
				if p.verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to get merge-base for %s: %v\n", taskID, err)
				}
			} else if mergeBase == baseCommit {
				// HEAD is ahead of the overlay's base - must use resolver for 3-way merge
				needsResolver = true
				resolverReason = fmt.Sprintf("overlay stale (base %s, HEAD now %s)", baseCommit[:8], preMergeCommit[:8])
				if p.verbose {
					fmt.Printf("[%s] Detected stale overlay: base=%s, current HEAD=%s, merge-base=%s\n",
						taskID, baseCommit[:8], preMergeCommit[:8], mergeBase[:8])
				}
			}
		}
	}

	// Prepare merge options for later use
	mergeOpts := &merge.MergeOptions{
		TaskTitle: req.Task.Title,
	}

	// If overlay is stale, skip direct merge and go straight to resolver
	var mergeResult *merge.Result
	if needsResolver {
		// Create empty merge result to trigger resolver flow
		mergeResult = &merge.Result{
			Errors:      []string{resolverReason},
			PatchFailed: make(map[string]bool),
		}
		if p.verbose {
			fmt.Printf("[%s] Skipping direct merge due to stale overlay, will spawn resolver\n", taskID)
		}
	} else {
		// Apply merge via merger.MergeSingle()
		// Pass task title for commit message generation if agent didn't make commits
		mergeResult, err = p.merger.MergeSingle(req.Result, mergeOpts)
		if err != nil {
			resp.Error = fmt.Sprintf("merge failed: %v", err)
			p.markTaskFailed(ctx, taskID, resp.Error)
			p.sendTaskUpdated(taskID, req.Task.Title, "failed")
			p.sendMergeStatusFull(taskID, ipc.MergeStatusFailed, resp.Error, 0, false, false)
			return resp
		}
	}

	resp.CommitsApplied = mergeResult.CommitsApplied

	// Send commit events for merged commits immediately after merge
	// These are the actual commits in the repo (not overlay commits which no longer exist)
	p.sendMergedCommits(taskID, mergeResult)

	// Handle pre-commit hook failures before checking for resolver needs
	// Pre-commit hook failures are NOT merge conflicts - they're validation failures at commit time
	if mergeResult.PreCommitHookFailed {
		preCommitResult := p.runPreCommitRepairLoop(ctx, taskID, req, mergeResult, mergeOpts)
		if preCommitResult.Success {
			// Pre-commit repair succeeded - commit was successful
			resp.CommitsApplied = preCommitResult.CommitsApplied

			// Continue to validation if enabled
			mergedDiff := p.getMergedDiff(resp.CommitsApplied)
			validationResult := p.runValidationAndRepair(ctx, taskID, req.Task.Title, mergedDiff, p.makeAgentID(taskID), false, false, resp.CommitsApplied)

			if !validationResult.ValidationPassed {
				// Validation failed even after repair attempts
				errMsg := validationResult.Error
				if errMsg == "" {
					errMsg = "validation failed"
				}
				resp.Error = errMsg

				// Check if strict validation mode is enabled
				isStrict := p.validationConfig != nil && p.validationConfig.IsStrict()

				if isStrict {
					// Strict mode: revert the merge and fail the task completely
					if preMergeCommit != "" {
						if err := p.revertMerge(preMergeCommit); err != nil {
							fmt.Fprintf(os.Stderr, "warning: failed to revert merge for %s: %v\n", taskID, err)
						} else {
							resp.CommitsApplied = 0 // Reset since we reverted
						}
					}
					p.markTaskFailed(ctx, taskID, errMsg)
					p.sendTaskUpdated(taskID, req.Task.Title, "failed")
					p.sendMergeStatusFull(taskID, ipc.MergeStatusFailed, errMsg, resp.CommitsApplied, false, false)
					return resp
				}

				// Lenient mode: file bead and use merged_needs_repair status
				if validationResult.RepairExhausted {
					repairCtx := &repairExhaustedContext{
						TaskID:           taskID,
						TaskTitle:        req.Task.Title,
						ValidationResult: validationResult.FinalValidationResult,
						RepairSummaries:  validationResult.RepairAttemptSummaries,
						MergedDiff:       mergedDiff,
						MaxAttempts:      validationResult.AttemptsUsed,
					}
					if _, err := p.fileRepairExhaustedBead(ctx, repairCtx); err != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to file repair exhaustion bead for %s: %v\n", taskID, err)
					}
				} else {
					p.fileValidationFailureBead(ctx, taskID, req.Task.Title, validationResult.FinalValidationResult)
				}

				p.markTaskFailed(ctx, taskID, errMsg)
				p.sendTaskUpdated(taskID, req.Task.Title, "merged_needs_repair")
				p.sendMergeStatusFull(taskID, ipc.MergeStatusMergedNeedsRepair, errMsg, resp.CommitsApplied, false, false)
				return resp
			}

			// Mark task as done after successful merge and validation
			p.markTaskDone(ctx, taskID)
			p.sendTaskUpdated(taskID, req.Task.Title, "completed")
			p.sendMergeStatusFull(taskID, ipc.MergeStatusMerged, "", resp.CommitsApplied, false, false)

			resp.Success = true
			return resp
		}

		// Pre-commit repair failed - report failure
		resp.Error = preCommitResult.Error
		p.markTaskFailed(ctx, taskID, resp.Error)
		p.sendTaskUpdated(taskID, req.Task.Title, "failed")
		p.sendMergeStatusFull(taskID, ipc.MergeStatusFailed, resp.Error, resp.CommitsApplied, false, false)
		return resp
	}

	// Determine if we need to spawn a resolver agent
	// Note: needsResolver may already be true if we detected a stale overlay above
	// In that case, resolverReason is already set

	// Case 1: Merge had errors (git add/commit failed, or stale overlay detected)
	if !needsResolver && len(mergeResult.Errors) > 0 {
		needsResolver = true
		resolverReason = "merge errors: " + strings.Join(mergeResult.Errors, "; ")
	}

	// Case 2: No actual changes applied despite agent having git patches
	// Only spawn resolver if the agent made git commits that we couldn't apply.
	// If the agent had no patches (no commits), then "no changes applied" just means
	// the work was already done or there was nothing to do - not a conflict.
	if !needsResolver && mergeResult.CommitsApplied == 0 && len(mergeResult.Applied) == 0 {
		// Check if agent actually made git commits we failed to apply
		hasPatches := req.Result.GitState != nil && len(req.Result.GitState.Patches) > 0
		if hasPatches {
			needsResolver = true
			resolverReason = "no changes applied despite agent commits"
		} else if len(req.Result.Changes) > 0 && p.verbose {
			// Agent reported changes but made no commits and nothing was applied.
			// This is the "work already done" or "nothing to do" case - not a conflict.
			fmt.Printf("[%s] Agent reported %d file changes but made no commits and nothing was applied (work already done or filtered)\n",
				taskID, len(req.Result.Changes))
		}
	}

	// Check context before spawning resolver
	if err := ctx.Err(); err != nil {
		resp.Error = fmt.Sprintf("cancelled before resolver: %v", err)
		p.markTaskFailed(ctx, taskID, resp.Error)
		p.sendTaskUpdated(taskID, req.Task.Title, "failed")
		p.sendMergeStatusFull(taskID, ipc.MergeStatusFailed, resp.Error, resp.CommitsApplied, false, false)
		return resp
	}

	// Spawn resolver if any merge issue was detected
	if needsResolver {
		metrics.IncMergeConflicts()
		if p.verbose {
			fmt.Printf("[%s] Merge issue detected (%s), spawning resolver agent...\n", taskID, resolverReason)
		}

		// Pause queue during conflict resolution
		p.queue.AgentPause()
		p.sendMergeStatus(taskID, ipc.MergeStatusResolving, 0, "")

		resp.HadConflict = true
		resp.ResolverSpawned = true

		// Build conflict context for the resolver
		// Include the original patches so resolver can understand what was intended
		var failedPatches []string
		if req.Result.GitState != nil {
			failedPatches = req.Result.GitState.Patches
		}

		var baseCommit string
		if req.Result.GitState != nil {
			baseCommit = req.Result.GitState.BaseCommit
		}

		// Validate base commit exists before using it
		// If the repo was force-pushed or rebased, the commit may no longer exist
		if baseCommit != "" {
			if err := p.validateBaseCommit(baseCommit); err != nil {
				errMsg := fmt.Sprintf("cannot spawn resolver: %v", err)
				resp.Error = errMsg
				p.markTaskFailed(ctx, taskID, errMsg)
				p.sendTaskUpdated(taskID, req.Task.Title, "failed")
				p.sendMergeStatusFull(taskID, ipc.MergeStatusFailed, errMsg, resp.CommitsApplied, resp.HadConflict, false)
				// Resume queue since we're not spawning a resolver
				p.queue.AgentResume()
				return resp
			}
		}

		// Generate diff showing concurrent changes (what other agents merged)
		var concurrentDiff string
		if baseCommit != "" {
			diff, err := sandbox.GetDiffBetween(p.outputDir, baseCommit, "HEAD")
			if err != nil {
				if p.verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to generate concurrent diff: %v\n", err)
				}
			} else {
				concurrentDiff = diff
			}
		}

		// Extract base file contents for files mentioned in patches
		// This gives the resolver the original state of files before any changes
		var baseFileContents map[string]string
		if baseCommit != "" && len(failedPatches) > 0 {
			affectedFiles := sandbox.ExtractFilesFromPatches(failedPatches)
			if len(affectedFiles) > 0 {
				contents, err := sandbox.GetBaseFileContents(p.outputDir, baseCommit, affectedFiles)
				if err != nil {
					if p.verbose {
						fmt.Fprintf(os.Stderr, "warning: failed to get base file contents: %v\n", err)
					}
				} else {
					baseFileContents = contents
				}
			}
		}

		conflictCtx := &resolver.ConflictContext{
			TaskID:           taskID,
			TaskTitle:        req.Task.Title,
			TaskDescription:  req.Task.Description,
			FailedPatches:    failedPatches,
			PatchErrors:      mergeResult.Errors,
			FileChanges:      req.Result.Changes,
			ParentAgentID:    p.makeAgentID(taskID),
			BaseCommit:       baseCommit,
			ConcurrentDiff:   concurrentDiff,
			BaseFileContents: baseFileContents,
		}

		// Spawn resolver agent asynchronously
		resolverResult, resolverErr := p.resolveAsync(ctx, conflictCtx)

		// Resume queue after resolution (regardless of outcome)
		p.queue.AgentResume()

		if resolverErr != nil {
			// Track resolver failure metrics (timeout or other error)
			metrics.IncResolverFailure()

			resp.Error = fmt.Sprintf("resolver error: %v", resolverErr)
			p.markTaskFailed(ctx, taskID, resp.Error)
			p.sendTaskUpdated(taskID, req.Task.Title, "failed")
			p.sendMergeStatusFull(taskID, ipc.MergeStatusFailed, resp.Error, resp.CommitsApplied, resp.HadConflict, resp.ResolverSpawned)
			return resp
		}

		if resolverResult != nil && resolverResult.Success {
			// Track resolver success metrics
			metrics.IncResolverSuccess()
			metrics.ObserveResolverDuration(resolverResult.Duration.Seconds())

			if p.verbose {
				fmt.Printf("[%s-resolver] Conflict resolved successfully (%.1fs)\n",
					taskID, resolverResult.Duration.Seconds())
			}

			// Merge the resolver's result
			if resolverResult.AgentResult != nil && resolverResult.AgentResult.Overlay != nil {
				// Validate resolver overlay is accessible before attempting merge
				// This prevents infinite retry loops when overlays become inaccessible (e.g., permission denied)
				if err := p.validateOverlayAccessible(resolverResult.AgentResult.Overlay); err != nil {
					errMsg := fmt.Sprintf("resolver overlay inaccessible for %s: %v (cannot retry - overlay is stale)", taskID, err)
					fmt.Fprintf(os.Stderr, "ERROR: %s\n", errMsg)
					resp.Error = errMsg
					// Clean up the inaccessible overlay
					if cleanupErr := resolverResult.AgentResult.Overlay.Cleanup(); cleanupErr != nil && p.verbose {
						fmt.Fprintf(os.Stderr, "warning: failed to cleanup stale resolver overlay for %s: %v\n", taskID, cleanupErr)
					}
					// Mark task as permanently failed - retrying won't help with a stale overlay
					// The task will need manual intervention or the user needs to re-run canopy
					p.markTaskFailed(ctx, taskID, errMsg)
					p.sendTaskUpdated(taskID, req.Task.Title, "failed")
					p.sendMergeStatusFull(taskID, ipc.MergeStatusFailed, errMsg, resp.CommitsApplied, resp.HadConflict, resp.ResolverSpawned)
					return resp
				}

				resolverMergeResult, mergeErr := p.merger.MergeSingle(resolverResult.AgentResult, nil)

				// Clean up resolver overlay regardless of merge outcome
				// Cleanup errors are logged but don't affect the merge result
				defer func() {
					if cleanupErr := resolverResult.AgentResult.Overlay.Cleanup(); cleanupErr != nil && p.verbose {
						fmt.Fprintf(os.Stderr, "warning: failed to cleanup resolver overlay for %s: %v\n", taskID, cleanupErr)
					}
				}()

				if mergeErr != nil {
					errMsg := fmt.Sprintf("failed to merge resolver result: %v", mergeErr)
					resp.Error = errMsg
					p.markTaskFailed(ctx, taskID, errMsg)
					p.sendTaskUpdated(taskID, req.Task.Title, "failed")
					p.sendMergeStatusFull(taskID, ipc.MergeStatusFailed, errMsg, resp.CommitsApplied, resp.HadConflict, resp.ResolverSpawned)
					return resp
				}

				// Check for merge errors (e.g., commit failures)
				if len(resolverMergeResult.Errors) > 0 {
					for _, errMsg := range resolverMergeResult.Errors {
						fmt.Fprintf(os.Stderr, "resolver merge error for %s: %s\n", taskID, errMsg)
					}
					errMsg := fmt.Sprintf("resolver merge had errors: %s", strings.Join(resolverMergeResult.Errors, "; "))
					resp.Error = errMsg
					p.markTaskFailed(ctx, taskID, errMsg)
					p.sendTaskUpdated(taskID, req.Task.Title, "failed")
					p.sendMergeStatusFull(taskID, ipc.MergeStatusFailed, errMsg, resp.CommitsApplied, resp.HadConflict, resp.ResolverSpawned)
					return resp
				}

				// Check if commits were actually applied
				if resolverMergeResult.CommitsApplied == 0 && len(resolverMergeResult.Applied) == 0 {
					// Resolver completed but had no changes to apply.
					// This is a benign outcome (work already done or nothing to do), not a failure.
					msg := fmt.Sprintf("resolver completed with no changes for %s", taskID)
					if p.verbose {
						fmt.Fprintf(os.Stderr, "[%s] Info: %s\n", taskID, msg)
					}
					// Mark task as done since the resolver successfully determined there's nothing to do
					p.markTaskDone(ctx, taskID)
					p.sendTaskUpdated(taskID, req.Task.Title, "completed")
					p.sendMergeStatusFull(taskID, ipc.MergeStatusSkipped, msg, resp.CommitsApplied, resp.HadConflict, resp.ResolverSpawned)
					resp.Success = true
					return resp
				}

				resp.CommitsApplied += resolverMergeResult.CommitsApplied

				// Send commit events for resolver's merged commits
				p.sendMergedCommits(taskID, resolverMergeResult)
			}

			// Run validation and repair loop after successful resolution
			mergedDiff := p.getMergedDiff(resp.CommitsApplied)
			validationResult := p.runValidationAndRepair(ctx, taskID, req.Task.Title, mergedDiff, p.makeAgentID(taskID), resp.HadConflict, resp.ResolverSpawned, resp.CommitsApplied)

			if !validationResult.ValidationPassed {
				// Validation failed even after repair attempts
				errMsg := validationResult.Error
				if errMsg == "" {
					errMsg = "validation failed"
				}
				resp.Error = errMsg

				// Handle repair exhaustion: file bead and use merged_needs_repair status
				if validationResult.RepairExhausted {
					// File a bead with full context for manual intervention
					repairCtx := &repairExhaustedContext{
						TaskID:           taskID,
						TaskTitle:        req.Task.Title,
						ValidationResult: validationResult.FinalValidationResult,
						RepairSummaries:  validationResult.RepairAttemptSummaries,
						MergedDiff:       mergedDiff,
						MaxAttempts:      validationResult.AttemptsUsed,
					}
					if _, err := p.fileRepairExhaustedBead(ctx, repairCtx); err != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to file repair exhaustion bead for %s: %v\n", taskID, err)
					}

					// Mark task with merged_needs_repair status (merge succeeded, validation failed)
					p.markTaskFailed(ctx, taskID, errMsg)
					p.sendTaskUpdated(taskID, req.Task.Title, "merged_needs_repair")
					p.sendMergeStatusFull(taskID, ipc.MergeStatusMergedNeedsRepair, errMsg, resp.CommitsApplied, resp.HadConflict, resp.ResolverSpawned)
					return resp
				}

				// Regular validation failure (not exhaustion)
				p.markTaskFailed(ctx, taskID, errMsg)
				p.sendTaskUpdated(taskID, req.Task.Title, "failed")
				// Status already sent by runValidationAndRepair
				return resp
			}

			// Mark task as done after successful resolution and validation
			p.markTaskDone(ctx, taskID)
			p.sendTaskUpdated(taskID, req.Task.Title, "completed")
			p.sendMergeStatusFull(taskID, ipc.MergeStatusMerged, "", resp.CommitsApplied, resp.HadConflict, resp.ResolverSpawned)
			resp.Success = true
		} else {
			// Resolver failed - track metrics
			metrics.IncResolverFailure()
			if resolverResult != nil && resolverResult.Duration > 0 {
				metrics.ObserveResolverDuration(resolverResult.Duration.Seconds())
			}

			errMsg := "resolver failed"
			if resolverResult != nil && resolverResult.Error != "" {
				errMsg = fmt.Sprintf("resolver failed: %s", resolverResult.Error)
			}
			resp.Error = errMsg
			p.markTaskFailed(ctx, taskID, errMsg)
			p.sendTaskUpdated(taskID, req.Task.Title, "failed")
			p.sendMergeStatusFull(taskID, ipc.MergeStatusFailed, errMsg, resp.CommitsApplied, resp.HadConflict, resp.ResolverSpawned)

			if p.verbose {
				fmt.Fprintf(os.Stderr, "[%s-resolver] Failed to resolve conflict: %s\n", taskID, errMsg)
			}
		}

		return resp
	}

	// No conflicts and no errors - run validation and repair loop
	// (Error cases and no-change cases are handled by resolver above)
	mergedDiff := p.getMergedDiff(resp.CommitsApplied)
	validationResult := p.runValidationAndRepair(ctx, taskID, req.Task.Title, mergedDiff, p.makeAgentID(taskID), false, false, resp.CommitsApplied)

	if !validationResult.ValidationPassed {
		// Validation failed even after repair attempts
		errMsg := validationResult.Error
		if errMsg == "" {
			errMsg = "validation failed"
		}
		resp.Error = errMsg

		// Check if strict validation mode is enabled
		isStrict := p.validationConfig != nil && p.validationConfig.IsStrict()

		if isStrict {
			// Strict mode: revert the merge and fail the task completely
			if preMergeCommit != "" {
				if err := p.revertMerge(preMergeCommit); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to revert merge for %s: %v\n", taskID, err)
					// Still mark as failed even if revert fails
				} else {
					resp.CommitsApplied = 0 // Reset since we reverted
				}
			}
			p.markTaskFailed(ctx, taskID, errMsg)
			p.sendTaskUpdated(taskID, req.Task.Title, "failed")
			p.sendMergeStatusFull(taskID, ipc.MergeStatusFailed, errMsg, resp.CommitsApplied, false, false)
			return resp
		}

		// Lenient mode: file bead and use merged_needs_repair status
		// Handle repair exhaustion: file bead with full context for manual intervention
		if validationResult.RepairExhausted {
			repairCtx := &repairExhaustedContext{
				TaskID:           taskID,
				TaskTitle:        req.Task.Title,
				ValidationResult: validationResult.FinalValidationResult,
				RepairSummaries:  validationResult.RepairAttemptSummaries,
				MergedDiff:       mergedDiff,
				MaxAttempts:      validationResult.AttemptsUsed,
			}
			if _, err := p.fileRepairExhaustedBead(ctx, repairCtx); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to file repair exhaustion bead for %s: %v\n", taskID, err)
			}
		} else {
			// Regular validation failure (not exhaustion) in lenient mode - file a bead
			p.fileValidationFailureBead(ctx, taskID, req.Task.Title, validationResult.FinalValidationResult)
		}

		// Mark task with merged_needs_repair status (merge succeeded, validation failed)
		p.markTaskFailed(ctx, taskID, errMsg)
		p.sendTaskUpdated(taskID, req.Task.Title, "merged_needs_repair")
		p.sendMergeStatusFull(taskID, ipc.MergeStatusMergedNeedsRepair, errMsg, resp.CommitsApplied, false, false)
		return resp
	}

	// Mark task as done after successful merge and validation
	p.markTaskDone(ctx, taskID)
	p.sendTaskUpdated(taskID, req.Task.Title, "completed")
	p.sendMergeStatusFull(taskID, ipc.MergeStatusMerged, "", resp.CommitsApplied, false, false)

	resp.Success = true
	return resp
}

// commitDirtyBeadsChanges commits any uncommitted changes in the .beads/ directory.
// This prevents git am from failing when .beads/ files have been modified.
func (p *Processor) commitDirtyBeadsChanges() error {
	// Check if .beads/ directory has uncommitted changes
	statusCmd := exec.Command("git", "status", "--porcelain", ".beads/")
	statusCmd.Dir = p.outputDir
	output, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("git status failed: %w", err)
	}

	// No changes to commit
	if len(output) == 0 {
		return nil
	}

	if p.verbose {
		fmt.Printf("Auto-committing dirty .beads/ changes before merge\n")
	}

	// Stage .beads/ changes
	addCmd := exec.Command("git", "add", ".beads/")
	addCmd.Dir = p.outputDir
	var addStderr bytes.Buffer
	addCmd.Stderr = &addStderr
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add .beads/ failed: %w: %s", err, addStderr.String())
	}

	// Commit with a clear message
	commitCmd := exec.Command("git", "commit", "-m", "canopy: auto-commit beads changes before merge")
	commitCmd.Dir = p.outputDir
	var commitStderr bytes.Buffer
	commitCmd.Stderr = &commitStderr
	if err := commitCmd.Run(); err != nil {
		// Check if there's actually nothing to commit (possible race with bd sync)
		return fmt.Errorf("git commit failed: %w: %s", err, commitStderr.String())
	}

	return nil
}

// markTaskDone marks a task as completed in beads.
func (p *Processor) markTaskDone(ctx context.Context, taskID string) {
	if err := p.beadsClient.Done(ctx, taskID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark task %s done: %v\n", taskID, err)
	}
}

// markTaskFailed marks a task as failed in beads.
func (p *Processor) markTaskFailed(ctx context.Context, taskID string, reason string) {
	if err := p.beadsClient.Fail(ctx, taskID, reason); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to mark task %s as failed: %v\n", taskID, err)
	}
}

// sendMergedCommits sends commit events for each commit created during merge.
// This sends the actual merged commits (with repo hashes) instead of overlay commits.
// Uses callback in daemon mode, IPC client in CLI mode.
func (p *Processor) sendMergedCommits(taskID string, mergeResult *merge.Result) {
	if mergeResult == nil {
		return
	}

	agentID := p.makeAgentID(taskID)
	for _, commitInfo := range mergeResult.MergedCommits {
		// Try callback first (daemon mode)
		if p.commitCallback != nil {
			p.commitCallback(CommitEvent{
				AgentID:      agentID,
				Hash:         commitInfo.Hash,
				ShortHash:    commitInfo.ShortHash,
				Message:      commitInfo.Message,
				Author:       commitInfo.Author,
				AuthorEmail:  commitInfo.AuthorEmail,
				Timestamp:    commitInfo.Timestamp,
				FilesChanged: commitInfo.FilesChanged,
			})
			continue
		}

		// Fall back to IPC (CLI mode)
		if p.ipcClient == nil {
			continue
		}

		commit := &ipc.AgentCommitPayload{
			Hash:         commitInfo.Hash,
			ShortHash:    commitInfo.ShortHash,
			Message:      commitInfo.Message,
			Author:       commitInfo.Author,
			AuthorEmail:  commitInfo.AuthorEmail,
			Timestamp:    commitInfo.Timestamp,
			FilesChanged: commitInfo.FilesChanged,
		}

		if err := p.ipcClient.SendAgentCommit(agentID, commit); err != nil && p.verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to send merged commit for %s: %v\n", taskID, err)
		}
	}
}

// sendMergeStatus sends a merge status update via IPC or callback.
func (p *Processor) sendMergeStatus(taskID string, status ipc.MergeStatus, queuePos int, errMsg string) {
	agentID := p.makeAgentID(taskID)

	// Try callback first (daemon mode)
	if p.mergeStatusCallback != nil {
		p.mergeStatusCallback(MergeStatusEvent{
			AgentID:  agentID,
			Status:   status,
			QueuePos: queuePos,
			Error:    errMsg,
		})
		return
	}

	// Fall back to IPC (CLI mode)
	if p.ipcClient == nil {
		return
	}
	if err := p.ipcClient.SendAgentMergeStatus(agentID, status, queuePos, errMsg); err != nil && p.verbose {
		fmt.Fprintf(os.Stderr, "warning: failed to send merge status for %s: %v\n", taskID, err)
	}
}

// sendMergeStatusFull sends a final merge status update with full details via IPC or callback.
// This should be used for merged/failed statuses to include commit and conflict information.
func (p *Processor) sendMergeStatusFull(taskID string, status ipc.MergeStatus, errMsg string, commitsApplied int, hadConflict, resolverSpawned bool) {
	agentID := p.makeAgentID(taskID)

	// Try callback first (daemon mode)
	if p.mergeStatusCallback != nil {
		p.mergeStatusCallback(MergeStatusEvent{
			AgentID:         agentID,
			Status:          status,
			Error:           errMsg,
			CommitsApplied:  commitsApplied,
			HadConflict:     hadConflict,
			ResolverSpawned: resolverSpawned,
		})
		return
	}

	// Fall back to IPC (CLI mode)
	if p.ipcClient == nil {
		return
	}
	if err := p.ipcClient.SendAgentMergeStatusFull(agentID, status, 0, errMsg, commitsApplied, hadConflict, resolverSpawned); err != nil && p.verbose {
		fmt.Fprintf(os.Stderr, "warning: failed to send merge status for %s: %v\n", taskID, err)
	}
}

// sendTaskUpdated sends a task status update via IPC if client is connected.
func (p *Processor) sendTaskUpdated(taskID, title, status string) {
	if p.ipcClient == nil {
		return
	}

	agentID := p.makeAgentID(taskID)
	if err := p.ipcClient.SendTaskUpdated(taskID, title, status, agentID, p.repoID); err != nil && p.verbose {
		fmt.Fprintf(os.Stderr, "warning: failed to send task updated for %s: %v\n", taskID, err)
	}
}

// resolverResult captures the result of an async resolver operation
type resolverResult struct {
	result *resolver.Result
	err    error
}

// resolveAsync spawns a resolver agent asynchronously with timeout support.
// This prevents the merge queue from blocking while waiting for conflict resolution.
// Returns the resolver result and any error, just like the synchronous Resolve() call.
func (p *Processor) resolveAsync(ctx context.Context, conflict *resolver.ConflictContext) (*resolver.Result, error) {
	// Create a channel for the resolver result
	resultChan := make(chan resolverResult, 1)

	// Spawn resolver in a goroutine
	go func() {
		result, err := p.resolver.Resolve(ctx, conflict)
		resultChan <- resolverResult{result: result, err: err}
	}()

	// Wait for result with timeout
	select {
	case <-ctx.Done():
		// Context cancelled
		return nil, fmt.Errorf("resolver cancelled: %w", ctx.Err())

	case res := <-resultChan:
		// Resolver completed
		return res.result, res.err

	case <-time.After(p.resolverTimeout):
		// Timeout exceeded
		if p.verbose {
			fmt.Fprintf(os.Stderr, "[%s] Resolver timeout after %v\n", conflict.TaskID, p.resolverTimeout)
		}
		return nil, fmt.Errorf("resolver timeout after %v", p.resolverTimeout)
	}
}

// validateBaseCommit checks that the base commit exists in the repository.
// This is important because force-pushes or rebases can remove commits that
// agents' work was based on, leading to cryptic git failures later.
func (p *Processor) validateBaseCommit(baseCommit string) error {
	if baseCommit == "" {
		return fmt.Errorf("BaseCommit is empty")
	}

	// Check commit exists using git cat-file -e (exits 0 if exists, non-zero otherwise)
	cmd := exec.Command("git", "-C", p.outputDir, "cat-file", "-e", baseCommit)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("BaseCommit %s does not exist in repository (may have been force-pushed or rebased): %w", baseCommit, err)
	}

	// Check if commit is an ancestor of HEAD (optional validation - warn if not)
	// This can happen if the repo was rebased after the agent started
	cmd = exec.Command("git", "-C", p.outputDir, "merge-base", "--is-ancestor", baseCommit, "HEAD")
	if err := cmd.Run(); err != nil {
		if p.verbose {
			fmt.Fprintf(os.Stderr, "warning: BaseCommit %s is not an ancestor of HEAD (repository may have been rebased)\n", baseCommit)
		}
	}

	return nil
}

// validationAndRepairResult holds the outcome of the validation and repair loop.
type validationAndRepairResult struct {
	// ValidationPassed indicates whether validation passed (initial or after repair)
	ValidationPassed bool
	// RepairAttempted indicates whether any repair was attempted
	RepairAttempted bool
	// RepairSucceeded indicates whether repair fixed the validation failure
	RepairSucceeded bool
	// RepairExhausted indicates whether max repair attempts were used without success
	RepairExhausted bool
	// AttemptsUsed is the number of repair attempts made
	AttemptsUsed int
	// FinalValidationResult is the last validation result (may be nil if skipped)
	FinalValidationResult *validation.Result
	// RepairAttemptSummaries contains summaries of each repair attempt for bead filing
	RepairAttemptSummaries []string
	// Error contains any error that occurred during the process
	Error string
}

// runValidationAndRepair runs validation and, if it fails, spawns repair agents
// in a retry loop until validation passes or max attempts are exhausted.
//
// The loop follows this pattern:
//  1. Run validation
//  2. If validation passes -> return success
//  3. If validation fails and attempts < max -> spawn repair agent
//  4. Wait for repair agent to complete
//  5. Go to step 1
//  6. If validation fails and attempts >= max -> return failure
func (p *Processor) runValidationAndRepair(ctx context.Context, taskID, taskTitle, mergedDiff string, agentID string, hadConflict, resolverSpawned bool, commitsApplied int) *validationAndRepairResult {
	result := &validationAndRepairResult{}

	// Check if validation is enabled
	if p.validationConfig == nil || !p.validationConfig.IsEnabled() {
		// Validation disabled - skip
		result.ValidationPassed = true
		return result
	}

	maxAttempts := p.validationConfig.GetMaxRepairAttempts()
	var previousAttempts []string

	for attempt := 0; attempt <= maxAttempts; attempt++ {
		// Check context before each iteration
		if err := ctx.Err(); err != nil {
			result.Error = fmt.Sprintf("cancelled: %v", err)
			return result
		}

		// Run validation
		validationResult := p.runValidation(ctx, taskID, agentID, hadConflict, resolverSpawned, commitsApplied)
		result.FinalValidationResult = validationResult

		// Check if validation passed
		if validationResult.Status == validation.ValidationStatusPassed {
			result.ValidationPassed = true
			if attempt > 0 {
				result.RepairSucceeded = true
				// Record successful repair in history
				if p.historyRecorder != nil {
					_ = p.historyRecorder.RecordFinalStatus(ctx, taskID, FinalStatusRepaired,
						fmt.Sprintf("Validation passed after %d repair attempt(s)", attempt))
				}
			} else {
				// First-time pass
				if p.historyRecorder != nil {
					_ = p.historyRecorder.RecordFinalStatus(ctx, taskID, FinalStatusValidated, "")
				}
			}
			return result
		}

		// Validation failed
		result.AttemptsUsed = attempt + 1

		// Record validation failure in history
		if p.historyRecorder != nil {
			_ = p.historyRecorder.RecordValidationFailure(ctx, taskID, validationResult, attempt+1)
		}

		// Check if we've exhausted repair attempts
		if attempt >= maxAttempts {
			result.Error = fmt.Sprintf("validation failed after %d repair attempts: %s", maxAttempts, validationResult.Error)
			result.RepairExhausted = true
			result.RepairAttemptSummaries = previousAttempts
			if p.historyRecorder != nil {
				_ = p.historyRecorder.RecordFinalStatus(ctx, taskID, FinalStatusNeedsManualFix,
					fmt.Sprintf("Exhausted %d repair attempts", maxAttempts))
			}
			return result
		}

		// Check if we have a repair agent configured
		if p.repairAgent == nil {
			result.Error = fmt.Sprintf("validation failed and no repair agent configured: %s", validationResult.Error)
			if p.historyRecorder != nil {
				_ = p.historyRecorder.RecordFinalStatus(ctx, taskID, FinalStatusNeedsManualFix,
					"No repair agent configured")
			}
			return result
		}

		// Spawn repair agent
		result.RepairAttempted = true

		if p.verbose {
			fmt.Printf("[%s] Validation failed, spawning repair agent (attempt %d/%d)...\n",
				taskID, attempt+1, maxAttempts)
		}

		// Find the failed step for context
		var failedStep *validation.StepResult
		for i := range validationResult.Steps {
			if validationResult.Steps[i].Status == validation.ValidationStatusFailed {
				failedStep = &validationResult.Steps[i]
				break
			}
		}

		// Build repair context
		repairCtx := &repairagent.RepairContext{
			TaskID:            taskID,
			TaskTitle:         taskTitle,
			ValidationResult:  validationResult,
			FailedStep:        failedStep,
			MergedDiff:        mergedDiff,
			PreviousAttempts:  previousAttempts,
			RepairAttempt:     attempt + 1,
			MaxRepairAttempts: maxAttempts,
		}

		// Send IPC status update for repair starting
		p.sendValidationStatus(agentID, ipc.MergeStatusResolving, "", commitsApplied, hadConflict, resolverSpawned,
			"repairing", fmt.Sprintf("Repair attempt %d/%d", attempt+1, maxAttempts), 0, nil)

		// Execute repair agent
		repairResult, err := p.repairAgent.Repair(ctx, repairCtx, agentID)
		if err != nil {
			result.Error = fmt.Sprintf("repair agent error: %v", err)
			return result
		}

		// Record repair attempt in history
		repairAttempt := &RepairAttempt{
			Number:   attempt + 1,
			Duration: repairResult.Duration,
			AgentID:  repairResult.RepairAgentID,
		}
		if repairResult.AgentResult != nil && repairResult.AgentResult.GitState != nil {
			repairAttempt.CommitsApplied = len(repairResult.AgentResult.GitState.NewCommits)
		}

		if repairResult.Success {
			repairAttempt.Status = "success"
			if p.verbose {
				fmt.Printf("[%s] Repair agent completed successfully (%.1fs), re-running validation...\n",
					taskID, repairResult.Duration.Seconds())
			}
		} else {
			repairAttempt.Status = "failed"
			repairAttempt.Error = repairResult.Error
			if p.verbose {
				fmt.Printf("[%s] Repair agent failed: %s\n", taskID, repairResult.Error)
			}
		}

		if p.historyRecorder != nil {
			_ = p.historyRecorder.RecordRepairAttempt(ctx, taskID, repairAttempt)
		}

		// Build summary of this attempt for future repair agents
		attemptSummary := buildRepairAttemptSummary(repairAttempt, repairResult)
		previousAttempts = append(previousAttempts, attemptSummary)

		// Persist repair state for daemon restart recovery
		// lastRepairOutput captures the error for failed attempts or "success" for successful ones
		lastOutput := repairResult.Error
		if repairResult.Success {
			lastOutput = "Repair completed successfully"
		}
		p.sendRepairStatus(agentID, attempt+1, lastOutput, "repairing")

		// Clean up repair context files
		_ = repairagent.CleanupRepairContext(p.outputDir)

		// Even if repair agent "failed", we still re-run validation
		// because the agent might have made partial fixes
	}

	return result
}

// runValidation executes validation and sends IPC status updates.
func (p *Processor) runValidation(ctx context.Context, taskID, agentID string, hadConflict, resolverSpawned bool, commitsApplied int) *validation.Result {
	// Send validation starting status
	p.sendValidationStatus(agentID, ipc.MergeStatusMerging, "", commitsApplied, hadConflict, resolverSpawned,
		"running", "", 0, nil)

	// Create and run validation executor
	executor := validation.NewExecutor(p.validationConfig, p.outputDir, p.verbose)
	result, err := executor.Run(ctx)
	if err != nil {
		// Executor error (not validation failure)
		result = &validation.Result{
			Status: validation.ValidationStatusFailed,
			Error:  fmt.Sprintf("validation executor error: %v", err),
		}
	}

	// Convert validation steps to IPC format
	ipcSteps := make([]ipc.ValidationStep, len(result.Steps))
	for i, step := range result.Steps {
		ipcSteps[i] = ipc.ValidationStep{
			Name:     step.Name,
			Status:   string(step.Status),
			Duration: step.Duration.Milliseconds(),
			Output:   step.Output,
		}
	}

	// Send validation result status
	validationStatus := string(result.Status)
	validationError := result.Error
	validationDuration := result.Duration.Milliseconds()

	// Determine merge status based on validation result
	mergeStatus := ipc.MergeStatusMerged
	if result.Status == validation.ValidationStatusFailed {
		mergeStatus = ipc.MergeStatusFailed
	}

	p.sendValidationStatus(agentID, mergeStatus, "", commitsApplied, hadConflict, resolverSpawned,
		validationStatus, validationError, validationDuration, ipcSteps)

	return result
}

// sendValidationStatus sends a merge status update with validation information via IPC.
func (p *Processor) sendValidationStatus(agentID string, mergeStatus ipc.MergeStatus, errMsg string, commitsApplied int, hadConflict, resolverSpawned bool, validationStatus, validationError string, validationDurationMS int64, validationSteps []ipc.ValidationStep) {
	if p.ipcClient == nil {
		return
	}

	if err := p.ipcClient.SendAgentMergeStatusWithValidation(
		agentID, mergeStatus, errMsg, commitsApplied, hadConflict, resolverSpawned,
		validationStatus, validationError, validationDurationMS, validationSteps,
	); err != nil && p.verbose {
		fmt.Fprintf(os.Stderr, "warning: failed to send validation status: %v\n", err)
	}
}

// sendRepairStatus sends a repair state update via IPC.
// This persists repair attempt count and last repair output for daemon restart recovery.
func (p *Processor) sendRepairStatus(agentID string, repairAttempts int, lastRepairOutput string, validationStatus string) {
	if p.ipcClient == nil {
		return
	}

	if err := p.ipcClient.SendAgentRepairStatus(agentID, repairAttempts, lastRepairOutput, validationStatus); err != nil && p.verbose {
		fmt.Fprintf(os.Stderr, "warning: failed to send repair status: %v\n", err)
	}
}

// getMergedDiff returns the git diff for the commits that were just merged.
// This provides context for repair agents about what changes were introduced.
func (p *Processor) getMergedDiff(commitsApplied int) string {
	if commitsApplied == 0 {
		return ""
	}

	// Get diff of the last N commits that were applied
	cmd := exec.Command("git", "-C", p.outputDir, "diff", fmt.Sprintf("HEAD~%d..HEAD", commitsApplied))
	output, err := cmd.Output()
	if err != nil {
		if p.verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to get merged diff: %v\n", err)
		}
		return ""
	}

	return string(output)
}

// getCurrentHead returns the current HEAD commit hash.
// Used to record the pre-merge state for potential rollback.
func (p *Processor) getCurrentHead() (string, error) {
	cmd := exec.Command("git", "-C", p.outputDir, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// getMergeBase returns the best common ancestor between two commits.
// This is used to detect if HEAD has moved ahead of an overlay's base commit.
func (p *Processor) getMergeBase(commit1, commit2 string) (string, error) {
	cmd := exec.Command("git", "-C", p.outputDir, "merge-base", commit1, commit2)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git merge-base failed: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// revertMerge reverts the repository to a previous commit state.
// This is used in strict validation mode when validation fails after merge.
// It performs a hard reset to discard all changes made during the merge.
func (p *Processor) revertMerge(preMergeCommit string) error {
	if preMergeCommit == "" {
		return fmt.Errorf("no pre-merge commit specified")
	}

	cmd := exec.Command("git", "-C", p.outputDir, "reset", "--hard", preMergeCommit)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git reset --hard %s failed: %w: %s", preMergeCommit, err, stderr.String())
	}

	if p.verbose {
		fmt.Printf("Reverted merge to pre-merge commit %s (strict validation mode)\n", preMergeCommit[:8])
	}

	return nil
}

// buildRepairAttemptSummary constructs a summary of a repair attempt for
// inclusion in subsequent repair agent context. This helps later repair
// agents understand what was already tried and avoid repeating failed approaches.
func buildRepairAttemptSummary(attempt *RepairAttempt, result *repairagent.Result) string {
	var sb strings.Builder

	// Header with attempt number and status
	sb.WriteString(fmt.Sprintf("Attempt %d: %s\n", attempt.Number, attempt.Status))

	// Error details if failed
	if attempt.Error != "" {
		sb.WriteString(fmt.Sprintf("Error: %s\n", attempt.Error))
	}

	// Commits applied
	if attempt.CommitsApplied > 0 {
		sb.WriteString(fmt.Sprintf("Commits applied: %d\n", attempt.CommitsApplied))

		// Include commit messages if available
		if result != nil && result.AgentResult != nil && result.AgentResult.GitState != nil {
			for _, msg := range result.AgentResult.GitState.CommitMessages {
				// Truncate long commit messages
				if len(msg) > 200 {
					msg = msg[:200] + "..."
				}
				sb.WriteString(fmt.Sprintf("  - %s\n", msg))
			}
		}
	}

	// Files changed
	if result != nil && result.AgentResult != nil && len(result.AgentResult.Changes) > 0 {
		sb.WriteString(fmt.Sprintf("Files modified: %d\n", len(result.AgentResult.Changes)))
		// List first few files
		for i, change := range result.AgentResult.Changes {
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("  ... and %d more files\n", len(result.AgentResult.Changes)-5))
				break
			}
			sb.WriteString(fmt.Sprintf("  - %s\n", change.Path))
		}
	}

	// Agent's result message (the agent's summary of what it did)
	if result != nil && result.AgentResult != nil && result.AgentResult.Output != nil {
		if result.AgentResult.Output.ResultMessage != "" {
			// Truncate long result messages
			msg := result.AgentResult.Output.ResultMessage
			if len(msg) > 1000 {
				msg = msg[:1000] + "..."
			}
			sb.WriteString(fmt.Sprintf("\nAgent summary:\n%s\n", msg))
		}
	}

	return sb.String()
}

// repairExhaustedContext contains information for filing a bead when repair attempts are exhausted.
type repairExhaustedContext struct {
	TaskID           string
	TaskTitle        string
	ValidationResult *validation.Result
	RepairSummaries  []string
	MergedDiff       string
	MaxAttempts      int
}

// fileRepairExhaustedBead creates a bead documenting the repair exhaustion for manual intervention.
// Returns the created bead ID.
func (p *Processor) fileRepairExhaustedBead(ctx context.Context, repairCtx *repairExhaustedContext) (string, error) {
	if p.beadsClient == nil {
		return "", fmt.Errorf("no beads client configured")
	}

	// Build the bead description with full context
	var sb strings.Builder

	sb.WriteString("## Validation Failure - Repair Exhausted\n\n")
	sb.WriteString(fmt.Sprintf("Task: %s - %s\n\n", repairCtx.TaskID, repairCtx.TaskTitle))

	// Add validation step information
	if repairCtx.ValidationResult != nil {
		sb.WriteString("### Validation Step\n")
		for _, step := range repairCtx.ValidationResult.Steps {
			if step.Status == validation.ValidationStatusFailed {
				sb.WriteString(fmt.Sprintf("- Step: %s\n", step.Name))
				sb.WriteString(fmt.Sprintf("- Command: %s\n", step.Command))
				// Truncate very long output
				output := step.Output
				if len(output) > 2000 {
					output = output[:2000] + "\n... (truncated)"
				}
				sb.WriteString(fmt.Sprintf("- Final output:\n```\n%s\n```\n\n", output))
				break
			}
		}
	}

	// Add repair attempt summaries
	sb.WriteString("### Repair Attempts\n")
	for i, summary := range repairCtx.RepairSummaries {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, summary))
	}
	sb.WriteString("\n")

	// Add merged diff (truncated if large)
	if repairCtx.MergedDiff != "" {
		sb.WriteString("### Merged Diff\n")
		diff := repairCtx.MergedDiff
		if len(diff) > 4000 {
			diff = diff[:4000] + "\n... (truncated)"
		}
		sb.WriteString(fmt.Sprintf("```diff\n%s\n```\n\n", diff))
	}

	// Add action required section
	sb.WriteString("### Action Required\n")
	sb.WriteString(fmt.Sprintf("Manual intervention needed to fix validation. Automated repair exhausted after %d attempts.\n", repairCtx.MaxAttempts))

	// Create the bead with high priority (1 = high)
	title := fmt.Sprintf("Repair exhausted: %s", repairCtx.TaskTitle)
	if len(title) > 100 {
		title = title[:97] + "..."
	}

	beadID, err := p.beadsClient.CreateWithDescription(ctx, title, sb.String(), 1)
	if err != nil {
		return "", fmt.Errorf("failed to create repair exhausted bead: %w", err)
	}

	if p.verbose {
		fmt.Printf("[%s] Filed repair exhaustion bead: %s\n", repairCtx.TaskID, beadID)
	}

	return beadID, nil
}

// fileValidationFailureBead creates a bead documenting a validation failure (lenient mode).
// This is used when validation fails but we're not in strict mode, so the merge remains.
func (p *Processor) fileValidationFailureBead(ctx context.Context, taskID, taskTitle string, result *validation.Result) {
	if p.beadsClient == nil {
		return
	}

	// Build the bead description
	var sb strings.Builder

	sb.WriteString("## Validation Failure\n\n")
	sb.WriteString(fmt.Sprintf("Task: %s - %s\n\n", taskID, taskTitle))
	sb.WriteString("Merge was applied but validation failed. Manual fix required.\n\n")

	// Add validation step information
	if result != nil {
		sb.WriteString("### Failed Validation Steps\n")
		for _, step := range result.Steps {
			if step.Status == validation.ValidationStatusFailed {
				sb.WriteString(fmt.Sprintf("- Step: %s\n", step.Name))
				sb.WriteString(fmt.Sprintf("- Command: %s\n", step.Command))
				// Truncate very long output
				output := step.Output
				if len(output) > 2000 {
					output = output[:2000] + "\n... (truncated)"
				}
				sb.WriteString(fmt.Sprintf("- Output:\n```\n%s\n```\n\n", output))
			}
		}
	}

	// Add action required section
	sb.WriteString("### Action Required\n")
	sb.WriteString("Fix the validation failure manually. The merge has already been applied (lenient mode).\n")

	// Create the bead with high priority (1 = high)
	title := fmt.Sprintf("Validation failed: %s", taskTitle)
	if len(title) > 100 {
		title = title[:97] + "..."
	}

	beadID, err := p.beadsClient.CreateWithDescription(ctx, title, sb.String(), 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to create validation failure bead for %s: %v\n", taskID, err)
		return
	}

	if p.verbose {
		fmt.Printf("[%s] Filed validation failure bead: %s\n", taskID, beadID)
	}
}

// preCommitRepairResult holds the outcome of the pre-commit repair loop.
type preCommitRepairResult struct {
	// Success indicates whether the repair was successful and commit succeeded
	Success bool
	// CommitsApplied is the number of commits applied after repair
	CommitsApplied int
	// Error contains the error message if repair failed
	Error string
}

// runPreCommitRepairLoop runs a repair loop for pre-commit hook failures.
// It spawns repair agents to fix the issue and retries the commit until
// it succeeds or max attempts are exhausted.
//
// The loop follows this pattern:
//  1. Pre-commit hook failed, staged changes are preserved
//  2. Spawn repair agent to fix the issue
//  3. Repair agent makes fixes
//  4. Re-stage any new changes and retry commit
//  5. If commit succeeds -> return success
//  6. If commit fails again -> go to step 2 (if attempts remain)
//  7. If attempts exhausted -> return failure
func (p *Processor) runPreCommitRepairLoop(ctx context.Context, taskID string, req *MergeRequest, mergeResult *merge.Result, mergeOpts *merge.MergeOptions) *preCommitRepairResult {
	result := &preCommitRepairResult{}

	// Ensure repair agent is initialized
	if p.repairAgent == nil {
		// Try to initialize it now
		p.InitializeRepairAgent()
	}

	if p.repairAgent == nil {
		result.Error = "pre-commit hook failed and no repair agent configured"
		return result
	}

	maxAttempts := 3 // Default max attempts for pre-commit repair
	if p.validationConfig != nil {
		maxAttempts = p.validationConfig.GetMaxRepairAttempts()
	}

	var previousAttempts []string
	parentAgentID := p.makeAgentID(taskID)

	// Get the staged diff for repair context
	stagedDiff := p.getStagedDiff()

	// Build the commit message that was being used
	commitMsg := p.merger.BuildCommitMessageForTask(req.Result, mergeOpts)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Check context before each iteration
		if err := ctx.Err(); err != nil {
			result.Error = fmt.Sprintf("cancelled: %v", err)
			return result
		}

		if p.verbose {
			fmt.Printf("[%s] Pre-commit hook failed, spawning repair agent (attempt %d/%d)...\n",
				taskID, attempt, maxAttempts)
		}

		// Pause queue during pre-commit repair
		p.queue.AgentPause()
		p.sendMergeStatus(taskID, ipc.MergeStatusResolving, 0, "")

		// Build repair context
		repairCtx := &repairagent.PreCommitRepairContext{
			TaskID:            taskID,
			TaskTitle:         req.Task.Title,
			TaskDescription:   req.Task.Description,
			HookOutput:        mergeResult.PreCommitHookOutput,
			StagedDiff:        stagedDiff,
			StagedPaths:       mergeResult.StagedPaths,
			CommitMessage:     commitMsg,
			PreviousAttempts:  previousAttempts,
			RepairAttempt:     attempt,
			MaxRepairAttempts: maxAttempts,
		}

		// Execute repair agent
		repairResult, err := p.repairAgent.RepairPreCommit(ctx, repairCtx, parentAgentID)

		// Resume queue after repair
		p.queue.AgentResume()

		if err != nil {
			result.Error = fmt.Sprintf("pre-commit repair agent error: %v", err)
			return result
		}

		// Record this attempt summary for future attempts
		attemptSummary := buildPreCommitRepairAttemptSummary(attempt, repairResult)
		previousAttempts = append(previousAttempts, attemptSummary)

		// Clean up repair context files
		_ = repairagent.CleanupRepairContext(p.outputDir)

		if p.verbose {
			if repairResult.Success {
				fmt.Printf("[%s] Pre-commit repair agent completed successfully (%.1fs), retrying commit...\n",
					taskID, repairResult.Duration.Seconds())
			} else {
				fmt.Printf("[%s] Pre-commit repair agent failed: %s\n", taskID, repairResult.Error)
			}
		}

		// Even if repair agent "failed", we still retry the commit
		// because the agent might have made partial fixes

		// Re-stage any modified files and retry the commit
		// The repair agent may have modified files that need to be staged
		if err := p.restageAndRetryCommit(mergeResult.StagedPaths, commitMsg); err != nil {
			// Commit still failed - update the hook output for next attempt
			mergeResult.PreCommitHookOutput = err.Error()
			stagedDiff = p.getStagedDiff() // Refresh staged diff after repair changes
			continue
		}

		// Commit succeeded!
		result.Success = true
		result.CommitsApplied = 1
		return result
	}

	// Exhausted all attempts
	result.Error = fmt.Sprintf("pre-commit hook repair failed after %d attempts", maxAttempts)
	return result
}

// getStagedDiff returns the git diff of staged changes.
func (p *Processor) getStagedDiff() string {
	cmd := exec.Command("git", "-C", p.outputDir, "diff", "--cached")
	output, err := cmd.Output()
	if err != nil {
		if p.verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to get staged diff: %v\n", err)
		}
		return ""
	}
	return string(output)
}

// restageAndRetryCommit re-stages any modified files and attempts to commit.
// Returns nil on success, or an error containing the commit failure output.
func (p *Processor) restageAndRetryCommit(originalPaths []string, commitMsg string) error {
	// First, add any changes the repair agent made to already-staged files
	// This ensures modifications to previously staged files are included
	if len(originalPaths) > 0 {
		addCmd := exec.Command("git", "-C", p.outputDir, "add", "--")
		addCmd.Args = append(addCmd.Args, originalPaths...)
		var addStderr bytes.Buffer
		addCmd.Stderr = &addStderr
		if err := addCmd.Run(); err != nil {
			// Non-fatal - some paths may have been deleted by repair
			if p.verbose {
				fmt.Fprintf(os.Stderr, "warning: re-staging files: %v: %s\n", err, addStderr.String())
			}
		}
	}

	// Check if there are staged changes
	diffCmd := exec.Command("git", "-C", p.outputDir, "diff", "--cached", "--quiet")
	if err := diffCmd.Run(); err == nil {
		// No staged changes - nothing to commit
		// This means the repair agent unstaged everything (shouldn't happen)
		return fmt.Errorf("no staged changes after repair - repair agent may have unstaged files")
	}

	// Attempt the commit
	commitCmd := exec.Command("git", "-C", p.outputDir, "commit", "-m", commitMsg)
	var commitOutput bytes.Buffer
	commitCmd.Stdout = &commitOutput
	commitCmd.Stderr = &commitOutput

	if err := commitCmd.Run(); err != nil {
		// Commit still failed - return the output so we can try again
		return fmt.Errorf("%s", commitOutput.String())
	}

	return nil
}

// buildPreCommitRepairAttemptSummary constructs a summary of a pre-commit repair attempt.
func buildPreCommitRepairAttemptSummary(attempt int, repairResult *repairagent.Result) string {
	var sb strings.Builder

	status := "failed"
	if repairResult.Success {
		status = "completed"
	}

	sb.WriteString(fmt.Sprintf("Attempt %d: %s\n", attempt, status))

	if repairResult.Error != "" {
		sb.WriteString(fmt.Sprintf("Error: %s\n", repairResult.Error))
	}

	sb.WriteString(fmt.Sprintf("Duration: %.1fs\n", repairResult.Duration.Seconds()))

	// Include files modified by repair agent
	if repairResult.AgentResult != nil && len(repairResult.AgentResult.Changes) > 0 {
		sb.WriteString(fmt.Sprintf("Files modified: %d\n", len(repairResult.AgentResult.Changes)))
		for i, change := range repairResult.AgentResult.Changes {
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("  ... and %d more files\n", len(repairResult.AgentResult.Changes)-5))
				break
			}
			sb.WriteString(fmt.Sprintf("  - %s\n", change.Path))
		}
	}

	return sb.String()
}

// validateOverlayAccessible checks if an overlay's upper directory is accessible.
// This prevents infinite retry loops when overlays become inaccessible (e.g., permission denied,
// FUSE mount issues, or stale mounts after crashes).
//
// The check reads the upper directory to verify files can be accessed. If this fails,
// the overlay is considered stale and should not be used for merge operations.
func (p *Processor) validateOverlayAccessible(overlay *sandbox.Overlay) error {
	if overlay == nil {
		return fmt.Errorf("overlay is nil")
	}

	// Check that the upper directory exists and is accessible
	upperDir := overlay.UpperDir
	if upperDir == "" {
		// Direct overlay (no UpperDir) - these write directly to the repo
		return nil
	}

	// Try to read the upper directory
	entries, err := os.ReadDir(upperDir)
	if err != nil {
		return fmt.Errorf("cannot read upper directory %s: %w", upperDir, err)
	}

	// Try to access at least one file in the upper directory to verify permissions
	// This catches cases where the directory is listable but files aren't readable
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Skip whiteout files and hidden files (these are overlayfs markers)
		name := entry.Name()
		if strings.HasPrefix(name, ".wh.") || strings.HasPrefix(name, ".") {
			continue
		}

		// Try to open the file
		filePath := filepath.Join(upperDir, name)
		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("cannot access file %s in upper directory: %w", filePath, err)
		}
		_ = f.Close()
		break // One successful file access is enough
	}

	return nil
}
