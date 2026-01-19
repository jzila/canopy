# Canopy CLI Reference

Complete reference for all Canopy commands and options.

## Global Options

These options are available on all commands:

| Option | Short | Description |
|--------|-------|-------------|
| `--verbose` | `-v` | Enable verbose output |
| `--workdir` | `-w` | Set working directory (default: current directory) |
| `--help` | `-h` | Show help for command |

## Commands

### canopy run

Execute ready tasks from beads in parallel.

```bash
canopy run [flags]
```

#### Options

| Option | Short | Default | Description |
|--------|-------|---------|-------------|
| `--concurrency` | `-c` | `4` | Maximum concurrent agents |
| `--output` | `-o` | workdir | Output directory for merged results |
| `--sandbox` | | `false` | Enable bubblewrap isolation |
| `--max-retries` | | `3` | Retry limit (-1 for infinite) |
| `--dry-run` | | `false` | Preview without executing |
| `--prompt` | | | Soft guidance for task selection |
| `--max-priority` | | `-1` | Hard filter by priority (0-4) |

#### Examples

```bash
# Basic execution with defaults
canopy run

# High concurrency
canopy run -c 8

# Preview what would run
canopy run --dry-run

# Full sandbox isolation
canopy run --sandbox

# No retries on failure
canopy run --max-retries 0

# Filter by priority (P0 and P1 only)
canopy run --max-priority 1

# Soft filter with prompt
canopy run --prompt "Only work on frontend tasks"
```

#### Behavior

1. Connects to daemon (auto-starts if not running)
2. Fetches ready tasks from `bd ready`
3. Spawns agents in isolated OverlayFS sandboxes
4. Executes `claude --print --output-format json`
5. Merges changes sequentially
6. Marks completed tasks with `bd done`
7. Repeats until no more ready tasks

---

### canopy daemon

Start the HTTP/IPC daemon server.

```bash
canopy daemon [flags]
canopy daemon [command]
```

#### Options

| Option | Default | Description |
|--------|---------|-------------|
| `--port` | `8080` | HTTP server port |
| `--ipc-socket` | runtime dir | Unix socket path for IPC |
| `--dev` | `false` | Enable development mode |
| `--tui` | `false` | Enable TUI dashboard |
| `--daemon-addr` | | Connect to daemon at address (with `--tui`) |

#### Subcommands

| Command | Description |
|---------|-------------|
| `status` | Check if daemon is running |
| `start` | Start daemon in background |
| `stop` | Stop running daemon |
| `restart` | Stop and restart daemon |
| `logs` | View daemon logs |

#### Examples

```bash
# Start daemon (foreground)
canopy daemon

# Start with TUI dashboard
canopy daemon --tui

# Custom port
canopy daemon --port 9090

# Background mode
canopy daemon start

# Check status
canopy daemon status

# View logs
canopy daemon logs -f -n 100

# Stop daemon
canopy daemon stop

# Restart
canopy daemon restart

# Connect TUI to remote daemon
canopy daemon --tui --daemon-addr myserver:8080
```

---

### canopy daemon status

Check daemon running status.

```bash
canopy daemon status
```

#### Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Daemon is running |
| `1` | Daemon is not running |

#### Example

```bash
if canopy daemon status; then
    echo "Daemon is running"
else
    canopy daemon start
fi
```

---

### canopy daemon start

Start daemon as background process.

```bash
canopy daemon start
```

Starts the daemon detached with output redirected to `~/.cache/canopy/daemon.log`.

---

### canopy daemon stop

Stop running daemon gracefully.

```bash
canopy daemon stop
```

Sends SIGTERM and waits up to 5 seconds for clean shutdown.

---

### canopy daemon restart

Stop and restart daemon.

```bash
canopy daemon restart
```

Equivalent to `canopy daemon stop && canopy daemon start`.

---

### canopy daemon logs

View daemon log output.

```bash
canopy daemon logs [flags]
```

#### Options

| Option | Short | Default | Description |
|--------|-------|---------|-------------|
| `--follow` | `-f` | `false` | Follow log output (like `tail -f`) |
| `--lines` | `-n` | `50` | Number of lines to show |

#### Examples

```bash
# Last 50 lines
canopy daemon logs

# Last 100 lines
canopy daemon logs -n 100

# Follow mode
canopy daemon logs -f

# Follow with context
canopy daemon logs -f -n 20
```

---

### canopy init

Initialize sandbox and validation configuration.

```bash
canopy init [flags]
```

#### Options

| Option | Default | Description |
|--------|---------|-------------|
| `--reconfigure` | `false` | Re-run detection and reconfigure (overwrites existing config) |
| `--non-interactive` | `false` | Accept all defaults without prompting |
| `--yes` / `-y` | `false` | Alias for `--non-interactive` |
| `--skip-beads` | `false` | Skip beads initialization |
| `--detect` | `false` | Output JSON detection results (does not create config) |
| `--agent` | `false` | Output questionnaire JSON for agentic integration |
| `--apply` | | Apply answers from agentic questionnaire JSON |

#### Behavior (Interactive Mode)

1. Initializes beads for task tracking (if not already done)
2. Scans project for type (Node, Rust, Go, Python, etc.)
3. Discovers installed tools and versions
4. Detects config files and caches
5. Detects validation commands (build, test)
6. Generates `.canopy/sandbox.toml` and `.canopy/validation.toml`
7. Shows recommended configuration
8. Prompts for confirmation

#### Example (Interactive)

```bash
canopy init
```

Output:
```
Canopy Sandbox Bootstrapper
===========================

Scanning project for tool requirements...

Detected Project Type: Node.js + Rust (monorepo)

Tools Found:
  [x] node v20.10.0 (~/.nvm/versions/node/v20.10.0)
  [x] cargo 1.75.0 (~/.cargo/bin/cargo)
  [x] git 2.43.0 (/usr/bin/git)

Recommended Configuration:
──────────────────────────
Read-Only Tool Paths:
  ~/.nvm              Node.js version manager
  ~/.cargo            Cargo binaries

Config Copies:
  ~/.npmrc            npm registry config
  ~/.gitconfig        Git identity

Proceed with this configuration? [Y/n/customize]
```

---

### canopy init --detect

Output project detection results as JSON without creating config files. Useful for tooling integration, CI/CD pipelines, and pre-flight checks.

```bash
canopy init --detect
```

#### JSON Output Structure

```json
{
  "project": {
    "type": "Go",
    "root": "/path/to/project",
    "markers": ["go.mod", "go.sum"]
  },
  "sandbox": {
    "tools": ["~/.goenv", "~/.asdf"],
    "configs": ["~/.gitconfig"],
    "caches": ["~/.cache/go-build"]
  },
  "validation": {
    "suggested": [
      {"name": "build", "command": "go build ./...", "confidence": "high"},
      {"name": "test", "command": "go test ./...", "confidence": "high"}
    ],
    "detected_files": {
      "justfile": false,
      "makefile": false,
      "package_json": false,
      "cargo_toml": false,
      "go_mod": true,
      "pyproject_toml": false,
      "gemfile": false,
      "pom_xml": false,
      "build_gradle": false
    }
  }
}
```

#### Output Fields

| Field | Description |
|-------|-------------|
| `project.type` | Human-readable project type (e.g., "Go", "Node.js + Python") |
| `project.root` | Absolute path to project root |
| `project.markers` | Files that identified the project type |
| `sandbox.tools` | Read-only tool paths to expose |
| `sandbox.configs` | Config files to copy into sandbox |
| `sandbox.caches` | Cache directories for read-write mount |
| `validation.suggested` | Detected validation commands with confidence levels |
| `validation.detected_files` | Build system files found in project |

---

### canopy init --agent

Output a questionnaire JSON for AI agent integration. The agent can present these questions via `AskUserQuestion` tool, then apply answers with `--apply`.

```bash
canopy init --agent
```

#### JSON Output Structure

```json
{
  "detection": { /* same as --detect output */ },
  "questions": [
    {
      "id": "confirm_validation",
      "question": "I detected build and test commands. Enable post-merge validation?",
      "type": "single_select",
      "options": [
        {"value": "yes", "label": "Yes, validate after each merge"},
        {"value": "no", "label": "No, skip validation"}
      ],
      "default": "yes",
      "depends_on": null
    },
    {
      "id": "validation_mode",
      "question": "How should validation failures be handled?",
      "type": "single_select",
      "options": [
        {"value": "strict", "label": "Strict - revert merge on failure"},
        {"value": "lenient", "label": "Lenient - file issue, keep merge"}
      ],
      "default": "strict",
      "depends_on": {"question_id": "confirm_validation", "value": "yes"}
    },
    {
      "id": "validation_steps",
      "question": "Which validation steps should run after each merge?",
      "type": "multi_select",
      "options": [
        {"value": "build", "label": "build (go build ./...)"},
        {"value": "test", "label": "test (go test ./...)"}
      ],
      "depends_on": {"question_id": "confirm_validation", "value": "yes"}
    },
    {
      "id": "extra_commands",
      "question": "Any additional validation commands? (comma-separated, or leave empty)",
      "type": "freeform",
      "depends_on": {"question_id": "confirm_validation", "value": "yes"}
    }
  ]
}
```

#### Question Types

| Type | Description |
|------|-------------|
| `single_select` | User picks one option from the list |
| `multi_select` | User picks multiple options |
| `freeform` | User provides free-form text input |

#### Conditional Questions

Questions can depend on previous answers via the `depends_on` field:
- If `depends_on` is `null`, the question is always shown
- If `depends_on.question_id` and `depends_on.value` match a previous answer, show the question
- Agents should skip questions whose dependencies are not met

---

### canopy init --apply

Apply answers from an agentic questionnaire to create configuration files.

```bash
canopy init --apply '<json-answers>'
```

#### Input Format

```json
{
  "confirm_validation": "yes",
  "validation_mode": "strict",
  "validation_steps": ["build", "test"],
  "extra_commands": "golangci-lint run"
}
```

#### Answer Fields

| Field | Type | Description |
|-------|------|-------------|
| `confirm_validation` | `"yes"` or `"no"` | Whether to enable validation |
| `validation_mode` | `"strict"` or `"lenient"` | How to handle validation failures |
| `validation_steps` | `string[]` | Array of step names to enable |
| `extra_commands` | `string` | Comma-separated additional commands |

#### Output

```json
{
  "success": true,
  "files_created": [".canopy/sandbox.toml", ".canopy/validation.toml"]
}
```

#### Files Created

- **`.canopy/sandbox.toml`**: Always created with detected tool/config/cache paths
- **`.canopy/validation.toml`**: Only created if `confirm_validation` is `"yes"`

#### Example Workflow

```bash
# Step 1: Get questionnaire
questionnaire=$(canopy init --agent)

# Step 2: Agent presents questions to user, collects answers

# Step 3: Apply answers
canopy init --apply '{"confirm_validation":"yes","validation_mode":"strict","validation_steps":["build","test"]}'
```

---

### canopy kill

Terminate a running worker agent.

```bash
canopy kill <agent-id> [flags]
```

#### Arguments

| Argument | Description |
|----------|-------------|
| `<agent-id>` | Agent ID, task ID, or partial agent ID prefix |

#### Options

| Option | Short | Default | Description |
|--------|-------|---------|-------------|
| `--force` | `-f` | `false` | Kill without confirmation prompt |

#### Behavior

1. Queries the daemon for running agents
2. Matches the target by full agent ID, task ID, or prefix
3. Prompts for confirmation (unless `--force`)
4. Sends kill signal to terminate the agent

#### Examples

```bash
# Kill an agent by its full agent ID
canopy kill agent-abc12345-beads-xyz

# Kill an agent by task ID (matches the running agent for that task)
canopy kill beads-xyz

# Kill by partial ID prefix
canopy kill agent-abc

# Force kill without confirmation
canopy kill --force agent-abc12345-beads-xyz
```

#### Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Agent killed successfully |
| `1` | Agent not found or not running |

---

### canopy ps

Show running canopy processes.

```bash
canopy ps [flags]
```

#### Options

| Option | Default | Description |
|--------|---------|-------------|
| `--json` | `false` | Output as JSON for scripting |
| `--daemon-only` | `false` | Show only daemon status |
| `--workers-only` | `false` | Show only worker agents |

#### Output Sections

**Daemon Section:**
- Running status and PID
- Port and socket path
- Uptime
- Paused state
- Active repository
- Task statistics (total, running, completed, failed)
- Accumulated cost

**Workers Section:**
- Agent ID
- Task ID
- Task title
- Status (running, starting, merge status)
- Duration

#### Examples

```bash
# Show all running canopy processes
canopy ps

# Show only the daemon status
canopy ps --daemon-only

# Show only worker agents
canopy ps --workers-only

# Output as JSON for scripting
canopy ps --json
```

#### Example Output

```
DAEMON
  Status: Running (PID 12345)
  Port: 8080
  Socket: /run/user/1000/canopy.sock
  Uptime: 2h 15m
  Tasks: 10 total (2 running, 7 completed, 1 failed)
  Cost: $1.23

WORKERS
  AGENT ID      TASK ID           STATUS      DURATION    TITLE
  agt-abc123    beads-xyz         running     5m 32s      Implement feature X
  agt-def456    beads-uvw         merging     2m 10s      Fix bug in module Y
```

---

### canopy history

View past run records.

```bash
canopy history [flags]
```

#### Options

| Option | Short | Default | Description |
|--------|-------|---------|-------------|
| `--limit` | `-n` | `10` | Number of runs to show |
| `--json` | | `false` | Output as JSON |

#### Example

```bash
canopy history -n 5
```

Output:
```
RUN ID     DATE        TASKS  SUCCESS  FAILED  COST
abc123     2024-01-15  5      5        0       $0.45
def456     2024-01-15  3      2        1       $0.28
ghi789     2024-01-14  8      8        0       $0.92
```

---

### canopy stats

Show run statistics.

```bash
canopy stats [flags]
```

#### Options

| Option | Default | Description |
|--------|---------|-------------|
| `--json` | `false` | Output as JSON |

#### Example

```bash
canopy stats
```

Output:
```
Canopy Statistics
─────────────────
Total runs:        42
Total tasks:       187
Success rate:      94.1%
Total cost:        $12.34
Total tokens:      2,456,789
Avg tokens/task:   13,138
```

---

### canopy help

Show help for commands.

```bash
canopy help [command]
canopy help --agent
```

#### Options

| Option | Description |
|--------|-------------|
| `--agent` | Show detailed workflow explanation for AI agents |

The `--agent` flag outputs a comprehensive guide for AI agents orchestrating work with Canopy, including architecture, workflow, and examples.

---

### canopy sync-beads

Sync beads tasks with the daemon.

```bash
canopy sync-beads [repo-path] [flags]
```

#### Arguments

| Argument | Description |
|----------|-------------|
| `[repo-path]` | Optional path to repository (default: current directory) |

#### Options

| Option | Default | Description |
|--------|---------|-------------|
| `--json` | `false` | Output as JSON for scripting |
| `--quiet` | `false` | Only output errors |

#### Behavior

Triggers the daemon to re-read tasks from the beads database (`.beads/` directory) and update its internal state. Useful when:
- Beads files have been manually modified
- Ensuring the daemon has the latest task information
- After external changes to the beads database

#### Examples

```bash
# Sync beads for the current directory
canopy sync-beads

# Sync beads for a specific repository
canopy sync-beads /path/to/repo

# Output as JSON for scripting
canopy sync-beads --json

# Quiet mode - only output errors
canopy sync-beads --quiet
```

#### Example Output

```
Synced 15 tasks from my-project
```

#### JSON Output

```json
{
  "success": true,
  "synced": 15,
  "repo_id": "abc123",
  "repo_name": "my-project"
}
```

#### Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Sync completed successfully |
| `1` | Error (daemon not running, connection failed, etc.) |

---

### canopy version

Show version information.

```bash
canopy version
```

---

## Environment Variables

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Claude API key (passed to agents) |
| `XDG_CACHE_HOME` | Override cache directory (default: `~/.cache`) |
| `XDG_RUNTIME_DIR` | Override runtime directory (Linux only) |
| `TMPDIR` | Temp directory for overlays |

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | General error |
| `2` | Invalid arguments |
| `130` | Interrupted (Ctrl+C) |

## HTTP API Endpoints

When the daemon is running, these endpoints are available for monitoring and control.

### State & Monitoring

#### GET /api/state

Returns the full runtime state snapshot including all agents, tasks, and statistics.

**Response:**
```json
{
  "agents": {
    "agent-abc123-beads-xyz": {
      "id": "agent-abc123-beads-xyz",
      "task_id": "beads-xyz",
      "status": "running",
      "start_time": "2024-01-15T10:30:00Z",
      "merge_status": "none"
    }
  },
  "tasks": {
    "beads-xyz": {
      "id": "beads-xyz",
      "title": "Implement feature X",
      "status": "in_progress",
      "priority": 1
    }
  },
  "stats": {
    "total_tasks": 10,
    "completed_tasks": 5,
    "failed_tasks": 1,
    "running_tasks": 2
  },
  "is_paused": false,
  "active_repo_id": "abc123"
}
```

#### GET /api/stats

Returns current run statistics.

**Response:**
```json
{
  "total_tasks": 10,
  "completed_tasks": 5,
  "failed_tasks": 1,
  "running_tasks": 2,
  "total_cost_usd": 1.23,
  "total_tokens": 45678
}
```

#### GET /api/merge-queue

Returns the current state of the merge queue.

**Response:**
```json
{
  "completed": [
    {
      "task_id": "beads-abc",
      "agent_id": "agent-123",
      "timestamp": "2024-01-15T10:30:00Z",
      "success": true
    }
  ],
  "resolvers": [
    {
      "parent_task_id": "beads-xyz",
      "resolver_task_id": "resolver-xyz",
      "parent_agent_id": "agent-456",
      "resolver_agent_id": "agent-789",
      "status": "running"
    }
  ],
  "pending": [
    {
      "task_id": "beads-def",
      "agent_id": "agent-111",
      "position": 0
    }
  ],
  "active_workers": [
    {
      "agent_id": "agent-222",
      "task_id": "beads-ghi",
      "status": "running"
    }
  ],
  "is_paused": false,
  "is_paused_by_user": false,
  "is_paused_by_agent": false,
  "pause_state": "running",
  "queue_length": 1
}
```

---

### Agents

#### GET /api/agents

Returns a list of all agents.

**Response:**
```json
[
  {
    "id": "agent-abc123-beads-xyz",
    "task_id": "beads-xyz",
    "status": "running",
    "start_time": "2024-01-15T10:30:00Z",
    "merge_status": "none",
    "archived": false
  }
]
```

#### PATCH /api/agents?id=\<agent-id\>

Updates an agent (currently supports archiving).

**Request:**
```json
{
  "archived": true
}
```

**Response:**
```json
{
  "id": "agent-abc123-beads-xyz",
  "archived": true
}
```

#### POST /api/agents/:id/kill

Terminates a running agent.

**Example:**
```bash
curl -X POST http://localhost:8080/api/agents/agent-abc123-beads-xyz/kill
```

**Response:**
```json
{
  "status": "killed",
  "agent_id": "agent-abc123-beads-xyz"
}
```

---

### Tasks

#### GET /api/tasks

Returns a list of all tasks.

**Response:**
```json
[
  {
    "id": "beads-xyz",
    "title": "Implement feature X",
    "status": "pending",
    "priority": 1,
    "archived": false
  }
]
```

#### POST /api/tasks

Creates a new task via the beads client.

**Request:**
```json
{
  "title": "New feature",
  "description": "Optional description",
  "priority": 2,
  "dependencies": ["beads-abc"]
}
```

**Response:**
```json
{
  "id": "beads-new123",
  "title": "New feature",
  "status": "created"
}
```

#### PATCH /api/tasks?id=\<task-id\>

Updates a task's status or archived state.

**Request:**
```json
{
  "status": "in_progress",
  "archived": false
}
```

**Response:**
```json
{
  "id": "beads-xyz",
  "status": "in_progress"
}
```

**Valid status values:** `pending`, `in_progress`, `completed`, `failed`

---

### Orchestration Control

#### POST /api/orch/pause

Pauses the orchestrator. No new tasks will be started, and the merge queue will pause.

**Example:**
```bash
curl -X POST http://localhost:8080/api/orch/pause
```

**Response:**
```json
{
  "status": "paused",
  "pause_state": "paused_by_user"
}
```

#### POST /api/orch/resume

Resumes the orchestrator.

**Example:**
```bash
curl -X POST http://localhost:8080/api/orch/resume
```

**Response:**
```json
{
  "status": "resumed",
  "pause_state": "running"
}
```

If a resolver agent is still active, the response may indicate partial resume:
```json
{
  "status": "partially_resumed",
  "pause_state": "paused_by_resolver",
  "still_paused_by_resolver": true
}
```

---

### Run History

#### GET /api/runs

Lists historical runs with filtering and pagination.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `since` | string | Filter runs after this time (ISO 8601 or duration like `7d`, `24h`) |
| `before` | string | Filter runs before this time |
| `status` | string | Filter by status: `running`, `completed`, `failed`, `cancelled` |
| `repo_id` | string | Filter by repository ID |
| `limit` | int | Maximum number of results (default: 50) |
| `offset` | int | Pagination offset |

**Example:**
```bash
curl "http://localhost:8080/api/runs?since=7d&status=completed&limit=10"
```

**Response:**
```json
{
  "runs": [
    {
      "id": "run-abc123",
      "repo_id": "repo-xyz",
      "status": "completed",
      "start_time": "2024-01-15T10:00:00Z",
      "end_time": "2024-01-15T10:30:00Z",
      "total_agents": 5,
      "completed_agents": 5,
      "failed_agents": 0,
      "total_cost_usd": 0.45
    }
  ],
  "total": 42,
  "has_more": true
}
```

#### GET /api/runs/:id

Returns details for a specific run including all agents.

**Example:**
```bash
curl http://localhost:8080/api/runs/run-abc123
```

**Response:**
```json
{
  "id": "run-abc123",
  "repo_id": "repo-xyz",
  "status": "completed",
  "start_time": "2024-01-15T10:00:00Z",
  "end_time": "2024-01-15T10:30:00Z",
  "agents": [
    {
      "id": "agent-1",
      "task_id": "beads-xyz",
      "status": "completed",
      "cost_usd": 0.15
    }
  ]
}
```

#### GET /api/runs/:id/agents

Returns only the agents for a specific run.

**Example:**
```bash
curl http://localhost:8080/api/runs/run-abc123/agents
```

**Response:**
```json
[
  {
    "id": "agent-1",
    "task_id": "beads-xyz",
    "status": "completed",
    "start_time": "2024-01-15T10:05:00Z",
    "end_time": "2024-01-15T10:15:00Z",
    "cost_usd": 0.15
  }
]
```

#### GET /api/stats/history

Returns aggregate historical statistics.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `since` | string | Aggregate stats since this time (ISO 8601 or duration) |
| `repo_id` | string | Filter by repository ID |

**Example:**
```bash
curl "http://localhost:8080/api/stats/history?since=30d&repo_id=abc123"
```

**Response:**
```json
{
  "total_runs": 42,
  "total_agents": 187,
  "completed_agents": 175,
  "failed_agents": 12,
  "total_cost_usd": 12.34,
  "total_tokens": 2456789,
  "avg_duration_seconds": 245.5
}
```

---

### Repository Management

#### GET /api/repositories

Lists all registered repositories.

**Response:**
```json
{
  "repositories": [
    {
      "id": "abc123",
      "path": "/home/user/project",
      "name": "project",
      "created_at": "2024-01-01T00:00:00Z",
      "is_active": true
    }
  ],
  "active_repo_id": "abc123"
}
```

#### GET /api/repositories/:id

Returns details for a specific repository including statistics.

**Example:**
```bash
curl http://localhost:8080/api/repositories/abc123
```

**Response:**
```json
{
  "id": "abc123",
  "path": "/home/user/project",
  "name": "project",
  "created_at": "2024-01-01T00:00:00Z",
  "stats": {
    "total_runs": 42,
    "total_tasks": 187,
    "total_cost_usd": 12.34
  }
}
```

#### POST /api/repositories/:id/activate

Sets the specified repository as the active repository.

**Example:**
```bash
curl -X POST http://localhost:8080/api/repositories/abc123/activate
```

**Response:**
```json
{
  "id": "abc123",
  "path": "/home/user/project",
  "name": "project",
  "created_at": "2024-01-01T00:00:00Z",
  "is_active": true
}
```

---

### Beads Operations

#### POST /api/beads/sync

Syncs tasks from the beads database into the daemon's runtime state.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `repo` | string | Optional repository ID or path. Defaults to active repository. |

**Example:**
```bash
# Sync active repository
curl -X POST http://localhost:8080/api/beads/sync

# Sync specific repository
curl -X POST "http://localhost:8080/api/beads/sync?repo=abc123"
```

**Response:**
```json
{
  "synced": 15,
  "repo_id": "abc123",
  "repo_name": "my-project"
}
```

---

### WebSocket

#### GET /ws

Upgrades to WebSocket connection for real-time event streaming.

**Events received:**

| Event Type | Description |
|------------|-------------|
| `state_sync` | Full state snapshot (sent on connection) |
| `agent_started` | Agent has started running |
| `agent_completed` | Agent finished successfully |
| `agent_failed` | Agent failed |
| `task_updated` | Task status changed |
| `orch_paused` | Orchestrator paused |
| `orch_resumed` | Orchestrator resumed |
| `merge_started` | Merge operation started |
| `merge_completed` | Merge operation completed |
| `merge_conflict` | Merge conflict detected |

**Example event:**
```json
{
  "type": "agent_completed",
  "timestamp": "2024-01-15T10:30:00Z",
  "payload": {
    "agent_id": "agent-abc123",
    "task_id": "beads-xyz",
    "status": "completed",
    "cost_usd": 0.15
  }
}
```

---

### Metrics

#### GET /metrics

Prometheus-compatible metrics endpoint. See [Prometheus Metrics](#prometheus-metrics) for details.

---

## Prometheus Metrics

Canopy exposes Prometheus-compatible metrics at the `/metrics` endpoint for monitoring, alerting, and observability integration.

### Accessing Metrics

```bash
# Fetch metrics from the daemon
curl http://localhost:8080/metrics

# With custom port
curl http://localhost:9090/metrics
```

### Available Metrics

All metrics use the `canopy_` namespace prefix.

#### Gauges (Current State)

| Metric | Labels | Description |
|--------|--------|-------------|
| `canopy_active_agents` | `status` | Current number of agents by status (`running`, `completed`, `failed`) |
| `canopy_merge_queue_depth` | - | Number of items waiting in the merge queue |
| `canopy_overlay_mounts` | - | Number of active overlay filesystem mounts |

#### Histograms (Distributions)

| Metric | Labels | Buckets | Description |
|--------|--------|---------|-------------|
| `canopy_task_duration_seconds` | `status` | 1, 5, 10, 30, 60, 300, 600, 1800, 3600 | Task execution duration in seconds |
| `canopy_resolver_duration_seconds` | - | 10, 30, 60, 120, 300, 600, 900, 1200 | Conflict resolver execution duration |

#### Counters (Cumulative)

| Metric | Labels | Description |
|--------|--------|-------------|
| `canopy_merge_conflicts_total` | - | Total merge conflicts encountered |
| `canopy_resolver_success_total` | - | Total successful conflict resolutions |
| `canopy_resolver_failure_total` | - | Total failed conflict resolutions |
| `canopy_ipc_messages_total` | `type` | Total IPC messages processed by message type |

### Example Prometheus Queries

```promql
# Current running agents
canopy_active_agents{status="running"}

# Merge queue depth
canopy_merge_queue_depth

# Task success rate (5m window)
sum(rate(canopy_task_duration_seconds_count{status="success"}[5m])) /
sum(rate(canopy_task_duration_seconds_count[5m]))

# Task duration percentiles
histogram_quantile(0.50, sum(rate(canopy_task_duration_seconds_bucket[5m])) by (le))
histogram_quantile(0.95, sum(rate(canopy_task_duration_seconds_bucket[5m])) by (le))
histogram_quantile(0.99, sum(rate(canopy_task_duration_seconds_bucket[5m])) by (le))

# Merge conflict rate
rate(canopy_merge_conflicts_total[5m])

# Resolver success rate
canopy_resolver_success_total / (canopy_resolver_success_total + canopy_resolver_failure_total)

# IPC message rate by type
rate(canopy_ipc_messages_total[5m])
```

### Prometheus Configuration

Add Canopy as a scrape target in your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'canopy'
    static_configs:
      - targets: ['localhost:8080']
    scrape_interval: 15s
```

### Grafana Integration

A pre-configured Grafana dashboard is available at `docs/grafana/canopy-dashboard.json`.

#### Importing the Dashboard

1. Open Grafana → Dashboards → Import
2. Upload `docs/grafana/canopy-dashboard.json` or paste its contents
3. Select your Prometheus data source
4. Click Import

#### Dashboard Panels

The included dashboard provides:

- **Running Agents**: Current count of active worker agents
- **Merge Queue Depth**: Items waiting to be merged
- **Active Overlay Mounts**: Current filesystem isolation count
- **Total Merge Conflicts**: Cumulative conflict counter
- **Agents by Status**: Time series of agent states
- **IPC Messages Rate**: Message throughput by type
- **Task Duration Percentiles**: p50/p95/p99 latency
- **Resource Usage**: Queue depth and overlay mounts over time
- **Hourly Task Activity**: Success/failure/conflict trends

### Alerting Examples

Example Prometheus alerting rules:

```yaml
groups:
  - name: canopy
    rules:
      - alert: CanopyHighMergeQueueDepth
        expr: canopy_merge_queue_depth > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Merge queue backing up"
          description: "{{ $value }} items in merge queue"

      - alert: CanopyHighConflictRate
        expr: rate(canopy_merge_conflicts_total[15m]) > 0.1
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "High merge conflict rate"
          description: "{{ $value | humanize }} conflicts/sec"

      - alert: CanopyResolverFailures
        expr: increase(canopy_resolver_failure_total[1h]) > 3
        labels:
          severity: critical
        annotations:
          summary: "Multiple resolver failures"
          description: "{{ $value }} resolver failures in the last hour"

      - alert: CanopySlowTasks
        expr: histogram_quantile(0.95, rate(canopy_task_duration_seconds_bucket[15m])) > 1800
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "Tasks taking longer than expected"
          description: "p95 task duration is {{ $value | humanizeDuration }}"
```

## See Also

- [Architecture](ARCHITECTURE.md) - System design
- [Configuration](CONFIGURATION.md) - Config file reference
- [Validation Guide](VALIDATION.md) - Post-merge validation and repair agents
- [Getting Started](GETTING-STARTED.md) - Tutorial
