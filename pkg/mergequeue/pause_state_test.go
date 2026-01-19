package mergequeue

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewPauseStateMachine(t *testing.T) {
	psm := NewPauseStateMachine()
	if psm == nil {
		t.Fatal("NewPauseStateMachine returned nil")
	}
	if psm.State() != Running {
		t.Errorf("expected initial state Running, got %v", psm.State())
	}
}

func TestPauseStateString(t *testing.T) {
	tests := []struct {
		state PauseState
		want  string
	}{
		{Running, "running"},
		{PausedUser, "paused_user"},
		{PausedAgent, "paused_agent"},
		{PausedBoth, "paused_both"},
		{PauseState(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("PauseState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestIsRunning(t *testing.T) {
	psm := NewPauseStateMachine()
	if !psm.IsRunning() {
		t.Error("new state machine should be running")
	}

	psm.UserPause()
	if psm.IsRunning() {
		t.Error("should not be running after UserPause")
	}

	psm.UserResume()
	if !psm.IsRunning() {
		t.Error("should be running after UserResume")
	}
}

func TestIsPausedByUser(t *testing.T) {
	psm := NewPauseStateMachine()
	if psm.IsPausedByUser() {
		t.Error("new state machine should not be paused by user")
	}

	psm.UserPause()
	if !psm.IsPausedByUser() {
		t.Error("should be paused by user after UserPause")
	}

	psm.AgentPause()
	if !psm.IsPausedByUser() {
		t.Error("should still be paused by user in PausedBoth")
	}

	psm.UserResume()
	if psm.IsPausedByUser() {
		t.Error("should not be paused by user after UserResume")
	}
}

func TestIsPausedByAgent(t *testing.T) {
	psm := NewPauseStateMachine()
	if psm.IsPausedByAgent() {
		t.Error("new state machine should not be paused by agent")
	}

	psm.AgentPause()
	if !psm.IsPausedByAgent() {
		t.Error("should be paused by agent after AgentPause")
	}

	psm.UserPause()
	if !psm.IsPausedByAgent() {
		t.Error("should still be paused by agent in PausedBoth")
	}

	psm.AgentResume()
	if psm.IsPausedByAgent() {
		t.Error("should not be paused by agent after AgentResume")
	}
}

// State transition tests

func TestUserPauseTransitions(t *testing.T) {
	tests := []struct {
		name       string
		initial    func(*PauseStateMachine)
		wantBefore PauseState
		wantAfter  PauseState
	}{
		{
			name:       "Running -> PausedUser",
			initial:    func(psm *PauseStateMachine) {},
			wantBefore: Running,
			wantAfter:  PausedUser,
		},
		{
			name:       "PausedAgent -> PausedBoth",
			initial:    func(psm *PauseStateMachine) { psm.AgentPause() },
			wantBefore: PausedAgent,
			wantAfter:  PausedBoth,
		},
		{
			name:       "PausedUser -> PausedUser (no change)",
			initial:    func(psm *PauseStateMachine) { psm.UserPause() },
			wantBefore: PausedUser,
			wantAfter:  PausedUser,
		},
		{
			name: "PausedBoth -> PausedBoth (no change)",
			initial: func(psm *PauseStateMachine) {
				psm.UserPause()
				psm.AgentPause()
			},
			wantBefore: PausedBoth,
			wantAfter:  PausedBoth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			psm := NewPauseStateMachine()
			tt.initial(psm)
			if got := psm.State(); got != tt.wantBefore {
				t.Errorf("before UserPause: got %v, want %v", got, tt.wantBefore)
			}
			psm.UserPause()
			if got := psm.State(); got != tt.wantAfter {
				t.Errorf("after UserPause: got %v, want %v", got, tt.wantAfter)
			}
		})
	}
}

func TestUserResumeTransitions(t *testing.T) {
	tests := []struct {
		name       string
		initial    func(*PauseStateMachine)
		wantBefore PauseState
		wantAfter  PauseState
	}{
		{
			name:       "PausedUser -> Running",
			initial:    func(psm *PauseStateMachine) { psm.UserPause() },
			wantBefore: PausedUser,
			wantAfter:  Running,
		},
		{
			name: "PausedBoth -> PausedAgent",
			initial: func(psm *PauseStateMachine) {
				psm.UserPause()
				psm.AgentPause()
			},
			wantBefore: PausedBoth,
			wantAfter:  PausedAgent,
		},
		{
			name:       "Running -> Running (no change)",
			initial:    func(psm *PauseStateMachine) {},
			wantBefore: Running,
			wantAfter:  Running,
		},
		{
			name:       "PausedAgent -> PausedAgent (no change)",
			initial:    func(psm *PauseStateMachine) { psm.AgentPause() },
			wantBefore: PausedAgent,
			wantAfter:  PausedAgent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			psm := NewPauseStateMachine()
			tt.initial(psm)
			if got := psm.State(); got != tt.wantBefore {
				t.Errorf("before UserResume: got %v, want %v", got, tt.wantBefore)
			}
			psm.UserResume()
			if got := psm.State(); got != tt.wantAfter {
				t.Errorf("after UserResume: got %v, want %v", got, tt.wantAfter)
			}
		})
	}
}

func TestAgentPauseTransitions(t *testing.T) {
	tests := []struct {
		name       string
		initial    func(*PauseStateMachine)
		wantBefore PauseState
		wantAfter  PauseState
	}{
		{
			name:       "Running -> PausedAgent",
			initial:    func(psm *PauseStateMachine) {},
			wantBefore: Running,
			wantAfter:  PausedAgent,
		},
		{
			name:       "PausedUser -> PausedBoth",
			initial:    func(psm *PauseStateMachine) { psm.UserPause() },
			wantBefore: PausedUser,
			wantAfter:  PausedBoth,
		},
		{
			name:       "PausedAgent -> PausedAgent (no change)",
			initial:    func(psm *PauseStateMachine) { psm.AgentPause() },
			wantBefore: PausedAgent,
			wantAfter:  PausedAgent,
		},
		{
			name: "PausedBoth -> PausedBoth (no change)",
			initial: func(psm *PauseStateMachine) {
				psm.UserPause()
				psm.AgentPause()
			},
			wantBefore: PausedBoth,
			wantAfter:  PausedBoth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			psm := NewPauseStateMachine()
			tt.initial(psm)
			if got := psm.State(); got != tt.wantBefore {
				t.Errorf("before AgentPause: got %v, want %v", got, tt.wantBefore)
			}
			psm.AgentPause()
			if got := psm.State(); got != tt.wantAfter {
				t.Errorf("after AgentPause: got %v, want %v", got, tt.wantAfter)
			}
		})
	}
}

func TestAgentResumeTransitions(t *testing.T) {
	tests := []struct {
		name       string
		initial    func(*PauseStateMachine)
		wantBefore PauseState
		wantAfter  PauseState
	}{
		{
			name:       "PausedAgent -> Running",
			initial:    func(psm *PauseStateMachine) { psm.AgentPause() },
			wantBefore: PausedAgent,
			wantAfter:  Running,
		},
		{
			name: "PausedBoth -> PausedUser",
			initial: func(psm *PauseStateMachine) {
				psm.UserPause()
				psm.AgentPause()
			},
			wantBefore: PausedBoth,
			wantAfter:  PausedUser,
		},
		{
			name:       "Running -> Running (no change)",
			initial:    func(psm *PauseStateMachine) {},
			wantBefore: Running,
			wantAfter:  Running,
		},
		{
			name:       "PausedUser -> PausedUser (no change)",
			initial:    func(psm *PauseStateMachine) { psm.UserPause() },
			wantBefore: PausedUser,
			wantAfter:  PausedUser,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			psm := NewPauseStateMachine()
			tt.initial(psm)
			if got := psm.State(); got != tt.wantBefore {
				t.Errorf("before AgentResume: got %v, want %v", got, tt.wantBefore)
			}
			psm.AgentResume()
			if got := psm.State(); got != tt.wantAfter {
				t.Errorf("after AgentResume: got %v, want %v", got, tt.wantAfter)
			}
		})
	}
}

// WaitUntilRunning tests

func TestWaitUntilRunningAlreadyRunning(t *testing.T) {
	psm := NewPauseStateMachine()
	ctx := context.Background()

	err := psm.WaitUntilRunning(ctx)
	if err != nil {
		t.Errorf("WaitUntilRunning should return nil when already running, got %v", err)
	}
}

func TestWaitUntilRunningUserResume(t *testing.T) {
	psm := NewPauseStateMachine()
	psm.UserPause()

	done := make(chan error)
	go func() {
		done <- psm.WaitUntilRunning(context.Background())
	}()

	// Give time for goroutine to start waiting
	time.Sleep(50 * time.Millisecond)

	// Verify still waiting
	select {
	case <-done:
		t.Fatal("WaitUntilRunning should block while paused")
	default:
	}

	psm.UserResume()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("WaitUntilRunning should return nil after resume, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WaitUntilRunning should return after UserResume")
	}
}

func TestWaitUntilRunningAgentResume(t *testing.T) {
	psm := NewPauseStateMachine()
	psm.AgentPause()

	done := make(chan error)
	go func() {
		done <- psm.WaitUntilRunning(context.Background())
	}()

	time.Sleep(50 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("WaitUntilRunning should block while paused")
	default:
	}

	psm.AgentResume()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("WaitUntilRunning should return nil after resume, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WaitUntilRunning should return after AgentResume")
	}
}

func TestWaitUntilRunningContextCancelled(t *testing.T) {
	psm := NewPauseStateMachine()
	psm.UserPause()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)

	go func() {
		done <- psm.WaitUntilRunning(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WaitUntilRunning should return after context cancellation")
	}
}

func TestWaitUntilRunningContextAlreadyCancelled(t *testing.T) {
	psm := NewPauseStateMachine()
	psm.UserPause()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before calling

	err := psm.WaitUntilRunning(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled for pre-cancelled context, got %v", err)
	}
}

func TestWaitUntilRunningTimeout(t *testing.T) {
	psm := NewPauseStateMachine()
	psm.UserPause()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := psm.WaitUntilRunning(ctx)
	elapsed := time.Since(start)

	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}

	if elapsed < 40*time.Millisecond || elapsed > 150*time.Millisecond {
		t.Errorf("expected ~50ms wait, got %v", elapsed)
	}
}

func TestWaitUntilRunningPausedBothNeedsBothResume(t *testing.T) {
	psm := NewPauseStateMachine()
	psm.UserPause()
	psm.AgentPause()

	done := make(chan error)
	go func() {
		done <- psm.WaitUntilRunning(context.Background())
	}()

	time.Sleep(50 * time.Millisecond)

	// Resume only user - should still be blocked
	psm.UserResume()
	time.Sleep(50 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("WaitUntilRunning should still block when agent is paused")
	default:
	}

	// Now resume agent
	psm.AgentResume()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("WaitUntilRunning should return nil, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WaitUntilRunning should return after both resume")
	}
}

// Concurrent access tests

func TestConcurrentStateTransitions(t *testing.T) {
	psm := NewPauseStateMachine()
	var wg sync.WaitGroup

	// Multiple goroutines doing pause/resume
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			psm.UserPause()
			psm.UserResume()
		}()
		go func() {
			defer wg.Done()
			psm.AgentPause()
			psm.AgentResume()
		}()
	}

	wg.Wait()

	// After all paired pause/resume, should be running
	if !psm.IsRunning() {
		t.Errorf("expected Running state after paired pause/resume, got %v", psm.State())
	}
}

func TestConcurrentWaitAndResume(t *testing.T) {
	psm := NewPauseStateMachine()
	psm.UserPause()

	const numWaiters = 10
	done := make(chan struct{}, numWaiters)

	// Start multiple waiters
	for i := 0; i < numWaiters; i++ {
		go func() {
			_ = psm.WaitUntilRunning(context.Background())
			done <- struct{}{}
		}()
	}

	time.Sleep(50 * time.Millisecond)

	// Resume should wake all waiters
	psm.UserResume()

	for i := 0; i < numWaiters; i++ {
		select {
		case <-done:
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("waiter %d did not wake up", i)
		}
	}
}

func TestIdempotentOperations(t *testing.T) {
	psm := NewPauseStateMachine()

	// Multiple pauses should be idempotent
	psm.UserPause()
	psm.UserPause()
	psm.UserPause()
	if psm.State() != PausedUser {
		t.Errorf("expected PausedUser after multiple UserPause, got %v", psm.State())
	}

	// Multiple resumes should be idempotent
	psm.UserResume()
	psm.UserResume()
	psm.UserResume()
	if psm.State() != Running {
		t.Errorf("expected Running after multiple UserResume, got %v", psm.State())
	}

	// Same for agent
	psm.AgentPause()
	psm.AgentPause()
	if psm.State() != PausedAgent {
		t.Errorf("expected PausedAgent after multiple AgentPause, got %v", psm.State())
	}

	psm.AgentResume()
	psm.AgentResume()
	if psm.State() != Running {
		t.Errorf("expected Running after multiple AgentResume, got %v", psm.State())
	}
}

func TestStateQueryDuringTransition(t *testing.T) {
	psm := NewPauseStateMachine()
	var wg sync.WaitGroup

	// Constantly query state while transitioning
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				// These should never panic and always return valid states
				_ = psm.State()
				_ = psm.IsRunning()
				_ = psm.IsPausedByUser()
				_ = psm.IsPausedByAgent()
			}
		}
	}()

	// Do many transitions
	for i := 0; i < 1000; i++ {
		psm.UserPause()
		psm.AgentPause()
		psm.UserResume()
		psm.AgentResume()
	}

	close(stop)
	wg.Wait()
}
