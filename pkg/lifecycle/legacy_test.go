package lifecycle

import (
	"testing"

	"github.com/jzila/canopy/pkg/daemon"
	"github.com/jzila/canopy/pkg/types"
)

func TestLegacyStatus(t *testing.T) {
	tests := []struct {
		state    AgentLifecycleState
		expected daemon.AgentStatus
	}{
		{StateStarting, daemon.AgentStatusStarting},
		{StateRunning, daemon.AgentStatusRunning},
		{StateQueuedForMerge, daemon.AgentStatusRunning},
		{StateMerging, daemon.AgentStatusRunning},
		{StateResolving, daemon.AgentStatusRunning},
		{StateValidating, daemon.AgentStatusRunning},
		{StateRepairing, daemon.AgentStatusRunning},
		{StateCompleted, daemon.AgentStatusCompleted},
		{StateFailed, daemon.AgentStatusFailed},
		{StateMergeFailed, daemon.AgentStatusFailed},
		{StateNeedsAttention, daemon.AgentStatusFailed},
		{StateCancelled, daemon.AgentStatusCancelled},
		{StateTimedOut, daemon.AgentStatusTimedOut},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			l := New(WithInitialState(tt.state))
			got := l.LegacyStatus()
			if got != tt.expected {
				t.Errorf("LegacyStatus() for %s = %s, want %s", tt.state, got, tt.expected)
			}
		})
	}
}

func TestLegacyStatusFromState(t *testing.T) {
	tests := []struct {
		state    AgentLifecycleState
		expected daemon.AgentStatus
	}{
		{StateStarting, daemon.AgentStatusStarting},
		{StateRunning, daemon.AgentStatusRunning},
		{StateQueuedForMerge, daemon.AgentStatusRunning},
		{StateMerging, daemon.AgentStatusRunning},
		{StateResolving, daemon.AgentStatusRunning},
		{StateValidating, daemon.AgentStatusRunning},
		{StateRepairing, daemon.AgentStatusRunning},
		{StateCompleted, daemon.AgentStatusCompleted},
		{StateFailed, daemon.AgentStatusFailed},
		{StateMergeFailed, daemon.AgentStatusFailed},
		{StateNeedsAttention, daemon.AgentStatusFailed},
		{StateCancelled, daemon.AgentStatusCancelled},
		{StateTimedOut, daemon.AgentStatusTimedOut},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			got := LegacyStatusFromState(tt.state)
			if got != tt.expected {
				t.Errorf("LegacyStatusFromState(%s) = %s, want %s", tt.state, got, tt.expected)
			}
		})
	}
}

func TestLegacyMergeStatus(t *testing.T) {
	tests := []struct {
		state    AgentLifecycleState
		expected types.MergeStatus
	}{
		{StateStarting, types.MergeStatusNone},
		{StateRunning, types.MergeStatusNone},
		{StateQueuedForMerge, types.MergeStatusPending},
		{StateMerging, types.MergeStatusMerging},
		{StateResolving, types.MergeStatusResolving},
		{StateValidating, types.MergeStatusMerged},
		{StateRepairing, types.MergeStatusMerged},
		{StateCompleted, types.MergeStatusMerged},
		{StateFailed, types.MergeStatusFailed},
		{StateMergeFailed, types.MergeStatusFailed},
		{StateNeedsAttention, types.MergeStatusMergedNeedsRepair},
		{StateCancelled, types.MergeStatusNone},
		{StateTimedOut, types.MergeStatusNone},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			l := New(WithInitialState(tt.state))
			got := l.LegacyMergeStatus()
			if got != tt.expected {
				t.Errorf("LegacyMergeStatus() for %s = %s, want %s", tt.state, got, tt.expected)
			}
		})
	}
}

func TestLegacyMergeStatusFromState(t *testing.T) {
	tests := []struct {
		state    AgentLifecycleState
		expected types.MergeStatus
	}{
		{StateStarting, types.MergeStatusNone},
		{StateRunning, types.MergeStatusNone},
		{StateQueuedForMerge, types.MergeStatusPending},
		{StateMerging, types.MergeStatusMerging},
		{StateResolving, types.MergeStatusResolving},
		{StateValidating, types.MergeStatusMerged},
		{StateRepairing, types.MergeStatusMerged},
		{StateCompleted, types.MergeStatusMerged},
		{StateFailed, types.MergeStatusFailed},
		{StateMergeFailed, types.MergeStatusFailed},
		{StateNeedsAttention, types.MergeStatusMergedNeedsRepair},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			got := LegacyMergeStatusFromState(tt.state)
			if got != tt.expected {
				t.Errorf("LegacyMergeStatusFromState(%s) = %s, want %s", tt.state, got, tt.expected)
			}
		})
	}
}

func TestLegacyValidationStatus(t *testing.T) {
	tests := []struct {
		name     string
		state    AgentLifecycleState
		expected string
	}{
		{"Starting", StateStarting, ""},
		{"Running", StateRunning, ""},
		{"QueuedForMerge", StateQueuedForMerge, ""},
		{"Merging", StateMerging, ""},
		{"Resolving", StateResolving, ""},
		{"Validating", StateValidating, "running"},
		{"Repairing", StateRepairing, "repairing"},
		{"NeedsAttention", StateNeedsAttention, "failed"},
		{"Failed", StateFailed, ""},
		{"Cancelled", StateCancelled, ""},
		{"TimedOut", StateTimedOut, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(WithInitialState(tt.state))
			got := l.LegacyValidationStatus()
			if got != tt.expected {
				t.Errorf("LegacyValidationStatus() for %s = %q, want %q", tt.state, got, tt.expected)
			}
		})
	}
}

func TestLegacyValidationStatusAfterValidation(t *testing.T) {
	// Test that Completed returns "passed" if we came from Validating
	l := New()
	l.Transition(EventAgentSpawned, TransitionContext{})
	l.Transition(EventWorkComplete, TransitionContext{})
	l.Transition(EventMergeStarted, TransitionContext{})
	l.Transition(EventMergeSuccess, TransitionContext{ValidationEnabled: true})
	l.Transition(EventValidationPassed, TransitionContext{})

	if l.State() != StateCompleted {
		t.Fatalf("expected Completed, got %s", l.State())
	}

	got := l.LegacyValidationStatus()
	if got != "passed" {
		t.Errorf("LegacyValidationStatus() after validation = %q, want %q", got, "passed")
	}
}

func TestLegacyValidationStatusWithoutValidation(t *testing.T) {
	// Test that Completed returns "" if we didn't go through validation
	l := New()
	l.Transition(EventAgentSpawned, TransitionContext{})
	l.Transition(EventWorkComplete, TransitionContext{})
	l.Transition(EventMergeStarted, TransitionContext{})
	l.Transition(EventMergeSuccess, TransitionContext{ValidationEnabled: false})

	if l.State() != StateCompleted {
		t.Fatalf("expected Completed, got %s", l.State())
	}

	got := l.LegacyValidationStatus()
	if got != "" {
		t.Errorf("LegacyValidationStatus() without validation = %q, want %q", got, "")
	}
}
