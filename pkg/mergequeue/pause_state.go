package mergequeue

import (
	"context"
	"sync"
)

// PauseState represents the possible states of the pause state machine.
type PauseState int

const (
	// Running indicates the system is running normally.
	Running PauseState = iota
	// PausedUser indicates the system was paused by user request.
	PausedUser
	// PausedAgent indicates the system was paused by an agent (resolver, repair, etc.).
	PausedAgent
	// PausedBoth indicates both user and agent have paused the system.
	PausedBoth
)

// String returns a human-readable representation of the state.
func (s PauseState) String() string {
	switch s {
	case Running:
		return "running"
	case PausedUser:
		return "paused_user"
	case PausedAgent:
		return "paused_agent"
	case PausedBoth:
		return "paused_both"
	default:
		return "unknown"
	}
}

// PauseStateMachine manages pause state from two independent sources:
// user-initiated pauses and agent-initiated pauses. The system only
// runs when neither source has paused it.
type PauseStateMachine struct {
	mu    sync.Mutex
	cond  *sync.Cond
	state PauseState
}

// NewPauseStateMachine creates a new pause state machine in the Running state.
func NewPauseStateMachine() *PauseStateMachine {
	psm := &PauseStateMachine{
		state: Running,
	}
	psm.cond = sync.NewCond(&psm.mu)
	return psm
}

// State returns the current pause state.
func (psm *PauseStateMachine) State() PauseState {
	psm.mu.Lock()
	defer psm.mu.Unlock()
	return psm.state
}

// IsRunning returns true if the system is in the Running state.
func (psm *PauseStateMachine) IsRunning() bool {
	psm.mu.Lock()
	defer psm.mu.Unlock()
	return psm.state == Running
}

// IsPausedByUser returns true if the user has paused the system
// (either PausedUser or PausedBoth).
func (psm *PauseStateMachine) IsPausedByUser() bool {
	psm.mu.Lock()
	defer psm.mu.Unlock()
	return psm.state == PausedUser || psm.state == PausedBoth
}

// IsPausedByAgent returns true if an agent has paused the system
// (either PausedAgent or PausedBoth).
func (psm *PauseStateMachine) IsPausedByAgent() bool {
	psm.mu.Lock()
	defer psm.mu.Unlock()
	return psm.state == PausedAgent || psm.state == PausedBoth
}

// UserPause transitions the state machine to account for a user-initiated pause.
// State transitions:
//   - Running -> PausedUser
//   - PausedAgent -> PausedBoth
//   - PausedUser, PausedBoth -> no change (already paused by user)
func (psm *PauseStateMachine) UserPause() {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	switch psm.state {
	case Running:
		psm.state = PausedUser
	case PausedAgent:
		psm.state = PausedBoth
	// PausedUser, PausedBoth: already paused by user, no change
	}
}

// UserResume transitions the state machine to account for a user-initiated resume.
// State transitions:
//   - PausedUser -> Running
//   - PausedBoth -> PausedAgent
//   - Running, PausedAgent -> no change (user hasn't paused)
//
// Broadcasts to wake any goroutines waiting in WaitUntilRunning if
// the state becomes Running.
func (psm *PauseStateMachine) UserResume() {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	switch psm.state {
	case PausedUser:
		psm.state = Running
		psm.cond.Broadcast()
	case PausedBoth:
		psm.state = PausedAgent
	// Running, PausedAgent: user hasn't paused, no change
	}
}

// AgentPause transitions the state machine to account for an agent-initiated pause.
// State transitions:
//   - Running -> PausedAgent
//   - PausedUser -> PausedBoth
//   - PausedAgent, PausedBoth -> no change (already paused by agent)
func (psm *PauseStateMachine) AgentPause() {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	switch psm.state {
	case Running:
		psm.state = PausedAgent
	case PausedUser:
		psm.state = PausedBoth
	// PausedAgent, PausedBoth: already paused by agent, no change
	}
}

// AgentResume transitions the state machine to account for an agent-initiated resume.
// State transitions:
//   - PausedAgent -> Running
//   - PausedBoth -> PausedUser
//   - Running, PausedUser -> no change (agent hasn't paused)
//
// Broadcasts to wake any goroutines waiting in WaitUntilRunning if
// the state becomes Running.
func (psm *PauseStateMachine) AgentResume() {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	switch psm.state {
	case PausedAgent:
		psm.state = Running
		psm.cond.Broadcast()
	case PausedBoth:
		psm.state = PausedUser
	// Running, PausedUser: agent hasn't paused, no change
	}
}

// WaitUntilRunning blocks until the state machine enters the Running state
// or the context is cancelled. Returns nil if the state is Running,
// or the context error if cancelled.
func (psm *PauseStateMachine) WaitUntilRunning(ctx context.Context) error {
	// Check context early
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	psm.mu.Lock()
	defer psm.mu.Unlock()

	for psm.state != Running {
		// Create a channel to receive context cancellation
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				psm.mu.Lock()
				psm.cond.Broadcast()
				psm.mu.Unlock()
			case <-done:
			}
		}()

		psm.cond.Wait()
		close(done)

		// Check if we woke due to context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	return nil
}
