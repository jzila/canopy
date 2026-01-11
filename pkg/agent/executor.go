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

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/sandbox"
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
	Overlay  *sandbox.Overlay  // Overlay sandbox (must be cleaned up after merge)
}

// DependencyContext holds outputs from upstream tasks
type DependencyContext struct {
	TaskID  string
	Summary string // Brief summary for prompt
	Output  string // Full output content
}

// ClaudeOutput represents the JSON output from claude --print --output-format json
type ClaudeOutput struct {
	SessionID                string                    `json:"session_id"`
	CostUSD                  float64                   `json:"cost_usd"`
	TotalInputTokens         int                       `json:"total_input_tokens"`
	TotalOutputTokens        int                       `json:"total_output_tokens"`
	CacheCreationInputTokens int                       `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int                       `json:"cache_read_input_tokens"`
	DurationMS               int64                     `json:"duration_ms"`
	DurationAPIMS            int64                     `json:"duration_api_ms"`
	NumTurns                 int                       `json:"num_turns"`
	ResultMessage            string                    `json:"result_message"`
	ModelUsage               map[string]ModelUsageData `json:"model_usage"`
	Messages                 []Message                 `json:"messages"`
}

// ModelUsageData represents per-model usage statistics
type ModelUsageData struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	CostUSD                  float64 `json:"cost_usd"`
}

// Message represents a conversation message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Config holds agent configuration
type Config struct {
	ClaudePath      string
	Timeout         time.Duration
	Verbose         bool
	UseBwrap        bool                                      // Use bubblewrap sandbox for isolation (auto-detected if not set)
	SandboxConfig   *sandbox.SandboxConfig                    // Sandbox configuration from .canopy/sandbox.toml
	OnLiveFeedEvent func(taskID string, event *LiveFeedEvent) // Callback for live streaming events
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

// SetLiveFeedCallback sets the callback for live feed events
func (e *Executor) SetLiveFeedCallback(callback func(taskID string, event *LiveFeedEvent)) {
	e.config.OnLiveFeedEvent = callback
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
		"--output-format", "stream-json",
		"--verbose", // Required for stream-json
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
	// Block git SSH operations - SSH ignores $HOME and uses getpwuid() for ~/.ssh
	// This is a belt-and-suspenders approach; gitconfig also has core.sshCommand=false
	// Using "false" works because git uses shell to execute the command
	env = append(env, "GIT_SSH_COMMAND=false")

	// Build command - use bwrap sandbox if available and enabled
	var cmd *exec.Cmd
	useBwrap := e.config.UseBwrap && sandbox.BwrapAvailable()

	if useBwrap {
		bwrapCfg := &sandbox.BwrapConfig{
			MergedDir:      overlay.MergedDir,
			Command:        e.config.ClaudePath,
			Args:           args,
			Env:            env,
			MaxMemoryBytes: 4 << 30, // 4GB (default, can be overridden by sandbox config)
			MaxProcesses:   100,
			MaxOpenFiles:   1024,
			SandboxConfig:  e.config.SandboxConfig, // Pass sandbox config for path bindings
		}
		bwrapCmd, err := sandbox.BuildBwrapCommand(bwrapCfg)
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

	// Set up streaming stdout/stderr capture
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// Create stdout pipe for streaming
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		result.Error = fmt.Sprintf("failed to create stdout pipe: %v", err)
		return result
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		result.Error = fmt.Sprintf("failed to start command: %v", err)
		result.ExitCode = -1
		return result
	}

	// Parse streaming output and collect final result
	var finalResult *ClaudeStreamResult
	parser := NewStreamParser(stdoutPipe)
	for parser.scanner.Scan() {
		line := parser.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Try to parse as generic event first for type checking
		var eventType struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &eventType); err != nil {
			continue
		}

		// Handle result events specially
		if eventType.Type == "result" {
			var result ClaudeStreamResult
			if err := json.Unmarshal(line, &result); err == nil {
				finalResult = &result
			}
			continue
		}

		// Parse as regular stream event for live feed
		var event StreamEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		// Forward live feed events if callback is set
		if e.config.OnLiveFeedEvent != nil {
			if liveEvent := FilterForLiveFeed(&event); liveEvent != nil {
				e.config.OnLiveFeedEvent(task.ID, liveEvent)
			}
		}
	}

	// Wait for command to complete
	err = cmd.Wait()
	result.Duration = time.Since(start)
	result.Stderr = stderr.String()

	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	// Convert stream result to ClaudeOutput
	if finalResult != nil {
		result.Output = &ClaudeOutput{
			SessionID:                finalResult.SessionID,
			CostUSD:                  finalResult.TotalCostUSD,
			TotalInputTokens:         finalResult.Usage.InputTokens,
			TotalOutputTokens:        finalResult.Usage.OutputTokens,
			CacheCreationInputTokens: finalResult.Usage.CacheCreationInputToken,
			CacheReadInputTokens:     finalResult.Usage.CacheReadInputTokens,
			DurationMS:               finalResult.DurationMS,
			DurationAPIMS:            finalResult.DurationAPIMS,
			NumTurns:                 finalResult.NumTurns,
			ResultMessage:            finalResult.Result,
		}

		// Convert model usage
		if finalResult.ModelUsage != nil {
			result.Output.ModelUsage = make(map[string]ModelUsageData)
			for model, usage := range finalResult.ModelUsage {
				result.Output.ModelUsage[model] = ModelUsageData{
					InputTokens:              usage.InputTokens,
					OutputTokens:             usage.OutputTokens,
					CacheReadInputTokens:     usage.CacheReadInputTokens,
					CacheCreationInputTokens: usage.CacheCreationInputTokens,
					CostUSD:                  usage.CostUSD,
				}
			}
		}

		// Extract stdout from final result for compatibility
		result.Stdout = finalResult.Result
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
