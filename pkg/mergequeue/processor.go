package mergequeue

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/merge"
	"github.com/jzila/canopy/pkg/resolver"
)

// Processor handles merge operations from the queue.
// It processes merge requests sequentially, spawning resolver agents when conflicts occur.
type Processor struct {
	queue           *Queue
	merger          *merge.SequentialMerger
	resolver        *resolver.Resolver
	beadsClient     *beads.Client
	outputDir       string
	ipcClient       *ipc.Client
	verbose         bool
	resolverTimeout time.Duration // Timeout for resolver operations (0 = no timeout)
}

// NewProcessor creates a new merge processor.
// If resolverTimeout is 0, a default timeout of 10 minutes is used.
func NewProcessor(
	queue *Queue,
	merger *merge.SequentialMerger,
	resolver *resolver.Resolver,
	beadsClient *beads.Client,
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

// Start begins the merge processing loop.
// It dequeues merge requests and processes them sequentially.
// The loop runs until the context is cancelled or the queue is closed.
func (p *Processor) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Dequeue blocks until a request is available or queue is closed/paused
		req := p.queue.Dequeue()
		if req == nil {
			// Queue is closed
			return
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
func (p *Processor) processMerge(ctx context.Context, req *MergeRequest) *MergeResponse {
	resp := &MergeResponse{}
	taskID := req.Task.ID

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

	// Send merging status
	p.sendMergeStatus(taskID, ipc.MergeStatusMerging, 0, "")

	// Apply merge via merger.MergeSingle()
	// When there are patches, skip file fallback since resolver will handle failures
	mergeOpts := &merge.MergeOptions{
		SkipFileFallback: req.Result.GitState != nil && len(req.Result.GitState.Patches) > 0,
	}
	mergeResult, err := p.merger.MergeSingle(req.Result, mergeOpts)
	if err != nil {
		resp.Error = fmt.Sprintf("merge failed: %v", err)
		p.markTaskFailed(taskID, resp.Error)
		p.sendMergeStatus(taskID, ipc.MergeStatusFailed, 0, resp.Error)
		return resp
	}

	resp.CommitsApplied = mergeResult.CommitsApplied

	// Determine if we need to spawn a resolver agent
	needsResolver := false
	resolverReason := ""

	// Case 1: git am failed (patch application failed)
	if mergeResult.PatchFailed[taskID] && req.Result.GitState != nil && len(req.Result.GitState.Patches) > 0 {
		needsResolver = true
		resolverReason = "git patch failed"
	}

	// Case 2: Merge had errors (git add/commit failed)
	if !needsResolver && len(mergeResult.Errors) > 0 {
		needsResolver = true
		resolverReason = "merge errors: " + strings.Join(mergeResult.Errors, "; ")
	}

	// Case 3: No actual changes applied (might be filtering issue)
	if !needsResolver && mergeResult.CommitsApplied == 0 && len(mergeResult.Applied) == 0 {
		// Only spawn resolver if the agent actually produced something
		if len(req.Result.Changes) > 0 || (req.Result.GitState != nil && len(req.Result.GitState.Patches) > 0) {
			needsResolver = true
			resolverReason = "no changes applied despite agent output"
		}
	}

	// Spawn resolver if any merge issue was detected
	if needsResolver {
		if p.verbose {
			fmt.Printf("[%s] Merge issue detected (%s), spawning resolver agent...\n", taskID, resolverReason)
		}

		// Pause queue during conflict resolution
		p.queue.Pause()
		p.queue.SetResolverActive(true)
		p.sendMergeStatus(taskID, ipc.MergeStatusResolving, 0, "")

		resp.HadConflict = true
		resp.ResolverSpawned = true

		// Build conflict context for the resolver
		// Include patches if available, otherwise use file changes
		var patches []string
		if req.Result.GitState != nil && len(req.Result.GitState.Patches) > 0 {
			patches = req.Result.GitState.Patches
		}

		conflictCtx := &resolver.ConflictContext{
			TaskID:          taskID,
			TaskTitle:       req.Task.Title,
			TaskDescription: req.Task.Description,
			FailedPatches:   patches,
			PatchErrors:     mergeResult.Errors,
			FileChanges:     req.Result.Changes,
		}

		// Spawn resolver agent asynchronously
		resolverResult, resolverErr := p.resolveAsync(ctx, conflictCtx)

		// Resume queue after resolution (regardless of outcome)
		p.queue.SetResolverActive(false)
		p.queue.Resume()

		if resolverErr != nil {
			resp.Error = fmt.Sprintf("resolver error: %v", resolverErr)
			p.markTaskFailed(taskID, resp.Error)
			p.sendMergeStatus(taskID, ipc.MergeStatusFailed, 0, resp.Error)
			return resp
		}

		if resolverResult != nil && resolverResult.Success {
			if p.verbose {
				fmt.Printf("[%s-resolver] Conflict resolved successfully (%.1fs)\n",
					taskID, resolverResult.Duration.Seconds())
			}

			// Merge the resolver's result
			if resolverResult.AgentResult != nil && resolverResult.AgentResult.Overlay != nil {
				resolverMergeResult, mergeErr := p.merger.MergeSingle(resolverResult.AgentResult, nil)

				// Clean up resolver overlay regardless of merge outcome
				defer resolverResult.AgentResult.Overlay.Cleanup()

				if mergeErr != nil {
					errMsg := fmt.Sprintf("failed to merge resolver result: %v", mergeErr)
					resp.Error = errMsg
					p.markTaskFailed(taskID, errMsg)
					p.sendMergeStatus(taskID, ipc.MergeStatusFailed, 0, errMsg)
					return resp
				}

				// Check for merge errors (e.g., commit failures)
				if len(resolverMergeResult.Errors) > 0 {
					for _, errMsg := range resolverMergeResult.Errors {
						fmt.Fprintf(os.Stderr, "resolver merge error for %s: %s\n", taskID, errMsg)
					}
					errMsg := fmt.Sprintf("resolver merge had errors: %s", strings.Join(resolverMergeResult.Errors, "; "))
					resp.Error = errMsg
					p.markTaskFailed(taskID, errMsg)
					p.sendMergeStatus(taskID, ipc.MergeStatusFailed, 0, errMsg)
					return resp
				}

				// Verify commits were actually applied
				if resolverMergeResult.CommitsApplied == 0 && len(resolverMergeResult.Applied) == 0 {
					errMsg := fmt.Sprintf("resolver completed but produced no changes for %s", taskID)
					if p.verbose {
						fmt.Fprintf(os.Stderr, "[%s] Warning: %s\n", taskID, errMsg)
					}
					resp.Error = errMsg
					p.markTaskFailed(taskID, errMsg)
					p.sendMergeStatus(taskID, ipc.MergeStatusFailed, 0, errMsg)
					return resp
				}

				resp.CommitsApplied += resolverMergeResult.CommitsApplied
			}

			// Mark task as done after successful resolution
			p.markTaskDone(taskID)
			p.sendMergeStatus(taskID, ipc.MergeStatusMerged, 0, "")
			resp.Success = true
		} else {
			// Resolver failed
			errMsg := "resolver failed"
			if resolverResult != nil && resolverResult.Error != "" {
				errMsg = fmt.Sprintf("resolver failed: %s", resolverResult.Error)
			}
			resp.Error = errMsg
			p.markTaskFailed(taskID, errMsg)
			p.sendMergeStatus(taskID, ipc.MergeStatusFailed, 0, errMsg)

			if p.verbose {
				fmt.Fprintf(os.Stderr, "[%s-resolver] Failed to resolve conflict: %s\n", taskID, errMsg)
			}
		}

		return resp
	}

	// No conflicts and no errors - mark task as done
	// (Error cases and no-change cases are handled by resolver above)
	p.markTaskDone(taskID)
	p.sendMergeStatus(taskID, ipc.MergeStatusMerged, 0, "")

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
func (p *Processor) markTaskDone(taskID string) {
	if err := p.beadsClient.Done(taskID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark task %s done: %v\n", taskID, err)
	}
}

// markTaskFailed marks a task as failed in beads.
func (p *Processor) markTaskFailed(taskID string, reason string) {
	if err := p.beadsClient.Fail(taskID, reason); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to mark task %s as failed: %v\n", taskID, err)
	}
}

// sendMergeStatus sends a merge status update via IPC if client is connected.
func (p *Processor) sendMergeStatus(taskID string, status ipc.MergeStatus, queuePos int, errMsg string) {
	if p.ipcClient == nil {
		return
	}

	// Use taskID as agentID since we don't have a separate agent ID here
	agentID := fmt.Sprintf("agent-%s", taskID)
	if err := p.ipcClient.SendAgentMergeStatus(agentID, status, queuePos, errMsg); err != nil && p.verbose {
		fmt.Fprintf(os.Stderr, "warning: failed to send merge status for %s: %v\n", taskID, err)
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
