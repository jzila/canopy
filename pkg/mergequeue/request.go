// Package mergequeue provides an in-process merge queue for coordinating
// sequential merge operations across concurrent agents.
package mergequeue

import (
	"time"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
)

// MergeRequest represents a request to merge an agent's changes into main.
type MergeRequest struct {
	// Result is the agent execution result containing changes to merge.
	Result *agent.Result

	// Task is the beads task associated with this merge request.
	Task *beads.Task

	// Response is the channel where the merge result will be sent.
	// The caller must read from this channel to receive the result.
	Response chan *MergeResponse

	// EnqueuedAt is when this request was added to the queue.
	EnqueuedAt time.Time
}

// MergeResponse contains the outcome of a merge operation.
type MergeResponse struct {
	// Success indicates whether the merge completed successfully.
	Success bool

	// Error contains the error message if Success is false.
	Error string

	// CommitsApplied is the number of commits successfully applied.
	CommitsApplied int

	// HadConflict indicates whether a merge conflict was encountered.
	HadConflict bool

	// ResolverSpawned indicates whether a resolver agent was spawned
	// to handle conflicts.
	ResolverSpawned bool
}

// NewMergeRequest creates a new merge request with initialized fields.
func NewMergeRequest(result *agent.Result, task *beads.Task) *MergeRequest {
	return &MergeRequest{
		Result:     result,
		Task:       task,
		Response:   make(chan *MergeResponse, 1),
		EnqueuedAt: time.Now(),
	}
}
