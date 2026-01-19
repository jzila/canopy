# Canopy Architecture

This document provides a detailed technical overview of Canopy's architecture, including package structure, execution flow, data models, and communication patterns.

## Table of Contents

1. [Overview](#overview)
2. [Package Structure](#package-structure)
3. [Execution Flow](#execution-flow)
4. [Data Structures](#data-structures)
5. [IPC Protocol](#ipc-protocol)
6. [Database Schema](#database-schema)
7. [Concurrency Model](#concurrency-model)
   - [Pause State Machine](#pause-state-machine)
8. [Merge Strategy](#merge-strategy)
   - [Merge Queue State Machine](#merge-queue-state-machine)

---

## Overview

Canopy is a parallel agent orchestrator that coordinates multiple Claude Code agents. The core insight is that many software tasks can be decomposed into independent subtasks that execute in parallel, with changes merged afterward.

```
┌─────────────────────────────────────────────────────────────────┐
│                         User / Root Agent                        │
│                              │                                   │
│                              ▼                                   │
│                      canopy run -c N                             │
└──────────────────────────────┬──────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────┐
│                        Orchestrator                              │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                     Beads Client                          │   │
│  │  - Fetches ready tasks (bd ready)                         │   │
│  │  - Marks completed tasks (bd done)                        │   │
│  └──────────────────────────────────────────────────────────┘   │
│                              │                                   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                      Scheduler                            │   │
│  │  - Bounded concurrency via semaphore                      │   │
│  │  - Creates overlay per agent                              │   │
│  │  - Executes agents in parallel                            │   │
│  └──────────────────────────────────────────────────────────┘   │
│                              │                                   │
│         ┌────────────────────┼────────────────────┐              │
│         ▼                    ▼                    ▼              │
│    ┌─────────┐          ┌─────────┐          ┌─────────┐         │
│    │ Agent 1 │          │ Agent 2 │          │ Agent N │         │
│    │ Overlay │          │ Overlay │          │ Overlay │         │
│    └────┬────┘          └────┬────┘          └────┬────┘         │
│         │                    │                    │              │
│         └────────────────────┴────────────────────┘              │
│                              │                                   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                    Merge Queue                            │   │
│  │  - Sequential merging (one at a time)                     │   │
│  │  - Conflict detection and resolver spawning               │   │
│  └──────────────────────────────────────────────────────────┘   │
│                              │                                   │
│                              ▼                                   │
│                     Main Repository                              │
└─────────────────────────────────────────────────────────────────┘
```

---

## Package Structure

```
canopy/
├── cmd/canopy/              # CLI entry points
│   ├── main.go              # Entry point
│   ├── root.go              # Root command, global flags
│   ├── run.go               # canopy run command
│   ├── daemon.go            # canopy daemon command
│   ├── init.go              # canopy init (sandbox config)
│   ├── help.go              # Help with --agent mode
│   ├── history.go           # canopy history command
│   ├── stats.go             # canopy stats command
│   └── version.go           # canopy version command
│
├── pkg/
│   ├── orchestrator/        # Main orchestration logic
│   │   ├── orchestrator.go  # Orchestrator struct, Run() loop
│   │   ├── prompt.go        # Prompt filtering logic
│   │   └── filter_test.go   # Filter tests
│   │
│   ├── scheduler/           # Parallel task execution
│   │   ├── scheduler.go     # Scheduler with semaphore
│   │   └── scheduler_test.go
│   │
│   ├── agent/               # Claude agent execution
│   │   ├── executor.go      # Executor runs claude CLI
│   │   ├── stream.go        # Streaming output parsing
│   │   ├── rlimit_linux.go  # Linux resource limits
│   │   └── rlimit_stub.go   # Non-Linux stub
│   │
│   ├── sandbox/             # OverlayFS and bwrap
│   │   ├── overlay.go       # Overlay interface
│   │   ├── overlay_linux.go # Linux OverlayFS implementation
│   │   ├── overlay_stub.go  # Non-Linux stub
│   │   ├── bwrap.go         # Bubblewrap sandbox
│   │   ├── config.go        # sandbox.toml parsing
│   │   ├── detect.go        # Project/tool detection
│   │   └── git.go           # Git state tracking
│   │
│   ├── merge/               # Change merging
│   │   ├── sequential.go    # Sequential merger
│   │   └── sequential_test.go
│   │
│   ├── mergequeue/          # Merge queue management
│   │   ├── queue.go         # Queue with pause/resume
│   │   ├── processor.go     # Sequential processor
│   │   └── request.go       # MergeRequest type
│   │
│   ├── resolver/            # Conflict resolution
│   │   ├── resolver.go      # Resolver agent spawning
│   │   └── resolver_test.go
│   │
│   ├── beads/               # Beads integration
│   │   └── client.go        # bd CLI wrapper
│   │
│   ├── daemon/              # HTTP/IPC daemon
│   │   ├── daemon.go        # Main daemon struct
│   │   ├── server.go        # HTTP server setup
│   │   ├── handlers.go      # HTTP handlers
│   │   ├── state.go         # RuntimeState
│   │   ├── websocket.go     # WebSocket hub
│   │   ├── events.go        # Event types
│   │   ├── control.go       # Start/stop control
│   │   └── logs.go          # Log viewing
│   │
│   ├── ipc/                 # Inter-process communication
│   │   ├── protocol.go      # Message types
│   │   ├── server.go        # Unix socket server
│   │   ├── client.go        # Unix socket client
│   │   ├── auto.go          # Auto daemon spawning
│   │   └── format.go        # Output formatting
│   │
│   ├── tui/                 # Terminal UI
│   │   ├── dashboard.go     # Main TUI model
│   │   ├── terminal.go      # Terminal rendering
│   │   ├── livefeed.go      # Live feed display
│   │   └── client.go        # Remote WebSocket client
│   │
│   ├── persistence/         # SQLite storage
│   │   ├── store.go         # Database operations
│   │   └── migrate.go       # Schema migrations
│   │
│   ├── history/             # JSON history storage
│   │   └── store.go         # File-based history
│   │
│   ├── repository/          # Repository identity
│   │   ├── repository.go    # Repository struct
│   │   └── registry.go      # Path → ID mapping
│   │
│   ├── runtime/             # Runtime directories
│   │   ├── runtime.go       # XDG paths
│   │   └── runtime_linux.go # Linux-specific
│   │
│   ├── logging/             # Logging setup
│   │   └── logging.go       # File + stderr logging
│   │
│   └── failedpatches/       # Failed patch tracking
│       └── failedpatches.go # Track failed git am
│
└── docs/                    # Documentation
    ├── ARCHITECTURE.md      # This file
    ├── SANDBOX-DESIGN.md    # Security architecture
    └── ...
```

---

## Execution Flow

### `canopy run` Lifecycle

```
1. CLI Parse
   canopy run -c 8 --sandbox
   └── run.go::runOrchestrator()

2. Setup
   ├── Create orchestrator with config
   ├── Load sandbox config (.canopy/sandbox.toml)
   ├── Initialize scheduler
   ├── Create merge queue + processor
   ├── Register event callbacks
   └── Connect to daemon (IPC)

3. Main Loop (orchestrator.Run)
   Loop until no more ready tasks:
   │
   ├── beads.Ready() ─── Fetch unblocked tasks
   │
   ├── scheduler.ExecuteBatch(tasks)
   │   └── For each task (parallel, up to N):
   │       ├── Create OverlayFS sandbox
   │       ├── agent.Execute(task)
   │       │   ├── Build command: claude --print --output-format json
   │       │   ├── Set up environment filtering
   │       │   ├── Run in overlay working directory
   │       │   ├── Stream output → callbacks
   │       │   ├── Parse JSON result
   │       │   └── Detect git commits + file changes
   │       └── Enqueue result for merge
   │
   ├── Merge Queue Processing (sequential)
   │   └── For each completed agent:
   │       ├── Dequeue merge request
   │       ├── Apply git commits (git am)
   │       ├── Apply file changes (copy/delete)
   │       ├── On conflict:
   │       │   ├── Pause queue
   │       │   ├── Spawn resolver agent
   │       │   └── Resume queue
   │       └── Send merge status via IPC
   │
   └── beads.Done(taskID) ─── Mark complete

4. Cleanup
   ├── Save run to history
   ├── Send run_completed via IPC
   ├── Unmount all overlays
   └── Close IPC connection
```

### Agent Execution Detail

```go
// pkg/agent/executor.go

func (e *Executor) Execute(ctx context.Context, task *beads.Task, overlay *sandbox.Overlay) (*Result, error) {
    // 1. Build command
    cmd := exec.CommandContext(ctx, "claude",
        "--print",
        "--output-format", "json",
        "--dangerously-skip-permissions",
        prompt,
    )

    // 2. Set working directory to overlay
    cmd.Dir = overlay.MergedDir()

    // 3. Filter environment
    cmd.Env = filterEnvironment(os.Environ())

    // 4. Set up process group (for clean termination)
    cmd.SysProcAttr = &syscall.SysProcAttr{
        Setpgid:   true,
        Pdeathsig: syscall.SIGTERM,
    }

    // 5. Execute with streaming output
    stdout, stderr := captureStreaming(cmd, callbacks)

    // 6. Parse JSON output
    output := parseClaudeOutput(stdout)

    // 7. Detect changes
    changes := overlay.GetChanges()
    gitState := overlay.GetGitState(beforeRef)

    return &Result{
        TaskID:   task.ID,
        Success:  err == nil && cmd.ProcessState.ExitCode() == 0,
        Output:   output,
        Changes:  changes,
        GitState: gitState,
        Duration: time.Since(start),
    }, nil
}
```

---

## Data Structures

### Core Types

```go
// Task from beads
type Task struct {
    ID          string   `json:"id"`
    Title       string   `json:"title"`
    Description string   `json:"description"`
    Type        string   `json:"type"`
    Priority    int      `json:"priority"`
    Status      string   `json:"status"`
    Blockers    []string `json:"blockers"`
}

// Agent execution result
type Result struct {
    TaskID   string
    Success  bool
    Output   *ClaudeOutput    // Parsed JSON from claude
    Stdout   string           // Raw stdout
    Stderr   string           // Raw stderr
    ExitCode int
    Changes  []FileChange     // Files modified
    GitState *GitState        // Git commits made
    Duration time.Duration
    Error    string
    Overlay  *Overlay         // For commit info retrieval
}

// Claude CLI JSON output
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
    ResultMessage            string                    `json:"result"`
    ModelUsage               map[string]ModelUsageData `json:"model_usage"`
}

// File change detected in overlay
type FileChange struct {
    Path    string
    Type    string  // "created", "modified", "deleted"
    NewHash string  // Git blob hash
}

// Git state before/after execution
type GitState struct {
    BeforeRef  string   // HEAD before agent ran
    AfterRef   string   // HEAD after agent ran
    NewCommits []string // Commit hashes created
    Patches    []string // Patch file contents
}
```

### Daemon State Types

```go
// In-memory runtime state
type RuntimeState struct {
    agents map[string]*AgentState
    tasks  map[string]*TaskState
    mu     sync.RWMutex
}

// Agent execution state
type AgentState struct {
    ID            string
    TaskID        string
    TaskTitle     string
    Status        string          // running, completed, failed
    ParentAgentID string          // For resolver agents
    Output        string          // Accumulated output
    Stdout        string
    Stderr        string
    StartTime     time.Time
    EndTime       time.Time
    Duration      time.Duration
    ExitCode      int
    MergeStatus   string          // See "Merge Queue State Machine" section for all values
    QueuePosition int
    RepoID        string
    Result        *agent.Result
}

// Merge queue request
type MergeRequest struct {
    Result  *agent.Result
    Done    chan error           // Signals completion
}
```

---

## IPC Protocol

### Unix Socket Communication

The orchestrator (`canopy run`) communicates with the daemon via Unix socket IPC. Messages are JSON-encoded.

### Message Types

```go
type MessageType string

const (
    MessageTypeAgentStart       MessageType = "agent_start"
    MessageTypeAgentOutput      MessageType = "agent_output"
    MessageTypeAgentLiveFeed    MessageType = "agent_live_feed"
    MessageTypeAgentCommit      MessageType = "agent_commit"
    MessageTypeAgentMergeStatus MessageType = "agent_merge_status"
    MessageTypeAgentDone        MessageType = "agent_done"
    MessageTypeAgentFail        MessageType = "agent_fail"
    MessageTypeRunStarted       MessageType = "run_started"
    MessageTypeRunCompleted     MessageType = "run_completed"
)

type Message struct {
    Type    MessageType     `json:"type"`
    Payload json.RawMessage `json:"payload"`
}
```

### Payload Examples

```json
// agent_start
{
    "type": "agent_start",
    "payload": {
        "agent_id": "agent-canopy-a1b2",
        "task_id": "canopy-a1b2",
        "task_title": "Implement authentication",
        "parent_agent_id": "",
        "repo_id": "550e8400-e29b-41d4-a716-446655440000"
    }
}

// agent_merge_status
{
    "type": "agent_merge_status",
    "payload": {
        "agent_id": "agent-canopy-a1b2",
        "merge_status": "merging",
        "queue_position": 0
    }
}

// agent_done
{
    "type": "agent_done",
    "payload": {
        "agent_id": "agent-canopy-a1b2",
        "parent_agent_id": "",
        "result": {
            "exit_code": 0,
            "input_tokens": 15000,
            "output_tokens": 3000,
            "cost_usd": 0.15,
            "duration_ms": 45000,
            "files_changed": 5,
            "commits_created": 2
        }
    }
}

// run_completed
{
    "type": "run_completed",
    "payload": {
        "run_id": "abc123",
        "stats": {
            "total_tasks": 5,
            "succeeded_tasks": 4,
            "failed_tasks": 1,
            "total_duration": 120.5,
            "total_cost_usd": 0.75
        }
    }
}
```

---

## Database Schema

### SQLite Tables (`~/.cache/canopy/runs.db`)

```sql
-- Runs table
CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    started_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP,
    status TEXT NOT NULL,           -- running, completed, failed, cancelled
    concurrency INTEGER,
    git_branch TEXT,
    git_commit TEXT,
    total_tasks INTEGER DEFAULT 0,
    completed_tasks INTEGER DEFAULT 0,
    failed_tasks INTEGER DEFAULT 0,
    repo_id TEXT,
    repo_path TEXT,
    repo_name TEXT
);

-- Agents table
CREATE TABLE agents (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    task_id TEXT,
    task_title TEXT,
    status TEXT NOT NULL,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    duration_seconds REAL,
    exit_code INTEGER,
    error_message TEXT,
    stdout TEXT,
    stderr TEXT,
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    total_tokens INTEGER DEFAULT 0,
    cost_usd REAL DEFAULT 0,
    files_changed INTEGER DEFAULT 0,
    git_commits_created INTEGER DEFAULT 0,
    repo_id TEXT,
    archived BOOLEAN DEFAULT FALSE,
    FOREIGN KEY (run_id) REFERENCES runs(id)
);

-- Commits table
CREATE TABLE commits (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    hash TEXT NOT NULL,
    short_hash TEXT,
    message TEXT,
    author TEXT,
    author_email TEXT,
    timestamp TIMESTAMP,
    files_changed TEXT,             -- JSON array
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);

-- Indexes
CREATE INDEX idx_agents_run_id ON agents(run_id);
CREATE INDEX idx_agents_repo_id ON agents(repo_id);
CREATE INDEX idx_commits_agent_id ON commits(agent_id);
CREATE INDEX idx_runs_started_at ON runs(started_at);
```

### Repository Registry (`~/.cache/canopy/repositories.json`)

```json
{
    "/home/user/project-a": {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "path": "/home/user/project-a",
        "name": "project-a",
        "created_at": "2024-01-15T10:30:00Z"
    },
    "/home/user/project-b": {
        "id": "6fa459ea-ee8a-3ca4-894e-db77e160355e",
        "path": "/home/user/project-b",
        "name": "project-b",
        "created_at": "2024-01-16T14:22:00Z"
    }
}
```

---

## Concurrency Model

### Bounded Parallelism

The scheduler uses a weighted semaphore to limit concurrent agents:

```go
// pkg/scheduler/scheduler.go

func (s *Scheduler) ExecuteBatch(ctx context.Context, tasks []*beads.Task) error {
    g, ctx := errgroup.WithContext(ctx)
    sem := semaphore.NewWeighted(int64(s.config.Concurrency))

    for _, task := range tasks {
        task := task  // Capture for goroutine

        g.Go(func() error {
            // Acquire semaphore slot
            if err := sem.Acquire(ctx, 1); err != nil {
                return err
            }
            defer sem.Release(1)

            // Execute agent
            return s.executeAgent(ctx, task)
        })
    }

    return g.Wait()
}
```

### Merge Queue

Merging is strictly sequential to avoid conflicts:

```go
// pkg/mergequeue/processor.go

func (p *Processor) Run(ctx context.Context) {
    for {
        // Dequeue blocks if paused (during conflict resolution)
        req, ok := p.queue.Dequeue(ctx)
        if !ok {
            return
        }

        // Acquire merge slot (only one merge at a time)
        p.mergeSem.Acquire(ctx, 1)

        // Perform merge
        err := p.merge(req)

        // Signal completion
        req.Done <- err

        p.mergeSem.Release(1)
    }
}
```

### Pause State Machine

The merge queue implements a two-source pause state machine that allows both user-initiated pauses and agent-initiated pauses to coexist independently. This ensures that:

1. User pauses (manual intervention) don't interfere with agent pauses (conflict resolution, repair)
2. Both sources must resume before the queue becomes running
3. State transitions are atomic and thread-safe

**State Diagram:**

```
         UserPause()              AgentPause()
    ┌────────────────┐       ┌────────────────┐
    │                ▼       │                ▼
    │   ┌────────────────────┴────┐    ┌─────┴──────────┐
    │   │        Running          │    │   PausedAgent  │
    │   │  (queue processing)     │◄───┤  (agent active)│
    │   └────────────┬────────────┘    └────────────────┘
    │                │                        │
    │         UserPause()              UserPause()
    │                │                        │
    │                ▼                        ▼
    │   ┌────────────────────┐     ┌─────────────────────┐
    └───┤     PausedUser     │     │     PausedBoth      │
        │ (user requested)   │────►│ (both sources)      │
        └────────────────────┘     └─────────────────────┘
                   ▲     AgentPause()         │
                   │                          │
                   └──────────────────────────┘
                         AgentResume()
```

**State Transition Table:**

| Action       | Running      | PausedUser   | PausedAgent  | PausedBoth   |
|--------------|--------------|--------------|--------------|--------------|
| UserPause    | → PausedUser | (no-op)      | → PausedBoth | (no-op)      |
| UserResume   | (no-op)      | → Running    | (no-op)      | → PausedAgent|
| AgentPause   | → PausedAgent| → PausedBoth | (no-op)      | (no-op)      |
| AgentResume  | (no-op)      | (no-op)      | → Running    | → PausedUser |

**Implementation:** `pkg/mergequeue/pause_state.go`

**Agent Types That Can Pause:**
- **Resolver agents**: Spawned during merge conflict resolution
- **Repair agents**: Spawned when post-merge validation fails
- **Future**: Architect agents, documentation agents, etc.

**Usage Example:**

```go
// During conflict resolution
p.queue.AgentPause()           // Pause queue for agent work
defer p.queue.AgentResume()    // Resume when agent completes

// Spawn resolver/repair agent
result, err := p.resolveAsync(ctx, conflictCtx)
// Queue resumes automatically via defer
```

---

## Process Hierarchy and IPC Design

### Process Hierarchy

Agents are managed as child processes of their orchestrator using Unix process groups:

```
┌─────────────────────────────────────────────────────────────────┐
│                           Daemon                                 │
│                (long-running, started once)                     │
│                              │                                   │
│                    IPC (Unix socket)                            │
│                              │                                   │
└──────────────────────────────┼──────────────────────────────────┘
                               ▼
┌──────────────────────────────────────────────────────────────────┐
│                        Orchestrator                               │
│               (one per canopy run invocation)                    │
│                              │                                   │
│              ┌───────────────┼───────────────┐                   │
│              │               │               │                   │
│              ▼               ▼               ▼                   │
│         [Agent 1]       [Agent 2]       [Agent N]                │
│      (PGID=PID_1)    (PGID=PID_2)    (PGID=PID_N)               │
│              │               │               │                   │
│              ▼               ▼               ▼                   │
│     (child procs)    (child procs)    (child procs)             │
└──────────────────────────────────────────────────────────────────┘
```

**Key design decisions:**

1. **Process Groups** (`Setpgid: true`): Each agent spawns in its own process group. This enables clean termination of the agent and all its child processes (e.g., subprocess spawned by Claude).

2. **Parent Death Signal** (`Pdeathsig: SIGKILL`): If the orchestrator crashes unexpectedly, all agents receive SIGKILL immediately. This prevents orphaned agent processes consuming resources without supervision.

3. **Graceful Shutdown**: On context cancellation (SIGINT/SIGTERM to orchestrator), agents receive SIGTERM to their process group, wait up to 3 seconds for graceful exit, then SIGKILL if still running.

```go
// Termination sequence:
// 1. Context cancelled (timeout, SIGINT, or SIGTERM)
// 2. SIGTERM sent to -PGID (negative = process group)
// 3. Wait up to 3 seconds for exit
// 4. SIGKILL sent to -PGID if still running
```

### Agent-Orchestrator Communication

Communication is one-way: agent → orchestrator via stdout streaming:

```
┌─────────────┐                      ┌─────────────────┐
│    Agent    │ ────── stdout ─────► │  Orchestrator   │
│  (claude)   │                      │                 │
│             │ ◄───── (none) ────── │  (no input)     │
│             │                      │                 │
│             │ ◄─── SIGTERM/KILL ── │  (cancellation) │
└─────────────┘                      └─────────────────┘
```

**Design rationale:**

- **No bidirectional IPC**: Agents are black-box CLI executions. The orchestrator doesn't send instructions mid-execution.
- **Cancellation via signals**: Process termination is the only communication to agents. No message-based cancellation protocol.
- **Security boundary**: Agents may run sandboxed (bubblewrap) and should be treated as potentially untrusted. They have no access to daemon sockets or orchestrator state.

### Agent-Daemon Isolation

Agents have zero knowledge of the daemon:

```
Agent process:
├── No IPC_SOCKET_PATH in environment
├── No daemon URLs or addresses
├── Filtered environment (only ANTHROPIC_*, PATH, etc.)
└── Sandboxed working directory (OverlayFS)

All status flows:
Agent → stdout → Orchestrator → IPC → Daemon → WebSocket → Dashboard
```

This isolation provides:
- **Security**: Agents cannot interfere with orchestration
- **Simplicity**: Agents are stateless command executions
- **Portability**: Agent execution doesn't depend on daemon availability

### Daemon Restart Resilience

The orchestrator-daemon connection handles daemon restarts:

```go
// IPC Client reconnection behavior:
// - Queue up to 10,000 messages during disconnection
// - Exponential backoff: 100ms → 200ms → ... → 5s
// - Retry for up to 5 minutes before giving up
// - Flush queued messages on reconnection
```

If the daemon restarts:
1. Orchestrator queues outgoing IPC messages
2. Reconnection attempts with exponential backoff
3. On reconnection, queued messages flush in order
4. No message loss during brief daemon outages

If the daemon is unreachable for >5 minutes:
1. IPC client enters "disconnected" state
2. Orchestrator continues execution (logging to stderr)
3. Dashboard will be out of date until daemon returns

---

## Merge Strategy

### Merge Queue State Machine

The merge queue tracks each agent's changes through a state machine as they are processed. Understanding these states is essential for debugging merge issues and monitoring agent progress.

#### Merge Status Values

| Status | Value | Description |
|--------|-------|-------------|
| None | `""` | Agent has not entered the merge queue, or has no changes to merge |
| Pending | `pending` | Agent is waiting in queue for a merge slot |
| Acquiring | `acquiring` | Agent is attempting to acquire a merge slot |
| Merging | `merging` | Agent is actively applying patches/changes via `git am` |
| Resolving | `resolving` | A resolver or repair agent was spawned to handle conflicts/failures |
| Merged | `merged` | Changes were successfully merged (with or without validation) |
| Failed | `failed` | Merge failed and could not be recovered |
| Resolved | `resolved` | Merge succeeded after conflict resolution (persistence only) |
| Skipped | `skipped` | Merge was skipped because there were no changes to apply |
| Merged Needs Repair | `merged_needs_repair` | Merge succeeded but validation failed after all repair attempts |

#### State Transition Diagram

```
                              ┌─────────────────────────────────────────────────────────────┐
                              │                     MERGE QUEUE STATES                        │
                              └─────────────────────────────────────────────────────────────┘

    ┌──────┐    enqueue     ┌─────────┐   slot available   ┌───────────┐   begin merge   ┌─────────┐
    │ none │ ──────────────►│ pending │ ─────────────────► │ acquiring │ ───────────────►│ merging │
    └──────┘                └─────────┘                    └───────────┘                 └────┬────┘
                                                                                              │
                                                                                              │
                         ┌────────────────────────────────────────────────────────────────────┤
                         │                                                                    │
                         │                                                                    │
    ┌────────────────────┼─────────────────────────────┐     ┌────────────────────────────────┼────────┐
    │                    │                             │     │                                │        │
    │   CONFLICT PATH    │                             │     │   HAPPY PATH                   │        │
    │                    │                             │     │                                │        │
    │        ┌───────────▼─────────┐                   │     │                                ▼        │
    │        │     resolving       │                   │     │                  ┌──────────────────┐   │
    │        │ (resolver spawned)  │                   │     │                  │ (git am success) │   │
    │        └─────────┬───────────┘                   │     │                  └────────┬─────────┘   │
    │                  │                               │     │                           │             │
    │      ┌───────────┼───────────┐                   │     │              ┌────────────┼─────────┐   │
    │      │           │           │                   │     │              │            │         │   │
    │      ▼           ▼           ▼                   │     │              ▼            ▼         │   │
    │  ┌────────┐  ┌────────┐  ┌─────────┐            │     │     ┌────────────┐  ┌─────────────┐ │   │
    │  │ merged │  │resolved│  │ failed  │            │     │     │ validation │  │  no changes │ │   │
    │  │        │  │(+val)  │  │         │            │     │     │  enabled?  │  │   (skip)    │ │   │
    │  └────────┘  └────────┘  └─────────┘            │     │     └──────┬─────┘  └──────┬──────┘ │   │
    │                                                 │     │            │               │        │   │
    └─────────────────────────────────────────────────┘     │     ┌──────┴──────┐        │        │   │
                                                            │     │             │        │        │   │
                                                            │     ▼             ▼        ▼        │   │
                                                            │  ┌──────┐    ┌────────┐ ┌─────────┐ │   │
                                                            │  │ yes  │    │   no   │ │ skipped │ │   │
                                                            │  └──┬───┘    └───┬────┘ └─────────┘ │   │
                                                            │     │            │                  │   │
                                                            └─────┼────────────┼──────────────────┘   │
                                                                  │            │                      │
                                                                  │            │                      │
                         ┌────────────────────────────────────────┘            │                      │
                         │                                                     │                      │
                         │                                                     ▼                      │
    ┌────────────────────┼─────────────────────────────┐              ┌────────────────┐              │
    │                    │                             │              │                │              │
    │   VALIDATION PATH  │                             │              │    merged      │◄─────────────┘
    │                    │                             │              │                │
    │        ┌───────────▼─────────┐                   │              └────────────────┘
    │        │    run validation   │                   │
    │        │    (build, test)    │                   │
    │        └─────────┬───────────┘                   │
    │                  │                               │
    │      ┌───────────┼───────────┐                   │
    │      │           │           │                   │
    │      ▼           ▼           ▼                   │
    │  ┌────────┐  ┌────────────┐  ┌────────────────┐  │
    │  │ passed │  │  failed    │  │ failed (strict)│  │
    │  │        │  │ (lenient)  │  │   → revert     │  │
    │  └───┬────┘  └─────┬──────┘  └───────┬────────┘  │
    │      │             │                 │           │
    │      ▼             │                 ▼           │
    │  ┌────────┐        │            ┌─────────┐      │
    │  │ merged │        │            │ failed  │      │
    │  └────────┘        │            └─────────┘      │
    │                    │                             │
    │                    ▼                             │
    │            ┌───────────────┐                     │
    │            │ repair agent  │                     │
    │            │ (up to N tries)                     │
    │            └───────┬───────┘                     │
    │                    │                             │
    │        ┌───────────┼───────────┐                 │
    │        │           │           │                 │
    │        ▼           ▼           ▼                 │
    │   ┌────────┐ ┌───────────┐ ┌──────────────────┐  │
    │   │ merged │ │ resolving │ │merged_needs_repair│ │
    │   │(fixed) │ │ (retry)   │ │ (exhausted)       │ │
    │   └────────┘ └─────┬─────┘ └──────────────────┘  │
    │                    │                             │
    │                    └─────► (loops to validation) │
    └──────────────────────────────────────────────────┘
```

#### When Each Status Is Set

**`none` (empty string)**
- Initial state when agent starts executing
- Never explicitly set; represents absence of merge queue involvement

**`pending`**
- Set when agent's result is enqueued for merge processing
- Agent is waiting for other agents ahead in the queue to complete their merges

**`acquiring`**
- Set when the queue processor starts processing this agent's merge request
- Brief transitional state before actual merge begins

**`merging`**
- Set when `git am` (patch application) or file copy operations begin
- Most time in this state is spent applying git patches

**`resolving`**
- Set when merge fails and a resolver agent is spawned to fix conflicts
- Also set when repair agent is spawned after validation failure
- Queue pauses while resolver/repair agent works

**`merged`**
- Set when all changes are successfully applied to the repository
- If validation is enabled: set only after validation passes
- If validation is disabled: set immediately after successful `git am`

**`resolved`**
- Used in persistence layer to distinguish merges that required conflict resolution
- Functionally equivalent to `merged` but indicates resolver agent intervention

**`failed`**
- Set when merge cannot be completed and no recovery is possible
- Causes: resolver agent failure, validation failure (strict mode), cancelled context
- In strict validation mode: merge is reverted before setting this status

**`skipped`**
- Set when agent completed but had no actual changes to apply
- Common when: work was already done by another agent, or resolver determined no action needed

**`merged_needs_repair`**
- Set when merge succeeded but validation failed after exhausting all repair attempts
- Only occurs in lenient validation mode (strict mode reverts and fails instead)
- Creates a bead for manual intervention with full context

#### Validation and Repair Interaction

Post-merge validation runs when configured in `.canopy/validation.toml`:

1. **Validation runs after successful merge**
2. **If validation fails:**
   - **Lenient mode**: Spawn repair agent (up to N attempts), then `merged_needs_repair` if exhausted
   - **Strict mode**: Revert merge via `git reset --hard`, set status to `failed`
3. **Repair agents** run directly on the working directory (not in overlay)
4. **Each repair attempt** re-runs validation to check if fix worked

#### State Persistence

Final merge statuses are persisted to SQLite (`~/.cache/canopy/runs.db`) in the `agents` table. The `resolved` status is only used in persistence to distinguish merges that required conflict resolution from direct merges.

---

### Sequential Merging

Changes are merged in completion order:

1. **Git commits**: Applied via `git am` to preserve history
2. **File changes**: Copied directly from overlay upper directory
3. **Conflicts**: Detected when multiple agents modify the same file

```go
// pkg/merge/sequential.go

func (m *SequentialMerger) Merge(results []*agent.Result) (*Result, error) {
    for _, result := range results {
        // Apply git commits
        for _, patch := range result.GitState.Patches {
            if err := m.applyPatch(patch); err != nil {
                return nil, &Conflict{
                    TaskID: result.TaskID,
                    Patch:  patch,
                    Error:  err,
                }
            }
        }

        // Apply file changes
        for _, change := range result.Changes {
            if err := m.applyChange(change); err != nil {
                return nil, err
            }
        }
    }

    return &Result{Applied: len(results)}, nil
}
```

### Conflict Resolution

When `git am` fails, a resolver agent is spawned:

```go
// pkg/resolver/resolver.go

func (r *Resolver) Resolve(ctx context.Context, conflict *ConflictContext) (*Result, error) {
    prompt := fmt.Sprintf(`
A merge conflict occurred. Please resolve it.

Failed patch:
%s

Error:
%s

File changes:
%v
`, conflict.Patch, conflict.Error, conflict.Changes)

    result, err := r.executor.Execute(ctx, resolverTask, overlay)
    return &Result{Resolved: err == nil}, err
}
```

---

## See Also

- [Sandbox Design](SANDBOX-DESIGN.md) - Security architecture and OverlayFS details
- [Validation Guide](VALIDATION.md) - Post-merge validation and repair agents
- [Commands](COMMANDS.md) - CLI reference
- [Configuration](CONFIGURATION.md) - Configuration options
