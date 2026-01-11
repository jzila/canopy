package agent

import (
	"bufio"
	"encoding/json"
	"io"
)

// ClaudeStreamResult represents the final result from stream-json output
type ClaudeStreamResult struct {
	Type         string      `json:"type"`
	SessionID    string      `json:"session_id"`
	Result       string      `json:"result"`
	TotalCostUSD float64     `json:"total_cost_usd"`
	Usage        StreamUsage `json:"usage"`
	SubType      string      `json:"subtype"`
}

// StreamUsage represents token usage from the stream result
type StreamUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// StreamEvent represents a parsed event from Claude's stream-json output
type StreamEvent struct {
	Type    string                 `json:"type"`
	SubType string                 `json:"subtype,omitempty"`
	Message *StreamMessage         `json:"message,omitempty"`
	Payload map[string]interface{} `json:"-"` // For custom fields
}

// StreamMessage represents an assistant or user message in the stream
type StreamMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ContentBlock represents a single content block (text or tool_use)
type ContentBlock struct {
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	Name  string                 `json:"name,omitempty"`  // For tool_use
	Input map[string]interface{} `json:"input,omitempty"` // For tool_use
}

// LiveFeedEvent represents a filtered event for live streaming to dashboard
type LiveFeedEvent struct {
	EventType string                 `json:"event_type"` // "tool_use", "file_change", "text"
	Data      map[string]interface{} `json:"data"`
}

// StreamParser reads and filters events from Claude's stream-json output
type StreamParser struct {
	scanner *bufio.Scanner
}

// NewStreamParser creates a parser for Claude's stream-json output
func NewStreamParser(reader io.Reader) *StreamParser {
	return &StreamParser{
		scanner: bufio.NewScanner(reader),
	}
}

// NextEvent reads and parses the next event from the stream
// Returns nil when stream ends or on parse error
func (p *StreamParser) NextEvent() *StreamEvent {
	if !p.scanner.Scan() {
		return nil
	}

	line := p.scanner.Bytes()
	if len(line) == 0 {
		return nil
	}

	var event StreamEvent
	if err := json.Unmarshal(line, &event); err != nil {
		return nil
	}

	return &event
}

// FilterForLiveFeed converts a stream event to a live feed event if relevant
// Returns nil for events that should not be streamed (hooks, init, thinking, etc.)
func FilterForLiveFeed(event *StreamEvent) *LiveFeedEvent {
	if event == nil {
		return nil
	}

	// Skip system events (hooks, init)
	if event.Type == "system" {
		return nil
	}

	// Skip result events (these are summary, not live)
	if event.Type == "result" {
		return nil
	}

	// Process assistant messages
	if event.Type == "assistant" && event.Message != nil {
		return filterAssistantMessage(event.Message)
	}

	return nil
}

// filterAssistantMessage extracts relevant data from assistant messages
func filterAssistantMessage(msg *StreamMessage) *LiveFeedEvent {
	// Parse content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return nil
	}

	for _, block := range blocks {
		// Filter tool use events
		if block.Type == "tool_use" {
			return filterToolUse(block)
		}

		// Filter text content (non-thinking)
		if block.Type == "text" && block.Text != "" {
			// Skip thinking blocks (heuristic: thinking is longer and more verbose)
			// For now, we'll send all text to dashboard and let UI filter
			return &LiveFeedEvent{
				EventType: "text",
				Data: map[string]interface{}{
					"text": block.Text,
				},
			}
		}
	}

	return nil
}

// filterToolUse extracts key information from tool use
func filterToolUse(block ContentBlock) *LiveFeedEvent {
	toolName := block.Name
	input := block.Input

	data := map[string]interface{}{
		"tool": toolName,
	}

	// Extract key parameters based on tool type
	switch toolName {
	case "Read", "Write", "Edit":
		if filePath, ok := input["file_path"].(string); ok {
			data["file_path"] = filePath
		}
	case "Bash":
		if cmd, ok := input["command"].(string); ok {
			data["command"] = cmd
		}
	case "Grep":
		if pattern, ok := input["pattern"].(string); ok {
			data["pattern"] = pattern
		}
	case "Glob":
		if pattern, ok := input["pattern"].(string); ok {
			data["pattern"] = pattern
		}
	}

	return &LiveFeedEvent{
		EventType: "tool_use",
		Data:      data,
	}
}
