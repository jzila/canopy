package lifecycle

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// allStates lists all defined lifecycle states for exhaustive testing.
var allStates = []AgentLifecycleState{
	StateStarting,
	StateRunning,
	StateQueuedForMerge,
	StateMerging,
	StateResolving,
	StateValidating,
	StateRepairing,
	StateMergeFailed,
	StateCompleted,
	StateFailed,
	StateNeedsAttention,
	StateCancelled,
	StateTimedOut,
}

// nonTerminalStates are states that allow transitions out.
var nonTerminalStates = []AgentLifecycleState{
	StateStarting,
	StateRunning,
	StateQueuedForMerge,
	StateMerging,
	StateResolving,
	StateValidating,
	StateRepairing,
	StateMergeFailed,
}

// terminalStates are states with no outbound transitions.
var terminalStates = []AgentLifecycleState{
	StateCompleted,
	StateFailed,
	StateNeedsAttention,
	StateCancelled,
	StateTimedOut,
}

// allEvents lists all defined events for exhaustive testing.
var allEvents = []AgentEvent{
	EventAgentSpawned,
	EventWorkComplete,
	EventWorkFailed,
	EventMergeStarted,
	EventMergeSuccess,
	EventMergeConflict,
	EventMergeFailed,
	EventResolveSuccess,
	EventResolveFailed,
	EventValidationPassed,
	EventValidationFailed,
	EventValidationSkipped,
	EventRepairComplete,
	EventRetry,
	EventRetriesExhausted,
	EventCancel,
	EventTimeout,
}

// transitionTest defines a test case for a state transition.
type transitionTest struct {
	name          string
	fromState     AgentLifecycleState
	event         AgentEvent
	ctx           TransitionContext
	expectedState AgentLifecycleState
	expectError   bool
}

// TestAllValidTransitions exhaustively tests all valid state transitions.
// This covers the complete transition table from the state machine design.
func TestAllValidTransitions(t *testing.T) {
	tests := []transitionTest{
		// Starting state transitions
		{
			name:          "Starting + AgentSpawned -> Running",
			fromState:     StateStarting,
			event:         EventAgentSpawned,
			expectedState: StateRunning,
		},
		{
			name:          "Starting + Cancel -> Cancelled",
			fromState:     StateStarting,
			event:         EventCancel,
			expectedState: StateCancelled,
		},
		{
			name:          "Starting + Timeout -> TimedOut",
			fromState:     StateStarting,
			event:         EventTimeout,
			expectedState: StateTimedOut,
		},

		// Running state transitions
		{
			name:          "Running + WorkComplete -> QueuedForMerge",
			fromState:     StateRunning,
			event:         EventWorkComplete,
			expectedState: StateQueuedForMerge,
		},
		{
			name:          "Running + WorkFailed (retries) -> Running",
			fromState:     StateRunning,
			event:         EventWorkFailed,
			ctx:           TransitionContext{AttemptsRemaining: 2},
			expectedState: StateRunning,
		},
		{
			name:          "Running + WorkFailed (no retries) -> Failed",
			fromState:     StateRunning,
			event:         EventWorkFailed,
			ctx:           TransitionContext{AttemptsRemaining: 0},
			expectedState: StateFailed,
		},
		{
			name:          "Running + Cancel -> Cancelled",
			fromState:     StateRunning,
			event:         EventCancel,
			expectedState: StateCancelled,
		},
		{
			name:          "Running + Timeout -> TimedOut",
			fromState:     StateRunning,
			event:         EventTimeout,
			expectedState: StateTimedOut,
		},

		// QueuedForMerge state transitions
		{
			name:          "QueuedForMerge + MergeStarted -> Merging",
			fromState:     StateQueuedForMerge,
			event:         EventMergeStarted,
			expectedState: StateMerging,
		},
		{
			name:          "QueuedForMerge + Cancel -> Cancelled",
			fromState:     StateQueuedForMerge,
			event:         EventCancel,
			expectedState: StateCancelled,
		},
		{
			name:          "QueuedForMerge + Timeout -> TimedOut",
			fromState:     StateQueuedForMerge,
			event:         EventTimeout,
			expectedState: StateTimedOut,
		},

		// Merging state transitions
		{
			name:          "Merging + MergeSuccess (no validation) -> Completed",
			fromState:     StateMerging,
			event:         EventMergeSuccess,
			ctx:           TransitionContext{ValidationEnabled: false},
			expectedState: StateCompleted,
		},
		{
			name:          "Merging + MergeSuccess (with validation) -> Validating",
			fromState:     StateMerging,
			event:         EventMergeSuccess,
			ctx:           TransitionContext{ValidationEnabled: true},
			expectedState: StateValidating,
		},
		{
			name:          "Merging + MergeConflict -> Resolving",
			fromState:     StateMerging,
			event:         EventMergeConflict,
			expectedState: StateResolving,
		},
		{
			name:          "Merging + MergeFailed -> MergeFailed",
			fromState:     StateMerging,
			event:         EventMergeFailed,
			expectedState: StateMergeFailed,
		},
		{
			name:          "Merging + Cancel -> Cancelled",
			fromState:     StateMerging,
			event:         EventCancel,
			expectedState: StateCancelled,
		},
		{
			name:          "Merging + Timeout -> TimedOut",
			fromState:     StateMerging,
			event:         EventTimeout,
			expectedState: StateTimedOut,
		},

		// Resolving state transitions
		{
			name:          "Resolving + ResolveSuccess (no validation) -> Completed",
			fromState:     StateResolving,
			event:         EventResolveSuccess,
			ctx:           TransitionContext{ValidationEnabled: false},
			expectedState: StateCompleted,
		},
		{
			name:          "Resolving + ResolveSuccess (with validation) -> Validating",
			fromState:     StateResolving,
			event:         EventResolveSuccess,
			ctx:           TransitionContext{ValidationEnabled: true},
			expectedState: StateValidating,
		},
		{
			name:          "Resolving + ResolveFailed -> MergeFailed",
			fromState:     StateResolving,
			event:         EventResolveFailed,
			expectedState: StateMergeFailed,
		},
		{
			name:          "Resolving + Cancel -> Cancelled",
			fromState:     StateResolving,
			event:         EventCancel,
			expectedState: StateCancelled,
		},
		{
			name:          "Resolving + Timeout -> TimedOut",
			fromState:     StateResolving,
			event:         EventTimeout,
			expectedState: StateTimedOut,
		},

		// Validating state transitions
		{
			name:          "Validating + ValidationPassed -> Completed",
			fromState:     StateValidating,
			event:         EventValidationPassed,
			expectedState: StateCompleted,
		},
		{
			name:          "Validating + ValidationFailed (repair enabled, attempts left) -> Repairing",
			fromState:     StateValidating,
			event:         EventValidationFailed,
			ctx:           TransitionContext{RepairEnabled: true, RepairAttempt: 0, MaxRepairAttempts: 3},
			expectedState: StateRepairing,
		},
		{
			name:          "Validating + ValidationFailed (repair enabled, at max, strict) -> Failed",
			fromState:     StateValidating,
			event:         EventValidationFailed,
			ctx:           TransitionContext{RepairEnabled: true, RepairAttempt: 3, MaxRepairAttempts: 3, StrictMode: true},
			expectedState: StateFailed,
		},
		{
			name:          "Validating + ValidationFailed (repair enabled, at max, lenient) -> NeedsAttention",
			fromState:     StateValidating,
			event:         EventValidationFailed,
			ctx:           TransitionContext{RepairEnabled: true, RepairAttempt: 3, MaxRepairAttempts: 3, StrictMode: false},
			expectedState: StateNeedsAttention,
		},
		{
			name:          "Validating + ValidationFailed (repair disabled, strict) -> Failed",
			fromState:     StateValidating,
			event:         EventValidationFailed,
			ctx:           TransitionContext{RepairEnabled: false, StrictMode: true},
			expectedState: StateFailed,
		},
		{
			name:          "Validating + ValidationFailed (repair disabled, lenient) -> NeedsAttention",
			fromState:     StateValidating,
			event:         EventValidationFailed,
			ctx:           TransitionContext{RepairEnabled: false, StrictMode: false},
			expectedState: StateNeedsAttention,
		},
		{
			name:          "Validating + ValidationSkipped -> Completed",
			fromState:     StateValidating,
			event:         EventValidationSkipped,
			expectedState: StateCompleted,
		},
		{
			name:          "Validating + Cancel -> Cancelled",
			fromState:     StateValidating,
			event:         EventCancel,
			expectedState: StateCancelled,
		},
		{
			name:          "Validating + Timeout -> TimedOut",
			fromState:     StateValidating,
			event:         EventTimeout,
			expectedState: StateTimedOut,
		},

		// Repairing state transitions
		{
			name:          "Repairing + RepairComplete -> Validating",
			fromState:     StateRepairing,
			event:         EventRepairComplete,
			expectedState: StateValidating,
		},
		{
			name:          "Repairing + Cancel -> Cancelled",
			fromState:     StateRepairing,
			event:         EventCancel,
			expectedState: StateCancelled,
		},
		{
			name:          "Repairing + Timeout -> TimedOut",
			fromState:     StateRepairing,
			event:         EventTimeout,
			expectedState: StateTimedOut,
		},

		// MergeFailed state transitions
		{
			name:          "MergeFailed + Retry (attempts left) -> Running",
			fromState:     StateMergeFailed,
			event:         EventRetry,
			ctx:           TransitionContext{AttemptsRemaining: 1},
			expectedState: StateRunning,
		},
		{
			name:          "MergeFailed + Retry (no attempts) -> Failed",
			fromState:     StateMergeFailed,
			event:         EventRetry,
			ctx:           TransitionContext{AttemptsRemaining: 0},
			expectedState: StateFailed,
		},
		{
			name:          "MergeFailed + RetriesExhausted -> Failed",
			fromState:     StateMergeFailed,
			event:         EventRetriesExhausted,
			expectedState: StateFailed,
		},
		{
			name:          "MergeFailed + Cancel -> Cancelled",
			fromState:     StateMergeFailed,
			event:         EventCancel,
			expectedState: StateCancelled,
		},
		{
			name:          "MergeFailed + Timeout -> TimedOut",
			fromState:     StateMergeFailed,
			event:         EventTimeout,
			expectedState: StateTimedOut,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(WithInitialState(tt.fromState))
			err := l.Transition(tt.event, tt.ctx)

			if err != nil {
				t.Fatalf("Transition() returned unexpected error: %v", err)
			}

			if l.State() != tt.expectedState {
				t.Errorf("Transition() resulted in state %s, want %s", l.State(), tt.expectedState)
			}
		})
	}
}

// TestAllInvalidTransitionsFromNonTerminalStates tests that invalid events
// are rejected from non-terminal states.
func TestAllInvalidTransitionsFromNonTerminalStates(t *testing.T) {
	// Define which events are valid for each non-terminal state
	validEvents := map[AgentLifecycleState][]AgentEvent{
		StateStarting:       {EventAgentSpawned, EventCancel, EventTimeout},
		StateRunning:        {EventWorkComplete, EventWorkFailed, EventCancel, EventTimeout},
		StateQueuedForMerge: {EventMergeStarted, EventCancel, EventTimeout},
		StateMerging:        {EventMergeSuccess, EventMergeConflict, EventMergeFailed, EventCancel, EventTimeout},
		StateResolving:      {EventResolveSuccess, EventResolveFailed, EventCancel, EventTimeout},
		StateValidating:     {EventValidationPassed, EventValidationFailed, EventValidationSkipped, EventCancel, EventTimeout},
		StateRepairing:      {EventRepairComplete, EventCancel, EventTimeout},
		StateMergeFailed:    {EventRetry, EventRetriesExhausted, EventCancel, EventTimeout},
	}

	for _, state := range nonTerminalStates {
		valid := validEvents[state]
		validSet := make(map[AgentEvent]bool)
		for _, e := range valid {
			validSet[e] = true
		}

		for _, event := range allEvents {
			if validSet[event] {
				continue // skip valid events
			}

			t.Run(string(state)+"+"+string(event), func(t *testing.T) {
				l := New(WithInitialState(state))
				err := l.Transition(event, TransitionContext{})

				if err == nil {
					t.Errorf("Transition(%s, %s) should have returned error, got state %s",
						state, event, l.State())
				}

				if !errors.Is(err, ErrInvalidTransition) {
					t.Errorf("error should wrap ErrInvalidTransition, got: %v", err)
				}

				// State should be unchanged
				if l.State() != state {
					t.Errorf("state changed despite error: got %s, want %s", l.State(), state)
				}
			})
		}
	}
}

// TestAllTransitionsFromTerminalStatesRejected verifies that no transitions
// are allowed from terminal states.
func TestAllTransitionsFromTerminalStatesRejected(t *testing.T) {
	for _, state := range terminalStates {
		for _, event := range allEvents {
			t.Run(string(state)+"+"+string(event), func(t *testing.T) {
				l := New(WithInitialState(state))
				err := l.Transition(event, TransitionContext{})

				if err == nil {
					t.Errorf("Transition(%s, %s) from terminal state should fail", state, event)
				}

				if !errors.Is(err, ErrInvalidTransition) {
					t.Errorf("error should wrap ErrInvalidTransition, got: %v", err)
				}

				// State must remain unchanged
				if l.State() != state {
					t.Errorf("terminal state changed: got %s, want %s", l.State(), state)
				}
			})
		}
	}
}

// TestCancelFromAllNonTerminalStates verifies Cancel works from every non-terminal state.
func TestCancelFromAllNonTerminalStates(t *testing.T) {
	for _, state := range nonTerminalStates {
		t.Run(string(state), func(t *testing.T) {
			l := New(WithInitialState(state))
			err := l.Transition(EventCancel, TransitionContext{})

			if err != nil {
				t.Errorf("Cancel from %s should succeed, got error: %v", state, err)
			}

			if l.State() != StateCancelled {
				t.Errorf("Cancel should result in Cancelled, got %s", l.State())
			}
		})
	}
}

// TestTimeoutFromAllNonTerminalStates verifies Timeout works from every non-terminal state.
func TestTimeoutFromAllNonTerminalStates(t *testing.T) {
	for _, state := range nonTerminalStates {
		t.Run(string(state), func(t *testing.T) {
			l := New(WithInitialState(state))
			err := l.Transition(EventTimeout, TransitionContext{})

			if err != nil {
				t.Errorf("Timeout from %s should succeed, got error: %v", state, err)
			}

			if l.State() != StateTimedOut {
				t.Errorf("Timeout should result in TimedOut, got %s", l.State())
			}
		})
	}
}

// TestValidationFailedBranchCoverage tests all branches of the ValidationFailed handling.
func TestValidationFailedBranchCoverage(t *testing.T) {
	tests := []struct {
		name          string
		ctx           TransitionContext
		expectedState AgentLifecycleState
	}{
		{
			name: "repair enabled, attempt 0 of 3 -> Repairing",
			ctx: TransitionContext{
				RepairEnabled:     true,
				RepairAttempt:     0,
				MaxRepairAttempts: 3,
			},
			expectedState: StateRepairing,
		},
		{
			name: "repair enabled, attempt 1 of 3 -> Repairing",
			ctx: TransitionContext{
				RepairEnabled:     true,
				RepairAttempt:     1,
				MaxRepairAttempts: 3,
			},
			expectedState: StateRepairing,
		},
		{
			name: "repair enabled, attempt 2 of 3 -> Repairing",
			ctx: TransitionContext{
				RepairEnabled:     true,
				RepairAttempt:     2,
				MaxRepairAttempts: 3,
			},
			expectedState: StateRepairing,
		},
		{
			name: "repair enabled, attempt 3 of 3, strict -> Failed",
			ctx: TransitionContext{
				RepairEnabled:     true,
				RepairAttempt:     3,
				MaxRepairAttempts: 3,
				StrictMode:        true,
			},
			expectedState: StateFailed,
		},
		{
			name: "repair enabled, attempt 3 of 3, lenient -> NeedsAttention",
			ctx: TransitionContext{
				RepairEnabled:     true,
				RepairAttempt:     3,
				MaxRepairAttempts: 3,
				StrictMode:        false,
			},
			expectedState: StateNeedsAttention,
		},
		{
			name: "repair enabled, attempt 5 of 3 (exceeded), strict -> Failed",
			ctx: TransitionContext{
				RepairEnabled:     true,
				RepairAttempt:     5,
				MaxRepairAttempts: 3,
				StrictMode:        true,
			},
			expectedState: StateFailed,
		},
		{
			name: "repair disabled, strict -> Failed",
			ctx: TransitionContext{
				RepairEnabled: false,
				StrictMode:    true,
			},
			expectedState: StateFailed,
		},
		{
			name: "repair disabled, lenient -> NeedsAttention",
			ctx: TransitionContext{
				RepairEnabled: false,
				StrictMode:    false,
			},
			expectedState: StateNeedsAttention,
		},
		{
			name: "max repair attempts = 0, strict -> Failed",
			ctx: TransitionContext{
				RepairEnabled:     true,
				RepairAttempt:     0,
				MaxRepairAttempts: 0,
				StrictMode:        true,
			},
			expectedState: StateFailed,
		},
		{
			name: "max repair attempts = 0, lenient -> NeedsAttention",
			ctx: TransitionContext{
				RepairEnabled:     true,
				RepairAttempt:     0,
				MaxRepairAttempts: 0,
				StrictMode:        false,
			},
			expectedState: StateNeedsAttention,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(WithInitialState(StateValidating))
			err := l.Transition(EventValidationFailed, tt.ctx)

			if err != nil {
				t.Fatalf("Transition() returned unexpected error: %v", err)
			}

			if l.State() != tt.expectedState {
				t.Errorf("got state %s, want %s", l.State(), tt.expectedState)
			}
		})
	}
}

// TestWorkFailedBranchCoverage tests all branches of WorkFailed handling.
func TestWorkFailedBranchCoverage(t *testing.T) {
	tests := []struct {
		name              string
		attemptsRemaining int
		expectedState     AgentLifecycleState
	}{
		{"attempts = 0 -> Failed", 0, StateFailed},
		{"attempts = 1 -> Running", 1, StateRunning},
		{"attempts = 10 -> Running", 10, StateRunning},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(WithInitialState(StateRunning))
			err := l.Transition(EventWorkFailed, TransitionContext{AttemptsRemaining: tt.attemptsRemaining})

			if err != nil {
				t.Fatalf("Transition() returned unexpected error: %v", err)
			}

			if l.State() != tt.expectedState {
				t.Errorf("got state %s, want %s", l.State(), tt.expectedState)
			}
		})
	}
}

// TestRetryBranchCoverage tests all branches of Retry handling from MergeFailed.
func TestRetryBranchCoverage(t *testing.T) {
	tests := []struct {
		name              string
		attemptsRemaining int
		expectedState     AgentLifecycleState
	}{
		{"attempts = 0 -> Failed", 0, StateFailed},
		{"attempts = 1 -> Running", 1, StateRunning},
		{"attempts = 5 -> Running", 5, StateRunning},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(WithInitialState(StateMergeFailed))
			err := l.Transition(EventRetry, TransitionContext{AttemptsRemaining: tt.attemptsRemaining})

			if err != nil {
				t.Fatalf("Transition() returned unexpected error: %v", err)
			}

			if l.State() != tt.expectedState {
				t.Errorf("got state %s, want %s", l.State(), tt.expectedState)
			}
		})
	}
}

// TestMergeSuccessBranchCoverage tests ValidationEnabled branching.
func TestMergeSuccessBranchCoverage(t *testing.T) {
	tests := []struct {
		name              string
		validationEnabled bool
		expectedState     AgentLifecycleState
	}{
		{"validation disabled -> Completed", false, StateCompleted},
		{"validation enabled -> Validating", true, StateValidating},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(WithInitialState(StateMerging))
			err := l.Transition(EventMergeSuccess, TransitionContext{ValidationEnabled: tt.validationEnabled})

			if err != nil {
				t.Fatalf("Transition() returned unexpected error: %v", err)
			}

			if l.State() != tt.expectedState {
				t.Errorf("got state %s, want %s", l.State(), tt.expectedState)
			}
		})
	}
}

// TestResolveSuccessBranchCoverage tests ValidationEnabled branching after resolve.
func TestResolveSuccessBranchCoverage(t *testing.T) {
	tests := []struct {
		name              string
		validationEnabled bool
		expectedState     AgentLifecycleState
	}{
		{"validation disabled -> Completed", false, StateCompleted},
		{"validation enabled -> Validating", true, StateValidating},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(WithInitialState(StateResolving))
			err := l.Transition(EventResolveSuccess, TransitionContext{ValidationEnabled: tt.validationEnabled})

			if err != nil {
				t.Fatalf("Transition() returned unexpected error: %v", err)
			}

			if l.State() != tt.expectedState {
				t.Errorf("got state %s, want %s", l.State(), tt.expectedState)
			}
		})
	}
}

// Property Tests

// TestPropertyTerminalStatesNeverTransitionOut verifies the fundamental property
// that terminal states never allow any transitions.
func TestPropertyTerminalStatesNeverTransitionOut(t *testing.T) {
	// For every terminal state
	for _, state := range terminalStates {
		// For every possible event
		for _, event := range allEvents {
			// For various contexts
			contexts := []TransitionContext{
				{},
				{ValidationEnabled: true},
				{RepairEnabled: true, RepairAttempt: 0, MaxRepairAttempts: 3},
				{AttemptsRemaining: 10},
				{StrictMode: true},
			}

			for _, ctx := range contexts {
				l := New(WithInitialState(state))
				initialState := l.State()
				_ = l.Transition(event, ctx) // ignore error

				if l.State() != initialState {
					t.Errorf("Property violation: terminal state %s changed to %s on event %s",
						initialState, l.State(), event)
				}
			}
		}
	}
}

// TestPropertyStateAfterTransitionMatchesExpected verifies that successful
// transitions always result in the documented target state.
func TestPropertyStateAfterTransitionMatchesExpected(t *testing.T) {
	// Build a map of expected transitions
	type transitionKey struct {
		from  AgentLifecycleState
		event AgentEvent
	}

	// This is a simplified version; real test uses context-aware expected states
	simpleExpected := map[transitionKey]AgentLifecycleState{
		{StateStarting, EventAgentSpawned}:       StateRunning,
		{StateRunning, EventWorkComplete}:        StateQueuedForMerge,
		{StateQueuedForMerge, EventMergeStarted}: StateMerging,
		{StateMerging, EventMergeConflict}:       StateResolving,
		{StateResolving, EventResolveFailed}:     StateMergeFailed,
		{StateValidating, EventValidationPassed}: StateCompleted,
		{StateRepairing, EventRepairComplete}:    StateValidating,
		{StateMergeFailed, EventRetriesExhausted}: StateFailed,
	}

	for key, expected := range simpleExpected {
		l := New(WithInitialState(key.from))
		err := l.Transition(key.event, TransitionContext{})

		if err != nil {
			t.Errorf("Unexpected error for %s + %s: %v", key.from, key.event, err)
			continue
		}

		if l.State() != expected {
			t.Errorf("Property violation: %s + %s = %s, want %s",
				key.from, key.event, l.State(), expected)
		}
	}
}

// TestPropertyHistoryAppendedCorrectly verifies that each transition
// appends exactly one entry to history.
func TestPropertyHistoryAppendedCorrectly(t *testing.T) {
	l := New()

	transitions := []struct {
		event AgentEvent
		ctx   TransitionContext
	}{
		{EventAgentSpawned, TransitionContext{}},
		{EventWorkComplete, TransitionContext{}},
		{EventMergeStarted, TransitionContext{}},
		{EventMergeSuccess, TransitionContext{ValidationEnabled: true}},
		{EventValidationPassed, TransitionContext{}},
	}

	for i, tr := range transitions {
		beforeLen := len(l.History())
		err := l.Transition(tr.event, tr.ctx)
		afterLen := len(l.History())

		if err != nil {
			t.Fatalf("Transition %d failed: %v", i, err)
		}

		if afterLen != beforeLen+1 {
			t.Errorf("History length after transition %d: got %d, want %d",
				i, afterLen, beforeLen+1)
		}

		// Verify the last entry matches
		history := l.History()
		last := history[len(history)-1]
		if last.Event != tr.event {
			t.Errorf("Last history entry event: got %s, want %s", last.Event, tr.event)
		}
	}
}

// TestPropertyHistoryFromToMatch verifies that history entries correctly
// record the from and to states.
func TestPropertyHistoryFromToMatch(t *testing.T) {
	l := New()

	// Perform a series of transitions
	l.Transition(EventAgentSpawned, TransitionContext{})
	l.Transition(EventWorkComplete, TransitionContext{})
	l.Transition(EventMergeStarted, TransitionContext{})

	history := l.History()

	// Check consecutive entries have matching states
	for i := 1; i < len(history); i++ {
		prev := history[i-1]
		curr := history[i]

		if prev.To != curr.From {
			t.Errorf("History discontinuity at index %d: entry %d To=%s, entry %d From=%s",
				i, i-1, prev.To, i, curr.From)
		}
	}
}

// TestPropertyFailedTransitionPreservesState verifies that failed transitions
// do not modify any state.
func TestPropertyFailedTransitionPreservesState(t *testing.T) {
	invalidTransitions := []struct {
		state AgentLifecycleState
		event AgentEvent
	}{
		{StateStarting, EventWorkComplete},
		{StateRunning, EventMergeSuccess},
		{StateCompleted, EventAgentSpawned},
		{StateFailed, EventRetry},
	}

	for _, tr := range invalidTransitions {
		l := New(WithInitialState(tr.state))
		beforeState := l.State()
		beforeHistory := l.History()
		beforeCtx := l.Context()

		err := l.Transition(tr.event, TransitionContext{ValidationEnabled: true})

		if err == nil {
			t.Errorf("Expected error for %s + %s", tr.state, tr.event)
			continue
		}

		// Verify nothing changed
		if l.State() != beforeState {
			t.Errorf("State changed on failed transition: got %s, want %s", l.State(), beforeState)
		}

		if len(l.History()) != len(beforeHistory) {
			t.Errorf("History length changed on failed transition")
		}

		if l.Context() != beforeCtx {
			t.Errorf("Context changed on failed transition")
		}
	}
}

// Concurrency Tests

// TestConcurrentTransitionsNoCorruption verifies that parallel Transition()
// calls don't corrupt internal state.
func TestConcurrentTransitionsNoCorruption(t *testing.T) {
	const numGoroutines = 100
	const transitionsPerGoroutine = 50

	l := New(WithInitialState(StateRunning))

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// All goroutines will try WorkFailed with retries, which keeps us in Running
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < transitionsPerGoroutine; j++ {
				_ = l.Transition(EventWorkFailed, TransitionContext{AttemptsRemaining: 1})
			}
		}()
	}

	wg.Wait()

	// State should still be Running (WorkFailed with retries stays in Running)
	if l.State() != StateRunning {
		t.Errorf("State corrupted: got %s, want %s", l.State(), StateRunning)
	}

	// History should have exactly numGoroutines * transitionsPerGoroutine entries
	// (up to maxHistoryEntries)
	expectedEntries := numGoroutines * transitionsPerGoroutine
	if expectedEntries > maxHistoryEntries {
		expectedEntries = maxHistoryEntries
	}
	actualEntries := len(l.History())
	if actualEntries != expectedEntries {
		t.Errorf("History count: got %d, want %d", actualEntries, expectedEntries)
	}
}

// TestConcurrentReadsNoRace verifies that concurrent reads don't race.
func TestConcurrentReadsNoRace(t *testing.T) {
	l := New(WithInitialState(StateRunning))
	l.Transition(EventWorkComplete, TransitionContext{})

	var wg sync.WaitGroup
	wg.Add(300)

	// Concurrent State() calls
	for i := 0; i < 100; i++ {
		go func() {
			defer wg.Done()
			_ = l.State()
		}()
	}

	// Concurrent IsTerminal() calls
	for i := 0; i < 100; i++ {
		go func() {
			defer wg.Done()
			_ = l.IsTerminal()
		}()
	}

	// Concurrent History() calls
	for i := 0; i < 100; i++ {
		go func() {
			defer wg.Done()
			_ = l.History()
		}()
	}

	wg.Wait()
}

// TestConcurrentReadWriteNoRace verifies mixed read/write doesn't race.
func TestConcurrentReadWriteNoRace(t *testing.T) {
	l := New(WithInitialState(StateRunning))

	var wg sync.WaitGroup
	wg.Add(200)

	// Writers
	for i := 0; i < 100; i++ {
		go func() {
			defer wg.Done()
			_ = l.Transition(EventWorkFailed, TransitionContext{AttemptsRemaining: 1})
		}()
	}

	// Readers
	for i := 0; i < 100; i++ {
		go func() {
			defer wg.Done()
			_ = l.State()
			_ = l.History()
			_ = l.Context()
			_ = l.IsTerminal()
		}()
	}

	wg.Wait()
}

// TestCallbackCalledUnderLock verifies the callback is called atomically
// with the state change.
func TestCallbackCalledUnderLock(t *testing.T) {
	var callbackCalls int64

	cb := func(from, to AgentLifecycleState, event AgentEvent) {
		atomic.AddInt64(&callbackCalls, 1)
		_ = to // use to prevent unused variable warning
	}

	l := New(WithTransitionCallback(cb))

	// Perform many concurrent transitions
	var wg sync.WaitGroup
	const numGoroutines = 50

	l.Transition(EventAgentSpawned, TransitionContext{}) // Get to Running first

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_ = l.Transition(EventWorkFailed, TransitionContext{AttemptsRemaining: 1})
		}()
	}

	wg.Wait()

	// Should have 1 (for AgentSpawned) + numGoroutines callbacks
	expected := int64(1 + numGoroutines)
	actual := atomic.LoadInt64(&callbackCalls)
	if actual != expected {
		t.Errorf("Callback calls: got %d, want %d", actual, expected)
	}
}

// TestConcurrentCancelRace tests that Cancel from multiple goroutines
// results in exactly one transition to Cancelled.
func TestConcurrentCancelRace(t *testing.T) {
	const iterations = 100

	for i := 0; i < iterations; i++ {
		l := New(WithInitialState(StateRunning))

		var wg sync.WaitGroup
		var successCount int64

		// Try to cancel from multiple goroutines simultaneously
		for j := 0; j < 10; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := l.Transition(EventCancel, TransitionContext{})
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				}
			}()
		}

		wg.Wait()

		// Exactly one Cancel should succeed
		if successCount != 1 {
			t.Errorf("Iteration %d: expected 1 successful cancel, got %d", i, successCount)
		}

		// State should be Cancelled
		if l.State() != StateCancelled {
			t.Errorf("Iteration %d: expected Cancelled, got %s", i, l.State())
		}
	}
}

// TestHistorySliceIndependentOfInternal verifies that History() returns
// a copy, not a reference to internal state.
func TestHistorySliceIndependentOfInternal(t *testing.T) {
	l := New()
	l.Transition(EventAgentSpawned, TransitionContext{})
	l.Transition(EventWorkComplete, TransitionContext{})

	history1 := l.History()
	len1 := len(history1)

	// Modify the returned slice
	history1[0].Event = "modified"

	// Get a new copy
	history2 := l.History()

	// Internal state should be unchanged
	if history2[0].Event == "modified" {
		t.Error("History() returned reference to internal slice")
	}

	// Perform another transition
	l.Transition(EventMergeStarted, TransitionContext{})

	// Original slice should be unchanged
	if len(history1) != len1 {
		t.Error("Original history slice was modified")
	}
}

// TestContextCopiedNotReferenced verifies Context() returns a copy.
func TestContextCopiedNotReferenced(t *testing.T) {
	l := New()
	ctx := TransitionContext{
		ValidationEnabled: true,
		AttemptsRemaining: 5,
	}
	l.Transition(EventAgentSpawned, ctx)

	got1 := l.Context()
	got1.AttemptsRemaining = 999 // modify the copy

	got2 := l.Context()
	if got2.AttemptsRemaining == 999 {
		t.Error("Context() returned reference, not copy")
	}
}
