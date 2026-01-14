// Package events provides a pub/sub event system for inter-component communication.
// This package exists to break the circular dependency between pkg/daemon and pkg/ipc.
package events

import (
	"sync"
	"time"
)

// Default configuration values
const (
	DefaultBufferSize           = 100
	CircuitBreakerThreshold     = 10
	CircuitBreakerResetDuration = 30 * time.Second
)

// CircuitState represents the state of a subscriber's circuit breaker
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Blocking delivery due to backpressure
	CircuitHalfOpen                     // Testing if subscriber recovered
)

// EventType represents the type of event being sent over WebSocket
type EventType string

// Event type constants
const (
	EventStateSync        EventType = "state:sync"
	EventRunStarted       EventType = "run:started"
	EventRunCompleted     EventType = "run:completed"
	EventAgentStarted     EventType = "agent:started"
	EventAgentOutput      EventType = "agent:output"
	EventAgentLiveFeed    EventType = "agent:live_feed"
	EventAgentCommit      EventType = "agent:commit"
	EventAgentMergeStatus EventType = "agent:merge_status"
	EventAgentCompleted   EventType = "agent:completed"
	EventTaskUpdated      EventType = "task:updated"
	EventOrchPaused       EventType = "orch:paused"
	EventOrchResumed      EventType = "orch:resumed"
	EventStatsUpdated     EventType = "stats:updated"
)

// Event represents a WebSocket event sent to browser clients
type Event struct {
	Type      EventType   `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

// EventHandler is a function that receives events
type EventHandler func(Event)

// subscription represents a single event subscription with backpressure handling
type subscription struct {
	id      int
	handler EventHandler
	ch      chan Event
	stopCh  chan struct{}

	// Backpressure metrics (protected by mu)
	mu             sync.Mutex
	state          CircuitState
	totalEvents    int64
	droppedEvents  int64
	consecutiveDrops int
	lastDropTime   time.Time
	bufferCapacity int
}

// EventBusMetrics contains aggregate metrics for the EventBus
type EventBusMetrics struct {
	SubscriberCount    int
	TotalDroppedEvents int64
	CircuitBreaks      int
}

// SubscriberMetrics contains metrics for a single subscriber
type SubscriberMetrics struct {
	ID             int
	TotalEvents    int64
	DroppedEvents  int64
	BufferCapacity int
	CircuitState   CircuitState
}

// EventBusOption is a function that configures an EventBus
type EventBusOption func(*EventBus)

// WithBufferSize sets the buffer size for subscriber channels
func WithBufferSize(size int) EventBusOption {
	return func(eb *EventBus) {
		eb.bufferSize = size
	}
}

// EventBus manages event subscriptions and distribution
// It provides a thread-safe pub/sub mechanism for internal communication
// between the IPC server (publisher) and WebSocket hub (subscriber)
type EventBus struct {
	mu            sync.RWMutex
	subscriptions map[int]*subscription
	nextID        int
	bufferSize    int
}

// NewEventBus creates a new EventBus instance
func NewEventBus(opts ...EventBusOption) *EventBus {
	eb := &EventBus{
		subscriptions: make(map[int]*subscription),
		nextID:        1,
		bufferSize:    DefaultBufferSize,
	}

	for _, opt := range opts {
		opt(eb)
	}

	return eb
}

// Subscribe registers a handler to receive events
// Returns an unsubscribe function that removes the subscription
func (eb *EventBus) Subscribe(handler EventHandler) func() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	// Assign unique ID and create subscription
	id := eb.nextID
	eb.nextID++

	sub := &subscription{
		id:             id,
		handler:        handler,
		ch:             make(chan Event, eb.bufferSize),
		stopCh:         make(chan struct{}),
		state:          CircuitClosed,
		bufferCapacity: eb.bufferSize,
	}

	eb.subscriptions[id] = sub

	// Start goroutine to deliver events to this subscriber
	go sub.deliverLoop()

	// Return unsubscribe closure
	return func() {
		eb.mu.Lock()
		defer eb.mu.Unlock()

		if sub, exists := eb.subscriptions[id]; exists {
			close(sub.stopCh)
			delete(eb.subscriptions, id)
		}
	}
}

// deliverLoop processes events for a single subscriber
func (s *subscription) deliverLoop() {
	for {
		select {
		case <-s.stopCh:
			return
		case event := <-s.ch:
			// Protect against panicking handlers
			func() {
				defer func() {
					if r := recover(); r != nil {
						// Handler panicked - log but continue
					}
				}()
				s.handler(event)
			}()
		}
	}
}

// Publish sends an event to all subscribers
// Uses non-blocking sends with backpressure handling
func (eb *EventBus) Publish(event Event) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	for _, sub := range eb.subscriptions {
		sub.mu.Lock()
		sub.totalEvents++

		// Check circuit breaker state
		switch sub.state {
		case CircuitOpen:
			// Check if enough time has passed to try recovery
			if time.Since(sub.lastDropTime) > CircuitBreakerResetDuration {
				sub.state = CircuitHalfOpen
				sub.consecutiveDrops = 0
			} else {
				sub.droppedEvents++
				sub.mu.Unlock()
				continue
			}
		case CircuitHalfOpen:
			// Let one event through to test recovery
		}

		// Non-blocking send
		select {
		case sub.ch <- event:
			// Successfully delivered
			if sub.state == CircuitHalfOpen {
				sub.state = CircuitClosed
			}
			sub.consecutiveDrops = 0
		default:
			// Buffer full - drop event
			sub.droppedEvents++
			sub.consecutiveDrops++
			sub.lastDropTime = time.Now()

			// Check if circuit breaker should open
			if sub.consecutiveDrops >= CircuitBreakerThreshold {
				sub.state = CircuitOpen
			}
		}
		sub.mu.Unlock()
	}
}

// SubscriberCount returns the number of active subscriptions
// Useful for testing and debugging
func (eb *EventBus) SubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.subscriptions)
}

// GetMetrics returns aggregate metrics for the EventBus
func (eb *EventBus) GetMetrics() EventBusMetrics {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	metrics := EventBusMetrics{
		SubscriberCount: len(eb.subscriptions),
	}

	for _, sub := range eb.subscriptions {
		sub.mu.Lock()
		metrics.TotalDroppedEvents += sub.droppedEvents
		if sub.state == CircuitOpen {
			metrics.CircuitBreaks++
		}
		sub.mu.Unlock()
	}

	return metrics
}

// GetSubscriberMetrics returns metrics for all subscribers
func (eb *EventBus) GetSubscriberMetrics() []SubscriberMetrics {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	metrics := make([]SubscriberMetrics, 0, len(eb.subscriptions))
	for _, sub := range eb.subscriptions {
		sub.mu.Lock()
		metrics = append(metrics, SubscriberMetrics{
			ID:             sub.id,
			TotalEvents:    sub.totalEvents,
			DroppedEvents:  sub.droppedEvents,
			BufferCapacity: sub.bufferCapacity,
			CircuitState:   sub.state,
		})
		sub.mu.Unlock()
	}

	return metrics
}

// ResetCircuitBreaker resets the circuit breaker for a specific subscriber
// Returns false if the subscriber ID doesn't exist
func (eb *EventBus) ResetCircuitBreaker(subscriberID int) bool {
	eb.mu.RLock()
	sub, exists := eb.subscriptions[subscriberID]
	eb.mu.RUnlock()

	if !exists {
		return false
	}

	sub.mu.Lock()
	sub.state = CircuitClosed
	sub.consecutiveDrops = 0
	sub.mu.Unlock()

	return true
}

// SetCircuitBreakerState is a testing helper that sets the circuit breaker state for a subscriber.
// This allows tests in other packages to manipulate internal state for testing.
// The lastDropOffset parameter sets lastDropTime relative to now (negative values = past).
// Returns false if the subscriber ID doesn't exist.
func (eb *EventBus) SetCircuitBreakerState(subscriberID int, state CircuitState, lastDropOffset time.Duration) bool {
	eb.mu.RLock()
	sub, exists := eb.subscriptions[subscriberID]
	eb.mu.RUnlock()

	if !exists {
		return false
	}

	sub.mu.Lock()
	sub.state = state
	sub.lastDropTime = time.Now().Add(lastDropOffset)
	if state == CircuitClosed {
		sub.consecutiveDrops = 0
	}
	sub.mu.Unlock()

	return true
}
