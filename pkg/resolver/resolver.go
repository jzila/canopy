// Package resolver implements conflict resolution agents for failed merge operations.
// When git am fails to apply patches, a resolver agent is spawned to manually
// resolve the conflicts and create new commits.
package resolver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/sandbox"
)

// Config holds resolver configuration
type Config struct {
	WorkDir       string                 // Base working directory
	TempDir       string                 // Temp directory for overlays
	Verbose       bool                   // Verbose logging
	UseBwrap      bool                   // Use bubblewrap sandbox
	SandboxConfig *sandbox.SandboxConfig // Sandbox configuration
	RepoID        string                 // Repository ID for IPC tracking
	RunID         string                 // Run ID for unique agent ID generation
}

// ConflictContext provides information about the failed merge
type ConflictContext struct {
	// TaskID is the ID of the original task that failed to merge
	TaskID string
	// TaskTitle is the title of the original task
	TaskTitle string
	// TaskDescription is the description of the original task
	TaskDescription string
	// FailedPatches contains the patch content that failed to apply
	FailedPatches []string
	// PatchErrors contains error messages from the failed patch attempts
	PatchErrors []string
	// FileChanges contains the file changes from the failed merge
	FileChanges []sandbox.FileChange
	// ParentAgentID is the ID of the parent agent (the implementor that failed)
	ParentAgentID string
	// BaseCommit is the commit hash where the original agent started work.
	// Patches in FailedPatches are diffs relative to this commit.
	BaseCommit string
}

// Result holds the outcome of a resolver agent execution
type Result struct {
	// Success indicates whether the resolver successfully resolved conflicts
	Success bool
	// Error contains an error message if resolution failed
	Error string
	// AgentResult contains the full agent execution result
	AgentResult *agent.Result
	// Duration is how long the resolver took
	Duration time.Duration
	// ResolverAgentID is the unique ID assigned to the resolver agent
	ResolverAgentID string
}

// Resolver manages conflict resolution agents
type Resolver struct {
	config    *Config
	executor  *agent.Executor
	ipcClient *ipc.Client
}

// New creates a new Resolver
func New(config *Config) *Resolver {
	executor := agent.NewExecutor(&agent.Config{
		Verbose:       config.Verbose,
		UseBwrap:      config.UseBwrap,
		SandboxConfig: config.SandboxConfig,
	})

	return &Resolver{
		config:   config,
		executor: executor,
	}
}

// SetIPCClient sets the IPC client for sending resolver events to the daemon.
// This enables parent-child agent tracking in the UI.
func (r *Resolver) SetIPCClient(client *ipc.Client) {
	r.ipcClient = client
}

// SetRepoID sets the repository ID for IPC tracking.
func (r *Resolver) SetRepoID(repoID string) {
	r.config.RepoID = repoID
}

// SetRunID sets the run ID for unique agent ID generation.
func (r *Resolver) SetRunID(runID string) {
	r.config.RunID = runID
}

// makeAgentID creates a unique resolver agent ID by combining run ID prefix with task ID.
// Format: agent-{runID[:8]}-{taskID}-resolver
func (r *Resolver) makeAgentID(taskID string) string {
	prefix := r.config.RunID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	if prefix == "" {
		// Fallback for when runID is not set (shouldn't happen in normal flow)
		return fmt.Sprintf("%s-resolver", taskID)
	}
	return fmt.Sprintf("agent-%s-%s-resolver", prefix, taskID)
}

// Resolve spawns a resolver agent to handle merge conflicts.
// It creates a fresh overlay based on current HEAD and provides the failed patch
// along with the original task context.
//
// The resolver agent:
// - Gets a fresh overlay based on current HEAD
// - Receives the failed patch as a file in the sandbox
// - Has access to the original task description
// - Should manually apply the changes and create proper commits
// - Is flagged as a child of the implementor agent via ParentAgentID
func (r *Resolver) Resolve(ctx context.Context, conflict *ConflictContext) (*Result, error) {
	start := time.Now()

	result := &Result{}

	// Generate unique resolver agent ID using run ID prefix
	resolverAgentID := r.makeAgentID(conflict.TaskID)
	result.ResolverAgentID = resolverAgentID

	// Create a fresh overlay based on current HEAD
	overlay, err := sandbox.NewOverlay(r.config.TempDir, r.config.WorkDir)
	if err != nil {
		result.Error = fmt.Sprintf("failed to create resolver sandbox: %v", err)
		result.Duration = time.Since(start)
		return result, nil
	}

	// Mount the overlay
	if err := overlay.Mount(); err != nil {
		overlay.Cleanup()
		result.Error = fmt.Sprintf("failed to mount resolver sandbox: %v", err)
		result.Duration = time.Since(start)
		return result, nil
	}

	// Write failed patches to sandbox for resolver to access
	if err := r.writePatchFiles(overlay, conflict); err != nil {
		overlay.Unmount()
		overlay.Cleanup()
		result.Error = fmt.Sprintf("failed to write patch files: %v", err)
		result.Duration = time.Since(start)
		return result, nil
	}

	// Create a synthetic beads task for the resolver
	resolverTask := &beads.Task{
		ID:          resolverAgentID,
		Title:       fmt.Sprintf("Resolve merge conflict for %s", conflict.TaskID),
		Description: r.buildResolverPrompt(conflict),
	}

	// Send IPC event for resolver start (child of original agent)
	if r.ipcClient != nil {
		if err := r.ipcClient.SendAgentStart(
			resolverAgentID,
			r.config.RunID,         // Run ID for historical filtering
			conflict.TaskID,        // TaskID is the original task
			resolverTask.Title,
			conflict.ParentAgentID, // Parent is the implementor agent
			r.config.RepoID,        // Repository ID for tracking
		); err != nil && r.config.Verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to send resolver start event: %v\n", err)
		}
	}

	// Execute the resolver agent
	agentResult := r.executor.Execute(ctx, resolverTask, overlay, nil, nil)

	// Cleanup overlay
	overlay.Unmount()
	// Don't cleanup directories yet - they're needed for merge

	result.AgentResult = agentResult
	result.Success = agentResult.Success
	result.Duration = time.Since(start)

	if !agentResult.Success {
		result.Error = agentResult.Error
	}

	// Send IPC event for resolver completion
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
				resolverAgentID,
				conflict.ParentAgentID,
				ipcResult,
			); err != nil && r.config.Verbose {
				fmt.Fprintf(os.Stderr, "warning: failed to send resolver done event: %v\n", err)
			}
		} else {
			if err := r.ipcClient.SendAgentFail(
				resolverAgentID,
				conflict.ParentAgentID,
				fmt.Errorf("%s", result.Error),
				ipcResult,
			); err != nil && r.config.Verbose {
				fmt.Fprintf(os.Stderr, "warning: failed to send resolver fail event: %v\n", err)
			}
		}
	}

	// Keep overlay reference in agent result for merge
	agentResult.Overlay = overlay

	return result, nil
}

// writePatchFiles writes the failed patches to the sandbox's .canopy directory
func (r *Resolver) writePatchFiles(overlay *sandbox.Overlay, conflict *ConflictContext) error {
	canopyDir := filepath.Join(overlay.MergedDir, ".canopy", "conflict")
	if err := os.MkdirAll(canopyDir, 0755); err != nil {
		return fmt.Errorf("failed to create conflict directory: %w", err)
	}

	// Write each failed patch
	for i, patch := range conflict.FailedPatches {
		patchPath := filepath.Join(canopyDir, fmt.Sprintf("patch-%d.patch", i))
		if err := os.WriteFile(patchPath, []byte(patch), 0644); err != nil {
			return fmt.Errorf("failed to write patch %d: %w", i, err)
		}
	}

	// Write patch errors for context
	if len(conflict.PatchErrors) > 0 {
		errorsPath := filepath.Join(canopyDir, "errors.txt")
		var content string
		for i, errMsg := range conflict.PatchErrors {
			content += fmt.Sprintf("=== Patch %d Error ===\n%s\n\n", i, errMsg)
		}
		if err := os.WriteFile(errorsPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write errors file: %w", err)
		}
	}

	// Write original task context
	contextPath := filepath.Join(canopyDir, "original-task.txt")
	context := fmt.Sprintf("Task ID: %s\nTitle: %s\n\nDescription:\n%s\n",
		conflict.TaskID, conflict.TaskTitle, conflict.TaskDescription)
	if err := os.WriteFile(contextPath, []byte(context), 0644); err != nil {
		return fmt.Errorf("failed to write context file: %w", err)
	}

	// Write base commit info if available
	if conflict.BaseCommit != "" {
		baseCommitPath := filepath.Join(canopyDir, "base-commit.txt")
		if err := os.WriteFile(baseCommitPath, []byte(conflict.BaseCommit+"\n"), 0644); err != nil {
			return fmt.Errorf("failed to write base commit file: %w", err)
		}
	}

	return nil
}

// buildResolverPrompt creates the prompt for the resolver agent.
// The prompt focuses specifically on conflict resolution - NOT reimplementation.
func (r *Resolver) buildResolverPrompt(conflict *ConflictContext) string {
	prompt := fmt.Sprintf(`## Merge Conflict Resolution

**CRITICAL: You are a MERGE RESOLVER, not a feature implementer.**

A previous agent completed the task below and created valid changes, but those changes could not be cleanly applied because HEAD has moved. Your ONLY job is to resolve the merge conflict - NOT to reimplement the feature.

### Original Task (for context only)
**ID:** %s
**Title:** %s

%s

### What Happened
1. An agent successfully completed this task and made commits
2. While that agent was working, other changes were merged to HEAD
3. The original agent's patches can no longer apply cleanly
4. The HEAD has been reset to a clean state before the merge attempt

### Your ONLY Goal: Resolve the Merge Conflict

The failed patches are in .canopy/conflict/patch-*.patch - these contain the EXACT changes the original agent made.

**DO:**
1. Read the patches carefully to understand what changes were made
2. Look at the current state of affected files
3. Apply the SAME changes from the patches, adapting for any conflicts with current HEAD
4. Preserve the original intent of the patches exactly
5. Create commits with messages that reference the original work

**DO NOT:**
- Reimplement the feature from scratch
- Make additional changes beyond what's in the patches
- "Improve" or extend the original implementation
- Skip changes because you think they're unnecessary
- Add new functionality not in the patches

### Conflict Files
- .canopy/conflict/patch-*.patch - The original patches (READ THESE FIRST)
- .canopy/conflict/errors.txt - Why the patches failed to apply
- .canopy/conflict/original-task.txt - Original task context

### Resolution Strategy
1. For each patch file, identify which hunks failed to apply
2. Find the affected lines in the current codebase
3. Apply the changes manually, resolving conflicts between old and new code
4. The result should be: current HEAD + the changes from the patches

### Success Criteria
Your work is complete when:
- All changes from the patches have been applied (adapted for current HEAD)
- The feature works as the original agent intended
- You've made clean commits that can be merged to HEAD
`, conflict.TaskID, conflict.TaskTitle, conflict.TaskDescription)

	return prompt
}
