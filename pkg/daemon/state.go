package daemon

import (
	"sync"
	"time"

	"github.com/jzila/canopy/pkg/beads"
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

// LiveFeedEvent represents a real-time event from the Claude API
type LiveFeedEvent struct {
	EventType string                 `json:"event_type"` // "tool_use", "text", "file_change", etc.
	Data      map[string]interface{} `json:"data"`       // Event-specific data
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

// TokenUsage tracks token consumption and cost for an agent execution
type TokenUsage struct {
	InputTokens              int                       `json:"input_tokens"`
	OutputTokens             int                       `json:"output_tokens"`
	CacheCreationInputTokens int                       `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int                       `json:"cache_read_input_tokens"`
	TotalTokens              int                       `json:"total_tokens"`
	CostUSD                  float64                   `json:"cost_usd"`
	ModelUsage               map[string]ModelUsageData `json:"model_usage,omitempty"`
}

// ModelUsageData represents per-model token usage and cost
type ModelUsageData struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	CostUSD                  float64 `json:"cost_usd"`
}

// AgentState tracks the state of a single agent execution
type AgentState struct {
	ID              string          `json:"id"`                // Unique agent ID
	TaskID          string          `json:"task_id"`           // Beads task ID
	TaskTitle       string          `json:"task_title"`        // Task title for display
	Status          AgentStatus     `json:"status"`            // Current agent status
	StartTime       time.Time       `json:"start_time"`        // When agent started
	EndTime         *time.Time      `json:"end_time"`          // When agent finished (nil if running)
	Duration        float64         `json:"duration"`          // Execution duration in seconds
	DurationMS      int64           `json:"duration_ms"`       // Execution duration in milliseconds (from Claude)
	DurationAPIMS   int64           `json:"duration_api_ms"`   // API duration in milliseconds
	NumTurns        int             `json:"num_turns"`         // Number of agentic turns
	Output          OutputBuffer    `json:"output"`            // Stdout/stderr buffers
	LiveFeedEvents  []LiveFeedEvent `json:"live_feed_events"`  // Real-time events from Claude API
	TokenUsage      TokenUsage      `json:"token_usage"`       // Token consumption stats
	ExitCode        int             `json:"exit_code"`         // Process exit code
	Error           string          `json:"error"`             // Error message if failed
	Changes         int             `json:"changes"`           // Number of files changed
	Commits         int             `json:"commits"`           // Number of git commits made (legacy, use len(GitCommits))
	GitCommits      []GitCommit     `json:"git_commits"`       // Detailed git commit history
	ResultMessage   string          `json:"result_message"`    // Final result message from Claude
	mu              sync.RWMutex
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
	AgentID      string   `json:"agent_id"` // ID of agent executing this task
	Priority     int      `json:"priority"`
	Dependencies []string `json:"dependencies"` // Task IDs this task depends on
	Archived     bool     `json:"archived"`     // Whether the task is archived
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
	Agents    map[string]*AgentState `json:"agents"`     // Agent ID -> AgentState
	Tasks     map[string]*TaskState  `json:"tasks"`      // Task ID -> TaskState
	Stats     Stats                  `json:"stats"`      // Aggregate statistics
	IsPaused  bool                   `json:"is_paused"`  // Whether orchestration is paused
	StartTime time.Time              `json:"start_time"` // When orchestration started
	mu        sync.RWMutex
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Tasks[task.ID] = &TaskState{
		ID:           task.ID,
		Title:        task.Title,
		Status:       task.Status,
		Priority:     task.Priority,
		Dependencies: task.GetDependencies(),
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
		case AgentStatusRunning:
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
	case EventAgentStarted:
		r.handleAgentStarted(payload, event.Timestamp)
	case EventAgentOutput:
		r.handleAgentOutput(payload)
	case EventAgentLiveFeed:
		r.handleAgentLiveFeed(payload)
	case EventAgentCommit:
		r.handleAgentCommit(payload)
	case EventAgentCompleted:
		r.handleAgentCompleted(payload, event.Timestamp)
	case EventStatsUpdated:
		// Stats updates are informational, we recalculate from agents
		r.UpdateStats()
	}
}

func (r *RuntimeState) handleAgentStarted(payload map[string]interface{}, timestamp time.Time) {
	agentID, _ := payload["agent_id"].(string)
	taskID, _ := payload["task_id"].(string)
	taskTitle, _ := payload["task_title"].(string)

	if agentID == "" {
		return
	}

	agent := &AgentState{
		ID:        agentID,
		TaskID:    taskID,
		TaskTitle: taskTitle,
		Status:    AgentStatusRunning,
		StartTime: timestamp,
	}

	r.AddAgent(agent)

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

	// Create and append the live feed event
	liveFeedEvent := LiveFeedEvent{
		EventType: eventType,
		Data:      data,
	}

	agent.Update(func(a *AgentState) {
		a.LiveFeedEvents = append(a.LiveFeedEvents, liveFeedEvent)
	})
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
