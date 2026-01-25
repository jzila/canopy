package events

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEventType_IsCritical(t *testing.T) {
	tests := []struct {
		eventType EventType
		critical  bool
	}{
		// Critical events - must never be dropped
		{EventRunStarted, true},
		{EventRunCompleted, true},
		{EventAgentStarted, true},
		{EventAgentCompleted, true},
		{EventAgentMergeStatus, true},
		{EventOrchPaused, true},
		{EventOrchResumed, true},
		{EventTaskUpdated, true},

		// Non-critical events - can be dropped under backpressure
		{EventStateSync, false},
		{EventAgentOutput, false},
		{EventAgentLiveFeed, false},
		{EventAgentCommit, false},
		{EventStatsUpdated, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			got := tt.eventType.IsCritical()
			if got != tt.critical {
				t.Errorf("EventType(%q).IsCritical() = %v, want %v", tt.eventType, got, tt.critical)
			}
		})
	}
}

func TestEventBus_ConcurrentPublish(t *testing.T) {
	eb := NewEventBus()

	var received atomic.Int64
	var wg sync.WaitGroup

	// Subscribe a handler
	eb.Subscribe(func(event Event) {
		received.Add(1)
	})

	// Publish many events concurrently
	numGoroutines := 10
	eventsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				eb.Publish(Event{
					Type:      EventAgentStarted,
					Timestamp: time.Now(),
					Payload:   map[string]string{"agent_id": "test"},
				})
			}
		}()
	}

	wg.Wait()

	expected := int64(numGoroutines * eventsPerGoroutine)
	if got := received.Load(); got != expected {
		t.Errorf("Expected %d events, received %d", expected, got)
	}
}

func TestEventBus_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	eb := NewEventBus()

	var wg sync.WaitGroup

	// Spawn goroutines that subscribe, publish, and unsubscribe
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				unsub := eb.Subscribe(func(event Event) {})
				eb.Publish(Event{Type: EventAgentStarted, Timestamp: time.Now()})
				unsub()
			}
		}()
	}

	wg.Wait()

	// All subscriptions should be cleaned up
	if count := eb.SubscriberCount(); count != 0 {
		t.Errorf("Expected 0 subscribers after cleanup, got %d", count)
	}
}

func TestEventBus_NestedPublish_DeliveryOrder(t *testing.T) {
	// Test that nested events are delivered in sequence order, not execution order.
	// This is the core fix for the out-of-order event delivery bug.
	eb := NewEventBus()

	var deliveryOrder []EventType
	var mu sync.Mutex

	// First subscriber: publishes nested events when it sees EventAgentStarted
	eb.Subscribe(func(event Event) {
		if event.Type == EventAgentStarted {
			// Simulate what RuntimeState does: lifecycle transition publishes nested events
			eb.Publish(Event{Type: EventLifecycleStateChanged, Timestamp: time.Now()})
			eb.Publish(Event{Type: EventAgentRunning, Timestamp: time.Now()})
		}
	})

	// Second subscriber: records delivery order (simulates WebSocket Hub)
	eb.Subscribe(func(event Event) {
		mu.Lock()
		deliveryOrder = append(deliveryOrder, event.Type)
		mu.Unlock()
	})

	// Publish the outer event
	eb.Publish(Event{Type: EventAgentStarted, Timestamp: time.Now()})

	// Verify delivery order: EventAgentStarted should be delivered to ALL subscribers
	// BEFORE nested events are delivered to ANY subscribers
	expected := []EventType{
		EventAgentStarted,          // Outer event delivered first
		EventLifecycleStateChanged, // Then first nested event
		EventAgentRunning,          // Then second nested event
	}

	if len(deliveryOrder) != len(expected) {
		t.Fatalf("Expected %d events, got %d: %v", len(expected), len(deliveryOrder), deliveryOrder)
	}

	for i, evt := range expected {
		if deliveryOrder[i] != evt {
			t.Errorf("Event %d: expected %s, got %s. Full order: %v", i, evt, deliveryOrder[i], deliveryOrder)
		}
	}
}

func TestEventBus_NestedPublish_SequenceNumbers(t *testing.T) {
	// Test that sequence numbers are assigned in publish order, not delivery order
	eb := NewEventBus()

	var sequences []uint64
	var mu sync.Mutex

	// First subscriber publishes nested event
	eb.Subscribe(func(event Event) {
		if event.Type == EventAgentStarted {
			eb.Publish(Event{Type: EventAgentRunning, Timestamp: time.Now()})
		}
	})

	// Second subscriber records sequence numbers
	eb.Subscribe(func(event Event) {
		mu.Lock()
		sequences = append(sequences, event.Sequence)
		mu.Unlock()
	})

	eb.Publish(Event{Type: EventAgentStarted, Timestamp: time.Now()})

	// Sequences should be monotonically increasing
	if len(sequences) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(sequences))
	}

	if sequences[0] >= sequences[1] {
		t.Errorf("Sequences not increasing: %v", sequences)
	}
}

func TestEventBus_NestedPublish_DeeplyNested(t *testing.T) {
	// Test that deeply nested publishing still delivers in correct order
	eb := NewEventBus()

	var deliveryOrder []string
	var mu sync.Mutex

	// Subscriber that creates a chain: A -> B -> C
	eb.Subscribe(func(event Event) {
		name := event.Payload.(string)
		if name == "A" {
			eb.Publish(Event{Type: EventAgentOutput, Timestamp: time.Now(), Payload: "B"})
		} else if name == "B" {
			eb.Publish(Event{Type: EventAgentOutput, Timestamp: time.Now(), Payload: "C"})
		}
	})

	// Record delivery order
	eb.Subscribe(func(event Event) {
		mu.Lock()
		deliveryOrder = append(deliveryOrder, event.Payload.(string))
		mu.Unlock()
	})

	eb.Publish(Event{Type: EventAgentOutput, Timestamp: time.Now(), Payload: "A"})

	expected := []string{"A", "B", "C"}
	if len(deliveryOrder) != len(expected) {
		t.Fatalf("Expected %d events, got %d: %v", len(expected), len(deliveryOrder), deliveryOrder)
	}

	for i, name := range expected {
		if deliveryOrder[i] != name {
			t.Errorf("Event %d: expected %s, got %s", i, name, deliveryOrder[i])
		}
	}
}

func TestEventBus_NestedPublish_ConcurrentOuterPublish(t *testing.T) {
	// Test concurrent outer Publish calls with nested publishing
	eb := NewEventBus()

	var received atomic.Int64

	// Subscriber that publishes a nested event
	eb.Subscribe(func(event Event) {
		if event.Type == EventAgentStarted {
			eb.Publish(Event{Type: EventAgentRunning, Timestamp: time.Now()})
		}
		received.Add(1)
	})

	var wg sync.WaitGroup
	numGoroutines := 10
	eventsPerGoroutine := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				eb.Publish(Event{Type: EventAgentStarted, Timestamp: time.Now()})
			}
		}()
	}

	wg.Wait()

	// Each EventAgentStarted generates one EventAgentRunning
	// So total events = 2 * numGoroutines * eventsPerGoroutine
	expected := int64(2 * numGoroutines * eventsPerGoroutine)
	if got := received.Load(); got != expected {
		t.Errorf("Expected %d events, got %d", expected, got)
	}
}
