// Package events provides a pub/sub event system for inter-component communication.
// This package exists to break the circular dependency between pkg/daemon and pkg/ipc.
package events

import (
	"runtime/debug"
	"sync"
	"time"

	"github.com/jzila/canopy/pkg/logging"
)

// EventType represents the type of event being sent over WebSocket
type EventType string

// Event type constants
const (
	EventStateSync          EventType = "state:sync"
	EventRunStarted         EventType = "run:started"
	EventRunCompleted       EventType = "run:completed"
	EventAgentStarted       EventType = "agent:started"
	EventAgentRunning       EventType = "agent:running" // Agent transitioned from starting to running
	EventAgentResumed       EventType = "agent:resumed" // Agent resumed after daemon restart
	EventAgentOutput        EventType = "agent:output"
	EventAgentOutputClear   EventType = "agent:output_clear"
	EventAgentLiveFeed      EventType = "agent:live_feed"
	EventAgentCommit        EventType = "agent:commit"
	EventAgentMergeStatus   EventType = "agent:merge_status"
	EventAgentCompleted     EventType = "agent:completed"
	EventAgentDone          EventType = "agent:done"   // Alias for completed (used in recovery)
	EventAgentFailed        EventType = "agent:failed" // Agent failed (used in recovery)
	EventTaskUpdated        EventType = "task:updated"
	EventOrchPaused         EventType = "orch:paused"
	EventOrchResumed        EventType = "orch:resumed"
	EventOrchStateChanged   EventType = "orch:state_changed" // Orchestrator state changed (off/idle/active/paused)
	EventStatsUpdated       EventType = "stats:updated"
	EventRulesChanged            EventType = "rules:changed"             // Rules configuration changed at runtime
	EventLifecycleStateChanged   EventType = "lifecycle:state_changed"   // Agent lifecycle state transition
	EventConfigUpdated           EventType = "config:updated"            // Run configuration updated
)

// IsCritical returns true if this event type must never be dropped.
// Critical events represent state transitions that cannot be reconstructed
// from subsequent events (e.g., agent lifecycle, run lifecycle, merge status).
func (et EventType) IsCritical() bool {
	switch et {
	case EventRunStarted, EventRunCompleted,
		EventAgentStarted, EventAgentRunning, EventAgentResumed, EventAgentCompleted,
		EventAgentDone, EventAgentFailed,
		EventAgentMergeStatus,
		EventOrchPaused, EventOrchResumed, EventOrchStateChanged,
		EventTaskUpdated,
		EventLifecycleStateChanged:
		return true
	default:
		return false
	}
}

// Event represents a WebSocket event sent to browser clients
type Event struct {
	Type      EventType   `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
	Sequence  uint64      `json:"sequence,omitempty"` // Monotonic sequence number for ordering
}

// EventHandler is a function that receives events
type EventHandler func(Event)

// subscription represents a single event subscription
type subscription struct {
	id      int
	handler EventHandler
}

// EventBus manages event subscriptions and distribution
// It provides a thread-safe pub/sub mechanism for internal communication
// between the IPC server (publisher) and WebSocket hub (subscriber)
type EventBus struct {
	mu            sync.RWMutex
	subscriptions map[int]subscription
	nextID        int
	sequence      uint64 // Monotonic sequence number for event ordering
}

// NewEventBus creates a new EventBus instance
func NewEventBus() *EventBus {
	return &EventBus{
		subscriptions: make(map[int]subscription),
		nextID:        1,
	}
}

// Subscribe registers a handler to receive events
// Returns an unsubscribe function that removes the subscription
func (eb *EventBus) Subscribe(handler EventHandler) func() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	// Assign unique ID and register subscription
	id := eb.nextID
	eb.nextID++
	eb.subscriptions[id] = subscription{
		id:      id,
		handler: handler,
	}

	// Return unsubscribe closure
	return func() {
		eb.mu.Lock()
		defer eb.mu.Unlock()
		delete(eb.subscriptions, id)
	}
}

// Publish sends an event to all subscribers
// Handlers are called synchronously in the order they subscribed
// If a handler panics, it does not affect other handlers
// Each event is assigned a monotonic sequence number for ordering.
func (eb *EventBus) Publish(event Event) {
	eb.mu.Lock()
	// Assign monotonic sequence number
	eb.sequence++
	event.Sequence = eb.sequence
	// Copy subscriptions to avoid holding lock during handler execution
	handlers := make([]EventHandler, 0, len(eb.subscriptions))
	for _, sub := range eb.subscriptions {
		handlers = append(handlers, sub.handler)
	}
	eb.mu.Unlock()

	// Execute handlers without holding lock
	for _, handler := range handlers {
		// Protect against panicking handlers
		func() {
			defer func() {
				if r := recover(); r != nil {
					logging.Error("event handler panicked",
						"event_type", event.Type,
						"panic", r,
						"stack", string(debug.Stack()),
					)
				}
			}()
			handler(event)
		}()
	}
}

// GetSequence returns the current sequence number.
// This is useful for including in snapshots so clients can discard
// events that occurred before the snapshot was taken.
func (eb *EventBus) GetSequence() uint64 {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return eb.sequence
}

// SubscriberCount returns the number of active subscriptions
// Useful for testing and debugging
func (eb *EventBus) SubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.subscriptions)
}
