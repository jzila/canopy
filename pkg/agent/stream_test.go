package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStreamParser(t *testing.T) {
	// Test parsing a simple stream with multiple events
	stream := `{"type":"system","subtype":"init"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/tmp/test.txt"}}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello world"}]}}
{"type":"result","session_id":"test-123","result":"Done","total_cost_usd":0.01,"usage":{"input_tokens":100,"output_tokens":50}}
`

	reader := strings.NewReader(stream)
	parser := NewStreamParser(reader)

	// Event 1: system init (should be filtered)
	event1 := parser.NextEvent()
	if event1 == nil || event1.Type != "system" {
		t.Errorf("Expected system event, got %v", event1)
	}
	feed1 := FilterForLiveFeed(event1)
	if feed1 != nil {
		t.Errorf("Expected nil for system event, got %v", feed1)
	}

	// Event 2: tool use (should pass filter)
	event2 := parser.NextEvent()
	if event2 == nil || event2.Type != "assistant" {
		t.Errorf("Expected assistant event, got %v", event2)
	}
	feed2 := FilterForLiveFeed(event2)
	if feed2 == nil {
		t.Fatal("Expected non-nil for tool_use event")
	}
	if feed2.EventType != "tool_use" {
		t.Errorf("Expected event_type=tool_use, got %s", feed2.EventType)
	}
	if feed2.Data["tool"] != "Read" {
		t.Errorf("Expected tool=Read, got %v", feed2.Data["tool"])
	}
	if feed2.Data["file_path"] != "/tmp/test.txt" {
		t.Errorf("Expected file_path=/tmp/test.txt, got %v", feed2.Data["file_path"])
	}

	// Event 3: text (should pass filter)
	event3 := parser.NextEvent()
	if event3 == nil || event3.Type != "assistant" {
		t.Errorf("Expected assistant event, got %v", event3)
	}
	feed3 := FilterForLiveFeed(event3)
	if feed3 == nil {
		t.Fatal("Expected non-nil for text event")
	}
	if feed3.EventType != "text" {
		t.Errorf("Expected event_type=text, got %s", feed3.EventType)
	}
	if feed3.Data["text"] != "Hello world" {
		t.Errorf("Expected text='Hello world', got %v", feed3.Data["text"])
	}

	// Event 4: result (should be filtered)
	event4 := parser.NextEvent()
	if event4 == nil || event4.Type != "result" {
		t.Errorf("Expected result event, got %v", event4)
	}
	feed4 := FilterForLiveFeed(event4)
	if feed4 != nil {
		t.Errorf("Expected nil for result event, got %v", feed4)
	}

	// No more events
	event5 := parser.NextEvent()
	if event5 != nil {
		t.Errorf("Expected nil for end of stream, got %v", event5)
	}
}

func TestParseResultEvent(t *testing.T) {
	resultJSON := `{"type":"result","session_id":"test-123","result":"Done","total_cost_usd":0.01,"usage":{"input_tokens":100,"output_tokens":50}}`

	var result ClaudeStreamResult
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if result.Type != "result" {
		t.Errorf("Expected type=result, got %s", result.Type)
	}
	if result.SessionID != "test-123" {
		t.Errorf("Expected session_id=test-123, got %s", result.SessionID)
	}
	if result.Result != "Done" {
		t.Errorf("Expected result=Done, got %s", result.Result)
	}
	if result.TotalCostUSD != 0.01 {
		t.Errorf("Expected total_cost_usd=0.01, got %f", result.TotalCostUSD)
	}
	if result.Usage.InputTokens != 100 {
		t.Errorf("Expected input_tokens=100, got %d", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 50 {
		t.Errorf("Expected output_tokens=50, got %d", result.Usage.OutputTokens)
	}
}

func TestFilterToolUse(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name:     "Read tool",
			toolName: "Read",
			input:    map[string]interface{}{"file_path": "/home/user/file.go"},
			expected: map[string]interface{}{"tool": "Read", "file_path": "/home/user/file.go"},
		},
		{
			name:     "Bash tool",
			toolName: "Bash",
			input:    map[string]interface{}{"command": "ls -la"},
			expected: map[string]interface{}{"tool": "Bash", "command": "ls -la"},
		},
		{
			name:     "Grep tool",
			toolName: "Grep",
			input:    map[string]interface{}{"pattern": "func.*Test"},
			expected: map[string]interface{}{"tool": "Grep", "pattern": "func.*Test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := ContentBlock{
				Type:  "tool_use",
				Name:  tt.toolName,
				Input: tt.input,
			}
			result := filterToolUse(block)
			if result == nil {
				t.Fatal("Expected non-nil result")
			}
			if result.EventType != "tool_use" {
				t.Errorf("Expected event_type=tool_use, got %s", result.EventType)
			}
			for key, expectedValue := range tt.expected {
				if result.Data[key] != expectedValue {
					t.Errorf("Expected %s=%v, got %v", key, expectedValue, result.Data[key])
				}
			}
		})
	}
}
