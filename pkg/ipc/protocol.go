/*
Package ipc provides the inter-process communication protocol for canopy.

# Protocol Specification

The IPC protocol uses newline-delimited JSON over Unix domain sockets for
communication between canopy run clients and the canopy daemon.

## Message Format

All messages use an envelope structure:

	{
	    "type": "agent_start",               // Message type identifier
	    "timestamp": "2024-01-15T10:30:00Z", // ISO 8601 timestamp
	    "payload": { ... }                   // Type-specific payload
	}

## Size Limits

To prevent denial-of-service attacks from oversized messages:

  - MaxMessageSize: 1MB - Maximum total message size including envelope
  - MaxPayloadSize: 512KB - Maximum payload size

Messages exceeding these limits are dropped and logged.

## Message Types

Agent lifecycle:

  - agent_start: Agent begins execution
  - agent_output: Agent stdout/stderr output
  - agent_live_feed: Real-time streaming events
  - agent_commit: Agent created a git commit
  - agent_merge_status: Merge queue status update
  - agent_done: Agent completed successfully
  - agent_fail: Agent failed

Task lifecycle:

  - task_updated: Task status changed

Run lifecycle:

  - run_started: Canopy run began
  - run_completed: Canopy run finished

## Transport

  - Protocol: Unix domain socket
  - Encoding: UTF-8 JSON
  - Framing: Newline-delimited (each message ends with \n)
  - Permissions: Socket created with 0600 (owner-only)

## Error Handling

  - Invalid JSON: Logged and skipped, connection continues
  - Oversized messages: Logged and dropped, connection continues
  - Unknown message types: Logged as warning, silently dropped
*/
package ipc

import (
	"encoding/json"
	"time"

	"github.com/jzila/canopy/pkg/types"
)

// Message size limits for DoS protection
const (
	// MaxMessageSize is the maximum size of a single IPC message (1MB)
	MaxMessageSize = 1 << 20 // 1MB
	// MaxPayloadSize is the maximum size of the message payload (512KB)
	MaxPayloadSize = 512 << 10 // 512KB
)

// MessageType identifies the type of IPC message
type MessageType string

const (
	// Agent lifecycle events
	MessageTypeAgentStart       MessageType = "agent_start"
	MessageTypeAgentOutput      MessageType = "agent_output"
	MessageTypeAgentLiveFeed    MessageType = "agent_live_feed"
	MessageTypeAgentCommit      MessageType = "agent_commit"
	MessageTypeAgentMergeStatus MessageType = "agent_merge_status"
	MessageTypeAgentDone        MessageType = "agent_done"
	MessageTypeAgentFail        MessageType = "agent_fail"

	// Task lifecycle events
	MessageTypeTaskUpdated MessageType = "task_updated"

	// Run lifecycle events
	MessageTypeRunStarted   MessageType = "run_started"
	MessageTypeRunCompleted MessageType = "run_completed"

	// Orchestrator status events
	MessageTypeOrchPauseStatus MessageType = "orch_pause_status"
)

// Message is the top-level IPC message envelope
type Message struct {
	// Type identifies the message type
	Type MessageType `json:"type"`
	// Timestamp is when the message was created
	Timestamp time.Time `json:"timestamp"`
	// Payload contains the type-specific message data
	Payload interface{} `json:"payload"`
}

// RawMessage is used for initial parsing to validate payload size
// before full deserialization
type RawMessage struct {
	Type      MessageType     `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// AgentStartPayload is sent when an agent begins execution
type AgentStartPayload struct {
	AgentID         string `json:"agent_id"`
	RunID           string `json:"run_id,omitempty"`            // Run ID this agent belongs to
	TaskID          string `json:"task_id"`
	TaskTitle       string `json:"task_title"`
	TaskDescription string `json:"task_description,omitempty"` // Task description for display
	ParentAgentID   string `json:"parent_agent_id,omitempty"`   // ID of parent agent if spawned by another agent
	RepoID          string `json:"repo_id,omitempty"`           // Repository ID for tracking
}

// AgentOutputPayload is sent when an agent produces output
type AgentOutputPayload struct {
	AgentID string `json:"agent_id"`
	Output  string `json:"output"`
	IsError bool   `json:"is_error"`
}

// AgentLiveFeedPayload is sent for real-time streaming events from agents
type AgentLiveFeedPayload struct {
	AgentID   string                 `json:"agent_id"`
	EventType string                 `json:"event_type"` // "tool_use", "file_change", "text"
	Data      map[string]interface{} `json:"data"`
}

// AgentCommitPayload is sent when an agent creates a git commit
type AgentCommitPayload struct {
	AgentID      string   `json:"agent_id"`
	Hash         string   `json:"hash"`           // Full commit hash
	ShortHash    string   `json:"short_hash"`     // Short (7-char) commit hash
	Message      string   `json:"message"`        // Commit message (first line)
	Author       string   `json:"author"`         // Author name
	AuthorEmail  string   `json:"author_email"`   // Author email
	Timestamp    string   `json:"timestamp"`      // ISO 8601 timestamp
	FilesChanged []string `json:"files_changed"`  // List of files modified in this commit
}

// MergeStatus is an alias to types.MergeStatus for backwards compatibility.
// New code should import types.MergeStatus directly.
type MergeStatus = types.MergeStatus

// MergeStatus constants - aliases to types package for backwards compatibility.
const (
	MergeStatusNone      = types.MergeStatusNone
	MergeStatusPending   = types.MergeStatusPending
	MergeStatusAcquiring = types.MergeStatusAcquiring
	MergeStatusMerging   = types.MergeStatusMerging
	MergeStatusResolving = types.MergeStatusResolving
	MergeStatusMerged    = types.MergeStatusMerged
	MergeStatusFailed    = types.MergeStatusFailed
	MergeStatusSkipped   = types.MergeStatusSkipped
)

// ValidationStep represents the result of a single validation step
type ValidationStep struct {
	Name     string `json:"name"`               // Name of the validation step (e.g., "build", "test", "lint")
	Status   string `json:"status"`             // Status: "pending", "running", "passed", "failed", "skipped"
	Duration int64  `json:"duration_ms"`        // Duration of the step in milliseconds
	Output   string `json:"output,omitempty"`   // Output or error message from the step
}

// AgentMergeStatusPayload is sent when an agent's merge status changes
type AgentMergeStatusPayload struct {
	AgentID         string      `json:"agent_id"`
	MergeStatus     MergeStatus `json:"merge_status"`
	QueuePos        int         `json:"queue_pos,omitempty"`         // Position in wait queue (0 = not waiting)
	Error           string      `json:"error,omitempty"`             // Error message if merge failed
	CommitsApplied  int         `json:"commits_applied,omitempty"`   // Number of commits applied (for final status)
	HadConflict     bool        `json:"had_conflict,omitempty"`      // Whether merge had conflicts
	ResolverSpawned bool        `json:"resolver_spawned,omitempty"`  // Whether resolver was spawned

	// Validation results
	ValidationStatus   string           `json:"validation_status,omitempty"`      // Overall status: "pending", "running", "passed", "failed", "skipped"
	ValidationSteps    []ValidationStep `json:"validation_steps,omitempty"`       // Results of individual validation steps
	ValidationDuration int64            `json:"validation_duration_ms,omitempty"` // Total validation duration in milliseconds
	ValidationError    string           `json:"validation_error,omitempty"`       // Error message if validation failed
}

// AgentResult is an alias to types.AgentResult for backwards compatibility.
// New code should import types.AgentResult directly.
type AgentResult = types.AgentResult

// ModelUsage is an alias to types.ModelUsage for backwards compatibility.
// New code should import types.ModelUsage directly.
type ModelUsage = types.ModelUsage

// AgentDonePayload is sent when an agent completes successfully
type AgentDonePayload struct {
	AgentID       string      `json:"agent_id"`
	ParentAgentID string      `json:"parent_agent_id,omitempty"` // ID of parent agent if spawned by another agent
	Result        AgentResult `json:"result"`
}

// AgentFailPayload is sent when an agent fails
type AgentFailPayload struct {
	AgentID       string      `json:"agent_id"`
	ParentAgentID string      `json:"parent_agent_id,omitempty"` // ID of parent agent if spawned by another agent
	Error         string      `json:"error"`
	Result        AgentResult `json:"result"`
}

// TaskUpdatedPayload is sent when a task status changes
type TaskUpdatedPayload struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Status  string `json:"status"`
	AgentID string `json:"agent_id,omitempty"`
	RepoID  string `json:"repo_id,omitempty"` // Repository ID for tracking
}

// RunStartedPayload is sent when a canopy run begins
type RunStartedPayload struct {
	RunID     string `json:"run_id"`
	TaskCount int    `json:"task_count"`
	RepoID    string `json:"repo_id,omitempty"`   // Repository UUID
	RepoPath  string `json:"repo_path,omitempty"` // Absolute path to repository
	RepoName  string `json:"repo_name,omitempty"` // Repository name (basename of path)
}

// RunStats contains aggregate statistics for a run
type RunStats struct {
	TotalTasks                   int     `json:"total_tasks"`
	SucceededTasks               int     `json:"succeeded_tasks"`
	FailedTasks                  int     `json:"failed_tasks"`
	TotalDuration                float64 `json:"total_duration_seconds"`
	TotalInputTokens             int     `json:"total_input_tokens"`
	TotalOutputTokens            int     `json:"total_output_tokens"`
	TotalCacheCreationInputToken int     `json:"total_cache_creation_input_tokens"`
	TotalCacheReadInputTokens    int     `json:"total_cache_read_input_tokens"`
	TotalCostUSD                 float64 `json:"total_cost_usd"`
	TotalTurns                   int     `json:"total_turns"`
	FilesChanged                 int     `json:"files_changed"`
	GitCommits                   int     `json:"git_commits"`
	ConflictsResolved            int     `json:"conflicts_resolved"`
}

// RunCompletedPayload is sent when a canopy run completes
type RunCompletedPayload struct {
	RunID string   `json:"run_id"`
	Stats RunStats `json:"stats"`
}

// OrchPauseStatusPayload is sent when the orchestrator pause state changes
type OrchPauseStatusPayload struct {
	IsPaused           bool   `json:"is_paused"`            // Whether the orchestrator is paused (by any source)
	IsPausedByUser     bool   `json:"is_paused_by_user"`    // Whether paused by user request
	IsPausedByResolver bool   `json:"is_paused_by_resolver"` // Whether paused for conflict resolution
	PauseState         string `json:"pause_state"`          // Detailed state: "running", "paused_user", "paused_resolver", "paused_both"
}
