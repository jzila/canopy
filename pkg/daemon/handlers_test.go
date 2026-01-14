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

func (m *mockBeadsClient) List() ([]beads.Task, error) {
	return nil, nil
}

func TestHandleGetState(t *testing.T) {
	state := NewRuntimeState()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient, nil)

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
	handler := NewHandler(state, scheduler, beadsClient, nil)

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
	handler := NewHandler(state, scheduler, beadsClient, nil)

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
	handler := NewHandler(state, scheduler, beadsClient, nil)

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
	handler := NewHandler(state, scheduler, beadsClient, nil)

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
	handler := NewHandler(state, scheduler, beadsClient, nil)

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
	handler := NewHandler(state, scheduler, beadsClient, nil)

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
	handler := NewHandler(state, scheduler, beadsClient, nil)

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
	handler := NewHandler(state, scheduler, beadsClient, nil)

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
	handler := NewHandler(state, scheduler, beadsClient, nil)

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

func TestHandlersWithNilScheduler(t *testing.T) {
	state := NewRuntimeState()
	handler := NewHandler(state, nil, nil, nil) // nil scheduler, beadsClient, and eventBus

	// Add an agent so we can test kill
	agent := &AgentState{
		ID:        "agent-1",
		TaskID:    "task-1",
		TaskTitle: "Test Task",
		Status:    AgentStatusRunning,
		StartTime: time.Now(),
	}
	state.AddAgent(agent)

	tests := []struct {
		name    string
		method  string
		path    string
		handler func(w http.ResponseWriter, r *http.Request)
	}{
		{"PauseOrch", http.MethodPost, "/api/orch/pause", handler.HandlePauseOrch},
		{"ResumeOrch", http.MethodPost, "/api/orch/resume", handler.HandleResumeOrch},
		{"KillAgent", http.MethodPost, "/api/agents/agent-1/kill", handler.HandleKillAgent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			tt.handler(w, req)

			if w.Code != http.StatusNotImplemented {
				t.Errorf("Expected status 501 Not Implemented, got %d", w.Code)
			}
		})
	}
}

func TestHandlersWithNilBeadsClient(t *testing.T) {
	state := NewRuntimeState()
	scheduler := &mockScheduler{}
	handler := NewHandler(state, scheduler, nil, nil) // nil beadsClient and eventBus

	t.Run("CreateTask", func(t *testing.T) {
		reqBody := TaskCreateRequest{Title: "Test", Priority: 5}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler.HandleCreateTask(w, req)

		if w.Code != http.StatusNotImplemented {
			t.Errorf("Expected status 501 Not Implemented, got %d", w.Code)
		}
	})

	t.Run("UpdateTask", func(t *testing.T) {
		reqBody := TaskUpdateRequest{Status: "in_progress"}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPatch, "/api/tasks?id=task-1", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler.HandleUpdateTask(w, req)

		// UpdateTask should succeed with 200 since it updates local state first
		// and only skips the beads sync
		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", w.Code)
		}
	})
}

func TestHandleUpdateAgent(t *testing.T) {
	state := NewRuntimeState()

	// Add a test agent
	agent := &AgentState{
		ID:        "agent-1",
		TaskID:    "task-1",
		TaskTitle: "Test Task",
		Status:    AgentStatusCompleted,
		StartTime: time.Now(),
		Archived:  false,
	}
	endTime := time.Now()
	agent.EndTime = &endTime
	state.AddAgent(agent)

	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient, nil)

	// Test archiving an agent
	t.Run("ArchiveAgent", func(t *testing.T) {
		archived := true
		reqBody := AgentUpdateRequest{Archived: &archived}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPatch, "/api/agents?id=agent-1", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler.HandleUpdateAgent(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if response["id"] != "agent-1" {
			t.Errorf("Expected agent ID 'agent-1', got %v", response["id"])
		}

		if response["archived"] != true {
			t.Errorf("Expected archived to be true, got %v", response["archived"])
		}

		// Verify the agent was actually archived in state
		updatedAgent := state.GetAgent("agent-1")
		if updatedAgent == nil {
			t.Fatal("Agent not found in state")
		}
		if !updatedAgent.Archived {
			t.Error("Expected agent to be archived in state")
		}
	})

	// Test unarchiving an agent
	t.Run("UnarchiveAgent", func(t *testing.T) {
		archived := false
		reqBody := AgentUpdateRequest{Archived: &archived}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPatch, "/api/agents?id=agent-1", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler.HandleUpdateAgent(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		// Verify the agent was unarchived in state
		updatedAgent := state.GetAgent("agent-1")
		if updatedAgent == nil {
			t.Fatal("Agent not found in state")
		}
		if updatedAgent.Archived {
			t.Error("Expected agent to be unarchived in state")
		}
	})

	// Test missing agent ID
	t.Run("MissingAgentID", func(t *testing.T) {
		archived := true
		reqBody := AgentUpdateRequest{Archived: &archived}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPatch, "/api/agents", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler.HandleUpdateAgent(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})

	// Test non-existent agent
	t.Run("NonExistentAgent", func(t *testing.T) {
		archived := true
		reqBody := AgentUpdateRequest{Archived: &archived}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPatch, "/api/agents?id=nonexistent", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler.HandleUpdateAgent(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}
	})
}

func TestHandleGetMergeQueue(t *testing.T) {
	state := NewRuntimeState()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}
	handler := NewHandler(state, scheduler, beadsClient, nil)

	t.Run("EmptyState", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/merge-queue", nil)
		w := httptest.NewRecorder()

		handler.HandleGetMergeQueue(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var result MergeQueueState
		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if len(result.Completed) != 0 {
			t.Errorf("Expected 0 completed, got %d", len(result.Completed))
		}
		if len(result.Pending) != 0 {
			t.Errorf("Expected 0 pending, got %d", len(result.Pending))
		}
		if len(result.Resolvers) != 0 {
			t.Errorf("Expected 0 resolvers, got %d", len(result.Resolvers))
		}
		if len(result.ActiveWorkers) != 0 {
			t.Errorf("Expected 0 active workers, got %d", len(result.ActiveWorkers))
		}
	})

	t.Run("WithAgentsInVariousMergeStates", func(t *testing.T) {
		// Clear existing agents
		state = NewRuntimeState()
		handler = NewHandler(state, scheduler, beadsClient, nil)

		endTime := time.Now()

		// Add agent with merged status
		mergedAgent := &AgentState{
			ID:          "agent-merged",
			TaskID:      "task-merged",
			TaskTitle:   "Merged Task",
			Status:      AgentStatusCompleted,
			MergeStatus: MergeStatusMerged,
			StartTime:   time.Now().Add(-10 * time.Second),
			EndTime:     &endTime,
		}
		state.AddAgent(mergedAgent)

		// Add agent with failed merge status
		failedAgent := &AgentState{
			ID:          "agent-failed",
			TaskID:      "task-failed",
			TaskTitle:   "Failed Task",
			Status:      AgentStatusFailed,
			MergeStatus: MergeStatusFailed,
			MergeError:  "conflict in file.go",
			StartTime:   time.Now().Add(-5 * time.Second),
			EndTime:     &endTime,
		}
		state.AddAgent(failedAgent)

		// Add agent pending in queue
		pendingAgent := &AgentState{
			ID:            "agent-pending",
			TaskID:        "task-pending",
			TaskTitle:     "Pending Task",
			Status:        AgentStatusRunning,
			MergeStatus:   MergeStatusPending,
			MergeQueuePos: 1,
			StartTime:     time.Now(),
		}
		state.AddAgent(pendingAgent)

		// Add running agent (no merge status)
		runningAgent := &AgentState{
			ID:        "agent-running",
			TaskID:    "task-running",
			TaskTitle: "Running Task",
			Status:    AgentStatusRunning,
			StartTime: time.Now(),
		}
		state.AddAgent(runningAgent)

		// Add agent currently merging
		mergingAgent := &AgentState{
			ID:          "agent-merging",
			TaskID:      "task-merging",
			TaskTitle:   "Merging Task",
			Status:      AgentStatusRunning,
			MergeStatus: MergeStatusMerging,
			StartTime:   time.Now(),
		}
		state.AddAgent(mergingAgent)

		// Add parent agent with resolving status and child resolver
		resolvingAgent := &AgentState{
			ID:            "agent-resolving",
			TaskID:        "task-resolving",
			TaskTitle:     "Resolving Task",
			Status:        AgentStatusRunning,
			MergeStatus:   MergeStatusResolving,
			StartTime:     time.Now(),
			ChildAgentIDs: []string{"agent-resolver"},
		}
		state.AddAgent(resolvingAgent)

		resolverAgent := &AgentState{
			ID:            "agent-resolver",
			TaskID:        "task-resolver",
			TaskTitle:     "Resolver Task",
			Status:        AgentStatusRunning,
			ParentAgentID: "agent-resolving",
			StartTime:     time.Now(),
		}
		state.AddAgent(resolverAgent)

		req := httptest.NewRequest(http.MethodGet, "/api/merge-queue", nil)
		w := httptest.NewRecorder()

		handler.HandleGetMergeQueue(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var result MergeQueueState
		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Should have 2 completed (1 merged, 1 failed)
		if len(result.Completed) != 2 {
			t.Errorf("Expected 2 completed, got %d", len(result.Completed))
		}

		// Should have 1 pending
		if len(result.Pending) != 1 {
			t.Errorf("Expected 1 pending, got %d", len(result.Pending))
		}

		// Should have 1 resolver
		if len(result.Resolvers) != 1 {
			t.Errorf("Expected 1 resolver, got %d", len(result.Resolvers))
		}

		// Should have 3 active workers: running agent, merging agent, resolver agent (running with no merge status)
		if len(result.ActiveWorkers) != 3 {
			t.Errorf("Expected 3 active workers, got %d", len(result.ActiveWorkers))
		}

		// Check the queue length
		if result.QueueLength != 1 {
			t.Errorf("Expected queue length 1, got %d", result.QueueLength)
		}
	})

	t.Run("MethodNotAllowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/merge-queue", nil)
		w := httptest.NewRecorder()

		handler.HandleGetMergeQueue(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", w.Code)
		}
	})
}
