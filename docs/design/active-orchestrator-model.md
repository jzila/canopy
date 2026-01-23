# Design: Active Orchestrator Model

**Status**: Draft
**Epic**: canopy-i4sm
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

5. **Session continuity**: No conceptual continuity between runs on the same repo

## Proposed Design

### Core Concept: Always-Running Orchestrator

Shift from "discrete runs" to "repo has an orchestrator that can be activated/deactivated":

```
Repository
  └─ Orchestrator (always exists when repo is registered)
       ├─ State: idle | active | paused
       ├─ RulesEngine (persistent)
       ├─ Config (persistent)
       └─ Sessions (historical tracking)
            ├─ Session 1 (was: Run)
            ├─ Session 2
            └─ ...
```

### State Model

```go
type OrchestratorState string

const (
    OrchestratorIdle   OrchestratorState = "idle"    // Ready but not processing
    OrchestratorActive OrchestratorState = "active"  // Processing tasks
    OrchestratorPaused OrchestratorState = "paused"  // Active but temporarily stopped
)
```

**State transitions:**
- `idle → active`: User clicks "Start" (or CLI `canopy run`)
- `active → paused`: User clicks "Pause" (or CLI signal)
- `paused → active`: User clicks "Resume"
- `active → idle`: Run completes or user clicks "Stop"
- `paused → idle`: User clicks "Stop"

### What's Already Implemented

Looking at `pkg/daemon/orchestrator_manager.go`, much of this is already in place:

```go
type RepoOrchestrator struct {
    RepoPath    string
    RepoID      string
    State       OrchestratorState  // idle | active | paused
    RunID       string             // Current session ID when active
    rulesEngine *rules.Engine      // Persistent rules engine
    // ...
}
```

The `RegisterRepo()` function creates an always-running orchestrator:
```go
func (m *OrchestratorManager) RegisterRepo(repoPath string, repoID string) (*RepoOrchestrator, error)
```

### What Needs to Change

#### 1. Mental Model in UI

**Current**: "Start Run" / "Stop Run" with run selector
**Proposed**: "Activate" / "Deactivate" with session history

The UI should present:
- **Primary state**: Is the orchestrator active?
- **Secondary**: Which session are you viewing? (for history)

```
┌─────────────────────────────────────────────────────────────────┐
│ [Repository Selector]  │ Orchestrator: Active ● │ [Pause] [Stop]│
│                        │ Session: abc123 (2h ago)│              │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   [Agent Cards - filtered by selected session]                  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### 2. Terminology Changes

| Current Term | Proposed Term | Notes |
|--------------|---------------|-------|
| Run | Session | Historical work period |
| Run ID | Session ID | For filtering/tracking |
| Start Run | Activate | State transition |
| Stop Run | Deactivate | State transition |
| Active Run | Current Session | When orchestrator is active |

#### 3. API Endpoints

**Keep (rename optional)**:
- `POST /api/orchestrator/run` → `POST /api/orchestrator/activate`
- `POST /api/orchestrator/run/stop` → `POST /api/orchestrator/deactivate`
- `GET /api/daemon/runs` → `GET /api/daemon/sessions`

**New**:
- `GET /api/repos/:repo_id/orchestrator` - Get orchestrator state
- `PATCH /api/repos/:repo_id/orchestrator` - Update config (concurrency, etc.)

#### 4. Persistence Between Sessions

**What persists (tied to repo):**
- Rules configuration (already in `.canopy/config.toml`)
- Rules engine state (enabled/disabled rules)
- Orchestrator configuration (concurrency, max_priority, etc.)
- Repository registration

**What resets (per session):**
- Active agents
- In-progress tasks (returned to "ready" on session end)
- Pause state
- Real-time metrics

**What accumulates (across sessions):**
- Session history (in SQLite)
- Agent history (in SQLite)
- Cost/token aggregates (per session)

### Migration Path

#### Phase 1: Backend Alignment (Mostly Done)

The backend already has `RepoOrchestrator` with state management. Remaining work:
- [ ] Ensure rules engine persists in idle state (already implemented)
- [ ] Add `GET /api/repos/:repo_id/orchestrator` endpoint
- [ ] Add `PATCH /api/repos/:repo_id/orchestrator` for config updates

#### Phase 2: API Deprecation Layer

Add new endpoints alongside old ones:
```go
// New
router.POST("/api/orchestrator/activate", h.HandleActivate)
router.POST("/api/orchestrator/deactivate", h.HandleDeactivate)
router.GET("/api/repos/:repo_id/sessions", h.HandleListSessions)

// Old (deprecated but functional)
router.POST("/api/orchestrator/run", h.HandleExecuteRun)  // calls HandleActivate internally
router.POST("/api/orchestrator/run/stop", h.HandleStopRun) // calls HandleDeactivate internally
```

#### Phase 3: Dashboard Updates

1. Rename UI elements (Start Run → Activate, etc.)
2. Add orchestrator state indicator to header
3. Change "Run Selector" to "Session Selector" with historical context
4. Update state store terminology

#### Phase 4: CLI Updates

1. `canopy run` becomes `canopy activate` (alias `canopy run` for backward compat)
2. `canopy status` shows orchestrator state
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
  // Orchestrator state (always present when repo selected)
  orchestrator: {
    state: 'idle' | 'active' | 'paused';
    config: OrchestratorConfig;
    currentSessionId: string | null;  // null when idle
  };

  // Session filtering (for history view)
  selectedSessionId: string;  // empty = all sessions
  sessions: Session[];        // Historical sessions
}
```

### Benefits Achieved

1. **Rules always accessible**: Rules panel works whenever a repo is selected (orchestrator exists)

2. **Simpler mental model**: "Activate the orchestrator" vs "Start a new run"

3. **Config changes feel natural**: Adjusting concurrency is tweaking the orchestrator, not a run

4. **Session continuity**: Clear that sessions are work periods within a persistent orchestrator

5. **Better state representation**: Dashboard shows orchestrator state, not just "is there a run?"

## Open Questions

1. **Session auto-naming?** Should sessions have human-readable names or descriptions?

2. **Session grouping?** Should multiple activations in quick succession be grouped?

3. **Watch mode sessions?** How do watch mode iterations relate to sessions?

4. **Multi-repo support?** How does this model extend to working on multiple repos?

## Implementation Tasks

See child tasks of canopy-i4sm for implementation breakdown.

## References

- `pkg/daemon/orchestrator_manager.go` - Current RepoOrchestrator implementation
- `web/dashboard/src/stores/stateStore.ts` - Dashboard state model
- `pkg/daemon/rules_handlers.go` - Rules API (already supports standalone engine)
