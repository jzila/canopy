package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/metrics"
	"github.com/jzila/canopy/pkg/sandbox"
)

// DefaultTimeout is the default execution timeout for agents
const DefaultTimeout = 10 * time.Minute

// processGroupGracePeriod is the time to wait for processes to exit after SIGTERM
// before sending SIGKILL
const processGroupGracePeriod = 3 * time.Second

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
	ClaudePath    string
	Timeout       time.Duration
	Verbose       bool
	UseBwrap      bool                   // Use bubblewrap sandbox for isolation (auto-detected if not set)
	SandboxConfig *sandbox.SandboxConfig // Sandbox configuration from .canopy/sandbox.toml
	UserPrompt    string                 // User-provided prompt instructions (takes precedence over bead description)
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

// LiveFeedCallback is the type for live feed event callbacks
type LiveFeedCallback func(taskID string, event *LiveFeedEvent)

// Execute runs an agent for the given task in the provided sandbox
// The optional liveFeedCallback receives streaming events during execution.
func (e *Executor) Execute(ctx context.Context, task *beads.Task, overlay *sandbox.Overlay, deps []DependencyContext, liveFeedCallback LiveFeedCallback) *Result {
	start := time.Now()

	result := &Result{
		TaskID: task.ID,
	}

	// Pass verbose flag to overlay for debug logging
	overlay.Verbose = e.config.Verbose

	// Record base commit if this is a git repo
	var baseCommit string
	if overlay.HasGitRepo() {
		var err error
		baseCommit, err = overlay.GetBaseCommit()
		if err != nil && e.config.Verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to get base commit: %v\n", err)
		}
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

	// Determine timeout: per-task > sandbox config > executor config > default
	timeout := task.GetTimeout()
	if timeout <= 0 && e.config.SandboxConfig != nil {
		timeout = e.config.SandboxConfig.GetTimeout()
	}
	if timeout <= 0 {
		timeout = e.config.Timeout
	}
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

	// Set Go cache environment variables if sandbox has cache mounts
	// This ensures Go uses shared caches instead of polluting workspace
	env = addGoCacheEnv(env, e.config.SandboxConfig)

	// Build command - use bwrap sandbox if available and enabled
	// Note: We don't use exec.CommandContext because it sends SIGKILL immediately
	// on context cancellation. Instead, we handle cancellation ourselves to enable
	// graceful shutdown (SIGTERM, wait, then SIGKILL to the process group).
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
		cmd = exec.Command(bwrapCmd.Path, bwrapCmd.Args[1:]...)
		cmd.Dir = bwrapCmd.Dir
		cmd.Env = bwrapCmd.Env
	} else {
		cmd = exec.Command(e.config.ClaudePath, args...)
		cmd.Dir = overlay.MergedDir
		cmd.Env = env
	}

	// Apply resource limits (process groups, death signals) on supported platforms
	// This is a no-op on non-Linux platforms
	// When using bwrap, it handles process isolation internally
	if !useBwrap {
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

	// Watch for context cancellation to gracefully terminate the process group
	// This runs in a goroutine and will clean up when context is cancelled
	// (either due to timeout or manual cancellation)
	processDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// Context cancelled - gracefully terminate the process group
			if cmd.Process != nil {
				pid := cmd.Process.Pid
				if pid > 0 {
					// Use our graceful termination: SIGTERM, wait, then SIGKILL
					killProcessGroup(pid, processGroupGracePeriod)
				}
			}
		case <-processDone:
			// Process exited normally, nothing to do
		}
	}()

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
			if err := json.Unmarshal(line, &result); err != nil {
				// Log parse errors - these indicate a mismatch between expected and actual format
				if e.config.Verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to parse result event: %v\n", err)
				}
			} else {
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
		if liveFeedCallback != nil {
			if liveEvent := FilterForLiveFeed(&event); liveEvent != nil {
				liveFeedCallback(task.ID, liveEvent)
			}
		}
	}

	// Wait for command to complete
	err = cmd.Wait()

	// Signal that process has exited (stops the context cancellation goroutine)
	close(processDone)

	result.Duration = time.Since(start)
	result.Stderr = stderr.String()

	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	// Convert stream result to ClaudeOutput
	// Note: finalResult will be nil if:
	// - Agent was killed/timed out before emitting result event
	// - Claude CLI crashed or exited abnormally
	// - JSON parsing of the result event failed
	// In these cases, token/cost metrics will be zero.
	if finalResult == nil && e.config.Verbose {
		fmt.Fprintf(os.Stderr, "warning: no result event received from claude CLI, token/cost metrics will be zero\n")
	}
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
	changes, err := overlay.GetChanges()
	if err != nil && e.config.Verbose {
		fmt.Fprintf(os.Stderr, "warning: failed to get overlay changes: %v\n", err)
	}
	result.Changes = changes

	// Extract git commits if this is a git repo
	if baseCommit != "" {
		gitState, err := overlay.ExtractNewCommits(baseCommit)
		if err != nil && e.config.Verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to extract git commits: %v\n", err)
		}
		result.GitState = gitState
	}

	// Set overlay for merge processing
	result.Overlay = overlay

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

	// Record task duration metric
	status := "success"
	if !result.Success {
		status = "failure"
	}
	metrics.RecordTaskDuration(result.Duration.Seconds(), status)

	return result
}

// buildPrompt constructs the prompt with task info, dependency context, and user instructions.
// User instructions take precedence over bead/task description.
func (e *Executor) buildPrompt(task *beads.Task, deps []DependencyContext) string {
	var parts []string

	// Add user instructions at the top if present - these take precedence
	if e.config.UserPrompt != "" {
		parts = append(parts, "## User Instructions (PRIORITY - follow these over task description)\n")
		parts = append(parts, e.config.UserPrompt)
		parts = append(parts, "\n---\n")
	}

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

	// Add reminder about user instructions if they were provided
	if e.config.UserPrompt != "" {
		parts = append(parts, "\n---\n")
		parts = append(parts, "**IMPORTANT**: Follow the User Instructions above. Stop when you reach the boundaries specified by the user, even if the task description suggests doing more.")
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
	"GOMODCACHE=", // Go module cache (set by sandbox for shared cache)
	"GOCACHE=",    // Go build cache (set by sandbox for shared cache)
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

// addGoCacheEnv adds GOMODCACHE and GOCACHE environment variables if sandbox
// config has corresponding cache mounts. This ensures Go uses shared caches
// instead of creating workspace-local go/pkg/mod/ directories.
func addGoCacheEnv(env []string, sandboxCfg *sandbox.SandboxConfig) []string {
	if sandboxCfg == nil {
		return env
	}

	cacheMounts := sandboxCfg.GetAllCacheMounts()
	for _, mount := range cacheMounts {
		// Check for Go module cache (~/go/pkg/mod or similar ending in /pkg/mod)
		if strings.HasSuffix(mount, "/pkg/mod") {
			env = append(env, "GOMODCACHE="+mount)
		}
		// Check for Go build cache (~/.cache/go-build)
		if strings.HasSuffix(mount, "/go-build") {
			env = append(env, "GOCACHE="+mount)
		}
	}

	return env
}
