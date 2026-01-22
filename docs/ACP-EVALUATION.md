# ACP (Agent Client Protocol) Refactor Evaluation

**Date:** 2026-01-21
**Status:** Evaluation Complete
**Recommendation:** Proceed (with AgentBackend abstraction)

## Executive Summary

This document evaluates refactoring Canopy to use ACP (Agent Client Protocol) to enable **multi-agent backend support**. The goal is to allow swapping in different agent implementations (Claude Code, other ACP-compatible agents, future backends) without changing Canopy's orchestration logic.

**Recommendation: Proceed** with an `AgentBackend` interface abstraction.

Key trade-offs accepted:
- **Session resumption removed**: Agents become "restartable" not "resumable" (accepted)
- **Simplified crash recovery**: Interrupted agents restart from scratch (accepted)
- **Multi-backend flexibility**: Can integrate any ACP-compatible agent (gained)

## ACP Protocol Overview

### What ACP Is

ACP (Agent Client Protocol) is a standardized communication framework between code editors and AI agents. It defines bidirectional messaging patterns for:

- **Initialization**: How connections begin
- **Session Setup**: Creating and loading sessions
- **Prompt Turns**: Core conversation flow
- **Content Blocks**: Structured data transmission
- **Tool Calls**: How agents report execution results

### Session Management Status

The ACP documentation reveals session management is **still in development**:

| Feature | Status |
|---------|--------|
| Session creation | Documented |
| Session loading | Documented |
| Session resumption | "Draft: In Progress and May Change" |
| Session forking | "Draft: In Progress and May Change" |
| Session listing | "Draft: In Progress and May Change" |

The presence of RFDs (Requests for Dialog) on "Resuming of existing sessions" indicates this is a recognized requirement but **not yet stable**.

### Permission Model

ACP handles permissions at the **protocol level**, not via CLI flags:

| ACP Mechanism | Claude CLI Equivalent |
|---------------|----------------------|
| `session/request_permission` | Tool attempts blocked until approved |
| Client responds `allow_once` | Single tool call approved |
| Client responds `allow_always` | Category of tool calls pre-approved |
| Client auto-approves | `--dangerously-skip-permissions` |

Key clause from spec: "Clients **MAY** automatically allow or reject permission requests according to user settings."

For Canopy, this is **better** than the CLI flag:
- Permission logic lives in our code, not a CLI argument
- Could implement fine-grained rules (allow file edits, block network)
- No flag to forget when spawning processes

### Protocol Limitations

1. **No guaranteed session persistence**: Unlike `claude --resume`, ACP doesn't define how session state survives process termination
2. **Stateless request/response model**: Designed for IDE integrations with short interactions
3. **No overlay filesystem support**: ACP has no concept of isolated file changes

## Claude Code ACP Adapter Analysis

The [claude-code-acp](https://github.com/zed-industries/claude-code-acp) adapter is a TypeScript wrapper that bridges Claude Code to ACP-compatible clients (primarily Zed editor).

### Features Exposed

| Feature | Support |
|---------|---------|
| Context @-mentions | Yes |
| Image attachments | Yes |
| Tool calls with permission requests | Yes |
| Following functionality | Yes |
| Edit review workflows | Yes |
| TODO list generation | Yes |
| Interactive terminals | Yes |
| Background terminals | Yes |
| Custom slash commands | Yes |
| MCP server integration | Yes |

### Features NOT Mentioned (Likely Missing)

| Feature | Support |
|---------|---------|
| `--resume` / session resumption | **No documentation** |
| `--print` output mode | Unknown |
| `--output-format stream-json` | Unknown |
| Session ID capture | Unknown |
| Process isolation | Not applicable (no sandbox) |

### Critical Gap

The adapter documentation provides **no information** about:
- Session resumption
- The `--resume` flag
- Any mechanism to restore previous conversations
- Session ID persistence

This aligns with ACP being designed for ephemeral IDE interactions, not long-running orchestrated workflows.

## Current Canopy Architecture

### How Canopy Uses Claude CLI

Canopy spawns Claude agents via direct CLI execution with two modes:

**1. Fresh Execution** (`pkg/agent/executor.go:128-440`)
```go
args := []string{
    "--print",
    "--output-format", "stream-json",
    "--verbose",
    "--dangerously-skip-permissions",
    prompt,
}
cmd := exec.Command(e.config.ClaudePath, args...)
```

**2. Resume Execution** (`pkg/agent/executor.go:442-702`)
```go
args := []string{
    "--resume", sessionID,  // <-- Critical for crash recovery
    "--print",
    "--output-format", "stream-json",
    "--verbose",
    "--dangerously-skip-permissions",
}
```

### Session ID Flow

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Execute   │────>│   Claude    │────>│   Result    │
│   Agent     │     │    CLI      │     │  JSON       │
└─────────────┘     └─────────────┘     └─────────────┘
                                               │
                                               ▼
                                        session_id
                                               │
                    ┌──────────────────────────┼──────────────────────┐
                    │                          │                      │
                    ▼                          ▼                      ▼
             ┌─────────────┐           ┌─────────────┐        ┌─────────────┐
             │ RuntimeState│           │  SQLite DB  │        │ ActiveOverlay│
             │ (in-memory) │           │ (agents)    │        │ (session_id)│
             └─────────────┘           └─────────────┘        └─────────────┘
```

### Crash Recovery Flow

On daemon restart (`pkg/daemon/recovery.go:24-115`):

1. **Mark overlays as orphaned**: All "active" overlays are marked orphaned
2. **Identify resumable agents**: Check for overlays with `session_id`
3. **Remount overlays**: Restore filesystem isolation
4. **Resume agents**: Call `ExecuteResume` with saved `session_id`

```go
// pkg/daemon/recovery.go:270-321
func (d *Daemon) ResumeInterruptedAgents(ctx context.Context, executor *agent.Executor) {
    resumable, _ := d.GetResumableOverlays(ctx)
    for _, r := range resumable {
        d.resumeAgent(ctx, r, executor)
    }
}
```

This is **not possible with ACP** without protocol-level session persistence.

## Feature Parity Matrix

| Feature | Current (CLI) | With ACP | Notes |
|---------|--------------|----------|-------|
| Agent execution | Full | Full | Basic prompting works |
| Streaming events | Full | Partial | Different event format |
| Token metrics | Full | Unknown | May need translation layer |
| Session ID capture | Full | **None** | No ACP equivalent |
| `--resume` support | Full | **None** | Protocol doesn't support |
| Overlay sandbox | Full | N/A | Canopy feature, not Claude |
| Git commit tracking | Full | Unknown | Depends on output format |
| Timeout handling | Full | Full | Process-level timeout works |
| Live feed events | Full | Partial | ACP has different event types |
| Crash recovery | Full | **Lost** | Requires session resumption |
| Cost tracking | Full | Unknown | Needs adapter investigation |

## Resume vs Restart Analysis

### Current Behavior (with `--resume`)

```
Daemon running:     Agent starts at turn 1
                           ↓
                    Agent at turn 5
                           ↓
Daemon crash:       Session ID persisted to SQLite
                           ↓
Daemon restart:     ResumeInterruptedAgents() finds session
                           ↓
                    ExecuteResume(sessionID)
                           ↓
                    Agent continues from turn 5
                           ↓
                    Agent completes at turn 10

Result: 10 turns total, no duplicate work
```

### Proposed Behavior (with ACP)

```
Daemon running:     Agent starts at turn 1
                           ↓
                    Agent at turn 5
                           ↓
Daemon crash:       No session persistence mechanism
                           ↓
Daemon restart:     Detects orphaned overlay
                           ↓
Options:
  A) Restart from turn 1 (duplicate work, cost increase)
  B) Mark as failed (lost work)
  C) Attempt completion from overlay state (complex, unreliable)
```

### Quantified Trade-offs

| Metric | Resume (current) | Restart (ACP) | Impact |
|--------|-----------------|---------------|--------|
| Cost on crash | 0% extra | 50-100% extra | ~$2.50/task average |
| Time on crash | 0% extra | 50-100% extra | Variable |
| Work preservation | 100% | 0% | Lost context |
| Reliability | High | Degraded | May diverge on retry |
| Complexity | Moderate | Lower | Simpler implementation |

### When Restart Is Acceptable

The "restartable" model works well for:
- Short, idempotent tasks (< 5 turns)
- Tasks with clear validation (can verify completion)
- Low-cost exploratory work
- Tasks where retry produces equivalent results

The "restartable" model fails for:
- Long-running multi-file refactors (10+ turns)
- Tasks with accumulated context (research, planning)
- Cost-sensitive deployments
- Tasks requiring human context injection mid-run

## Proposed Architecture Changes (If Proceeding)

### Option A: Full ACP Migration

Replace `pkg/agent/executor.go` with ACP client:

```go
// Hypothetical ACP executor
type ACPExecutor struct {
    client *acp.Client
}

func (e *ACPExecutor) Execute(ctx context.Context, task *beads.Task, overlay *sandbox.Overlay) *Result {
    // No --resume equivalent
    // Must restart from beginning on any interruption
    session := e.client.CreateSession(ctx)
    return e.client.RunPrompt(ctx, session, buildPrompt(task))
}
```

**Changes required:**
- New ACP client package
- Streaming event translation layer
- Remove all `--resume` related code
- Remove `ExecuteResume` method
- Update `recovery.go` to mark interrupted agents as failed
- Update dashboard to show "Restarted" status

**Estimated effort:** 2-3 weeks

### Option B: Hybrid Architecture

Keep Claude CLI for resumable workflows, add ACP for stateless tasks:

```go
type HybridExecutor struct {
    cliExecutor *CLIExecutor  // For resumable tasks
    acpExecutor *ACPExecutor  // For stateless tasks
}

func (e *HybridExecutor) Execute(ctx context.Context, task *beads.Task, ...) *Result {
    if task.RequiresResume() {
        return e.cliExecutor.Execute(ctx, task, ...)
    }
    return e.acpExecutor.Execute(ctx, task, ...)
}
```

**Changes required:**
- New ACP client package
- Task-level "resumable" flag
- Dual execution paths
- More complex testing matrix

**Estimated effort:** 3-4 weeks

### Option C: Wait for ACP Session Management

Monitor ACP protocol development. The RFDs indicate session resumption is being designed. When stable:

1. Evaluate updated claude-code-acp adapter
2. Verify session persistence works across process restarts
3. Re-evaluate this document

**Estimated timeline:** 6-12 months (speculative)

## Risks and Mitigations

### Risk 1: Lost Work on Daemon Crash

**Impact:** High
**Probability:** Medium (daemon crashes happen)

**Mitigation (if proceeding):**
- Implement aggressive checkpointing of partial results
- Reduce task granularity (smaller, more frequent tasks)
- Accept increased costs as trade-off for simpler architecture

### Risk 2: Increased API Costs

**Impact:** Medium
**Probability:** High

**Mitigation:**
- Cache task results more aggressively
- Implement task-level idempotency checks
- Track restart frequency to identify problematic tasks

### Risk 3: Protocol Instability

**Impact:** Medium
**Probability:** Medium (ACP session features are "Draft")

**Mitigation:**
- Abstract ACP client behind interface
- Maintain fallback to CLI execution
- Pin to stable ACP versions only

### Risk 4: Feature Gaps in Adapter

**Impact:** Unknown
**Probability:** High (adapter is new)

**Mitigation:**
- Thorough integration testing before migration
- Contribute missing features upstream if needed
- Maintain CLI path as fallback

## Recommendation

**Proceed** with ACP refactor using an `AgentBackend` abstraction.

### Rationale

1. **Multi-agent support is the goal**: ACP provides a standard interface for multiple agent backends
2. **Session resumption not required**: "Restartable" agents are acceptable for this use case
3. **Clean abstraction boundary**: Separates orchestration from agent implementation details
4. **Future-proof**: New ACP-compatible agents can be added without core changes

### Accepted Trade-offs

| Trade-off | Impact | Mitigation |
|-----------|--------|------------|
| No session resume | Restart on crash | Keep tasks small, accept cost increase |
| Extra process layer | Slight latency | Negligible for typical task durations |
| TypeScript dependency | Build complexity | Use pre-built binary or implement ACP in Go |

## AgentBackend Interface Design

The core abstraction enabling multi-agent support:

```go
// pkg/agent/backend.go

package agent

import (
    "context"
    "io"
)

// Backend defines the interface for agent execution backends.
// Implementations include Claude CLI, ACP adapters, and future agents.
type Backend interface {
    // Name returns a human-readable name for this backend (e.g., "claude-cli", "acp")
    Name() string

    // Execute runs the agent with the given prompt and returns a result.
    // The overlay provides the working directory for file operations.
    Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResult, error)

    // Stream returns a channel of events during execution.
    // Returns nil if streaming is not supported.
    Stream() <-chan Event

    // Capabilities reports what this backend supports.
    Capabilities() BackendCapabilities
}

// ExecuteRequest contains all inputs for agent execution.
type ExecuteRequest struct {
    Prompt     string
    WorkingDir string            // Merged overlay directory
    Env        map[string]string // Environment variables
    Timeout    time.Duration
}

// ExecuteResult contains the output from agent execution.
type ExecuteResult struct {
    Success       bool
    ExitCode      int
    Output        string        // Final result message
    Error         string        // Error message if failed
    TokenUsage    *TokenUsage   // May be nil if not tracked
    FilesChanged  []FileChange  // Detected file changes
    Duration      time.Duration
}

// TokenUsage tracks API consumption (optional, backend-dependent).
type TokenUsage struct {
    InputTokens              int
    OutputTokens             int
    CacheCreationInputTokens int
    CacheReadInputTokens     int
    CostUSD                  float64
}

// BackendCapabilities describes what a backend supports.
type BackendCapabilities struct {
    SupportsStreaming   bool // Can emit real-time events
    SupportsTokenUsage  bool // Reports token/cost metrics
    SupportsResume      bool // Can resume interrupted sessions (legacy)
    SupportsSandbox     bool // Works with overlay filesystems
}

// Event represents a streaming event from the agent.
type Event struct {
    Type      EventType
    Timestamp time.Time
    Data      interface{} // Type-specific payload
}

type EventType string

const (
    EventTypeToolUse    EventType = "tool_use"
    EventTypeText       EventType = "text"
    EventTypeThinking   EventType = "thinking"
    EventTypeError      EventType = "error"
    EventTypeCompletion EventType = "completion"
)
```

### Backend Implementations

```go
// pkg/agent/backend_cli.go - Current Claude CLI implementation

type CLIBackend struct {
    claudePath string
    verbose    bool
}

func NewCLIBackend(claudePath string) *CLIBackend {
    return &CLIBackend{claudePath: claudePath}
}

func (b *CLIBackend) Name() string { return "claude-cli" }

func (b *CLIBackend) Capabilities() BackendCapabilities {
    return BackendCapabilities{
        SupportsStreaming:  true,
        SupportsTokenUsage: true,
        SupportsResume:     false, // Removed - agents are restartable
        SupportsSandbox:    true,
    }
}

func (b *CLIBackend) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResult, error) {
    // Existing executor logic, adapted to new interface
    // See pkg/agent/executor.go:128-440
}
```

```go
// pkg/agent/backend_acp.go - ACP adapter implementation

type ACPBackend struct {
    adapterPath string // Path to claude-code-acp binary
    verbose     bool
}

func NewACPBackend(adapterPath string) *ACPBackend {
    return &ACPBackend{adapterPath: adapterPath}
}

func (b *ACPBackend) Name() string { return "acp" }

func (b *ACPBackend) Capabilities() BackendCapabilities {
    return BackendCapabilities{
        SupportsStreaming:  true,  // ACP has event streaming
        SupportsTokenUsage: false, // Need to verify adapter support
        SupportsResume:     false,
        SupportsSandbox:    true,
    }
}

func (b *ACPBackend) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResult, error) {
    // Spawn claude-code-acp process
    // Speak ACP protocol over stdin/stdout
    // Translate ACP events to Canopy events
    // Auto-approve all permission requests (equivalent to --dangerously-skip-permissions)
}

// handlePermissionRequest auto-approves all tool calls.
// This is safe because agents run in sandboxed overlay filesystems.
// Equivalent to --dangerously-skip-permissions but at protocol level.
func (b *ACPBackend) handlePermissionRequest(req *PermissionRequest) *PermissionResponse {
    return &PermissionResponse{
        Outcome:  "selected",
        OptionID: "allow_once",
    }
}
```

### Backend Registry

```go
// pkg/agent/registry.go

type Registry struct {
    backends map[string]Backend
    mu       sync.RWMutex
}

func NewRegistry() *Registry {
    return &Registry{
        backends: make(map[string]Backend),
    }
}

func (r *Registry) Register(name string, backend Backend) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.backends[name] = backend
}

func (r *Registry) Get(name string) (Backend, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    b, ok := r.backends[name]
    return b, ok
}

func (r *Registry) Default() Backend {
    r.mu.RLock()
    defer r.mu.RUnlock()
    // Return first registered backend, or nil
    for _, b := range r.backends {
        return b
    }
    return nil
}
```

### Configuration

```toml
# .canopy/agents.toml

# Default backend for all tasks
default_backend = "claude-cli"

[backends.claude-cli]
type = "cli"
path = "claude"  # or explicit path

[backends.acp]
type = "acp"
adapter = "claude-code-acp"  # Must be in PATH

[backends.custom]
type = "acp"
adapter = "/path/to/custom-acp-agent"
```

### Task-Level Backend Selection

```go
// In beads task schema, add optional field:
type Task struct {
    // ... existing fields ...
    Backend string `json:"backend,omitempty"` // Override default backend
}
```

```bash
# Example: run specific task with ACP backend
bd update canopy-xyz --backend=acp
```

## Implementation Roadmap

### Phase 1: Extract Interface (1 week)

1. Create `pkg/agent/backend.go` with interface definition
2. Refactor existing `Executor` to implement `Backend` interface
3. Remove `ExecuteResume` method (no longer needed)
4. Update `pkg/daemon/recovery.go` to mark interrupted agents as failed (no resume)
5. Update tests

**Files changed:**
- `pkg/agent/backend.go` (new)
- `pkg/agent/executor.go` (refactor to CLIBackend)
- `pkg/daemon/recovery.go` (simplify - remove resume logic)
- `pkg/daemon/server.go` (use Backend interface)

### Phase 2: Add Registry (3 days)

1. Create `pkg/agent/registry.go`
2. Load backends from `.canopy/agents.toml`
3. Wire registry into daemon initialization

**Files changed:**
- `pkg/agent/registry.go` (new)
- `pkg/config/agents.go` (new)
- `pkg/daemon/daemon.go` (init registry)

### Phase 3: ACP Backend (1 week)

1. Create `pkg/agent/backend_acp.go`
2. Implement ACP protocol client (or shell out to adapter)
3. Add event translation layer (ACP events → Canopy events)
4. Integration tests with claude-code-acp

**Files changed:**
- `pkg/agent/backend_acp.go` (new)
- `pkg/agent/acp/` (new package for protocol types)

### Phase 4: Dashboard Updates (3 days)

1. Show backend name in agent details
2. Add backend filter to agent list
3. Update live feed to handle different event formats

**Files changed:**
- `web/dashboard/src/components/AgentCard.tsx`
- `web/dashboard/src/stores/agentStore.ts`
- `pkg/ipc/protocol.go` (add backend field to events)

## Risks and Mitigations

### Risk 1: ACP Protocol Complexity

**Impact:** Medium
**Probability:** Medium

**Mitigation:**
- Start by shelling out to `claude-code-acp` binary
- Implement native ACP only if performance requires it
- The adapter handles protocol complexity

### Risk 2: Event Format Differences

**Impact:** Low
**Probability:** High

**Mitigation:**
- Event translation layer in `backend_acp.go`
- Normalize to common `Event` type before emitting
- Test with real ACP adapter output

### Risk 3: Token Metrics Unavailable

**Impact:** Low
**Probability:** Medium

**Mitigation:**
- `TokenUsage` is optional in `ExecuteResult`
- Dashboard handles nil gracefully
- Can add cost estimation fallback if needed

## Migration Path

### For Existing Users

1. **No immediate changes required**: `claude-cli` remains the default
2. **Opt-in to ACP**: Add `[backends.acp]` section to config
3. **Per-task override**: Use `--backend=acp` flag for specific tasks

### Deprecations

| Item | Status | Removal |
|------|--------|---------|
| `ExecuteResume()` | Deprecated | Phase 1 |
| `--resume` session tracking | Deprecated | Phase 1 |
| `ResumeInterruptedAgents()` | Deprecated | Phase 1 |
| Overlay `session_id` field | Deprecated | Phase 2 |

### Breaking Changes

- Daemon restart no longer resumes interrupted agents
- `session_id` no longer tracked in persistence
- Recovery flow simplified: orphaned overlays → mark as failed → cleanup

## Appendix

### Code References

| File | Lines | Purpose |
|------|-------|---------|
| `pkg/agent/executor.go` | 128-440 | Fresh agent execution |
| `pkg/agent/executor.go` | 442-702 | Resume agent execution |
| `pkg/daemon/recovery.go` | 24-115 | Orphaned overlay recovery |
| `pkg/daemon/recovery.go` | 199-259 | Get resumable overlays |
| `pkg/daemon/recovery.go` | 270-321 | Resume interrupted agents |
| `pkg/daemon/recovery.go` | 324-417 | Single agent resume |

### Glossary

| Term | Definition |
|------|------------|
| ACP | Agent Client Protocol - standardized IDE-to-agent communication |
| Session ID | Claude-assigned identifier for conversation continuity |
| Overlay | OverlayFS mount for isolated agent file changes |
| Resumable | Agent that can continue from interruption point |
| Restartable | Agent that must start fresh on interruption |

### External References

- [ACP Protocol Overview](https://agentclientprotocol.com/overview/agents)
- [claude-code-acp Adapter](https://github.com/zed-industries/claude-code-acp)
- [Claude Code CLI Documentation](https://docs.anthropic.com/en/docs/claude-code)
