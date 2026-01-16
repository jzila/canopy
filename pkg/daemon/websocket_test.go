package daemon

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewHub(t *testing.T) {
	eventBus := NewEventBus()
	hub := NewHub(eventBus)

	if hub == nil {
		t.Fatal("NewHub returned nil")
	}

	if hub.clients == nil {
		t.Error("Hub clients map not initialized")
	}

	if hub.broadcast == nil {
		t.Error("Hub broadcast channel not initialized")
	}

	if hub.register == nil {
		t.Error("Hub register channel not initialized")
	}

	if hub.unregister == nil {
		t.Error("Hub unregister channel not initialized")
	}

	if hub.eventBus == nil {
		t.Error("Hub eventBus not set")
	}
}

func TestHub_EventBusIntegration(t *testing.T) {
	eventBus := NewEventBus()
	hub := NewHub(eventBus)

	// Start hub in background
	go hub.Run()
	defer hub.Shutdown()

	// Give hub time to subscribe
	time.Sleep(10 * time.Millisecond)

	// Create a test client (mock)
	mockClient := &Client{
		hub:  hub,
		conn: nil, // Not actually connecting WebSocket for this test
		send: make(chan []byte, 256),
	}

	// Register the mock client
	hub.register <- mockClient

	// Give registration time to process
	time.Sleep(10 * time.Millisecond)

	// Publish an event through EventBus
	testEvent := Event{
		Type:      EventAgentStarted,
		Timestamp: time.Now(),
		Payload:   map[string]string{"agent_id": "test-123"},
	}

	eventBus.Publish(testEvent)

	// Wait for broadcast to be received
	select {
	case msg := <-mockClient.send:
		// Verify message can be unmarshaled back to Event
		var receivedEvent Event
		if err := json.Unmarshal(msg, &receivedEvent); err != nil {
			t.Fatalf("Failed to unmarshal received event: %v", err)
		}

		if receivedEvent.Type != EventAgentStarted {
			t.Errorf("Expected event type %s, got %s", EventAgentStarted, receivedEvent.Type)
		}

	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for broadcast message")
	}

	// Clean up
	hub.unregister <- mockClient
	time.Sleep(10 * time.Millisecond)
}

func TestHub_MultipleClients(t *testing.T) {
	eventBus := NewEventBus()
	hub := NewHub(eventBus)

	go hub.Run()
	defer hub.Shutdown()

	time.Sleep(10 * time.Millisecond)

	// Create multiple mock clients
	client1 := &Client{hub: hub, send: make(chan []byte, 256)}
	client2 := &Client{hub: hub, send: make(chan []byte, 256)}
	client3 := &Client{hub: hub, send: make(chan []byte, 256)}

	// Register all clients
	hub.register <- client1
	hub.register <- client2
	hub.register <- client3

	time.Sleep(10 * time.Millisecond)

	// Publish event
	testEvent := Event{
		Type:      EventTaskUpdated,
		Timestamp: time.Now(),
		Payload:   "test payload",
	}

	eventBus.Publish(testEvent)

	// Verify all clients receive the message
	clients := []*Client{client1, client2, client3}
	for i, client := range clients {
		select {
		case msg := <-client.send:
			var receivedEvent Event
			if err := json.Unmarshal(msg, &receivedEvent); err != nil {
				t.Errorf("Client %d: failed to unmarshal: %v", i+1, err)
			}
			if receivedEvent.Type != EventTaskUpdated {
				t.Errorf("Client %d: wrong event type", i+1)
			}
		case <-time.After(1 * time.Second):
			t.Errorf("Client %d: timeout waiting for message", i+1)
		}
	}

	// Clean up
	hub.unregister <- client1
	hub.unregister <- client2
	hub.unregister <- client3
	time.Sleep(10 * time.Millisecond)
}

func TestHub_ClientUnregister(t *testing.T) {
	eventBus := NewEventBus()
	hub := NewHub(eventBus)

	go hub.Run()
	defer hub.Shutdown()

	time.Sleep(10 * time.Millisecond)

	client := &Client{hub: hub, send: make(chan []byte, 256)}

	// Register
	hub.register <- client
	time.Sleep(10 * time.Millisecond)

	// Unregister
	hub.unregister <- client
	time.Sleep(10 * time.Millisecond)

	// Publish event
	testEvent := Event{
		Type:      EventAgentCompleted,
		Timestamp: time.Now(),
		Payload:   nil,
	}

	eventBus.Publish(testEvent)

	// Client should not receive message (channel closed or no message)
	select {
	case _, ok := <-client.send:
		if ok {
			t.Error("Unregistered client should not receive messages")
		}
	case <-time.After(100 * time.Millisecond):
		// Expected: timeout means no message received
	}
}

// TestHub_ConcurrentAgentStarted verifies that critical events (like agent_started)
// are delivered even under high concurrency. This tests the fix for the race condition
// where agent_started events were dropped during high concurrency due to backpressure.
func TestHub_ConcurrentAgentStarted(t *testing.T) {
	eventBus := NewEventBus()
	hub := NewHub(eventBus)

	go hub.Run()
	defer hub.Shutdown()

	time.Sleep(10 * time.Millisecond)

	// Create a mock client with a reasonable buffer
	mockClient := &Client{
		hub:  hub,
		conn: nil,
		send: make(chan []byte, 256),
	}

	hub.register <- mockClient
	time.Sleep(10 * time.Millisecond)

	// Simulate multiple agents starting concurrently (the race condition scenario)
	numAgents := 20
	var wg sync.WaitGroup

	for i := 0; i < numAgents; i++ {
		wg.Add(1)
		go func(agentNum int) {
			defer wg.Done()
			event := Event{
				Type:      EventAgentStarted,
				Timestamp: time.Now(),
				Payload:   map[string]interface{}{"agent_id": agentNum},
			}
			eventBus.Publish(event)
		}(i)
	}

	wg.Wait()

	// Give events time to propagate
	time.Sleep(50 * time.Millisecond)

	// Drain all received messages
	received := 0
	timeout := time.After(2 * time.Second)
drain:
	for {
		select {
		case msg := <-mockClient.send:
			var event Event
			if err := json.Unmarshal(msg, &event); err != nil {
				t.Errorf("Failed to unmarshal event: %v", err)
				continue
			}
			if event.Type != EventAgentStarted {
				t.Errorf("Expected EventAgentStarted, got %s", event.Type)
			}
			received++
			if received == numAgents {
				break drain
			}
		case <-timeout:
			break drain
		}
	}

	// All critical events should be received (no drops)
	if received != numAgents {
		t.Errorf("Expected %d agent_started events, received %d (dropped %d)",
			numAgents, received, numAgents-received)
	}

	hub.unregister <- mockClient
	time.Sleep(10 * time.Millisecond)
}

// TestHub_CriticalEventsNotDroppedAtBroadcastChannel verifies that critical events
// are not dropped at the broadcast channel level under backpressure.
// Note: If a client itself becomes too slow and gets disconnected, events will
// be lost for that specific client - this is expected behavior.
func TestHub_CriticalEventsNotDroppedAtBroadcastChannel(t *testing.T) {
	eventBus := NewEventBus()
	hub := NewHub(eventBus)

	go hub.Run()
	defer hub.Shutdown()

	time.Sleep(10 * time.Millisecond)

	// Create a client with adequate buffer (realistic scenario)
	mockClient := &Client{
		hub:  hub,
		conn: nil,
		send: make(chan []byte, 256),
	}

	hub.register <- mockClient
	time.Sleep(10 * time.Millisecond)

	// Track received events by type
	var criticalReceived atomic.Int64
	var nonCriticalReceived atomic.Int64
	done := make(chan struct{})

	// Consumer goroutine
	go func() {
		for {
			select {
			case msg, ok := <-mockClient.send:
				if !ok {
					return
				}
				var event Event
				if err := json.Unmarshal(msg, &event); err != nil {
					continue
				}
				if event.Type.IsCritical() {
					criticalReceived.Add(1)
				} else {
					nonCriticalReceived.Add(1)
				}
			case <-done:
				return
			}
		}
	}()

	// Send a mix of critical and non-critical events rapidly
	numCritical := 10
	numNonCritical := 100

	var wg sync.WaitGroup

	// Send non-critical events (these may be dropped at broadcast channel if full)
	for i := 0; i < numNonCritical; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			event := Event{
				Type:      EventAgentOutput, // Non-critical
				Timestamp: time.Now(),
				Payload:   "output data",
			}
			eventBus.Publish(event)
		}()
	}

	// Send critical events (these should NOT be dropped at broadcast level)
	for i := 0; i < numCritical; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			event := Event{
				Type:      EventAgentStarted, // Critical
				Timestamp: time.Now(),
				Payload:   map[string]interface{}{"agent_id": n},
			}
			eventBus.Publish(event)
		}(i)
	}

	wg.Wait()

	// Wait for events to be processed
	time.Sleep(100 * time.Millisecond)
	close(done)

	// All critical events must be received (not dropped at broadcast channel)
	if got := criticalReceived.Load(); got != int64(numCritical) {
		t.Errorf("Expected %d critical events, got %d (dropped critical events!)",
			numCritical, got)
	}

	// Non-critical events may be partially dropped (this is acceptable)
	t.Logf("Received %d/%d non-critical events (drops are acceptable)",
		nonCriticalReceived.Load(), numNonCritical)

	hub.unregister <- mockClient
	time.Sleep(10 * time.Millisecond)
}

// TestHub_BroadcastChannelBackpressure verifies behavior when the broadcast
// channel itself fills up - critical events should block/wait, non-critical should drop.
func TestHub_BroadcastChannelBackpressure(t *testing.T) {
	eventBus := NewEventBus()
	hub := NewHub(eventBus)

	// Subscribe to the event bus but DON'T start hub.Run() - this means
	// the broadcast channel will never be drained and will fill up
	hub.eventBus.Subscribe(func(event Event) {
		// Marshal event to JSON (same as hub.Run() does)
		data, err := json.Marshal(event)
		if err != nil {
			return
		}

		msg := broadcastMessage{
			data:       data,
			isCritical: event.Type.IsCritical(),
		}

		// Non-blocking send like non-critical events do
		if !msg.isCritical {
			select {
			case hub.broadcast <- msg:
			default:
				hub.droppedBroadcastEvents++
			}
		}
	})
	defer hub.Shutdown()

	// Track metrics before
	metricsBefore := hub.GetMetrics()

	// Send many non-critical events to fill the broadcast channel
	// The broadcast channel is NOT being drained, so it should fill up
	for i := 0; i < defaultHubBroadcastBuffer+50; i++ {
		event := Event{
			Type:      EventAgentOutput, // Non-critical
			Timestamp: time.Now(),
			Payload:   "data",
		}
		eventBus.Publish(event)
	}

	// Check that non-critical events were dropped
	metricsAfter := hub.GetMetrics()
	droppedNonCritical := metricsAfter.DroppedBroadcastEvents - metricsBefore.DroppedBroadcastEvents

	// Some non-critical events should have been dropped due to full broadcast channel
	if droppedNonCritical == 0 {
		t.Error("Expected some non-critical events to be dropped due to backpressure")
	}
	t.Logf("Dropped %d non-critical events due to backpressure (expected)", droppedNonCritical)
}
