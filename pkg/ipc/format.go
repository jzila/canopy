package ipc

import (
	"bytes"
	"encoding/json"
	"strings"
)

// FormatJSON formats a value as pretty-printed JSON with 2-space indentation.
// String fields containing newlines, tabs, and other control characters are
// rendered with actual whitespace rather than escaped sequences.
func FormatJSON(v interface{}) (string, error) {
	// First, marshal to JSON with indentation
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}

	// Unescape control characters in string values for readability
	result := unescapeJSONStrings(data)
	return result, nil
}

// unescapeJSONStrings processes JSON bytes and converts escaped control
// characters (\n, \t, etc.) within string values to their actual characters.
// This makes multi-line string content (like result summaries) display
// with actual line breaks for readability.
func unescapeJSONStrings(data []byte) string {
	var result bytes.Buffer
	inString := false
	escaped := false
	i := 0

	for i < len(data) {
		b := data[i]

		if escaped {
			// Handle escape sequence
			switch b {
			case 'n':
				if inString {
					result.WriteByte('\n')
				} else {
					result.WriteString("\\n")
				}
			case 't':
				if inString {
					result.WriteByte('\t')
				} else {
					result.WriteString("\\t")
				}
			case 'r':
				if inString {
					result.WriteByte('\r')
				} else {
					result.WriteString("\\r")
				}
			case '\\':
				result.WriteByte('\\')
			case '"':
				result.WriteByte('"')
			case '/':
				result.WriteByte('/')
			case 'b':
				if inString {
					result.WriteByte('\b')
				} else {
					result.WriteString("\\b")
				}
			case 'f':
				if inString {
					result.WriteByte('\f')
				} else {
					result.WriteString("\\f")
				}
			case 'u':
				// Unicode escape sequence \uXXXX - keep as-is
				result.WriteString("\\u")
			default:
				// Unknown escape, preserve original
				result.WriteByte('\\')
				result.WriteByte(b)
			}
			escaped = false
			i++
			continue
		}

		if b == '\\' {
			escaped = true
			i++
			continue
		}

		if b == '"' && !escaped {
			inString = !inString
		}

		result.WriteByte(b)
		i++
	}

	return result.String()
}

// FormatAgentResult formats an AgentResult or AgentDonePayload for logging.
// Returns a human-readable string with proper indentation.
func FormatAgentResult(payload interface{}) string {
	formatted, err := FormatJSON(payload)
	if err != nil {
		return "<error formatting JSON>"
	}
	return formatted
}

// IndentMultilineString adds a prefix to each line of a multi-line string.
// Useful for indenting log output.
func IndentMultilineString(s string, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
