package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jzila/canopy/pkg/daemon"
)

// Color palette for live feed events
var (
	// Event type colors
	toolColor    = lipgloss.Color("86")  // Cyan for tool uses
	successColor = lipgloss.Color("42")  // Green for success/completed
	errorColor   = lipgloss.Color("196") // Red for errors
	textColor    = lipgloss.Color("252") // Light gray for text
	dimColor     = lipgloss.Color("240") // Dim gray for metadata

	// Styles for different elements
	toolHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(toolColor)

	successHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(successColor)

	errorHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(errorColor)

	textHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(textColor)

	paramStyle = lipgloss.NewStyle().
			Foreground(dimColor)

	filePathStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("33")) // Blue for file paths

	commandStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")) // Orange for commands

	patternStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("183")) // Light purple for patterns

	boxBorderStyle = lipgloss.NewStyle().
			Foreground(dimColor)
)

// Tool icons for visual distinction
var toolIcons = map[string]string{
	"Read":       "📖",
	"Write":      "📝",
	"Edit":       "✏️",
	"Bash":       "💻",
	"Grep":       "🔍",
	"Glob":       "📂",
	"WebFetch":   "🌐",
	"WebSearch":  "🔎",
	"Task":       "🤖",
	"TodoWrite":  "📋",
	"LSP":        "🔗",
}

// RenderLiveFeedEvents renders a slice of live feed events with styling
func RenderLiveFeedEvents(events []daemon.LiveFeedEvent, width int) string {
	if len(events) == 0 {
		return ""
	}

	var lines []string
	contentWidth := width - 4 // Account for box borders

	for i, event := range events {
		rendered := renderEvent(event, contentWidth)
		if rendered != "" {
			// Add separator between events (except before first)
			if i > 0 {
				lines = append(lines, renderSeparator(contentWidth))
			}
			lines = append(lines, rendered)
		}
	}

	return strings.Join(lines, "\n")
}

// renderEvent renders a single live feed event
func renderEvent(event daemon.LiveFeedEvent, width int) string {
	switch event.EventType {
	case "tool_use":
		return renderToolUse(event.Data, width)
	case "text":
		return renderText(event.Data, width)
	case "file_change":
		return renderFileChange(event.Data, width)
	default:
		return ""
	}
}

// renderToolUse renders a tool use event with appropriate styling
func renderToolUse(data map[string]interface{}, width int) string {
	toolName, _ := data["tool"].(string)
	if toolName == "" {
		return ""
	}

	// Get icon for tool
	icon := toolIcons[toolName]
	if icon == "" {
		icon = "🔧"
	}

	// Build header
	header := toolHeaderStyle.Render(fmt.Sprintf("%s %s", icon, toolName))

	// Build parameter line based on tool type
	var paramLine string
	switch toolName {
	case "Read", "Write", "Edit":
		if filePath, ok := data["file_path"].(string); ok {
			// Truncate long paths
			displayPath := truncatePath(filePath, width-10)
			paramLine = filePathStyle.Render(displayPath)
		}
	case "Bash":
		if cmd, ok := data["command"].(string); ok {
			// Truncate long commands
			displayCmd := truncateString(cmd, width-10)
			// Replace newlines with spaces for display
			displayCmd = strings.ReplaceAll(displayCmd, "\n", " ")
			paramLine = commandStyle.Render("$ " + displayCmd)
		}
	case "Grep", "Glob":
		if pattern, ok := data["pattern"].(string); ok {
			displayPattern := truncateString(pattern, width-10)
			paramLine = patternStyle.Render("/" + displayPattern + "/")
		}
	}

	if paramLine != "" {
		return fmt.Sprintf("%s\n%s", header, paramLine)
	}
	return header
}

// renderText renders assistant text output
func renderText(data map[string]interface{}, width int) string {
	text, _ := data["text"].(string)
	if text == "" {
		return ""
	}

	// Truncate very long text
	if len(text) > 500 {
		text = text[:500] + "..."
	}

	// Wrap text to width
	wrapped := wrapText(text, width)

	header := textHeaderStyle.Render("💬 Assistant")
	return fmt.Sprintf("%s\n%s", header, wrapped)
}

// renderFileChange renders a file change event
func renderFileChange(data map[string]interface{}, width int) string {
	action, _ := data["action"].(string)
	filePath, _ := data["file_path"].(string)

	if filePath == "" {
		return ""
	}

	var icon string
	var style lipgloss.Style
	switch action {
	case "created":
		icon = "✨"
		style = successHeaderStyle
	case "modified":
		icon = "📝"
		style = toolHeaderStyle
	case "deleted":
		icon = "🗑️"
		style = errorHeaderStyle
	default:
		icon = "📄"
		style = toolHeaderStyle
	}

	displayPath := truncatePath(filePath, width-10)
	return style.Render(fmt.Sprintf("%s %s %s", icon, action, displayPath))
}

// renderSeparator renders a horizontal separator between events
func renderSeparator(width int) string {
	line := strings.Repeat("─", min(width, 50))
	return boxBorderStyle.Render(line)
}

// truncatePath truncates a file path intelligently
func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}

	// Try to show filename with some directory context
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	if len(base) >= maxLen-4 {
		return "..." + base[len(base)-(maxLen-3):]
	}

	remaining := maxLen - len(base) - 4 // 4 for ".../"
	if remaining > 0 && len(dir) > remaining {
		dir = "..." + dir[len(dir)-remaining:]
	}

	return dir + "/" + base
}

// truncateString truncates a string to maxLen with ellipsis
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 4 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// wrapText wraps text to the given width
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if len(line) <= width {
			lines = append(lines, line)
			continue
		}

		// Word wrap
		words := strings.Fields(line)
		var currentLine string
		for _, word := range words {
			if currentLine == "" {
				currentLine = word
			} else if len(currentLine)+1+len(word) <= width {
				currentLine += " " + word
			} else {
				lines = append(lines, currentLine)
				currentLine = word
			}
		}
		if currentLine != "" {
			lines = append(lines, currentLine)
		}
	}

	return strings.Join(lines, "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
