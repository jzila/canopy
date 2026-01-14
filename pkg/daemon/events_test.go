package daemon

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEventBus_SubscribeAndPublish(t *testing.T) {
	bus := NewEventBus()

	// Track received events
	var received []Event
	var mu sync.Mutex
	done := make(chan struct{})

	// Subscribe handler
	unsubscribe := bus.Subscribe(func(e Event) {
		mu.Lock()
		received = append(received, e)
		if len(received) == 1 {
			close(done)
		}
		mu.Unlock()
	})
	defer unsubscribe()

	// Publish event
	event := Event{
		Type:      EventAgentStarted,
		Timestamp: time.Now(),
		Payload:   map[string]string{"agent_id": "agent-1"},
	}
	bus.Publish(event)

	// Wait for async delivery
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

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

	// Create multiple subscribers with counters
	var count1, count2, count3 int32
	var wg sync.WaitGroup
	wg.Add(3)

	unsub1 := bus.Subscribe(func(e Event) {
		if atomic.AddInt32(&count1, 1) == 1 {
			wg.Done()
		}
	})
	defer unsub1()

	unsub2 := bus.Subscribe(func(e Event) {
		if atomic.AddInt32(&count2, 1) == 1 {
			wg.Done()
		}
	})
	defer unsub2()

	unsub3 := bus.Subscribe(func(e Event) {
		if atomic.AddInt32(&count3, 1) == 1 {
			wg.Done()
		}
	})
	defer unsub3()

	// Publish event
	event := Event{Type: EventAgentOutput, Timestamp: time.Now()}
	bus.Publish(event)

	// Wait for async delivery
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for events")
	}

	// Verify all subscribers received event
	if atomic.LoadInt32(&count1) != 1 || atomic.LoadInt32(&count2) != 1 || atomic.LoadInt32(&count3) != 1 {
		t.Errorf("expected all counts to be 1, got %d, %d, %d", count1, count2, count3)
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus()

	var count int32

	// Subscribe and immediately unsubscribe
	unsubscribe := bus.Subscribe(func(e Event) {
		atomic.AddInt32(&count, 1)
	})
	unsubscribe()

	// Give goroutine time to stop
	time.Sleep(10 * time.Millisecond)

	// Publish event
	event := Event{Type: EventAgentCompleted, Timestamp: time.Now()}
	bus.Publish(event)

	// Wait a bit to ensure no delivery
	time.Sleep(50 * time.Millisecond)

	// Verify handler not called
	if atomic.LoadInt32(&count) != 0 {
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

	var count int32
	done := make(chan struct{})

	const numGoroutines = 10

	// Subscribe handler
	bus.Subscribe(func(e Event) {
		if atomic.AddInt32(&count, 1) == numGoroutines {
			close(done)
		}
	})

	// Publish concurrently
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

	// Wait for async delivery
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for events")
	}

	// Verify all events received
	if atomic.LoadInt32(&count) != numGoroutines {
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

	var goodHandlerCalled int32
	done := make(chan struct{})

	// Handler that panics
	bus.Subscribe(func(e Event) {
		panic("handler panic")
	})

	// Handler that should still execute (each subscriber has its own goroutine)
	bus.Subscribe(func(e Event) {
		if atomic.AddInt32(&goodHandlerCalled, 1) == 1 {
			close(done)
		}
	})

	// Publish event
	event := Event{Type: EventTaskUpdated, Timestamp: time.Now()}
	bus.Publish(event)

	// Wait for async delivery
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Verify good handler was called despite panic in other handler
	if atomic.LoadInt32(&goodHandlerCalled) != 1 {
		t.Error("expected good handler to be called after panicking handler")
	}
}

func TestEventBus_MultipleEvents(t *testing.T) {
	bus := NewEventBus()

	var events []EventType
	var mu sync.Mutex
	done := make(chan struct{})

	// Publish multiple event types
	eventTypes := []EventType{
		EventAgentStarted,
		EventAgentOutput,
		EventAgentCompleted,
		EventTaskUpdated,
		EventOrchPaused,
		EventStatsUpdated,
	}

	bus.Subscribe(func(e Event) {
		mu.Lock()
		events = append(events, e.Type)
		if len(events) == len(eventTypes) {
			close(done)
		}
		mu.Unlock()
	})

	for _, et := range eventTypes {
		bus.Publish(Event{Type: et, Timestamp: time.Now()})
	}

	// Wait for async delivery
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for events")
	}

	// Verify all events received in order (order preserved due to single channel)
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
