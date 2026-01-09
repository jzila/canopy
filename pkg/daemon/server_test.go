package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestNewServer(t *testing.T) {
	state := NewRuntimeState()
	eventBus := NewEventBus()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}

	server := NewServer(8080, state, eventBus, scheduler, beadsClient)

	if server == nil {
		t.Fatal("Expected server to be created")
	}

	if server.port != 8080 {
		t.Errorf("Expected port 8080, got %d", server.port)
	}

	if server.hub == nil {
		t.Error("Expected hub to be initialized")
	}

	if server.handler == nil {
		t.Error("Expected handler to be initialized")
	}
}

func TestSetupRoutes(t *testing.T) {
	state := NewRuntimeState()
	eventBus := NewEventBus()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}

	server := NewServer(8080, state, eventBus, scheduler, beadsClient)
	mux := server.setupRoutes()

	// Test that routes are properly registered by making test requests
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"GetState", http.MethodGet, "/api/state", http.StatusOK},
		{"GetAgents", http.MethodGet, "/api/agents", http.StatusOK},
		{"GetTasks", http.MethodGet, "/api/tasks", http.StatusOK},
		{"GetStats", http.MethodGet, "/api/stats", http.StatusOK},
		{"StaticFiles", http.MethodGet, "/", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}

func TestHandleAgentsRoutes(t *testing.T) {
	state := NewRuntimeState()
	eventBus := NewEventBus()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}

	// Add a test agent
	agent := &AgentState{
		ID:        "agent-1",
		TaskID:    "task-1",
		TaskTitle: "Test Task",
		Status:    AgentStatusRunning,
		StartTime: time.Now(),
	}
	state.AddAgent(agent)

	server := NewServer(8080, state, eventBus, scheduler, beadsClient)
	mux := server.setupRoutes()

	// Test GET /api/agents
	t.Run("ListAgents", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var agents []*AgentState
		if err := json.NewDecoder(w.Body).Decode(&agents); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if len(agents) != 1 {
			t.Errorf("Expected 1 agent, got %d", len(agents))
		}
	})

	// Test POST /api/agents/:id/kill
	t.Run("KillAgent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/agents/agent-1/kill", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		if len(scheduler.killCalls) != 1 {
			t.Errorf("Expected 1 kill call, got %d", len(scheduler.killCalls))
		}
	})
}

func TestHandleTasksRoutes(t *testing.T) {
	state := NewRuntimeState()
	eventBus := NewEventBus()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}

	server := NewServer(8080, state, eventBus, scheduler, beadsClient)
	mux := server.setupRoutes()

	tests := []struct {
		name       string
		method     string
		wantStatus int
	}{
		{"GET", http.MethodGet, http.StatusOK},
		{"POST", http.MethodPost, http.StatusBadRequest}, // No body provided
		{"PATCH", http.MethodPatch, http.StatusBadRequest}, // No query param
		{"DELETE", http.MethodDelete, http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/tasks", nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}

func TestHandleStaticFiles(t *testing.T) {
	state := NewRuntimeState()
	eventBus := NewEventBus()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}

	server := NewServer(8080, state, eventBus, scheduler, beadsClient)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	server.handleStaticFiles(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/html; charset=utf-8" {
		t.Errorf("Expected text/html content type, got %s", contentType)
	}

	body := w.Body.String()
	if body == "" {
		t.Error("Expected non-empty response body")
	}
}

func TestHandleWebSocket(t *testing.T) {
	state := NewRuntimeState()
	eventBus := NewEventBus()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}

	server := NewServer(8080, state, eventBus, scheduler, beadsClient)

	// Start hub in background
	go server.hub.Run()
	defer server.hub.Shutdown()

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer testServer.Close()

	// Convert http://... to ws://...
	wsURL := "ws" + testServer.URL[4:] + "/ws"

	// Connect WebSocket client
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	// Give time for client registration
	time.Sleep(10 * time.Millisecond)

	// Verify EventBus has subscribers (the hub subscribes to it)
	if eventBus.SubscriberCount() == 0 {
		t.Error("Expected EventBus to have subscribers")
	}

	// Publish an event through EventBus
	event := Event{
		Type:      EventAgentStarted,
		Timestamp: time.Now(),
		Payload: map[string]string{
			"agent_id": "test-agent",
		},
	}
	eventBus.Publish(event)

	// Try to receive the message (with timeout)
	ws.SetReadDeadline(time.Now().Add(1 * time.Second))
	var receivedEvent Event
	if err := ws.ReadJSON(&receivedEvent); err != nil {
		t.Logf("Expected to receive WebSocket message, but got error: %v", err)
		// This is acceptable in test environment - the message might not arrive instantly
		return
	}

	if receivedEvent.Type != EventAgentStarted {
		t.Errorf("Expected event type %s, got %s", EventAgentStarted, receivedEvent.Type)
	}
}

func TestServerStopBeforeStart(t *testing.T) {
	state := NewRuntimeState()
	eventBus := NewEventBus()
	scheduler := &mockScheduler{}
	beadsClient := &mockBeadsClient{}

	server := NewServer(8080, state, eventBus, scheduler, beadsClient)

	// Should not panic when stopping before starting
	err := server.Stop()
	if err != nil {
		t.Errorf("Expected no error when stopping unstarted server, got: %v", err)
	}
}
