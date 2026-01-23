package agent

import (
	"bufio"
	"encoding/json"
	"io"
)

// ClaudeStreamResult represents the final result from stream-json output
type ClaudeStreamResult struct {
	Type          string                    `json:"type"`
	SubType       string                    `json:"subtype"`
	SessionID     string                    `json:"session_id"`
	Result        string                    `json:"result"`
	TotalCostUSD  float64                   `json:"total_cost_usd"`
	DurationMS    int64                     `json:"duration_ms"`
	DurationAPIMS int64                     `json:"duration_api_ms"`
	NumTurns      int                       `json:"num_turns"`
	Usage         StreamUsage                    `json:"usage"`
	ModelUsage    map[string]StreamModelUsageData `json:"modelUsage"`
	IsError       bool                      `json:"is_error"`
}

// StreamUsage represents token usage from the stream result
type StreamUsage struct {
	InputTokens             int                `json:"input_tokens"`
	OutputTokens            int                `json:"output_tokens"`
	CacheCreationInputToken int                `json:"cache_creation_input_tokens"`
	CacheReadInputTokens    int                `json:"cache_read_input_tokens"`
	ServiceTier             string             `json:"service_tier"`
	CacheCreation           *CacheCreationInfo `json:"cache_creation,omitempty"`
}

// CacheCreationInfo contains detailed cache token information
type CacheCreationInfo struct {
	Ephemeral1HInputTokens int `json:"ephemeral_1h_input_tokens"`
	Ephemeral5MInputTokens int `json:"ephemeral_5m_input_tokens"`
}

// StreamModelUsageData represents per-model usage statistics from stream result
// Note: JSON keys use camelCase to match Claude's stream-json output format
type StreamModelUsageData struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	WebSearchRequests        int     `json:"webSearchRequests"`
	CostUSD                  float64 `json:"costUSD"`
	ContextWindow            int     `json:"contextWindow"`
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

// LiveFeedEventType defines the type of live feed event
type LiveFeedEventType string

const (
	// LiveFeedEventToolUse represents a tool invocation by the agent
	LiveFeedEventToolUse LiveFeedEventType = "tool_use"
	// LiveFeedEventText represents text output from the agent
	LiveFeedEventText LiveFeedEventType = "text"
	// LiveFeedEventFileChange represents a file modification
	LiveFeedEventFileChange LiveFeedEventType = "file_change"
)

// LiveFeedEventData is an interface for type-safe event data
type LiveFeedEventData interface {
	eventData() // marker method
}

// ToolUseEventData contains data for a tool_use event
type ToolUseEventData struct {
	Tool     string `json:"tool"`                // Tool name (e.g., "Read", "Bash", "Grep")
	FilePath string `json:"file_path,omitempty"` // For file tools (Read, Write, Edit)
	Command  string `json:"command,omitempty"`   // For Bash tool
	Pattern  string `json:"pattern,omitempty"`   // For Grep/Glob tools
}

func (ToolUseEventData) eventData() {}

// TextEventData contains data for a text event
type TextEventData struct {
	Text string `json:"text"` // The text content
}

func (TextEventData) eventData() {}

// LiveFeedEvent represents a filtered event for live streaming to dashboard
type LiveFeedEvent struct {
	EventType LiveFeedEventType      `json:"event_type"` // "tool_use", "file_change", "text"
	Data      LiveFeedEventData      `json:"-"`          // Type-safe event data (not directly serialized)
	RawData   map[string]interface{} `json:"data"`       // For JSON serialization
}

// NewToolUseEvent creates a new tool_use event with typed data
func NewToolUseEvent(tool, filePath, command, pattern string) *LiveFeedEvent {
	data := ToolUseEventData{
		Tool:     tool,
		FilePath: filePath,
		Command:  command,
		Pattern:  pattern,
	}
	rawData := map[string]interface{}{"tool": tool}
	if filePath != "" {
		rawData["file_path"] = filePath
	}
	if command != "" {
		rawData["command"] = command
	}
	if pattern != "" {
		rawData["pattern"] = pattern
	}
	return &LiveFeedEvent{
		EventType: LiveFeedEventToolUse,
		Data:      data,
		RawData:   rawData,
	}
}

// NewTextEvent creates a new text event with typed data
func NewTextEvent(text string) *LiveFeedEvent {
	data := TextEventData{Text: text}
	return &LiveFeedEvent{
		EventType: LiveFeedEventText,
		Data:      data,
		RawData:   map[string]interface{}{"text": text},
	}
}

// GetToolUseData returns the typed tool_use data if this is a tool_use event
func (e *LiveFeedEvent) GetToolUseData() (ToolUseEventData, bool) {
	if e == nil {
		return ToolUseEventData{}, false
	}
	if d, ok := e.Data.(ToolUseEventData); ok {
		return d, true
	}
	return ToolUseEventData{}, false
}

// GetTextData returns the typed text data if this is a text event
func (e *LiveFeedEvent) GetTextData() (TextEventData, bool) {
	if e == nil {
		return TextEventData{}, false
	}
	if d, ok := e.Data.(TextEventData); ok {
		return d, true
	}
	return TextEventData{}, false
}

// MaxStreamLineSize is the maximum size of a single line in the stream.
// Claude CLI can emit large events (assistant messages with code blocks,
// tool outputs, etc.) so we use a larger buffer than the default 64KB.
const MaxStreamLineSize = 4 * 1024 * 1024 // 4MB

// StreamParser reads and filters events from Claude's stream-json output
type StreamParser struct {
	scanner *bufio.Scanner
}

// NewStreamParser creates a parser for Claude's stream-json output
func NewStreamParser(reader io.Reader) *StreamParser {
	scanner := bufio.NewScanner(reader)
	// Use a larger buffer to handle long lines (assistant messages with code blocks, etc.)
	// The default 64KB can be exceeded by large tool outputs or code blocks
	buf := make([]byte, 64*1024) // Start with 64KB
	scanner.Buffer(buf, MaxStreamLineSize)
	return &StreamParser{
		scanner: scanner,
	}
}

// Err returns any error that occurred during scanning.
// This should be called after the scan loop to check for scanner errors
// like buffer overflow (token too long).
func (p *StreamParser) Err() error {
	return p.scanner.Err()
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
			return NewTextEvent(block.Text)
		}
	}

	return nil
}

// InteractiveToolNames lists tools that require user input and should cause
// worker agents to fail immediately (no user present to respond).
var InteractiveToolNames = map[string]bool{
	"AskUserQuestion": true,
}

// IsInteractiveTool returns true if the tool name requires user input.
func IsInteractiveTool(toolName string) bool {
	return InteractiveToolNames[toolName]
}

// CheckForInteractiveTool examines a stream event and returns the tool name
// if it's an interactive tool that requires user input, otherwise returns empty string.
func CheckForInteractiveTool(event *StreamEvent) string {
	if event == nil || event.Type != "assistant" || event.Message == nil {
		return ""
	}

	// Parse content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(event.Message.Content, &blocks); err != nil {
		return ""
	}

	for _, block := range blocks {
		if block.Type == "tool_use" && IsInteractiveTool(block.Name) {
			return block.Name
		}
	}

	return ""
}

// filterToolUse extracts key information from tool use
func filterToolUse(block ContentBlock) *LiveFeedEvent {
	toolName := block.Name
	input := block.Input

	var filePath, command, pattern string

	// Extract key parameters based on tool type
	switch toolName {
	case "Read", "Write", "Edit":
		filePath, _ = input["file_path"].(string)
	case "Bash":
		command, _ = input["command"].(string)
	case "Grep", "Glob":
		pattern, _ = input["pattern"].(string)
	}

	return NewToolUseEvent(toolName, filePath, command, pattern)
}
