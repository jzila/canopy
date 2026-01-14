package daemon

import "github.com/jzila/canopy/pkg/events"

// Re-export event types from pkg/events for backwards compatibility
type EventType = events.EventType

// Event type constants - re-exported from pkg/events
const (
	EventStateSync        = events.EventStateSync
	EventRunStarted       = events.EventRunStarted
	EventRunCompleted     = events.EventRunCompleted
	EventAgentStarted     = events.EventAgentStarted
	EventAgentOutput      = events.EventAgentOutput
	EventAgentLiveFeed    = events.EventAgentLiveFeed
	EventAgentCommit      = events.EventAgentCommit
	EventAgentMergeStatus = events.EventAgentMergeStatus
	EventAgentCompleted   = events.EventAgentCompleted
	EventTaskUpdated      = events.EventTaskUpdated
	EventOrchPaused       = events.EventOrchPaused
	EventOrchResumed      = events.EventOrchResumed
	EventStatsUpdated     = events.EventStatsUpdated
)

// Event - re-exported from pkg/events
type Event = events.Event
