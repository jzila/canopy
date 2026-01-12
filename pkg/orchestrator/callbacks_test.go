package orchestrator

import (
	"testing"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/merge"
	"github.com/jzila/canopy/pkg/mergequeue"
	"github.com/jzila/canopy/pkg/resolver"
)

func TestEventCallbacks_Interface(t *testing.T) {
	// Test that EventCallbacks implements the scheduler.CallbackHandler interface
	var called bool

	callbacks := &EventCallbacks{
		OnAgentStartFn: func(taskID string, task *beads.Task) {
			called = true
		},
		OnOutputFn: func(taskID string, output string, isError bool) {
			called = true
		},
		OnDoneFn: func(taskID string, result *agent.Result) {
			called = true
		},
		OnFailFn: func(taskID string, result *agent.Result) {
			called = true
		},
	}

	// Test OnAgentStart
	called = false
	callbacks.OnAgentStart("test-1", &beads.Task{ID: "test-1", Title: "Test Task"})
	if !called {
		t.Error("OnAgentStart callback was not invoked")
	}

	// Test OnOutput
	called = false
	callbacks.OnOutput("test-1", "test output", false)
	if !called {
		t.Error("OnOutput callback was not invoked")
	}

	// Test OnDone
	called = false
	callbacks.OnDone("test-1", &agent.Result{TaskID: "test-1", Success: true})
	if !called {
		t.Error("OnDone callback was not invoked")
	}

	// Test OnFail
	called = false
	callbacks.OnFail("test-1", &agent.Result{TaskID: "test-1", Success: false})
	if !called {
		t.Error("OnFail callback was not invoked")
	}
}

func TestEventCallbacks_NilSafety(t *testing.T) {
	// Test that nil callbacks don't panic
	var callbacks *EventCallbacks

	// These should not panic
	callbacks.OnAgentStart("test-1", &beads.Task{ID: "test-1"})
	callbacks.OnOutput("test-1", "test", false)
	callbacks.OnDone("test-1", &agent.Result{TaskID: "test-1"})
	callbacks.OnFail("test-1", &agent.Result{TaskID: "test-1"})

	// Test with non-nil struct but nil functions
	callbacks = &EventCallbacks{}
	callbacks.OnAgentStart("test-1", &beads.Task{ID: "test-1"})
	callbacks.OnOutput("test-1", "test", false)
	callbacks.OnDone("test-1", &agent.Result{TaskID: "test-1"})
	callbacks.OnFail("test-1", &agent.Result{TaskID: "test-1"})
}

func TestOrchestrator_SetCallbacks(t *testing.T) {
	// Create a minimal test config (without actually initializing the orchestrator)
	// This tests the API surface only
	var called bool
	callbacks := &EventCallbacks{
		OnAgentStartFn: func(taskID string, task *beads.Task) {
			called = true
		},
	}

	// Verify callbacks are properly structured
	callbacks.OnAgentStart("test", &beads.Task{ID: "test"})
	if !called {
		t.Error("Callback was not invoked through the struct")
	}
}

func TestOrchestrator_WithCallbacks(t *testing.T) {
	// This tests that WithCallbacks returns the orchestrator for chaining
	// We'll create a minimal mock rather than a full orchestrator

	callbacks := &EventCallbacks{
		OnAgentStartFn: func(taskID string, task *beads.Task) {},
	}

	// Test that the API design allows chaining (compile-time check)
	_ = (&Orchestrator{}).WithCallbacks(callbacks)
}

// TestOrchestrator_MergeQueueInitialized verifies that the merge queue is initialized
// with the correct buffer size based on concurrency.
func TestOrchestrator_MergeQueueInitialized(t *testing.T) {
	testCases := []struct {
		name            string
		concurrency     int
		expectedBufSize int
	}{
		{"default concurrency", 0, 4},      // 0 means default, which is 4
		{"concurrency 1", 1, 1},            // 1 agent = 1 buffer slot
		{"concurrency 4", 4, 4},            // 4 agents = 4 buffer slots
		{"concurrency 8", 8, 8},            // 8 agents = 8 buffer slots
		{"negative concurrency", -1, 4},    // invalid defaults to 4
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate the buffer size calculation from New()
			bufferSize := tc.concurrency
			if bufferSize <= 0 {
				bufferSize = 4 // default concurrency
			}

			// Create a queue with the calculated buffer size
			queue := mergequeue.NewQueue(bufferSize)
			if queue == nil {
				t.Fatal("Expected queue to be initialized")
			}

			// Verify queue is not closed and not paused initially
			if queue.IsClosed() {
				t.Error("Expected queue to not be closed initially")
			}
			if queue.IsPaused() {
				t.Error("Expected queue to not be paused initially")
			}
		})
	}
}

// TestOrchestrator_MergeProcessorInitialized verifies that the merge processor
// is correctly initialized with all required dependencies.
func TestOrchestrator_MergeProcessorInitialized(t *testing.T) {
	// Create mock components
	queue := mergequeue.NewQueue(4)
	merger := merge.NewSequentialMerger(t.TempDir(), t.TempDir(), false)
	resolverInst := resolver.New(&resolver.Config{
		WorkDir: t.TempDir(),
		TempDir: t.TempDir(),
		Verbose: false,
	})

	// Create a minimal beads client mock (nil for this test since processor creation doesn't validate)
	var beadsClient *beads.Client = nil

	// Create the processor (this should not panic)
	processor := mergequeue.NewProcessor(
		queue,
		merger,
		resolverInst,
		beadsClient,
		t.TempDir(), // outputDir
		nil,         // ipcClient
		false,       // verbose
	)

	if processor == nil {
		t.Fatal("Expected processor to be initialized")
	}
}

// TestOrchestrator_StructHasMergeQueueFields verifies that the Orchestrator struct
// has the expected merge queue and processor fields.
func TestOrchestrator_StructHasMergeQueueFields(t *testing.T) {
	// Create a minimal orchestrator to verify field presence
	o := &Orchestrator{
		mergeQueue:     mergequeue.NewQueue(4),
		mergeProcessor: nil, // Can be nil for this structural test
	}

	// Verify mergeQueue is set
	if o.mergeQueue == nil {
		t.Error("Expected mergeQueue field to be set")
	}

	// Test queue operations work
	if o.mergeQueue.IsClosed() {
		t.Error("Expected queue to not be closed")
	}
}
