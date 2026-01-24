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
	TaskID       string
	Success      bool
	Output       *ClaudeOutput
	Stdout       string
	Stderr       string
	ExitCode     int
	Changes      []sandbox.FileChange
	GitState     *sandbox.GitState // Git commits made by worker
	Duration     time.Duration
	Error        string
	Overlay      *sandbox.Overlay // Overlay sandbox (must be cleaned up after merge)
	InputBlocked bool             // True if agent paused waiting for user input
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
	// This is critical for tracking git commits made by agents
	var baseCommit string
	hasGitRepo := overlay.HasGitRepo()
	if e.config.Verbose {
		fmt.Fprintf(os.Stderr, "[executor] HasGitRepo=%v, MergedDir=%s\n", hasGitRepo, overlay.MergedDir)
	}
	if hasGitRepo {
		var err error
		baseCommit, err = overlay.GetBaseCommit()
		if err != nil {
			// Always log git errors since they cause commits to not be tracked
			fmt.Fprintf(os.Stderr, "warning: failed to get base commit (commits won't be tracked): %v\n", err)
		} else if e.config.Verbose {
			fmt.Fprintf(os.Stderr, "[executor] baseCommit=%s\n", baseCommit)
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

	// Apply resource limits (process groups, death signals) on supported platforms.
	// This is a no-op on non-Linux platforms.
	// Process group isolation (Setpgid) is needed for killProcessGroup to work
	// on context cancellation, regardless of whether we use bwrap.
	setResourceLimits(cmd)

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
					_ = killProcessGroup(pid, processGroupGracePeriod)
				}
			}
		case <-processDone:
			// Process exited normally, nothing to do
		}
	}()

	// Parse streaming output and collect final result
	var finalResult *ClaudeStreamResult
	var inputBlockedTool string // Set if interactive tool detected
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

		// Check for interactive tools that require user input
		// Worker agents cannot proceed when these are invoked - fail immediately
		if toolName := CheckForInteractiveTool(&event); toolName != "" {
			inputBlockedTool = toolName
			if e.config.Verbose {
				fmt.Fprintf(os.Stderr, "[executor] detected interactive tool %s, terminating agent\n", toolName)
			}
			// Terminate the process - it cannot proceed without user input
			if cmd.Process != nil {
				pid := cmd.Process.Pid
				if pid > 0 {
					_ = killProcessGroup(pid, processGroupGracePeriod)
				}
			}
			break // Stop processing stream
		}

		// Forward live feed events if callback is set
		if liveFeedCallback != nil {
			if liveEvent := FilterForLiveFeed(&event); liveEvent != nil {
				liveFeedCallback(task.ID, liveEvent)
			}
		}
	}

	// Check for scanner errors (e.g., token too long, buffer overflow)
	// This is critical because scanner.Scan() returns false on both EOF and error,
	// and we need to distinguish between normal completion and failure.
	if scanErr := parser.Err(); scanErr != nil {
		// Log scanner errors - this indicates something went wrong with stream parsing
		fmt.Fprintf(os.Stderr, "warning: stream scanner error (token/cost metrics may be incomplete): %v\n", scanErr)
	}

	// Wait for command to complete (error handled via ProcessState.ExitCode below)
	_ = cmd.Wait()

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
	// - Scanner encountered an error (buffer overflow, token too long)
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
		if err != nil {
			// Always log extraction errors - they affect commit tracking
			fmt.Fprintf(os.Stderr, "warning: failed to extract git commits: %v\n", err)
		}
		result.GitState = gitState
		if e.config.Verbose && gitState != nil {
			fmt.Fprintf(os.Stderr, "[executor] extracted %d commits\n", len(gitState.NewCommits))
		}
	} else if hasGitRepo && e.config.Verbose {
		fmt.Fprintf(os.Stderr, "[executor] skipping commit extraction: no base commit\n")
	}

	// Set overlay for merge processing
	result.Overlay = overlay

	// Determine success
	// Priority: input-blocked > timeout > error > exit code
	if inputBlockedTool != "" {
		// Agent paused waiting for user input - this is a non-retryable failure
		result.Success = false
		result.InputBlocked = true
		sessionID := ""
		if result.Output != nil {
			sessionID = result.Output.SessionID
		}
		if sessionID != "" {
			result.Error = fmt.Sprintf("agent paused waiting for user input (%s tool); session %s", inputBlockedTool, sessionID)
		} else {
			result.Error = fmt.Sprintf("agent paused waiting for user input (%s tool)", inputBlockedTool)
		}
	} else if ctx.Err() == context.DeadlineExceeded {
		result.Success = false
		result.Error = "execution timed out"
	} else if err != nil {
		result.Success = false
		result.Error = err.Error()
	} else {
		result.Success = result.ExitCode == 0
		if !result.Success {
			result.Error = fmt.Sprintf("exit code %d", result.ExitCode)
		}
	}

	// Record task duration metric
	status := "success"
	if !result.Success {
		if result.InputBlocked {
			status = "input_blocked"
		} else {
			status = "failure"
		}
	}
	metrics.RecordTaskDuration(result.Duration.Seconds(), status)

	return result
}

// ExecuteResume continues an interrupted agent using claude --resume.
// This is used to resume agents after daemon restart when a valid session ID exists.
// The overlay should already be remounted before calling this method.
func (e *Executor) ExecuteResume(ctx context.Context, task *beads.Task, overlay *sandbox.Overlay, sessionID string, liveFeedCallback LiveFeedCallback) *Result {
	start := time.Now()

	result := &Result{
		TaskID: task.ID,
	}

	// Pass verbose flag to overlay for debug logging
	overlay.Verbose = e.config.Verbose

	// Record base commit if this is a git repo
	var baseCommit string
	hasGitRepo := overlay.HasGitRepo()
	if e.config.Verbose {
		fmt.Fprintf(os.Stderr, "[executor] Resume: HasGitRepo=%v, MergedDir=%s, SessionID=%s\n", hasGitRepo, overlay.MergedDir, sessionID)
	}
	if hasGitRepo {
		var err error
		baseCommit, err = overlay.GetBaseCommit()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to get base commit (commits won't be tracked): %v\n", err)
		} else if e.config.Verbose {
			fmt.Fprintf(os.Stderr, "[executor] baseCommit=%s\n", baseCommit)
		}
	}

	// Build command arguments for resume - no prompt, just --resume
	args := []string{
		"--resume", sessionID,
		"--print",
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
	}

	// Determine timeout
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
	env = append(env, "GIT_SSH_COMMAND=false")

	// Set Go cache environment variables if sandbox has cache mounts
	env = addGoCacheEnv(env, e.config.SandboxConfig)

	// Build command - use bwrap sandbox if available and enabled
	var cmd *exec.Cmd
	useBwrap := e.config.UseBwrap && sandbox.BwrapAvailable()

	if useBwrap {
		bwrapCfg := &sandbox.BwrapConfig{
			MergedDir:      overlay.MergedDir,
			Command:        e.config.ClaudePath,
			Args:           args,
			Env:            env,
			MaxMemoryBytes: 4 << 30,
			MaxProcesses:   100,
			MaxOpenFiles:   1024,
			SandboxConfig:  e.config.SandboxConfig,
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

	// Apply resource limits on non-bwrap execution
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

	// Watch for context cancellation
	processDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				pid := cmd.Process.Pid
				if pid > 0 {
					_ = killProcessGroup(pid, processGroupGracePeriod)
				}
			}
		case <-processDone:
		}
	}()

	// Parse streaming output
	var finalResult *ClaudeStreamResult
	var inputBlockedTool string // Set if interactive tool detected
	parser := NewStreamParser(stdoutPipe)
	for parser.scanner.Scan() {
		line := parser.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var eventType struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &eventType); err != nil {
			continue
		}

		if eventType.Type == "result" {
			var result ClaudeStreamResult
			if err := json.Unmarshal(line, &result); err != nil {
				if e.config.Verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to parse result event: %v\n", err)
				}
			} else {
				finalResult = &result
			}
			continue
		}

		var event StreamEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		// Check for interactive tools that require user input
		// Worker agents cannot proceed when these are invoked - fail immediately
		if toolName := CheckForInteractiveTool(&event); toolName != "" {
			inputBlockedTool = toolName
			if e.config.Verbose {
				fmt.Fprintf(os.Stderr, "[executor] detected interactive tool %s, terminating agent\n", toolName)
			}
			// Terminate the process - it cannot proceed without user input
			if cmd.Process != nil {
				pid := cmd.Process.Pid
				if pid > 0 {
					_ = killProcessGroup(pid, processGroupGracePeriod)
				}
			}
			break // Stop processing stream
		}

		if liveFeedCallback != nil {
			if liveEvent := FilterForLiveFeed(&event); liveEvent != nil {
				liveFeedCallback(task.ID, liveEvent)
			}
		}
	}

	if scanErr := parser.Err(); scanErr != nil {
		fmt.Fprintf(os.Stderr, "warning: stream scanner error (token/cost metrics may be incomplete): %v\n", scanErr)
	}

	_ = cmd.Wait()
	close(processDone)

	result.Duration = time.Since(start)
	result.Stderr = stderr.String()

	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	// Convert stream result to ClaudeOutput
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

		result.Stdout = finalResult.Result
	}

	// Get file changes from overlay
	changes, err := overlay.GetChanges()
	if err != nil && e.config.Verbose {
		fmt.Fprintf(os.Stderr, "warning: failed to get overlay changes: %v\n", err)
	}
	result.Changes = changes

	// Extract git commits
	if baseCommit != "" {
		gitState, err := overlay.ExtractNewCommits(baseCommit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to extract git commits: %v\n", err)
		}
		result.GitState = gitState
		if e.config.Verbose && gitState != nil {
			fmt.Fprintf(os.Stderr, "[executor] extracted %d commits\n", len(gitState.NewCommits))
		}
	}

	// Set overlay for merge processing
	result.Overlay = overlay

	// Determine success
	// Priority: input-blocked > timeout > error > exit code
	if inputBlockedTool != "" {
		// Agent paused waiting for user input - this is a non-retryable failure
		result.Success = false
		result.InputBlocked = true
		resumeSessionID := sessionID // Use the session ID we were resuming
		if result.Output != nil && result.Output.SessionID != "" {
			resumeSessionID = result.Output.SessionID
		}
		if resumeSessionID != "" {
			result.Error = fmt.Sprintf("agent paused waiting for user input (%s tool); session %s", inputBlockedTool, resumeSessionID)
		} else {
			result.Error = fmt.Sprintf("agent paused waiting for user input (%s tool)", inputBlockedTool)
		}
	} else if ctx.Err() == context.DeadlineExceeded {
		result.Success = false
		result.Error = "execution timed out"
	} else if err != nil {
		result.Success = false
		result.Error = err.Error()
	} else {
		result.Success = result.ExitCode == 0
		if !result.Success {
			result.Error = fmt.Sprintf("exit code %d", result.ExitCode)
		}
	}

	// Record task duration metric
	status := "success"
	if !result.Success {
		if result.InputBlocked {
			status = "input_blocked"
		} else {
			status = "failure"
		}
	}
	metrics.RecordTaskDuration(result.Duration.Seconds(), status)

	return result
}

// buildPrompt constructs the prompt with task info and dependency context.
func (e *Executor) buildPrompt(task *beads.Task, deps []DependencyContext) string {
	var parts []string

	// Add system prompt for autonomous operation
	parts = append(parts, WorkerSystemPrompt)

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
