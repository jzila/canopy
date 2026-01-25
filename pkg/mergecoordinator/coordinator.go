// Package mergecoordinator provides centralized coordination of merge operations.
// It encapsulates the merge queue, processor, merger, and resolver into a single
// component that the orchestrator can delegate to.
package mergecoordinator

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/merge"
	"github.com/jzila/canopy/pkg/mergequeue"
	"github.com/jzila/canopy/pkg/resolver"
	"github.com/jzila/canopy/pkg/sandbox"
)

// Config holds configuration for the MergeCoordinator.
type Config struct {
	// WorkDir is the working directory for the repository.
	WorkDir string

	// OutputDir is where merged changes are written.
	OutputDir string

	// TempDir is for temporary files during merge operations.
	TempDir string

	// Concurrency determines the merge queue buffer size.
	Concurrency int

	// Verbose enables detailed logging.
	Verbose bool

	// UseBwrap enables bubblewrap sandboxing for resolver agents.
	UseBwrap bool

	// SandboxConfig is the sandbox configuration for resolver agents.
	SandboxConfig *sandbox.SandboxConfig

	// ResolverTimeout is the timeout for resolver agent operations.
	// If 0, uses the default timeout of 10 minutes.
	ResolverTimeout time.Duration
}

// MergeCoordinator coordinates all merge operations including:
// - Sequential merge queue processing
// - Conflict resolution via resolver agents
// - IPC status updates
// - Beads task status updates
type MergeCoordinator struct {
	config      *Config
	queue       *mergequeue.Queue
	processor   *mergequeue.Processor
	merger      *merge.SequentialMerger
	resolver    *resolver.Resolver
	beadsClient beads.BeadsClient
	ipcClient   *ipc.Client
	repoID      string

	// agentIDMap maps taskID -> agentID for parent-child tracking
	agentIDMap sync.Map

	// taskCache maps taskID -> *beads.Task for passing task to merge queue
	taskCache sync.Map

	// cleanupCallback is called after merge completes to cleanup overlay
	cleanupCallback func(ctx context.Context, result *agent.Result)
}

// New creates a new MergeCoordinator.
func New(config *Config, beadsClient beads.BeadsClient) (*MergeCoordinator, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if beadsClient == nil {
		return nil, fmt.Errorf("beadsClient is required")
	}

	// Create merger
	merger := merge.NewSequentialMerger(config.OutputDir, config.TempDir, config.Verbose)

	// Create resolver for handling merge conflicts
	resolverInst := resolver.New(&resolver.Config{
		WorkDir:       config.WorkDir,
		TempDir:       config.TempDir,
		Verbose:       config.Verbose,
		UseBwrap:      config.UseBwrap,
		SandboxConfig: config.SandboxConfig,
	})

	// Initialize merge queue with buffer size equal to concurrency
	bufferSize := config.Concurrency
	if bufferSize <= 0 {
		bufferSize = 4 // default concurrency
	}
	queue := mergequeue.NewQueue(bufferSize)

	// Initialize merge processor
	// Note: ipcClient is set via SetIPCClient after construction
	processor := mergequeue.NewProcessor(
		queue,
		merger,
		resolverInst,
		beadsClient,
		config.OutputDir,
		nil, // ipcClient set later via SetIPCClient
		config.Verbose,
		config.ResolverTimeout, // Use configured resolver timeout (0 = default 10 minutes)
	)

	return &MergeCoordinator{
		config:      config,
		queue:       queue,
		processor:   processor,
		merger:      merger,
		resolver:    resolverInst,
		beadsClient: beadsClient,
	}, nil
}

// Start begins the merge processor goroutine.
// It processes merge requests from the queue until context is cancelled.
func (mc *MergeCoordinator) Start(ctx context.Context) {
	go mc.processor.Start(ctx)
}

// SetIPCClient sets the IPC client for merge status updates.
func (mc *MergeCoordinator) SetIPCClient(client *ipc.Client) {
	mc.ipcClient = client
	if mc.processor != nil {
		mc.processor.SetIPCClient(client)
	}
	if mc.resolver != nil {
		mc.resolver.SetIPCClient(client)
	}
}

// SetRepoID sets the repository ID for IPC tracking.
func (mc *MergeCoordinator) SetRepoID(repoID string) {
	mc.repoID = repoID
	if mc.processor != nil {
		mc.processor.SetRepoID(repoID)
	}
	if mc.resolver != nil {
		mc.resolver.SetRepoID(repoID)
	}
}

// SetRunID sets the run ID for unique agent ID generation.
// This ensures agent IDs are unique per run, even when retrying tasks.
func (mc *MergeCoordinator) SetRunID(runID string) {
	mc.processor.SetRunID(runID)
	if mc.resolver != nil {
		mc.resolver.SetRunID(runID)
	}
}

// SetCleanupCallback sets a callback to cleanup overlays after merge.
func (mc *MergeCoordinator) SetCleanupCallback(callback func(ctx context.Context, result *agent.Result)) {
	mc.cleanupCallback = callback
}

// SetMergeStatusCallback sets a callback for merge status events.
// This is used when running in daemon mode where IPC is not available.
func (mc *MergeCoordinator) SetMergeStatusCallback(callback mergequeue.MergeStatusCallback) {
	if mc.processor != nil {
		mc.processor.SetMergeStatusCallback(callback)
	}
}

// SetCommitCallback sets a callback for commit events.
// This is used when running in daemon mode where IPC is not available.
func (mc *MergeCoordinator) SetCommitCallback(callback mergequeue.CommitCallback) {
	if mc.processor != nil {
		mc.processor.SetCommitCallback(callback)
	}
}

// SetAgentCallback sets a callback for agent lifecycle events (start, done, fail).
// This is used when running in daemon mode where IPC is not available.
// The callback is propagated to the resolver for tracking resolver agent events.
// The callback receives events as interface{} which callers should type-assert to resolver.AgentEvent.
func (mc *MergeCoordinator) SetAgentCallback(callback func(event interface{})) {
	if mc.resolver != nil {
		// Wrap the interface{} callback to match resolver.AgentCallback signature
		mc.resolver.SetAgentCallback(func(event resolver.AgentEvent) {
			callback(event)
		})
	}
}

// SetAgentID records the agentID for a taskID, enabling parent-child tracking.
func (mc *MergeCoordinator) SetAgentID(taskID, agentID string) {
	mc.agentIDMap.Store(taskID, agentID)
}

// GetAgentID retrieves the agentID for a taskID.
func (mc *MergeCoordinator) GetAgentID(taskID string) string {
	if val, ok := mc.agentIDMap.Load(taskID); ok {
		return val.(string)
	}
	return ""
}

// CacheTask stores a task for later retrieval during merge.
func (mc *MergeCoordinator) CacheTask(taskID string, task *beads.Task) {
	mc.taskCache.Store(taskID, task)
}

// EnqueueMerge adds a completed agent result to the merge queue.
// This blocks until the merge completes and returns the merge response.
// Returns nil if the queue is full or closed.
func (mc *MergeCoordinator) EnqueueMerge(ctx context.Context, result *agent.Result) *mergequeue.MergeResponse {
	// Retrieve task from cache
	var task *beads.Task
	if cached, ok := mc.taskCache.Load(result.TaskID); ok {
		task = cached.(*beads.Task)
		mc.taskCache.Delete(result.TaskID) // Clean up cache
	} else {
		// Fallback: fetch from beads if not cached
		if fetched, err := mc.beadsClient.Show(ctx, result.TaskID); err == nil {
			task = fetched
		}
	}

	// Create merge request and enqueue
	req := mergequeue.NewMergeRequest(result, task)
	if !mc.queue.Enqueue(req) {
		// Queue is full or closed
		if mc.config.Verbose {
			fmt.Fprintf(os.Stderr, "warning: merge queue full for task %s\n", result.TaskID)
		}
		mc.cleanup(ctx, result)
		return nil
	}

	// Block waiting for merge response
	resp := <-req.Response

	// Cleanup overlay after merge completes
	mc.cleanup(ctx, result)

	return resp
}

// HandleFailure handles a failed task - cleans up and marks failed in beads.
// Failed tasks don't need merge since there are no changes to merge.
// If the task failed because it's waiting for user input (InputBlocked), it's
// marked as needs-input instead of failed to prevent scheduler re-pickup.
func (mc *MergeCoordinator) HandleFailure(ctx context.Context, taskID string, result *agent.Result, errMsg string) {
	// Get task title from cache before cleanup for IPC notification
	var taskTitle string
	if cached, ok := mc.taskCache.Load(taskID); ok {
		task := cached.(*beads.Task)
		taskTitle = task.Title
	}

	// Clean up task cache if present
	mc.taskCache.Delete(taskID)

	// Clean up overlay for failed task
	mc.cleanup(ctx, result)

	// Get agent ID for this task (needed for merge status)
	agentID := mc.GetAgentID(taskID)

	// Check if this is an input-blocked failure (agent paused waiting for user input)
	if result != nil && result.InputBlocked {
		// Get session ID for resume support
		sessionID := ""
		if result.Output != nil {
			sessionID = result.Output.SessionID
		}

		// Mark task as needs-input instead of failed
		if err := mc.beadsClient.NeedsInput(ctx, taskID, sessionID, errMsg); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed to mark task %s as needs-input: %v\n", taskID, err)
		}

		// Send task status update via IPC with needs-input status
		mc.SendTaskUpdated(taskID, taskTitle, "needs-input")
		return
	}

	// Mark task as failed in beads
	if err := mc.beadsClient.Fail(ctx, taskID, errMsg); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to mark task %s as failed: %v\n", taskID, err)
	}

	// Send merge status as failed for agents that never entered the merge queue
	// This ensures the UI can display the Details button (which requires merge_status)
	mc.sendMergeStatusFailed(agentID, errMsg)

	// Send task status update via IPC
	mc.SendTaskUpdated(taskID, taskTitle, "failed")
}

// sendMergeStatusFailed sends a merge status failed event via IPC if client is connected.
// This is used for agents that fail before entering the merge queue.
func (mc *MergeCoordinator) sendMergeStatusFailed(agentID, errMsg string) {
	if mc.ipcClient == nil || agentID == "" {
		return
	}

	if err := mc.ipcClient.SendAgentMergeStatusFull(agentID, ipc.MergeStatusFailed, 0, errMsg, 0, false, false); err != nil && mc.config.Verbose {
		fmt.Fprintf(os.Stderr, "warning: failed to send merge status for %s: %v\n", agentID, err)
	}
}

// SendTaskUpdated sends a task status update via IPC if client is connected.
func (mc *MergeCoordinator) SendTaskUpdated(taskID, title, status string) {
	if mc.ipcClient == nil {
		return
	}

	// Get agent ID for this task
	agentID := mc.GetAgentID(taskID)

	if err := mc.ipcClient.SendTaskUpdated(taskID, title, status, agentID, mc.repoID); err != nil && mc.config.Verbose {
		fmt.Fprintf(os.Stderr, "warning: failed to send task updated for %s: %v\n", taskID, err)
	}
}

// cleanup runs the cleanup callback if set.
func (mc *MergeCoordinator) cleanup(ctx context.Context, result *agent.Result) {
	if mc.cleanupCallback != nil {
		mc.cleanupCallback(ctx, result)
	}
}

// Queue returns the underlying merge queue for advanced operations.
func (mc *MergeCoordinator) Queue() *mergequeue.Queue {
	return mc.queue
}

// Resolver returns the underlying resolver for advanced operations.
func (mc *MergeCoordinator) Resolver() *resolver.Resolver {
	return mc.resolver
}
