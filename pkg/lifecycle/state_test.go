package lifecycle

import (
	"testing"
)

func TestNew(t *testing.T) {
	l := New()
	if l.State() != StateStarting {
		t.Errorf("expected initial state %s, got %s", StateStarting, l.State())
	}
	if l.IsTerminal() {
		t.Error("initial state should not be terminal")
	}
}

func TestNewWithInitialState(t *testing.T) {
	l := New(WithInitialState(StateRunning))
	if l.State() != StateRunning {
		t.Errorf("expected initial state %s, got %s", StateRunning, l.State())
	}
}

func TestBasicWorkflow(t *testing.T) {
	l := New()

	// Starting -> Running
	err := l.Transition(EventAgentSpawned, TransitionContext{})
	if err != nil {
		t.Fatalf("Transition to Running failed: %v", err)
	}
	if l.State() != StateRunning {
		t.Errorf("expected %s, got %s", StateRunning, l.State())
	}

	// Running -> QueuedForMerge
	err = l.Transition(EventWorkComplete, TransitionContext{})
	if err != nil {
		t.Fatalf("Transition to QueuedForMerge failed: %v", err)
	}
	if l.State() != StateQueuedForMerge {
		t.Errorf("expected %s, got %s", StateQueuedForMerge, l.State())
	}

	// QueuedForMerge -> Merging
	err = l.Transition(EventMergeStarted, TransitionContext{})
	if err != nil {
		t.Fatalf("Transition to Merging failed: %v", err)
	}
	if l.State() != StateMerging {
		t.Errorf("expected %s, got %s", StateMerging, l.State())
	}

	// Merging -> Completed (no validation)
	err = l.Transition(EventMergeSuccess, TransitionContext{ValidationEnabled: false})
	if err != nil {
		t.Fatalf("Transition to Completed failed: %v", err)
	}
	if l.State() != StateCompleted {
		t.Errorf("expected %s, got %s", StateCompleted, l.State())
	}
	if !l.IsTerminal() {
		t.Error("Completed should be terminal")
	}
}

func TestValidationWorkflow(t *testing.T) {
	l := New()

	// Fast forward to Merging
	_ = l.Transition(EventAgentSpawned, TransitionContext{})
	_ = l.Transition(EventWorkComplete, TransitionContext{})
	_ = l.Transition(EventMergeStarted, TransitionContext{})

	// Merging -> Validating (with validation enabled)
	err := l.Transition(EventMergeSuccess, TransitionContext{ValidationEnabled: true})
	if err != nil {
		t.Fatalf("Transition to Validating failed: %v", err)
	}
	if l.State() != StateValidating {
		t.Errorf("expected %s, got %s", StateValidating, l.State())
	}

	// Validating -> Completed
	err = l.Transition(EventValidationPassed, TransitionContext{})
	if err != nil {
		t.Fatalf("Transition to Completed failed: %v", err)
	}
	if l.State() != StateCompleted {
		t.Errorf("expected %s, got %s", StateCompleted, l.State())
	}
}

func TestRepairWorkflow(t *testing.T) {
	l := New()

	// Fast forward to Validating
	_ = l.Transition(EventAgentSpawned, TransitionContext{})
	_ = l.Transition(EventWorkComplete, TransitionContext{})
	_ = l.Transition(EventMergeStarted, TransitionContext{})
	_ = l.Transition(EventMergeSuccess, TransitionContext{ValidationEnabled: true})

	// Validating -> Repairing (repair enabled, attempts remaining)
	ctx := TransitionContext{
		RepairEnabled:     true,
		RepairAttempt:     0,
		MaxRepairAttempts: 3,
	}
	err := l.Transition(EventValidationFailed, ctx)
	if err != nil {
		t.Fatalf("Transition to Repairing failed: %v", err)
	}
	if l.State() != StateRepairing {
		t.Errorf("expected %s, got %s", StateRepairing, l.State())
	}

	// Repairing -> Validating
	err = l.Transition(EventRepairComplete, TransitionContext{})
	if err != nil {
		t.Fatalf("Transition to Validating failed: %v", err)
	}
	if l.State() != StateValidating {
		t.Errorf("expected %s, got %s", StateValidating, l.State())
	}

	// Validating -> Completed
	err = l.Transition(EventValidationPassed, TransitionContext{})
	if err != nil {
		t.Fatalf("Transition to Completed failed: %v", err)
	}
	if l.State() != StateCompleted {
		t.Errorf("expected %s, got %s", StateCompleted, l.State())
	}
}

func TestRepairExhausted(t *testing.T) {
	l := New()

	// Fast forward to Validating
	_ = l.Transition(EventAgentSpawned, TransitionContext{})
	_ = l.Transition(EventWorkComplete, TransitionContext{})
	_ = l.Transition(EventMergeStarted, TransitionContext{})
	_ = l.Transition(EventMergeSuccess, TransitionContext{ValidationEnabled: true})

	// Validating -> NeedsAttention (repair exhausted, lenient mode)
	ctx := TransitionContext{
		RepairEnabled:     true,
		RepairAttempt:     3,
		MaxRepairAttempts: 3,
		StrictMode:        false,
	}
	err := l.Transition(EventValidationFailed, ctx)
	if err != nil {
		t.Fatalf("Transition to NeedsAttention failed: %v", err)
	}
	if l.State() != StateNeedsAttention {
		t.Errorf("expected %s, got %s", StateNeedsAttention, l.State())
	}
}

func TestStrictModeValidationFailure(t *testing.T) {
	l := New()

	// Fast forward to Validating
	_ = l.Transition(EventAgentSpawned, TransitionContext{})
	_ = l.Transition(EventWorkComplete, TransitionContext{})
	_ = l.Transition(EventMergeStarted, TransitionContext{})
	_ = l.Transition(EventMergeSuccess, TransitionContext{ValidationEnabled: true})

	// Validating -> Failed (strict mode, repair exhausted)
	ctx := TransitionContext{
		RepairEnabled:     true,
		RepairAttempt:     3,
		MaxRepairAttempts: 3,
		StrictMode:        true,
	}
	err := l.Transition(EventValidationFailed, ctx)
	if err != nil {
		t.Fatalf("Transition to Failed failed: %v", err)
	}
	if l.State() != StateFailed {
		t.Errorf("expected %s, got %s", StateFailed, l.State())
	}
}

func TestConflictResolution(t *testing.T) {
	l := New()

	// Fast forward to Merging
	_ = l.Transition(EventAgentSpawned, TransitionContext{})
	_ = l.Transition(EventWorkComplete, TransitionContext{})
	_ = l.Transition(EventMergeStarted, TransitionContext{})

	// Merging -> Resolving
	err := l.Transition(EventMergeConflict, TransitionContext{})
	if err != nil {
		t.Fatalf("Transition to Resolving failed: %v", err)
	}
	if l.State() != StateResolving {
		t.Errorf("expected %s, got %s", StateResolving, l.State())
	}

	// Resolving -> Completed (no validation)
	err = l.Transition(EventResolveSuccess, TransitionContext{ValidationEnabled: false})
	if err != nil {
		t.Fatalf("Transition to Completed failed: %v", err)
	}
	if l.State() != StateCompleted {
		t.Errorf("expected %s, got %s", StateCompleted, l.State())
	}
}

func TestMergeFailedRetry(t *testing.T) {
	l := New()

	// Fast forward to Merging
	_ = l.Transition(EventAgentSpawned, TransitionContext{})
	_ = l.Transition(EventWorkComplete, TransitionContext{})
	_ = l.Transition(EventMergeStarted, TransitionContext{})

	// Merging -> MergeFailed
	err := l.Transition(EventMergeFailed, TransitionContext{})
	if err != nil {
		t.Fatalf("Transition to MergeFailed failed: %v", err)
	}
	if l.State() != StateMergeFailed {
		t.Errorf("expected %s, got %s", StateMergeFailed, l.State())
	}

	// MergeFailed -> Running (retry with attempts remaining)
	err = l.Transition(EventRetry, TransitionContext{AttemptsRemaining: 2})
	if err != nil {
		t.Fatalf("Transition to Running failed: %v", err)
	}
	if l.State() != StateRunning {
		t.Errorf("expected %s, got %s", StateRunning, l.State())
	}
}

func TestCancelFromAnyState(t *testing.T) {
	states := []AgentLifecycleState{
		StateStarting,
		StateRunning,
		StateQueuedForMerge,
		StateMerging,
		StateResolving,
		StateValidating,
		StateRepairing,
		StateMergeFailed,
	}

	for _, state := range states {
		l := New(WithInitialState(state))
		err := l.Transition(EventCancel, TransitionContext{})
		if err != nil {
			t.Errorf("Cancel from %s should succeed, got error: %v", state, err)
		}
		if l.State() != StateCancelled {
			t.Errorf("Cancel from %s should result in Cancelled, got %s", state, l.State())
		}
	}
}

func TestTimeoutFromAnyState(t *testing.T) {
	states := []AgentLifecycleState{
		StateStarting,
		StateRunning,
		StateQueuedForMerge,
		StateMerging,
		StateResolving,
		StateValidating,
		StateRepairing,
		StateMergeFailed,
	}

	for _, state := range states {
		l := New(WithInitialState(state))
		err := l.Transition(EventTimeout, TransitionContext{})
		if err != nil {
			t.Errorf("Timeout from %s should succeed, got error: %v", state, err)
		}
		if l.State() != StateTimedOut {
			t.Errorf("Timeout from %s should result in TimedOut, got %s", state, l.State())
		}
	}
}

func TestTerminalStatesRejectTransitions(t *testing.T) {
	terminalStates := []AgentLifecycleState{
		StateCompleted,
		StateFailed,
		StateNeedsAttention,
		StateCancelled,
		StateTimedOut,
	}

	events := []AgentEvent{
		EventAgentSpawned,
		EventWorkComplete,
		EventMergeStarted,
		EventCancel,
		EventTimeout,
	}

	for _, state := range terminalStates {
		for _, event := range events {
			l := New(WithInitialState(state))
			err := l.Transition(event, TransitionContext{})
			if err == nil {
				t.Errorf("Transition %s from terminal state %s should fail", event, state)
			}
		}
	}
}

func TestInvalidTransitions(t *testing.T) {
	tests := []struct {
		name    string
		initial AgentLifecycleState
		event   AgentEvent
	}{
		{"Starting + WorkComplete", StateStarting, EventWorkComplete},
		{"Running + MergeStarted", StateRunning, EventMergeStarted},
		{"QueuedForMerge + WorkComplete", StateQueuedForMerge, EventWorkComplete},
		{"Merging + AgentSpawned", StateMerging, EventAgentSpawned},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(WithInitialState(tt.initial))
			err := l.Transition(tt.event, TransitionContext{})
			if err == nil {
				t.Errorf("expected error for invalid transition %s + %s", tt.initial, tt.event)
			}
		})
	}
}

func TestTransitionCallback(t *testing.T) {
	var callbackCalled bool
	var fromState, toState AgentLifecycleState
	var triggeredEvent AgentEvent

	cb := func(from, to AgentLifecycleState, event AgentEvent) {
		callbackCalled = true
		fromState = from
		toState = to
		triggeredEvent = event
	}

	l := New(WithTransitionCallback(cb))
	err := l.Transition(EventAgentSpawned, TransitionContext{})
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}

	if !callbackCalled {
		t.Error("callback should have been called")
	}
	if fromState != StateStarting {
		t.Errorf("expected from state %s, got %s", StateStarting, fromState)
	}
	if toState != StateRunning {
		t.Errorf("expected to state %s, got %s", StateRunning, toState)
	}
	if triggeredEvent != EventAgentSpawned {
		t.Errorf("expected event %s, got %s", EventAgentSpawned, triggeredEvent)
	}
}

func TestHistoryBounding(t *testing.T) {
	var l *AgentLifecycle

	// Simulate many transitions
	// Start with a valid workflow and repeat
	for i := 0; i < maxHistoryEntries+50; i++ {
		// Create a new lifecycle each time to avoid needing complex valid paths
		l = New()
		_ = l.Transition(EventAgentSpawned, TransitionContext{})
	}

	// For a fresh lifecycle with just the spawned transition
	for i := 0; i < maxHistoryEntries+10; i++ {
		// Reset to starting state by creating new lifecycle
		l = New()
		_ = l.Transition(EventAgentSpawned, TransitionContext{})
	}

	// Test history bounding on a single lifecycle
	// We'll use cancel transitions since they're allowed from any non-terminal state
	// But once cancelled, we can't transition anymore. So let's test differently.

	// Actually, let's test the history limit by directly checking the history slice
	// after many transitions that are valid

	l = New()
	_ = l.Transition(EventAgentSpawned, TransitionContext{})
	// Now we're in Running, do work complete/retry cycles
	for i := 0; i < maxHistoryEntries+20; i++ {
		_ = l.Transition(EventWorkFailed, TransitionContext{AttemptsRemaining: 1})
	}

	history := l.History()
	if len(history) > maxHistoryEntries {
		t.Errorf("history length %d exceeds max %d", len(history), maxHistoryEntries)
	}
}

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		state    AgentLifecycleState
		terminal bool
	}{
		{StateStarting, false},
		{StateRunning, false},
		{StateQueuedForMerge, false},
		{StateMerging, false},
		{StateResolving, false},
		{StateValidating, false},
		{StateRepairing, false},
		{StateMergeFailed, false},
		{StateCompleted, true},
		{StateFailed, true},
		{StateNeedsAttention, true},
		{StateCancelled, true},
		{StateTimedOut, true},
	}

	for _, tt := range tests {
		if tt.state.IsTerminal() != tt.terminal {
			t.Errorf("state %s: expected terminal=%v, got %v", tt.state, tt.terminal, tt.state.IsTerminal())
		}
	}
}

func TestWorkFailedWithRetry(t *testing.T) {
	l := New()
	_ = l.Transition(EventAgentSpawned, TransitionContext{})

	// Work failed but attempts remaining - stay in Running
	err := l.Transition(EventWorkFailed, TransitionContext{AttemptsRemaining: 2})
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if l.State() != StateRunning {
		t.Errorf("expected %s, got %s", StateRunning, l.State())
	}

	// Work failed with no attempts - go to Failed
	err = l.Transition(EventWorkFailed, TransitionContext{AttemptsRemaining: 0})
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if l.State() != StateFailed {
		t.Errorf("expected %s, got %s", StateFailed, l.State())
	}
}

func TestValidationSkipped(t *testing.T) {
	l := New()
	_ = l.Transition(EventAgentSpawned, TransitionContext{})
	_ = l.Transition(EventWorkComplete, TransitionContext{})
	_ = l.Transition(EventMergeStarted, TransitionContext{})
	_ = l.Transition(EventMergeSuccess, TransitionContext{ValidationEnabled: true})

	// Validation skipped -> Completed
	err := l.Transition(EventValidationSkipped, TransitionContext{})
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if l.State() != StateCompleted {
		t.Errorf("expected %s, got %s", StateCompleted, l.State())
	}
}

func TestRetriesExhausted(t *testing.T) {
	l := New()
	_ = l.Transition(EventAgentSpawned, TransitionContext{})
	_ = l.Transition(EventWorkComplete, TransitionContext{})
	_ = l.Transition(EventMergeStarted, TransitionContext{})
	_ = l.Transition(EventMergeFailed, TransitionContext{})

	// RetriesExhausted -> Failed
	err := l.Transition(EventRetriesExhausted, TransitionContext{})
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if l.State() != StateFailed {
		t.Errorf("expected %s, got %s", StateFailed, l.State())
	}
}

func TestResolveFailed(t *testing.T) {
	l := New()
	_ = l.Transition(EventAgentSpawned, TransitionContext{})
	_ = l.Transition(EventWorkComplete, TransitionContext{})
	_ = l.Transition(EventMergeStarted, TransitionContext{})
	_ = l.Transition(EventMergeConflict, TransitionContext{})

	// ResolveFailed -> MergeFailed
	err := l.Transition(EventResolveFailed, TransitionContext{})
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if l.State() != StateMergeFailed {
		t.Errorf("expected %s, got %s", StateMergeFailed, l.State())
	}
}

func TestContextPreserved(t *testing.T) {
	l := New()
	ctx := TransitionContext{
		ValidationEnabled: true,
		RepairEnabled:     true,
		StrictMode:        false,
		AttemptsRemaining: 3,
		RepairAttempt:     1,
		MaxRepairAttempts: 5,
		QueuePosition:     7,
		Error:             "test error",
	}

	_ = l.Transition(EventAgentSpawned, ctx)
	got := l.Context()

	if got.ValidationEnabled != ctx.ValidationEnabled {
		t.Errorf("ValidationEnabled mismatch")
	}
	if got.RepairEnabled != ctx.RepairEnabled {
		t.Errorf("RepairEnabled mismatch")
	}
	if got.StrictMode != ctx.StrictMode {
		t.Errorf("StrictMode mismatch")
	}
	if got.AttemptsRemaining != ctx.AttemptsRemaining {
		t.Errorf("AttemptsRemaining mismatch")
	}
	if got.RepairAttempt != ctx.RepairAttempt {
		t.Errorf("RepairAttempt mismatch")
	}
	if got.MaxRepairAttempts != ctx.MaxRepairAttempts {
		t.Errorf("MaxRepairAttempts mismatch")
	}
	if got.QueuePosition != ctx.QueuePosition {
		t.Errorf("QueuePosition mismatch")
	}
	if got.Error != ctx.Error {
		t.Errorf("Error mismatch")
	}
}

func TestSetState(t *testing.T) {
	t.Run("force transition bypasses state machine", func(t *testing.T) {
		l := New()

		// Start at StateStarting
		if l.State() != StateStarting {
			t.Fatalf("expected StateStarting, got %s", l.State())
		}

		// Force to StateCompleted, skipping all intermediate states
		ctx := TransitionContext{}
		oldState := l.SetState(StateCompleted, EventMergeSuccess, ctx)

		if oldState != StateStarting {
			t.Errorf("expected old state %s, got %s", StateStarting, oldState)
		}
		if l.State() != StateCompleted {
			t.Errorf("expected new state %s, got %s", StateCompleted, l.State())
		}
		if !l.IsTerminal() {
			t.Error("StateCompleted should be terminal")
		}
	})

	t.Run("no-op when setting same state", func(t *testing.T) {
		l := New()

		// Force to StateRunning
		l.SetState(StateRunning, EventAgentSpawned, TransitionContext{})
		historyLen := len(l.History())

		// Set to same state - should be no-op
		oldState := l.SetState(StateRunning, EventAgentSpawned, TransitionContext{})

		if oldState != StateRunning {
			t.Errorf("expected old state %s, got %s", StateRunning, oldState)
		}
		if len(l.History()) != historyLen {
			t.Error("history should not change when setting same state")
		}
	})

	t.Run("records forced transition in history", func(t *testing.T) {
		l := New()

		// Progress normally to QueuedForMerge
		_ = l.Transition(EventAgentSpawned, TransitionContext{})
		_ = l.Transition(EventWorkComplete, TransitionContext{})

		if l.State() != StateQueuedForMerge {
			t.Fatalf("expected StateQueuedForMerge, got %s", l.State())
		}
		initialHistoryLen := len(l.History())

		// Force to StateCompleted (simulating recovery)
		ctx := TransitionContext{Error: "recovered from stuck state"}
		l.SetState(StateCompleted, EventMergeSuccess, ctx)

		history := l.History()
		if len(history) != initialHistoryLen+1 {
			t.Errorf("expected %d history entries, got %d", initialHistoryLen+1, len(history))
		}

		lastEntry := history[len(history)-1]
		if lastEntry.From != StateQueuedForMerge {
			t.Errorf("expected From=%s, got %s", StateQueuedForMerge, lastEntry.From)
		}
		if lastEntry.To != StateCompleted {
			t.Errorf("expected To=%s, got %s", StateCompleted, lastEntry.To)
		}
		if lastEntry.Event != EventMergeSuccess {
			t.Errorf("expected Event=%s, got %s", EventMergeSuccess, lastEntry.Event)
		}
		if lastEntry.Context.Error != "recovered from stuck state" {
			t.Errorf("expected context.Error to be preserved")
		}
	})

	t.Run("fires callback on forced transition", func(t *testing.T) {
		var callbackCalled bool
		var cbFrom, cbTo AgentLifecycleState
		var cbEvent AgentEvent

		l := New(WithTransitionCallback(func(from, to AgentLifecycleState, event AgentEvent) {
			callbackCalled = true
			cbFrom = from
			cbTo = to
			cbEvent = event
		}))

		l.SetState(StateCompleted, EventMergeSuccess, TransitionContext{})

		if !callbackCalled {
			t.Error("expected callback to be called")
		}
		if cbFrom != StateStarting {
			t.Errorf("expected callback from=%s, got %s", StateStarting, cbFrom)
		}
		if cbTo != StateCompleted {
			t.Errorf("expected callback to=%s, got %s", StateCompleted, cbTo)
		}
		if cbEvent != EventMergeSuccess {
			t.Errorf("expected callback event=%s, got %s", EventMergeSuccess, cbEvent)
		}
	})
}

func TestTerminalCallback(t *testing.T) {
	t.Run("fires on transition to terminal state", func(t *testing.T) {
		var terminalCalled bool
		var terminalAgentID string

		l := New(
			WithTerminalCallback("agent-123", func(agentID string) {
				terminalCalled = true
				terminalAgentID = agentID
			}),
		)

		// Transition to running (not terminal)
		err := l.Transition(EventAgentSpawned, TransitionContext{})
		if err != nil {
			t.Fatalf("Transition failed: %v", err)
		}
		if terminalCalled {
			t.Error("terminal callback should not be called for non-terminal state")
		}

		// Transition to failed (terminal)
		err = l.Transition(EventWorkFailed, TransitionContext{AttemptsRemaining: 0})
		if err != nil {
			t.Fatalf("Transition failed: %v", err)
		}
		if !terminalCalled {
			t.Error("terminal callback should be called for terminal state")
		}
		if terminalAgentID != "agent-123" {
			t.Errorf("expected agentID 'agent-123', got '%s'", terminalAgentID)
		}
	})

	t.Run("fires on SetState to terminal state", func(t *testing.T) {
		var terminalCalled bool
		var terminalAgentID string

		l := New(
			WithTerminalCallback("agent-456", func(agentID string) {
				terminalCalled = true
				terminalAgentID = agentID
			}),
		)

		// Force transition to completed (terminal)
		l.SetState(StateCompleted, EventMergeSuccess, TransitionContext{})

		if !terminalCalled {
			t.Error("terminal callback should be called for terminal state via SetState")
		}
		if terminalAgentID != "agent-456" {
			t.Errorf("expected agentID 'agent-456', got '%s'", terminalAgentID)
		}
	})

	t.Run("does not fire for non-terminal SetState", func(t *testing.T) {
		var terminalCalled bool

		l := New(
			WithTerminalCallback("agent-789", func(agentID string) {
				terminalCalled = true
			}),
		)

		// Force transition to running (not terminal)
		l.SetState(StateRunning, EventAgentSpawned, TransitionContext{})

		if terminalCalled {
			t.Error("terminal callback should not be called for non-terminal state")
		}
	})

	t.Run("fires for all terminal states", func(t *testing.T) {
		terminalStates := []AgentLifecycleState{
			StateCompleted,
			StateFailed,
			StateNeedsAttention,
			StateCancelled,
			StateTimedOut,
		}

		for _, termState := range terminalStates {
			var terminalCalled bool

			l := New(
				WithTerminalCallback("test-agent", func(agentID string) {
					terminalCalled = true
				}),
			)

			l.SetState(termState, EventCancel, TransitionContext{})

			if !terminalCalled {
				t.Errorf("terminal callback should be called for state %s", termState)
			}
		}
	})
}
