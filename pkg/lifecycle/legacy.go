package lifecycle

import (
	"github.com/jzila/canopy/pkg/types"
)

// LegacyStatus returns the legacy AgentStatus for backwards compatibility.
// This maps the unified lifecycle state to the original 6-value status enum.
func (l *AgentLifecycle) LegacyStatus() types.AgentStatus {
	switch l.State() {
	case StateStarting:
		return types.AgentStatusStarting
	case StateRunning, StateQueuedForMerge, StateMerging, StateResolving, StateValidating, StateRepairing:
		return types.AgentStatusRunning
	case StateCompleted:
		return types.AgentStatusCompleted
	case StateFailed, StateMergeFailed, StateNeedsAttention:
		return types.AgentStatusFailed
	case StateCancelled:
		return types.AgentStatusCancelled
	case StateTimedOut:
		return types.AgentStatusTimedOut
	default:
		return types.AgentStatusRunning
	}
}

// LegacyMergeStatus returns the legacy MergeStatus for backwards compatibility.
// This maps the unified lifecycle state to the merge phase.
func (l *AgentLifecycle) LegacyMergeStatus() types.MergeStatus {
	switch l.State() {
	case StateStarting, StateRunning:
		return types.MergeStatusNone
	case StateQueuedForMerge:
		return types.MergeStatusPending
	case StateMerging:
		return types.MergeStatusMerging
	case StateResolving:
		return types.MergeStatusResolving
	case StateValidating, StateRepairing:
		// Validation and repair happen AFTER successful merge
		return types.MergeStatusMerged
	case StateCompleted:
		return types.MergeStatusMerged
	case StateFailed, StateMergeFailed:
		return types.MergeStatusFailed
	case StateNeedsAttention:
		return types.MergeStatusMergedNeedsRepair
	case StateCancelled, StateTimedOut:
		// These can occur at any phase; report as failed if we were in merge
		return types.MergeStatusNone
	default:
		return types.MergeStatusNone
	}
}

// LegacyValidationStatus returns the legacy validation status string.
// Returns empty string for states that don't involve validation.
func (l *AgentLifecycle) LegacyValidationStatus() string {
	switch l.State() {
	case StateValidating:
		return "running"
	case StateRepairing:
		return "repairing"
	case StateCompleted:
		// Only return "passed" if we went through validation
		// Check history to see if we came from validating
		history := l.History()
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].From == StateValidating {
				return "passed"
			}
		}
		return ""
	case StateNeedsAttention:
		return "failed"
	default:
		return ""
	}
}

// LegacyStatusFromState returns the legacy AgentStatus for a given lifecycle state.
// This is a static version useful when you have a state but not the full lifecycle.
func LegacyStatusFromState(state AgentLifecycleState) types.AgentStatus {
	switch state {
	case StateStarting:
		return types.AgentStatusStarting
	case StateRunning, StateQueuedForMerge, StateMerging, StateResolving, StateValidating, StateRepairing:
		return types.AgentStatusRunning
	case StateCompleted:
		return types.AgentStatusCompleted
	case StateFailed, StateMergeFailed, StateNeedsAttention:
		return types.AgentStatusFailed
	case StateCancelled:
		return types.AgentStatusCancelled
	case StateTimedOut:
		return types.AgentStatusTimedOut
	default:
		return types.AgentStatusRunning
	}
}

// LegacyMergeStatusFromState returns the legacy MergeStatus for a given lifecycle state.
// This is a static version useful when you have a state but not the full lifecycle.
func LegacyMergeStatusFromState(state AgentLifecycleState) types.MergeStatus {
	switch state {
	case StateStarting, StateRunning:
		return types.MergeStatusNone
	case StateQueuedForMerge:
		return types.MergeStatusPending
	case StateMerging:
		return types.MergeStatusMerging
	case StateResolving:
		return types.MergeStatusResolving
	case StateValidating, StateRepairing:
		return types.MergeStatusMerged
	case StateCompleted:
		return types.MergeStatusMerged
	case StateFailed, StateMergeFailed:
		return types.MergeStatusFailed
	case StateNeedsAttention:
		return types.MergeStatusMergedNeedsRepair
	default:
		return types.MergeStatusNone
	}
}
