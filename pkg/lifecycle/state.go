// Package lifecycle provides a state machine for managing agent lifecycle states.
//
// The lifecycle state machine replaces the previous multi-field status model
// (Status, MergeStatus, ValidationStatus) with a single authoritative state.
// This eliminates race conditions and invalid state combinations.
//
// See docs/design/agent-lifecycle-state-machine.md for the full design.
package lifecycle

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// maxHistoryEntries limits the number of state transitions kept in memory.
const maxHistoryEntries = 100

// ErrInvalidTransition indicates an event is not valid for the current state.
var ErrInvalidTransition = errors.New("invalid state transition")

// AgentLifecycleState represents the current state in the agent lifecycle.
type AgentLifecycleState string

const (
	StateStarting       AgentLifecycleState = "starting"
	StateRunning        AgentLifecycleState = "running"
	StateQueuedForMerge AgentLifecycleState = "queued_for_merge"
	StateMerging        AgentLifecycleState = "merging"
	StateResolving      AgentLifecycleState = "resolving"
	StateValidating     AgentLifecycleState = "validating"
	StateRepairing      AgentLifecycleState = "repairing"
	StateMergeFailed    AgentLifecycleState = "merge_failed"
	StateCompleted      AgentLifecycleState = "completed"
	StateFailed         AgentLifecycleState = "failed"
	StateNeedsAttention AgentLifecycleState = "needs_attention"
	StateCancelled      AgentLifecycleState = "cancelled"
	StateTimedOut       AgentLifecycleState = "timed_out"
)

// String returns the string representation of the state.
func (s AgentLifecycleState) String() string {
	return string(s)
}

// IsTerminal returns true if this state is terminal (no further transitions possible).
func (s AgentLifecycleState) IsTerminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateNeedsAttention, StateCancelled, StateTimedOut:
		return true
	}
	return false
}

// AgentEvent represents an event that can trigger a state transition.
type AgentEvent string

const (
	EventAgentSpawned      AgentEvent = "agent_spawned"
	EventWorkComplete      AgentEvent = "work_complete"
	EventWorkFailed        AgentEvent = "work_failed"
	EventMergeStarted      AgentEvent = "merge_started"
	EventMergeSuccess      AgentEvent = "merge_success"
	EventMergeConflict     AgentEvent = "merge_conflict"
	EventMergeFailed       AgentEvent = "merge_failed"
	EventResolveSuccess    AgentEvent = "resolve_success"
	EventResolveFailed     AgentEvent = "resolve_failed"
	EventValidationPassed  AgentEvent = "validation_passed"
	EventValidationFailed  AgentEvent = "validation_failed"
	EventValidationSkipped AgentEvent = "validation_skipped"
	EventRepairComplete    AgentEvent = "repair_complete"
	EventRetry             AgentEvent = "retry"
	EventRetriesExhausted  AgentEvent = "retries_exhausted"
	EventCancel            AgentEvent = "cancel"
	EventTimeout           AgentEvent = "timeout"
)

// String returns the string representation of the event.
func (e AgentEvent) String() string {
	return string(e)
}

// TransitionContext provides context for state transitions.
// This struct carries information needed to determine the correct target state.
type TransitionContext struct {
	// ValidationEnabled indicates whether post-merge validation is configured.
	ValidationEnabled bool
	// RepairEnabled indicates whether automated repair is enabled.
	RepairEnabled bool
	// StrictMode indicates whether validation failures should cause failure (vs needs_attention).
	StrictMode bool
	// AttemptsRemaining tracks remaining retry attempts for work failures.
	AttemptsRemaining int
	// RepairAttempt is the current repair attempt number (1-indexed).
	RepairAttempt int
	// MaxRepairAttempts is the maximum number of repair attempts allowed.
	MaxRepairAttempts int
	// QueuePosition is the agent's position in the merge queue.
	QueuePosition int
	// Error is an optional error message associated with the transition.
	Error string
}

// StateTransition records a state change.
type StateTransition struct {
	From      AgentLifecycleState
	To        AgentLifecycleState
	Event     AgentEvent
	Timestamp time.Time
	Context   TransitionContext
}

// TransitionCallback is called after a successful state transition.
// The callback receives the old state, new state, and the event that triggered the transition.
type TransitionCallback func(from, to AgentLifecycleState, event AgentEvent)

// TerminalCallback is called when a terminal state is reached.
// The callback receives the agent ID associated with this lifecycle.
// This is useful for cleanup operations that must occur regardless of
// how the terminal state was reached (normal completion, cancellation, timeout, etc.).
type TerminalCallback func(agentID string)

// AgentLifecycle manages state transitions for a single agent.
// All methods are thread-safe.
type AgentLifecycle struct {
	mu           sync.RWMutex
	agentID      string // Agent ID for terminal callback
	state        AgentLifecycleState
	stateHistory []StateTransition
	context      TransitionContext
	onTransition TransitionCallback
	onTerminal   TerminalCallback
}

// Option configures an AgentLifecycle instance.
type Option func(*AgentLifecycle)

// WithTransitionCallback sets a callback that fires after each state transition.
// The callback is invoked while holding the lock, so it should be fast and non-blocking.
func WithTransitionCallback(cb TransitionCallback) Option {
	return func(l *AgentLifecycle) {
		l.onTransition = cb
	}
}

// WithTerminalCallback sets a callback that fires when a terminal state is reached.
// The callback is invoked while holding the lock, so it should be fast and non-blocking.
// This is useful for cleanup operations (e.g., overlay cleanup) that must occur
// regardless of how the terminal state was reached.
func WithTerminalCallback(agentID string, cb TerminalCallback) Option {
	return func(l *AgentLifecycle) {
		l.agentID = agentID
		l.onTerminal = cb
	}
}

// WithInitialState sets the initial state (defaults to StateStarting).
func WithInitialState(state AgentLifecycleState) Option {
	return func(l *AgentLifecycle) {
		l.state = state
	}
}

// New creates a new AgentLifecycle starting in the StateStarting state.
func New(opts ...Option) *AgentLifecycle {
	l := &AgentLifecycle{
		state:        StateStarting,
		stateHistory: make([]StateTransition, 0, 16),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// State returns the current state (thread-safe).
func (l *AgentLifecycle) State() AgentLifecycleState {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.state
}

// Context returns the current transition context (thread-safe).
func (l *AgentLifecycle) Context() TransitionContext {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.context
}

// IsTerminal returns true if the current state is terminal (thread-safe).
func (l *AgentLifecycle) IsTerminal() bool {
	return l.State().IsTerminal()
}

// History returns a copy of the state transition history (thread-safe).
func (l *AgentLifecycle) History() []StateTransition {
	l.mu.RLock()
	defer l.mu.RUnlock()
	result := make([]StateTransition, len(l.stateHistory))
	copy(result, l.stateHistory)
	return result
}

// SetState forcibly sets the lifecycle to a specific state, bypassing normal transition rules.
// This should only be used for recovery when events arrive out of order and the lifecycle
// becomes stuck in an intermediate state while the merge status indicates completion.
// The event parameter describes why the state was forced (for history tracking).
// Returns the previous state.
func (l *AgentLifecycle) SetState(newState AgentLifecycleState, event AgentEvent, ctx TransitionContext) AgentLifecycleState {
	l.mu.Lock()
	defer l.mu.Unlock()

	oldState := l.state
	if oldState == newState {
		return oldState
	}

	l.state = newState
	l.context = ctx

	// Record the forced transition in history
	transition := StateTransition{
		From:      oldState,
		To:        newState,
		Event:     event,
		Timestamp: time.Now(),
		Context:   ctx,
	}
	l.stateHistory = append(l.stateHistory, transition)

	// Trim history if it exceeds the limit
	if len(l.stateHistory) > maxHistoryEntries {
		l.stateHistory = l.stateHistory[len(l.stateHistory)-maxHistoryEntries:]
	}

	// Fire callback if registered
	if l.onTransition != nil {
		l.onTransition(oldState, newState, event)
	}

	// Fire terminal callback if we transitioned to a terminal state
	if newState.IsTerminal() && l.onTerminal != nil {
		l.onTerminal(l.agentID)
	}

	return oldState
}

// Transition attempts to transition to a new state based on an event.
// Returns an error if the transition is invalid for the current state.
// The context provides additional information needed to determine the target state.
func (l *AgentLifecycle) Transition(event AgentEvent, ctx TransitionContext) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	newState, err := l.nextState(l.state, event, ctx)
	if err != nil {
		return fmt.Errorf("%w: %s + %s", ErrInvalidTransition, l.state, event)
	}

	oldState := l.state
	l.state = newState
	l.context = ctx

	// Record transition in history, maintaining bounded size
	transition := StateTransition{
		From:      oldState,
		To:        newState,
		Event:     event,
		Timestamp: time.Now(),
		Context:   ctx,
	}
	l.stateHistory = append(l.stateHistory, transition)

	// Trim history if it exceeds the limit
	if len(l.stateHistory) > maxHistoryEntries {
		// Keep the last maxHistoryEntries entries
		l.stateHistory = l.stateHistory[len(l.stateHistory)-maxHistoryEntries:]
	}

	// Fire callback if registered (still holding lock for atomicity)
	if l.onTransition != nil {
		l.onTransition(oldState, newState, event)
	}

	// Fire terminal callback if we transitioned to a terminal state
	if newState.IsTerminal() && l.onTerminal != nil {
		l.onTerminal(l.agentID)
	}

	return nil
}

// nextState determines the target state for a given current state and event.
// Returns an error if the transition is invalid.
func (l *AgentLifecycle) nextState(current AgentLifecycleState, event AgentEvent, ctx TransitionContext) (AgentLifecycleState, error) {
	// Global events that can fire from any non-terminal state
	if !current.IsTerminal() {
		switch event {
		case EventCancel:
			return StateCancelled, nil
		case EventTimeout:
			return StateTimedOut, nil
		}
	}

	// State-specific transitions
	switch current {
	case StateStarting:
		switch event {
		case EventAgentSpawned:
			return StateRunning, nil
		}

	case StateRunning:
		switch event {
		case EventWorkComplete:
			return StateQueuedForMerge, nil
		case EventWorkFailed:
			if ctx.AttemptsRemaining > 0 {
				return StateRunning, nil
			}
			return StateFailed, nil
		}

	case StateQueuedForMerge:
		switch event {
		case EventMergeStarted:
			return StateMerging, nil
		}

	case StateMerging:
		switch event {
		case EventMergeSuccess:
			if ctx.ValidationEnabled {
				return StateValidating, nil
			}
			return StateCompleted, nil
		case EventMergeConflict:
			return StateResolving, nil
		case EventMergeFailed:
			return StateMergeFailed, nil
		}

	case StateResolving:
		switch event {
		case EventResolveSuccess:
			if ctx.ValidationEnabled {
				return StateValidating, nil
			}
			return StateCompleted, nil
		case EventResolveFailed:
			return StateMergeFailed, nil
		}

	case StateValidating:
		switch event {
		case EventValidationPassed:
			return StateCompleted, nil
		case EventValidationFailed:
			// Check if repair is possible
			if ctx.RepairEnabled && ctx.RepairAttempt < ctx.MaxRepairAttempts {
				return StateRepairing, nil
			}
			// Repair exhausted or disabled
			if ctx.StrictMode {
				return StateFailed, nil
			}
			return StateNeedsAttention, nil
		case EventValidationSkipped:
			return StateCompleted, nil
		}

	case StateRepairing:
		switch event {
		case EventRepairComplete:
			// After repair completes, re-run validation
			return StateValidating, nil
		}

	case StateMergeFailed:
		switch event {
		case EventRetry:
			if ctx.AttemptsRemaining > 0 {
				return StateRunning, nil
			}
			return StateFailed, nil
		case EventRetriesExhausted:
			return StateFailed, nil
		}

	// Terminal states - no transitions allowed (except global cancel/timeout already handled)
	case StateCompleted, StateFailed, StateNeedsAttention, StateCancelled, StateTimedOut:
		// No valid transitions from terminal states
	}

	return "", fmt.Errorf("no transition defined for state %s with event %s", current, event)
}
