package errors

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{0 * time.Second, "0 seconds"},
		{1 * time.Second, "1 seconds"},
		{30 * time.Second, "30 seconds"},
		{59 * time.Second, "59 seconds"},
		{1 * time.Minute, "1 minute"},
		{2 * time.Minute, "2 minutes"},
		{59 * time.Minute, "59 minutes"},
		{1 * time.Hour, "1 hour"},
		{1*time.Hour + 30*time.Minute, "1 hour 30 minutes"},
		{2 * time.Hour, "2 hours"},
		{2*time.Hour + 15*time.Minute, "2 hours 15 minutes"},
		{24 * time.Hour, "24 hours"},
	}

	for _, tt := range tests {
		t.Run(tt.duration.String(), func(t *testing.T) {
			result := formatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestRunActiveError(t *testing.T) {
	startedAt := time.Now().Add(-5 * time.Minute)
	err := &RunActiveError{
		RepoPath:  "/home/user/myproject",
		RunID:     "canopy-abc123",
		StartedAt: startedAt,
	}

	// Test Error() method
	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}

	// Check for expected content
	expectedParts := []string{
		"canopy-abc123",
		"/home/user/myproject",
		"canopy run --status",
		"canopy run --stop",
	}
	for _, part := range expectedParts {
		if !containsSubstring(msg, part) {
			t.Errorf("error message should contain %q, got: %s", part, msg)
		}
	}

	// Test Unwrap() method
	unwrapped := err.Unwrap()
	if unwrapped != ErrRunAlreadyActive {
		t.Errorf("expected ErrRunAlreadyActive, got %v", unwrapped)
	}

	// Test errors.Is()
	if !Is(err, ErrRunAlreadyActive) {
		t.Error("expected error to match ErrRunAlreadyActive via Is()")
	}

	// Test errors.As()
	var runActiveErr *RunActiveError
	if !As(err, &runActiveErr) {
		t.Error("expected error to be extractable via As()")
	}
	if runActiveErr.RunID != "canopy-abc123" {
		t.Errorf("expected RunID %q, got %q", "canopy-abc123", runActiveErr.RunID)
	}
}

// containsSubstring checks if s contains substr
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
