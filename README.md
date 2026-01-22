# Canopy

[![Go](https://github.com/jzila/canopy/actions/workflows/go.yml/badge.svg)](https://github.com/jzila/canopy/actions/workflows/go.yml)
[![Dashboard CI](https://github.com/jzila/canopy/actions/workflows/dashboard.yml/badge.svg)](https://github.com/jzila/canopy/actions/workflows/dashboard.yml)
[![Nix](https://github.com/jzila/canopy/actions/workflows/nix.yml/badge.svg)](https://github.com/jzila/canopy/actions/workflows/nix.yml)

> **⚠️ HERE BE DRAGONS ⚠️**
>
> This is **very early alpha software**. It might eat your commits, corrupt your repo, make questionable life choices with your filesystem, or spontaneously decide that `main` was more of a suggestion than a branch name.
>
> **What could possibly go wrong:**
> - Your repository (we use OverlayFS and merge things automatically)
> - Your git history (we spawn agents that commit things)
> - Your sanity (debugging parallel agent conflicts is a character-building exercise)
> - Anything the agents touch (they're quite enthusiastic)
>
> **Recommended precautions:** Use on toy projects first. Keep backups. Have `git reflog` bookmarked. Consider a cup of tea and a moment of quiet reflection before running `canopy run` on anything you care about.
>
> You have been warned. Proceed with curiosity and caution.

**Parallel agent orchestrator for Claude Code**

![Dashboard](https://github.com/user-attachments/assets/8c0b98bb-9497-44a4-bdc6-73fb4c673893)

Canopy coordinates multiple Claude Code agents working on different tasks simultaneously, each in an isolated sandbox. It handles task scheduling, workspace isolation, change merging, and conflict resolution automatically.

```
Human (You)
  └── Claude Code (Root Agent)
        └── canopy run
              ├── Worker 1 (isolated sandbox) → task A
              ├── Worker 2 (isolated sandbox) → task B
              ├── Worker 3 (isolated sandbox) → task C
              └── ... up to N concurrent workers
```

## Why Canopy?

Without Canopy, Claude Code processes tasks sequentially. A 10-task project takes 10x the time of a single task. Canopy enables:

- **Parallel execution**: Run N tasks simultaneously (default: 4 agents)
- **Workspace isolation**: Each agent works in an isolated copy of the repository
- **Automatic merging**: Changes merge back to the main repo after completion
- **Conflict resolution**: Spawns resolver agents when merge conflicts occur
- **Retry logic**: Automatically retries failed tasks (configurable)
- **Real-time monitoring**: TUI dashboard and WebSocket API for observability

## Requirements

- **Linux or macOS** (Linux uses OverlayFS; macOS uses APFS clones)
- **Go 1.24+** for building
- **Claude Code CLI** (`claude`) installed and authenticated
- **Beads-compatible CLI** (`bd`) for task tracking - any implementation conforming to the [Beads Classic Protocol](https://github.com/jzila/beads-protocol)
- **bubblewrap** (`bwrap`) optional, for full sandbox isolation

## Installation

### Using devenv (Recommended)

If you have [devenv](https://devenv.sh/) installed, the development environment is fully automated:

```bash
# Clone repository
git clone https://github.com/jzila/canopy
cd canopy

# Allow direnv to load the environment automatically
direnv allow

# Or manually enter the devenv shell
devenv shell
```

This provides Go, beads (`bd`), git, sqlite, and fuse-overlayfs with a pre-commit hook that verifies Go compilation.

### Manual Build

```bash
# Clone and build
git clone https://github.com/jzila/canopy
cd canopy
go build -o canopy ./cmd/canopy

# Add to PATH (or move to /usr/local/bin)
export PATH="$PATH:$(pwd)"

# Verify installation
canopy version
```

## Quick Start

### 1. Set up task tracking

Canopy uses [beads](https://github.com/steveyegge/beads) for task management:

```bash
# Initialize beads with a separate sync branch
# IMPORTANT: Must use --branch to avoid overlay conflicts
bd init --branch beads-sync

# Create some tasks
bd create --title "Implement user authentication" --type feature --priority 1
bd create --title "Add input validation" --type task --priority 2
bd create --title "Write unit tests" --type task --priority 2

# View ready tasks (no blockers)
bd ready
```

### 2. Run parallel agents

```bash
# Execute all ready tasks with 4 concurrent agents (default)
canopy run

# Or with more concurrency
canopy run -c 8

# Dry run to preview what would execute
canopy run --dry-run

# With full sandbox isolation (requires bwrap)
canopy run --sandbox
```

### 3. Monitor progress

```bash
# Start the daemon with TUI dashboard
canopy daemon --tui

# Or check daemon status
canopy daemon status

# View logs
canopy daemon logs -f
```

## Commands

| Command | Description |
|---------|-------------|
| `canopy run` | Execute ready tasks in parallel |
| `canopy daemon` | Start HTTP/IPC server for monitoring |
| `canopy daemon --tui` | Start daemon with TUI dashboard |
| `canopy daemon status` | Check if daemon is running |
| `canopy daemon logs` | View daemon logs |
| `canopy init` | Interactive sandbox configuration |
| `canopy history` | View past run records |
| `canopy stats` | Show run statistics |
| `canopy help --agent` | Detailed workflow for AI agents |

### `canopy run` Options

```bash
canopy run [flags]
  -c, --concurrency N     Max concurrent agents (default: 4)
  -o, --output DIR        Output directory for merged results
  --sandbox               Enable bubblewrap isolation (requires bwrap)
  --max-retries N         Retry limit (default: 3, -1 for infinite)
  --dry-run               Preview without executing
  --prompt TEXT           Soft guidance for task selection
  --max-priority N        Hard filter by priority (0-4)
  -v, --verbose           Verbose output
```

The daemon is automatically started if not running. The run command requires daemon connectivity for monitoring and real-time updates.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      Canopy Orchestrator                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  bd ready ────┬──────────────────────────────────────────────── │
│               │                                                 │
│               ▼                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                     Scheduler                           │    │
│  │  - Bounded concurrency (semaphore)                      │    │
│  │  - Per-agent contexts for kill support                  │    │
│  └──────────────┬──────────────────────────────────────────┘    │
│                 │                                               │
│    ┌────────────┼────────────┐                                  │
│    ▼            ▼            ▼                                  │
│ ┌──────┐    ┌──────┐    ┌──────┐                                │
│ │Agent1│    │Agent2│    │Agent3│   ... up to N                  │
│ │Overlay│   │Overlay│   │Overlay│                               │
│ └──┬───┘    └──┬───┘    └──┬───┘                                │
│    │           │           │                                    │
│    └───────────┴───────────┘                                    │
│                │                                                │
│                ▼                                                │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │              Merge Queue (Sequential)                   │    │
│  │  - Last-writer-wins for non-conflicting changes         │    │
│  │  - Spawns resolver agents for conflicts                 │    │
│  └─────────────────────────────────────────────────────────┘    │
│                │                                                │
│                ▼                                                │
│           bd done (mark tasks complete)                         │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Workspace Isolation

Each agent operates in an isolated workspace. The isolation mechanism varies by platform:

**Linux (OverlayFS):** Uses copy-on-write filesystem for efficient isolation:

```
┌──────────────────────────────────────────────────────────────┐
│                MergedDir (Agent's View)                      │
│  ~/.cache/canopy/overlays/{id}/merged                        │
├──────────────────────────────────────────────────────────────┤
│                UpperDir (Agent's Changes)                    │
│  ~/.cache/canopy/overlays/{id}/upper                         │
├──────────────────────────────────────────────────────────────┤
│                LowerDir (Original Repo)                      │
│  /home/user/project (read-only)                              │
└──────────────────────────────────────────────────────────────┘
```

**macOS (APFS Clone):** Uses copy-on-write clones for efficient isolation:

```
┌──────────────────────────────────────────────────────────────┐
│                  MergedDir (Agent View)                      │
│  ~/.cache/canopy/overlays/{id}/merged                        │
│  (APFS clone of original repo - COW copy)                    │
└──────────────────────────────────────────────────────────────┘
                           │
                           │ clonefile(2) / cp -c
                           ▼
┌──────────────────────────────────────────────────────────────┐
│              Original Repo (Unmodified)                      │
│  /Users/user/project (read-only reference)                   │
└──────────────────────────────────────────────────────────────┘
```

On both platforms, agents cannot see each other's changes until merge.

## Configuration

### Sandbox Configuration (`.canopy/sandbox.toml`)

Run `canopy init` to generate an interactive configuration:

```toml
[sandbox]
enabled = true
network = "allow"

[resources]
max_memory = "4GB"
max_processes = 100
max_open_files = 1024

[paths]
# Tool paths (read-only)
read_only = ["~/.cargo", "~/.nvm", "~/.rustup"]

# Config files (copied to overlay)
copy_configs = ["~/.gitconfig", "~/.npmrc"]

# Shared caches (read-write)
cache_mounts = ["~/.npm", "~/.cargo/registry"]

[security]
# Always blocked (in addition to built-in blocklist)
blocked = ["~/.ssh", "~/.aws", "~/.gnupg"]
```

### Security

**Default protections (always on):**
- Environment filtering: only `ANTHROPIC_*`, `PATH`, `LANG`, `LC_*`, `TERM`, `TMPDIR`, `TZ`
- `.claude/` directory hidden from workers
- Process groups: workers die if orchestrator dies

**With `--sandbox` flag (requires bwrap):**
- Full namespace isolation (user, PID, IPC, UTS)
- All capabilities dropped
- Resource limits: 4GB memory, 100 processes, 1024 file descriptors
- Read-only system mounts
- Security blocklist: `~/.ssh`, `~/.aws`, `~/.gnupg`, etc. never exposed

## Daemon and Monitoring

The daemon provides real-time monitoring:

```bash
# Start daemon (foreground)
canopy daemon

# Start daemon with TUI
canopy daemon --tui

# Start in background
canopy daemon start

# Connect TUI to running daemon
canopy daemon --tui --daemon-addr localhost:8080

# View logs
canopy daemon logs -f
```

### HTTP API

The daemon exposes a REST API for monitoring and control:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/state` | GET | Full runtime state snapshot |
| `/api/agents` | GET | List all agents |
| `/api/agents` | PATCH | Update agent (archive) |
| `/api/agents/:id/kill` | POST | Terminate a running agent |
| `/api/tasks` | GET | List all tasks |
| `/api/tasks` | POST | Create a new task |
| `/api/tasks` | PATCH | Update task status |
| `/api/stats` | GET | Current run statistics |
| `/api/merge-queue` | GET | Merge queue state |
| `/api/orch/pause` | POST | Pause orchestrator |
| `/api/orch/resume` | POST | Resume orchestrator |
| `/api/runs` | GET | List historical runs |
| `/api/runs/:id` | GET | Get run details |
| `/api/runs/:id/agents` | GET | Get agents for a run |
| `/api/stats/history` | GET | Aggregate historical stats |
| `/api/repositories` | GET | List repositories |
| `/api/repositories/:id` | GET | Get repository details |
| `/api/repositories/:id/activate` | POST | Set active repository |
| `/api/beads/sync` | POST | Sync tasks from beads |
| `/ws` | GET | WebSocket for real-time updates |
| `/metrics` | GET | Prometheus metrics (see [Metrics](#prometheus-metrics)) |

See [Commands](docs/COMMANDS.md#http-api-endpoints) for detailed request/response formats.

### Prometheus Metrics

Canopy exposes Prometheus-compatible metrics at `/metrics` for monitoring and alerting:

```bash
# Fetch metrics
curl http://localhost:8080/metrics
```

**Available metrics:**

| Metric | Type | Description |
|--------|------|-------------|
| `canopy_active_agents` | Gauge | Current agents by status (running/completed/failed) |
| `canopy_merge_queue_depth` | Gauge | Items waiting in merge queue |
| `canopy_overlay_mounts` | Gauge | Active overlay filesystem mounts |
| `canopy_task_duration_seconds` | Histogram | Task execution duration by status |
| `canopy_merge_conflicts_total` | Counter | Total merge conflicts encountered |
| `canopy_resolver_success_total` | Counter | Successful conflict resolutions |
| `canopy_resolver_failure_total` | Counter | Failed conflict resolutions |
| `canopy_ipc_messages_total` | Counter | IPC messages by type |

A pre-configured Grafana dashboard is available at `docs/grafana/canopy-dashboard.json`.

## Persistence

All state lives in `~/.cache/canopy/` (or `$XDG_CACHE_HOME/canopy/`):

```
~/.cache/canopy/
├── runs.db           # SQLite database for run history
├── repositories.json # Repository identity registry
├── daemon.log        # Daemon log file
├── history/          # JSON run records (backup)
└── overlays/         # Temporary overlay mounts (auto-cleaned)
```

## Documentation

- [Architecture](docs/ARCHITECTURE.md) - Detailed system design
- [Getting Started](docs/GETTING-STARTED.md) - Step-by-step tutorial
- [Commands](docs/COMMANDS.md) - Complete CLI reference
- [Configuration](docs/CONFIGURATION.md) - Configuration options
- [Dashboard](docs/DASHBOARD.md) - Web dashboard user guide
- [Sandbox Design](docs/SANDBOX-DESIGN.md) - Security architecture

## Integration with Beads

Canopy requires a CLI tool (`bd`) that implements the [Beads Classic Protocol](https://github.com/jzila/beads-protocol)—a minimal git-backed task tracking interface. The protocol defines the commands Canopy uses for task discovery, status updates, and dependency management.

**Known implementations:**
- [beads](https://github.com/steveyegge/beads) - Original Go implementation
- [beads_rust](https://github.com/Dicklesworthstone/beads_rust) - Rust implementation

Any implementation conforming to the protocol will work with Canopy.

```bash
# Create tasks with dependencies
bd create --title "Design API" --type task --priority 0     # → canopy-a1b2
bd create --title "Implement API" --type task --priority 1  # → canopy-c3d4
bd dep add canopy-c3d4 canopy-a1b2  # Implement depends on Design

# Canopy respects dependencies
canopy run  # Design runs first, then Implement
```

## License

MIT License - see [LICENSE](LICENSE) for details.
