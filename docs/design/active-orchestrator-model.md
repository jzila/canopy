# Design: Active Orchestrator Model

**Status**: Draft
**Epic**: canopy-2360
**Author**: Design Agent
**Date**: 2026-01-23

## Summary

This design proposes shifting the mental model from discrete "runs" to an "active orchestrator per repo" pattern, reducing friction in the dashboard and CLI while maintaining full backward compatibility.

## Background

### Current Model

The current architecture treats orchestration as **discrete runs with IDs**:

```
Repository
  └─ Run (UUID) - created each time you start
       ├─ Agent 1
       ├─ Agent 2
       └─ ...
```

Key characteristics:
- A new run is created each time `canopy run` is executed
- Run IDs are required for many API operations
- The rules engine is instantiated per-run
- Dashboard state is primarily indexed by run ID
- "Starting a run" creates new objects and resources

### Pain Points

1. **Rules panel friction**: Required an active run to display/edit rules (canopy-n2us - fixed via standalone engine fallback)

2. **Heavyweight "start run" action**: Users think "work on this repo" but must conceptualize "start a new run"

3. **Run ID proliferation**: Concurrency changes, config adjustments, and API calls often need run IDs

4. **Dashboard confusion**: Switching between run-centric (filtering by run) and repo-centric (the repo I'm working on) mental models

5. **Run continuity**: No conceptual continuity between runs on the same repo

## Proposed Design

### Core Concept: Always-Watching Orchestrator

Shift from "discrete runs" to "repo has an orchestrator that can be activated/deactivated":

```
Repository
  └─ OrchestratorLifecycle (created on activation)
       ├─ State: off | idle | active | paused
       ├─ RulesEngine (persistent while activated)
       ├─ Config (persistent in .canopy/config.toml)
       └─ Runs (historical tracking)
            ├─ Run 1
            ├─ Run 2
            └─ ...
```

### State Model

```go
type OrchestratorState string

const (
    OrchestratorOff    OrchestratorState = "off"     // Not activated, no instance exists
    OrchestratorIdle   OrchestratorState = "idle"    // Activated, watching for work, none available
    OrchestratorActive OrchestratorState = "active"  // Processing tasks
    OrchestratorPaused OrchestratorState = "paused"  // Temporarily stopped (resumes on unpause)
)
```

**State transitions:**

```
                    ┌──────────────────────────────────┐
                    │                                  │
                    ▼                                  │
┌─────┐  activate  ┌──────┐  work available  ┌────────┴─┐
│ Off │ ─────────► │ Idle │ ◄──────────────► │  Active  │
└─────┘            └──────┘  work exhausted  └────────┬─┘
   ▲                  │                          │    │
   │                  │ deactivate               │    │
   │                  ▼                          │    │
   │              (destroyed)◄───────────────────┘    │
   │                                                  │
   │                                                  │ pause
   │                                                  ▼
   │                                            ┌──────────┐
   │                                            │  Paused  │
   │                                            └────┬─────┘
   │                                                 │
   │◄────────────────────────────────────────────────┘
                      deactivate
```

**Key behaviors:**
- `off → idle`: Activation creates OrchestratorLifecycle, starts watching for work
- `idle ↔ active`: Automatic transitions as work becomes available/exhausted
- `active → paused`: User pauses; orchestrator stops dispatching but keeps watching
- `paused → active`: User resumes; orchestrator continues processing
- `* → off`: Deactivation (context cancellation) destroys the lifecycle

**Critical invariant:** An activated orchestrator **never exits on its own**. It continuously polls for available work and transitions between idle and active states. Only explicit deactivation (user action or context cancellation) terminates it.

### What's Already Implemented

Looking at `pkg/daemon/orchestrator_manager.go`, the structural foundation exists:

```go
type OrchestratorLifecycle struct {
    RepoPath    string
    RepoID      string
    State       OrchestratorState  // off | idle | active | paused
    RunID       string             // Current run ID when active
    rulesEngine *rules.Engine      // Persistent rules engine
    // ...
}
```

**canopy-o7ww added:**
- The `OrchestratorLifecycle` wrapper struct
- State enum with `off` state
- Basic state management infrastructure

**What canopy-o7ww did NOT add:**
- Always-watching behavior (orchestrator still exits when work is exhausted)
- Automatic idle ↔ active transitions based on work availability
- Polling loop for continuous work detection

The current implementation still treats "idle" as essentially "off" — there is no persistent watching behavior. The orchestrator terminates when its current batch of work completes rather than polling for new work.

### What Needs to Change

#### 1. Core Orchestration Loop

The orchestrator needs a persistent watch loop:

```go
func (o *OrchestratorLifecycle) Run(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()  // Only way to exit
        default:
            tasks := o.pollForWork()
            if len(tasks) == 0 {
                o.setState(OrchestratorIdle)
                time.Sleep(o.pollInterval)
                continue
            }
            o.setState(OrchestratorActive)
            o.dispatchTasks(ctx, tasks)
        }
    }
}
```

#### 2. Mental Model in UI

**Current**: "Start Run" / "Stop Run" with run selector
**Proposed**: "Activate" / "Deactivate" with run history

The UI should present:
- **Primary state**: Is the orchestrator active/idle/off?
- **Secondary**: Which run are you viewing? (for history)

```
┌─────────────────────────────────────────────────────────────────┐
│ [Repository Selector]  │ Orchestrator: Active ● │ [Pause] [Stop]│
│                        │ Run: abc123 (2h ago)   │               │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   [Agent Cards - filtered by selected run]                      │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### 3. Terminology

| Term | Meaning |
|------|---------|
| Run | A period of work within an activated orchestrator |
| OrchestratorLifecycle | Wrapper managing orchestrator state and lifecycle |
| Orchestrator | The execution engine that dispatches agents |
| OrchestratorManager | Daemon component that manages lifecycles across repos |
| Activate | Create lifecycle and start watching |
| Deactivate | Cancel context, destroy lifecycle |

#### 4. API Endpoints

**Keep (rename optional)**:
- `POST /api/orchestrator/run` → `POST /api/orchestrator/activate`
- `POST /api/orchestrator/run/stop` → `POST /api/orchestrator/deactivate`

**New**:
- `GET /api/repos/:repo_id/orchestrator` - Get orchestrator state
- `PATCH /api/repos/:repo_id/orchestrator` - Update config (concurrency, etc.)

#### 5. Persistence

**What persists (tied to repo):**
- Rules configuration (in `.canopy/config.toml`)
- Orchestrator configuration (concurrency, max_priority, etc.)
- Repository registration

**What resets (per activation):**
- Active agents
- In-progress tasks (returned to "ready" on deactivation)
- Pause state
- Real-time metrics

**What accumulates (across runs):**
- Run history (in SQLite)
- Agent history (in SQLite)
- Cost/token aggregates (per run)

### Migration Path

#### Phase 1: Always-Watching Behavior

Implement the core watch loop so activated orchestrators poll for work:
- [ ] Add polling loop to OrchestratorLifecycle
- [ ] Implement automatic idle ↔ active transitions
- [ ] Ensure context cancellation is the only exit path
- [ ] Add configurable poll interval

#### Phase 2: API Updates

Add new endpoints alongside old ones:
```go
// New
router.POST("/api/orchestrator/activate", h.HandleActivate)
router.POST("/api/orchestrator/deactivate", h.HandleDeactivate)

// Old (deprecated but functional)
router.POST("/api/orchestrator/run", h.HandleExecuteRun)  // calls HandleActivate internally
router.POST("/api/orchestrator/run/stop", h.HandleStopRun) // calls HandleDeactivate internally
```

- [ ] Add `GET /api/repos/:repo_id/orchestrator` endpoint
- [ ] Add `PATCH /api/repos/:repo_id/orchestrator` for config updates

#### Phase 3: Dashboard Updates

1. Rename UI elements (Start Run → Activate, etc.)
2. Add orchestrator state indicator showing off/idle/active/paused
3. Update state store to reflect new state model
4. Show "Watching for work..." in idle state

#### Phase 4: CLI Updates

1. `canopy run` becomes `canopy activate` (alias `canopy run` for backward compat)
2. `canopy status` shows orchestrator state (off/idle/active/paused)
3. `canopy config set concurrency=8` modifies repo orchestrator

### Dashboard State Store Changes

```typescript
// Current (stateStore.ts)
interface StateStore {
  currentRunId: string;           // Active run
  activeRunId: string;            // Selected for filtering
  runs: Run[];                    // Historical
  activeOrchestratorRun: ActiveRunStatus | null;
}

// Proposed
interface StateStore {
  // Orchestrator state (present when repo has activated orchestrator)
  orchestrator: {
    state: 'off' | 'idle' | 'active' | 'paused';
    config: OrchestratorConfig;
    currentRunId: string | null;  // null when off
  };

  // Run filtering (for history view)
  selectedRunId: string;  // empty = all runs
  runs: Run[];            // Historical runs
}
```

### Benefits Achieved

1. **Rules always accessible**: Rules panel works whenever a repo is selected (orchestrator exists)

2. **Simpler mental model**: "Activate the orchestrator" vs "Start a new run"

3. **Config changes feel natural**: Adjusting concurrency is tweaking the orchestrator, not a run

4. **Run continuity**: Clear that runs are work periods within a persistent orchestrator

5. **Better state representation**: Dashboard shows orchestrator state (off/idle/active/paused), not just "is there a run?"

6. **No orphaned work**: Orchestrator watches continuously until explicitly deactivated

## Open Questions

1. **Run auto-naming?** Should runs have human-readable names or descriptions?

2. **Multi-repo support?** How does this model extend to working on multiple repos?

3. **Poll interval tuning?** What's the right balance between responsiveness and resource usage?

## Implementation Tasks

See child tasks of canopy-2360 for implementation breakdown.

## References

- `pkg/daemon/orchestrator_manager.go` - Current OrchestratorLifecycle implementation
- `web/dashboard/src/stores/stateStore.ts` - Dashboard state model
- `pkg/daemon/rules_handlers.go` - Rules API (already supports standalone engine)
