# Agent Instructions

This project uses **bd** (beads) for issue tracking and **canopy** for parallel agent orchestration.

Run `bd onboard` to learn beads, and `canopy help` to learn the orchestrator.

## IMPORTANT: Filing vs Implementing

**When the user asks to "file beads", "file issues", "create tickets", or similar phrasing, STOP after filing.** You may investigate to understand the problem, but once the bead is created with `bd create`, you are DONE. Do NOT proceed to implement or fix.

- "file a bead for X" → investigate if needed, `bd create`, then **stop**
- "create tickets for these bugs" → `bd create` for each, then **stop**
- "track this as an issue" → `bd create`, then **stop**

If the user wants implementation, they will say "fix", "implement", "resolve", or explicitly ask you to work on it.

## Git Setup
Before any git operations, verify identity is configured:
```bash
git config user.name && git config user.email  # Must both exist
```
If missing, check `~/.gitconfig` or set locally for this repo.

Use plain `git` commands—the working directory is already set, so `-C` is unnecessary.

## Commit Standards
- Format: `type: concise description` (feat, fix, refactor, test, docs, chore)
- Reference bead ID when relevant: `fix(canopy-abc): description`
- Run `go test ./...` before committing code changes
- Run `go build ./...` to verify compilation (enforced by pre-commit hook)

## Beads API Invariant

Canopy treats beads as a **minimal issue tracker with dependencies**. It MUST NOT depend on:
- Gates (human, timer, gh:*, bead)
- Formulas or workflow automation
- Watchers or notifications
- GitHub integration
- Cross-rig (gastown) references
- Any field not in the core schema

### Core Schema (canopy may use)

```go
type Task struct {
    ID          string   `json:"id"`
    Title       string   `json:"title"`
    Description string   `json:"description,omitempty"`
    Type        string   `json:"type,omitempty"`        // bug, feature, task, chore
    Priority    int      `json:"priority,omitempty"`    // 0-4
    Status      string   `json:"status,omitempty"`      // open, in_progress, closed, deferred
    Labels      []string `json:"labels,omitempty"`
    Assignee    string   `json:"assignee,omitempty"`
    Blockers    []string `json:"blockers,omitempty"`    // Dependency IDs
    UpdatedAt   string   `json:"updated_at,omitempty"`
}
```

### Task Selection (Strict)

Canopy filters tasks using ONLY these fields:
- `priority` - numeric range (0-4)
- `type` - exact match (bug, feature, task, chore)
- `labels` - set membership
- `assignee` - exact match or empty

**No fuzzy/natural language filtering.** The `--prompt` flag is deprecated.

### Commands (canopy may use)

```bash
bd ready                    # Actionable work (open, unblocked)
bd ready --priority 2       # Filter by max priority
bd ready --type bug         # Filter by type
bd list --status=open       # Query by status
bd show <id>                # Task details
bd update <id> --status=X   # Change status
bd close <id>               # Complete
bd close <id> --reason="X"  # Complete with reason
bd dep add <a> <b>          # a depends on b
bd sync                     # Git sync
```

Reference: https://github.com/jzila/beads-protocol

## Git Hooks

The pre-commit hook (`scripts/hooks/pre-commit`) enforces:
1. **Go build check**: Runs `go build ./...` when `.go` files are staged
2. **Beads sync**: Flushes pending beads changes to JSONL

Install hooks after cloning:
```bash
cp scripts/hooks/pre-commit .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit
```

Or use devenv which automatically installs hooks on shell entry.

## Error Handling Guidelines

See `pkg/errors/errors.go` for defined error types and full guidelines.

### Core Rules

1. **Always wrap errors with context** using `fmt.Errorf("context: %w", err)`:
   ```go
   // Good
   if err := doSomething(); err != nil {
       return fmt.Errorf("failed to do something: %w", err)
   }

   // Bad - loses context
   if err := doSomething(); err != nil {
       return err
   }
   ```

2. **Never silently swallow errors**. If you can't return an error, log it:
   ```go
   // Good - explicit about ignoring
   _ = optionalCleanup() // Non-fatal: cleanup is best-effort

   // Good - log non-fatal errors
   if err := cleanup(); err != nil && verbose {
       fmt.Fprintf(os.Stderr, "warning: cleanup failed: %v\n", err)
   }

   // Bad - silent failure
   cleanup()
   ```

3. **Use sentinel errors** for programmatic handling:
   ```go
   import "github.com/jzila/canopy/pkg/errors"

   if errors.Is(err, errors.ErrMergeConflict) {
       // Handle conflict specifically
   }
   ```

4. **Use typed errors** for detailed information:
   ```go
   var mergeErr *errors.MergeError
   if errors.As(err, &mergeErr) {
       fmt.Printf("Merge failed for task %s\n", mergeErr.TaskID)
   }
   ```

### When to Return vs Log

- **Return errors** when the caller can handle them or needs to know
- **Log errors** only for truly non-fatal side effects (cleanup, telemetry, etc.)
- Use `warning:` prefix for non-fatal errors in verbose output

## Concurrency Guidelines

Use consistent concurrency patterns based on the access pattern of your data:

### When to Use `sync.Map`

Use `sync.Map` for maps that are:
- **Append-only or mostly-append**: Keys are written once and read many times
- **High read contention**: Many goroutines read concurrently
- **Disjoint key access**: Different goroutines access different keys

```go
// Good: Results cache (write once per task, read many times)
results sync.Map // map[taskID]*Result

// Good: Short-lived cancel functions (store, then delete)
agentContexts sync.Map // map[taskID]context.CancelFunc
```

**Avoid** `sync.Map` for maps that require:
- Iteration during updates
- Complex multi-field updates
- Snapshot/copy operations
- Coordinated updates across multiple keys

### When to Use `sync.RWMutex`

Use `sync.RWMutex` for structured data with:
- **Complex update patterns**: Multiple fields updated together
- **Iteration requirements**: Need to range over all entries
- **Snapshot operations**: Need consistent point-in-time copies
- **Coordinated updates**: Changes span multiple entries

```go
// Good: Runtime state with snapshots and stats aggregation
type RuntimeState struct {
    Agents map[string]*AgentState
    Tasks  map[string]*TaskState
    Stats  Stats
    mu     sync.RWMutex
}

func (r *RuntimeState) GetSnapshot() RuntimeState {
    r.mu.RLock()
    defer r.mu.RUnlock()
    // Return consistent copy of all fields
}
```

### When to Use `atomic` Types

Use `atomic.Bool`, `atomic.Int64`, etc. for:
- **Simple flags**: Boolean state (paused, closed, active)
- **Counters**: Incrementing/decrementing integers
- **Single-value state**: No coordination with other fields needed

```go
// Good: Independent boolean flags
paused atomic.Bool
closed atomic.Bool

// Good: Simple counter
activeCount atomic.Int64
```

**Avoid** atomics when the flag must be coordinated with other state changes—use a mutex instead.

### Pattern Summary

| Pattern | Use Case | Example |
|---------|----------|---------|
| `sync.Map` | Append-only cache, disjoint keys | `results`, `agentContexts` |
| `sync.RWMutex` | Structured data, snapshots, iteration | `RuntimeState` |
| `atomic` | Simple flags, counters | `paused`, `closed` |

## State Machine Patterns

| Complexity | Pattern | Example |
|------------|---------|---------|
| ≤5 events | Explicit methods (`UserPause()`, `AgentResume()`) | `pkg/mergequeue/pause_state.go` |
| >5 events | Event-driven (`Transition(ctx, event)`) | `pkg/lifecycle/state.go` |

**Both patterns require:** mutex-protected state, query methods (`State()`, `IsTerminal()`), `String()` for logging.

## API Conventions (Go ↔ TypeScript)

**JSON field names use snake_case** throughout the codebase.

When defining API types that cross the Go/TypeScript boundary:

1. **Go struct tags**: Use snake_case in `json:"..."` tags
   ```go
   type AgentState struct {
       TaskID    string `json:"task_id"`    // ✓ snake_case
       StartTime string `json:"start_time"` // ✓ snake_case
   }
   ```

2. **TypeScript interfaces**: Use snake_case property names to match Go
   ```ts
   interface AgentState {
       task_id: string;    // ✓ matches Go JSON tag
       start_time: string; // ✓ matches Go JSON tag
   }
   ```

3. **When adding new API types**: Define Go struct first with snake_case JSON tags, then mirror exactly in TypeScript. Both sides must match character-for-character.

4. **Rebuild dashboard after API changes**: Run `just run` to catch TypeScript errors early.

## Data Flow Invariants

### Event Pipeline

ALL state changes MUST flow through this pipeline:

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│ IPC Client  │ -> │ IPC Server  │ -> │  EventBus   │ -> │ Subscribers │
│(canopy run) │    │  (daemon)   │    │             │    │             │
└─────────────┘    └─────────────┘    └─────────────┘    └─────────────┘
                                              │
                   ┌──────────────────────────┼──────────────────────────┐
                   │                          │                          │
                   ▼                          ▼                          ▼
            ┌─────────────┐           ┌─────────────┐           ┌─────────────┐
            │RuntimeState │           │Persistence  │           │ WebSocket   │
            │ (in-memory) │           │  Handler    │           │    Hub      │
            └─────────────┘           └─────────────┘           └─────────────┘
                                              │                          │
                                              ▼                          ▼
                                       ┌─────────────┐           ┌─────────────┐
                                       │   SQLite    │           │  Dashboard  │
                                       │  (runs.db)  │           │   (React)   │
                                       └─────────────┘           └─────────────┘
```

**Critical invariant**: Fields MUST be present at every layer or data will be lost.

### Validation and Repair Flow

After each successful merge, validation runs if configured:

```
┌─────────────┐
│   Merge     │
│  Complete   │
└──────┬──────┘
       │
       ▼
┌─────────────┐     ┌─────────────┐
│  Validation │ No  │    Done     │
│  Enabled?   │────>│  (success)  │
└──────┬──────┘     └─────────────┘
       │ Yes
       ▼
┌─────────────┐     ┌─────────────┐
│    Run      │ All │    Done     │
│ Validation  │────>│  (success)  │
│   Steps     │Pass └─────────────┘
└──────┬──────┘
       │ Fail
       ▼
┌─────────────┐     ┌─────────────┐
│   Spawn     │     │  Re-run     │
│   Repair    │────>│ Validation  │
│   Agent     │     │             │
└──────┬──────┘     └──────┬──────┘
       │                   │
       │◄──────────────────┘
       │ Pass: Done (repaired)
       │ Fail: Loop until max_repair_attempts
       ▼
┌─────────────┐     ┌─────────────┐
│  Max        │ Yes │ File Bead:  │
│ Attempts?   │────>│  "Repair    │
└─────────────┘     │  Exhausted" │
                    └─────────────┘
```

**Repair Agent Behavior:**
- Runs directly on the working directory (not in overlay)
- Sees failed step output, exit code, and merged diff
- Knows what previous attempts tried (to avoid repetition)
- Commits fixes directly to the repository
- Tracked as child of the original implementor agent

**Validation Status Values:**
- `pending` - Not yet started
- `running` - Steps executing
- `passed` - All steps succeeded
- `failed` - Required step failed
- `repairing` - Repair agent active
- `skipped` - Validation disabled

**Merge Status Values:**
- `MergeStatusMerged` - Success (with or without repair)
- `MergeStatusMergedNeedsRepair` - Merge kept but validation failed after all repairs

### Field Naming Checklist

When adding new fields to any struct that crosses boundaries:

- [ ] Use snake_case in all Go JSON tags (e.g., `json:"merge_status"`)
- [ ] Use snake_case in TypeScript interfaces (e.g., `merge_status: string`)
- [ ] Use snake_case in database columns (e.g., `merge_status TEXT`)
- [ ] Verify field appears in IPC protocol message type
- [ ] Verify field appears in EventBus event payload
- [ ] Verify field appears in WebSocket event handler
- [ ] Verify field appears in dashboard TypeScript interface

**Never use camelCase in JSON tags** - it breaks the Go/TypeScript/persistence boundary.

### State Restoration Invariant

On daemon restart, ALL persisted state MUST be restored to RuntimeState:

1. `RestoreState()` loads from SQLite (runs, agents, tasks)
2. `ApplyRestoredState()` maps persistence types to runtime types
3. `loadTasksFromBeads()` loads current beads (requires beads client factory)

If a field exists in persistence.Agent, it MUST be mapped in `ConvertPersistenceAgentToState()`.

### WebSocket Event Parity

Every field sent by the IPC server MUST be:
1. Defined in the TypeScript event interface
2. Extracted in the WebSocket event handler
3. Applied to the corresponding state store field

Audit when adding new metrics: `pkg/ipc/server.go` → `web/dashboard/src/hooks/useWebSocket.ts`

## Dashboard (web/dashboard) TypeScript

**Never run `tsc` directly** in `web/dashboard`. Vite handles all transpilation.

- **Type checking**: `npm run type-check` (runs `tsc --noEmit`)
- **Building**: `just run` (rebuilds dashboard + Go binary, restarts daemon)
- **Development**: `npm run dev` (Vite dev server)

Running raw `tsc` without `--noEmit` generates `.js`, `.d.ts`, and `.map` files in `src/` which pollute the working directory. These are gitignored but cause issues with overlay change detection.

## Dashboard Typography Conventions

The dashboard uses **IBM Plex** fonts with strict weight rules:

| Element | Font | Weight | Class |
|---------|------|--------|-------|
| Main title ("Canopy") | Mono | Light (300) | `font-mono font-light` |
| All other UI text | Mono | Normal (400) | `font-mono font-normal` |
| Body text | Sans | Normal (400) | (default) |

**Invariants:**

1. **`font-light` is ONLY for the main title** - Never use on section headers, labels, or tabs
2. **Navigation/chrome uses mono font** - Tabs, filter buttons, section headers all use `font-mono`
3. **Consistent sizing within context** - Labels and counts in the same component use the same `text-*` size
4. **Filter chips are compact** - Use `px-3 py-2` not `px-5 py-3`; avoid fixed heights like `h-12`
5. **Counts use `font-normal`** - Never `font-semibold` or `font-medium` for numeric badges

**Files:**
- Font config: `web/dashboard/tailwind.config.js` (weights, tracking, sizes)
- Global styles: `web/dashboard/src/index.css` (base typography rules)
- Font import: `web/dashboard/index.html` (Google Fonts link)

## Persistence Invariant

**ALL canopy persistence MUST live at `$XDG_CACHE_HOME/canopy/` or `~/.cache/canopy/`.**

This includes:
- `runs.db` - SQLite database for run/agent history
- `repositories.json` - Registry of known repositories
- Any other persistent state

**Do NOT store canopy state in:**
- The repository itself (`.canopy/` is for config only, not state)
- Other XDG directories (`~/.local/share/`, etc.)
- User home directory directly

Repository identity is stored in `$XDG_CACHE_HOME/canopy/repositories.json` keyed by absolute path, not in the repo itself. This keeps repos clean and portable.

## Repo Configuration Invariant

**ALL repo-specific configuration MUST live in `.canopy/config.toml`** - one file, not multiple.

```
.canopy/
└── config.toml    # ALL orchestrator settings go here
```

**Do NOT create separate config files like:**
- `orchestrator.toml`
- `rules.toml`
- `validation.toml`
- `sandbox.toml`
- Any other `.toml` files

**Why one file?**
- Simple mental model: one repo = one config file
- Easy to review, copy, version control
- No confusion about which file controls what
- Avoids config fragmentation and precedence issues

**Example `.canopy/config.toml`:**
```toml
[orchestrator]
concurrency = 4

[validation]
enabled = true
mode = "strict"
steps = ["build", "test"]

[rules]
max_priority = 2
types = ["bug", "task"]
```

All orchestrator settings (concurrency, rules, validation, sandbox paths, etc.) belong in sections within this single file.

## Epics

Epics define acceptance criteria; tasks implement them. Epic depends on tasks, not vice versa.

```bash
bd create --title="Feature X" --type=epic --priority=2
bd create --title="Implement core" --type=task --priority=2
bd create --title="Add tests" --type=task --priority=2
bd dep add <epic-id> <core-id>    # epic depends on core
bd dep add <epic-id> <tests-id>   # epic depends on tests
bd dep add <tests-id> <core-id>   # tests depend on core (ordering)
```

This way tasks are ready to work, and the epic is blocked until all tasks complete. Close the epic last to verify acceptance criteria are met.

## Quick Reference

```bash
# Beads (task tracking)
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --status in_progress  # Claim work
bd close <id>         # Complete work
bd sync               # Sync with git

# Canopy (parallel orchestration)
canopy help           # Show all commands
canopy run --help     # Show run options
canopy run            # Execute ready tasks in parallel (4 agents)
canopy run -c 8       # Run with 8 concurrent agents
canopy run --dry-run  # Preview what would execute
canopy help --agent   # Detailed workflow explanation for AI agents
```

## Agentic Initialization

When setting up a new repository for Canopy, use the agentic init flow to configure sandbox and validation settings.

### Overview

The agentic init flow is a three-step JSON-based interface:

1. **`canopy init --agent`** - Get detection results + questionnaire
2. **Present questions** - Use `AskUserQuestion` to collect user preferences
3. **`canopy init --apply`** - Apply answers to create config files

### Step 1: Get Questionnaire

```bash
canopy init --agent
```

Returns JSON with project detection and questions:
```json
{
  "detection": {
    "project": {"type": "Go", "root": "/path/to/project"},
    "sandbox": {"tools": ["~/.goenv"], "configs": ["~/.gitconfig"]},
    "validation": {
      "suggested": [
        {"name": "build", "command": "go build ./...", "confidence": "high"},
        {"name": "test", "command": "go test ./...", "confidence": "high"}
      ]
    }
  },
  "questions": [
    {
      "id": "confirm_validation",
      "question": "Enable post-merge validation?",
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
      "depends_on": {"question_id": "confirm_validation", "value": "yes"}
    },
    {
      "id": "validation_steps",
      "question": "Which validation steps should run?",
      "type": "multi_select",
      "options": [
        {"value": "build", "label": "build (go build ./...)"},
        {"value": "test", "label": "test (go test ./...)"}
      ],
      "depends_on": {"question_id": "confirm_validation", "value": "yes"}
    },
    {
      "id": "extra_commands",
      "question": "Additional validation commands? (comma-separated)",
      "type": "freeform",
      "depends_on": {"question_id": "confirm_validation", "value": "yes"}
    }
  ]
}
```

### Step 2: Present Questions

Use `AskUserQuestion` to collect answers. Handle `depends_on` to skip questions:
- `depends_on: null` - always show
- `depends_on: {"question_id": "x", "value": "y"}` - show only if question `x` answered `y`

Map question types to `AskUserQuestion`:
- `single_select` → `multiSelect: false`
- `multi_select` → `multiSelect: true`
- `freeform` → Allow "Other" option for free text

### Step 3: Apply Answers

```bash
canopy init --apply '{"confirm_validation":"yes","validation_mode":"strict","validation_steps":["build","test"]}'
```

Returns:
```json
{
  "success": true,
  "files_created": [".canopy/sandbox.toml", ".canopy/validation.toml"]
}
```

### Complete Example

```bash
# 1. Get questionnaire
questionnaire=$(canopy init --agent)

# 2. Agent uses AskUserQuestion to present questions, collects:
answers='{"confirm_validation":"yes","validation_mode":"strict","validation_steps":["build","test"]}'

# 3. Apply answers
canopy init --apply "$answers"
# Output: {"success":true,"files_created":[".canopy/sandbox.toml",".canopy/validation.toml"]}
```

### Detection-Only Mode

For pre-flight checks without the questionnaire:
```bash
canopy init --detect
```

Returns just the detection results (project type, tools, validation commands) without questions.

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

Use `bd` for task tracking and `canopy` for parallel execution.
