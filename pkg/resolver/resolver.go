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
	Model         string                 // Model to use (empty = use Claude CLI default)
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
	// ConcurrentDiff is the git diff from BaseCommit to current HEAD,
	// showing what other agents merged while this agent was working.
	ConcurrentDiff string
	// BaseFileContents maps file paths to their content at BaseCommit.
	// Only populated for files mentioned in FailedPatches that existed at BaseCommit.
	// Files that were created by the agent (didn't exist at base) will not appear here.
	BaseFileContents map[string]string
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

// AgentCallback is called for agent lifecycle events (start, done, fail).
// This is used when running in daemon mode where IPC is not available.
type AgentCallback func(event AgentEvent)

// AgentEvent contains data for agent lifecycle callbacks.
// This allows the daemon to receive agent events from resolver/repair agents
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

// Resolver manages conflict resolution agents
type Resolver struct {
	config        *Config
	executor      *agent.Executor
	agentCallback AgentCallback
}

// New creates a new Resolver
func New(config *Config) *Resolver {
	executor := agent.NewExecutor(&agent.Config{
		Verbose:       config.Verbose,
		UseBwrap:      config.UseBwrap,
		SandboxConfig: config.SandboxConfig,
		Model:         config.Model,
	})

	return &Resolver{
		config:   config,
		executor: executor,
	}
}

// SetModel updates the model used for new resolver agent executions.
func (r *Resolver) SetModel(model string) {
	r.config.Model = model
	r.executor.SetModel(model)
}

// SetAgentCallback sets the callback for agent lifecycle events.
// The callback is invoked for agent start, done, and fail events.
func (r *Resolver) SetAgentCallback(callback AgentCallback) {
	r.agentCallback = callback
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
		_ = overlay.Cleanup()
		result.Error = fmt.Sprintf("failed to mount resolver sandbox: %v", err)
		result.Duration = time.Since(start)
		return result, nil
	}

	// Write failed patches to sandbox for resolver to access
	if err := r.writePatchFiles(overlay, conflict); err != nil {
		_ = overlay.Unmount()
		_ = overlay.Cleanup()
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

	// Send event for resolver start (child of original agent)
	if r.agentCallback != nil {
		r.agentCallback(AgentEvent{
			AgentID:         resolverAgentID,
			RunID:           r.config.RunID,
			TaskID:          conflict.TaskID,
			TaskTitle:       resolverTask.Title,
			TaskDescription: conflict.TaskDescription,
			ParentAgentID:   conflict.ParentAgentID,
			RepoID:          r.config.RepoID,
			EventType:       "started",
		})
	}

	// Execute the resolver agent
	agentResult := r.executor.Execute(ctx, resolverTask, overlay, nil, nil)

	// Set the original bead ID for commit messages (TaskID is the synthetic resolver agent ID)
	agentResult.BeadID = conflict.TaskID

	// Cleanup overlay
	_ = overlay.Unmount()
	// Don't cleanup directories yet - they're needed for merge

	result.AgentResult = agentResult
	result.Success = agentResult.Success
	result.Duration = time.Since(start)

	if !agentResult.Success {
		result.Error = agentResult.Error
	}

	// Send event for resolver completion
	if r.agentCallback != nil {
		event := AgentEvent{
			AgentID:         resolverAgentID,
			RunID:           r.config.RunID,
			TaskID:          conflict.TaskID,
			TaskTitle:       resolverTask.Title,
			TaskDescription: conflict.TaskDescription,
			ParentAgentID:   conflict.ParentAgentID,
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

	// Write concurrent changes diff if available
	// This shows what other agents merged while the original agent was working
	if conflict.ConcurrentDiff != "" {
		diffPath := filepath.Join(canopyDir, "concurrent-changes.diff")
		if err := os.WriteFile(diffPath, []byte(conflict.ConcurrentDiff), 0644); err != nil {
			return fmt.Errorf("failed to write concurrent diff file: %w", err)
		}
	}

	// Write base file contents to .canopy/conflict/base/<filepath>
	// This provides the original state of files at the base commit
	if len(conflict.BaseFileContents) > 0 {
		baseDir := filepath.Join(canopyDir, "base")
		if err := os.MkdirAll(baseDir, 0755); err != nil {
			return fmt.Errorf("failed to create base directory: %w", err)
		}

		for filePath, content := range conflict.BaseFileContents {
			// Create subdirectories if needed
			fullPath := filepath.Join(baseDir, filePath)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				return fmt.Errorf("failed to create directory for base file %s: %w", filePath, err)
			}
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				return fmt.Errorf("failed to write base file %s: %w", filePath, err)
			}
		}
	}

	return nil
}

// buildResolverPrompt creates the prompt for the resolver agent.
// The prompt focuses specifically on conflict resolution using three-way merge semantics.
func (r *Resolver) buildResolverPrompt(conflict *ConflictContext) string {
	prompt := fmt.Sprintf(`## Three-Way Merge Resolution

**You are resolving a git merge conflict using three-way merge semantics.**

### The Three Versions

1. **BASE** (.canopy/conflict/base/): The codebase when the original agent started.
   The patches in patch-*.patch are diffs relative to this state.
   Commit: %s

2. **OURS** (current HEAD): The codebase now, after other agents merged their work.
   This is what you see in the working directory.

3. **THEIRS** (.canopy/conflict/patch-*.patch): The changes the original agent made.
   These are expressed as diffs from BASE.

### Original Task (for context only)
**ID:** %s
**Title:** %s

%s

### What Happened

While the original agent worked on BASE, other changes were merged to HEAD.
The concurrent changes are shown in: .canopy/conflict/concurrent-changes.diff

These concurrent changes are VALID and MUST be preserved.

### Your Goal

Produce code that has BOTH:
1. All the concurrent changes that are already in HEAD (OURS)
2. The intent of the patches (THEIRS), adapted for the new context

**CRITICAL: DO NOT revert any code that exists in HEAD but not in the patches.**
The patches were created against BASE, not HEAD. Missing lines in patches
don't mean those lines should be removed - they mean those lines were
added concurrently and must stay.

### Resolution Process

1. Read the patches to understand what the agent INTENDED to change
2. Read .canopy/conflict/base/ to see original file states at BASE
3. Look at the current files (OURS/HEAD) to see concurrent changes
4. Apply the INTENT of the patches to the current state
5. Preserve all concurrent changes from OURS

### Conflict Files
- .canopy/conflict/patch-*.patch - The original patches (diffs from BASE)
- .canopy/conflict/base/<filepath> - Original file contents at BASE commit
- .canopy/conflict/concurrent-changes.diff - What changed BASE→HEAD (concurrent work)
- .canopy/conflict/errors.txt - Why the patches failed to apply
- .canopy/conflict/original-task.txt - Original task context

### Example Resolution

If BASE had:
    func foo() { return 1 }

And THEIRS (patch) changes it to:
    func foo() { return 2 }

But OURS (HEAD) has:
    func foo() { return 1 }
    func bar() { return 3 }  // Added by concurrent agent

The correct resolution is:
    func foo() { return 2 }  // Apply the patch's intent
    func bar() { return 3 }  // Keep concurrent addition

**DO NOT:**
- Reimplement the feature from scratch
- Make additional changes beyond what's in the patches
- Remove concurrent changes that aren't in the patches
- Skip changes because you think they're unnecessary

### Success Criteria
Your work is complete when:
- All changes from the patches have been applied (adapted for current HEAD)
- All concurrent changes in HEAD have been preserved
- You've made clean commits that can be merged
`, conflict.BaseCommit, conflict.TaskID, conflict.TaskTitle, conflict.TaskDescription)

	return prompt
}
