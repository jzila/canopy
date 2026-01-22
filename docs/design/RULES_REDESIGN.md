# Rules System Redesign: Reactive Loop Pattern

## Problem Statement

The current rules system has several architectural and UX issues:

1. **Config is not source of truth** - Daemon starts with hard-coded defaults; `.canopy/config.toml` is never auto-loaded
2. **Confusing rule categories** - Three categories (Config Settings, Custom Rules, Runtime Rules) create confusion
3. **Global rules engine** - Shared across all repos, causing cross-contamination
4. **Poor editing UX** - In-place editing is cramped (11 fields in ~200px card)
5. **Undiscoverable syntax** - Users don't know what conditions/actions are available

## Architecture Overview

### Current Architecture (Problems)

```
┌─────────────────────────────────────────────────────────────┐
│                 OrchestratorManager (Global)                │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  rules.Engine (SHARED across all repos)             │   │
│  │  - Starts with hard-coded defaults                  │   │
│  │  - Config file never loaded automatically           │   │
│  │  - 3 categories: config / custom / runtime          │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
   Orchestrator          Orchestrator          Orchestrator
   (repo A)              (repo B)              (repo C)
   [uses global]         [uses global]         [uses global]
```

### New Architecture (Reactive Loop)

```
┌─────────────────────────────────────────────────────────────┐
│                 OrchestratorManager                         │
│  (No rules engine - just orchestrator lifecycle)            │
└─────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│  Orchestrator   │  │  Orchestrator   │  │  Orchestrator   │
│    (repo A)     │  │    (repo B)     │  │    (repo C)     │
│ ┌─────────────┐ │  │ ┌─────────────┐ │  │ ┌─────────────┐ │
│ │rules.Engine │ │  │ │rules.Engine │ │  │ │rules.Engine │ │
│ │(per-repo)   │ │  │ │(per-repo)   │ │  │ │(per-repo)   │ │
│ └─────────────┘ │  │ └─────────────┘ │  │ └─────────────┘ │
└────────┬────────┘  └────────┬────────┘  └────────┬────────┘
         │                    │                    │
         ▼                    ▼                    ▼
   .canopy/config.toml  .canopy/config.toml  .canopy/config.toml
   (repo A)             (repo B)             (repo C)
```

### Reactive Loop Flow

```
┌──────────────────┐
│ Run Activated    │
│ (start/switch)   │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Load Config      │◄──────────────────────────────┐
│ from disk        │                               │
└────────┬─────────┘                               │
         │                                         │
         ▼                                         │
┌──────────────────┐                               │
│ Create/Update    │                               │
│ rules.Engine     │                               │
└────────┬─────────┘                               │
         │                                         │
         ▼                                         │
┌──────────────────┐     ┌──────────────────┐      │
│ Dashboard shows  │────►│ User edits rule  │      │
│ unified rules    │     │ (modal)          │      │
└──────────────────┘     └────────┬─────────┘      │
                                  │                │
                                  ▼                │
                         ┌──────────────────┐      │
                         │ Mark as          │      │
                         │ persisted=false  │      │
                         └────────┬─────────┘      │
                                  │                │
                                  ▼                │
                         ┌──────────────────┐      │
                         │ User saves       │      │
                         │ (persist)        │      │
                         └────────┬─────────┘      │
                                  │                │
                                  ▼                │
                         ┌──────────────────┐      │
                         │ Write to         │──────┘
                         │ config.toml      │
                         └──────────────────┘
```

## Key Concept: Rules vs Orchestrator Parameters

**Rules** answer one question: "Can this bead be scheduled?"

- Ordered list of filters, evaluated top-to-bottom
- Each rule returns DENY (final) or ALLOW (continue evaluating)
- If no rule denies, the bead is allowed

**Orchestrator parameters** control how the orchestrator runs:

- `max_concurrent` - number of parallel agents
- `timeout` - agent timeout
- `retry_policy` - failure handling

These are **separate concerns**. Rules don't set concurrency limits; they filter tasks.

## Data Model Changes

### Before: Three Rule Categories (Confused)

```go
// pkg/rules/engine.go (current) - PROBLEMS:
// 1. Mixes task filters with orchestrator params (max_concurrent)
// 2. Three confusing categories
// 3. "boost" and "limit" actions blur the line
type Engine struct {
    config  *RulesSettings  // "Config Settings" in UI
    runtime []CustomRule    // "Runtime Rules" in UI
    mu      sync.RWMutex
}

type RulesSettings struct {
    PriorityMin    int      // Filter
    PriorityMax    int      // Filter
    ExcludeTypes   []string // Filter
    ExcludeLabels  []string // Filter
    MaxConcurrent  int      // NOT A RULE - orchestrator param!
    Custom         []CustomRule
}
```

### After: Clean Separation

```go
// pkg/rules/engine.go (new)
type Engine struct {
    rules []Rule        // Ordered list of DENY/ALLOW filters
    mu    sync.RWMutex
}

// Rule answers: "Should this bead be scheduled?"
// Evaluated in order. DENY is final, ALLOW continues.
type Rule struct {
    Name       string      `json:"name"`
    Conditions []Condition `json:"conditions"`
    Action     Action      `json:"action"`      // DENY or ALLOW only
    Enabled    bool        `json:"enabled"`
    Persisted  bool        `json:"persisted"`   // Does current state match config?
}

type Action string
const (
    ActionDeny  Action = "deny"   // Stop evaluation, reject bead
    ActionAllow Action = "allow"  // Continue evaluation (or accept if last)
)

// Persisted() returns true if the entire rules list matches config.
// This is computed, not stored - compare current state to loaded snapshot.
func (e *Engine) Persisted() bool {
    // Compare: same rules, same order, all individual rules persisted
    // Returns false if any rule edited, created, deleted, or reordered
}

// Orchestrator params live separately in config
type OrchestratorSettings struct {
    MaxConcurrent int           `toml:"max_concurrent"`
    AgentTimeout  time.Duration `toml:"agent_timeout"`
    // ... other runtime params
}
```

### Dashboard State Changes

```typescript
// Before: Confused mix of rules and settings
interface RulesState {
  configRules: ConfigRulesSettings | null;  // Included max_concurrent!
  customRules: RuntimeRule[];
  runtimeRules: RuntimeRule[];
}

// After: Rules are just filters
interface RulesState {
  rules: Rule[];                   // Ordered DENY/ALLOW filters
  persisted: boolean;              // Does entire list match config? (includes deletions/reorders)
}

interface Rule {
  name: string;
  conditions: Condition[];
  action: 'deny' | 'allow';        // Only two actions
  enabled: boolean;
  persisted: boolean;              // Does current state match config?
}

// Orchestrator params are separate (different UI section)
interface OrchestratorSettings {
  max_concurrent: number;
  agent_timeout: number;
}
```

## API Changes

### GET /api/rules (Updated Response)

```json
{
  "rules": [
    {
      "name": "deny-low-priority",
      "conditions": [{"field": "priority", "op": ">=", "value": 3}],
      "action": "deny",
      "enabled": true,
      "persisted": true
    },
    {
      "name": "deny-chores",
      "conditions": [{"field": "type", "op": "==", "value": "chore"}],
      "action": "deny",
      "enabled": true,
      "persisted": false
    }
  ],
  "persisted": false
}
```

**Two levels of `persisted`:**
- **Per-rule**: Does this rule's current state match what's in config?
- **List-level**: Does the entire list match config? (false if any rule edited, created, deleted, or reordered)

Note: No "settings" block. Orchestrator params (`max_concurrent`, etc.) are
fetched via a separate endpoint or passed at run start.

### POST /api/rules (Create Rule)

```json
{
  "name": "deny-wontfix",
  "conditions": [{"field": "labels", "op": "contains", "value": "wontfix"}],
  "action": "deny",
  "enabled": true
}
```

Response includes `persisted: false` (not yet saved to config).

### PUT /api/rules/:name (Update Rule)

Full replacement. Sets `persisted: false` on any change.

```json
{
  "conditions": [{"field": "priority", "op": ">=", "value": 4}],
  "action": "deny",
  "enabled": true
}
```

### POST /api/rules/:name/reorder (Reorder Rule - NEW)

Rules are ordered. This moves a rule to a new position.

```json
{
  "position": 0
}
```

### POST /api/rules/save (Save All - NEW)

Persists all rules with `persisted: false` to config file.

```json
// Response
{
  "saved_count": 2,
  "rules": ["deny-chores", "deny-wontfix"]
}
```

### DELETE /api/rules/:name (Updated)

Removes rule from in-memory list immediately. Sets list-level `persisted: false`.

To persist the deletion, user must click "Save" which writes current list to config.
If the run restarts without saving, deleted rules will reappear (config is source of truth).

**Trapdoor case**: Rule loaded from config → edited (`persisted: false`) → deleted.
At save time, we write the current in-memory list. The deleted rule is simply not in
the list, so it's removed from config. The intermediate edit state is irrelevant.

## UI Design

### Rules Panel Layout (Updated)

```
┌─────────────────────────────────────────────────────────────┐
│ Rules                              [Save] [+ Add Rule]      │
│                                                             │
│ Rules are evaluated in order. DENY stops evaluation.        │
│ ALLOW continues to the next rule (or accepts if last).      │
│                                                             │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ ≡ 1. deny-low-priority               [✓] [Edit] [Del]   │ │
│ │     IF priority >= 3 THEN DENY                          │ │
│ └─────────────────────────────────────────────────────────┘ │
│                                                             │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ ≡ 2. deny-chores              ● [✓] [Edit] [Del]        │ │
│ │     IF type == "chore" THEN DENY                        │ │
│ └─────────────────────────────────────────────────────────┘ │
│                                                             │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ ≡ 3. allow-urgent             ● [✓] [Edit] [Del]        │ │
│ │     IF labels contains "urgent" THEN ALLOW              │ │
│ └─────────────────────────────────────────────────────────┘ │
│                                                             │
│ ● = unsaved (persisted=false)                               │
│ ≡ = drag handle for reordering                              │
└─────────────────────────────────────────────────────────────┘
```

Key changes:
- No "Settings" card (that's orchestrator params, separate UI)
- Rules show order numbers (evaluation order matters)
- Drag handles for reordering
- Simple DENY/ALLOW actions only

### Edit Rule Modal

```
┌─────────────────────────────────────────────────────────────┐
│ Edit Rule                                               [X] │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│ Name: [deny-low-priority__________]                         │
│                                                             │
│ ┌─ Conditions (all must match) ───────────────────────────┐ │
│ │                                                         │ │
│ │  Field: [priority ▼]  Op: [>=  ▼]  Value: [3____]      │ │
│ │                                         [+ Add] [- Del] │ │
│ │                                                         │ │
│ └─────────────────────────────────────────────────────────┘ │
│                                                             │
│ Action: ( ) ALLOW - continue evaluating (or accept if last) │
│         (•) DENY  - reject bead, stop evaluating            │
│                                                             │
│ [✓] Enabled                                                 │
│                                                             │
│ ┌─ Syntax Reference ─────────────────────────────── [▼] ─┐  │
│ │                                                        │  │
│ │ FIELDS                                                 │  │
│ │   priority    Numeric (0=critical, 4=backlog)          │  │
│ │   type        bug, feature, task, chore, epic          │  │
│ │   assignee    Username or empty string                 │  │
│ │   labels      Array of label strings                   │  │
│ │                                                        │  │
│ │ OPERATORS                                              │  │
│ │   ==, !=      Exact match                              │  │
│ │   <, <=       Less than (numeric)                      │  │
│ │   >, >=       Greater than (numeric)                   │  │
│ │   contains    Array contains value                     │  │
│ │   not_contains Array does not contain                  │  │
│ │                                                        │  │
│ │ EXAMPLES                                               │  │
│ │   priority >= 3              Skip P3+ (low priority)   │  │
│ │   type == "chore"            Skip all chores           │  │
│ │   labels contains "blocked"  Skip blocked items        │  │
│ │   assignee == ""             Skip unassigned           │  │
│ │                                                        │  │
│ └────────────────────────────────────────────────────────┘  │
│                                                             │
├─────────────────────────────────────────────────────────────┤
│                                    [Cancel]  [Save]         │
└─────────────────────────────────────────────────────────────┘
```

Key changes:
- Single action selector (DENY or ALLOW radio buttons)
- No "boost" or "limit" - those are orchestrator concerns
- Collapsible syntax reference at bottom
- Single "Save" button (always marks as unsaved until explicitly persisted)

## Migration Strategy

### Phase 1: Backend Refactor

1. Move `rules.Engine` from `OrchestratorManager` to `Orchestrator`
2. Load config in `Orchestrator.New()` or `Start()`
3. Update scheduler to use per-orchestrator engine
4. Add isolation tests

### Phase 2: Unified Rule Model

1. Add `Persisted` field to `Rule` struct
2. Collapse three categories into single `rules` slice
3. Update API responses to new format (per-rule and list-level `persisted`)
4. Track state: any edit/create/delete/reorder sets `persisted: false`
5. Add `POST /api/rules/save` endpoint

### Phase 3: UI Improvements

1. Create `EditRuleModal` component with full form
2. Create `RuleSyntaxHelp` collapsible component
3. Update `RulesPanel` to use modal for editing
4. Add unsaved indicator (yellow dot)
5. Add "Save All" button when changes exist

### Backward Compatibility

- Old config format auto-migrates (existing rules get `persisted: true`)
- API version bump not required (additive changes only)
- Dashboard gracefully handles old/new response formats during rollout

## File Changes

### Backend (Go)

| File | Changes |
|------|---------|
| `pkg/rules/engine.go` | Add `Persisted` field; unified rule slice; list-level persisted |
| `pkg/daemon/orchestrator.go` | Own rules engine; load config on start |
| `pkg/daemon/orchestrator_manager.go` | Remove global rules engine |
| `pkg/daemon/rules_handlers.go` | Update API responses; add save endpoint |
| `pkg/config/config.go` | No changes (format compatible) |

### Frontend (TypeScript)

| File | Changes |
|------|---------|
| `src/stores/stateStore.ts` | Unified rules state; list-level `persisted` |
| `src/api/client.ts` | Update types; add saveRules() |
| `src/components/rules/RulesPanel.tsx` | Use modal editing; show unsaved indicator |
| `src/components/rules/EditRuleModal.tsx` | NEW: Full editing form |
| `src/components/rules/RuleSyntaxHelp.tsx` | NEW: Collapsible help |
| `src/hooks/useWebSocket.ts` | Handle updated event format |

## Acceptance Criteria

### Phase 1: Per-Repo Rules Engine
- [ ] Each repo's orchestrator has its own rules engine
- [ ] Config is loaded from disk when run starts
- [ ] Rules from repo A don't affect repo B
- [ ] Tests verify isolation

### Phase 2: Unified Rule Model
- [ ] Single `rules` array in API response (no settings block)
- [ ] Per-rule `persisted` field tracks individual rule state
- [ ] List-level `persisted` field tracks overall state (deletions, reorders)
- [ ] Actions are only DENY or ALLOW (no boost/limit)
- [ ] DENY stops evaluation, ALLOW continues
- [ ] Any edit/create/delete/reorder sets `persisted: false`
- [ ] Save endpoint writes current in-memory list to config, sets `persisted: true`
- [ ] Reorder endpoint changes rule position

### Phase 3: UI Improvements
- [ ] Edit button opens modal with full form
- [ ] Modal has DENY/ALLOW radio buttons (not dropdown)
- [ ] Modal includes collapsible syntax reference
- [ ] Rules show order numbers
- [ ] Drag handles allow reordering
- [ ] Unsaved rules show indicator (yellow dot)
- [ ] "Save" button persists all changes to config
- [ ] Tests cover modal and drag-drop interactions
