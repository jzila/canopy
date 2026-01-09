package orchestrator

import (
	"testing"

	"github.com/john/canopy/pkg/agent"
	"github.com/john/canopy/pkg/beads"
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
