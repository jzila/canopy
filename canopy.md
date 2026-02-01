# Canopy: Everything Everywhere All at Once

## Intro

**"Who here uses Beads?"** _(hands)_

Beads is great. Steve Yegge wrote it. Simple idea: git-backed issue tracking. Tasks live in your repo. Dependencies between tasks. Agents can read and update them. Clean primitive.

Then Steve wrote a paper called "Welcome to Gas Town." Let me explain it.

You start with a **rig**. Your rig has **polecats** — those are your agents. The polecats do work, but they need a **deacon** to coordinate them. Multiple deacons report to a **mayor**. The mayor manages your **gas** — that's your context window. When you run low on gas, you need to **compress** into a **bead**, which is like a checkpoint. Beads can have **gates** — human gates, timer gates, bead gates. Gates can trigger **formulas**. Formulas spawn more polecats.

Clear? Great.

_(pause)_

Steve calls Gastown "very opinionated" — and it is, by design. I wanted something more modular. Something I could understand in an afternoon.

Canopy uses Beads as a minimal primitive. Tasks with dependencies. That's it.

---

Here's the thing about coding agents: they're incredible. Speed-of-thought execution. You describe what you want, it happens.

But you're constantly tantalized by how much more they could be.

You start chatting with an LLM. Then you want it to see your files. Then you want it to run code. Then modify your codebase. Then track its own work. Then... why is it only doing one thing at a time?

The Gastown paper captures this progression:

| Stage | Description |
|-------|-------------|
| 1 | Chat with an LLM |
| 2 | Chat with context (files, docs) |
| 3 | Agent can execute code |
| 4 | Agent can modify your codebase |
| 5 | Agent tracks work in issues |
| 6 | Multiple agents in parallel |
| 7 | Agents spawn sub-agents |
| 8 | **Build your own orchestrator** |

I kept wanting the next stage. Eventually I got to 8. Here's my orchestrator.

---

## Architecture

### The Vision

Engineering should be about **specifying**, **defining acceptance criteria**, and **reviewing** — not waiting for code one task at a time.

AI coding tools gave us speed-of-thought execution. But single-threaded. Canopy completes the transition.

### Core Pattern: Reactive Loops

The same pattern shows up everywhere in Canopy: declarative state on top of a consistent API, with changes flowing through a single path.

**UI Loop** — Dashboard is declarative on streamed state:
```
EventBus → WebSocket → Zustand store → React render
```

**Config Loop** — Config is loaded from disk, cached in memory, reloaded on change:
```
.canopy/config.toml → in-memory config → daemon behavior
```

**Merge Loop** — Agents commit in their sandbox, then hand off to the orchestrator:
```
Agent completes → commit in overlay → send to merge queue (locked)
  → 3-way merge → success? apply to HEAD
                 → conflict? spawn resolver agent
  → next agents launch from new HEAD
```

Each loop has one source of truth, one mutation path, and consumers that react to changes.

---

### Building Blocks

**Beads-Powered Tasks**
- Minimal protocol: `Ready()`, `Start()`, `Done()`, `Fail()`
- Core schema only: ID, title, status, priority, blockers
- Any implementation of [beads-protocol](https://github.com/jzila/beads-protocol) works

**Configurable Rules** (`.canopy/config.toml`)
```toml
[rules]
priority_max = 2          # Only P0-P2
types = ["bug", "task"]   # Skip features/chores
exclude_labels = ["wip"]  # Skip work-in-progress
```
Explicit filters only — no fuzzy/NLP matching.

**Configurable Concurrency**
```bash
canopy run              # Default: 4 agents
canopy run -c 8         # 8 parallel agents
canopy run -c 1         # Serial (debugging)
```

**Sandboxing**
- OverlayFS (Linux) / APFS clones (macOS) — copy-on-write isolation
- `.claude/` hidden from agents via whiteout
- Optional `--sandbox` for full bwrap namespace isolation

**Hierarchy**
```
Daemon (1 per machine)
└── OrchestratorManager
    ├── Orchestrator[repo1] ──→ Scheduler ──→ Agent[1..N]
    └── Orchestrator[repo2] ──→ Scheduler ──→ Agent[1..N]
```
One orchestrator per repo. Agents run in isolated overlays.

---

## Pluggable Agent Types

| Type | Trigger | Purpose |
|------|---------|---------|
| **Implementor** | Task ready | Execute task in sandbox |
| **Resolver** | Merge conflict | Fix failed patches |
| **Repair** | Validation failure | Fix broken build/tests |

All three use the same `agent.Executor` and emit events via callbacks — decoupled from IPC/daemon.

---

## Web UI

Embedded in the Go binary via `//go:embed`:

```go
//go:embed all:dashboard/dist
var distFS embed.FS
```

- React 18 + TypeScript + Zustand + Tailwind
- Single binary deployment — no Node runtime needed
- Real-time updates via WebSocket

---

## TUI

Built on Charmbracelet (Bubble Tea + Lipgloss). Two modes:

```bash
canopy daemon --tui     # In-process
canopy tui :8080        # Remote (WebSocket)
```

Still early — web dashboard is primary interface for now.

---

## Demo

_(Live demo: define tasks with dependencies, launch parallel agents, watch dashboard)_

---

## Links

- **Repo:** https://github.com/jzila/canopy
- **Beads Protocol:** https://github.com/jzila/beads-protocol
- **Gastown Paper:** https://steve-yegge.medium.com/welcome-to-gas-town-4f25ee16dd04
