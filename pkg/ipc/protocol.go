/*
Package ipc provides the inter-process communication protocol for canopy.

# Protocol Specification

The IPC protocol uses newline-delimited JSON over Unix domain sockets for
communication between canopy run clients and the canopy daemon.

## Message Format

All messages use a versioned envelope structure:

	{
	    "v": 2,                              // Protocol version (optional in v1)
	    "type": "agent_start",               // Message type identifier
	    "timestamp": "2024-01-15T10:30:00Z", // ISO 8601 timestamp
	    "payload": { ... }                   // Type-specific payload
	}

## Protocol Versions

  - Version 1 (v1): Legacy unversioned protocol. Messages without a "v" field
    are treated as v1. Supported for backwards compatibility.
  - Version 2 (v2): Current protocol with explicit version field, size limits,
    and DoS protection.

## Size Limits

To prevent denial-of-service attacks from oversized messages:

  - MaxMessageSize: 1MB - Maximum total message size including envelope
  - MaxPayloadSize: 512KB - Maximum payload size

Messages exceeding these limits are dropped and logged.

## Backwards Compatibility

The server accepts both v1 (unversioned) and v2 messages:

  - v1 messages: No "v" field, processed normally for compatibility
  - v2 messages: Include "v": 2, subject to size limit enforcement

Clients should always send v2 messages with the version field set.

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
)

// Protocol version constants
const (
	// ProtocolVersion1 is the legacy unversioned protocol (implicit)
	ProtocolVersion1 = 1
	// ProtocolVersion2 is the current versioned protocol with envelope
	ProtocolVersion2 = 2
	// CurrentProtocolVersion is the version used by this implementation
	CurrentProtocolVersion = ProtocolVersion2
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
)

// Message is the top-level IPC message envelope
// Version 2+ messages include the Version field for protocol negotiation
type Message struct {
	// Version is the protocol version (omitted in v1 messages for backwards compatibility)
	Version int `json:"v,omitempty"`
	// Type identifies the message type
	Type MessageType `json:"type"`
	// Timestamp is when the message was created
	Timestamp time.Time `json:"timestamp"`
	// Payload contains the type-specific message data
	Payload interface{} `json:"payload"`
}

// RawMessage is used for initial parsing to detect protocol version
// and validate message size before full deserialization
type RawMessage struct {
	Version   int             `json:"v,omitempty"`
	Type      MessageType     `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// ProtocolVersion returns the protocol version of the message
// Returns ProtocolVersion1 if no version field is present (legacy messages)
func (m *Message) ProtocolVersion() int {
	if m.Version == 0 {
		return ProtocolVersion1
	}
	return m.Version
}

// ProtocolVersion returns the protocol version of the raw message
// Returns ProtocolVersion1 if no version field is present (legacy messages)
func (r *RawMessage) ProtocolVersion() int {
	if r.Version == 0 {
		return ProtocolVersion1
	}
	return r.Version
}

// AgentStartPayload is sent when an agent begins execution
type AgentStartPayload struct {
	AgentID       string `json:"agent_id"`
	TaskID        string `json:"task_id"`
	TaskTitle     string `json:"task_title"`
	ParentAgentID string `json:"parent_agent_id,omitempty"` // ID of parent agent if spawned by another agent
	RepoID        string `json:"repo_id,omitempty"`         // Repository ID for tracking
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

// MergeStatus represents the current phase of merge processing
type MergeStatus string

const (
	MergeStatusPending   MergeStatus = "pending"   // Waiting in queue for merge slot
	MergeStatusAcquiring MergeStatus = "acquiring" // Attempting to acquire merge slot
	MergeStatusMerging   MergeStatus = "merging"   // Applying patches/changes
	MergeStatusResolving MergeStatus = "resolving" // Spawned resolver for conflicts
	MergeStatusMerged    MergeStatus = "merged"    // Successfully merged
	MergeStatusFailed    MergeStatus = "failed"    // Merge failed
)

// AgentMergeStatusPayload is sent when an agent's merge status changes
type AgentMergeStatusPayload struct {
	AgentID         string      `json:"agent_id"`
	MergeStatus     MergeStatus `json:"merge_status"`
	QueuePos        int         `json:"queue_pos,omitempty"`         // Position in wait queue (0 = not waiting)
	Error           string      `json:"error,omitempty"`             // Error message if merge failed
	CommitsApplied  int         `json:"commits_applied,omitempty"`   // Number of commits applied (for final status)
	HadConflict     bool        `json:"had_conflict,omitempty"`      // Whether merge had conflicts
	ResolverSpawned bool        `json:"resolver_spawned,omitempty"`  // Whether resolver was spawned
}

// AgentResult contains execution metrics for an agent
type AgentResult struct {
	ExitCode                int                     `json:"exit_code"`
	DurationSeconds         float64                 `json:"duration_seconds"`
	DurationMS              int64                   `json:"duration_ms,omitempty"`
	DurationAPIMS           int64                   `json:"duration_api_ms,omitempty"`
	NumTurns                int                     `json:"num_turns,omitempty"`
	InputTokens             int                     `json:"input_tokens,omitempty"`
	OutputTokens            int                     `json:"output_tokens,omitempty"`
	CacheCreationInputToken int                     `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens    int                     `json:"cache_read_input_tokens,omitempty"`
	CostUSD                 float64                 `json:"cost_usd,omitempty"`
	FilesChanged            int                     `json:"files_changed"`
	CommitsCreated          int                     `json:"commits_created,omitempty"`
	ModelUsage              map[string]ModelUsage   `json:"model_usage,omitempty"`
	ResultMessage           string                  `json:"result_message,omitempty"`
	Stdout                  string                  `json:"stdout,omitempty"`
	Stderr                  string                  `json:"stderr,omitempty"`
}

// ModelUsage represents per-model token usage and cost
type ModelUsage struct {
	InputTokens             int     `json:"input_tokens"`
	OutputTokens            int     `json:"output_tokens"`
	CacheReadInputTokens    int     `json:"cache_read_input_tokens"`
	CacheCreationInputToken int     `json:"cache_creation_input_tokens"`
	CostUSD                 float64 `json:"cost_usd"`
}

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
