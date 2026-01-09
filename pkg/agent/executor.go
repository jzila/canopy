package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/john/canopy/pkg/beads"
	"github.com/john/canopy/pkg/sandbox"
)

// DefaultTimeout is the default execution timeout for agents
const DefaultTimeout = 10 * time.Minute

// Result holds the execution result from an agent
type Result struct {
	TaskID   string
	Success  bool
	Output   *ClaudeOutput
	Stdout   string
	Stderr   string
	ExitCode int
	Changes  []sandbox.FileChange
	Duration time.Duration
	Error    string
}

// ClaudeOutput represents the JSON output from claude --print --output-format json
type ClaudeOutput struct {
	SessionID         string    `json:"session_id"`
	CostUSD           float64   `json:"cost_usd"`
	TotalInputTokens  int       `json:"total_input_tokens"`
	TotalOutputTokens int       `json:"total_output_tokens"`
	Messages          []Message `json:"messages"`
}

// Message represents a conversation message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Config holds agent configuration
type Config struct {
	ClaudePath string
	Timeout    time.Duration
	Verbose    bool
}

// NewConfig creates a default agent config
func NewConfig() *Config {
	claudePath, _ := exec.LookPath("claude")
	if claudePath == "" {
		claudePath = "claude"
	}

	return &Config{
		ClaudePath: claudePath,
		Timeout:    DefaultTimeout,
	}
}

// Executor runs Claude CLI agents in sandboxed environments
type Executor struct {
	config *Config
}

// NewExecutor creates a new agent executor
func NewExecutor(config *Config) *Executor {
	if config == nil {
		config = NewConfig()
	}
	return &Executor{config: config}
}

// Execute runs an agent for the given task in the provided sandbox
func (e *Executor) Execute(ctx context.Context, task *beads.Task, overlay *sandbox.Overlay) *Result {
	start := time.Now()

	result := &Result{
		TaskID: task.ID,
	}

	// Build the prompt from task title and description
	prompt := task.Title
	if task.Description != "" {
		prompt = fmt.Sprintf("%s\n\n%s", task.Title, task.Description)
	}

	// Build command arguments
	args := []string{
		"--print",
		"--output-format", "json",
		"--dangerously-skip-permissions", // Safe in sandbox
		prompt,
	}

	// Create command with timeout
	timeout := e.config.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, e.config.ClaudePath, args...)
	cmd.Dir = overlay.MergedDir

	// Set up environment
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "HOME="+overlay.MergedDir)

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute
	err := cmd.Run()
	result.Duration = time.Since(start)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	// Parse JSON output
	if stdout.Len() > 0 {
		var output ClaudeOutput
		if jsonErr := json.Unmarshal(stdout.Bytes(), &output); jsonErr == nil {
			result.Output = &output
		}
	}

	// Get file changes from overlay
	changes, _ := overlay.GetChanges()
	result.Changes = changes

	// Determine success
	if err != nil {
		result.Success = false
		if ctx.Err() == context.DeadlineExceeded {
			result.Error = "execution timed out"
		} else {
			result.Error = err.Error()
		}
	} else {
		result.Success = result.ExitCode == 0
		if !result.Success {
			result.Error = fmt.Sprintf("exit code %d", result.ExitCode)
		}
	}

	return result
}
