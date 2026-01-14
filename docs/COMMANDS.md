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
| `--no-daemon` | | `false` | Disable daemon connection |
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

# Standalone mode (no daemon)
canopy run --no-daemon -v
```

#### Behavior

1. Connects to daemon (unless `--no-daemon`)
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

Initialize sandbox configuration interactively.

```bash
canopy init [flags]
```

#### Options

| Option | Default | Description |
|--------|---------|-------------|
| `--reconfigure` | `false` | Re-run wizard even if config exists |

#### Behavior

1. Scans project for type (Node, Rust, Go, Python, etc.)
2. Discovers installed tools and versions
3. Detects config files and caches
4. Generates `.canopy/sandbox.toml`
5. Shows recommended configuration
6. Prompts for confirmation

#### Example

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
- [Getting Started](GETTING-STARTED.md) - Tutorial
