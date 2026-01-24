# Agent Lifecycle State Machine

## Status: Approved (Architect Review Complete)

## Problem Statement

The current agent status model uses three independent fields updated from different code paths:

```go
type AgentState struct {
    Status           AgentStatus  // starting, running, completed, failed, timed_out, cancelled
    MergeStatus      MergeStatus  // none, pending, acquiring, merging, resolving, merged, failed, skipped, merged_needs_repair
    ValidationStatus string       // pending, running, passed, failed, skipped, repairing
    RepairAttempts   int
}
```

This creates several problems:

1. **Race conditions**: Merge processor updates `MergeStatus` while `OnDoneFn` callback updates `Status` - these are not synchronized, leading to windows where task shows "completed" but agent shows "running"

2. **Invalid state combinations**: Nothing prevents `Status=completed` with `ValidationStatus=repairing` or other nonsensical combinations

3. **Scattered update logic**: Status fields are mutated from:
   - `pkg/mergequeue/processor.go` (merge status, validation status)
   - `pkg/daemon/orchestrator_manager.go` (agent status via callbacks)
   - `pkg/daemon/state.go` (event handlers)
   - `pkg/repairagent/spawner.go` (repair status)

4. **UI confusion**: Dashboard must interpret multiple fields to determine what to display. `canopy ps` shows "running" during validation/repair because main `Status` field isn't updated.

5. **No transition validation**: Any code can set any status at any time with no enforcement of valid transitions.

## Proposed Solution

Replace the multi-field model with a single **Agent Lifecycle State Machine** that:
- Has one authoritative state field
- Defines explicit valid transitions
- Rejects invalid transitions with errors
- Emits events atomically with state changes
- Provides computed properties for backwards compatibility

## State Diagram

```
                         ┌────────────────┐
                         │    Starting    │
                         └───────┬────────┘
                                 │ AgentSpawned
                                 ▼
                         ┌────────────────┐
        ┌───────────────►│    Running     │◄──────────────────┐
        │                └───────┬────────┘                   │
        │                        │ WorkComplete               │
        │ Retry                  ▼                            │ Retry
        │ (attempts remaining)  ┌────────────────┐            │ (attempts remaining)
        │                       │  QueuedForMerge│            │
        │                       └───────┬────────┘            │
        │                               │ MergeStarted        │
        │                               ▼                     │
        │                       ┌────────────────┐            │
        │              ┌────────│    Merging     │────────┐   │
        │              │        └────────────────┘        │   │
        │              │ Conflict                  Success│   │
        │              ▼                                  ▼   │
        │      ┌────────────────┐                ┌────────────────┐
        │      │   Resolving    │───────────────►│   Validating   │
        │      └───────┬────────┘   Resolved     └───────┬────────┘
        │              │                                 │
        │              │ ResolveFailed          ┌────────┼────────┐
        │              ▼                        │        │        │
        │      ┌────────────────┐        Passed │ Failed │ Skipped│
        │      │  MergeFailed   │◄──────────────┘        │        │
        │      └───────┬────────┘                        ▼        │
        │              │                         ┌────────────────┐│
        └──────────────┤                         │   Repairing    ││
                       │                         └───────┬────────┘│
                       │                                 │         │
                       │              RepairSuccess      │         │
                       │    ┌────────────────────────────┘         │
                       │    │                                      │
                       │    │  RepairExhausted                     │
                       │    │         │                            │
                       ▼    ▼         ▼                            ▼
               ┌────────────────┐  ┌────────────────┐      ┌────────────────┐
               │    Failed      │  │ NeedsAttention │      │   Completed    │
               └────────────────┘  └────────────────┘      └────────────────┘

Terminal States: Failed, Completed, NeedsAttention, Cancelled, TimedOut
```

## State Definitions

| State | Description | What User Sees |
|-------|-------------|----------------|
| `Starting` | Agent process spawning | "Starting..." |
| `Running` | Agent executing work (Claude API) | "Running" with live output |
| `QueuedForMerge` | Work done, waiting in merge queue | "Queued (position N)" |
| `Merging` | Applying changes to repo (includes pre-commit hook repair if needed) | "Merging..." |
| `Resolving` | Conflict resolver agent running | "Resolving conflicts..." |
| `Validating` | Post-merge validation running | "Validating..." |
| `Repairing` | Repair agent fixing validation failure | "Repairing (attempt N/M)" |
| `MergeFailed` | Merge/pre-commit failed, will retry if attempts remain | "Merge failed (retry N/M)" |
| `Completed` | Successfully done | "Completed" (green) |
| `Failed` | Terminal failure | "Failed" (red) |
| `NeedsAttention` | Repair exhausted, human needed | "Needs attention" (orange) |
| `Cancelled` | User cancelled | "Cancelled" |
| `TimedOut` | Exceeded time limit | "Timed out" |

## Transition Table

| From State | Event | To State | Condition |
|------------|-------|----------|-----------|
| Starting | AgentSpawned | Running | - |
| Running | WorkComplete | QueuedForMerge | - |
| Running | WorkFailed | Failed | max_retries exhausted |
| Running | WorkFailed | Running | retry attempt |
| QueuedForMerge | MergeStarted | Merging | - |
| Merging | MergeSuccess | Validating | validation enabled |
| Merging | MergeSuccess | Completed | validation disabled |
| Merging | MergeConflict | Resolving | - |
| Merging | MergeFailed | MergeFailed | - |
| Resolving | ResolveSuccess | Validating | - |
| Resolving | ResolveFailed | MergeFailed | - |
| Validating | ValidationPassed | Completed | - |
| Validating | ValidationFailed | Repairing | repair enabled, attempts remaining |
| Validating | ValidationFailed | NeedsAttention | repair exhausted |
| Validating | ValidationFailed | NeedsAttention | lenient mode, repair disabled |
| Validating | ValidationFailed | Failed | strict mode |
| Validating | ValidationSkipped | Completed | - |
| Repairing | RepairComplete | Validating | re-run validation |
| MergeFailed | Retry | Running | attempts remaining |
| MergeFailed | RetriesExhausted | Failed | - |
| * | Cancel | Cancelled | - |
| * | Timeout | TimedOut | - |

## Implementation Design

### Core Types

```go
// pkg/lifecycle/state.go

// AgentLifecycleState represents the current state in the agent lifecycle.
type AgentLifecycleState string

const (
    StateStarting       AgentLifecycleState = "starting"
    StateRunning        AgentLifecycleState = "running"
    StateQueuedForMerge AgentLifecycleState = "queued_for_merge"
    StateMerging        AgentLifecycleState = "merging"
    StateResolving      AgentLifecycleState = "resolving"
    StateValidating     AgentLifecycleState = "validating"
    StateRepairing      AgentLifecycleState = "repairing"
    StateMergeFailed    AgentLifecycleState = "merge_failed"
    StateCompleted      AgentLifecycleState = "completed"
    StateFailed         AgentLifecycleState = "failed"
    StateNeedsAttention AgentLifecycleState = "needs_attention"
    StateCancelled      AgentLifecycleState = "cancelled"
    StateTimedOut       AgentLifecycleState = "timed_out"
)

// AgentEvent represents an event that can trigger a state transition.
type AgentEvent string

const (
    EventAgentSpawned     AgentEvent = "agent_spawned"
    EventWorkComplete     AgentEvent = "work_complete"
    EventWorkFailed       AgentEvent = "work_failed"
    EventMergeStarted     AgentEvent = "merge_started"
    EventMergeSuccess     AgentEvent = "merge_success"
    EventMergeConflict    AgentEvent = "merge_conflict"
    EventMergeFailed      AgentEvent = "merge_failed"
    EventResolveSuccess   AgentEvent = "resolve_success"
    EventResolveFailed    AgentEvent = "resolve_failed"
    EventValidationPassed AgentEvent = "validation_passed"
    EventValidationFailed AgentEvent = "validation_failed"
    EventValidationSkipped AgentEvent = "validation_skipped"
    EventRepairComplete   AgentEvent = "repair_complete"
    EventRetry            AgentEvent = "retry"
    EventRetriesExhausted AgentEvent = "retries_exhausted"
    EventCancel           AgentEvent = "cancel"
    EventTimeout          AgentEvent = "timeout"
)

// TransitionContext provides context for state transitions.
type TransitionContext struct {
    ValidationEnabled bool
    RepairEnabled     bool
    StrictMode        bool
    AttemptsRemaining int
    RepairAttempt     int
    MaxRepairAttempts int
    QueuePosition     int
    Error             string
}

// AgentLifecycle manages state transitions for a single agent.
type AgentLifecycle struct {
    mu            sync.RWMutex
    state         AgentLifecycleState
    stateHistory  []StateTransition
    context       TransitionContext
    onTransition  func(from, to AgentLifecycleState, event AgentEvent)
}

// StateTransition records a state change.
type StateTransition struct {
    From      AgentLifecycleState
    To        AgentLifecycleState
    Event     AgentEvent
    Timestamp time.Time
    Context   TransitionContext
}

// Transition attempts to transition to a new state based on an event.
// Returns an error if the transition is invalid.
func (l *AgentLifecycle) Transition(event AgentEvent, ctx TransitionContext) error {
    l.mu.Lock()
    defer l.mu.Unlock()

    newState, err := l.nextState(l.state, event, ctx)
    if err != nil {
        return fmt.Errorf("invalid transition: %s + %s: %w", l.state, event, err)
    }

    oldState := l.state
    l.state = newState
    l.context = ctx
    l.stateHistory = append(l.stateHistory, StateTransition{
        From:      oldState,
        To:        newState,
        Event:     event,
        Timestamp: time.Now(),
        Context:   ctx,
    })

    if l.onTransition != nil {
        l.onTransition(oldState, newState, event)
    }

    return nil
}

// State returns the current state (thread-safe).
func (l *AgentLifecycle) State() AgentLifecycleState {
    l.mu.RLock()
    defer l.mu.RUnlock()
    return l.state
}

// IsTerminal returns true if the current state is terminal.
func (l *AgentLifecycle) IsTerminal() bool {
    switch l.State() {
    case StateCompleted, StateFailed, StateNeedsAttention, StateCancelled, StateTimedOut:
        return true
    }
    return false
}
```

### Backwards Compatibility

```go
// LegacyStatus returns the legacy AgentStatus for backwards compatibility.
func (l *AgentLifecycle) LegacyStatus() AgentStatus {
    switch l.State() {
    case StateStarting:
        return AgentStatusStarting
    case StateRunning, StateQueuedForMerge, StateMerging, StateResolving, StateValidating, StateRepairing:
        return AgentStatusRunning
    case StateCompleted:
        return AgentStatusCompleted
    case StateFailed, StateMergeFailed, StateNeedsAttention:
        return AgentStatusFailed
    case StateCancelled:
        return AgentStatusCancelled
    case StateTimedOut:
        return AgentStatusTimedOut
    }
    return AgentStatusRunning
}

// LegacyMergeStatus returns the legacy MergeStatus for backwards compatibility.
func (l *AgentLifecycle) LegacyMergeStatus() MergeStatus {
    switch l.State() {
    case StateStarting, StateRunning:
        return MergeStatusNone
    case StateQueuedForMerge:
        return MergeStatusPending
    case StateMerging:
        return MergeStatusMerging
    case StateResolving:
        return MergeStatusResolving
    case StateValidating, StateRepairing:
        return MergeStatusMerged // validation/repair happen AFTER successful merge
    case StateCompleted:
        return MergeStatusMerged
    case StateFailed, StateMergeFailed:
        return MergeStatusFailed
    case StateNeedsAttention:
        return MergeStatusMergedNeedsRepair
    }
    return MergeStatusNone
}

// LegacyValidationStatus returns the legacy validation status string.
func (l *AgentLifecycle) LegacyValidationStatus() string {
    switch l.State() {
    case StateValidating:
        return "running"
    case StateRepairing:
        return "repairing"
    case StateCompleted:
        return "passed"
    case StateNeedsAttention:
        return "failed"
    }
    return ""
}
```

### Integration Points

1. **Merge Queue Processor**: Replace direct field mutations with `Transition()` calls
2. **RuntimeState**: Store `AgentLifecycle` instead of separate fields
3. **IPC Events**: Map events to `AgentEvent` constants
4. **Dashboard**: Read `lifecycle_state` field, use legacy fields as fallback
5. **canopy ps**: Display `State()` directly instead of interpreting multiple fields

### Transition Ownership and Integration

The state machine integrates with the existing event-driven architecture as follows:

**Transition Call Sites:**
1. **Merge Queue Processor** (`pkg/mergequeue/processor.go`):
   - Calls `lifecycle.Transition()` for all merge/validation/repair events
   - Replaces scattered `sendMergeStatus()` calls with atomic transitions
   - Highest density of state transitions

2. **Orchestrator Callbacks** (`pkg/daemon/orchestrator_manager.go`):
   - `OnAgentStartFn` → `Transition(EventAgentSpawned)`
   - `OnDoneFn` → `Transition(EventWorkComplete)`
   - `OnFailFn` → `Transition(EventWorkFailed)`

3. **Event Handlers** (`pkg/daemon/state.go`):
   - Subscribe to EventBus events
   - Call `lifecycle.Transition()` instead of direct field assignment
   - Handle validation/repair status updates

**Event Publishing:**
- `onTransition` callback publishes to EventBus when state changes
- Maintains event-driven architecture for dashboard/WebSocket updates
- Events contain both new lifecycle state and legacy fields for backwards compat

**Concurrency:**
- All transitions protected by `AgentLifecycle.mu` mutex
- `Transition()` is atomic: state update + history append + callback
- No gaps where state is inconsistent

### Child Agent Lifecycle

Resolver and repair agents are spawned as independent agents with their own lifecycle instances:

- **Resolver agents**: Spawned when merge conflicts occur. Parent transitions to `Resolving`, waits for resolver to complete, then transitions to `Validating` (success) or `MergeFailed` (failure).

- **Repair agents**: Spawned when validation fails. Parent transitions to `Repairing`, waits for repair to complete, then transitions to `Validating` (re-run validation).

**Key properties:**
- Child agents have their own `AgentLifecycle` instance
- Child agent states are independent and queryable (for UI display)
- Parent transitions based on child's **final result** only
- Child completion doesn't directly trigger parent state changes (processor logic handles this)
- Parent-child relationship tracked via `ParentAgentID` and `ChildAgentIDs[]` fields

## Migration Strategy

### Phase 1: Add State Machine (Non-Breaking)
- Add `pkg/lifecycle` package with state machine implementation
- Add `LifecycleState` field to `AgentState` (new field, optional)
- Existing fields remain and continue to work
- State machine runs in parallel, logging any divergence

### Phase 2: Migrate Writers
- Update merge processor to call `Transition()` instead of sending separate events
- Update orchestrator callbacks to use state machine
- Keep legacy field updates for backwards compatibility

### Phase 3: Migrate Readers
- Update dashboard to prefer `lifecycle_state` over legacy fields
- Update `canopy ps` to use state machine
- Update persistence to store/restore lifecycle state

### Phase 4: Remove Legacy (Breaking)
- Remove separate `Status`, `MergeStatus`, `ValidationStatus` fields
- Remove legacy compatibility methods
- Clean up redundant event types

## Testing Strategy

1. **Unit tests**: Test all valid transitions, reject invalid ones
2. **Property tests**: Verify state machine invariants (terminal states stay terminal, etc.)
3. **Integration tests**: Verify IPC events map correctly to transitions
4. **Divergence tests**: Run old and new systems in parallel, flag any differences

## Alternatives Considered

### Alternative 1: Keep Separate Fields, Add Coordinator
Add a coordinator that serializes all status updates through a single channel. Rejected because it doesn't solve the semantic confusion of multiple fields.

### Alternative 2: Enum with Substates
Use a primary enum with optional substates (e.g., `Running(MergePhase)`). Rejected because it's more complex and Go doesn't have sum types.

### Alternative 3: Event Sourcing
Store all events and derive state. Considered for future work, but adds significant complexity. The state machine can be extended to event sourcing later.

## Design Decisions

Based on architectural review, the following questions have been resolved:

### 1. Should `QueuedForMerge` be visible to users?

**YES**. The merge queue position is explicitly tracked (`MergeQueuePos` field) and users need to know when work is done but waiting. Display as "Queued (position N)" in UI.

### 2. How to handle daemon restart?

**Restore from `LifecycleState` as source of truth**. During migration (Phases 1-3), database stores both lifecycle state and legacy fields. After Phase 4, only `lifecycle_state` is stored. Legacy fields are derived via `LegacyStatus()` methods for any remaining compatibility needs.

### 3. Should state history be persisted?

**In-memory initially**. State history (`stateHistory[]`) is kept in memory for debugging but not persisted to database. Can add persistence later if debugging needs warrant it.

### 4. How to handle API versioning for clients expecting legacy fields?

**Dual-field approach during Phases 1-3**. IPC events and JSON responses include:
- `lifecycle_state` (new field)
- `status`, `merge_status`, `validation_status` (legacy fields, derived from lifecycle)

Dashboard checks for `lifecycle_state` first, falls back to legacy. Remove legacy fields only in Phase 4 as a documented breaking change requiring major version bump.

## Implementation Risks and Mitigations

| Risk | Severity | Impact | Mitigation |
|------|----------|--------|------------|
| Invalid transition logic in `nextState()` | **Critical** | Agents stuck in invalid states, orchestration breaks | Comprehensive unit tests covering all 30+ transitions; property-based tests (terminal states never transition out); parallel tracking in Phase 1 to detect divergence |
| Race between `Transition()` and EventBus | **High** | State updates lost, UI shows stale data | `Transition()` holds mutex during callback; EventBus publish is synchronous within lock; add integration test for concurrent transitions |
| Breaking changes to existing clients | **Medium** | Dashboard/CLI breaks on upgrade | Dual-field approach (lifecycle + legacy fields); phased migration over 4 releases; feature flag to enable/disable lifecycle |
| Performance regression | **Low** | Slower agent throughput | Benchmark `Transition()` overhead; state history bounded (e.g., last 100 transitions); profile in staging with 50+ concurrent agents |
| Debugging complexity | **Medium** | Harder to trace state issues | State history kept in memory; log all transitions at DEBUG level; add `canopy ps --show-history` command |

## References

- [canopy-jygc](beads://canopy-jygc): Agent appears 'running' after merge complete due to status update race
- [canopy-vf1u](beads://canopy-vf1u): Repair agents run silently - no UI indication parent agent is repairing
