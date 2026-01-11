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
	Commits         int             `json:"commits"`           // Number of git commits made
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
}

// Stats aggregates statistics across all agents
type Stats struct {
	TotalTasks               int     `json:"total_tasks"`
	CompletedTasks           int     `json:"completed_tasks"`
	FailedTasks              int     `json:"failed_tasks"`
	RunningTasks             int     `json:"running_tasks"`
	TotalInputTokens         int     `json:"total_input_tokens"`
	TotalOutputTokens        int     `json:"total_output_tokens"`
	TotalCacheCreationTokens int     `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens     int     `json:"total_cache_read_tokens"`
	TotalTokens              int     `json:"total_tokens"`
	TotalCostUSD             float64 `json:"total_cost_usd"`
	TotalTurns               int     `json:"total_turns"`
	TotalDuration            float64 `json:"total_duration"` // Total execution time in seconds
	AverageDuration          float64 `json:"avg_duration"`   // Average task duration
	FileChanges              int     `json:"file_changes"`   // Total files changed
	GitCommits               int     `json:"git_commits"`    // Total commits made
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

// UpdateStats recalculates aggregate statistics from all agents
func (r *RuntimeState) UpdateStats() {
	r.mu.Lock()
	defer r.mu.Unlock()

	stats := Stats{}
	var totalDuration float64
	completedCount := 0

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

		agent.mu.RUnlock()
	}

	stats.TotalTasks = len(r.Agents)
	stats.TotalDuration = totalDuration
	if completedCount > 0 {
		stats.AverageDuration = totalDuration / float64(completedCount)
	}

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
		if exitCode, ok := payload["exit_code"].(float64); ok {
			a.ExitCode = int(exitCode)
		}
		if duration, ok := payload["duration"].(float64); ok {
			a.Duration = duration
		}
		if durationMS, ok := payload["duration_ms"].(float64); ok {
			a.DurationMS = int64(durationMS)
		}
		if durationAPIMS, ok := payload["duration_api_ms"].(float64); ok {
			a.DurationAPIMS = int64(durationAPIMS)
		}
		if numTurns, ok := payload["num_turns"].(float64); ok {
			a.NumTurns = int(numTurns)
		}
		if inputTokens, ok := payload["input_tokens"].(float64); ok {
			a.TokenUsage.InputTokens = int(inputTokens)
		}
		if outputTokens, ok := payload["output_tokens"].(float64); ok {
			a.TokenUsage.OutputTokens = int(outputTokens)
		}
		if cacheCreation, ok := payload["cache_creation_input_tokens"].(float64); ok {
			a.TokenUsage.CacheCreationInputTokens = int(cacheCreation)
		}
		if cacheRead, ok := payload["cache_read_input_tokens"].(float64); ok {
			a.TokenUsage.CacheReadInputTokens = int(cacheRead)
		}
		a.TokenUsage.TotalTokens = a.TokenUsage.InputTokens + a.TokenUsage.OutputTokens
		if costUSD, ok := payload["cost_usd"].(float64); ok {
			a.TokenUsage.CostUSD = costUSD
		}
		if filesChanged, ok := payload["files_changed"].(float64); ok {
			a.Changes = int(filesChanged)
		}
		if commitsCreated, ok := payload["commits_created"].(float64); ok {
			a.Commits = int(commitsCreated)
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
					if v, ok := usage["input_tokens"].(float64); ok {
						data.InputTokens = int(v)
					}
					if v, ok := usage["output_tokens"].(float64); ok {
						data.OutputTokens = int(v)
					}
					if v, ok := usage["cache_read_input_tokens"].(float64); ok {
						data.CacheReadInputTokens = int(v)
					}
					if v, ok := usage["cache_creation_input_tokens"].(float64); ok {
						data.CacheCreationInputTokens = int(v)
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
