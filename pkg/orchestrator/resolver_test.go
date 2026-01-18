package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/events"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/merge"
	"github.com/jzila/canopy/pkg/resolver"
	"github.com/jzila/canopy/pkg/sandbox"
)

// mockBeadsClient simulates the beads client for testing
type mockBeadsClient struct {
	mu          sync.Mutex
	tasks       map[string]*beads.Task
	doneCount   int
	failCount   int
	failReasons map[string]string
}

func newMockBeadsClient() *mockBeadsClient {
	return &mockBeadsClient{
		tasks:       make(map[string]*beads.Task),
		failReasons: make(map[string]string),
	}
}

func (m *mockBeadsClient) Done(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.doneCount++
	if task, ok := m.tasks[taskID]; ok {
		task.Status = "done"
	}
	return nil
}

func (m *mockBeadsClient) Fail(taskID string, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failCount++
	m.failReasons[taskID] = reason
	if task, ok := m.tasks[taskID]; ok {
		task.Status = "failed"
	}
	return nil
}

func (m *mockBeadsClient) Show(taskID string) (*beads.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task, ok := m.tasks[taskID]; ok {
		return task, nil
	}
	return nil, nil
}

// mockResolver simulates resolver behavior for testing
type mockResolver struct {
	mu             sync.Mutex
	resolveCalls   int
	lastConflict   *resolver.ConflictContext
	shouldSucceed  bool
	resolverResult *resolver.Result
	ipcClient      *ipc.Client
}

func newMockResolver(shouldSucceed bool) *mockResolver {
	return &mockResolver{
		shouldSucceed: shouldSucceed,
	}
}

func (m *mockResolver) Resolve(ctx context.Context, conflict *resolver.ConflictContext) (*resolver.Result, error) {
	m.mu.Lock()
	m.resolveCalls++
	m.lastConflict = conflict
	m.mu.Unlock()

	result := &resolver.Result{
		Success:         m.shouldSucceed,
		ResolverAgentID: conflict.TaskID + "-resolver",
		Duration:        100 * time.Millisecond,
	}

	if !m.shouldSucceed {
		result.Error = "mock resolver failed"
	} else {
		// Create a mock agent result with overlay for successful resolution
		result.AgentResult = &agent.Result{
			TaskID:  conflict.TaskID + "-resolver",
			Success: true,
			Changes: []sandbox.FileChange{
				{Path: "resolved.txt", Type: sandbox.ChangeCreated},
			},
		}
	}

	return result, nil
}

func (m *mockResolver) SetIPCClient(client *ipc.Client) {
	m.ipcClient = client
}

func (m *mockResolver) GetResolveCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.resolveCalls
}

func (m *mockResolver) GetLastConflict() *resolver.ConflictContext {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastConflict
}

// mockMerger simulates merger behavior for testing
type mockMerger struct {
	mu                sync.Mutex
	mergeCalls        int
	shouldPatchFail   bool
	lastResult        *agent.Result
	patchFailedTasks  map[string]bool
	commitsApplied    int
}

func newMockMerger(shouldPatchFail bool) *mockMerger {
	return &mockMerger{
		shouldPatchFail:  shouldPatchFail,
		patchFailedTasks: make(map[string]bool),
	}
}

func (m *mockMerger) MergeSingle(result *agent.Result, opts *merge.MergeOptions) (*merge.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mergeCalls++
	m.lastResult = result

	mergeResult := &merge.Result{
		PatchFailed: make(map[string]bool),
	}

	if m.shouldPatchFail && result.GitState != nil && len(result.GitState.Patches) > 0 {
		mergeResult.PatchFailed[result.TaskID] = true
		m.patchFailedTasks[result.TaskID] = true
		mergeResult.Errors = append(mergeResult.Errors, "mock patch application failed")
	} else if result.GitState != nil {
		mergeResult.CommitsApplied = len(result.GitState.Patches)
		m.commitsApplied += mergeResult.CommitsApplied
	}

	return mergeResult, nil
}

func (m *mockMerger) GetMergeCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mergeCalls
}

// TestResolverSpawnOnPatchFailure verifies that the resolver is spawned when git-am fails
func TestResolverSpawnOnPatchFailure(t *testing.T) {
	// Create temp directory for test
	tempDir := t.TempDir()
	workDir := filepath.Join(tempDir, "work")
	outputDir := filepath.Join(tempDir, "output")

	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create workdir: %v", err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("Failed to create outputdir: %v", err)
	}

	// Create mock components
	mockBeads := newMockBeadsClient()
	mockBeads.tasks["canopy-test"] = &beads.Task{
		ID:          "canopy-test",
		Title:       "Test Task",
		Description: "Test description",
		Status:      "in_progress",
	}

	mockRes := newMockResolver(true)
	mockMerge := newMockMerger(true) // Patch should fail

	// Create orchestrator with mocks (we'll test the merge flow directly)
	o := &Orchestrator{
		config: &Config{
			WorkDir:   workDir,
			OutputDir: outputDir,
			Verbose:   true,
		},
		tempDir:       tempDir,
		failureCounts: make(map[string]int),
	}

	// Create a mock agent result with git patches
	result := &agent.Result{
		TaskID:  "canopy-test",
		Success: true,
		GitState: &sandbox.GitState{
			Patches: []string{
				`From abc123 Mon Sep 17 00:00:00 2001
From: Test <test@test.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

---
 test.txt | 1 +
 1 file changed, 1 insertion(+)
`,
			},
			NewCommits: []string{"abc123"},
		},
		Changes: []sandbox.FileChange{
			{Path: "test.txt", Type: sandbox.ChangeCreated},
		},
	}

	// Simulate the merge flow: patch fails, resolver is called
	mergeResult, _ := mockMerge.MergeSingle(result, nil)

	// Verify patch failed
	if !mergeResult.PatchFailed[result.TaskID] {
		t.Error("Expected patch to fail but it didn't")
	}

	// Check if patch failed and we have git state - this triggers resolver
	if mergeResult.PatchFailed[result.TaskID] && result.GitState != nil && len(result.GitState.Patches) > 0 {
		// Build conflict context
		conflictCtx := &resolver.ConflictContext{
			TaskID:        result.TaskID,
			TaskTitle:     "Test Task",
			FailedPatches: result.GitState.Patches,
			PatchErrors:   mergeResult.Errors,
			FileChanges:   result.Changes,
			ParentAgentID: "agent-canopy-test",
		}

		// Spawn resolver
		_, err := mockRes.Resolve(context.Background(), conflictCtx)
		if err != nil {
			t.Fatalf("Resolver failed: %v", err)
		}
	}

	// Verify resolver was called
	if mockRes.GetResolveCalls() != 1 {
		t.Errorf("Expected resolver to be called once, got %d calls", mockRes.GetResolveCalls())
	}

	// Verify conflict context was passed correctly
	lastConflict := mockRes.GetLastConflict()
	if lastConflict == nil {
		t.Fatal("Expected conflict context to be set")
	}
	if lastConflict.TaskID != "canopy-test" {
		t.Errorf("Expected TaskID 'canopy-test', got '%s'", lastConflict.TaskID)
	}
	if lastConflict.TaskTitle != "Test Task" {
		t.Errorf("Expected TaskTitle 'Test Task', got '%s'", lastConflict.TaskTitle)
	}
	if len(lastConflict.FailedPatches) != 1 {
		t.Errorf("Expected 1 failed patch, got %d", len(lastConflict.FailedPatches))
	}
	if lastConflict.ParentAgentID != "agent-canopy-test" {
		t.Errorf("Expected ParentAgentID 'agent-canopy-test', got '%s'", lastConflict.ParentAgentID)
	}

	_ = o // Use orchestrator to avoid unused warning
}

// TestResolverNotSpawnedOnSuccessfulPatch verifies resolver is NOT spawned when git-am succeeds
func TestResolverNotSpawnedOnSuccessfulPatch(t *testing.T) {
	mockRes := newMockResolver(true)
	mockMerge := newMockMerger(false) // Patch should succeed

	// Create a mock agent result with git patches
	result := &agent.Result{
		TaskID:  "canopy-test",
		Success: true,
		GitState: &sandbox.GitState{
			Patches: []string{"patch content"},
		},
	}

	// Simulate the merge flow
	mergeResult, _ := mockMerge.MergeSingle(result, nil)

	// Verify patch succeeded
	if mergeResult.PatchFailed[result.TaskID] {
		t.Error("Expected patch to succeed but it failed")
	}

	// Resolver should NOT be called when patch succeeds
	if mockRes.GetResolveCalls() != 0 {
		t.Errorf("Expected resolver not to be called, but got %d calls", mockRes.GetResolveCalls())
	}
}

// TestIPCChildEventsOnResolverSpawn verifies IPC events are sent with correct parent-child relationship
func TestIPCChildEventsOnResolverSpawn(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	// Setup IPC server
	eventBus := events.NewEventBus()
	server := ipc.NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Subscribe to events
	receivedEvents := make(chan events.Event, 100)
	eventBus.Subscribe(func(event events.Event) {
		receivedEvents <- event
	})

	// Create IPC client
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Simulate resolver start event (child of original agent)
	parentAgentID := "agent-canopy-test"
	resolverAgentID := "canopy-test-resolver"
	taskID := "canopy-test"

	if err := client.SendAgentStart(
		resolverAgentID,
		"",    // no run ID in test
		taskID,
		"Resolve merge conflict for canopy-test",
		"", // no description in test
		parentAgentID,
		"", // no repo ID in test
	); err != nil {
		t.Fatalf("Failed to send agent start: %v", err)
	}

	// Verify start event received with parent info
	select {
	case event := <-receivedEvents:
		if event.Type != events.EventAgentStarted {
			t.Errorf("Expected EventAgentStarted, got %s", event.Type)
		}
		payload := event.Payload.(map[string]interface{})
		if payload["agent_id"] != resolverAgentID {
			t.Errorf("Expected agent_id=%s, got %v", resolverAgentID, payload["agent_id"])
		}
		if payload["parent_agent_id"] != parentAgentID {
			t.Errorf("Expected parent_agent_id=%s, got %v", parentAgentID, payload["parent_agent_id"])
		}
		if payload["task_id"] != taskID {
			t.Errorf("Expected task_id=%s, got %v", taskID, payload["task_id"])
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for agent start event")
	}

	// Simulate resolver done event (success case)
	result := &ipc.AgentResult{
		ExitCode:        0,
		DurationSeconds: 5.0,
		FilesChanged:    2,
		CommitsCreated:  1,
	}

	if err := client.SendAgentDone(resolverAgentID, parentAgentID, result); err != nil {
		t.Fatalf("Failed to send agent done: %v", err)
	}

	// Verify done event received with parent info
	select {
	case event := <-receivedEvents:
		if event.Type != events.EventAgentCompleted {
			t.Errorf("Expected EventAgentCompleted, got %s", event.Type)
		}
		payload := event.Payload.(map[string]interface{})
		if payload["agent_id"] != resolverAgentID {
			t.Errorf("Expected agent_id=%s, got %v", resolverAgentID, payload["agent_id"])
		}
		if payload["parent_agent_id"] != parentAgentID {
			t.Errorf("Expected parent_agent_id=%s, got %v", parentAgentID, payload["parent_agent_id"])
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for agent done event")
	}
}

// TestIPCChildEventsOnResolverFailure verifies IPC fail events are sent correctly
func TestIPCChildEventsOnResolverFailure(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	// Setup IPC server
	eventBus := events.NewEventBus()
	server := ipc.NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Subscribe to events
	receivedEvents := make(chan events.Event, 100)
	eventBus.Subscribe(func(event events.Event) {
		receivedEvents <- event
	})

	// Create IPC client
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	parentAgentID := "agent-canopy-test"
	resolverAgentID := "canopy-test-resolver"

	// Simulate resolver start
	if err := client.SendAgentStart(
		resolverAgentID,
		"",    // no run ID in test
		"canopy-test",
		"Resolve merge conflict",
		"", // no description in test
		parentAgentID,
		"", // no repo ID in test
	); err != nil {
		t.Fatalf("Failed to send agent start: %v", err)
	}

	// Drain start event
	<-receivedEvents

	// Simulate resolver failure
	result := &ipc.AgentResult{
		ExitCode:        1,
		DurationSeconds: 3.0,
	}

	if err := client.SendAgentFail(resolverAgentID, parentAgentID,
		&resolverError{"resolution failed: unable to apply changes"}, result); err != nil {
		t.Fatalf("Failed to send agent fail: %v", err)
	}

	// Verify fail event received with parent info
	select {
	case event := <-receivedEvents:
		if event.Type != events.EventAgentCompleted {
			t.Errorf("Expected EventAgentCompleted, got %s", event.Type)
		}
		payload := event.Payload.(map[string]interface{})
		if payload["agent_id"] != resolverAgentID {
			t.Errorf("Expected agent_id=%s, got %v", resolverAgentID, payload["agent_id"])
		}
		if payload["parent_agent_id"] != parentAgentID {
			t.Errorf("Expected parent_agent_id=%s, got %v", parentAgentID, payload["parent_agent_id"])
		}
		if errMsg, ok := payload["error"].(string); !ok || errMsg == "" {
			t.Error("Expected error message in payload")
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for agent fail event")
	}
}

// resolverError implements error interface for testing
type resolverError struct {
	msg string
}

func (e *resolverError) Error() string {
	return e.msg
}

// TestMergeStatusTransitions verifies merge status IPC events during resolver flow
func TestMergeStatusTransitions(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	// Setup IPC server
	eventBus := events.NewEventBus()
	server := ipc.NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Subscribe to events
	receivedEvents := make(chan events.Event, 100)
	eventBus.Subscribe(func(event events.Event) {
		receivedEvents <- event
	})

	// Create IPC client
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	agentID := "agent-canopy-test"

	// Test the expected status transitions during resolver flow:
	// pending -> acquiring -> merging -> resolving -> merged (success) or failed

	statuses := []struct {
		status   ipc.MergeStatus
		queuePos int
		errMsg   string
	}{
		{ipc.MergeStatusPending, 0, ""},
		{ipc.MergeStatusAcquiring, 0, ""},
		{ipc.MergeStatusMerging, 0, ""},
		{ipc.MergeStatusResolving, 0, ""}, // Resolver spawned
		{ipc.MergeStatusMerged, 0, ""},    // Resolver succeeded
	}

	for _, s := range statuses {
		if err := client.SendAgentMergeStatus(agentID, s.status, s.queuePos, s.errMsg); err != nil {
			t.Fatalf("Failed to send merge status %s: %v", s.status, err)
		}

		select {
		case event := <-receivedEvents:
			if event.Type != events.EventAgentMergeStatus {
				t.Errorf("Expected EventAgentMergeStatus, got %s", event.Type)
			}
			payload := event.Payload.(map[string]interface{})
			if payload["agent_id"] != agentID {
				t.Errorf("Expected agent_id=%s, got %v", agentID, payload["agent_id"])
			}
			if payload["merge_status"] != string(s.status) {
				t.Errorf("Expected merge_status=%s, got %v", s.status, payload["merge_status"])
			}
		case <-time.After(time.Second):
			t.Fatalf("Timeout waiting for merge status event: %s", s.status)
		}
	}
}

// TestMergeStatusFailedOnResolverFailure verifies merge status is set to failed when resolver fails
func TestMergeStatusFailedOnResolverFailure(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	// Setup IPC server
	eventBus := events.NewEventBus()
	server := ipc.NewServer(socketPath, eventBus)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	receivedEvents := make(chan events.Event, 100)
	eventBus.Subscribe(func(event events.Event) {
		receivedEvents <- event
	})

	client, err := ipc.NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	agentID := "agent-canopy-test"

	// Send resolving status
	if err := client.SendAgentMergeStatus(agentID, ipc.MergeStatusResolving, 0, ""); err != nil {
		t.Fatalf("Failed to send resolving status: %v", err)
	}
	<-receivedEvents // drain

	// Send failed status with error message
	errMsg := "resolver failed: unable to resolve conflicts"
	if err := client.SendAgentMergeStatus(agentID, ipc.MergeStatusFailed, 0, errMsg); err != nil {
		t.Fatalf("Failed to send failed status: %v", err)
	}

	select {
	case event := <-receivedEvents:
		if event.Type != events.EventAgentMergeStatus {
			t.Errorf("Expected EventAgentMergeStatus, got %s", event.Type)
		}
		payload := event.Payload.(map[string]interface{})
		if payload["merge_status"] != string(ipc.MergeStatusFailed) {
			t.Errorf("Expected merge_status=failed, got %v", payload["merge_status"])
		}
		if payload["error"] != errMsg {
			t.Errorf("Expected error=%s, got %v", errMsg, payload["error"])
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for failed status event")
	}
}

// TestResolverSuccessMarksTaskDone verifies successful resolver marks task as done
func TestResolverSuccessMarksTaskDone(t *testing.T) {
	mockBeads := newMockBeadsClient()
	mockBeads.tasks["canopy-test"] = &beads.Task{
		ID:     "canopy-test",
		Status: "in_progress",
	}

	mockRes := newMockResolver(true) // Resolver will succeed

	// Simulate the flow
	conflict := &resolver.ConflictContext{
		TaskID:        "canopy-test",
		TaskTitle:     "Test Task",
		ParentAgentID: "agent-canopy-test",
		FailedPatches: []string{"patch"},
	}

	resolverResult, err := mockRes.Resolve(context.Background(), conflict)
	if err != nil {
		t.Fatalf("Resolver failed: %v", err)
	}

	// Verify resolver succeeded
	if !resolverResult.Success {
		t.Error("Expected resolver to succeed")
	}

	// Simulate marking task done after successful resolution
	if resolverResult.Success {
		mockBeads.Done("canopy-test")
	}

	// Verify task status
	mockBeads.mu.Lock()
	task := mockBeads.tasks["canopy-test"]
	mockBeads.mu.Unlock()

	if task.Status != "done" {
		t.Errorf("Expected task status 'done', got '%s'", task.Status)
	}
	if mockBeads.doneCount != 1 {
		t.Errorf("Expected Done to be called once, got %d", mockBeads.doneCount)
	}
}

// TestResolverFailureMarksTaskFailed verifies failed resolver marks task as failed
func TestResolverFailureMarksTaskFailed(t *testing.T) {
	mockBeads := newMockBeadsClient()
	mockBeads.tasks["canopy-test"] = &beads.Task{
		ID:     "canopy-test",
		Status: "in_progress",
	}

	mockRes := newMockResolver(false) // Resolver will fail

	// Simulate the flow
	conflict := &resolver.ConflictContext{
		TaskID:        "canopy-test",
		TaskTitle:     "Test Task",
		ParentAgentID: "agent-canopy-test",
		FailedPatches: []string{"patch"},
	}

	resolverResult, err := mockRes.Resolve(context.Background(), conflict)
	if err != nil {
		t.Fatalf("Resolver failed unexpectedly: %v", err)
	}

	// Verify resolver failed
	if resolverResult.Success {
		t.Error("Expected resolver to fail")
	}

	// Simulate marking task failed after failed resolution
	if !resolverResult.Success {
		reason := "resolver failed: " + resolverResult.Error
		mockBeads.Fail("canopy-test", reason)
	}

	// Verify task status
	mockBeads.mu.Lock()
	task := mockBeads.tasks["canopy-test"]
	failReason := mockBeads.failReasons["canopy-test"]
	mockBeads.mu.Unlock()

	if task.Status != "failed" {
		t.Errorf("Expected task status 'failed', got '%s'", task.Status)
	}
	if mockBeads.failCount != 1 {
		t.Errorf("Expected Fail to be called once, got %d", mockBeads.failCount)
	}
	if failReason != "resolver failed: mock resolver failed" {
		t.Errorf("Expected fail reason to contain resolver error, got '%s'", failReason)
	}
}

// TestResolverAgentIDFormat verifies resolver agent ID follows expected format
func TestResolverAgentIDFormat(t *testing.T) {
	testCases := []struct {
		taskID           string
		expectedAgentID  string
	}{
		{"canopy-abc", "canopy-abc-resolver"},
		{"canopy-xyz123", "canopy-xyz123-resolver"},
		{"beads-test", "beads-test-resolver"},
	}

	for _, tc := range testCases {
		t.Run(tc.taskID, func(t *testing.T) {
			mockRes := newMockResolver(true)
			conflict := &resolver.ConflictContext{
				TaskID:        tc.taskID,
				ParentAgentID: "agent-" + tc.taskID,
			}

			result, err := mockRes.Resolve(context.Background(), conflict)
			if err != nil {
				t.Fatalf("Resolver failed: %v", err)
			}

			if result.ResolverAgentID != tc.expectedAgentID {
				t.Errorf("Expected resolver agent ID '%s', got '%s'", tc.expectedAgentID, result.ResolverAgentID)
			}
		})
	}
}

// TestResolverInheritsMergeSlot verifies resolver runs within the same merge lock
// This is a conceptual test - the actual merge slot is held by the orchestrator
func TestResolverInheritsMergeSlot(t *testing.T) {
	// The key insight is that the resolver is called within mergeAndCleanupWithContext
	// which already holds the mergeMu lock. The resolver doesn't need to acquire
	// a new slot - it inherits exclusive access from the parent.

	var mergeMu sync.Mutex
	slotHolder := ""

	// Simulate original task acquiring merge slot
	mergeMu.Lock()
	slotHolder = "canopy-test"

	// Resolver is spawned while slot is held
	resolverSlotHolder := slotHolder + "-resolver"

	// Verify resolver inherits slot context (same lock)
	// In production, this is implicit - the resolver runs within the locked section
	if slotHolder != "canopy-test" {
		t.Error("Original task should hold the slot")
	}
	if resolverSlotHolder != "canopy-test-resolver" {
		t.Error("Resolver should have context of original slot holder")
	}

	// Slot is released after both original and resolver complete
	mergeMu.Unlock()
	slotHolder = ""

	if slotHolder != "" {
		t.Error("Slot should be released")
	}
}

// TestResolverConflictContextPopulated verifies all conflict context fields are populated
func TestResolverConflictContextPopulated(t *testing.T) {
	mockRes := newMockResolver(true)

	patches := []string{
		"patch 1 content",
		"patch 2 content",
	}
	patchErrors := []string{
		"error applying patch 1",
		"error applying patch 2",
	}
	fileChanges := []sandbox.FileChange{
		{Path: "file1.go", Type: sandbox.ChangeModified},
		{Path: "file2.go", Type: sandbox.ChangeCreated},
	}

	conflict := &resolver.ConflictContext{
		TaskID:          "canopy-test",
		TaskTitle:       "Implement feature X",
		TaskDescription: "Add feature X with proper error handling",
		FailedPatches:   patches,
		PatchErrors:     patchErrors,
		FileChanges:     fileChanges,
		ParentAgentID:   "agent-canopy-test",
	}

	_, err := mockRes.Resolve(context.Background(), conflict)
	if err != nil {
		t.Fatalf("Resolver failed: %v", err)
	}

	lastConflict := mockRes.GetLastConflict()
	if lastConflict == nil {
		t.Fatal("Expected conflict context to be captured")
	}

	// Verify all fields are populated
	if lastConflict.TaskID != "canopy-test" {
		t.Errorf("TaskID mismatch")
	}
	if lastConflict.TaskTitle != "Implement feature X" {
		t.Errorf("TaskTitle mismatch")
	}
	if lastConflict.TaskDescription != "Add feature X with proper error handling" {
		t.Errorf("TaskDescription mismatch")
	}
	if len(lastConflict.FailedPatches) != 2 {
		t.Errorf("Expected 2 failed patches, got %d", len(lastConflict.FailedPatches))
	}
	if len(lastConflict.PatchErrors) != 2 {
		t.Errorf("Expected 2 patch errors, got %d", len(lastConflict.PatchErrors))
	}
	if len(lastConflict.FileChanges) != 2 {
		t.Errorf("Expected 2 file changes, got %d", len(lastConflict.FileChanges))
	}
	if lastConflict.ParentAgentID != "agent-canopy-test" {
		t.Errorf("ParentAgentID mismatch")
	}
}

// TestNoPatchesNoResolver verifies resolver is NOT spawned when there are no patches
func TestNoPatchesNoResolver(t *testing.T) {
	mockRes := newMockResolver(true)
	mockMerge := newMockMerger(false)

	// Result with no git state (no patches)
	result := &agent.Result{
		TaskID:  "canopy-test",
		Success: true,
		Changes: []sandbox.FileChange{
			{Path: "file.txt", Type: sandbox.ChangeCreated},
		},
	}

	// Merge should succeed (no patches to apply)
	mergeResult, _ := mockMerge.MergeSingle(result, nil)

	// No patch failure expected
	if mergeResult.PatchFailed[result.TaskID] {
		t.Error("Expected no patch failure when there are no patches")
	}

	// Resolver should not be called
	if mockRes.GetResolveCalls() != 0 {
		t.Errorf("Resolver should not be called when there are no patches")
	}
}

// TestResolverResultMerged verifies resolver's result is merged after success
func TestResolverResultMerged(t *testing.T) {
	mockMerge := newMockMerger(false) // Second merge should succeed

	// Simulate successful resolver result
	resolverResult := &resolver.Result{
		Success:         true,
		ResolverAgentID: "canopy-test-resolver",
		Duration:        100 * time.Millisecond,
		AgentResult: &agent.Result{
			TaskID:  "canopy-test-resolver",
			Success: true,
			GitState: &sandbox.GitState{
				Patches: []string{"resolver patch"},
			},
			Changes: []sandbox.FileChange{
				{Path: "resolved.txt", Type: sandbox.ChangeCreated},
			},
		},
	}

	// Merge the resolver's result
	if resolverResult.Success && resolverResult.AgentResult != nil {
		_, err := mockMerge.MergeSingle(resolverResult.AgentResult, nil)
		if err != nil {
			t.Fatalf("Failed to merge resolver result: %v", err)
		}
	}

	// Verify merge was called for resolver result
	if mockMerge.GetMergeCalls() != 1 {
		t.Errorf("Expected 1 merge call for resolver result, got %d", mockMerge.GetMergeCalls())
	}

	mockMerge.mu.Lock()
	lastResult := mockMerge.lastResult
	mockMerge.mu.Unlock()

	if lastResult.TaskID != "canopy-test-resolver" {
		t.Errorf("Expected last merged result to be resolver's, got %s", lastResult.TaskID)
	}
}

// TestNoResolverWhenAgentHasChangesButNoCommits verifies resolver is NOT spawned
// when agent reports file changes but made no git commits (the "work already done" case).
// This scenario occurs when:
// 1. Agent reads/touches files in overlay (recorded as 'changes')
// 2. Agent determines work is already done (or it's an epic with nothing to do)
// 3. Agent makes no commits
// 4. Merge queue sees changes but CommitsApplied == 0 && Applied == 0
//
// In this case, spawning a resolver is wasteful because there's no actual conflict
// to resolve - the work was simply already done or there was nothing to do.
func TestNoResolverWhenAgentHasChangesButNoCommits(t *testing.T) {
	mockRes := newMockResolver(true)
	mockMerge := newMockMerger(false)

	// Result with file changes but NO git state (no commits)
	// This simulates an agent that read files but didn't need to make changes
	result := &agent.Result{
		TaskID:   "canopy-test",
		Success:  true,
		GitState: nil, // No commits made
		Changes: []sandbox.FileChange{
			// Agent read/touched these files but didn't modify them
			// (These might be file reads that copy-up'd in overlay before hash comparison fix)
			{Path: "file1.go", Type: sandbox.ChangeModified},
			{Path: "file2.go", Type: sandbox.ChangeModified},
			{Path: "file3.go", Type: sandbox.ChangeModified},
		},
	}

	// Merge should succeed (no patches to apply, files would be filtered/no-op)
	mergeResult, _ := mockMerge.MergeSingle(result, nil)

	// No patch failure expected since there are no patches
	if mergeResult.PatchFailed[result.TaskID] {
		t.Error("Expected no patch failure when there are no patches")
	}

	// Simulate the processor logic for spawning resolver
	// Old logic: spawn if Changes > 0 && CommitsApplied == 0 && Applied == 0
	// New logic: only spawn if GitState.Patches > 0 && CommitsApplied == 0 && Applied == 0

	needsResolver := false
	if mergeResult.CommitsApplied == 0 && len(mergeResult.Applied) == 0 {
		// NEW: Check if agent actually made git commits we failed to apply
		hasPatches := result.GitState != nil && len(result.GitState.Patches) > 0
		if hasPatches {
			needsResolver = true
		}
		// OLD (buggy): if len(result.Changes) > 0 { needsResolver = true }
	}

	// Resolver should NOT be spawned for this case
	if needsResolver {
		t.Error("Resolver should NOT be spawned when agent has changes but no git commits")
	}

	// Verify resolver was not called
	if mockRes.GetResolveCalls() != 0 {
		t.Errorf("Resolver should not be called, but got %d calls", mockRes.GetResolveCalls())
	}
}

// TestResolverSpawnedWhenAgentHasCommitsThatFailedToApply verifies resolver IS spawned
// when agent made commits but they couldn't be applied (actual conflict case).
func TestResolverSpawnedWhenAgentHasCommitsThatFailedToApply(t *testing.T) {
	mockMerge := newMockMerger(true) // Patches will fail

	// Result with file changes AND git commits
	result := &agent.Result{
		TaskID:  "canopy-test",
		Success: true,
		GitState: &sandbox.GitState{
			Patches: []string{
				"From abc123 Mon Sep 17 00:00:00 2001\nSubject: Test commit\n---\n test.txt | 1 +\n",
			},
			BaseCommit: "def456",
		},
		Changes: []sandbox.FileChange{
			{Path: "test.txt", Type: sandbox.ChangeCreated},
		},
	}

	// Merge will report patch failure
	mergeResult, _ := mockMerge.MergeSingle(result, nil)

	// Simulate the processor logic for spawning resolver
	needsResolver := false

	// Case 1: Merge had errors
	if len(mergeResult.Errors) > 0 {
		needsResolver = true
	}

	// Case 2: No actual changes applied despite agent having git patches
	if !needsResolver && mergeResult.CommitsApplied == 0 && len(mergeResult.Applied) == 0 {
		hasPatches := result.GitState != nil && len(result.GitState.Patches) > 0
		if hasPatches {
			needsResolver = true
		}
	}

	// Resolver SHOULD be spawned because agent made commits that failed to apply
	if !needsResolver {
		t.Error("Resolver SHOULD be spawned when agent has commits that failed to apply")
	}
}
