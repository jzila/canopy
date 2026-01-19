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

When the daemon is running, these endpoints are available:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/state` | GET | Full state snapshot |
| `/api/agents` | GET | List all agents |
| `/api/agents/:id` | GET | Get specific agent |
| `/api/runs` | GET | List runs |
| `/api/runs/:id` | GET | Get specific run |
| `/ws` | GET | WebSocket for real-time updates |

## See Also

- [Architecture](ARCHITECTURE.md) - System design
- [Configuration](CONFIGURATION.md) - Config file reference
- [Validation Guide](VALIDATION.md) - Post-merge validation and repair agents
- [Getting Started](GETTING-STARTED.md) - Tutorial
