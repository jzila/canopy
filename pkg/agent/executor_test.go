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

func TestInputBlockedDetection(t *testing.T) {
	t.Run("input blocked takes precedence over exit code 0", func(t *testing.T) {
		// Simulate the result determination logic when input is blocked
		var result Result
		result.ExitCode = 0 // Process exited cleanly but was blocked
		inputBlockedTool := "AskUserQuestion"
		sessionID := "test-session-123"

		// Simulate the success determination logic from executor.go
		if inputBlockedTool != "" {
			result.Success = false
			result.InputBlocked = true
			if sessionID != "" {
				result.Error = "agent paused waiting for user input (" + inputBlockedTool + " tool); session " + sessionID
			} else {
				result.Error = "agent paused waiting for user input (" + inputBlockedTool + " tool)"
			}
		} else {
			result.Success = result.ExitCode == 0
		}

		if result.Success {
			t.Error("Expected Success=false when input blocked")
		}
		if !result.InputBlocked {
			t.Error("Expected InputBlocked=true")
		}
		if !strings.Contains(result.Error, "AskUserQuestion") {
			t.Errorf("Expected error to contain 'AskUserQuestion', got: %s", result.Error)
		}
		if !strings.Contains(result.Error, "test-session-123") {
			t.Errorf("Expected error to contain session ID, got: %s", result.Error)
		}
	})

	t.Run("input blocked without session ID", func(t *testing.T) {
		var result Result
		result.ExitCode = 0
		inputBlockedTool := "AskUserQuestion"
		sessionID := ""

		if inputBlockedTool != "" {
			result.Success = false
			result.InputBlocked = true
			if sessionID != "" {
				result.Error = "agent paused waiting for user input (" + inputBlockedTool + " tool); session " + sessionID
			} else {
				result.Error = "agent paused waiting for user input (" + inputBlockedTool + " tool)"
			}
		}

		if result.Success {
			t.Error("Expected Success=false when input blocked")
		}
		if !result.InputBlocked {
			t.Error("Expected InputBlocked=true")
		}
		if !strings.Contains(result.Error, "AskUserQuestion") {
			t.Errorf("Expected error to contain 'AskUserQuestion', got: %s", result.Error)
		}
		if strings.Contains(result.Error, "session") {
			t.Errorf("Expected error to NOT contain 'session' when no session ID, got: %s", result.Error)
		}
	})

	t.Run("normal execution not input blocked", func(t *testing.T) {
		var result Result
		result.ExitCode = 0
		inputBlockedTool := "" // No interactive tool detected

		if inputBlockedTool != "" {
			result.Success = false
			result.InputBlocked = true
		} else {
			result.Success = result.ExitCode == 0
		}

		if !result.Success {
			t.Error("Expected Success=true for normal execution")
		}
		if result.InputBlocked {
			t.Error("Expected InputBlocked=false for normal execution")
		}
	})
}

func TestSetModelAndTimeout(t *testing.T) {
	t.Run("SetModel updates model", func(t *testing.T) {
		e := NewExecutor(&Config{Timeout: DefaultTimeout})
		e.SetModel("claude-3-opus")
		model, _ := e.getModelAndTimeout()
		if model != "claude-3-opus" {
			t.Errorf("Expected model 'claude-3-opus', got %q", model)
		}
	})

	t.Run("SetTimeout updates timeout", func(t *testing.T) {
		e := NewExecutor(&Config{Timeout: DefaultTimeout})
		e.SetTimeout(30 * time.Minute)
		_, timeout := e.getModelAndTimeout()
		if timeout != 30*time.Minute {
			t.Errorf("Expected 30m timeout, got %v", timeout)
		}
	})

	t.Run("SetTimeout zero resets to default", func(t *testing.T) {
		e := NewExecutor(&Config{Timeout: 30 * time.Minute})
		e.SetTimeout(0)
		_, timeout := e.getModelAndTimeout()
		if timeout != DefaultTimeout {
			t.Errorf("Expected default timeout %v, got %v", DefaultTimeout, timeout)
		}
	})

	t.Run("SetTimeout negative is ignored", func(t *testing.T) {
		e := NewExecutor(&Config{Timeout: 30 * time.Minute})
		e.SetTimeout(-5 * time.Minute)
		_, timeout := e.getModelAndTimeout()
		if timeout != 30*time.Minute {
			t.Errorf("Expected 30m timeout unchanged, got %v", timeout)
		}
	})

	t.Run("concurrent SetModel and getModelAndTimeout", func(t *testing.T) {
		e := NewExecutor(&Config{Timeout: DefaultTimeout})
		done := make(chan struct{})

		// Writer goroutine
		go func() {
			defer close(done)
			for i := 0; i < 1000; i++ {
				e.SetModel("model-a")
				e.SetModel("model-b")
			}
		}()

		// Reader goroutine
		for i := 0; i < 1000; i++ {
			model, _ := e.getModelAndTimeout()
			// Just verify no panic; value can be any of the set values
			_ = model
		}
		<-done
	})
}

func TestBuildPrompt(t *testing.T) {
	t.Run("basic task", func(t *testing.T) {
		executor := &Executor{
			config: &Config{},
		}
		task := &beads.Task{
			ID:          "task-123",
			Title:       "Implement feature X",
			Description: "Add support for feature X with tests",
		}

		prompt := executor.buildPrompt(task, nil)

		// Should contain system prompt at the beginning
		if !strings.HasPrefix(prompt, "## Worker Agent") {
			t.Error("Expected prompt to start with system prompt")
		}
		// Should contain key behavioral instructions
		if !strings.Contains(prompt, "autonomous worker agent") {
			t.Error("Expected prompt to contain autonomous operation context")
		}
		if !strings.Contains(prompt, "DO NOT:") {
			t.Error("Expected prompt to contain DO NOT section")
		}
		if !strings.Contains(prompt, "AskUserQuestion") {
			t.Error("Expected prompt to mention AskUserQuestion prohibition")
		}

		// Should contain task title and description
		if !strings.Contains(prompt, "## Task: Implement feature X") {
			t.Error("Expected prompt to contain task title")
		}
		if !strings.Contains(prompt, "Add support for feature X with tests") {
			t.Error("Expected prompt to contain task description")
		}

		// System prompt should come before task
		systemIdx := strings.Index(prompt, "## Worker Agent")
		taskIdx := strings.Index(prompt, "## Task:")
		if systemIdx > taskIdx {
			t.Error("Expected system prompt before task")
		}
	})

	t.Run("task with dependencies", func(t *testing.T) {
		executor := &Executor{
			config: &Config{},
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

		// Check ordering: System Prompt -> Dependencies -> Task
		systemIdx := strings.Index(prompt, "## Worker Agent")
		depsIdx := strings.Index(prompt, "Context from upstream tasks")
		taskIdx := strings.Index(prompt, "## Task:")

		if systemIdx == -1 || depsIdx == -1 || taskIdx == -1 {
			t.Errorf("Missing expected sections. System: %d, Deps: %d, Task: %d", systemIdx, depsIdx, taskIdx)
		}

		if systemIdx > depsIdx {
			t.Error("Expected system prompt before dependencies")
		}
		if depsIdx > taskIdx {
			t.Error("Expected dependency context before task")
		}

		// Dependencies should be included
		if !strings.Contains(prompt, "dep-123") {
			t.Error("Expected prompt to contain dependency task ID")
		}
		if !strings.Contains(prompt, "Implemented the base parser") {
			t.Error("Expected prompt to contain dependency summary")
		}
	})
}
