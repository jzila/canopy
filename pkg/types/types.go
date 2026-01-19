// Package types provides shared type definitions used across canopy packages.
//
// This package consolidates types that were previously duplicated across pkg/ipc,
// pkg/daemon, and pkg/persistence packages. By defining canonical versions here,
// we ensure type safety across package boundaries and eliminate conversion overhead.
package types

// MergeStatus represents the current phase of merge processing for an agent's changes.
// This type is used consistently across IPC communication, daemon state tracking,
// and persistence storage.
type MergeStatus string

const (
	// MergeStatusNone indicates the agent has not yet entered the merge queue,
	// or has no changes to merge.
	MergeStatusNone MergeStatus = ""
	// MergeStatusPending indicates the agent is waiting in queue for a merge slot.
	MergeStatusPending MergeStatus = "pending"
	// MergeStatusAcquiring indicates the agent is attempting to acquire a merge slot.
	MergeStatusAcquiring MergeStatus = "acquiring"
	// MergeStatusMerging indicates the agent is actively applying patches/changes.
	MergeStatusMerging MergeStatus = "merging"
	// MergeStatusResolving indicates a resolver agent was spawned to handle conflicts.
	MergeStatusResolving MergeStatus = "resolving"
	// MergeStatusMerged indicates changes were successfully merged.
	MergeStatusMerged MergeStatus = "merged"
	// MergeStatusFailed indicates the merge failed.
	MergeStatusFailed MergeStatus = "failed"
	// MergeStatusResolved indicates the merge succeeded after conflict resolution
	// (used in persistence to distinguish from direct merges).
	MergeStatusResolved MergeStatus = "resolved"
	// MergeStatusSkipped indicates merge was skipped because there were no changes
	// to apply (e.g., resolver determined work was already done).
	MergeStatusSkipped MergeStatus = "skipped"
	// MergeStatusMergedNeedsRepair indicates the merge succeeded but validation
	// failed and automated repair attempts were exhausted. Manual intervention needed.
	MergeStatusMergedNeedsRepair MergeStatus = "merged_needs_repair"
)

// ModelUsage represents per-model token usage and cost.
// This is used to track detailed usage broken down by model (e.g., "claude-3-opus").
type ModelUsage struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	CostUSD                  float64 `json:"cost_usd"`
}

// TokenUsage tracks aggregate token consumption and cost for an agent execution.
// This provides a summary of all token usage including per-model breakdowns.
type TokenUsage struct {
	InputTokens              int                   `json:"input_tokens"`
	OutputTokens             int                   `json:"output_tokens"`
	CacheCreationInputTokens int                   `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int                   `json:"cache_read_input_tokens"`
	TotalTokens              int                   `json:"total_tokens"`
	CostUSD                  float64               `json:"cost_usd"`
	ModelUsage               map[string]ModelUsage `json:"model_usage,omitempty"`
}

// AgentResult contains execution metrics for an agent.
// This is sent over IPC when an agent completes (successfully or with failure).
type AgentResult struct {
	ExitCode                 int                   `json:"exit_code"`
	DurationSeconds          float64               `json:"duration_seconds"`
	DurationMS               int64                 `json:"duration_ms,omitempty"`
	DurationAPIMS            int64                 `json:"duration_api_ms,omitempty"`
	NumTurns                 int                   `json:"num_turns,omitempty"`
	InputTokens              int                   `json:"input_tokens,omitempty"`
	OutputTokens             int                   `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int                   `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int                   `json:"cache_read_input_tokens,omitempty"`
	CostUSD                  float64               `json:"cost_usd,omitempty"`
	FilesChanged             int                   `json:"files_changed"`
	CommitsCreated           int                   `json:"commits_created,omitempty"`
	ModelUsage               map[string]ModelUsage `json:"model_usage,omitempty"`
	ResultMessage            string                `json:"result_message,omitempty"`
	Stdout                   string                `json:"stdout,omitempty"`
	Stderr                   string                `json:"stderr,omitempty"`
}

// ToTokenUsage converts an AgentResult to TokenUsage for storage in AgentState.
// This extracts the token-related fields from a result.
func (r *AgentResult) ToTokenUsage() TokenUsage {
	return TokenUsage{
		InputTokens:              r.InputTokens,
		OutputTokens:             r.OutputTokens,
		CacheCreationInputTokens: r.CacheCreationInputTokens,
		CacheReadInputTokens:     r.CacheReadInputTokens,
		TotalTokens:              r.InputTokens + r.OutputTokens,
		CostUSD:                  r.CostUSD,
		ModelUsage:               r.ModelUsage,
	}
}

// ValidationStep represents the result of a single validation step (build, test, lint, etc.).
// Used consistently across IPC communication, daemon state, and persistence.
type ValidationStep struct {
	Name     string `json:"name"`             // Name of the validation step (e.g., "build", "test", "lint")
	Status   string `json:"status"`           // Status: "pending", "running", "passed", "failed", "skipped"
	Duration int64  `json:"duration_ms"`      // Duration of the step in milliseconds
	Output   string `json:"output,omitempty"` // Output or error message from the step
}
