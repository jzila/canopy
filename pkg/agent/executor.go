package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
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
	GitState *sandbox.GitState // Git commits made by worker
	Duration time.Duration
	Error    string
}

// DependencyContext holds outputs from upstream tasks
type DependencyContext struct {
	TaskID  string
	Summary string // Brief summary for prompt
	Output  string // Full output content
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
	UseBwrap   bool // Use bubblewrap sandbox for isolation (auto-detected if not set)
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
	// Ensure ClaudePath is set
	if config.ClaudePath == "" {
		claudePath, _ := exec.LookPath("claude")
		if claudePath != "" {
			config.ClaudePath = claudePath
		} else {
			config.ClaudePath = "claude"
		}
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultTimeout
	}
	return &Executor{config: config}
}

// Execute runs an agent for the given task in the provided sandbox
func (e *Executor) Execute(ctx context.Context, task *beads.Task, overlay *sandbox.Overlay, deps []DependencyContext) *Result {
	start := time.Now()

	result := &Result{
		TaskID: task.ID,
	}

	// Record base commit if this is a git repo
	var baseCommit string
	if overlay.HasGitRepo() {
		baseCommit, _ = overlay.GetBaseCommit()
	}

	// Write dependency context to sandbox
	if len(deps) > 0 {
		contextMap := make(map[string]string)
		for _, dep := range deps {
			contextMap[dep.TaskID] = dep.Output
		}
		if err := overlay.WriteContextFile(contextMap); err != nil && e.config.Verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to write context file: %v\n", err)
		}
	}

	// Build the prompt from task title, description, and dependency context
	prompt := e.buildPrompt(task, deps)

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

	// Set up filtered environment
	env := filterEnvironment(os.Environ())
	env = append(env, "HOME="+overlay.MergedDir)

	// Build command - use bwrap sandbox if available and enabled
	var cmd *exec.Cmd
	useBwrap := e.config.UseBwrap && sandbox.BwrapAvailable()

	if useBwrap {
		bwrapCmd, err := sandbox.BuildBwrapCommand(&sandbox.BwrapConfig{
			MergedDir:      overlay.MergedDir,
			Command:        e.config.ClaudePath,
			Args:           args,
			Env:            env,
			MaxMemoryBytes: 4 << 30, // 4GB
			MaxProcesses:   100,
			MaxOpenFiles:   1024,
		})
		if err != nil {
			result.Error = fmt.Sprintf("failed to build bwrap command: %v", err)
			return result
		}
		// Wrap with context for timeout support
		cmd = exec.CommandContext(ctx, bwrapCmd.Path, bwrapCmd.Args[1:]...)
		cmd.Dir = bwrapCmd.Dir
		cmd.Env = bwrapCmd.Env
	} else {
		cmd = exec.CommandContext(ctx, e.config.ClaudePath, args...)
		cmd.Dir = overlay.MergedDir
		cmd.Env = env
	}

	// Apply resource limits on Linux (when not using bwrap)
	if runtime.GOOS == "linux" && !useBwrap {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Pdeathsig: syscall.SIGKILL, // Kill agent if parent dies
		}
		setResourceLimits(cmd)
	}

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

	// Extract git commits if this is a git repo
	if baseCommit != "" {
		gitState, err := overlay.ExtractNewCommits(baseCommit)
		if err != nil && e.config.Verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to extract git commits: %v\n", err)
		}
		result.GitState = gitState
	}

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

// buildPrompt constructs the prompt with task info and dependency context
func (e *Executor) buildPrompt(task *beads.Task, deps []DependencyContext) string {
	var parts []string

	// Add dependency context if present
	if len(deps) > 0 {
		parts = append(parts, "## Context from upstream tasks\n")
		for _, dep := range deps {
			if dep.Summary != "" {
				parts = append(parts, fmt.Sprintf("### Task %s\n%s\n", dep.TaskID, dep.Summary))
			}
		}
		parts = append(parts, "Full outputs are available in .canopy/dep-<task-id>.txt files.\n")
		parts = append(parts, "---\n")
	}

	// Add task title
	parts = append(parts, fmt.Sprintf("## Task: %s\n", task.Title))

	// Add description if present
	if task.Description != "" {
		parts = append(parts, task.Description)
	}

	return strings.Join(parts, "\n")
}

// allowedEnvPrefixes defines environment variable prefixes that are safe to pass to agents
var allowedEnvPrefixes = []string{
	"ANTHROPIC_", // API key and settings
	"PATH=",      // Required for finding executables
	"LANG=",      // Locale
	"LC_",        // Locale variants
	"TERM=",      // Terminal type
	"TMPDIR=",    // Temp directory
	"TZ=",        // Timezone
}

// filterEnvironment returns only safe environment variables for agent execution
func filterEnvironment(env []string) []string {
	var filtered []string
	for _, e := range env {
		for _, prefix := range allowedEnvPrefixes {
			if strings.HasPrefix(e, prefix) {
				filtered = append(filtered, e)
				break
			}
		}
	}
	return filtered
}
