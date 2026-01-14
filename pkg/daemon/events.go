package daemon

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// EventHandler is a function that receives events
type EventHandler func(Event)

// Default configuration for subscriber backpressure
const (
	// DefaultSubscriberBufferSize is the buffer size for each subscriber's event channel
	DefaultSubscriberBufferSize = 256

	// CircuitBreakerThreshold is number of consecutive drops before circuit opens
	CircuitBreakerThreshold = 10

	// CircuitBreakerResetDuration is how long circuit stays open before half-open
	CircuitBreakerResetDuration = 5 * time.Second
)

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Rejecting events
	CircuitHalfOpen                     // Testing if subscriber recovered
)

// subscription represents a single event subscription with backpressure handling
type subscription struct {
	id      int
	handler EventHandler
	events  chan Event
	done    chan struct{}

	// Circuit breaker state
	state            CircuitState
	consecutiveDrops int
	lastDropTime     time.Time
	mu               sync.Mutex

	// Metrics
	droppedEvents int64
	totalEvents   int64
}

// EventBus manages event subscriptions and distribution
// It provides a thread-safe pub/sub mechanism for internal communication
// between the IPC server (publisher) and WebSocket hub (subscriber)
//
// Backpressure handling:
// - Each subscriber has its own buffered channel
// - Events are dropped for slow subscribers (non-blocking publish)
// - Circuit breaker disconnects persistently slow subscribers
type EventBus struct {
	mu            sync.RWMutex
	subscriptions map[int]*subscription
	nextID        int
	bufferSize    int

	// Global metrics
	totalDroppedEvents int64
	circuitBreaks      int64
}

// EventBusOption configures the EventBus
type EventBusOption func(*EventBus)

// WithBufferSize sets the per-subscriber buffer size
func WithBufferSize(size int) EventBusOption {
	return func(eb *EventBus) {
		eb.bufferSize = size
	}
}

// NewEventBus creates a new EventBus instance
func NewEventBus(opts ...EventBusOption) *EventBus {
	eb := &EventBus{
		subscriptions: make(map[int]*subscription),
		nextID:        1,
		bufferSize:    DefaultSubscriberBufferSize,
	}
	for _, opt := range opts {
		opt(eb)
	}
	return eb
}

// Subscribe registers a handler to receive events
// Returns an unsubscribe function that removes the subscription
//
// Each subscriber gets its own buffered channel. Events are delivered
// asynchronously via a dedicated goroutine per subscriber. If a subscriber's
// buffer fills up, events are dropped for that subscriber only.
func (eb *EventBus) Subscribe(handler EventHandler) func() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	// Assign unique ID and register subscription
	id := eb.nextID
	eb.nextID++

	sub := &subscription{
		id:      id,
		handler: handler,
		events:  make(chan Event, eb.bufferSize),
		done:    make(chan struct{}),
		state:   CircuitClosed,
	}
	eb.subscriptions[id] = sub

	// Start subscriber goroutine
	go sub.run()

	// Return unsubscribe closure
	return func() {
		eb.mu.Lock()
		sub, exists := eb.subscriptions[id]
		if exists {
			delete(eb.subscriptions, id)
		}
		eb.mu.Unlock()

		if exists {
			close(sub.done)
		}
	}
}

// run is the subscriber's event processing loop
func (s *subscription) run() {
	for {
		select {
		case <-s.done:
			return
		case event := <-s.events:
			s.deliverEvent(event)
		}
	}
}

// deliverEvent safely delivers an event to the handler
func (s *subscription) deliverEvent(event Event) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("EventBus: handler panic for subscriber %d: %v", s.id, r)
		}
	}()
	s.handler(event)
}

// Publish sends an event to all subscribers
// Events are delivered asynchronously via per-subscriber channels.
// If a subscriber's buffer is full, the event is dropped for that subscriber.
// This ensures slow subscribers don't block the publisher or other subscribers.
func (eb *EventBus) Publish(event Event) {
	eb.mu.RLock()
	// Copy subscriptions to avoid holding read lock during send
	subs := make([]*subscription, 0, len(eb.subscriptions))
	for _, sub := range eb.subscriptions {
		subs = append(subs, sub)
	}
	eb.mu.RUnlock()

	// Send to each subscriber's channel (non-blocking)
	for _, sub := range subs {
		sub.tryDeliver(event, eb)
	}
}

// tryDeliver attempts to deliver an event to a subscriber with backpressure handling
func (s *subscription) tryDeliver(event Event, eb *EventBus) {
	atomic.AddInt64(&s.totalEvents, 1)

	// Check circuit breaker state
	s.mu.Lock()
	state := s.state
	if state == CircuitOpen {
		// Check if we should transition to half-open
		if time.Since(s.lastDropTime) > CircuitBreakerResetDuration {
			s.state = CircuitHalfOpen
			state = CircuitHalfOpen
		}
	}
	s.mu.Unlock()

	// If circuit is open, drop the event
	if state == CircuitOpen {
		atomic.AddInt64(&s.droppedEvents, 1)
		atomic.AddInt64(&eb.totalDroppedEvents, 1)
		return
	}

	// Try non-blocking send
	select {
	case s.events <- event:
		// Success - reset circuit breaker if in half-open state
		s.mu.Lock()
		if s.state == CircuitHalfOpen {
			s.state = CircuitClosed
			s.consecutiveDrops = 0
		}
		s.mu.Unlock()
	default:
		// Buffer full - drop event and update circuit breaker
		atomic.AddInt64(&s.droppedEvents, 1)
		atomic.AddInt64(&eb.totalDroppedEvents, 1)

		s.mu.Lock()
		s.consecutiveDrops++
		s.lastDropTime = time.Now()

		if s.consecutiveDrops >= CircuitBreakerThreshold {
			if s.state != CircuitOpen {
				s.state = CircuitOpen
				atomic.AddInt64(&eb.circuitBreaks, 1)
				log.Printf("EventBus: circuit breaker opened for subscriber %d (dropped %d events)",
					s.id, s.droppedEvents)
			}
		}
		s.mu.Unlock()
	}
}

// SubscriberCount returns the number of active subscriptions
// Useful for testing and debugging
func (eb *EventBus) SubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.subscriptions)
}

// EventBusMetrics contains backpressure metrics for the EventBus
type EventBusMetrics struct {
	TotalDroppedEvents int64 // Total events dropped across all subscribers
	CircuitBreaks      int64 // Number of times circuit breakers opened
	SubscriberCount    int   // Current number of subscribers
}

// GetMetrics returns the current backpressure metrics
func (eb *EventBus) GetMetrics() EventBusMetrics {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return EventBusMetrics{
		TotalDroppedEvents: atomic.LoadInt64(&eb.totalDroppedEvents),
		CircuitBreaks:      atomic.LoadInt64(&eb.circuitBreaks),
		SubscriberCount:    len(eb.subscriptions),
	}
}

// SubscriberMetrics contains per-subscriber metrics
type SubscriberMetrics struct {
	ID             int
	DroppedEvents  int64
	TotalEvents    int64
	CircuitState   CircuitState
	BufferCapacity int
	BufferUsed     int
}

// GetSubscriberMetrics returns metrics for all subscribers
func (eb *EventBus) GetSubscriberMetrics() []SubscriberMetrics {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	metrics := make([]SubscriberMetrics, 0, len(eb.subscriptions))
	for _, sub := range eb.subscriptions {
		sub.mu.Lock()
		state := sub.state
		sub.mu.Unlock()

		metrics = append(metrics, SubscriberMetrics{
			ID:             sub.id,
			DroppedEvents:  atomic.LoadInt64(&sub.droppedEvents),
			TotalEvents:    atomic.LoadInt64(&sub.totalEvents),
			CircuitState:   state,
			BufferCapacity: cap(sub.events),
			BufferUsed:     len(sub.events),
		})
	}
	return metrics
}

// ResetCircuitBreaker manually resets a subscriber's circuit breaker
// Useful for recovery after fixing slow subscriber issues
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
