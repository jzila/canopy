package daemon

import "time"

// EventType represents the type of event being sent over WebSocket
type EventType string

// Event type constants
const (
	EventStateSync      EventType = "state:sync"
	EventAgentStarted   EventType = "agent:started"
	EventAgentOutput    EventType = "agent:output"
	EventAgentLiveFeed  EventType = "agent:live_feed"
	EventAgentCompleted EventType = "agent:completed"
	EventTaskUpdated    EventType = "task:updated"
	EventOrchPaused     EventType = "orch:paused"
	EventOrchResumed    EventType = "orch:resumed"
	EventStatsUpdated   EventType = "stats:updated"
)

// Event represents a WebSocket event sent to browser clients
type Event struct {
	Type      EventType   `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}
