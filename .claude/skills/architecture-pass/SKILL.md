# Architecture Pass Skill

Systematic architecture review skill for identifying structural issues in the canopy codebase.

## Overview

This skill guides deep architectural analysis to find issues that aren't apparent from surface-level code review. It traces data flows across system boundaries, verifies documented invariants, and discovers undocumented patterns that should be codified.

## Invocation

```bash
/architecture-pass                    # Full architecture review
/architecture-pass data-flow          # Trace data flows only
/architecture-pass concurrency        # Review sync patterns only
/architecture-pass api-contract       # Verify Go/TypeScript boundary
/architecture-pass state-management   # Review persistence/restoration
```

## Core Responsibilities

### 1. Trace Data Flows

Follow data from source to sink across ALL layers:

- **IPC Events** → **Daemon State** → **Persistence** → **Restoration** → **Dashboard**
- **CLI Commands** → **gRPC** → **Daemon Handlers** → **State Updates** → **Broadcasts**
- **Task Execution** → **Status Events** → **Database** → **UI Updates**

**Critical Questions:**
- Where are fields lost during transformation?
- Where are fields renamed inconsistently?
- Where do type conversions introduce bugs?
- Where does async timing cause race conditions?

**Key Files:**
- `pkg/ipc/protocol.go` - IPC message definitions
- `pkg/daemon/state.go` - RuntimeState and event handlers
- `pkg/daemon/persistence_manager.go` - State restoration
- `pkg/daemon/daemon.go` - gRPC handlers
- `web/dashboard/src/stores/stateStore.ts` - TypeScript state management
- `web/dashboard/src/lib/ipc.ts` - IPC client

### 2. Verify Documented Invariants

Check all invariants documented in `AGENTS.md`:

#### API Conventions (Go ↔ TypeScript)
- [ ] All JSON tags use snake_case
- [ ] TypeScript interfaces match Go struct tags exactly
- [ ] No camelCase in API layer

#### Concurrency Patterns
- [ ] `sync.Map` used only for append-only/disjoint-key access
- [ ] `sync.RWMutex` used for structured data requiring snapshots/iteration
- [ ] `atomic` types used only for simple flags/counters
- [ ] No improper mixing of patterns

#### Error Handling
- [ ] All errors wrapped with context using `%w`
- [ ] No silent error swallowing
- [ ] Sentinel errors used correctly with `errors.Is`
- [ ] Typed errors used correctly with `errors.As`

#### Persistence
- [ ] All canopy state lives in `$XDG_CACHE_HOME/canopy/`
- [ ] No state in repository `.canopy/` directory
- [ ] Repository identity in `repositories.json` keyed by absolute path

### 3. Discover Undocumented Invariants

Find patterns that should be documented:

**Look for:**
- Repeated validation patterns
- Implicit ordering requirements
- Undocumented state machine transitions
- Assumptions about field presence/nullability
- Timing dependencies between components
- Resource cleanup patterns
- Retry/backoff strategies

**When found:**
- Document in `AGENTS.md` immediately
- File a bead to update related code
- Add validation/assertions where appropriate

### 4. File Issues

For each architectural problem:

1. **Create a bead** with `[Arch]` prefix:
   ```bash
   bd create --title="[Arch] Description of issue" --priority=2
   ```

2. **Include in description:**
   - **Data flow diagram** showing the path through the system
   - **Exact code locations** with file paths and line numbers
   - **Current behavior** vs **expected behavior**
   - **Root cause analysis**
   - **Recommended fix** with specific steps
   - **Impact assessment** (what breaks if not fixed)

3. **Severity levels:**
   - **Critical**: Data loss, crashes, security issues
   - **High**: Incorrect behavior, race conditions, API contract violations
   - **Medium**: Inconsistencies, missing validation, poor error handling
   - **Low**: Code smells, missing documentation

## Architecture Pass Types

### Data Flow Pass

Trace specific fields/events through the entire system:

1. **Choose a data element** (e.g., `task_id`, `status`, `error_message`)
2. **Start at source** (where it enters the system)
3. **Follow transformations**:
   - How is it parsed/validated?
   - How is it stored in memory?
   - How is it persisted to disk?
   - How is it restored on startup?
   - How is it sent to UI?
   - How is it displayed?
4. **Verify at each boundary**:
   - Field names consistent?
   - Types preserved correctly?
   - Nullability handled?
   - Validation present?

**Common Issues:**
- Field renamed between layers (e.g., `task_id` → `taskId` → `id`)
- Optional fields treated as required
- Empty strings vs nil vs missing fields
- Timestamps in different formats

### Concurrency Pass

Review all synchronization patterns:

1. **Map all shared state**:
   ```bash
   grep -r "sync.Map\|sync.RWMutex\|sync.Mutex\|atomic\." pkg/
   ```

2. **For each synchronization primitive:**
   - Does it match the pattern in `AGENTS.md`?
   - Is the access pattern appropriate for the primitive?
   - Are all accesses protected?
   - Any race conditions?

3. **Check for common mistakes:**
   - Reading without lock
   - Lock held during blocking I/O
   - Locks acquired in inconsistent order (deadlock risk)
   - `sync.Map` used where RWMutex would be better
   - Missing defer for unlocks

**Key Files:**
- `pkg/daemon/state.go` - RuntimeState with RWMutex
- `pkg/orchestrator/orchestrator.go` - Result maps

### API Contract Pass

Verify Go/TypeScript boundary consistency:

1. **Extract all API types:**
   ```bash
   grep -r "json:\"" pkg/daemon pkg/orchestrator pkg/ipc
   ```

2. **Extract TypeScript interfaces:**
   ```bash
   grep -r "interface.*{" web/dashboard/src
   ```

3. **For each API type:**
   - [ ] Go struct has snake_case JSON tags
   - [ ] TypeScript interface exists with matching snake_case properties
   - [ ] Field types compatible (string, number, boolean, arrays, objects)
   - [ ] Optional fields marked with `?` in TypeScript match `omitempty` in Go
   - [ ] No camelCase anywhere in the API layer

4. **Test the boundary:**
   - Run `cd web/dashboard && npm run type-check`
   - Look for type errors indicating mismatches

**Common Issues:**
- Go uses camelCase in JSON tags
- TypeScript uses camelCase properties
- Missing TypeScript interface for Go type
- Optional field mismatch (`omitempty` without `?` or vice versa)

### State Management Pass

Review persistence and restoration:

1. **Trace state lifecycle:**
   - How is state created in memory?
   - What triggers persistence?
   - How is state serialized?
   - How is state deserialized?
   - How are errors handled?
   - How is state validated after restoration?

2. **Check restoration paths:**
   - Daemon restart (`PersistenceManager.RestoreState`)
   - Dashboard reconnect (IPC state sync)
   - CLI queries (gRPC state access)

3. **Verify invariants:**
   - All state in `$XDG_CACHE_HOME/canopy/`
   - No partial writes (atomic file operations)
   - Schema migration handled
   - Corrupt data handled gracefully

**Key Files:**
- `pkg/daemon/persistence_manager.go` - Restoration logic
- `pkg/daemon/daemon.go` - State initialization
- `pkg/daemon/state.go` - State structure

## Execution Protocol

### Step 1: Prepare

1. Read all key files to understand current architecture
2. Review recent commits for context on recent changes
3. Check existing beads for known architectural issues

### Step 2: Execute Pass

For each pass type requested:

1. **Create checklist** of items to verify
2. **Systematically check each item** - no shortcuts
3. **Document findings** as you go
4. **Create reproduction steps** for each issue

### Step 3: Analyze

1. **Group related issues** (same root cause, same subsystem)
2. **Assess severity** using the levels above
3. **Identify quick wins** (easy fixes with high impact)
4. **Identify systemic issues** (require broader refactoring)

### Step 4: Report

Create output in this format:

```markdown
# Architecture Pass Report: [Pass Type]

## Summary

- Critical: X issues
- High: Y issues
- Medium: Z issues
- Low: W issues

## Critical Issues

### [Issue Title]

**Severity:** Critical
**Subsystem:** [e.g., IPC, State Management]
**Bead:** canopy-xxxx

**Data Flow:**
[Diagram or description]

**Code Locations:**
- `path/to/file.go:123` - [what happens here]
- `path/to/file.ts:456` - [what happens here]

**Problem:**
[Detailed description]

**Impact:**
[What breaks]

**Recommended Fix:**
[Specific steps]

---

[Repeat for each issue]

## New Invariants to Document

Add to `AGENTS.md`:

```markdown
### [Section Name]

[Invariant description]

Example:
[Code example]
```

## Beads Filed

- canopy-xxxx: [Arch] Issue 1
- canopy-yyyy: [Arch] Issue 2
```

### Step 5: Update Documentation

1. **Add new invariants to AGENTS.md**
2. **Update code comments** where patterns should be followed
3. **Create follow-up beads** for documentation improvements

## Common Patterns to Check

### Field Name Transformations

```go
// Go API type
type TaskState struct {
    TaskID string `json:"task_id"`  // ✓ snake_case
}

// TypeScript interface
interface TaskState {
    task_id: string;  // ✓ matches Go
}

// IPC message
message TaskUpdate {
    string task_id = 1;  // ✓ consistent
}
```

**Anti-patterns:**
- `json:"taskId"` in Go
- `taskId: string` in TypeScript
- Renaming during transformation

### Concurrency Pattern Matching

```go
// ✓ sync.Map for append-only cache
var results sync.Map  // map[taskID]*Result

// ✓ RWMutex for structured state with snapshots
type RuntimeState struct {
    Agents map[string]*AgentState
    mu     sync.RWMutex
}

// ✓ atomic for simple flags
var paused atomic.Bool
```

**Anti-patterns:**
- `sync.Map` with iteration during updates
- `RWMutex` for single flag
- No synchronization on shared map

### Error Handling

```go
// ✓ Wrapped with context
if err := doSomething(); err != nil {
    return fmt.Errorf("failed to do something: %w", err)
}

// ✓ Logged if non-fatal
if err := cleanup(); err != nil {
    fmt.Fprintf(os.Stderr, "warning: cleanup failed: %v\n", err)
}
```

**Anti-patterns:**
- `return err` without context
- `_ = doSomething()` without comment
- Silent error swallowing

## Success Criteria

A complete architecture pass should:

1. [ ] Review all key files listed for the pass type
2. [ ] Identify at least one issue or confirm clean state
3. [ ] File beads for all issues found
4. [ ] Add new invariants to AGENTS.md
5. [ ] Provide specific reproduction steps
6. [ ] Assess impact of each issue
7. [ ] Recommend concrete fixes
8. [ ] Generate actionable follow-up work

## Integration with Workflow

This skill is typically invoked:

- **After major features** to catch architectural drift
- **Before releases** to ensure invariants hold
- **When debugging complex issues** to find root causes
- **During onboarding** to learn system architecture
- **When issues span multiple subsystems** to trace data flows

The output should generate beads that can be executed by normal development workflow or orchestrated with `canopy run`.

## Tips

- **Be systematic**: Don't skip steps, even if obvious
- **Be specific**: "Line 123" not "somewhere in that file"
- **Be visual**: Diagrams help show complex flows
- **Be actionable**: Every issue should have clear next steps
- **Be thorough**: One deep issue is better than ten shallow ones
