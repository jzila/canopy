package daemon

import (
	"testing"

	"github.com/jzila/canopy/pkg/beads"
)

func TestGetIntFromPayload(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]interface{}
		key      string
		wantVal  int
		wantOk   bool
	}{
		{
			name:    "int value",
			payload: map[string]interface{}{"count": 42},
			key:     "count",
			wantVal: 42,
			wantOk:  true,
		},
		{
			name:    "float64 value",
			payload: map[string]interface{}{"count": float64(42)},
			key:     "count",
			wantVal: 42,
			wantOk:  true,
		},
		{
			name:    "int64 value",
			payload: map[string]interface{}{"count": int64(42)},
			key:     "count",
			wantVal: 42,
			wantOk:  true,
		},
		{
			name:    "missing key",
			payload: map[string]interface{}{"other": 42},
			key:     "count",
			wantVal: 0,
			wantOk:  false,
		},
		{
			name:    "wrong type (string)",
			payload: map[string]interface{}{"count": "42"},
			key:     "count",
			wantVal: 0,
			wantOk:  false,
		},
		{
			name:    "zero int",
			payload: map[string]interface{}{"count": 0},
			key:     "count",
			wantVal: 0,
			wantOk:  true,
		},
		{
			name:    "zero float64",
			payload: map[string]interface{}{"count": float64(0)},
			key:     "count",
			wantVal: 0,
			wantOk:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotOk := getIntFromPayload(tt.payload, tt.key)
			if gotVal != tt.wantVal {
				t.Errorf("getIntFromPayload() value = %v, want %v", gotVal, tt.wantVal)
			}
			if gotOk != tt.wantOk {
				t.Errorf("getIntFromPayload() ok = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

func TestGetInt64FromPayload(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]interface{}
		key      string
		wantVal  int64
		wantOk   bool
	}{
		{
			name:    "int value",
			payload: map[string]interface{}{"count": 42},
			key:     "count",
			wantVal: 42,
			wantOk:  true,
		},
		{
			name:    "int64 value",
			payload: map[string]interface{}{"count": int64(9223372036854775807)},
			key:     "count",
			wantVal: 9223372036854775807,
			wantOk:  true,
		},
		{
			name:    "float64 value",
			payload: map[string]interface{}{"count": float64(42)},
			key:     "count",
			wantVal: 42,
			wantOk:  true,
		},
		{
			name:    "missing key",
			payload: map[string]interface{}{"other": 42},
			key:     "count",
			wantVal: 0,
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotOk := getInt64FromPayload(tt.payload, tt.key)
			if gotVal != tt.wantVal {
				t.Errorf("getInt64FromPayload() value = %v, want %v", gotVal, tt.wantVal)
			}
			if gotOk != tt.wantOk {
				t.Errorf("getInt64FromPayload() ok = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

func TestGetTasksForRepo(t *testing.T) {
	state := NewRuntimeState()

	// Add tasks with different repo IDs
	state.mu.Lock()
	state.Tasks["task-1"] = &TaskState{ID: "task-1", Title: "Task 1", RepoID: "repo-a"}
	state.Tasks["task-2"] = &TaskState{ID: "task-2", Title: "Task 2", RepoID: "repo-a"}
	state.Tasks["task-3"] = &TaskState{ID: "task-3", Title: "Task 3", RepoID: "repo-b"}
	state.Tasks["task-4"] = &TaskState{ID: "task-4", Title: "Task 4", RepoID: ""}
	state.mu.Unlock()

	// Get tasks for repo-a
	tasksA := state.GetTasksForRepo("repo-a")
	if len(tasksA) != 2 {
		t.Errorf("expected 2 tasks for repo-a, got %d", len(tasksA))
	}
	if _, ok := tasksA["task-1"]; !ok {
		t.Error("expected task-1 in repo-a tasks")
	}
	if _, ok := tasksA["task-2"]; !ok {
		t.Error("expected task-2 in repo-a tasks")
	}

	// Get tasks for repo-b
	tasksB := state.GetTasksForRepo("repo-b")
	if len(tasksB) != 1 {
		t.Errorf("expected 1 task for repo-b, got %d", len(tasksB))
	}

	// Get all tasks (empty repo ID)
	allTasks := state.GetTasksForRepo("")
	if len(allTasks) != 4 {
		t.Errorf("expected 4 tasks for empty repo ID, got %d", len(allTasks))
	}
}

func TestGetSnapshotForRepo(t *testing.T) {
	state := NewRuntimeState()

	// Add tasks with different repo IDs
	state.mu.Lock()
	state.Tasks["task-1"] = &TaskState{ID: "task-1", Title: "Task 1", RepoID: "repo-a"}
	state.Tasks["task-2"] = &TaskState{ID: "task-2", Title: "Task 2", RepoID: "repo-b"}
	state.mu.Unlock()

	// Add an agent
	agent := &AgentState{
		ID:        "agent-1",
		TaskID:    "task-1",
		TaskTitle: "Task 1",
		RepoID:    "repo-a",
		Status:    AgentStatusCompleted,
	}
	state.AddAgent(agent)

	// Get snapshot for repo-a
	snapshotA := state.GetSnapshotForRepo("repo-a")
	if len(snapshotA.Tasks) != 1 {
		t.Errorf("expected 1 task in repo-a snapshot, got %d", len(snapshotA.Tasks))
	}
	if _, ok := snapshotA.Tasks["task-1"]; !ok {
		t.Error("expected task-1 in repo-a snapshot")
	}

	// Agents are included (they don't filter by repo currently)
	if len(snapshotA.Agents) != 1 {
		t.Errorf("expected 1 agent in repo-a snapshot, got %d", len(snapshotA.Agents))
	}

	// Empty repo ID should return full snapshot
	snapshotFull := state.GetSnapshotForRepo("")
	if len(snapshotFull.Tasks) != 2 {
		t.Errorf("expected 2 tasks in full snapshot, got %d", len(snapshotFull.Tasks))
	}
}

func TestAddTaskWithRepo(t *testing.T) {
	state := NewRuntimeState()

	// Create a mock beads task
	task := &beads.Task{
		ID:       "task-1",
		Title:    "Test Task",
		Status:   "ready",
		Priority: 2,
	}

	// Add task with repo ID
	state.AddTaskWithRepo(task, "test-repo-id")

	// Verify task was added with correct repo ID
	state.mu.RLock()
	addedTask, exists := state.Tasks["task-1"]
	state.mu.RUnlock()

	if !exists {
		t.Fatal("expected task to be added")
	}
	if addedTask.RepoID != "test-repo-id" {
		t.Errorf("expected repo ID 'test-repo-id', got '%s'", addedTask.RepoID)
	}
	if addedTask.Title != "Test Task" {
		t.Errorf("expected title 'Test Task', got '%s'", addedTask.Title)
	}
}

func TestHandleAgentStartedWithRepoID(t *testing.T) {
	state := NewRuntimeState()

	// Simulate agent started event with repo_id
	payload := map[string]interface{}{
		"agent_id":   "agent-1",
		"task_id":    "task-1",
		"task_title": "Test Task",
		"repo_id":    "test-repo-id",
	}

	event := Event{
		Type:      EventAgentStarted,
		Payload:   payload,
	}

	state.handleEvent(event)

	// Verify agent was added with repo ID
	agent := state.GetAgent("agent-1")
	if agent == nil {
		t.Fatal("expected agent to be added")
	}
	if agent.RepoID != "test-repo-id" {
		t.Errorf("expected repo ID 'test-repo-id', got '%s'", agent.RepoID)
	}
}
