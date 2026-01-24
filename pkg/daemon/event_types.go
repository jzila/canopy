package daemon

import "github.com/jzila/canopy/pkg/events"

// Type aliases - the canonical types are defined in pkg/events.
// These aliases enable pkg/daemon to reference event types without
// fully qualifying them throughout the package.

type EventHandler = events.EventHandler
type EventBus = events.EventBus
type EventType = events.EventType
type Event = events.Event

const (
	EventStateSync        = events.EventStateSync
	EventRunStarted       = events.EventRunStarted
	EventRunCompleted     = events.EventRunCompleted
	EventAgentStarted     = events.EventAgentStarted
	EventAgentRunning     = events.EventAgentRunning
	EventAgentResumed     = events.EventAgentResumed
	EventAgentOutput      = events.EventAgentOutput
	EventAgentLiveFeed    = events.EventAgentLiveFeed
	EventAgentCommit      = events.EventAgentCommit
	EventAgentMergeStatus = events.EventAgentMergeStatus
	EventAgentCompleted   = events.EventAgentCompleted
	EventAgentDone        = events.EventAgentDone
	EventAgentFailed      = events.EventAgentFailed
	EventTaskUpdated      = events.EventTaskUpdated
	EventOrchPaused       = events.EventOrchPaused
	EventOrchResumed      = events.EventOrchResumed
	EventStatsUpdated     = events.EventStatsUpdated
	EventRulesChanged     = events.EventRulesChanged
)

// NewEventBus creates a new EventBus instance.
func NewEventBus() *EventBus {
	return events.NewEventBus()
}
