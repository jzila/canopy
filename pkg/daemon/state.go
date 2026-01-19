package daemon

import (
	"sync"
	"time"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/logging"
	"github.com/jzila/canopy/pkg/metrics"
	"github.com/jzila/canopy/pkg/persistence"
	"github.com/jzila/canopy/pkg/types"
)

// AgentStatus represents the current state of an agent
type AgentStatus string

const (
	AgentStatusStarting  AgentStatus = "starting"
	AgentStatusRunning   AgentStatus = "running"
	AgentStatusCompleted AgentStatus = "completed"
	AgentStatusFailed    AgentStatus = "failed"
	AgentStatusTimedOut  AgentStatus = "timed_out"
	AgentStatusCancelled AgentStatus = "cancelled"
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
	MergeStatusNone      = types.MergeStatusNone
	MergeStatusPending   = types.MergeStatusPending
	MergeStatusAcquiring = types.MergeStatusAcquiring
	MergeStatusMerging   = types.MergeStatusMerging
	MergeStatusResolving = types.MergeStatusResolving
	MergeStatusMerged    = types.MergeStatusMerged
	MergeStatusFailed    = types.MergeStatusFailed
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
	Status          AgentStatus     `json:"status"`                     // Current agent status
	MergeStatus     MergeStatus     `json:"merge_status,omitempty"`     // Current merge queue status
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
	mu                 sync.RWMutex
}

// Update atomically updates agent state fields
func (a *AgentState) Update(fn func(*AgentState)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fn(a)
}

// GetSnapshot returns a copy of the agent state (thread-safe)
func (a *AgentState) GetSnapshot() AgentState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return *a
}

// TaskState represents the state of a task in the orchestration
type TaskState struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`   // ready, in_progress, completed, failed
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
	Tasks        map[string]*TaskState  `json:"tasks"`         // Task ID -> TaskState
	Stats        Stats                  `json:"stats"`         // Aggregate statistics
	IsPaused     bool                   `json:"is_paused"`     // Whether orchestration is paused
	StartTime    time.Time              `json:"start_time"`    // When orchestration started
	CurrentRunID string                 `json:"current_run_id"` // Current run ID for new agents
	mu           sync.RWMutex
}

// NewRuntimeState creates a new runtime state tracker
func NewRuntimeState() *RuntimeState {
	return &RuntimeState{
		Agents:    make(map[string]*AgentState),
		Tasks:     make(map[string]*TaskState),
		StartTime: time.Now(),
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

// AddTaskWithRepo registers a task in the state with an associated repository ID
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

	r.Tasks[task.ID] = &TaskState{
		ID:           task.ID,
		Title:        task.Title,
		Status:       task.Status,
		Priority:     task.Priority,
		Dependencies: task.GetDependencies(),
		RepoID:       repoID,
		UpdatedAt:    updatedAt,
	}
}

// ClearTasksForRepo removes all tasks associated with a specific repository.
// If repoID is empty, clears all tasks.
func (r *RuntimeState) ClearTasksForRepo(repoID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if repoID == "" {
		r.Tasks = make(map[string]*TaskState)
		return
	}
	for id, task := range r.Tasks {
		if task.RepoID == repoID {
			delete(r.Tasks, id)
		}
	}
}

// UpdateTaskStatus updates the status of a task
func (r *RuntimeState) UpdateTaskStatus(taskID, status, agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if task, exists := r.Tasks[taskID]; exists {
		task.Status = status
		task.AgentID = agentID
	}
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

// GetSnapshot returns a complete snapshot of the runtime state (thread-safe)
func (r *RuntimeState) GetSnapshot() RuntimeState {
	// Recalculate stats before taking snapshot to ensure they're up to date
	r.UpdateStats()

	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot := RuntimeState{
		Agents:    make(map[string]*AgentState),
		Tasks:     make(map[string]*TaskState),
		Stats:     r.Stats,
		IsPaused:  r.IsPaused,
		StartTime: r.StartTime,
	}

	// Deep copy agents
	for id, agent := range r.Agents {
		agentCopy := agent.GetSnapshot()
		snapshot.Agents[id] = &agentCopy
	}

	// Deep copy tasks
	for id, task := range r.Tasks {
		taskCopy := *task
		if task.Dependencies != nil {
			taskCopy.Dependencies = make([]string, len(task.Dependencies))
			copy(taskCopy.Dependencies, task.Dependencies)
		}
		snapshot.Tasks[id] = &taskCopy
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
func (r *RuntimeState) GetSnapshotForRepo(repoID string) RuntimeState {
	if repoID == "" {
		return r.GetSnapshot()
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot := RuntimeState{
		Agents:    make(map[string]*AgentState),
		Tasks:     make(map[string]*TaskState),
		IsPaused:  r.IsPaused,
		StartTime: r.StartTime,
	}

	// Filter and deep copy agents (note: agents don't have repo_id in struct yet,
	// but they are associated with tasks that do)
	// For now, we include all agents since agent<->repo mapping requires task lookup
	for id, agent := range r.Agents {
		agentCopy := agent.GetSnapshot()
		snapshot.Agents[id] = &agentCopy
	}

	// Filter and deep copy tasks by repo
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

	// Recalculate stats for filtered data
	snapshot.recalculateStats()

	return snapshot
}

// recalculateStats calculates aggregate statistics from agents in the snapshot
func (r *RuntimeState) recalculateStats() {
	stats := Stats{}
	var totalDuration float64
	completedCount := 0
	var allCommits []GitCommit

	for _, agent := range r.Agents {
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

	stats.TotalTasks = len(r.Agents)
	stats.TotalDuration = totalDuration
	if completedCount > 0 {
		stats.AverageDuration = totalDuration / float64(completedCount)
	}
	stats.AllGitCommits = allCommits

	r.Stats = stats
}

// SubscribeToEventBus subscribes to the EventBus and updates state from events.
// Returns an unsubscribe function. This bridges IPC events to RuntimeState updates.
func (r *RuntimeState) SubscribeToEventBus(eventBus *EventBus) func() {
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

	if agentID == "" {
		return
	}

	// Use run_id from payload if provided, otherwise fall back to CurrentRunID
	if runID == "" {
		r.mu.RLock()
		runID = r.CurrentRunID
		r.mu.RUnlock()
	}

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
	}

	r.AddAgent(agent)

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

	agent.Update(func(a *AgentState) {
		a.MergeStatus = MergeStatus(mergeStatus)
		a.MergeQueuePos = queuePos
		a.MergeError = mergeErr
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
