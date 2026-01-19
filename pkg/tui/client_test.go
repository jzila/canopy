package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jzila/canopy/pkg/daemon"
	"github.com/jzila/canopy/pkg/events"
)

func TestNormalizeAddr(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"8080", "http://localhost:8080"},
		{"localhost:8080", "http://localhost:8080"},
		{"127.0.0.1:9000", "http://127.0.0.1:9000"},
		{"http://example.com:8080", "http://example.com:8080"},
		{"https://example.com:8443", "https://example.com:8443"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := normalizeAddr(tc.input)
			if result != tc.expected {
				t.Errorf("normalizeAddr(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestHttpToWS(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"http://localhost:8080", "ws://localhost:8080/ws"},
		{"https://example.com:8443", "wss://example.com:8443/ws"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := httpToWS(tc.input)
			if result != tc.expected {
				t.Errorf("httpToWS(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestNewRemoteClient(t *testing.T) {
	client := NewRemoteClient("8080")

	if client.baseURL != "http://localhost:8080" {
		t.Errorf("Expected baseURL http://localhost:8080, got %s", client.baseURL)
	}

	if client.wsURL != "ws://localhost:8080/ws" {
		t.Errorf("Expected wsURL ws://localhost:8080/ws, got %s", client.wsURL)
	}

	if client.state == nil {
		t.Error("Expected state to be initialized")
	}

	if client.eventBus == nil {
		t.Error("Expected eventBus to be initialized")
	}

	if client.IsConnected() {
		t.Error("Expected client to not be connected initially")
	}
}

func TestFetchInitialState(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/state" {
			state := &daemon.RuntimeState{
				IsPaused: true,
				Stats: daemon.Stats{
					RunningTasks:   2,
					CompletedTasks: 5,
					TotalCostUSD:   1.23,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(state)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Use the full URL to avoid normalization issues with test server URLs
	client := NewRemoteClient(server.URL)
	err := client.fetchInitialState()
	if err != nil {
		t.Fatalf("fetchInitialState failed: %v", err)
	}

	if !client.state.IsPaused {
		t.Error("Expected state.IsPaused to be true")
	}

	if client.state.Stats.RunningTasks != 2 {
		t.Errorf("Expected RunningTasks=2, got %d", client.state.Stats.RunningTasks)
	}

	if client.state.Stats.CompletedTasks != 5 {
		t.Errorf("Expected CompletedTasks=5, got %d", client.state.Stats.CompletedTasks)
	}
}

func TestParseAgentState(t *testing.T) {
	client := NewRemoteClient("8080")

	data := map[string]interface{}{
		"id":           "agent-123",
		"task_id":      "task-456",
		"task_title":   "Test Task",
		"status":       "running",
		"merge_status": "pending",
		"num_turns":    float64(5),
		"duration":     float64(120.5),
		"commits":      float64(3),
		"start_time":   time.Now().Format(time.RFC3339Nano),
		"token_usage": map[string]interface{}{
			"input_tokens":  float64(1000),
			"output_tokens": float64(500),
			"cost_usd":      float64(0.05),
		},
	}

	agent := client.parseAgentState(data)

	if agent.ID != "agent-123" {
		t.Errorf("Expected ID=agent-123, got %s", agent.ID)
	}
	if agent.TaskID != "task-456" {
		t.Errorf("Expected TaskID=task-456, got %s", agent.TaskID)
	}
	if agent.TaskTitle != "Test Task" {
		t.Errorf("Expected TaskTitle=Test Task, got %s", agent.TaskTitle)
	}
	if agent.Status != daemon.AgentStatusRunning {
		t.Errorf("Expected Status=running, got %s", agent.Status)
	}
	if agent.MergeStatus != daemon.MergeStatusPending {
		t.Errorf("Expected MergeStatus=pending, got %s", agent.MergeStatus)
	}
	if agent.NumTurns != 5 {
		t.Errorf("Expected NumTurns=5, got %d", agent.NumTurns)
	}
	if agent.Duration != 120.5 {
		t.Errorf("Expected Duration=120.5, got %f", agent.Duration)
	}
	if agent.Commits != 3 {
		t.Errorf("Expected Commits=3, got %d", agent.Commits)
	}
	if agent.TokenUsage.InputTokens != 1000 {
		t.Errorf("Expected InputTokens=1000, got %d", agent.TokenUsage.InputTokens)
	}
	if agent.TokenUsage.OutputTokens != 500 {
		t.Errorf("Expected OutputTokens=500, got %d", agent.TokenUsage.OutputTokens)
	}
}

func TestParseStats(t *testing.T) {
	client := NewRemoteClient("8080")

	data := map[string]interface{}{
		"total_tasks":             float64(10),
		"completed_tasks":         float64(5),
		"failed_tasks":            float64(1),
		"running_tasks":           float64(4),
		"total_input_tokens":      float64(50000),
		"total_output_tokens":     float64(25000),
		"total_cache_read_tokens": float64(10000),
		"total_cost_usd":          float64(2.50),
		"total_turns":             float64(100),
	}

	stats := client.parseStats(data)

	if stats.TotalTasks != 10 {
		t.Errorf("Expected TotalTasks=10, got %d", stats.TotalTasks)
	}
	if stats.CompletedTasks != 5 {
		t.Errorf("Expected CompletedTasks=5, got %d", stats.CompletedTasks)
	}
	if stats.FailedTasks != 1 {
		t.Errorf("Expected FailedTasks=1, got %d", stats.FailedTasks)
	}
	if stats.RunningTasks != 4 {
		t.Errorf("Expected RunningTasks=4, got %d", stats.RunningTasks)
	}
	if stats.TotalInputTokens != 50000 {
		t.Errorf("Expected TotalInputTokens=50000, got %d", stats.TotalInputTokens)
	}
	if stats.TotalCostUSD != 2.50 {
		t.Errorf("Expected TotalCostUSD=2.50, got %f", stats.TotalCostUSD)
	}
}

func TestClientGetState(t *testing.T) {
	client := NewRemoteClient("8080")
	state := client.GetState()

	if state == nil {
		t.Error("Expected GetState to return non-nil state")
	}
}

func TestClientGetEventBus(t *testing.T) {
	client := NewRemoteClient("8080")
	eventBus := client.GetEventBus()

	if eventBus == nil {
		t.Error("Expected GetEventBus to return non-nil eventBus")
	}
}

// TestConnectWebSocket tests the WebSocket connection (integration test)
func TestConnectWebSocket(t *testing.T) {
	// Create a test WebSocket server
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	wsHandler := func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		// Send a state sync event
		event := events.Event{
			Type:      events.EventStateSync,
			Timestamp: time.Now(),
			Payload: map[string]interface{}{
				"is_paused": false,
				"agents":    map[string]interface{}{},
				"tasks":     map[string]interface{}{},
			},
		}
		data, _ := json.Marshal(event)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Keep connection open briefly
		time.Sleep(100 * time.Millisecond)
	}

	stateHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(daemon.RuntimeState{})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsHandler)
	mux.HandleFunc("/api/state", stateHandler)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Use the full URL to avoid normalization issues
	client := NewRemoteClient(server.URL)

	err := client.Connect()
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if !client.IsConnected() {
		t.Error("Expected client to be connected")
	}

	// Allow some time for WebSocket messages to be processed
	time.Sleep(50 * time.Millisecond)

	_ = client.Close()

	// After close, connection should be marked as disconnected (eventually)
	time.Sleep(50 * time.Millisecond)
}
