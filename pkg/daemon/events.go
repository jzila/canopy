package daemon

import "github.com/jzila/canopy/pkg/events"

// Re-export EventBus types from pkg/events for backwards compatibility

// EventHandler is a function that receives events
type EventHandler = events.EventHandler

// EventBus manages event subscriptions and distribution
// It provides a thread-safe pub/sub mechanism for internal communication
// between the IPC server (publisher) and WebSocket hub (subscriber)
type EventBus = events.EventBus

// EventBusOption is a function that configures an EventBus
type EventBusOption = events.EventBusOption

// EventBusMetrics contains aggregate metrics for the EventBus
type EventBusMetrics = events.EventBusMetrics

// SubscriberMetrics contains metrics for a single subscriber
type SubscriberMetrics = events.SubscriberMetrics

// CircuitState represents the state of a subscriber's circuit breaker
type CircuitState = events.CircuitState

// Circuit breaker state constants
const (
	CircuitClosed   = events.CircuitClosed
	CircuitOpen     = events.CircuitOpen
	CircuitHalfOpen = events.CircuitHalfOpen
)

// Backpressure configuration constants
const (
	DefaultBufferSize           = events.DefaultBufferSize
	CircuitBreakerThreshold     = events.CircuitBreakerThreshold
	CircuitBreakerResetDuration = events.CircuitBreakerResetDuration
)

// WithBufferSize sets the buffer size for subscriber channels
func WithBufferSize(size int) EventBusOption {
	return events.WithBufferSize(size)
}

// NewEventBus creates a new EventBus instance
func NewEventBus(opts ...EventBusOption) *EventBus {
	return events.NewEventBus(opts...)
}

// Note: SetCircuitBreakerState is available directly on EventBus for testing.
// Since EventBus is a type alias, all methods from events.EventBus are available.
