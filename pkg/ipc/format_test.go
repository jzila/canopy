package ipc

import (
	"strings"
	"testing"
)

func TestFormatJSON_BasicTypes(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    string
		wantErr bool
	}{
		{
			name: "simple struct",
			input: struct {
				Name  string `json:"name"`
				Count int    `json:"count"`
			}{Name: "test", Count: 42},
			want: `{
  "name": "test",
  "count": 42
}`,
		},
		{
			name: "AgentResult",
			input: AgentResult{
				ExitCode:        0,
				DurationSeconds: 10.5,
				InputTokens:     1000,
				OutputTokens:    500,
				CostUSD:         0.05,
				FilesChanged:    3,
			},
			want: `{
  "exit_code": 0,
  "duration_seconds": 10.5,
  "input_tokens": 1000,
  "output_tokens": 500,
  "cost_usd": 0.05,
  "files_changed": 3
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FormatJSON(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("FormatJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("FormatJSON() =\n%s\n\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestFormatJSON_ControlCharacters(t *testing.T) {
	// Test that newlines in string values are rendered as actual newlines
	input := struct {
		Result string `json:"result"`
	}{
		Result: "Line 1\nLine 2\nLine 3",
	}

	got, err := FormatJSON(input)
	if err != nil {
		t.Fatalf("FormatJSON() error = %v", err)
	}

	// The output should contain actual newlines, not escaped \n
	if strings.Contains(got, `\n`) {
		t.Errorf("FormatJSON() should not contain escaped newlines, got:\n%s", got)
	}

	// Should have actual newlines in the result value
	if !strings.Contains(got, "Line 1\nLine 2\nLine 3") {
		t.Errorf("FormatJSON() should contain actual newlines in string value, got:\n%s", got)
	}
}

func TestFormatJSON_TabCharacters(t *testing.T) {
	input := struct {
		Code string `json:"code"`
	}{
		Code: "func main() {\n\tfmt.Println(\"hello\")\n}",
	}

	got, err := FormatJSON(input)
	if err != nil {
		t.Fatalf("FormatJSON() error = %v", err)
	}

	// The output should contain actual tabs, not escaped \t
	if strings.Contains(got, `\t`) {
		t.Errorf("FormatJSON() should not contain escaped tabs, got:\n%s", got)
	}

	// Should have actual tab in the result value
	if !strings.Contains(got, "\t") {
		t.Errorf("FormatJSON() should contain actual tab in string value, got:\n%s", got)
	}
}

func TestFormatJSON_PreservesQuotesAndBackslashes(t *testing.T) {
	input := struct {
		Path  string `json:"path"`
		Quote string `json:"quote"`
	}{
		Path:  "C:\\Users\\test",
		Quote: `He said "hello"`,
	}

	got, err := FormatJSON(input)
	if err != nil {
		t.Fatalf("FormatJSON() error = %v", err)
	}

	// Should preserve backslashes in paths (though single backslashes get unescaped)
	if !strings.Contains(got, "C:\\Users\\test") {
		t.Errorf("FormatJSON() should preserve backslashes, got:\n%s", got)
	}

	// Should preserve quotes within strings
	if !strings.Contains(got, `"hello"`) {
		t.Errorf("FormatJSON() should preserve quotes, got:\n%s", got)
	}
}

func TestIndentMultilineString(t *testing.T) {
	input := "line 1\nline 2\nline 3"
	got := IndentMultilineString(input, "  ")
	want := "  line 1\n  line 2\n  line 3"

	if got != want {
		t.Errorf("IndentMultilineString() = %q, want %q", got, want)
	}
}

func TestFormatAgentResult(t *testing.T) {
	result := AgentDonePayload{
		AgentID: "agent-test-1",
		Result: AgentResult{
			ExitCode:        0,
			DurationSeconds: 15.3,
			FilesChanged:    2,
		},
	}

	got := FormatAgentResult(result)

	if !strings.Contains(got, "agent-test-1") {
		t.Errorf("FormatAgentResult() should contain agent_id, got:\n%s", got)
	}
	if !strings.Contains(got, "15.3") {
		t.Errorf("FormatAgentResult() should contain duration, got:\n%s", got)
	}
}
