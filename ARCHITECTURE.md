# Canopy Architecture

This document describes the core architectural components of Canopy: a parallel AI agent orchestrator that coordinates multiple Claude agents working on different tasks simultaneously.

## Overview

Canopy uses a daemon/orchestrator split architecture where:
- The **daemon** runs persistently, providing monitoring, state tracking, and a web dashboard
- The **orchestrator** runs transiently during `canopy run`, executing tasks in parallel
- **Overlay sandboxes** isolate each agent's filesystem changes
- A **merge coordinator** serializes agent outputs into the main repository

```
┌─────────────────────────────────────────────────────────────────┐
│                         Canopy Daemon                           │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────────┐   │
│  │ EventBus │──│  State   │──│  HTTP    │──│ WebSocket Hub  │   │
│  └──────────┘  └──────────┘  └──────────┘  └────────────────┘   │
│       ▲                                              │          │
│       │ IPC                                          │ WS       │
└───────┼──────────────────────────────────────────────┼──────────┘
        │                                              ▼
┌───────┴─────────────────────────┐           ┌──────────────────┐
│         Orchestrator            │           │  Web Dashboard   │
│  ┌───────────┐  ┌────────────┐  │           └──────────────────┘
│  │ Scheduler │  │   Merge    │  │
│  │           │  │Coordinator │  │
│  └─────┬─────┘  └─────┬──────┘  │
│        │              │         │
│   ┌────┴────────┬─────┴────┐    │
│   ▼             ▼          ▼    │
│ Agent 1     Agent 2    Agent N  │
│ [Overlay]   [Overlay]  [Overlay]│
└─────────────────────────────────┘
```

## 1. Daemon/Orchestrator Split

### Daemon (`pkg/daemon/`)

The daemon is a long-running background process that provides centralized monitoring and state tracking for all Canopy operations.

**Entry Point:** `cmd/canopy/daemon.go`

**Commands:**
- `canopy daemon` - Start in foreground
- `canopy daemon --tui` - Start with TUI dashboard
- `canopy daemon start|stop|restart|status` - Manage as background service

**Responsibilities:**
- Runs HTTP server (default port 8080) for REST API and WebSocket connections
- Listens on Unix socket for IPC events from orchestrators
- Maintains `RuntimeState` tracking all agents and tasks across runs
- Distributes events via `EventBus` to WebSocket clients (web dashboard)
- Persists run history to SQLite (`$XDG_CACHE_HOME/canopy/runs.db`)
- Manages repository registry for multi-repo support

**Core Components:**

```go
// pkg/daemon/daemon.go
type Daemon struct {
    config           Config
    eventBus         *EventBus        // Pub/sub event distribution
    state            *RuntimeState    // Agent/task state tracking
    repoManager      *RepositoryManager
    persistManager   *PersistenceManager
    lifecycle        *LifecycleManager
    ipcServer        IPCServer        // Unix socket server
    httpServer       *Server          // HTTP/WebSocket server
}
```

**Key Files:**
| File | Purpose |
|------|---------|
| `daemon.go` | Main coordinator, component lifecycle |
| `state.go` | `RuntimeState` for agent/task tracking |
| `events.go` | `EventBus` pub/sub implementation |
| `handlers.go` | HTTP handlers (pause/resume/kill) |
| `server.go` | HTTP server setup and routing |
| `websocket.go` | WebSocket hub for browser clients |
| `lifecycle_manager.go` | Signal handling, PID files |
| `persistence_manager.go` | SQLite persistence |

### Orchestrator (`pkg/orchestrator/`)

The orchestrator is a short-lived process spawned by `canopy run` that executes tasks from beads in parallel.

**Entry Point:** `cmd/canopy/run.go`

**Responsibilities:**
- Loads ready tasks from beads database (respecting dependencies)
- Creates overlay sandboxes for each agent
- Executes Claude agents in parallel with bounded concurrency
- Routes completed agents to the merge coordinator
- Handles retries for failed tasks
- Reports progress to daemon via IPC

**Core Components:**

```go
// pkg/orchestrator/orchestrator.go
type Orchestrator struct {
    config           *Config
    beadsClient      beads.BeadsClient      // Task database
    scheduler        *scheduler.Scheduler    // Parallel execution
    mergeCoordinator *mergecoordinator.MergeCoordinator
    failureCounts    map[string]int         // Retry tracking
    sandboxConfig    *sandbox.SandboxConfig // Sandbox configuration
}

type Config struct {
    WorkDir         string        // Repository directory
    OutputDir       string        // Where merged changes are written
    Concurrency     int           // Parallel agents (default: 4)
    UseBwrap        bool          // Full sandbox isolation
    MaxRetries      int           // Retry failed tasks (default: 3)
    MaxPriority     int           // Only run tasks with priority <= this (-1 = no filter)
    ResolverTimeout time.Duration // Timeout for resolver agents
}
```

**Execution Flow:**

1. Connect to daemon via IPC
2. Load ready tasks from beads
3. For each batch of tasks:
   - Create overlay sandbox per task
   - Execute Claude agent in sandbox
   - On completion, enqueue to merge coordinator
   - On failure, increment retry count or mark failed
4. Wait for all merges to complete
5. Report final status to daemon

## 2. IPC Protocol

The IPC system enables the orchestrator (`canopy run`) to communicate real-time status updates to the daemon.

**Location:** `pkg/ipc/`

### Architecture

- **Transport:** Unix domain sockets (default: `$XDG_RUNTIME_DIR/canopy.sock`)
- **Format:** Newline-delimited JSON
- **Pattern:** One-way messaging (client → server)
- **Resilience:** Auto-reconnection with event buffering

### Client (`pkg/ipc/client.go`)

Sends events from orchestrator/agents to daemon:

```go
type Client struct {
    socketPath    string
    conn          net.Conn
    encoder       *json.Encoder
    eventQueue    []queuedEvent    // Buffer when disconnected
    maxQueueSize  int              // Default: 10,000
    droppedEvents int
}
```

**Key Methods:**
- `SendAgentStart(agentID, taskID, title, parentAgentID, repoID)` - Agent started
- `SendAgentOutput(agentID, output, isError)` - Stdout/stderr output
- `SendAgentLiveFeed(agentID, eventType, rawData)` - Real-time Claude events
- `SendAgentCommit(agentID, hash, message, filesChanged)` - Git commit made
- `SendAgentMergeStatus(agentID, status)` - Merge in progress
- `SendAgentDone(agentID, parentAgentID, result)` - Agent completed
- `SendAgentFailed(agentID, parentAgentID, error)` - Agent failed

**Reconnection Logic:**
- Exponential backoff: 100ms → 5s (max)
- Timeout: 5 minutes
- Events queued during disconnection, flushed on reconnect
- Oldest events dropped first if queue overflows (10,000 limit)

### Server (`pkg/ipc/server.go`)

Accepts connections and forwards events to EventBus:

```go
type Server struct {
    socketPath  string
    listener    net.Listener
    eventBus    *events.EventBus
    connections map[net.Conn]struct{}
}
```

### Event Types (`pkg/events/events.go`)

```go
const (
    EventRunStarted       = "run:started"       // Orchestration started
    EventRunCompleted     = "run:completed"     // Orchestration finished
    EventAgentStarted     = "agent:started"     // Agent started
    EventAgentOutput      = "agent:output"      // Stdout/stderr
    EventAgentLiveFeed    = "agent:live_feed"   // Claude API events
    EventAgentCommit      = "agent:commit"      // Git commit made
    EventAgentMergeStatus = "agent:merge_status"// Merge progress
    EventAgentCompleted   = "agent:completed"   // Agent finished (success)
    EventTaskUpdated      = "task:updated"      // Task status changed
    EventOrchPaused       = "orch:paused"       // Orchestration paused
    EventOrchResumed      = "orch:resumed"      // Orchestration resumed
)
```

### Message Flow

```
IPC Client (orchestrator)
    ↓ (Unix socket, JSON per line)
IPC Server (daemon)
    ↓ (in-process)
EventBus (pub/sub)
    ↓ (subscribers)
├── RuntimeState (updates agent state)
├── WebSocket Hub (broadcasts to browsers)
└── PersistenceManager (records to SQLite)
```

### Example Message

```json
{
  "type": "agent:started",
  "timestamp": "2024-01-16T12:34:56Z",
  "payload": {
    "agent_id": "agent-abc123-task-xyz",
    "task_id": "task-xyz",
    "task_title": "Implement feature X",
    "parent_agent_id": "",
    "repo_id": "repo-001"
  }
}
```

## 3. Merge Coordinator

The merge coordinator serializes agent outputs into the repository, handling conflicts via resolver agents.

**Location:** `pkg/mergecoordinator/`, `pkg/mergequeue/`, `pkg/merge/`, `pkg/resolver/`

### Why Sequential Merging?

While agents execute in parallel, their outputs must merge sequentially to prevent git conflicts. Each agent may create commits that modify overlapping files—merging them concurrently would create race conditions.

```
Parallel Agents           Sequential Merge Queue
[Agent 1] ────┐
[Agent 2] ────┼──→ [Queue] → [Processor] → [Git Merge] → [Output Dir]
[Agent 3] ────┤                   │
[Agent 4] ────┘                   │
                                  ▼
                          (on conflict)
                         [Resolver Agent]
```

### Core Components

**MergeCoordinator** (`pkg/mergecoordinator/coordinator.go`):

```go
type MergeCoordinator struct {
    config      *Config
    queue       *mergequeue.Queue      // Buffered merge requests
    processor   *mergequeue.Processor  // Sequential processor
    merger      *merge.SequentialMerger
    resolver    *resolver.Resolver     // Conflict handler
    beadsClient beads.BeadsClient
    ipcClient   *ipc.Client
    agentIDMap  sync.Map               // taskID → agentID (write-once, disjoint keys)
    taskCache   sync.Map               // taskID → *beads.Task (store-then-delete)
}
```

**Queue** (`pkg/mergequeue/queue.go`):

```go
type Queue struct {
    requests   chan *MergeRequest
    pauseState *PauseStateMachine  // Manages pause from user and resolver sources
    closed     atomic.Bool
}
```

**PauseStateMachine** (`pkg/mergequeue/pause_state.go`):

The queue uses an explicit state machine to coordinate pause requests from two independent sources: user-initiated pauses (via dashboard) and resolver-initiated pauses (during conflict resolution). This prevents race conditions where a user resume could incorrectly unblock a resolver operation, or vice versa.

```
┌──────────┐  UserPause   ┌──────────────┐
│ Running  │─────────────▶│ PausedUser   │
└──────────┘              └──────────────┘
     │                           │
     │ ResolverPause             │ ResolverPause
     ▼                           ▼
┌──────────────┐          ┌──────────────┐
│PausedResolver│─────────▶│ PausedBoth   │
└──────────────┘ UserPause└──────────────┘
```

The system only runs (allows dequeue) when in the `Running` state. Transitions:
- `UserPause()`: Running→PausedUser, PausedResolver→PausedBoth
- `UserResume()`: PausedUser→Running, PausedBoth→PausedResolver
- `ResolverPause()`: Running→PausedResolver, PausedUser→PausedBoth
- `ResolverResume()`: PausedResolver→Running, PausedBoth→PausedUser
- `WaitUntilRunning(ctx)`: Blocks until Running state or context cancelled

**MergeRequest/Response**:

```go
type MergeRequest struct {
    Result      *agent.Result      // Agent output with changes
    Task        *beads.Task        // Original task
    Response    chan *MergeResponse
    EnqueuedAt  time.Time
}

type MergeResponse struct {
    Success          bool
    Error            string
    CommitsApplied   int
    HadConflict      bool
    ResolverSpawned  bool
}
```

### Merge Processor Workflow

The processor (`pkg/mergequeue/processor.go`) runs in a dedicated goroutine:

```
1. Dequeue merge request
2. Commit any dirty beads changes (pre-merge sync)
3. Apply agent changes:
   a. If agent made git commits → use `git am` to apply patches
   b. If only file changes → copy files to output directory
4. On conflict:
   a. Pause the queue (block new merges)
   b. Spawn resolver agent with conflict context
   c. Wait for resolver to complete (10 min timeout)
   d. Resume queue
5. Update beads: mark task Done or Failed
6. Send IPC merge status
7. Return response to caller
```

### Sequential Merger (`pkg/merge/sequential.go`)

Applies changes from agent overlays to the output directory:

- Uses `git am` for commits (preserves history and authorship)
- Falls back to file copy for non-commit changes
- Skips `.canopy/` and `.beads/` directories
- Filters transient files (`.claude.json.lock`, etc.)
- Strategy: Later results overwrite earlier ones

### Resolver Agents (`pkg/resolver/resolver.go`)

When a git merge fails, a resolver agent is spawned to manually fix conflicts:

```go
type ConflictContext struct {
    TaskID          string           // Original task
    TaskTitle       string
    TaskDescription string
    FailedPatches   []string         // Patch content that failed
    PatchErrors     []string         // Error messages
    FileChanges     []FileChange     // Files from the failed merge
    ParentAgentID   string           // The failing agent (for UI)
}
```

**Resolver Execution:**
1. Creates fresh overlay based on current HEAD
2. Writes failed patches to sandbox as reference
3. Receives original task description for context
4. Must manually resolve conflicts and create commits
5. Marked as child of parent agent in UI (`parent_agent_id`)
6. Has 10-minute timeout (configurable)

## 4. Overlay Sandbox Lifecycle

Overlays provide isolated filesystem environments for each agent, preventing cross-agent interference while maintaining access to the original codebase.

**Location:** `pkg/sandbox/`

### OverlayFS Architecture

Uses Linux overlayfs kernel feature (with FUSE fallback for rootless operation):

```
┌─────────────────────────────────────────┐
│     Merged View (Agent's workspace)     │
│  (Read-write from agent's perspective)  │
└──────────────┬──────────────────────────┘
               │ overlayfs mount
       ┌───────┴────────┬─────────────┐
       │                │             │
   Upper Dir        Lower Dir     Work Dir
(Agent changes)  (Original repo)  (OverlayFS
   writable        read-only       internal)
```

### Overlay Structure

```go
type Overlay struct {
    ID         string   // Unique hex identifier
    LowerDir   string   // Read-only base (original workdir)
    UpperDir   string   // Writable changes layer
    WorkDir    string   // OverlayFS internal workdir
    MergedDir  string   // Combined view where agent operates
    mounted    bool
    useFuse    bool     // True if FUSE, false if kernel overlay
}
```

**Directory Layout:**

```
{baseDir}/
└── {overlay-id}/
    ├── upper/     # Writable layer (agent changes land here)
    ├── work/      # OverlayFS temporary space
    └── merged/    # Mount point (agent sees this)
```

### Lifecycle

**1. Creation (`NewOverlay`):**

```go
overlay, err := sandbox.NewOverlay(tempDir, workDir)
```

- Generates unique ID (random hex)
- Creates directory structure (`upper/`, `work/`, `merged/`)
- Creates whiteouts for hidden paths (e.g., `.claude/`)

**2. Mounting (`Mount`):**

```go
err := overlay.Mount()
```

- Attempts kernel overlayfs first:
  ```
  mount -t overlay overlay -o lowerdir=X,upperdir=Y,workdir=Z {merged}
  ```
- Falls back to fuse-overlayfs if rootless:
  ```
  fuse-overlayfs -o lowerdir=X,upperdir=Y,workdir=Z {merged}
  ```

**3. Agent Execution:**

Agent runs with `MergedDir` as working directory. All writes go to `UpperDir`, reads fall through to `LowerDir`.

**4. Change Detection (`GetChanges`):**

```go
changes, err := overlay.GetChanges()
```

- Recursively compares `UpperDir` against `LowerDir`
- Returns list of created/modified/deleted files
- Filters out:
  - Whiteout files (`.wh.*`)
  - `.canopy/`, `.beads/` directories
  - Home directory artifacts (`.npm`, `.cache`, etc.)
- Computes SHA256 hashes for deduplication

**5. Git State Extraction (`GetGitState`):**

```go
gitState, err := overlay.GetGitState()
```

- Detects new commits made by the agent
- Extracts patch format for `git am` application

**6. Cleanup:**

```go
err := overlay.Cleanup()
```

- Unmounts filesystem (fusermount or umount)
- Removes all directories (`upper/`, `work/`, `merged/`)
- Called after merge completes or on error

### Hidden and Filtered Paths

**Hidden via Whiteout** (`DefaultHiddenPaths`):
- `.claude` - Prevents agents from seeing parent session settings

**Excluded from Changes** (`HomeExcludedPaths`):
- `.npm`, `.cache`, `.config`, `.local` - Tool caches
- `.claude.json`, `.gitconfig` - Credentials
- `.bash_history`, `.viminfo`, `.lesshst` - Shell state
- `go/` - Go module cache

### Optional Bubblewrap Isolation (`pkg/sandbox/bwrap.go`)

When `--sandbox` flag is used, agents run in full process isolation:

```
┌────────────────────────────────────────┐
│   Bubblewrap Container                 │
│ ┌──────────────────────────────────┐   │
│ │ OverlayFS Mounted as /workspace  │   │
│ │ - User namespace isolation       │   │
│ │ - PID namespace isolation        │   │
│ │ - IPC namespace isolation        │   │
│ │ - All capabilities dropped       │   │
│ │ - Resource limits enforced       │   │
│ └──────────────────────────────────┘   │
└────────────────────────────────────────┘
```

**Resource Limits:**
- Memory: 4GB default
- Processes: 100 default
- Open files: 1024 default

**Configuration:** `.canopy/sandbox.toml`

```toml
[security]
blocked = ["/etc/passwd", "/root"]

[tools]
paths = ["/usr/local/bin"]

[cache]
mounts = ["/home/user/.cache"]

[resources]
max_memory = "4GB"
max_processes = 100
```

## Data Flow Summary

```
1. User runs: canopy run --concurrency 4

2. Orchestrator connects to daemon:
   orchestrator → ipc.NewClient() → Unix socket → Daemon

3. Load ready tasks:
   orchestrator → beads.NewClient() → .beads/db.sqlite

4. Execute agents (parallel, bounded by concurrency):
   scheduler → [Agent 1, 2, 3, 4]
       ↓ (per agent)
       ├── Create overlay sandbox
       ├── Run Claude CLI (optionally in bwrap)
       ├── Detect file changes
       └── Send IPC events (output, commits, etc.)

5. Queue merges (sequential):
   Agent done → MergeQueue.Enqueue()
   ↓
   Processor.Start() loop:
       ├── Dequeue one request
       ├── Apply via git am or file copy
       ├── On conflict: spawn resolver
       ├── Update beads task status
       └── Send IPC merge status

6. Daemon receives events:
   IPC Server → EventBus → RuntimeState
                        → WebSocket Hub → Browser Dashboard
                        → PersistenceManager → SQLite
```

## Key Concurrency Patterns

| Component | Pattern | Use Case |
|-----------|---------|----------|
| `RuntimeState` | `sync.Map` | High-read agent/task state |
| `MergeQueue.pauseState` | `PauseStateMachine` (mutex + cond) | Dual-source pause coordination |
| `Scheduler.results` | `sync.RWMutex` | Iteration via AllResults() |
| `Scheduler.activeOverlays` | `sync.RWMutex` | Iteration via CleanupAll() |
| `Scheduler.agentContexts` | `sync.Map` | Store-then-delete pattern |
| `Scheduler` | `errgroup` + `semaphore` | Bounded parallelism |
| `IPC Client` | `sync.Mutex` + queue | Reconnection buffering |

**PauseStateMachine Details**: The merge queue's pause state machine uses `sync.Mutex` for state transitions and `sync.Cond` for goroutine wakeup. It tracks two independent pause sources (user and resolver) with a 4-state enum (Running, PausedUser, PausedResolver, PausedBoth). The system only processes when in Running state. `WaitUntilRunning()` blocks on the condition variable until both sources have resumed.

See `AGENTS.md` for detailed concurrency guidelines.
