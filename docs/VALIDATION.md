# Validation and Repair Agents

This guide covers Canopy's post-merge validation system and the repair agent that automatically fixes validation failures.

## Overview

After agents complete their work and changes are merged, Canopy can automatically run validation commands (build, test, lint) to ensure the merged code is correct. If validation fails, a **repair agent** attempts to fix the issues automatically.

```
Agent completes task
        │
        ▼
  Changes merged
        │
        ▼
┌───────────────────┐
│  Run validation   │◄─────────────────┐
│  (build, test)    │                  │
└────────┬──────────┘                  │
         │                             │
    ┌────┴────┐                        │
    │         │                        │
  Pass      Fail                       │
    │         │                        │
    ▼         ▼                        │
  Done    Spawn repair ──► Fix ──► Re-validate
              │
              ▼
         Max attempts?
              │
         ┌────┴────┐
         │         │
        No        Yes
         │         │
         └────►    ▼
              File bead
              (manual fix)
```

## Configuration

Create `.canopy/validation.toml` in your project:

```toml
[validation]
enabled = true              # Enable post-merge validation
strict = false              # true = revert on failure, false = keep merge
timeout = "5m"              # Global timeout for all steps
max_repair_attempts = 3     # Retries before filing issue

[[validation.steps]]
name = "build"
command = "go build ./..."
timeout = "2m"
required = true             # Stop on failure

[[validation.steps]]
name = "test"
command = "go test ./..."
timeout = "5m"
required = true

[[validation.steps]]
name = "lint"
command = "golangci-lint run"
timeout = "2m"
required = false            # Continue on failure
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable validation |
| `strict` | bool | `false` | Revert merge on failure (after all repair attempts) |
| `timeout` | string | `"5m"` | Global timeout for all steps |
| `max_repair_attempts` | int | `3` | Max repair retries before filing issue |

### Step Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `name` | string | required | Human-readable step name |
| `command` | string | required | Shell command to execute |
| `timeout` | string | global | Step-specific timeout |
| `required` | bool | `false` | Stop validation if this step fails |

## Validation Flow

### Step Execution

Steps run sequentially in the order defined:

1. Create context with global timeout
2. For each step:
   - Apply step-specific timeout (or global)
   - Execute command via `sh -c` in working directory
   - Capture stdout/stderr
   - Track exit code and duration
3. Stop if:
   - A **required** step fails
   - Any step fails in **strict** mode
   - All steps complete

### Status Values

| Status | Description |
|--------|-------------|
| `pending` | Not yet started |
| `running` | Steps executing |
| `passed` | All required steps succeeded |
| `failed` | Required step failed |
| `skipped` | Validation disabled |
| `repairing` | Repair agent attempting fix |

## Repair Agent

When validation fails, Canopy spawns a repair agent to fix the issue.

### Repair Context

The repair agent receives:

1. **Failed step details**: Name, command, exit code, output (truncated to 8KB)
2. **Merged diff**: What changes were merged (truncated to 12KB, full in files)
3. **Previous attempts**: Summaries of prior failed repairs (to avoid repetition)
4. **Task context**: Original task ID and title

### Context Files

During repair, these files are written to `.canopy/repair/`:

| File | Contents |
|------|----------|
| `merged.diff` | Full git diff of merged changes |
| `validation-output.txt` | Complete validation output |
| `failed-step.txt` | Failed step details |
| `task-context.txt` | Task metadata |
| `previous-attempts/attempt-N.txt` | Details of attempt N |

### Repair Behavior

The repair agent:
- Runs **directly on the working directory** (not in overlay)
- Has the merge already applied
- Can commit fixes directly to the repository
- Is tracked as a child of the original implementor agent
- Sees what previous attempts tried to avoid repeating failures

### Repair Agent ID Format

```
agent-{runID[:8]}-{taskID}-repair-{attemptNum}
```

Example: `agent-abc12345-task-foo-repair-2`

## Strict vs Lenient Mode

### Lenient Mode (Default)

```toml
[validation]
strict = false
```

When validation fails after all repair attempts:
- Merge **remains** in the repository
- High-priority bead filed: "Repair exhausted: {task}"
- Task marked with `merged_needs_repair` status
- Manual intervention required

Use when: You prefer to keep progress and fix forward.

### Strict Mode

```toml
[validation]
strict = true
```

When validation fails after all repair attempts:
- Merge is **reverted**
- Task marked as failed
- No changes persist

Use when: Broken code must never reach main branch.

## Auto-Filed Beads

When repair exhausts all attempts, a bead is automatically filed:

**Title**: `Repair exhausted: {task-title}`

**Content includes**:
- Validation step that failed (name, command)
- Final validation output (truncated to 2KB)
- Summary of each repair attempt:
  - Attempt number and status
  - Error message
  - Commits applied
  - Files modified
  - Agent's summary
- Merged diff (truncated to 4KB)
- Action required notice

**Priority**: 1 (high)

### Example Bead

```markdown
## Validation Failure - Repair Exhausted

Task: canopy-abc - Implement user authentication

### Validation Step
- Step: test
- Command: npm test
- Final output:
```
FAIL  src/auth.test.ts
  ✕ Should validate JWT tokens
    Expected: true
    Received: false
```

### Repair Attempts
1. Attempt 1: failed
   Error: Test still failing after adding token validation
   Commits applied: 1
   Files modified: 2
     - src/auth.ts
     - src/utils.ts

2. Attempt 2: failed
   Error: New error introduced in token parsing
   Commits applied: 1
   Files modified: 1
     - src/auth.ts

3. Attempt 3: failed
   Error: Unable to resolve type mismatch
   Commits applied: 1
   Files modified: 2
     - src/auth.ts
     - src/types.ts

### Merged Diff
```diff
+ export function validateToken(token: string): boolean {
+   // JWT validation logic
+ }
```

### Action Required
Manual intervention needed. Automated repair exhausted after 3 attempts.
```

## History and Audit Trail

Canopy records validation events on task beads:

| Event | Description |
|-------|-------------|
| Validation failure | Per-attempt failure with step details |
| Repair attempt | Commit count, agent ID, success/failure |
| Final status | `validated`, `repaired`, `needs_manual_fix`, or `skipped` |

## Troubleshooting

### Validation Never Runs

Check:
1. `.canopy/validation.toml` exists
2. `enabled = true` is set
3. At least one step is defined

### Repair Agent Can't Fix Issue

The repair agent may fail when:
- Issue requires architectural changes
- Problem is in dependencies, not code
- Tests require external services
- Fix requires human judgment

In these cases, the filed bead provides full context for manual resolution.

### Timeouts

Increase timeouts for slow operations:

```toml
[validation]
timeout = "15m"              # Global

[[validation.steps]]
name = "test"
command = "npm test"
timeout = "10m"              # Step-specific
```

### Required vs Optional Steps

Use `required = true` for critical checks (build, core tests).
Use `required = false` for advisory checks (lint, formatting).

```toml
[[validation.steps]]
name = "build"
command = "go build ./..."
required = true              # Merge fails if build fails

[[validation.steps]]
name = "fmt-check"
command = "gofmt -l ."
required = false             # Warning only
```

## Examples

### Go Project

```toml
[validation]
enabled = true
strict = true
timeout = "10m"
max_repair_attempts = 3

[[validation.steps]]
name = "build"
command = "go build ./..."
timeout = "2m"
required = true

[[validation.steps]]
name = "test"
command = "go test ./..."
timeout = "5m"
required = true

[[validation.steps]]
name = "vet"
command = "go vet ./..."
timeout = "1m"
required = false
```

### Node.js Project

```toml
[validation]
enabled = true
strict = false
timeout = "10m"
max_repair_attempts = 3

[[validation.steps]]
name = "typecheck"
command = "npm run type-check"
timeout = "2m"
required = true

[[validation.steps]]
name = "lint"
command = "npm run lint"
timeout = "3m"
required = true

[[validation.steps]]
name = "test"
command = "npm test"
timeout = "5m"
required = true

[[validation.steps]]
name = "build"
command = "npm run build"
timeout = "5m"
required = false
```

### Python Project

```toml
[validation]
enabled = true
strict = false
timeout = "10m"
max_repair_attempts = 2

[[validation.steps]]
name = "typecheck"
command = "mypy ."
timeout = "3m"
required = true

[[validation.steps]]
name = "lint"
command = "ruff check ."
timeout = "1m"
required = true

[[validation.steps]]
name = "test"
command = "pytest"
timeout = "5m"
required = true
```

### Rust Project

```toml
[validation]
enabled = true
strict = true
timeout = "15m"
max_repair_attempts = 3

[[validation.steps]]
name = "check"
command = "cargo check"
timeout = "3m"
required = true

[[validation.steps]]
name = "clippy"
command = "cargo clippy -- -D warnings"
timeout = "3m"
required = true

[[validation.steps]]
name = "test"
command = "cargo test"
timeout = "10m"
required = true
```

## Worker Chains and Parent-Child Agent Relationships

Worker chains track the relationship between agents that work together to complete a task. When an implementor agent finishes and validation fails, child agents (resolver, repair) are spawned to fix issues.

### What is a Worker Chain?

A worker chain is the sequence of agents that work on a single task:

```
┌─────────────────┐
│  Worker Agent   │  ← Original implementor
│  (implementor)  │
└────────┬────────┘
         │
    ┌────┴────────────────────────────┐
    │                                 │
    ▼                                 ▼
┌───────────────┐             ┌───────────────┐
│   Resolver    │             │  Validation   │
│  (optional)   │             │   (if merge   │
│               │             │   succeeds)   │
└───────────────┘             └───────┬───────┘
                                      │ Fails
                                      ▼
                              ┌───────────────┐
                              │ Repair Agent  │
                              │   #1, #2...   │
                              └───────────────┘
```

### Parent-Child Relationships

Each spawned agent tracks its parent via `parent_agent_id`:

| Agent Type | Parent | When Spawned |
|------------|--------|--------------|
| Resolver | Implementor | Merge conflict detected |
| Repair Agent | Implementor | Validation fails after merge |

**IPC Protocol Fields** (from `pkg/ipc/protocol.go`):

```go
type AgentStartPayload struct {
    AgentID       string `json:"agent_id"`
    ParentAgentID string `json:"parent_agent_id,omitempty"` // ID of parent agent
    // ...
}
```

### Agent ID Formats

- **Implementor**: `agent-{runID[:8]}-{taskID}`
- **Resolver**: `agent-{runID[:8]}-{taskID}-resolver`
- **Repair Agent**: `agent-{runID[:8]}-{taskID}-repair-{attemptNum}`

Example chain:
```
agent-abc12345-canopy-xyz           (implementor)
├── agent-abc12345-canopy-xyz-resolver      (if conflict)
├── agent-abc12345-canopy-xyz-repair-1      (first repair)
└── agent-abc12345-canopy-xyz-repair-2      (second repair)
```

### When Child Agents Spawn

**Resolver Agents** spawn when:
- Merge queue attempts to apply patches from an implementor
- `git am` fails due to conflicts with concurrent changes
- The resolver receives the failed patches and current HEAD state

**Repair Agents** spawn when:
- Merge succeeds but validation fails (build error, test failure)
- Previous repair attempts didn't fix the issue
- `max_repair_attempts` hasn't been reached

### How Chains are Built

The daemon tracks parent-child relationships:

1. **On agent_start**: If `parent_agent_id` is present, add child to parent's `child_agent_ids` list
2. **On restore**: `RebuildAgentChildLinks()` reconstructs relationships from `parent_agent_id` fields
3. **In the dashboard**: `WorkerChainTimeline` component queries all agents, filtering by `parent_agent_id`

```typescript
// From WorkerChainTimeline.tsx
const resolverAgents = childAgents.filter(child =>
  child.parent_agent_id === agent.id && !child.task_id.includes('repair')
);

const repairAgents = childAgents.filter(child =>
  child.task_id.includes('repair') || child.parent_agent_id === agent.id
);
```

### Timeline Visualization

The dashboard's `WorkerChainTimeline` component renders the worker chain as a vertical timeline:

```
● Worker Agent          completed    2m 15s
│
├─● Conflict Resolver   resolved     45s
│
├─● Validation          failed       12s
│  ├─ build            passed       8s
│  └─ test             failed       4s
│
└─● Repair Agent #1     repairing    ...
```

Each timeline item shows:
- **Icon**: Type-specific (play, merge, wrench, check/x)
- **Title**: Agent type or validation step name
- **Status**: Current state with color coding
- **Duration**: Time taken (for completed items)
- **Expandable output**: Error messages or validation details

### Status Colors

| Status | Color | Meaning |
|--------|-------|---------|
| `completed`/`passed`/`merged` | Green | Success |
| `failed` | Red | Failure |
| `running`/`pending`/`merging` | Blue | In progress |
| `repairing` | Orange | Repair agent active |
| `skipped` | Gray | Not executed |

## Dashboard Integration

The dashboard shows validation status in real-time:

- **Validation Status**: Current state (`running`, `passed`, `failed`, `repairing`)
- **Repair Attempts**: Count of repair attempts and their outcomes
- **Validation Message**: Details about current step or failure

When repair is exhausted:
- Task shows `merged_needs_repair` status
- Orange indicator in merge status
- Link to filed bead for manual resolution

## See Also

- [Configuration Reference](CONFIGURATION.md#validation-configuration) - All validation options
- [Getting Started](GETTING-STARTED.md#post-merge-validation) - Quick setup
- [Architecture](ARCHITECTURE.md) - System design
