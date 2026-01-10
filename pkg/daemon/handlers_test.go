package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/beads"
)

// Mock scheduler for testing
type mockScheduler struct {
	paused    bool
	killCalls []string
}

func (m *mockScheduler) Pause() {
	m.paused = true
}

func (m *mockScheduler) Resume() {
	m.paused = false
}

func (m *mockScheduler) IsPaused() bool {
	return m.paused
}

func (m *mockScheduler) Kill(agentID string) error {
	m.killCalls = append(m.killCalls, agentID)
	return nil
}

// Mock beads client for testing
type mockBeadsClient struct {
	createCalls []string
	startCalls  []string
	doneCalls   []string
	failCalls   []string
}

func (m *mockBeadsClient) Create(title string, priority int) (string, error) {
	m.createCalls = append(m.createCalls, title)
	return "test-task-id", nil
}

func (m *mockBeadsClient) Start(taskID string) error {
	m.startCalls = append(m.startCalls, taskID)
	return nil
}

func (m *mockBeadsClient) Done(taskID string) error {
	m.doneCalls = append(m.doneCalls, taskID)
	return nil
}

func (m *mockBeadsClient) Fail(taskID string, reason string) error {
	m.failCalls = append(m.failCalls, taskID)
	return nil
}

func (m *mockBeadsClient) AddDep(child, parent string) error {
	return nil
}

func TestHandleGetState(t *testing.T) {
	state := NewRuntimeState()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient)

	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	w := httptest.NewRecorder()

	handler.HandleGetState(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var result RuntimeState
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
}

func TestHandleGetAgents(t *testing.T) {
	state := NewRuntimeState()

	// Add a test agent
	agent := &AgentState{
		ID:        "agent-1",
		TaskID:    "task-1",
		TaskTitle: "Test Task",
		Status:    AgentStatusRunning,
		StartTime: time.Now(),
	}
	state.AddAgent(agent)

	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient)

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	w := httptest.NewRecorder()

	handler.HandleGetAgents(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var agents []*AgentState
	if err := json.NewDecoder(w.Body).Decode(&agents); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(agents) != 1 {
		t.Errorf("Expected 1 agent, got %d", len(agents))
	}
}

func TestHandleKillAgent(t *testing.T) {
	state := NewRuntimeState()

	// Add a test agent
	agent := &AgentState{
		ID:        "agent-1",
		TaskID:    "task-1",
		TaskTitle: "Test Task",
		Status:    AgentStatusRunning,
		StartTime: time.Now(),
	}
	state.AddAgent(agent)

	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient)

	req := httptest.NewRequest(http.MethodPost, "/api/agents/agent-1/kill", nil)
	w := httptest.NewRecorder()

	handler.HandleKillAgent(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if len(scheduler.killCalls) != 1 {
		t.Errorf("Expected 1 kill call, got %d", len(scheduler.killCalls))
	}
}

func TestHandleGetTasks(t *testing.T) {
	state := NewRuntimeState()

	// Add a test task
	task := &beads.Task{
		ID:       "task-1",
		Title:    "Test Task",
		Status:   "ready",
		Priority: 5,
	}
	state.AddTask(task)

	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	w := httptest.NewRecorder()

	handler.HandleGetTasks(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var tasks []*TaskState
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("Expected 1 task, got %d", len(tasks))
	}
}

func TestHandleCreateTask(t *testing.T) {
	state := NewRuntimeState()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient)

	reqBody := TaskCreateRequest{
		Title:    "New Task",
		Priority: 5,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleCreateTask(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", w.Code)
	}

	if len(beadsClient.createCalls) != 1 {
		t.Errorf("Expected 1 create call, got %d", len(beadsClient.createCalls))
	}
}

func TestHandleUpdateTask(t *testing.T) {
	state := NewRuntimeState()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient)

	reqBody := TaskUpdateRequest{
		Status: "in_progress",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, "/api/tasks?id=task-1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleUpdateTask(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if len(beadsClient.startCalls) != 1 {
		t.Errorf("Expected 1 start call, got %d", len(beadsClient.startCalls))
	}
}

func TestHandlePauseOrch(t *testing.T) {
	state := NewRuntimeState()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient)

	req := httptest.NewRequest(http.MethodPost, "/api/orch/pause", nil)
	w := httptest.NewRecorder()

	handler.HandlePauseOrch(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if !scheduler.paused {
		t.Error("Expected scheduler to be paused")
	}

	if !state.IsPaused {
		t.Error("Expected state to be paused")
	}
}

func TestHandleResumeOrch(t *testing.T) {
	state := NewRuntimeState()
	scheduler := &mockScheduler{paused: true}
	state.Pause()

	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient)

	req := httptest.NewRequest(http.MethodPost, "/api/orch/resume", nil)
	w := httptest.NewRecorder()

	handler.HandleResumeOrch(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if scheduler.paused {
		t.Error("Expected scheduler to be resumed")
	}

	if state.IsPaused {
		t.Error("Expected state to be resumed")
	}
}

func TestHandleGetStats(t *testing.T) {
	state := NewRuntimeState()

	// Add test agents to generate stats
	agent1 := &AgentState{
		ID:        "agent-1",
		TaskID:    "task-1",
		TaskTitle: "Test Task 1",
		Status:    AgentStatusCompleted,
		StartTime: time.Now().Add(-10 * time.Second),
		Duration:  10.0,
	}
	agent1.EndTime = &time.Time{}
	*agent1.EndTime = time.Now()

	state.AddAgent(agent1)
	state.UpdateStats()

	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	w := httptest.NewRecorder()

	handler.HandleGetStats(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var stats Stats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if stats.TotalTasks != 1 {
		t.Errorf("Expected 1 total task, got %d", stats.TotalTasks)
	}

	if stats.CompletedTasks != 1 {
		t.Errorf("Expected 1 completed task, got %d", stats.CompletedTasks)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	state := NewRuntimeState()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient)

	tests := []struct {
		name    string
		method  string
		path    string
		handler func(w http.ResponseWriter, r *http.Request)
	}{
		{"GetState POST", http.MethodPost, "/api/state", handler.HandleGetState},
		{"GetAgents POST", http.MethodPost, "/api/agents", handler.HandleGetAgents},
		{"KillAgent GET", http.MethodGet, "/api/agents/x/kill", handler.HandleKillAgent},
		{"GetTasks POST", http.MethodPost, "/api/tasks", handler.HandleGetTasks},
		{"CreateTask GET", http.MethodGet, "/api/tasks", handler.HandleCreateTask},
		{"UpdateTask GET", http.MethodGet, "/api/tasks", handler.HandleUpdateTask},
		{"PauseOrch GET", http.MethodGet, "/api/orch/pause", handler.HandlePauseOrch},
		{"ResumeOrch GET", http.MethodGet, "/api/orch/resume", handler.HandleResumeOrch},
		{"GetStats POST", http.MethodPost, "/api/stats", handler.HandleGetStats},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			tt.handler(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status 405, got %d", w.Code)
			}
		})
	}
}
