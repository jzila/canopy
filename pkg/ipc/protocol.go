package ipc

import "time"

// MessageType identifies the type of IPC message
type MessageType string

const (
	// Agent lifecycle events
	MessageTypeAgentStart     MessageType = "agent_start"
	MessageTypeAgentOutput    MessageType = "agent_output"
	MessageTypeAgentLiveFeed  MessageType = "agent_live_feed"
	MessageTypeAgentCommit    MessageType = "agent_commit"
	MessageTypeAgentDone      MessageType = "agent_done"
	MessageTypeAgentFail      MessageType = "agent_fail"

	// Run lifecycle events
	MessageTypeRunStarted   MessageType = "run_started"
	MessageTypeRunCompleted MessageType = "run_completed"
)

// Message is the top-level IPC message envelope
type Message struct {
	Type      MessageType `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

// AgentStartPayload is sent when an agent begins execution
type AgentStartPayload struct {
	AgentID   string `json:"agent_id"`
	TaskID    string `json:"task_id"`
	TaskTitle string `json:"task_title"`
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
	AgentID string      `json:"agent_id"`
	Result  AgentResult `json:"result"`
}

// AgentFailPayload is sent when an agent fails
type AgentFailPayload struct {
	AgentID string      `json:"agent_id"`
	Error   string      `json:"error"`
	Result  AgentResult `json:"result"`
}

// RunStartedPayload is sent when a canopy run begins
type RunStartedPayload struct {
	RunID     string `json:"run_id"`
	TaskCount int    `json:"task_count"`
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
	ConflictsResolved            int     `json:"conflicts_resolved"`
}

// RunCompletedPayload is sent when a canopy run completes
type RunCompletedPayload struct {
	RunID string   `json:"run_id"`
	Stats RunStats `json:"stats"`
}
