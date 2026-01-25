# Dashboard User Guide

The Canopy dashboard provides real-time monitoring of parallel agent execution. It shows agent status, merge queue progress, validation results, and live output streaming via WebSocket.

## Quick Start

```bash
# Start daemon with web dashboard
canopy daemon

# Open in browser
open http://localhost:8080
```

The web dashboard is embedded in the daemon and served automatically at port 8080.

## Accessing the Dashboard

### Web Dashboard (Browser)

Start the daemon and open your browser:

```bash
# Start daemon (foreground)
canopy daemon

# Or start in background
canopy daemon start

# Access at:
http://localhost:8080
```

### Custom Port

```bash
canopy daemon --port 9090
# Access at http://localhost:9090
```

### TUI Dashboard (Terminal)

For terminal-based monitoring:

```bash
# Start daemon with terminal UI
canopy daemon --tui

# Connect to existing daemon
canopy daemon --tui --daemon-addr localhost:8080
```

### Development Mode

For dashboard development with hot reload:

```bash
# Terminal 1: Start daemon
canopy daemon

# Terminal 2: Start Vite dev server
cd web/dashboard
npm run dev
# Access at http://localhost:5173
```

## Dashboard Layout

```
┌─────────────────────────────────────────────────────────────────────────┐
│  Canopy          [Repo ▾]  [Run ▾]  │ Stats │  [Pause] [☀/☾]  [●]      │
├──────────────┬──────────────────────────────────────────────────────────┤
│              │                                                          │
│   Beads      │               Agent Grid                                 │
│   Pane       │    ┌───────────────┐  ┌───────────────┐                 │
│              │    │  Agent Card   │  │  Agent Card   │                 │
│  [Tasks]     │    │  canopy-abc   │  │  canopy-def   │                 │
│  [Ready]     │    │  running      │  │  completed    │                 │
│  [Progress]  │    └───────────────┘  └───────────────┘                 │
│              │                                                          │
│  ┌────────┐  │    ┌───────────────┐  ┌───────────────┐                 │
│  │ Merge  │  │    │  Agent Card   │  │  Agent Card   │                 │
│  │ Queue  │  │    │  canopy-ghi   │  │  canopy-jkl   │                 │
│  └────────┘  │    │  failed       │  │  starting     │                 │
│              │    └───────────────┘  └───────────────┘                 │
├──────────────┴──────────────────────────────────────────────────────────┤
│  Terminal Panel                                                         │
│  [Live Feed] [Raw Output] [Commits] [Detail]                           │
│                                                                         │
│  12:34:05  tool_use  Read src/main.go                                  │
│  12:34:06  file      Modified src/main.go (+15, -3)                    │
│  12:34:08  tool_use  Bash go test ./...                                │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

## Header Bar

The header bar displays global controls and statistics.

### Repository Selector

Switch between registered repositories:
- Dropdown shows all known repositories
- Active repository is highlighted
- Switching loads that repository's agents and tasks

### Run Selector

Filter agents by historical run:
- **Current**: Shows agents from the active run
- **Past runs**: Filter to see agents from specific completed runs
- Shows run timestamp, task count, and completion status

### Stats Summary

Real-time statistics for the current view:
- **Tasks**: Total task count
- **Tokens**: Combined token usage (formatted: 1.2K, 3.5M)
- **Cost**: Total USD spent
- **Commits**: Git commits created
- **Files**: Files changed across all agents

### Pause/Resume Control

Control orchestrator execution:
- **Pause**: Stop scheduling new agents (running agents continue)
- **Paused**: Orchestrator is paused, click to resume
- **Agent Active...**: Cannot pause while agent completing
- **Paused (agent active)**: Paused but waiting for agent to finish

### Theme Toggle

Switch between light and dark mode (persisted in localStorage).

### Connection Status

Green indicator when WebSocket is connected. Shows disconnected state with automatic reconnection.

## Beads Pane (Left Sidebar)

The collapsible left pane shows tasks from the beads tracking system.

### Task List

Each task shows:
- **Title**: Task name (clickable to filter agents)
- **Status badge**: Ready/In Progress/Running/Failed/Done
- **Priority badge**: P0-P4 with color coding
- **Dependencies**: Parent tasks shown in hierarchy view

### View Modes

Toggle between:
- **Hierarchy**: Tasks grouped by dependencies
- **Flat**: All tasks in a single list

### Sort Options

- **Priority**: P0 first, then P1, etc.
- **Updated**: Most recently updated first

### Show Completed

Toggle to include finished tasks in the list. When enabled, automatically switches status filter to "all".

### Merge Queue Status

Inline visualization of the merge queue:

```
○ ○ ● ◐ ○ ○
    ↑  ↑
    │  └── Resolving (amber pulse)
    └───── Active (blue pulse)

● = Completed (green=success, red=failed)
○ = Queued (hollow)
```

Hover over dots for tooltips showing task ID and status.

## Agent Grid

The main content area displays agent cards in a responsive grid (1-4 columns based on viewport width).

### Status Filter Bar

Filter agents by execution status:

| Filter | Description | Color |
|--------|-------------|-------|
| All | Show all agents | Gray |
| Running | Currently executing | Blue |
| Completed | Finished successfully | Green |
| Failed | Encountered error | Red |
| Archived | Show/hide archived agents | Purple |

Each filter shows a count badge.

### Bead Filter Indicator

When a task is selected in the Beads pane, a filter indicator appears showing which task's agents are displayed. Click to clear the filter.

## Agent Cards

Each card represents one agent execution.

### Card Header

```
┌─────────────────────────────────────┐
│ [Archived]  canopy-abc    [running] │
└─────────────────────────────────────┘
```

- **Archived badge**: Shown if agent is archived
- **Task ID**: Clickable to highlight in Beads pane
- **Status badge**: starting/running/completed/failed

### Metrics Grid

```
┌──────────┬──────────┬──────────┬──────────┐
│  2m 34s  │  12.5K   │  $0.42   │  3 ⊛     │
│  elapsed │  tokens  │  cost    │  commits │
└──────────┴──────────┴──────────┴──────────┘
```

- **Elapsed**: Updates every second for running agents
- **Tokens**: Formatted with K/M suffixes
- **Cost**: USD with 2 decimal places
- **Commits**: Shows commit count (hidden if 0)

### Merge & Validation Status

```
[Merged (3)]  [Validation: passed ✓]
```

**Merge status values:**
- `Merged (N)`: Successfully merged N commits
- `No Changes`: Agent made no file changes
- `Merge Failed`: Merge encountered errors
- `Conflict`: Merge conflict detected
- `Resolving`: Resolver agent spawned

**Validation status values:**
- `passed` (green ✓): All validation steps succeeded
- `failed` (red ✗): Validation step failed
- `repairing` (orange 🔧): Repair agent active
- `pending` (gray ⏱): Not yet started
- `skipped`: Validation disabled

Repair attempt count shown when relevant: `(attempt 2/3)`

### Agent Chain Timeline

Expandable section showing the full execution flow:

```
▼ Agent Chain
  ├─ Implementor ──── completed (2m 15s)
  ├─ Merge ────────── merged (0.3s)
  │   └─ Resolver ─── spawned (conflict in main.go)
  ├─ Validation ───── passed (15s)
  │   ├─ build ────── ✓ (8s)
  │   └─ test ─────── ✓ (7s)
  └─ Repair ───────── not needed
```

Click to expand/collapse. Shows:
- Step name and status icon
- Duration for completed steps
- Error details for failed steps
- Output excerpts where relevant

### Error Display

Failed agents show error message:

```
┌─────────────────────────────────────┐
│ Error: Command failed: go build     │
│ exit status 1                       │
└─────────────────────────────────────┘
```

### Action Buttons

- **Archive** (finished agents): Hide from default view
- **Kill** (running agents): Terminate agent immediately

## Terminal Panel

Click an agent card to open the terminal panel at the bottom.

### Tab: Live Feed

Structured timeline of agent activity:

```
12:34:05  tool_use   Read src/main.go
12:34:06  file       Modified src/main.go (+15, -3)
12:34:07  text       "I'll add error handling..."
12:34:08  tool_use   Bash go test ./...
12:34:12  result     ✓ Tests passed
12:34:15  completed  Agent finished (3 files, 2 commits)
```

**Event types:**
- **tool_use**: Tool invocation (Read, Write, Bash, etc.)
- **file**: File modification with line counts
- **text**: Assistant messages
- **result**: Tool execution result (success/failure)
- **error**: Error messages (red)
- **completed**: Final status summary

Historic events (loaded from persistence) show with amber tint.

### Tab: Raw Output

Full terminal emulation via xterm.js:
- Complete stdout/stderr output
- Supports ANSI colors and formatting
- 10,000 line scrollback buffer
- Dark/light theme follows dashboard theme

### Tab: Commits

List of git commits created by the agent:

```
▶ abc1234 - Add user authentication
  Author: Claude <claude@anthropic.com>
  Date: 2024-01-15 12:34:56
  Files: 3 changed

  ▼ Changed files:
    M src/auth.go
    A src/auth_test.go
    M go.mod
```

Click commit hash to copy. Expand to see changed files.

### Tab: Detail

Comprehensive agent metadata:

```
Agent ID:     agent-abc123
Task:         canopy-xyz (Implement user auth)
Status:       completed
Duration:     2m 34s
Tokens:       12,543
Cost:         $0.42
Exit Code:    0

Merge Status: merged (3 commits)
Validation:   passed
  - build: ✓ (8s)
  - test: ✓ (7s)
```

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Esc` | Close terminal panel |
| `1-4` | Switch terminal tabs |
| `d` | Toggle dark mode |

## Real-Time Updates

The dashboard receives live updates via WebSocket:

- **Agent events**: Start, output, completion
- **Merge queue**: Position changes, conflicts
- **Validation**: Step progress, pass/fail
- **Task updates**: Status changes from beads
- **Statistics**: Token/cost totals

Connection automatically reconnects with exponential backoff (1s → 30s max).

## Configuration

### LocalStorage Persistence

The dashboard remembers:
- Theme preference (dark/light)
- Terminal panel height
- Beads pane width
- Beads pane expanded/collapsed
- Show archived agents toggle
- Show completed beads toggle

Clear browser localStorage to reset to defaults.

### Environment Variables

The daemon respects:
- `XDG_CACHE_HOME`: Override cache directory (default: `~/.cache`)

## Troubleshooting

### Dashboard won't load

1. **Check daemon is running:**
   ```bash
   canopy daemon status
   ```

2. **Check port availability:**
   ```bash
   lsof -i :8080
   ```

3. **View daemon logs:**
   ```bash
   canopy daemon logs -f
   ```

### No agents appearing

1. **Verify tasks exist:**
   ```bash
   bd ready
   ```

2. **Check run is active:**
   ```bash
   canopy ps
   ```

3. **Ensure correct repository:**
   - Check repository selector in header
   - Verify working directory matches

### WebSocket disconnected

The connection indicator shows red when disconnected. The dashboard will automatically reconnect.

If persistent disconnection:

1. **Check daemon health:**
   ```bash
   curl http://localhost:8080/api/state
   ```

2. **Restart daemon:**
   ```bash
   canopy daemon restart
   ```

### Live feed not updating

1. **Check agent selection:**
   - Click an agent card to select it
   - Terminal panel should open

2. **Verify WebSocket:**
   - Check browser DevTools → Network → WS
   - Should show active connection to `/ws`

### Stale data after restart

After daemon restart, the dashboard loads persisted state from SQLite. If data seems stale:

1. **Force refresh:**
   ```bash
   # Browser: Ctrl+Shift+R (hard refresh)
   ```

2. **Check persistence:**
   ```bash
   sqlite3 ~/.cache/canopy/runs.db "SELECT COUNT(*) FROM agents;"
   ```

### High memory usage

The Live Feed tab can accumulate many events. To reduce memory:

1. Archive completed agents (removes from active view)
2. Close terminal panel when not needed
3. Refresh browser periodically for long runs

### Theme not applying

1. Clear localStorage:
   ```javascript
   // Browser console
   localStorage.removeItem('darkMode')
   ```

2. Refresh page

## API Reference

The dashboard communicates with these endpoints:

| Endpoint | Purpose |
|----------|---------|
| `GET /api/state` | Full state snapshot |
| `GET /api/agents` | List agents |
| `PATCH /api/agents` | Update agent (archive) |
| `POST /api/agents/:id/kill` | Kill running agent |
| `GET /api/runs` | Historical runs |
| `GET /ws` | WebSocket for real-time updates |

See [Commands](COMMANDS.md#http-api-endpoints) for complete API documentation.
