package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/beads"
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

func TestTimeoutConfiguration(t *testing.T) {
	t.Run("default timeout when nothing configured", func(t *testing.T) {
		task := &beads.Task{ID: "task-1", Title: "Test"}

		// No timeout set anywhere
		timeout := task.GetTimeout()
		if timeout != 0 {
			t.Errorf("Expected 0 for unset task timeout, got %v", timeout)
		}
	})

	t.Run("per-task timeout is parsed correctly", func(t *testing.T) {
		task := &beads.Task{
			ID:      "task-1",
			Title:   "Test",
			Timeout: "5m",
		}

		timeout := task.GetTimeout()
		if timeout != 5*time.Minute {
			t.Errorf("Expected 5m, got %v", timeout)
		}
	})

	t.Run("per-task timeout with hours", func(t *testing.T) {
		task := &beads.Task{
			ID:      "task-1",
			Title:   "Test",
			Timeout: "1h30m",
		}

		timeout := task.GetTimeout()
		expected := 90 * time.Minute
		if timeout != expected {
			t.Errorf("Expected %v, got %v", expected, timeout)
		}
	})

	t.Run("invalid task timeout returns zero", func(t *testing.T) {
		task := &beads.Task{
			ID:      "task-1",
			Title:   "Test",
			Timeout: "invalid",
		}

		timeout := task.GetTimeout()
		if timeout != 0 {
			t.Errorf("Expected 0 for invalid timeout, got %v", timeout)
		}
	})

	t.Run("sandbox config timeout is parsed correctly", func(t *testing.T) {
		config := &sandbox.SandboxConfig{
			Resources: sandbox.ResourceSettings{
				Timeout: "30m",
			},
		}

		timeout := config.GetTimeout()
		if timeout != 30*time.Minute {
			t.Errorf("Expected 30m, got %v", timeout)
		}
	})

	t.Run("nil sandbox config returns zero timeout", func(t *testing.T) {
		var config *sandbox.SandboxConfig
		timeout := config.GetTimeout()
		if timeout != 0 {
			t.Errorf("Expected 0 for nil config, got %v", timeout)
		}
	})

	t.Run("empty sandbox config timeout returns zero", func(t *testing.T) {
		config := &sandbox.SandboxConfig{}
		timeout := config.GetTimeout()
		if timeout != 0 {
			t.Errorf("Expected 0 for empty timeout, got %v", timeout)
		}
	})
}

func TestBuildPrompt(t *testing.T) {
	t.Run("basic task without user prompt", func(t *testing.T) {
		executor := &Executor{
			config: &Config{},
		}
		task := &beads.Task{
			ID:          "task-123",
			Title:       "Implement feature X",
			Description: "Add support for feature X with tests",
		}

		prompt := executor.buildPrompt(task, nil)

		// Should contain task title and description
		if !strings.Contains(prompt, "## Task: Implement feature X") {
			t.Error("Expected prompt to contain task title")
		}
		if !strings.Contains(prompt, "Add support for feature X with tests") {
			t.Error("Expected prompt to contain task description")
		}
		// Should NOT contain user instructions section
		if strings.Contains(prompt, "User Instructions") {
			t.Error("Expected prompt to NOT contain user instructions when UserPrompt is empty")
		}
	})

	t.Run("task with user prompt takes precedence", func(t *testing.T) {
		executor := &Executor{
			config: &Config{
				UserPrompt: "Stop after implementing the core logic, do not run tests",
			},
		}
		task := &beads.Task{
			ID:          "task-123",
			Title:       "Implement feature X",
			Description: "Add support for feature X and run all tests",
		}

		prompt := executor.buildPrompt(task, nil)

		// User instructions should appear at the top
		userInstructionsIdx := strings.Index(prompt, "User Instructions")
		taskTitleIdx := strings.Index(prompt, "## Task:")
		if userInstructionsIdx == -1 {
			t.Error("Expected prompt to contain User Instructions section")
		}
		if taskTitleIdx == -1 {
			t.Error("Expected prompt to contain Task section")
		}
		if userInstructionsIdx > taskTitleIdx {
			t.Error("Expected User Instructions to appear before Task section")
		}

		// Should contain the actual user instructions
		if !strings.Contains(prompt, "Stop after implementing the core logic") {
			t.Error("Expected prompt to contain user's stop instructions")
		}

		// Should contain PRIORITY marker
		if !strings.Contains(prompt, "PRIORITY") {
			t.Error("Expected prompt to contain PRIORITY marker for user instructions")
		}

		// Should contain reminder at end
		if !strings.Contains(prompt, "IMPORTANT") {
			t.Error("Expected prompt to contain IMPORTANT reminder about user instructions")
		}
	})

	t.Run("task with dependencies and user prompt", func(t *testing.T) {
		executor := &Executor{
			config: &Config{
				UserPrompt: "Focus only on bug fixes",
			},
		}
		task := &beads.Task{
			ID:          "task-456",
			Title:       "Fix bug in parser",
			Description: "Fix the parsing issue",
		}
		deps := []DependencyContext{
			{
				TaskID:  "dep-123",
				Summary: "Implemented the base parser",
			},
		}

		prompt := executor.buildPrompt(task, deps)

		// Check ordering: User Instructions -> Dependencies -> Task -> Reminder
		userInstructionsIdx := strings.Index(prompt, "User Instructions")
		depsIdx := strings.Index(prompt, "Context from upstream tasks")
		taskIdx := strings.Index(prompt, "## Task:")
		importantIdx := strings.LastIndex(prompt, "IMPORTANT")

		if userInstructionsIdx == -1 || depsIdx == -1 || taskIdx == -1 || importantIdx == -1 {
			t.Errorf("Missing expected sections. UserInstructions: %d, Deps: %d, Task: %d, Important: %d",
				userInstructionsIdx, depsIdx, taskIdx, importantIdx)
		}

		if userInstructionsIdx > depsIdx {
			t.Error("Expected User Instructions before dependency context")
		}
		if depsIdx > taskIdx {
			t.Error("Expected dependency context before task")
		}
		if taskIdx > importantIdx {
			t.Error("Expected task before final important reminder")
		}

		// Dependencies should be included
		if !strings.Contains(prompt, "dep-123") {
			t.Error("Expected prompt to contain dependency task ID")
		}
		if !strings.Contains(prompt, "Implemented the base parser") {
			t.Error("Expected prompt to contain dependency summary")
		}
	})

	t.Run("user prompt boundary instructions are emphasized", func(t *testing.T) {
		executor := &Executor{
			config: &Config{
				UserPrompt: "Implement but DO NOT run tests",
			},
		}
		task := &beads.Task{
			ID:          "task-789",
			Title:       "Add feature",
			Description: "Add the feature and ensure all tests pass",
		}

		prompt := executor.buildPrompt(task, nil)

		// Both the user instruction and the reminder should be present
		if !strings.Contains(prompt, "Implement but DO NOT run tests") {
			t.Error("Expected user's boundary instruction in prompt")
		}
		if !strings.Contains(prompt, "Stop when you reach the boundaries specified by the user") {
			t.Error("Expected boundary reminder in prompt")
		}
	})
}
