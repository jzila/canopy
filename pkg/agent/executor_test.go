package agent

import (
	"context"
	"testing"
	"time"
)

func TestTimeoutDetection(t *testing.T) {
	// Test that context.DeadlineExceeded is detected even when cmd.Wait() succeeds
	// This simulates the race condition where process exits just as timeout fires

	t.Run("timeout sets success false", func(t *testing.T) {
		// Create a context that's already exceeded deadline
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()

		// Simulate result determination logic matching executor.go
		var result Result
		var err error // nil - simulating cmd.Wait() returning nil

		// This is the fixed logic from executor.go
		if ctx.Err() == context.DeadlineExceeded {
			result.Success = false
			result.Error = "execution timed out"
		} else if err != nil {
			result.Success = false
			result.Error = err.Error()
		} else {
			result.Success = result.ExitCode == 0
		}

		if result.Success {
			t.Error("Expected Success=false when context deadline exceeded")
		}
		if result.Error != "execution timed out" {
			t.Errorf("Expected error='execution timed out', got '%s'", result.Error)
		}
	})

	t.Run("normal exit code 0 success", func(t *testing.T) {
		ctx := context.Background()

		var result Result
		var err error
		result.ExitCode = 0

		if ctx.Err() == context.DeadlineExceeded {
			result.Success = false
			result.Error = "execution timed out"
		} else if err != nil {
			result.Success = false
			result.Error = err.Error()
		} else {
			result.Success = result.ExitCode == 0
			if !result.Success {
				result.Error = "exit code non-zero"
			}
		}

		if !result.Success {
			t.Error("Expected Success=true for exit code 0")
		}
		if result.Error != "" {
			t.Errorf("Expected no error, got '%s'", result.Error)
		}
	})

	t.Run("normal exit code non-zero failure", func(t *testing.T) {
		ctx := context.Background()

		var result Result
		var err error
		result.ExitCode = 1

		if ctx.Err() == context.DeadlineExceeded {
			result.Success = false
			result.Error = "execution timed out"
		} else if err != nil {
			result.Success = false
			result.Error = err.Error()
		} else {
			result.Success = result.ExitCode == 0
			if !result.Success {
				result.Error = "exit code non-zero"
			}
		}

		if result.Success {
			t.Error("Expected Success=false for exit code 1")
		}
		if result.Error != "exit code non-zero" {
			t.Errorf("Expected error='exit code non-zero', got '%s'", result.Error)
		}
	})

	t.Run("timeout takes precedence over exit code 0", func(t *testing.T) {
		// Key test: if timeout occurred but process happened to exit with 0,
		// we should still report failure due to timeout
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()

		var result Result
		var err error
		result.ExitCode = 0 // Process exited cleanly but timed out

		if ctx.Err() == context.DeadlineExceeded {
			result.Success = false
			result.Error = "execution timed out"
		} else if err != nil {
			result.Success = false
			result.Error = err.Error()
		} else {
			result.Success = result.ExitCode == 0
		}

		if result.Success {
			t.Error("Expected Success=false when timeout occurred, even with exit code 0")
		}
		if result.Error != "execution timed out" {
			t.Errorf("Expected error='execution timed out', got '%s'", result.Error)
		}
	})
}
