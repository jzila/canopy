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
