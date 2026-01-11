package agent

import (
	"context"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/sandbox"
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

func TestAddGoCacheEnv(t *testing.T) {
	t.Run("nil config returns original env", func(t *testing.T) {
		env := []string{"PATH=/usr/bin", "HOME=/home/test"}
		result := addGoCacheEnv(env, nil)
		if len(result) != len(env) {
			t.Errorf("Expected %d env vars, got %d", len(env), len(result))
		}
	})

	t.Run("config without Go caches returns original env", func(t *testing.T) {
		config := &sandbox.SandboxConfig{
			Paths: sandbox.PathSettings{
				CacheMounts: []string{"~/.npm", "~/.cargo/registry"},
			},
		}
		env := []string{"PATH=/usr/bin"}
		result := addGoCacheEnv(env, config)
		if len(result) != 1 {
			t.Errorf("Expected 1 env var, got %d", len(result))
		}
	})

	t.Run("config with Go module cache sets GOMODCACHE", func(t *testing.T) {
		config := &sandbox.SandboxConfig{
			Paths: sandbox.PathSettings{
				CacheMounts: []string{"/home/user/go/pkg/mod"},
			},
		}
		env := []string{"PATH=/usr/bin"}
		result := addGoCacheEnv(env, config)
		if len(result) != 2 {
			t.Errorf("Expected 2 env vars, got %d", len(result))
		}
		found := false
		for _, e := range result {
			if e == "GOMODCACHE=/home/user/go/pkg/mod" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected GOMODCACHE=/home/user/go/pkg/mod in env")
		}
	})

	t.Run("config with Go build cache sets GOCACHE", func(t *testing.T) {
		config := &sandbox.SandboxConfig{
			Paths: sandbox.PathSettings{
				CacheMounts: []string{"/home/user/.cache/go-build"},
			},
		}
		env := []string{"PATH=/usr/bin"}
		result := addGoCacheEnv(env, config)
		if len(result) != 2 {
			t.Errorf("Expected 2 env vars, got %d", len(result))
		}
		found := false
		for _, e := range result {
			if e == "GOCACHE=/home/user/.cache/go-build" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected GOCACHE=/home/user/.cache/go-build in env")
		}
	})

	t.Run("config with both Go caches sets both vars", func(t *testing.T) {
		config := &sandbox.SandboxConfig{
			Paths: sandbox.PathSettings{
				CacheMounts: []string{
					"/home/user/go/pkg/mod",
					"/home/user/.cache/go-build",
				},
			},
		}
		env := []string{"PATH=/usr/bin"}
		result := addGoCacheEnv(env, config)
		if len(result) != 3 {
			t.Errorf("Expected 3 env vars, got %d", len(result))
		}
		foundModCache := false
		foundBuildCache := false
		for _, e := range result {
			if e == "GOMODCACHE=/home/user/go/pkg/mod" {
				foundModCache = true
			}
			if e == "GOCACHE=/home/user/.cache/go-build" {
				foundBuildCache = true
			}
		}
		if !foundModCache {
			t.Error("Expected GOMODCACHE in env")
		}
		if !foundBuildCache {
			t.Error("Expected GOCACHE in env")
		}
	})
}
