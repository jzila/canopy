package beads

import (
	"context"
	"fmt"
	"sync"
)

// MockClient is a mock implementation of BeadsClient for testing.
// It stores tasks in memory and allows tests to verify method calls.
type MockClient struct {
	mu sync.RWMutex

	// Tasks stores mock tasks by ID
	Tasks map[string]*Task

	// ReadyTasks is the list of tasks returned by Ready()
	ReadyTasks []Task

	// Calls tracks method invocations for verification
	Calls struct {
		Ready         int
		List          int
		ReadyWithArgs [][]string
		Show          []string
		Start         []string
		Done          []string
		Fail          []struct {
			TaskID string
			Reason string
		}
		NeedsInput []struct {
			TaskID    string
			SessionID string
			Reason    string
		}
		Create []struct {
			Title    string
			Priority int
		}
		CreateWithDescription []struct {
			Title       string
			Description string
			Priority    int
		}
		AddDep []struct {
			Child  string
			Parent string
		}
		GetDeps    []string
		Sync       int
		AddComment []struct {
			TaskID  string
			Comment string
		}
	}

	// Errors allows tests to inject errors for specific methods
	Errors struct {
		Ready                 error
		List                  error
		ReadyWithArgs         error
		Show                  error
		Start                 error
		Done                  error
		Fail                  error
		NeedsInput            error
		Create                error
		CreateWithDescription error
		AddDep                error
		GetDeps               error
		Sync                  error
		AddComment            error
	}

	// NextCreateID is the ID to return from the next Create call
	NextCreateID string
}

// NewMockClient creates a new MockClient with initialized maps
func NewMockClient() *MockClient {
	return &MockClient{
		Tasks:        make(map[string]*Task),
		NextCreateID: "mock-task-1",
	}
}

// Ensure MockClient implements BeadsClient
var _ BeadsClient = (*MockClient)(nil)

// Ready returns ReadyTasks or the configured error
func (m *MockClient) Ready(_ context.Context) ([]Task, error) {
	m.mu.Lock()
	m.Calls.Ready++
	m.mu.Unlock()

	if m.Errors.Ready != nil {
		return nil, m.Errors.Ready
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ReadyTasks, nil
}

// List returns all tasks or the configured error
func (m *MockClient) List(_ context.Context) ([]Task, error) {
	m.mu.Lock()
	m.Calls.List++
	m.mu.Unlock()

	if m.Errors.List != nil {
		return nil, m.Errors.List
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var tasks []Task
	for _, t := range m.Tasks {
		tasks = append(tasks, *t)
	}
	return tasks, nil
}

// ReadyWithArgs returns ReadyTasks and records the args
func (m *MockClient) ReadyWithArgs(_ context.Context, args ...string) ([]Task, error) {
	m.mu.Lock()
	m.Calls.ReadyWithArgs = append(m.Calls.ReadyWithArgs, args)
	m.mu.Unlock()

	if m.Errors.ReadyWithArgs != nil {
		return nil, m.Errors.ReadyWithArgs
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ReadyTasks, nil
}

// Show returns the task with the given ID or an error
func (m *MockClient) Show(_ context.Context, taskID string) (*Task, error) {
	m.mu.Lock()
	m.Calls.Show = append(m.Calls.Show, taskID)
	m.mu.Unlock()

	if m.Errors.Show != nil {
		return nil, m.Errors.Show
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if task, ok := m.Tasks[taskID]; ok {
		return task, nil
	}
	return nil, fmt.Errorf("task not found: %s", taskID)
}

// Start marks a task as in-progress
func (m *MockClient) Start(_ context.Context, taskID string) error {
	m.mu.Lock()
	m.Calls.Start = append(m.Calls.Start, taskID)
	m.mu.Unlock()

	if m.Errors.Start != nil {
		return m.Errors.Start
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if task, ok := m.Tasks[taskID]; ok {
		task.Status = "in_progress"
	}
	return nil
}

// Done marks a task as completed
func (m *MockClient) Done(_ context.Context, taskID string) error {
	m.mu.Lock()
	m.Calls.Done = append(m.Calls.Done, taskID)
	m.mu.Unlock()

	if m.Errors.Done != nil {
		return m.Errors.Done
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if task, ok := m.Tasks[taskID]; ok {
		task.Status = "closed"
	}
	return nil
}

// Fail marks a task as failed by resetting it to open status so it can be retried
func (m *MockClient) Fail(_ context.Context, taskID string, reason string) error {
	m.mu.Lock()
	m.Calls.Fail = append(m.Calls.Fail, struct {
		TaskID string
		Reason string
	}{taskID, reason})
	m.mu.Unlock()

	if m.Errors.Fail != nil {
		return m.Errors.Fail
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if task, ok := m.Tasks[taskID]; ok {
		task.Status = "open"
	}
	return nil
}

// NeedsInput marks a task as needing user input
func (m *MockClient) NeedsInput(_ context.Context, taskID string, sessionID string, reason string) error {
	m.mu.Lock()
	m.Calls.NeedsInput = append(m.Calls.NeedsInput, struct {
		TaskID    string
		SessionID string
		Reason    string
	}{taskID, sessionID, reason})
	m.mu.Unlock()

	if m.Errors.NeedsInput != nil {
		return m.Errors.NeedsInput
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if task, ok := m.Tasks[taskID]; ok {
		task.Status = "needs-input"
	}
	return nil
}

// Create creates a new task
func (m *MockClient) Create(_ context.Context, title string, priority int) (string, error) {
	m.mu.Lock()
	m.Calls.Create = append(m.Calls.Create, struct {
		Title    string
		Priority int
	}{title, priority})
	id := m.NextCreateID
	m.mu.Unlock()

	if m.Errors.Create != nil {
		return "", m.Errors.Create
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.Tasks[id] = &Task{
		ID:       id,
		Title:    title,
		Priority: priority,
		Status:   "open",
	}
	return id, nil
}

// CreateWithDescription creates a new task with a description
func (m *MockClient) CreateWithDescription(_ context.Context, title, description string, priority int) (string, error) {
	m.mu.Lock()
	m.Calls.CreateWithDescription = append(m.Calls.CreateWithDescription, struct {
		Title       string
		Description string
		Priority    int
	}{title, description, priority})
	id := m.NextCreateID
	m.mu.Unlock()

	if m.Errors.CreateWithDescription != nil {
		return "", m.Errors.CreateWithDescription
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.Tasks[id] = &Task{
		ID:          id,
		Title:       title,
		Description: description,
		Priority:    priority,
		Status:      "open",
	}
	return id, nil
}

// AddDep adds a dependency
func (m *MockClient) AddDep(_ context.Context, child, parent string) error {
	m.mu.Lock()
	m.Calls.AddDep = append(m.Calls.AddDep, struct {
		Child  string
		Parent string
	}{child, parent})
	m.mu.Unlock()

	if m.Errors.AddDep != nil {
		return m.Errors.AddDep
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if task, ok := m.Tasks[child]; ok {
		task.Blockers = append(task.Blockers, parent)
	}
	return nil
}

// GetDeps returns the dependencies for a task
func (m *MockClient) GetDeps(_ context.Context, taskID string) ([]string, error) {
	m.mu.Lock()
	m.Calls.GetDeps = append(m.Calls.GetDeps, taskID)
	m.mu.Unlock()

	if m.Errors.GetDeps != nil {
		return nil, m.Errors.GetDeps
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if task, ok := m.Tasks[taskID]; ok {
		deps := task.Blockers
		if len(deps) == 0 {
			deps = task.BlockedBy
		}
		return deps, nil
	}
	return nil, fmt.Errorf("task not found: %s", taskID)
}

// Sync is a no-op in the mock
func (m *MockClient) Sync(_ context.Context) error {
	m.mu.Lock()
	m.Calls.Sync++
	m.mu.Unlock()

	return m.Errors.Sync
}

// AddComment adds a comment to a task
func (m *MockClient) AddComment(_ context.Context, taskID, comment string) error {
	m.mu.Lock()
	m.Calls.AddComment = append(m.Calls.AddComment, struct {
		TaskID  string
		Comment string
	}{taskID, comment})
	m.mu.Unlock()

	return m.Errors.AddComment
}

// SetTask adds or updates a task in the mock
func (m *MockClient) SetTask(task *Task) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Tasks[task.ID] = task
}

// SetReadyTasks sets the tasks returned by Ready()
func (m *MockClient) SetReadyTasks(tasks []Task) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReadyTasks = tasks
}

// Reset clears all recorded calls and errors
func (m *MockClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls.Ready = 0
	m.Calls.List = 0
	m.Calls.ReadyWithArgs = nil
	m.Calls.Show = nil
	m.Calls.Start = nil
	m.Calls.Done = nil
	m.Calls.Fail = nil
	m.Calls.NeedsInput = nil
	m.Calls.Create = nil
	m.Calls.CreateWithDescription = nil
	m.Calls.AddDep = nil
	m.Calls.GetDeps = nil
	m.Calls.Sync = 0
	m.Calls.AddComment = nil

	m.Errors.Ready = nil
	m.Errors.List = nil
	m.Errors.ReadyWithArgs = nil
	m.Errors.Show = nil
	m.Errors.Start = nil
	m.Errors.Done = nil
	m.Errors.Fail = nil
	m.Errors.NeedsInput = nil
	m.Errors.Create = nil
	m.Errors.CreateWithDescription = nil
	m.Errors.AddDep = nil
	m.Errors.GetDeps = nil
	m.Errors.Sync = nil
	m.Errors.AddComment = nil
}
