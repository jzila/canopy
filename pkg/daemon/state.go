package daemon

import (
	"sync"
	"time"

	"github.com/john/canopy/pkg/beads"
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
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// AgentState tracks the state of a single agent execution
type AgentState struct {
	ID         string       `json:"id"`          // Unique agent ID
	TaskID     string       `json:"task_id"`     // Beads task ID
	TaskTitle  string       `json:"task_title"`  // Task title for display
	Status     AgentStatus  `json:"status"`      // Current agent status
	StartTime  time.Time    `json:"start_time"`  // When agent started
	EndTime    *time.Time   `json:"end_time"`    // When agent finished (nil if running)
	Duration   float64      `json:"duration"`    // Execution duration in seconds
	Output     OutputBuffer `json:"output"`      // Stdout/stderr buffers
	TokenUsage TokenUsage   `json:"token_usage"` // Token consumption stats
	ExitCode   int          `json:"exit_code"`   // Process exit code
	Error      string       `json:"error"`       // Error message if failed
	Changes    int          `json:"changes"`     // Number of files changed
	Commits    int          `json:"commits"`     // Number of git commits made
	mu         sync.RWMutex
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
	TotalTasks      int     `json:"total_tasks"`
	CompletedTasks  int     `json:"completed_tasks"`
	FailedTasks     int     `json:"failed_tasks"`
	RunningTasks    int     `json:"running_tasks"`
	TotalTokens     int     `json:"total_tokens"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
	TotalDuration   float64 `json:"total_duration"` // Total execution time in seconds
	AverageDuration float64 `json:"avg_duration"`   // Average task duration
	FileChanges     int     `json:"file_changes"`   // Total files changed
	GitCommits      int     `json:"git_commits"`    // Total commits made
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

		stats.TotalTokens += agent.TokenUsage.TotalTokens
		stats.TotalCostUSD += agent.TokenUsage.CostUSD
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
