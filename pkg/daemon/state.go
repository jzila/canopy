package daemon

import (
	"sync"
	"time"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/lifecycle"
	"github.com/jzila/canopy/pkg/logging"
	"github.com/jzila/canopy/pkg/metrics"
	"github.com/jzila/canopy/pkg/persistence"
	"github.com/jzila/canopy/pkg/types"
)

// AgentStatus is an alias to types.AgentStatus for backwards compatibility.
// New code should import types.AgentStatus directly.
type AgentStatus = types.AgentStatus

// AgentStatus constants - aliases to types package for backwards compatibility.
const (
	AgentStatusStarting  = types.AgentStatusStarting
	AgentStatusRunning   = types.AgentStatusRunning
	AgentStatusCompleted = types.AgentStatusCompleted
	AgentStatusFailed    = types.AgentStatusFailed
	AgentStatusTimedOut  = types.AgentStatusTimedOut
	AgentStatusCancelled = types.AgentStatusCancelled
)

// OutputBuffer stores stdout/stderr output from an agent
type OutputBuffer struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	mu     sync.RWMutex
}

// LiveFeedEventType defines the type of live feed event
type LiveFeedEventType string

const (
	// LiveFeedEventToolUse represents a tool invocation by the agent
	LiveFeedEventToolUse LiveFeedEventType = "tool_use"
	// LiveFeedEventText represents text output from the agent
	LiveFeedEventText LiveFeedEventType = "text"
	// LiveFeedEventFileChange represents a file modification
	LiveFeedEventFileChange LiveFeedEventType = "file_change"
	// LiveFeedEventAgentCompleted represents agent completion
	LiveFeedEventAgentCompleted LiveFeedEventType = "agent_completed"
	// LiveFeedEventError represents an error event
	LiveFeedEventError LiveFeedEventType = "error"
	// LiveFeedEventToolResult represents the result of a tool execution
	LiveFeedEventToolResult LiveFeedEventType = "tool_result"
)

// LiveFeedEventData is an interface for type-safe event data
// Use type assertions or the helper methods to access typed data
type LiveFeedEventData interface {
	eventData() // marker method
}

// ToolUseEventData contains data for a tool_use event
type ToolUseEventData struct {
	Tool     string `json:"tool"`               // Tool name (e.g., "Read", "Bash", "Grep")
	FilePath string `json:"file_path,omitempty"` // For file tools (Read, Write, Edit)
	Command  string `json:"command,omitempty"`   // For Bash tool
	Pattern  string `json:"pattern,omitempty"`   // For Grep/Glob tools
}

func (ToolUseEventData) eventData() {}

// TextEventData contains data for a text event
type TextEventData struct {
	Text       string `json:"text"`                  // The text content
	IsHistoric bool   `json:"is_historic,omitempty"` // True if reconstructed from historical data
}

func (TextEventData) eventData() {}

// FileChangeEventData contains data for a file_change event
type FileChangeEventData struct {
	Action   string `json:"action"`    // "created", "modified", "deleted"
	FilePath string `json:"file_path"` // Path to the changed file
}

func (FileChangeEventData) eventData() {}

// AgentCompletedEventData contains data for an agent_completed event
type AgentCompletedEventData struct {
	FilesChanged   int    `json:"files_changed"`             // Number of files modified
	CommitsCreated int    `json:"commits_created"`           // Number of git commits
	Error          string `json:"error,omitempty"`           // Error message if failed
	ResultMessage  string `json:"result_message,omitempty"`  // Final result from agent
	IsHistoric     bool   `json:"is_historic,omitempty"`     // True if reconstructed from historical data
}

func (AgentCompletedEventData) eventData() {}

// ErrorEventData contains data for an error event
type ErrorEventData struct {
	Error   string `json:"error"`            // Error message
	Code    string `json:"code,omitempty"`   // Error code if available
	Details string `json:"details,omitempty"` // Additional error details
}

func (ErrorEventData) eventData() {}

// ToolResultEventData contains data for a tool_result event
type ToolResultEventData struct {
	Tool    string `json:"tool"`              // Tool name
	Success bool   `json:"success"`           // Whether the tool succeeded
	Output  string `json:"output,omitempty"`  // Tool output (truncated)
	Error   string `json:"error,omitempty"`   // Error message if failed
}

func (ToolResultEventData) eventData() {}

// LiveFeedEvent represents a real-time event from the Claude API
// EventType determines which typed data struct is stored in Data
type LiveFeedEvent struct {
	EventType LiveFeedEventType `json:"event_type"` // "tool_use", "text", "file_change", etc.
	Data      LiveFeedEventData `json:"-"`          // Type-safe event data (not directly serialized)
	// RawData is used for JSON serialization to maintain backwards compatibility
	RawData map[string]interface{} `json:"data"`
}

// NewToolUseEvent creates a new tool_use event with typed data
func NewToolUseEvent(tool string, filePath, command, pattern string) LiveFeedEvent {
	data := ToolUseEventData{
		Tool:     tool,
		FilePath: filePath,
		Command:  command,
		Pattern:  pattern,
	}
	return LiveFeedEvent{
		EventType: LiveFeedEventToolUse,
		Data:      data,
		RawData:   toolUseToMap(data),
	}
}

// NewTextEvent creates a new text event with typed data
func NewTextEvent(text string, isHistoric bool) LiveFeedEvent {
	data := TextEventData{
		Text:       text,
		IsHistoric: isHistoric,
	}
	return LiveFeedEvent{
		EventType: LiveFeedEventText,
		Data:      data,
		RawData:   textToMap(data),
	}
}

// NewFileChangeEvent creates a new file_change event with typed data
func NewFileChangeEvent(action, filePath string) LiveFeedEvent {
	data := FileChangeEventData{
		Action:   action,
		FilePath: filePath,
	}
	return LiveFeedEvent{
		EventType: LiveFeedEventFileChange,
		Data:      data,
		RawData:   fileChangeToMap(data),
	}
}

// NewAgentCompletedEvent creates a new agent_completed event with typed data
func NewAgentCompletedEvent(filesChanged, commitsCreated int, errMsg, resultMsg string, isHistoric bool) LiveFeedEvent {
	data := AgentCompletedEventData{
		FilesChanged:   filesChanged,
		CommitsCreated: commitsCreated,
		Error:          errMsg,
		ResultMessage:  resultMsg,
		IsHistoric:     isHistoric,
	}
	return LiveFeedEvent{
		EventType: LiveFeedEventAgentCompleted,
		Data:      data,
		RawData:   agentCompletedToMap(data),
	}
}

// Helper functions to convert typed data to map for JSON serialization

func toolUseToMap(d ToolUseEventData) map[string]interface{} {
	m := map[string]interface{}{"tool": d.Tool}
	if d.FilePath != "" {
		m["file_path"] = d.FilePath
	}
	if d.Command != "" {
		m["command"] = d.Command
	}
	if d.Pattern != "" {
		m["pattern"] = d.Pattern
	}
	return m
}

func textToMap(d TextEventData) map[string]interface{} {
	m := map[string]interface{}{"text": d.Text}
	if d.IsHistoric {
		m["is_historic"] = d.IsHistoric
	}
	return m
}

func fileChangeToMap(d FileChangeEventData) map[string]interface{} {
	return map[string]interface{}{
		"action":    d.Action,
		"file_path": d.FilePath,
	}
}

func agentCompletedToMap(d AgentCompletedEventData) map[string]interface{} {
	m := map[string]interface{}{
		"files_changed":   d.FilesChanged,
		"commits_created": d.CommitsCreated,
	}
	if d.Error != "" {
		m["error"] = d.Error
	}
	if d.ResultMessage != "" {
		m["result_message"] = d.ResultMessage
	}
	if d.IsHistoric {
		m["is_historic"] = d.IsHistoric
	}
	return m
}

// GetToolUseData returns the typed tool_use data if this is a tool_use event
func (e LiveFeedEvent) GetToolUseData() (ToolUseEventData, bool) {
	if d, ok := e.Data.(ToolUseEventData); ok {
		return d, true
	}
	// Fallback: parse from RawData for events created from JSON
	if e.EventType == LiveFeedEventToolUse && e.RawData != nil {
		return ToolUseEventData{
			Tool:     getStringFromMap(e.RawData, "tool"),
			FilePath: getStringFromMap(e.RawData, "file_path"),
			Command:  getStringFromMap(e.RawData, "command"),
			Pattern:  getStringFromMap(e.RawData, "pattern"),
		}, true
	}
	return ToolUseEventData{}, false
}

// GetTextData returns the typed text data if this is a text event
func (e LiveFeedEvent) GetTextData() (TextEventData, bool) {
	if d, ok := e.Data.(TextEventData); ok {
		return d, true
	}
	// Fallback: parse from RawData
	if e.EventType == LiveFeedEventText && e.RawData != nil {
		return TextEventData{
			Text:       getStringFromMap(e.RawData, "text"),
			IsHistoric: getBoolFromMap(e.RawData, "is_historic"),
		}, true
	}
	return TextEventData{}, false
}

// GetFileChangeData returns the typed file_change data if this is a file_change event
func (e LiveFeedEvent) GetFileChangeData() (FileChangeEventData, bool) {
	if d, ok := e.Data.(FileChangeEventData); ok {
		return d, true
	}
	// Fallback: parse from RawData
	if e.EventType == LiveFeedEventFileChange && e.RawData != nil {
		return FileChangeEventData{
			Action:   getStringFromMap(e.RawData, "action"),
			FilePath: getStringFromMap(e.RawData, "file_path"),
		}, true
	}
	return FileChangeEventData{}, false
}

// GetAgentCompletedData returns the typed agent_completed data if this is an agent_completed event
func (e LiveFeedEvent) GetAgentCompletedData() (AgentCompletedEventData, bool) {
	if d, ok := e.Data.(AgentCompletedEventData); ok {
		return d, true
	}
	// Fallback: parse from RawData
	if e.EventType == LiveFeedEventAgentCompleted && e.RawData != nil {
		filesChanged, _ := getIntFromPayload(e.RawData, "files_changed")
		commitsCreated, _ := getIntFromPayload(e.RawData, "commits_created")
		return AgentCompletedEventData{
			FilesChanged:   filesChanged,
			CommitsCreated: commitsCreated,
			Error:          getStringFromMap(e.RawData, "error"),
			ResultMessage:  getStringFromMap(e.RawData, "result_message"),
			IsHistoric:     getBoolFromMap(e.RawData, "is_historic"),
		}, true
	}
	return AgentCompletedEventData{}, false
}

// Helper functions for parsing map values
func getStringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBoolFromMap(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func getInt64FromMap(m map[string]interface{}, key string) (int64, bool) {
	val, exists := m[key]
	if !exists {
		return 0, false
	}
	switch v := val.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	default:
		return 0, false
	}
}

// GitCommit represents a git commit made by an agent
type GitCommit struct {
	Hash         string   `json:"hash"`          // Full commit hash
	ShortHash    string   `json:"short_hash"`    // Short (7-char) commit hash
	Message      string   `json:"message"`       // Commit message (first line)
	Author       string   `json:"author"`        // Author name
	AuthorEmail  string   `json:"author_email"`  // Author email
	Timestamp    string   `json:"timestamp"`     // ISO 8601 timestamp
	FilesChanged []string `json:"files_changed"` // List of files modified in this commit
}

// LifecycleHistoryEntry represents a single state transition in the agent lifecycle.
type LifecycleHistoryEntry struct {
	From      string `json:"from"`      // Previous state
	To        string `json:"to"`        // New state
	Event     string `json:"event"`     // Event that triggered the transition
	Timestamp string `json:"timestamp"` // ISO 8601 timestamp
}

// Append adds new output to the buffer (thread-safe)
func (b *OutputBuffer) Append(stdout, stderr string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Stdout += stdout
	b.Stderr += stderr
}

// Get returns the current output (thread-safe)
func (b *OutputBuffer) Get() (stdout, stderr string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.Stdout, b.Stderr
}

// getIntFromPayload extracts an integer from a map value that may be int or float64
// (JSON unmarshal produces float64, but direct map assignment produces int)
func getIntFromPayload(payload map[string]interface{}, key string) (int, bool) {
	val, exists := payload[key]
	if !exists {
		return 0, false
	}
	switch v := val.(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	case int64:
		return int(v), true
	default:
		return 0, false
	}
}

// getInt64FromPayload extracts an int64 from a map value that may be int, int64, or float64
func getInt64FromPayload(payload map[string]interface{}, key string) (int64, bool) {
	val, exists := payload[key]
	if !exists {
		return 0, false
	}
	switch v := val.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	default:
		return 0, false
	}
}

// TokenUsage is an alias to types.TokenUsage for backwards compatibility.
// New code should import types.TokenUsage directly.
type TokenUsage = types.TokenUsage

// ModelUsageData is an alias to types.ModelUsage for backwards compatibility.
// Note: This was previously named ModelUsageData in daemon but ModelUsage in ipc.
// The canonical name is now types.ModelUsage.
type ModelUsageData = types.ModelUsage

// MergeStatus is an alias to types.MergeStatus for backwards compatibility.
// New code should import types.MergeStatus directly.
type MergeStatus = types.MergeStatus

// MergeStatus constants - aliases to types package for backwards compatibility.
const (
	MergeStatusNone             = types.MergeStatusNone
	MergeStatusPending          = types.MergeStatusPending
	MergeStatusAcquiring        = types.MergeStatusAcquiring
	MergeStatusMerging          = types.MergeStatusMerging
	MergeStatusResolving        = types.MergeStatusResolving
	MergeStatusMerged           = types.MergeStatusMerged
	MergeStatusFailed           = types.MergeStatusFailed
	MergeStatusResolved         = types.MergeStatusResolved
	MergeStatusSkipped          = types.MergeStatusSkipped
	MergeStatusMergedNeedsRepair = types.MergeStatusMergedNeedsRepair
)

// ValidationStep is an alias to types.ValidationStep for backwards compatibility.
// New code should import types.ValidationStep directly.
type ValidationStep = types.ValidationStep

// AgentState tracks the state of a single agent execution
type AgentState struct {
	ID              string          `json:"id"`                         // Unique agent ID
	RunID           string          `json:"run_id,omitempty"`           // ID of the run this agent belongs to
	TaskID          string          `json:"task_id"`                    // Beads task ID
	TaskTitle       string          `json:"task_title"`                 // Task title for display
	TaskDescription string          `json:"task_description,omitempty"` // Task description for display
	RepoID          string          `json:"repo_id,omitempty"`          // Repository this agent is working in
	ParentAgentID   string          `json:"parent_agent_id,omitempty"`  // ID of parent agent if spawned by another agent
	ChildAgentIDs   []string        `json:"child_agent_ids,omitempty"`  // IDs of child agents spawned by this agent
	Status          AgentStatus     `json:"status"`                     // Current agent status (legacy)
	MergeStatus     MergeStatus     `json:"merge_status,omitempty"`     // Current merge queue status (legacy)

	// Lifecycle is the new state machine that runs in parallel with legacy fields.
	// During the transition period (Phase 1), both systems run concurrently and
	// divergence is logged for monitoring. The lifecycle field is not serialized
	// to JSON as it is ephemeral runtime state.
	Lifecycle *lifecycle.AgentLifecycle `json:"-"`
	// LifecycleState is the current lifecycle state as a string for JSON serialization.
	// Derived from the Lifecycle state machine when available.
	LifecycleState string `json:"lifecycle_state,omitempty"`
	// LifecycleHistory contains the state transition history for debugging.
	// Populated on demand (e.g., when --show-history is requested).
	LifecycleHistory []LifecycleHistoryEntry `json:"lifecycle_history,omitempty"`
	MergeQueuePos   int             `json:"merge_queue_pos,omitempty"`  // Position in merge wait queue (0 = not waiting)
	MergeError      string          `json:"merge_error,omitempty"`      // Error message if merge failed
	StartTime       time.Time       `json:"start_time"`                 // When agent started
	EndTime         *time.Time      `json:"end_time"`                   // When agent finished (nil if running)
	Duration        float64         `json:"duration"`                   // Execution duration in seconds
	DurationMS      int64           `json:"duration_ms"`                // Execution duration in milliseconds (from Claude)
	DurationAPIMS   int64           `json:"duration_api_ms"`            // API duration in milliseconds
	NumTurns        int             `json:"num_turns"`                  // Number of agentic turns
	Output          OutputBuffer    `json:"output"`                     // Stdout/stderr buffers
	LiveFeedEvents  []LiveFeedEvent `json:"live_feed_events"`           // Real-time events from Claude API
	TokenUsage      TokenUsage      `json:"token_usage"`                // Token consumption stats
	ExitCode        int             `json:"exit_code"`                  // Process exit code
	Error           string          `json:"error"`                      // Error message if failed
	Changes         int             `json:"changes"`                    // Number of files changed
	Commits         int             `json:"commits"`                    // Number of git commits made (legacy, use len(GitCommits))
	GitCommits       []GitCommit     `json:"git_commits"`                  // Detailed git commit history
	ResultMessage    string          `json:"result_message"`               // Final result message from Claude
	Archived         bool            `json:"archived"`                     // Whether the agent is archived (hidden by default)
	RepairAttempts     int             `json:"repair_attempts"`                // Number of repair attempts made (0 = no repairs attempted)
	LastRepairOutput   string          `json:"last_repair_output,omitempty"`   // Output/error from the last repair attempt
	ValidationStatus   string          `json:"validation_status,omitempty"`    // Validation status: pending, running, passed, failed, skipped, repairing
	ValidationSteps    []ValidationStep `json:"validation_steps,omitempty"`    // Results of individual validation steps
	ValidationDuration int64           `json:"validation_duration_ms,omitempty"` // Total validation duration in milliseconds
	ValidationError    string          `json:"validation_error,omitempty"`     // Error message if validation failed
	SessionID          string          `json:"session_id,omitempty"`           // Claude CLI session ID for claude --resume support
	IsResume           bool            `json:"is_resume,omitempty"`            // True if this agent was resumed after daemon restart
	ResumeCount        int             `json:"resume_count,omitempty"`         // Number of times this agent has been resumed
	InterruptedAt      *time.Time      `json:"interrupted_at,omitempty"`       // When the agent was interrupted (for resumed agents)
	Attempt            int             `json:"attempt,omitempty"`              // Current attempt number (1 = first try)
	MaxRetries         int             `json:"max_retries,omitempty"`          // Max retry attempts configured
	mu                 sync.RWMutex
}

// Update atomically updates agent state fields
func (a *AgentState) Update(fn func(*AgentState)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fn(a)
}

// GetSnapshot returns a copy of the agent state (thread-safe)
// Note: This returns a copy without the mutex to avoid copylocks issues.
func (a *AgentState) GetSnapshot() AgentState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Get output buffer values safely
	stdout, stderr := a.Output.Get()

	// Derive lifecycle state from the state machine if available
	var lifecycleState string
	var lifecycleHistory []LifecycleHistoryEntry
	if a.Lifecycle != nil {
		lifecycleState = a.Lifecycle.State().String()
		// Copy history entries
		for _, h := range a.Lifecycle.History() {
			lifecycleHistory = append(lifecycleHistory, LifecycleHistoryEntry{
				From:      h.From.String(),
				To:        h.To.String(),
				Event:     h.Event.String(),
				Timestamp: h.Timestamp.Format(time.RFC3339),
			})
		}
	}

	// Copy all fields except mutexes
	return AgentState{
		ID:                 a.ID,
		RunID:              a.RunID,
		TaskID:             a.TaskID,
		TaskTitle:          a.TaskTitle,
		TaskDescription:    a.TaskDescription,
		RepoID:             a.RepoID,
		ParentAgentID:      a.ParentAgentID,
		ChildAgentIDs:      append([]string(nil), a.ChildAgentIDs...),
		Status:             a.Status,
		MergeStatus:        a.MergeStatus,
		LifecycleState:     lifecycleState,
		LifecycleHistory:   lifecycleHistory,
		MergeQueuePos:      a.MergeQueuePos,
		MergeError:         a.MergeError,
		StartTime:          a.StartTime,
		EndTime:            a.EndTime,
		Duration:           a.Duration,
		DurationMS:         a.DurationMS,
		DurationAPIMS:      a.DurationAPIMS,
		NumTurns:           a.NumTurns,
		Output:             OutputBuffer{Stdout: stdout, Stderr: stderr},
		LiveFeedEvents:     append([]LiveFeedEvent(nil), a.LiveFeedEvents...),
		TokenUsage:         a.TokenUsage,
		ExitCode:           a.ExitCode,
		Error:              a.Error,
		Changes:            a.Changes,
		Commits:            a.Commits,
		GitCommits:         append([]GitCommit(nil), a.GitCommits...),
		ResultMessage:      a.ResultMessage,
		Archived:           a.Archived,
		RepairAttempts:     a.RepairAttempts,
		LastRepairOutput:   a.LastRepairOutput,
		ValidationStatus:   a.ValidationStatus,
		ValidationSteps:    append([]ValidationStep(nil), a.ValidationSteps...),
		ValidationDuration: a.ValidationDuration,
		ValidationError:    a.ValidationError,
		SessionID:          a.SessionID,
		IsResume:           a.IsResume,
		ResumeCount:        a.ResumeCount,
		InterruptedAt:      a.InterruptedAt,
		Attempt:            a.Attempt,
		MaxRetries:         a.MaxRetries,
		// mu is intentionally not copied
	}
}

// TaskState represents the state of a task in the orchestration
type TaskState struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`   // ready, in_progress, completed, failed, needs-input
	Type         string   `json:"type,omitempty"` // Task type (task, bug, feature, etc.)
	AgentID      string   `json:"agent_id"` // ID of agent executing this task
	Priority     int      `json:"priority"`
	Dependencies []string `json:"dependencies"` // Task IDs this task depends on
	Archived     bool     `json:"archived"`     // Whether the task is archived
	RepoID       string   `json:"repo_id,omitempty"` // Repository this task belongs to
	UpdatedAt    int64    `json:"updated_at,omitempty"` // Unix timestamp of last update
}

// Stats aggregates statistics across all agents
type Stats struct {
	TotalTasks               int         `json:"total_tasks"`
	CompletedTasks           int         `json:"completed_tasks"`
	FailedTasks              int         `json:"failed_tasks"`
	RunningTasks             int         `json:"running_tasks"`
	TotalInputTokens         int         `json:"total_input_tokens"`
	TotalOutputTokens        int         `json:"total_output_tokens"`
	TotalCacheCreationTokens int         `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens     int         `json:"total_cache_read_tokens"`
	TotalTokens              int         `json:"total_tokens"`
	TotalCostUSD             float64     `json:"total_cost_usd"`
	TotalTurns               int         `json:"total_turns"`
	TotalDuration            float64     `json:"total_duration"` // Total execution time in seconds
	AverageDuration          float64     `json:"avg_duration"`   // Average task duration
	FileChanges              int         `json:"file_changes"`   // Total files changed
	GitCommits               int         `json:"git_commits"`    // Total commits made (count)
	AllGitCommits            []GitCommit `json:"all_git_commits"` // All commits from all agents
}

// RuntimeState aggregates the complete state of an orchestration run
type RuntimeState struct {
	Agents       map[string]*AgentState `json:"agents"`        // Agent ID -> AgentState
	Tasks        map[string]*TaskState  `json:"tasks"`         // Task ID -> TaskState (legacy: merged view)
	Stats        Stats                  `json:"stats"`         // Aggregate statistics
	IsPaused     bool                   `json:"is_paused"`     // Whether orchestration is paused
	StartTime    time.Time              `json:"start_time"`    // When orchestration started
	CurrentRunID string                 `json:"current_run_id"` // Current run ID for new agents

	// Hybrid overlay architecture: separate persistent (beads) from runtime state
	// persistentTasks: canonical state from beads (source of truth)
	// runtimeTasks: ephemeral overlay (in_progress, agent assignments)
	persistentTasks map[string]*TaskState // From beads - does NOT get modified during runtime
	runtimeTasks    map[string]*TaskState // Runtime overlay - rebuilt from agent events

	// eventBus is stored when SubscribeToEventBus is called, enabling lifecycle
	// transition callbacks to publish events for real-time UI updates.
	eventBus *EventBus

	mu sync.RWMutex
}

// NewRuntimeState creates a new runtime state tracker
func NewRuntimeState() *RuntimeState {
	return &RuntimeState{
		Agents:          make(map[string]*AgentState),
		Tasks:           make(map[string]*TaskState),
		persistentTasks: make(map[string]*TaskState),
		runtimeTasks:    make(map[string]*TaskState),
		StartTime:       time.Now(),
	}
}

// AddAgent registers a new agent in the state
func (r *RuntimeState) AddAgent(agent *AgentState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Agents[agent.ID] = agent
}

// GetAgent retrieves an agent by ID (thread-safe)
func (r *RuntimeState) GetAgent(id string) *AgentState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Agents[id]
}

// AddTask registers a task in the state
func (r *RuntimeState) AddTask(task *beads.Task) {
	r.AddTaskWithRepo(task, "")
}

// AddTaskWithRepo registers a task in the state with an associated repository ID.
// This adds the task to persistentTasks (from beads) and also to the legacy Tasks map
// for backwards compatibility.
func (r *RuntimeState) AddTaskWithRepo(task *beads.Task, repoID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Parse UpdatedAt from ISO 8601 string to Unix timestamp
	var updatedAt int64
	if task.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, task.UpdatedAt); err == nil {
			updatedAt = t.Unix()
		}
	}

	taskState := &TaskState{
		ID:           task.ID,
		Title:        task.Title,
		Status:       task.Status,
		Priority:     task.Priority,
		Dependencies: task.GetDependencies(),
		RepoID:       repoID,
		UpdatedAt:    updatedAt,
	}

	// Add to persistent tasks (canonical state from beads)
	r.persistentTasks[task.ID] = taskState

	// Also maintain legacy Tasks map for backwards compatibility
	// Create a copy to avoid shared state issues
	taskCopy := *taskState
	if taskState.Dependencies != nil {
		taskCopy.Dependencies = make([]string, len(taskState.Dependencies))
		copy(taskCopy.Dependencies, taskState.Dependencies)
	}
	r.Tasks[task.ID] = &taskCopy
}

// SetRuntimeTaskStatus updates the runtime overlay for a task's status and agent assignment.
// This is called when agents start working on tasks (in_progress) or complete them.
// The runtime state overlays the persistent state from beads.
func (r *RuntimeState) SetRuntimeTaskStatus(taskID, status, agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Get or create runtime task entry
	rt, exists := r.runtimeTasks[taskID]
	if !exists {
		// Create minimal runtime overlay with just the changed fields
		rt = &TaskState{
			ID: taskID,
		}
		r.runtimeTasks[taskID] = rt
	}

	rt.Status = status
	rt.AgentID = agentID
}

// GetPersistentTasks returns a copy of the persistent tasks map (from beads).
// This is the canonical source of truth for task definitions.
func (r *RuntimeState) GetPersistentTasks() map[string]*TaskState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*TaskState, len(r.persistentTasks))
	for id, task := range r.persistentTasks {
		taskCopy := *task
		if task.Dependencies != nil {
			taskCopy.Dependencies = make([]string, len(task.Dependencies))
			copy(taskCopy.Dependencies, task.Dependencies)
		}
		result[id] = &taskCopy
	}
	return result
}

// GetRuntimeTasks returns a copy of the runtime tasks overlay.
// These are ephemeral states (in_progress, agent assignments) that overlay persistent tasks.
func (r *RuntimeState) GetRuntimeTasks() map[string]*TaskState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*TaskState, len(r.runtimeTasks))
	for id, task := range r.runtimeTasks {
		taskCopy := *task
		if task.Dependencies != nil {
			taskCopy.Dependencies = make([]string, len(task.Dependencies))
			copy(taskCopy.Dependencies, task.Dependencies)
		}
		result[id] = &taskCopy
	}
	return result
}

// ClearRuntimeTasksForRepo clears runtime task overlays for a specific repository.
// Called when switching repositories or resetting state.
func (r *RuntimeState) ClearRuntimeTasksForRepo(repoID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if repoID == "" {
		r.runtimeTasks = make(map[string]*TaskState)
		return
	}

	// We need to check persistent tasks to find the repo association
	for taskID := range r.runtimeTasks {
		if pt, exists := r.persistentTasks[taskID]; exists && pt.RepoID == repoID {
			delete(r.runtimeTasks, taskID)
		}
	}
}

// ClearTasksForRepo removes all tasks associated with a specific repository.
// If repoID is empty, clears all tasks.
func (r *RuntimeState) ClearTasksForRepo(repoID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if repoID == "" {
		r.Tasks = make(map[string]*TaskState)
		r.persistentTasks = make(map[string]*TaskState)
		r.runtimeTasks = make(map[string]*TaskState)
		return
	}
	for id, task := range r.Tasks {
		if task.RepoID == repoID {
			delete(r.Tasks, id)
		}
	}
	for id, task := range r.persistentTasks {
		if task.RepoID == repoID {
			delete(r.persistentTasks, id)
			delete(r.runtimeTasks, id) // Also clear runtime overlay
		}
	}
}

// RemoveTask removes a task from all task maps (persistent, runtime, and legacy).
// This is used during garbage collection when a task no longer exists in beads.
func (r *RuntimeState) RemoveTask(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Tasks, taskID)
	delete(r.persistentTasks, taskID)
	delete(r.runtimeTasks, taskID)
}

// UpdateTaskStatus updates the status of a task.
// This updates both the legacy Tasks map and the runtime overlay.
func (r *RuntimeState) UpdateTaskStatus(taskID, status, agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Update legacy Tasks map for backwards compatibility
	if task, exists := r.Tasks[taskID]; exists {
		task.Status = status
		task.AgentID = agentID
	}

	// Update runtime overlay
	rt, exists := r.runtimeTasks[taskID]
	if !exists {
		rt = &TaskState{ID: taskID}
		r.runtimeTasks[taskID] = rt
	}
	rt.Status = status
	rt.AgentID = agentID
}

// SetTaskArchived sets the archived status of a task
func (r *RuntimeState) SetTaskArchived(taskID string, archived bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if task, exists := r.Tasks[taskID]; exists {
		task.Archived = archived
	}
}

// SetAgentArchived sets the archived status of an agent
func (r *RuntimeState) SetAgentArchived(agentID string, archived bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if agent, exists := r.Agents[agentID]; exists {
		agent.Update(func(a *AgentState) {
			a.Archived = archived
		})
	}
}

// UpdateStats recalculates aggregate statistics from all agents
func (r *RuntimeState) UpdateStats() {
	r.mu.Lock()
	defer r.mu.Unlock()

	stats := Stats{}
	var totalDuration float64
	completedCount := 0
	var allCommits []GitCommit

	for _, agent := range r.Agents {
		agent.mu.RLock()

		switch agent.Status {
		case AgentStatusCompleted:
			stats.CompletedTasks++
			completedCount++
			totalDuration += agent.Duration
		case AgentStatusFailed, AgentStatusTimedOut:
			stats.FailedTasks++
		case AgentStatusRunning, AgentStatusStarting:
			stats.RunningTasks++
		}

		stats.TotalInputTokens += agent.TokenUsage.InputTokens
		stats.TotalOutputTokens += agent.TokenUsage.OutputTokens
		stats.TotalCacheCreationTokens += agent.TokenUsage.CacheCreationInputTokens
		stats.TotalCacheReadTokens += agent.TokenUsage.CacheReadInputTokens
		stats.TotalTokens += agent.TokenUsage.TotalTokens
		stats.TotalCostUSD += agent.TokenUsage.CostUSD
		stats.TotalTurns += agent.NumTurns
		stats.FileChanges += agent.Changes
		stats.GitCommits += agent.Commits

		// Aggregate git commit details from all agents
		if len(agent.GitCommits) > 0 {
			allCommits = append(allCommits, agent.GitCommits...)
		}

		agent.mu.RUnlock()
	}

	stats.TotalTasks = len(r.Agents)
	stats.TotalDuration = totalDuration
	if completedCount > 0 {
		stats.AverageDuration = totalDuration / float64(completedCount)
	}
	stats.AllGitCommits = allCommits

	r.Stats = stats

	// Update Prometheus metrics for agent counts
	metrics.SetActiveAgents("running", stats.RunningTasks)
	metrics.SetActiveAgents("completed", stats.CompletedTasks)
	metrics.SetActiveAgents("failed", stats.FailedTasks)
}

// Pause sets the paused state
func (r *RuntimeState) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.IsPaused = true
}

// Resume clears the paused state
func (r *RuntimeState) Resume() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.IsPaused = false
}

// CountRunningAgents returns the number of agents currently in "running" status.
func (r *RuntimeState) CountRunningAgents() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, agent := range r.Agents {
		if agent.Status == "running" {
			count++
		}
	}
	return count
}

// RuntimeStateSnapshot is a snapshot of RuntimeState without the mutex.
// Used to safely return state copies without triggering copylocks warnings.
type RuntimeStateSnapshot struct {
	Agents       map[string]*AgentState `json:"agents"`
	Tasks        map[string]*TaskState  `json:"tasks"`          // Legacy: merged view for backwards compat
	Stats        Stats                  `json:"stats"`
	IsPaused     bool                   `json:"is_paused"`
	StartTime    time.Time              `json:"start_time"`
	CurrentRunID string                 `json:"current_run_id"`

	// Orchestrator state fields
	OrchestratorState string `json:"orchestrator_state,omitempty"` // off, idle, active, paused
	ActiveAgentCount  int    `json:"active_agent_count,omitempty"` // Number of currently running agents

	// Hybrid overlay architecture: dual-source task state
	// PersistentTasks: canonical state from beads (source of truth)
	// RuntimeTasks: ephemeral overlay (in_progress, agent assignments)
	// Frontend merges: runtime overlays persistent for display
	PersistentTasks map[string]*TaskState `json:"persistent_tasks,omitempty"`
	RuntimeTasks    map[string]*TaskState `json:"runtime_tasks,omitempty"`

	// EventSequence is the sequence number of the last event published before
	// this snapshot was taken. Clients should discard any events with sequence
	// numbers <= this value, as they are already reflected in the snapshot.
	EventSequence uint64 `json:"event_sequence,omitempty"`
}

// GetSnapshot returns a complete snapshot of the runtime state (thread-safe)
func (r *RuntimeState) GetSnapshot() RuntimeStateSnapshot {
	// Recalculate stats before taking snapshot to ensure they're up to date
	r.UpdateStats()

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Get current event sequence if eventBus is available.
	// Clients can use this to discard events that occurred before the snapshot.
	var eventSequence uint64
	if r.eventBus != nil {
		eventSequence = r.eventBus.GetSequence()
	}

	snapshot := RuntimeStateSnapshot{
		Agents:          make(map[string]*AgentState),
		Tasks:           make(map[string]*TaskState),
		PersistentTasks: make(map[string]*TaskState),
		RuntimeTasks:    make(map[string]*TaskState),
		Stats:           r.Stats,
		IsPaused:        r.IsPaused,
		StartTime:       r.StartTime,
		CurrentRunID:    r.CurrentRunID,
		EventSequence:   eventSequence,
	}

	// Deep copy agents
	for id, agent := range r.Agents {
		agentCopy := agent.GetSnapshot()
		snapshot.Agents[id] = &agentCopy
	}

	// Deep copy legacy tasks (merged view for backwards compat)
	for id, task := range r.Tasks {
		taskCopy := *task
		if task.Dependencies != nil {
			taskCopy.Dependencies = make([]string, len(task.Dependencies))
			copy(taskCopy.Dependencies, task.Dependencies)
		}
		snapshot.Tasks[id] = &taskCopy
	}

	// Deep copy persistent tasks (from beads)
	for id, task := range r.persistentTasks {
		taskCopy := *task
		if task.Dependencies != nil {
			taskCopy.Dependencies = make([]string, len(task.Dependencies))
			copy(taskCopy.Dependencies, task.Dependencies)
		}
		snapshot.PersistentTasks[id] = &taskCopy
	}

	// Deep copy runtime tasks (ephemeral overlay)
	for id, task := range r.runtimeTasks {
		taskCopy := *task
		if task.Dependencies != nil {
			taskCopy.Dependencies = make([]string, len(task.Dependencies))
			copy(taskCopy.Dependencies, task.Dependencies)
		}
		snapshot.RuntimeTasks[id] = &taskCopy
	}

	return snapshot
}

// GetTasksForRepo returns all tasks belonging to a specific repository.
// If repoID is empty, returns all tasks.
func (r *RuntimeState) GetTasksForRepo(repoID string) map[string]*TaskState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*TaskState)
	for id, task := range r.Tasks {
		if repoID == "" || task.RepoID == repoID {
			taskCopy := *task
			if task.Dependencies != nil {
				taskCopy.Dependencies = make([]string, len(task.Dependencies))
				copy(taskCopy.Dependencies, task.Dependencies)
			}
			result[id] = &taskCopy
		}
	}
	return result
}

// GetSnapshotForRepo returns a snapshot of the runtime state filtered to a specific repository.
// Agents and tasks are filtered by repo ID. Stats are recalculated for the filtered data.
// If repoID is empty, behaves like GetSnapshot().
func (r *RuntimeState) GetSnapshotForRepo(repoID string) RuntimeStateSnapshot {
	if repoID == "" {
		return r.GetSnapshot()
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Get current event sequence if eventBus is available.
	var eventSequence uint64
	if r.eventBus != nil {
		eventSequence = r.eventBus.GetSequence()
	}

	snapshot := RuntimeStateSnapshot{
		Agents:          make(map[string]*AgentState),
		Tasks:           make(map[string]*TaskState),
		PersistentTasks: make(map[string]*TaskState),
		RuntimeTasks:    make(map[string]*TaskState),
		IsPaused:        r.IsPaused,
		StartTime:       r.StartTime,
		CurrentRunID:    r.CurrentRunID,
		EventSequence:   eventSequence,
	}

	// Filter and deep copy agents (note: agents don't have repo_id in struct yet,
	// but they are associated with tasks that do)
	// For now, we include all agents since agent<->repo mapping requires task lookup
	for id, agent := range r.Agents {
		agentCopy := agent.GetSnapshot()
		snapshot.Agents[id] = &agentCopy
	}

	// Filter and deep copy legacy tasks by repo
	for id, task := range r.Tasks {
		if task.RepoID == repoID {
			taskCopy := *task
			if task.Dependencies != nil {
				taskCopy.Dependencies = make([]string, len(task.Dependencies))
				copy(taskCopy.Dependencies, task.Dependencies)
			}
			snapshot.Tasks[id] = &taskCopy
		}
	}

	// Filter and deep copy persistent tasks by repo
	for id, task := range r.persistentTasks {
		if task.RepoID == repoID {
			taskCopy := *task
			if task.Dependencies != nil {
				taskCopy.Dependencies = make([]string, len(task.Dependencies))
				copy(taskCopy.Dependencies, task.Dependencies)
			}
			snapshot.PersistentTasks[id] = &taskCopy

			// Also include runtime overlay for this task if it exists
			if rt, exists := r.runtimeTasks[id]; exists {
				rtCopy := *rt
				if rt.Dependencies != nil {
					rtCopy.Dependencies = make([]string, len(rt.Dependencies))
					copy(rtCopy.Dependencies, rt.Dependencies)
				}
				snapshot.RuntimeTasks[id] = &rtCopy
			}
		}
	}

	// Recalculate stats for filtered data
	snapshot.recalculateStats()

	return snapshot
}

// recalculateStats calculates aggregate statistics from agents in the snapshot
func (s *RuntimeStateSnapshot) recalculateStats() {
	stats := Stats{}
	var totalDuration float64
	completedCount := 0
	var allCommits []GitCommit

	for _, agent := range s.Agents {
		switch agent.Status {
		case AgentStatusCompleted:
			stats.CompletedTasks++
			completedCount++
			totalDuration += agent.Duration
		case AgentStatusFailed, AgentStatusTimedOut:
			stats.FailedTasks++
		case AgentStatusRunning, AgentStatusStarting:
			stats.RunningTasks++
		}

		stats.TotalInputTokens += agent.TokenUsage.InputTokens
		stats.TotalOutputTokens += agent.TokenUsage.OutputTokens
		stats.TotalCacheCreationTokens += agent.TokenUsage.CacheCreationInputTokens
		stats.TotalCacheReadTokens += agent.TokenUsage.CacheReadInputTokens
		stats.TotalTokens += agent.TokenUsage.TotalTokens
		stats.TotalCostUSD += agent.TokenUsage.CostUSD
		stats.TotalTurns += agent.NumTurns
		stats.FileChanges += agent.Changes
		stats.GitCommits += agent.Commits

		if len(agent.GitCommits) > 0 {
			allCommits = append(allCommits, agent.GitCommits...)
		}
	}

	stats.TotalTasks = len(s.Agents)
	stats.TotalDuration = totalDuration
	if completedCount > 0 {
		stats.AverageDuration = totalDuration / float64(completedCount)
	}
	stats.AllGitCommits = allCommits

	s.Stats = stats
}

// expectedLifecycleState returns the lifecycle state that corresponds to the legacy
// Status and MergeStatus fields. Used to detect divergence during parallel rollout.
func expectedLifecycleState(status AgentStatus, mergeStatus MergeStatus, validationStatus string) lifecycle.AgentLifecycleState {
	// First check validation status if merge is complete
	if mergeStatus == MergeStatusMerged || mergeStatus == MergeStatusResolved {
		switch validationStatus {
		case "running":
			return lifecycle.StateValidating
		case "repairing":
			return lifecycle.StateRepairing
		case "failed":
			return lifecycle.StateNeedsAttention
		case "passed", "skipped", "":
			// Fall through to check agent status
		}
	}

	// Check merge status
	switch mergeStatus {
	case MergeStatusPending:
		return lifecycle.StateQueuedForMerge
	case MergeStatusAcquiring, MergeStatusMerging:
		return lifecycle.StateMerging
	case MergeStatusResolving:
		return lifecycle.StateResolving
	case MergeStatusFailed:
		return lifecycle.StateMergeFailed
	}

	// Check agent status
	switch status {
	case AgentStatusStarting:
		return lifecycle.StateStarting
	case AgentStatusRunning:
		// If we have a merge status indicating queue, use that
		if mergeStatus == MergeStatusPending {
			return lifecycle.StateQueuedForMerge
		}
		return lifecycle.StateRunning
	case AgentStatusCompleted:
		return lifecycle.StateCompleted
	case AgentStatusFailed:
		return lifecycle.StateFailed
	case AgentStatusCancelled:
		return lifecycle.StateCancelled
	case AgentStatusTimedOut:
		return lifecycle.StateTimedOut
	}

	// Default to running if we can't determine
	return lifecycle.StateRunning
}

// checkLifecycleDivergence compares the lifecycle state machine state against the
// legacy status fields and logs a warning + records a metric if they diverge.
// This is used during the parallel rollout phase to detect inconsistencies.
func checkLifecycleDivergence(agent *AgentState, event string) {
	if agent == nil || agent.Lifecycle == nil {
		return
	}

	// Read legacy fields under lock
	agent.mu.RLock()
	legacyStatus := agent.Status
	mergeStatus := agent.MergeStatus
	validationStatus := agent.ValidationStatus
	agentID := agent.ID
	agent.mu.RUnlock()

	lifecycleState := agent.Lifecycle.State()
	expected := expectedLifecycleState(legacyStatus, mergeStatus, validationStatus)

	if lifecycleState != expected {
		logging.Warn("lifecycle state divergence detected",
			"agent_id", agentID,
			"event", event,
			"legacy_status", string(legacyStatus),
			"merge_status", string(mergeStatus),
			"validation_status", validationStatus,
			"lifecycle_state", lifecycleState.String(),
			"expected_lifecycle_state", expected.String(),
		)
		metrics.RecordLifecycleDivergence(string(legacyStatus), lifecycleState.String(), event)
	}
}

// makeLifecycleCallback creates a callback that publishes lifecycle state transitions
// to the EventBus for real-time UI updates. All lifecycle state changes are published
// via EventLifecycleStateChanged, including terminal states (completed, failed, etc.).
// EventAgentRunning is also published for backwards compatibility when transitioning
// to the running state.
func (r *RuntimeState) makeLifecycleCallback(agentID string) lifecycle.TransitionCallback {
	return func(from, to lifecycle.AgentLifecycleState, event lifecycle.AgentEvent) {
		r.mu.RLock()
		eventBus := r.eventBus
		r.mu.RUnlock()

		if eventBus == nil {
			return
		}

		// Publish the lifecycle state change event for all states including terminal states.
		// This ensures the dashboard receives real-time updates for all state transitions.
		// Terminal states (completed, failed, etc.) are published here even though
		// EventAgentCompleted/EventAgentFailed also fire - the dashboard needs both
		// for full state updates (lifecycle_state from this event, result data from completion).
		eventBus.Publish(Event{
			Type:      EventLifecycleStateChanged,
			Timestamp: time.Now(),
			Payload: map[string]interface{}{
				"agent_id":        agentID,
				"lifecycle_state": to.String(),
				"previous_state":  from.String(),
				"event":           event.String(),
			},
		})

		// Also publish EventAgentRunning for backwards compatibility
		// This ensures existing code that listens for agent:running still works
		if to == lifecycle.StateRunning {
			eventBus.Publish(Event{
				Type:      EventAgentRunning,
				Timestamp: time.Now(),
				Payload: map[string]interface{}{
					"agent_id":        agentID,
					"lifecycle_state": to.String(),
					"previous_state":  from.String(),
					"event":           event.String(),
				},
			})
		}
	}
}

// SubscribeToEventBus subscribes to the EventBus and updates state from events.
// Returns an unsubscribe function. This bridges IPC events to RuntimeState updates.
// Also stores the eventBus reference for publishing lifecycle transition events.
func (r *RuntimeState) SubscribeToEventBus(eventBus *EventBus) func() {
	r.mu.Lock()
	r.eventBus = eventBus
	r.mu.Unlock()

	return eventBus.Subscribe(func(event Event) {
		r.handleEvent(event)
	})
}

// handleEvent processes an event and updates the runtime state accordingly
func (r *RuntimeState) handleEvent(event Event) {
	// Extract payload as a map for easy access
	payload, ok := event.Payload.(map[string]interface{})
	if !ok {
		return
	}

	switch event.Type {
	case EventRunStarted:
		r.handleRunStarted(payload)
	case EventAgentStarted:
		r.handleAgentStarted(payload, event.Timestamp)
	case EventAgentOutput:
		r.handleAgentOutput(payload)
	case EventAgentLiveFeed:
		r.handleAgentLiveFeed(payload)
	case EventAgentCommit:
		r.handleAgentCommit(payload)
	case EventAgentMergeStatus:
		r.handleAgentMergeStatus(payload)
	case EventAgentCompleted:
		r.handleAgentCompleted(payload, event.Timestamp)
	case EventTaskUpdated:
		r.handleTaskUpdated(payload)
	case EventStatsUpdated:
		// Stats updates are informational, we recalculate from agents
		r.UpdateStats()
	case EventLifecycleStateChanged:
		r.handleLifecycleStateChanged(payload)
	}
}

func (r *RuntimeState) handleRunStarted(payload map[string]interface{}) {
	runID, _ := payload["run_id"].(string)
	if runID == "" {
		return
	}

	r.mu.Lock()
	r.CurrentRunID = runID
	r.mu.Unlock()
}

func (r *RuntimeState) handleAgentStarted(payload map[string]interface{}, timestamp time.Time) {
	agentID, _ := payload["agent_id"].(string)
	runID, _ := payload["run_id"].(string)
	taskID, _ := payload["task_id"].(string)
	taskTitle, _ := payload["task_title"].(string)
	taskDescription, _ := payload["task_description"].(string)
	parentAgentID, _ := payload["parent_agent_id"].(string)
	repoID, _ := payload["repo_id"].(string)

	// Extract retry information from payload
	var attempt, maxRetries int
	if attemptVal, ok := payload["attempt"].(float64); ok {
		attempt = int(attemptVal)
	}
	if maxRetriesVal, ok := payload["max_retries"].(float64); ok {
		maxRetries = int(maxRetriesVal)
	}

	if agentID == "" {
		return
	}

	// Use run_id from payload if provided, otherwise fall back to CurrentRunID
	if runID == "" {
		r.mu.RLock()
		runID = r.CurrentRunID
		r.mu.RUnlock()
	}

	// Initialize lifecycle state machine with transition callback for real-time UI updates
	agentLifecycle := lifecycle.New(
		lifecycle.WithTransitionCallback(r.makeLifecycleCallback(agentID)),
	)

	agent := &AgentState{
		ID:              agentID,
		RunID:           runID,
		TaskID:          taskID,
		TaskTitle:       taskTitle,
		TaskDescription: taskDescription,
		RepoID:          repoID,
		ParentAgentID:   parentAgentID,
		Status:          AgentStatusRunning,
		StartTime:       timestamp,
		Lifecycle:       agentLifecycle,
		Attempt:         attempt,
		MaxRetries:      maxRetries,
	}

	// IMPORTANT: Add agent to RuntimeState BEFORE lifecycle transition.
	// The lifecycle transition publishes events via the callback. If we transition
	// first, clients receiving the event may request a state snapshot that doesn't
	// include the agent yet, causing stale state in the UI.
	r.AddAgent(agent)

	// Transition lifecycle to running state (parallel with legacy Status field)
	if err := agentLifecycle.Transition(lifecycle.EventAgentSpawned, lifecycle.TransitionContext{}); err != nil {
		logging.Warn("lifecycle transition failed on agent start",
			"agent_id", agentID,
			"event", lifecycle.EventAgentSpawned,
			"error", err,
		)
	}

	// Check for divergence between legacy and lifecycle state
	checkLifecycleDivergence(agent, "agent_started")

	// Link child to parent agent if parent exists
	if parentAgentID != "" {
		if parent := r.GetAgent(parentAgentID); parent != nil {
			parent.Update(func(p *AgentState) {
				p.ChildAgentIDs = append(p.ChildAgentIDs, agentID)
			})
		}
	}

	// Update task status if we have it
	if taskID != "" {
		r.UpdateTaskStatus(taskID, "in_progress", agentID)
	}

	r.UpdateStats()
}

func (r *RuntimeState) handleAgentOutput(payload map[string]interface{}) {
	agentID, _ := payload["agent_id"].(string)
	output, _ := payload["output"].(string)
	isError, _ := payload["is_error"].(bool)

	if agentID == "" || output == "" {
		return
	}

	agent := r.GetAgent(agentID)
	if agent == nil {
		return
	}

	if isError {
		agent.Output.Append("", output)
	} else {
		agent.Output.Append(output, "")
	}
}

func (r *RuntimeState) handleAgentLiveFeed(payload map[string]interface{}) {
	agentID, _ := payload["agent_id"].(string)
	eventType, _ := payload["event_type"].(string)
	data, _ := payload["data"].(map[string]interface{})

	if agentID == "" || eventType == "" {
		return
	}

	agent := r.GetAgent(agentID)
	if agent == nil {
		return
	}

	// Create the live feed event with typed data based on event type
	var liveFeedEvent LiveFeedEvent
	switch LiveFeedEventType(eventType) {
	case LiveFeedEventToolUse:
		tool := getStringFromMap(data, "tool")
		filePath := getStringFromMap(data, "file_path")
		command := getStringFromMap(data, "command")
		pattern := getStringFromMap(data, "pattern")
		liveFeedEvent = NewToolUseEvent(tool, filePath, command, pattern)
	case LiveFeedEventText:
		text := getStringFromMap(data, "text")
		isHistoric := getBoolFromMap(data, "is_historic")
		liveFeedEvent = NewTextEvent(text, isHistoric)
	case LiveFeedEventFileChange:
		action := getStringFromMap(data, "action")
		filePath := getStringFromMap(data, "file_path")
		liveFeedEvent = NewFileChangeEvent(action, filePath)
	case LiveFeedEventAgentCompleted:
		filesChanged, _ := getIntFromPayload(data, "files_changed")
		commitsCreated, _ := getIntFromPayload(data, "commits_created")
		errMsg := getStringFromMap(data, "error")
		resultMsg := getStringFromMap(data, "result_message")
		isHistoric := getBoolFromMap(data, "is_historic")
		liveFeedEvent = NewAgentCompletedEvent(filesChanged, commitsCreated, errMsg, resultMsg, isHistoric)
	default:
		// For unknown event types, store with raw data only
		liveFeedEvent = LiveFeedEvent{
			EventType: LiveFeedEventType(eventType),
			RawData:   data,
		}
	}

	agent.Update(func(a *AgentState) {
		a.LiveFeedEvents = append(a.LiveFeedEvents, liveFeedEvent)
	})

	// Persist event to JSONL file for later restoration
	if err := persistence.AppendLiveFeedEvent(agentID, eventType, liveFeedEvent.RawData); err != nil {
		logging.Debug("failed to persist live feed event", "agent_id", agentID, "error", err)
	}
}

func (r *RuntimeState) handleAgentCommit(payload map[string]interface{}) {
	agentID, _ := payload["agent_id"].(string)
	if agentID == "" {
		return
	}

	agent := r.GetAgent(agentID)
	if agent == nil {
		return
	}

	// Extract commit details from payload
	hash, _ := payload["hash"].(string)
	shortHash, _ := payload["short_hash"].(string)
	message, _ := payload["message"].(string)
	author, _ := payload["author"].(string)
	authorEmail, _ := payload["author_email"].(string)
	timestamp, _ := payload["timestamp"].(string)

	// Extract files_changed as []string
	var filesChanged []string
	if files, ok := payload["files_changed"].([]interface{}); ok {
		for _, f := range files {
			if s, ok := f.(string); ok {
				filesChanged = append(filesChanged, s)
			}
		}
	}

	commit := GitCommit{
		Hash:         hash,
		ShortHash:   shortHash,
		Message:      message,
		Author:       author,
		AuthorEmail:  authorEmail,
		Timestamp:    timestamp,
		FilesChanged: filesChanged,
	}

	agent.Update(func(a *AgentState) {
		a.GitCommits = append(a.GitCommits, commit)
		a.Commits = len(a.GitCommits) // Keep legacy field in sync
	})

	// Update stats to include the new commit
	r.UpdateStats()
}

func (r *RuntimeState) handleAgentMergeStatus(payload map[string]interface{}) {
	agentID, _ := payload["agent_id"].(string)
	if agentID == "" {
		return
	}

	agent := r.GetAgent(agentID)
	if agent == nil {
		return
	}

	// Extract merge status fields
	mergeStatus, _ := payload["merge_status"].(string)
	queuePos, _ := getIntFromPayload(payload, "queue_pos")
	mergeErr, _ := payload["error"].(string)

	// Extract merge result fields
	commitsApplied, _ := getIntFromPayload(payload, "commits_applied")
	hadConflict, _ := payload["had_conflict"].(bool)
	resolverSpawned, _ := payload["resolver_spawned"].(bool)

	// Extract validation fields
	validationStatus, _ := payload["validation_status"].(string)
	validationError, _ := payload["validation_error"].(string)
	validationDuration, _ := getInt64FromPayload(payload, "validation_duration_ms")

	// Extract validation steps (comes as []interface{} from JSON)
	var validationSteps []ValidationStep
	if stepsRaw, ok := payload["validation_steps"].([]interface{}); ok {
		for _, stepRaw := range stepsRaw {
			if stepMap, ok := stepRaw.(map[string]interface{}); ok {
				step := ValidationStep{
					Name:   getStringFromMap(stepMap, "name"),
					Status: getStringFromMap(stepMap, "status"),
					Output: getStringFromMap(stepMap, "output"),
				}
				if dur, ok := getInt64FromMap(stepMap, "duration_ms"); ok {
					step.Duration = dur
				}
				validationSteps = append(validationSteps, step)
			}
		}
	} else if stepsTyped, ok := payload["validation_steps"].([]ValidationStep); ok {
		// Direct type assertion if already typed (e.g., from internal events)
		validationSteps = stepsTyped
	}

	// Extract repair tracking fields
	repairAttempts, _ := getIntFromPayload(payload, "repair_attempts")
	lastRepairOutput, _ := payload["last_repair_output"].(string)

	// Get taskID before update (immutable after creation, but read under lock for safety)
	agent.mu.RLock()
	taskID := agent.TaskID
	agent.mu.RUnlock()

	now := time.Now()
	agent.Update(func(a *AgentState) {
		a.MergeStatus = MergeStatus(mergeStatus)
		a.MergeQueuePos = queuePos
		a.MergeError = mergeErr

		// Update agent status atomically with merge status for terminal states.
		// This prevents the race condition where agent appears 'running' after
		// merge completes but before EventAgentCompleted is processed.
		switch MergeStatus(mergeStatus) {
		case MergeStatusMerged, MergeStatusMergedNeedsRepair, MergeStatusResolved, MergeStatusSkipped:
			// Merge succeeded (with or without repair/resolution)
			a.Status = AgentStatusCompleted
			if a.EndTime == nil {
				a.EndTime = &now
			}
		case MergeStatusFailed:
			// Merge failed
			a.Status = AgentStatusFailed
			if a.EndTime == nil {
				a.EndTime = &now
			}
		}

		// Update merge result fields (only if present to avoid overwriting)
		if commitsApplied > 0 {
			a.Commits = commitsApplied
		}
		// Store conflict info in merge error if there was a conflict
		if hadConflict && a.MergeError == "" {
			a.MergeError = "merge had conflicts"
		}
		// Note: resolverSpawned is informational, no field to store it currently
		_ = resolverSpawned

		// Update validation fields
		if validationStatus != "" {
			a.ValidationStatus = validationStatus
		}
		if validationError != "" {
			a.ValidationError = validationError
		}
		if validationDuration > 0 {
			a.ValidationDuration = validationDuration
		}
		if len(validationSteps) > 0 {
			a.ValidationSteps = validationSteps
		}

		// Update repair tracking fields
		if repairAttempts > 0 {
			a.RepairAttempts = repairAttempts
		}
		if lastRepairOutput != "" {
			a.LastRepairOutput = lastRepairOutput
		}
	})

	// Update lifecycle state machine (parallel with legacy fields)
	if agent.Lifecycle != nil {
		transitionLifecycleForMergeStatus(agent, mergeStatus, hadConflict, validationStatus, repairAttempts)
	}

	// Check for divergence after all updates
	checkLifecycleDivergence(agent, "merge_status_"+mergeStatus)

	// Update task status for terminal merge states
	if taskID != "" {
		switch MergeStatus(mergeStatus) {
		case MergeStatusMerged, MergeStatusMergedNeedsRepair, MergeStatusResolved, MergeStatusSkipped:
			r.UpdateTaskStatus(taskID, "completed", agentID)
		case MergeStatusFailed:
			r.UpdateTaskStatus(taskID, "failed", agentID)
		}
	}

	// Recalculate stats to reflect agent completion
	r.UpdateStats()
}

// transitionLifecycleForMergeStatus maps merge status changes to lifecycle events.
// Called during handleAgentMergeStatus to keep lifecycle state machine in sync.
func transitionLifecycleForMergeStatus(agent *AgentState, mergeStatus string, hadConflict bool, validationStatus string, repairAttempts int) {
	if agent.Lifecycle == nil {
		return
	}

	agentID := agent.ID
	lc := agent.Lifecycle

	// Build transition context
	ctx := lifecycle.TransitionContext{
		RepairAttempt:     repairAttempts,
		MaxRepairAttempts: 3, // Default, would need to get from config
	}

	// Determine the appropriate lifecycle event based on merge status
	var event lifecycle.AgentEvent
	var shouldTransition bool

	switch MergeStatus(mergeStatus) {
	case MergeStatusPending:
		// Agent work complete, now queued for merge
		if lc.State() == lifecycle.StateRunning {
			event = lifecycle.EventWorkComplete
			shouldTransition = true
		}

	case MergeStatusAcquiring, MergeStatusMerging:
		// Merge started
		if lc.State() == lifecycle.StateQueuedForMerge {
			event = lifecycle.EventMergeStarted
			shouldTransition = true
		}

	case MergeStatusResolving:
		// Merge had conflict, now resolving
		if lc.State() == lifecycle.StateMerging && hadConflict {
			event = lifecycle.EventMergeConflict
			shouldTransition = true
		}

	case MergeStatusMerged, MergeStatusResolved, MergeStatusSkipped:
		// Handle based on current state and validation status
		currentState := lc.State()

		// If we're in merging state and merge succeeded
		if currentState == lifecycle.StateMerging {
			event = lifecycle.EventMergeSuccess
			ctx.ValidationEnabled = validationStatus != "" && validationStatus != "skipped"
			shouldTransition = true
		} else if currentState == lifecycle.StateResolving {
			event = lifecycle.EventResolveSuccess
			ctx.ValidationEnabled = validationStatus != "" && validationStatus != "skipped"
			shouldTransition = true
		} else if currentState == lifecycle.StateValidating {
			// Validation completed
			if validationStatus == "passed" {
				event = lifecycle.EventValidationPassed
				shouldTransition = true
			} else if validationStatus == "skipped" {
				event = lifecycle.EventValidationSkipped
				shouldTransition = true
			}
		} else if currentState == lifecycle.StateRepairing {
			// Repair complete, back to validation
			event = lifecycle.EventRepairComplete
			shouldTransition = true
		}

	case MergeStatusMergedNeedsRepair:
		// Validation failed but kept the merge
		if lc.State() == lifecycle.StateValidating {
			event = lifecycle.EventValidationFailed
			ctx.RepairEnabled = false // No more repair attempts
			ctx.StrictMode = false     // Lenient mode keeps merge
			shouldTransition = true
		}

	case MergeStatusFailed:
		// Merge failed
		currentState := lc.State()
		if currentState == lifecycle.StateMerging {
			event = lifecycle.EventMergeFailed
			shouldTransition = true
		} else if currentState == lifecycle.StateResolving {
			event = lifecycle.EventResolveFailed
			shouldTransition = true
		} else if currentState == lifecycle.StateValidating {
			event = lifecycle.EventValidationFailed
			ctx.StrictMode = true // Strict mode fails on validation failure
			shouldTransition = true
		}
	}

	// Also handle validation status transitions if merge is already complete
	if !shouldTransition && validationStatus != "" {
		currentState := lc.State()
		switch validationStatus {
		case "running":
			// Validation starting - should already be in validating state
		case "passed":
			if currentState == lifecycle.StateValidating {
				event = lifecycle.EventValidationPassed
				shouldTransition = true
			}
		case "failed":
			if currentState == lifecycle.StateValidating {
				event = lifecycle.EventValidationFailed
				ctx.RepairEnabled = repairAttempts > 0
				shouldTransition = true
			}
		case "repairing":
			if currentState == lifecycle.StateValidating {
				event = lifecycle.EventValidationFailed
				ctx.RepairEnabled = true
				ctx.RepairAttempt = repairAttempts
				shouldTransition = true
			}
		}
	}

	if shouldTransition {
		if err := lc.Transition(event, ctx); err != nil {
			logging.Warn("lifecycle transition failed on merge status",
				"agent_id", agentID,
				"event", event,
				"merge_status", mergeStatus,
				"current_state", lc.State(),
				"error", err,
			)
		}
	}
}

// handleLifecycleStateChanged updates the agent's lifecycle state when notified
// of a lifecycle state transition. This keeps the RuntimeState in sync with
// the lifecycle state machine's transitions for real-time UI updates.
func (r *RuntimeState) handleLifecycleStateChanged(payload map[string]interface{}) {
	agentID, _ := payload["agent_id"].(string)
	if agentID == "" {
		return
	}

	lifecycleState, _ := payload["lifecycle_state"].(string)
	if lifecycleState == "" {
		return
	}

	agent := r.GetAgent(agentID)
	if agent == nil {
		return
	}

	agent.Update(func(a *AgentState) {
		a.LifecycleState = lifecycleState
	})
}

func (r *RuntimeState) handleAgentCompleted(payload map[string]interface{}, timestamp time.Time) {
	agentID, _ := payload["agent_id"].(string)
	if agentID == "" {
		return
	}

	agent := r.GetAgent(agentID)
	if agent == nil {
		return
	}

	agent.Update(func(a *AgentState) {
		a.EndTime = &timestamp

		// Check if this is a failure (has error field)
		if errMsg, ok := payload["error"].(string); ok && errMsg != "" {
			a.Status = AgentStatusFailed
			a.Error = errMsg
		} else {
			a.Status = AgentStatusCompleted
		}

		// Extract result fields
		if exitCode, ok := getIntFromPayload(payload, "exit_code"); ok {
			a.ExitCode = exitCode
		}
		if duration, ok := payload["duration"].(float64); ok {
			a.Duration = duration
		}
		if durationMS, ok := getInt64FromPayload(payload, "duration_ms"); ok {
			a.DurationMS = durationMS
		}
		if durationAPIMS, ok := getInt64FromPayload(payload, "duration_api_ms"); ok {
			a.DurationAPIMS = durationAPIMS
		}
		if numTurns, ok := getIntFromPayload(payload, "num_turns"); ok {
			a.NumTurns = numTurns
		}
		if inputTokens, ok := getIntFromPayload(payload, "input_tokens"); ok {
			a.TokenUsage.InputTokens = inputTokens
		}
		if outputTokens, ok := getIntFromPayload(payload, "output_tokens"); ok {
			a.TokenUsage.OutputTokens = outputTokens
		}
		if cacheCreation, ok := getIntFromPayload(payload, "cache_creation_input_tokens"); ok {
			a.TokenUsage.CacheCreationInputTokens = cacheCreation
		}
		if cacheRead, ok := getIntFromPayload(payload, "cache_read_input_tokens"); ok {
			a.TokenUsage.CacheReadInputTokens = cacheRead
		}
		a.TokenUsage.TotalTokens = a.TokenUsage.InputTokens + a.TokenUsage.OutputTokens
		if costUSD, ok := payload["cost_usd"].(float64); ok {
			a.TokenUsage.CostUSD = costUSD
		}
		if filesChanged, ok := getIntFromPayload(payload, "files_changed"); ok {
			a.Changes = filesChanged
		}
		if commitsCreated, ok := getIntFromPayload(payload, "commits_created"); ok {
			a.Commits = commitsCreated
		}
		if resultMessage, ok := payload["result_message"].(string); ok {
			a.ResultMessage = resultMessage
		}
		if sessionID, ok := payload["session_id"].(string); ok {
			a.SessionID = sessionID
		}

		// Parse model usage if present
		if modelUsageRaw, ok := payload["model_usage"].(map[string]interface{}); ok {
			a.TokenUsage.ModelUsage = make(map[string]ModelUsageData)
			for model, usageRaw := range modelUsageRaw {
				if usage, ok := usageRaw.(map[string]interface{}); ok {
					data := ModelUsageData{}
					if v, ok := getIntFromPayload(usage, "input_tokens"); ok {
						data.InputTokens = v
					}
					if v, ok := getIntFromPayload(usage, "output_tokens"); ok {
						data.OutputTokens = v
					}
					if v, ok := getIntFromPayload(usage, "cache_read_input_tokens"); ok {
						data.CacheReadInputTokens = v
					}
					if v, ok := getIntFromPayload(usage, "cache_creation_input_tokens"); ok {
						data.CacheCreationInputTokens = v
					}
					if v, ok := usage["cost_usd"].(float64); ok {
						data.CostUSD = v
					}
					a.TokenUsage.ModelUsage[model] = data
				}
			}
		}
	})

	// Update lifecycle state machine for completion (parallel with legacy fields)
	// Note: Many completion events are already handled via merge status events,
	// but agents can also complete directly (e.g., work failed, cancelled, timeout).
	if agent.Lifecycle != nil {
		transitionLifecycleForCompletion(agent, payload)
	}

	// Check for divergence after all updates
	checkLifecycleDivergence(agent, "agent_completed")

	// Update task status
	if agent.TaskID != "" {
		status := "completed"
		if agent.Status == AgentStatusFailed {
			status = "failed"
		}
		r.UpdateTaskStatus(agent.TaskID, status, agentID)
	}

	r.UpdateStats()
}

// transitionLifecycleForCompletion handles lifecycle transitions for agent completion.
// Called during handleAgentCompleted to keep lifecycle state machine in sync.
func transitionLifecycleForCompletion(agent *AgentState, payload map[string]interface{}) {
	if agent.Lifecycle == nil {
		return
	}

	agentID := agent.ID
	lc := agent.Lifecycle
	currentState := lc.State()

	// Skip if already in a terminal state
	if currentState.IsTerminal() {
		return
	}

	// Determine the appropriate lifecycle event
	var event lifecycle.AgentEvent
	var shouldTransition bool
	ctx := lifecycle.TransitionContext{}

	// Check if this is an error completion
	errMsg, hasError := payload["error"].(string)
	if hasError && errMsg != "" {
		ctx.Error = errMsg
		// Work failed while running
		if currentState == lifecycle.StateRunning {
			event = lifecycle.EventWorkFailed
			ctx.AttemptsRemaining = 0 // No retries for direct failure
			shouldTransition = true
		}
	}

	// If not an error and lifecycle isn't already terminal/advanced past running,
	// we don't need to transition here - the merge status handler will do it.
	// This is because agent_completed events often arrive after merge completion.

	if shouldTransition {
		if err := lc.Transition(event, ctx); err != nil {
			logging.Warn("lifecycle transition failed on agent completed",
				"agent_id", agentID,
				"event", event,
				"current_state", currentState,
				"error", err,
			)
		}
	}
}

func (r *RuntimeState) handleTaskUpdated(payload map[string]interface{}) {
	taskID, _ := payload["id"].(string)
	if taskID == "" {
		return
	}

	status, _ := payload["status"].(string)
	agentID, _ := payload["agent_id"].(string)
	title, _ := payload["title"].(string)

	r.mu.Lock()
	defer r.mu.Unlock()

	task, exists := r.Tasks[taskID]
	if !exists {
		// Create new task entry
		task = &TaskState{
			ID:    taskID,
			Title: title,
		}
		r.Tasks[taskID] = task
	}

	// Update task fields
	if status != "" {
		task.Status = status
	}
	if agentID != "" {
		task.AgentID = agentID
	}
	if title != "" {
		task.Title = title
	}
}
