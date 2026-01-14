package daemon

import "github.com/jzila/canopy/pkg/events"

// Re-export EventBus types from pkg/events for backwards compatibility

// EventHandler is a function that receives events
type EventHandler = events.EventHandler

// EventBus manages event subscriptions and distribution
// It provides a thread-safe pub/sub mechanism for internal communication
// between the IPC server (publisher) and WebSocket hub (subscriber)
type EventBus = events.EventBus

// NewEventBus creates a new EventBus instance
func NewEventBus() *EventBus {
	return events.NewEventBus()
}
