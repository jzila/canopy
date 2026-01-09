package daemon

import (
	"sync"
	"testing"
	"time"
)

func TestEventBus_SubscribeAndPublish(t *testing.T) {
	bus := NewEventBus()

	// Track received events
	var received []Event
	var mu sync.Mutex

	// Subscribe handler
	unsubscribe := bus.Subscribe(func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, e)
	})
	defer unsubscribe()

	// Publish event
	event := Event{
		Type:      EventAgentStarted,
		Timestamp: time.Now(),
		Payload:   map[string]string{"agent_id": "agent-1"},
	}
	bus.Publish(event)

	// Verify event received
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].Type != EventAgentStarted {
		t.Errorf("expected type %s, got %s", EventAgentStarted, received[0].Type)
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := NewEventBus()

	// Create multiple subscribers
	var count1, count2, count3 int
	var mu sync.Mutex

	unsub1 := bus.Subscribe(func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		count1++
	})
	defer unsub1()

	unsub2 := bus.Subscribe(func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		count2++
	})
	defer unsub2()

	unsub3 := bus.Subscribe(func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		count3++
	})
	defer unsub3()

	// Publish event
	event := Event{Type: EventAgentOutput, Timestamp: time.Now()}
	bus.Publish(event)

	// Verify all subscribers received event
	mu.Lock()
	defer mu.Unlock()
	if count1 != 1 || count2 != 1 || count3 != 1 {
		t.Errorf("expected all counts to be 1, got %d, %d, %d", count1, count2, count3)
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus()

	var count int
	var mu sync.Mutex

	// Subscribe and immediately unsubscribe
	unsubscribe := bus.Subscribe(func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		count++
	})
	unsubscribe()

	// Publish event
	event := Event{Type: EventAgentCompleted, Timestamp: time.Now()}
	bus.Publish(event)

	// Verify handler not called
	mu.Lock()
	defer mu.Unlock()
	if count != 0 {
		t.Errorf("expected count 0 after unsubscribe, got %d", count)
	}
}

func TestEventBus_SubscriberCount(t *testing.T) {
	bus := NewEventBus()

	if bus.SubscriberCount() != 0 {
		t.Errorf("expected 0 subscribers initially, got %d", bus.SubscriberCount())
	}

	unsub1 := bus.Subscribe(func(e Event) {})
	if bus.SubscriberCount() != 1 {
		t.Errorf("expected 1 subscriber, got %d", bus.SubscriberCount())
	}

	unsub2 := bus.Subscribe(func(e Event) {})
	if bus.SubscriberCount() != 2 {
		t.Errorf("expected 2 subscribers, got %d", bus.SubscriberCount())
	}

	unsub1()
	if bus.SubscriberCount() != 1 {
		t.Errorf("expected 1 subscriber after unsub1, got %d", bus.SubscriberCount())
	}

	unsub2()
	if bus.SubscriberCount() != 0 {
		t.Errorf("expected 0 subscribers after unsub2, got %d", bus.SubscriberCount())
	}
}

func TestEventBus_ConcurrentPublish(t *testing.T) {
	bus := NewEventBus()

	var count int
	var mu sync.Mutex

	// Subscribe handler
	bus.Subscribe(func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		count++
	})

	// Publish concurrently
	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			event := Event{Type: EventStatsUpdated, Timestamp: time.Now()}
			bus.Publish(event)
		}()
	}

	wg.Wait()

	// Verify all events received
	mu.Lock()
	defer mu.Unlock()
	if count != numGoroutines {
		t.Errorf("expected %d events, got %d", numGoroutines, count)
	}
}

func TestEventBus_ConcurrentSubscribe(t *testing.T) {
	bus := NewEventBus()

	const numSubscribers = 10
	var wg sync.WaitGroup
	wg.Add(numSubscribers)

	// Subscribe concurrently
	unsubscribers := make([]func(), numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		i := i
		go func() {
			defer wg.Done()
			unsubscribers[i] = bus.Subscribe(func(e Event) {})
		}()
	}

	wg.Wait()

	// Verify subscriber count
	if bus.SubscriberCount() != numSubscribers {
		t.Errorf("expected %d subscribers, got %d", numSubscribers, bus.SubscriberCount())
	}

	// Cleanup
	for _, unsub := range unsubscribers {
		unsub()
	}
}

func TestEventBus_PanicInHandler(t *testing.T) {
	bus := NewEventBus()

	var goodHandlerCalled bool
	var mu sync.Mutex

	// Handler that panics
	bus.Subscribe(func(e Event) {
		panic("handler panic")
	})

	// Handler that should still execute
	bus.Subscribe(func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		goodHandlerCalled = true
	})

	// Publish event
	event := Event{Type: EventTaskUpdated, Timestamp: time.Now()}
	bus.Publish(event)

	// Verify good handler was called despite panic
	mu.Lock()
	defer mu.Unlock()
	if !goodHandlerCalled {
		t.Error("expected good handler to be called after panicking handler")
	}
}

func TestEventBus_MultipleEvents(t *testing.T) {
	bus := NewEventBus()

	var events []EventType
	var mu sync.Mutex

	bus.Subscribe(func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e.Type)
	})

	// Publish multiple event types
	eventTypes := []EventType{
		EventAgentStarted,
		EventAgentOutput,
		EventAgentCompleted,
		EventTaskUpdated,
		EventOrchPaused,
		EventStatsUpdated,
	}

	for _, et := range eventTypes {
		bus.Publish(Event{Type: et, Timestamp: time.Now()})
	}

	// Verify all events received in order
	mu.Lock()
	defer mu.Unlock()
	if len(events) != len(eventTypes) {
		t.Fatalf("expected %d events, got %d", len(eventTypes), len(events))
	}
	for i, et := range eventTypes {
		if events[i] != et {
			t.Errorf("event %d: expected type %s, got %s", i, et, events[i])
		}
	}
}
