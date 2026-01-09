package daemon

import (
	"encoding/json"
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
