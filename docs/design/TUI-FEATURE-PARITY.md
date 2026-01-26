# TUI Feature Parity Design

## Problem Statement

The TUI excels at **monitoring** but lacks the **interactive control surfaces** of the web UI:

1. **No task management** - Cannot view, filter, or select beads; no dependency visualization
2. **No run management** - Cannot start runs, select between runs, or configure run parameters
3. **No repository switching** - TUI is single-repo; web supports multi-repo workflows
4. **No rules management** - Cannot view or edit task filtering rules
5. **Limited agent details** - Missing collapsible detail sections, validation steps, parent/child navigation
6. **No filtering/search** - Cannot filter agents by status or search by ID
7. **Fixed layout** - No resizable panes or customizable layout

## Goals

- Bring TUI to feature parity with web dashboard
- Use vim-style keybindings consistently throughout
- No mouse-dependent features (no drag-and-drop)
- Maintain terminal performance with large agent counts

## Non-Goals

- Pixel-perfect visual parity with web UI
- Mouse-based interactions (drag-and-drop, click-to-select)
- Inline editing (use modals/forms instead)

## Architecture Overview

### Current Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Dashboard (Bubble Tea)                    │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  RuntimeState (in-process or via RemoteClient)       │   │
│  │  - Agents map                                        │   │
│  │  - Stats                                             │   │
│  │  - (No tasks, runs, repos, rules)                    │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                             │
│  ┌─────────────────┐  ┌──────────────────────────────────┐  │
│  │   Agent List    │  │        Terminal Output           │  │
│  │   (left pane)   │  │        (right pane)              │  │
│  │   - j/k nav     │  │        - Viewport scroll         │  │
│  │   - g/G jump    │  │        - Live feed events        │  │
│  └─────────────────┘  └──────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### Target Architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│                         Dashboard (Bubble Tea)                            │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │  RuntimeState (extended)                                           │  │
│  │  - Agents, Stats (existing)                                        │  │
│  │  - Tasks map (NEW: from beads)                                     │  │
│  │  - Runs list + ActiveRunID (NEW)                                   │  │
│  │  - Repos list + ActiveRepoID (NEW)                                 │  │
│  │  - Rules list (NEW)                                                │  │
│  └────────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│  ┌─ Header ─────────────────────────────────────────────────────────┐    │
│  │ Canopy │ Repo: [selector] │ Run: [selector] │ [Pause] │ Stats   │    │
│  └──────────────────────────────────────────────────────────────────┘    │
│                                                                          │
│  ┌─────────────┐ ┌─────────────────────┐ ┌──────────────────────────┐   │
│  │ Beads Pane  │ │    Agents Pane      │ │     Detail Pane          │   │
│  │ (leftmost)  │ │    (center)         │ │     (rightmost)          │   │
│  │             │ │                     │ │                          │   │
│  │ - Task tree │ │ - Agent list        │ │ - Agent detail sections  │   │
│  │ - Filters   │ │ - Status filters    │ │ - Terminal output        │   │
│  │ - Deps      │ │ - Search            │ │ - Commits                │   │
│  └─────────────┘ └─────────────────────┘ └──────────────────────────┘   │
│                                                                          │
│  ┌─ Footer ─────────────────────────────────────────────────────────┐    │
│  │ [Mode indicator] │ Key hints │ Help: ?                           │    │
│  └──────────────────────────────────────────────────────────────────┘    │
│                                                                          │
│  ┌─ Modal Layer (when active) ──────────────────────────────────────┐    │
│  │  - Run Config Dialog                                             │    │
│  │  - Rule Editor                                                   │    │
│  │  - Repo Selector                                                 │    │
│  │  - Help Screen                                                   │    │
│  └──────────────────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────────────────┘
```

## Keyboard Navigation Design

### Core Principles

1. **Vim-style everywhere** - j/k/h/l for navigation, J/K for reordering
2. **Pane switching via ctrl-w** - Matches vim window commands
3. **Modal escape via Esc** - Consistent modal dismissal
4. **Mode indicators** - Show current context in footer

### Global Keybindings

| Key | Action |
|-----|--------|
| `ctrl-w h` | Focus pane to the left |
| `ctrl-w j` | Focus pane below |
| `ctrl-w k` | Focus pane above |
| `ctrl-w l` | Focus pane to the right |
| `ctrl-w H` | Grow pane width |
| `ctrl-w L` | Shrink pane width |
| `1-4` | Quick-focus pane by number |
| `?` | Toggle help modal |
| `q` | Quit (or close modal if open) |
| `Esc` | Close modal / cancel operation |
| `:` | Command mode (future) |

### Pane-Local Keybindings

#### Beads Pane (Task Tree)

| Key | Action |
|-----|--------|
| `j` | Move down in tree |
| `k` | Move up in tree |
| `h` | Collapse node / go to parent |
| `l` | Expand node / enter children |
| `g` | Go to first item |
| `G` | Go to last item |
| `Enter` | Select bead (filter agents) |
| `x` | Clear bead selection |
| `f` | Open filter menu |
| `/` | Search beads |
| `n` | Next search result |
| `N` | Previous search result |
| `o` | Toggle open/closed view |
| `d` | Show dependencies |

#### Agents Pane

| Key | Action |
|-----|--------|
| `j` | Move down |
| `k` | Move up |
| `g` | Go to first agent |
| `G` | Go to last agent |
| `Enter` | View agent detail |
| `f` | Cycle status filter (all → running → completed → failed) |
| `/` | Search agents |
| `n` | Next search result |
| `N` | Previous search result |
| `[` | Go to parent agent |
| `]` | Go to child agent |

#### Detail Pane

| Key | Action |
|-----|--------|
| `j` | Scroll down |
| `k` | Scroll up |
| `ctrl-d` | Half-page down |
| `ctrl-u` | Half-page up |
| `ctrl-f` | Full page down |
| `ctrl-b` | Full page up |
| `g` | Go to top |
| `G` | Go to bottom |
| `Tab` | Cycle through sections |
| `1-9` | Jump to section by number |
| `c` | Toggle commits section |
| `v` | Toggle validation section |
| `o` | Toggle output section |

#### Rules Pane (when visible)

| Key | Action |
|-----|--------|
| `j` | Move down |
| `k` | Move up |
| `J` | Move rule down (reorder) |
| `K` | Move rule up (reorder) |
| `a` | Add new rule |
| `e` | Edit selected rule |
| `d` | Delete selected rule |
| `Space` | Toggle rule enabled |
| `s` | Save changes to config |
| `u` | Undo changes (reload) |

### Modal Keybindings

#### Form Navigation (Run Config, Rule Editor)

| Key | Action |
|-----|--------|
| `Tab` | Next field |
| `Shift-Tab` | Previous field |
| `j` / `k` | Next/prev field (when not in text input) |
| `Enter` | Activate button / select option |
| `Esc` | Cancel and close |

Modals have explicit `[Cancel]` and `[Save]` buttons. Navigate to them with Tab/j/k and press Enter.

#### List Selection (Repo/Run Selector)

| Key | Action |
|-----|--------|
| `j` | Move down |
| `k` | Move up |
| `/` | Filter list |
| `Enter` | Select item |
| `Esc` | Cancel |

## Component Design

### PaneManager

Central component managing pane focus and layout:

```go
type PaneManager struct {
    panes       []Pane           // Ordered list of panes
    focusIndex  int              // Currently focused pane
    layout      LayoutMode       // horizontal, vertical, or grid
    sizes       []float64        // Relative sizes (0.0-1.0)
}

type Pane interface {
    ID() string
    Update(msg tea.Msg) (Pane, tea.Cmd)
    View() string
    Focused() bool
    SetFocused(bool)
    KeyBindings() []KeyBinding   // For help display
}

type LayoutMode int
const (
    LayoutHorizontal LayoutMode = iota  // Panes side by side
    LayoutVertical                       // Panes stacked
    LayoutGrid                           // 2x2 or custom grid
)
```

### BeadsPane

Task tree with filtering and dependency visualization:

```go
type BeadsPane struct {
    tasks       []*Task          // Flat list of all tasks
    tree        *TaskTree        // Hierarchical structure
    expanded    map[string]bool  // Expanded state per task
    cursor      int              // Current position
    selected    string           // Selected task ID (for agent filtering)
    filter      TaskFilter       // Current filter settings
    search      string           // Search query
    focused     bool
}

type TaskTree struct {
    Root     []*TaskNode
}

type TaskNode struct {
    Task     *Task
    Children []*TaskNode
    Depth    int
}

type TaskFilter struct {
    Status     []string   // open, in_progress, closed
    Priority   *int       // max priority (0-4)
    Type       []string   // bug, feature, task, chore
    Labels     []string   // required labels
    ShowClosed bool
}
```

### AgentsPane

Agent list with filtering and search:

```go
type AgentsPane struct {
    agents       []*AgentState
    cursor       int
    filter       AgentFilter
    search       string
    beadFilter   string           // Filter by selected bead
    focused      bool
}

type AgentFilter struct {
    Status     AgentStatusFilter  // all, running, completed, failed
}

type AgentStatusFilter int
const (
    StatusAll AgentStatusFilter = iota
    StatusRunning
    StatusCompleted
    StatusFailed
)
```

### DetailPane

Collapsible sections for agent details:

```go
type DetailPane struct {
    agent        *AgentState
    sections     []Section
    activeSection int
    viewport     viewport.Model
    focused      bool
}

type Section struct {
    ID        string
    Title     string
    Collapsed bool
    Render    func(agent *AgentState, width int) string
}

// Default sections:
// 1. Summary (ID, task, status, duration)
// 2. Tokens & Cost
// 3. Validation Steps
// 4. Git Commits
// 5. Related Agents (parent/children)
// 6. Terminal Output
// 7. Errors (if any)
```

### RulesPane

Rule list with inline reordering:

```go
type RulesPane struct {
    rules       []Rule
    cursor      int
    modified    bool      // Unsaved changes exist
    focused     bool
}

// Reordering via J/K (shift + j/k)
func (p *RulesPane) MoveUp() {
    if p.cursor > 0 {
        p.rules[p.cursor], p.rules[p.cursor-1] =
            p.rules[p.cursor-1], p.rules[p.cursor]
        p.cursor--
        p.modified = true
    }
}
```

### Modal System

Overlay modals for forms and selection:

```go
type ModalManager struct {
    stack    []Modal
}

type Modal interface {
    Update(msg tea.Msg) (Modal, tea.Cmd, bool)  // bool = closed
    View() string
    Title() string
}

// Modal types:
// - RunConfigModal: Start new run with settings
// - RuleEditorModal: Add/edit rule
// - RepoSelectorModal: Switch repository
// - RunSelectorModal: Switch run
// - HelpModal: Keybinding reference
// - ConfirmModal: Confirm destructive actions
```

## Data Model Extensions

### RuntimeState Additions

```go
type RuntimeState struct {
    // Existing
    Agents map[string]*AgentState
    Stats  Stats

    // NEW: Task management
    Tasks map[string]*TaskState

    // NEW: Run management
    Runs        []Run
    ActiveRunID string

    // NEW: Repository management
    Repos        []Repository
    ActiveRepoID string

    // NEW: Rules (cached from daemon)
    Rules     []Rule
    Persisted bool  // Rules match config file
}

type TaskState struct {
    ID          string
    Title       string
    Description string
    Type        string      // bug, feature, task, chore
    Priority    int         // 0-4
    Status      string      // open, in_progress, closed
    Labels      []string
    Assignee    string
    Blockers    []string    // Dependency IDs
    BlockedBy   []string    // Reverse dependencies
    UpdatedAt   time.Time
}
```

### WebSocket Event Extensions

TUI already connects via WebSocket for remote mode. Extend events:

```go
// New event types for TUI
const (
    EventTasksUpdated = "tasks_updated"
    EventRunsUpdated  = "runs_updated"
    EventReposUpdated = "repos_updated"
    EventRulesUpdated = "rules_updated"
)

type TasksUpdatedPayload struct {
    Tasks map[string]*TaskState `json:"tasks"`
}

type RunsUpdatedPayload struct {
    Runs        []Run  `json:"runs"`
    ActiveRunID string `json:"active_run_id"`
}
```

## UI Layout

### Default Layout (Three Panes)

```
┌─────────────────────────────────────────────────────────────────────────┐
│ 🌲 Canopy │ repo: canopy │ run: run-1234 (▶ active) │ ⏸ Pause │ Stats  │
├───────────────────┬─────────────────────────┬───────────────────────────┤
│ Beads         [1] │ Agents              [2] │ Detail                [3] │
│ ─────────────────│ ───────────────────────│ ────────────────────────── │
│ ▼ open (12)      │ Filter: [all ▼] [____] │ Agent: a1b2c3d4            │
│   ● canopy-abc   │                         │ Task: Implement feature X │
│   ○ canopy-def   │ ● a1b2c3d4 Implement.. │ Status: ▶ running (2m34s) │
│   ○ canopy-ghi   │   b2c3d4e5 Fix bug...  │                            │
│ ▶ in_progress(3) │   c3d4e5f6 Add tests.. │ ┌─ Tokens ────────────────┐│
│ ▶ closed (45)    │ ✓ d4e5f6g7 Refactor..  │ │ in: 12.4k  out: 3.2k    ││
│                   │ ✗ e5f6g7h8 Deploy...   │ │ cache: 8.1k  $0.42      ││
│ ─────────────────│                         │ └─────────────────────────┘│
│ Priority: ≤2     │                         │                            │
│ Type: all        │                         │ ┌─ Validation ────────────┐│
│ Labels: none     │                         │ │ ✓ build (2.1s)          ││
│                   │                         │ │ ✓ test (14.3s)          ││
│                   │                         │ │ ○ lint (pending)        ││
│                   │                         │ └─────────────────────────┘│
│                   │                         │                            │
│                   │                         │ ┌─ Output ────────────────┐│
│                   │                         │ │ Reading file...         ││
│                   │                         │ │ Analyzing dependencies..││
│                   │                         │ │ ...                     ││
│                   │                         │ └─────────────────────────┘│
├───────────────────┴─────────────────────────┴───────────────────────────┤
│ [NORMAL] ctrl-w: switch pane │ j/k: navigate │ ?: help │ q: quit        │
└─────────────────────────────────────────────────────────────────────────┘
```

### Two-Pane Layout (Agents + Detail)

```
┌─────────────────────────────────────────────────────────────────────────┐
│ 🌲 Canopy │ repo: canopy │ run: run-1234 (▶ active) │ ⏸ Pause │ Stats  │
├───────────────────────────────────┬─────────────────────────────────────┤
│ Agents                        [1] │ Detail                          [2] │
│ ─────────────────────────────────│ ─────────────────────────────────── │
│ Filter: [running ▼] [search___]  │ Agent: a1b2c3d4                     │
│                                   │ Task: Implement feature X           │
│ ● a1b2c3d4 Implement feature X   │ Status: ▶ running (2m34s)           │
│   b2c3d4e5 Fix authentication    │ Parent: z9y8x7w6                     │
│   c3d4e5f6 Add unit tests        │                                      │
│                                   │ ┌─ Tokens ──────────────────────────┐│
│                                   │ │ Input: 12,423    Output: 3,201    ││
│                                   │ │ Cache read: 8,102  Cost: $0.42    ││
│                                   │ └────────────────────────────────────┘│
│                                   │                                      │
│                                   │ ┌─ Git Commits (2) ─────────────────┐│
│                                   │ │ a1b2c3d Add user auth middleware  ││
│                                   │ │ b2c3d4e Fix token validation      ││
│                                   │ └────────────────────────────────────┘│
│                                   │                                      │
│                                   │ ┌─ Terminal Output ─────────── 45% ─┐│
│                                   │ │ [tool:Read] src/auth/handler.go   ││
│                                   │ │ [tool:Edit] Adding middleware...  ││
│                                   │ │ [tool:Bash] go test ./...         ││
│                                   │ │ PASS: TestAuthMiddleware          ││
│                                   │ └────────────────────────────────────┘│
├───────────────────────────────────┴─────────────────────────────────────┤
│ [NORMAL] ctrl-w: switch pane │ j/k: navigate │ Tab: sections │ q: quit  │
└─────────────────────────────────────────────────────────────────────────┘
```

### Run Config Modal

```
┌─────────────────────────────────────────────────────────────────────────┐
│                                                                          │
│   ┌─ Start Run ──────────────────────────────────────────────────────┐  │
│   │                                                                   │  │
│   │  Repository: /home/user/repos/canopy                             │  │
│   │                                                                   │  │
│   │  Concurrency: [4______]                                          │  │
│   │                                                                   │  │
│   │  Priority Filter                                                 │  │
│   │    Max priority: [2] (0=critical, 4=backlog)                     │  │
│   │                                                                   │  │
│   │  Type Filter                                                     │  │
│   │    [x] bug  [x] feature  [x] task  [ ] chore                     │  │
│   │                                                                   │  │
│   │  Retry Policy                                                    │  │
│   │    Max retries: [3______]                                        │  │
│   │                                                                   │  │
│   │  Validation                                                      │  │
│   │    (•) Strict - revert on failure                                │  │
│   │    ( ) Lenient - file issue, keep merge                          │  │
│   │    ( ) Skip - no validation                                      │  │
│   │                                                                   │  │
│   │                                        [Cancel]  [Start Run]     │  │
│   └───────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│   Tab: next field │ Shift-Tab: prev │ Enter: select │ Esc: cancel       │
└─────────────────────────────────────────────────────────────────────────┘
```

### Rule Editor Modal

```
┌─────────────────────────────────────────────────────────────────────────┐
│                                                                          │
│   ┌─ Edit Rule ──────────────────────────────────────────────────────┐  │
│   │                                                                   │  │
│   │  Name: [deny-low-priority_______________]                        │  │
│   │                                                                   │  │
│   │  Conditions (all must match):                                    │  │
│   │  ┌────────────────────────────────────────────────────────────┐  │  │
│   │  │ Field: [priority ▼]  Op: [>= ▼]  Value: [3_____]    [ x ] │  │  │
│   │  └────────────────────────────────────────────────────────────┘  │  │
│   │                                               [+ Add Condition]  │  │
│   │                                                                   │  │
│   │  Action:                                                         │  │
│   │    (•) DENY  - reject bead, stop evaluating                      │  │
│   │    ( ) ALLOW - continue evaluating                               │  │
│   │                                                                   │  │
│   │  [x] Enabled                                                     │  │
│   │                                                                   │  │
│   │  ┌─ Syntax Reference ─────────────────────────────────── [v] ─┐  │  │
│   │  │ FIELDS: priority (0-4), type, assignee, labels             │  │  │
│   │  │ OPS: ==, !=, <, <=, >, >=, contains, not_contains          │  │  │
│   │  └────────────────────────────────────────────────────────────┘  │  │
│   │                                                                   │  │
│   │                                        [Cancel]  [Save Rule]     │  │
│   └───────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│   Tab/j/k: navigate │ Enter: select │ Esc: cancel                       │
└─────────────────────────────────────────────────────────────────────────┘
```

### Help Modal

```
┌─────────────────────────────────────────────────────────────────────────┐
│                                                                          │
│   ┌─ Keyboard Shortcuts ─────────────────────────────────────────────┐  │
│   │                                                                   │  │
│   │  GLOBAL                          PANE SWITCHING                  │  │
│   │  ──────                          ──────────────                  │  │
│   │  ?        Toggle help            ctrl-w h   Focus left pane      │  │
│   │  q        Quit                   ctrl-w j   Focus down pane      │  │
│   │  Esc      Close modal            ctrl-w k   Focus up pane        │  │
│   │  1-4      Focus pane N           ctrl-w l   Focus right pane     │  │
│   │                                                                   │  │
│   │  NAVIGATION                      ACTIONS                         │  │
│   │  ──────────                      ───────                         │  │
│   │  j/k      Move down/up           Enter      Select/open          │  │
│   │  h/l      Collapse/expand        f          Filter               │  │
│   │  g/G      First/last item        /          Search               │  │
│   │  ctrl-d/u Half page down/up      n/N        Next/prev match      │  │
│   │                                                                   │  │
│   │  RULES (when focused)            REORDERING                      │  │
│   │  ─────                           ──────────                      │  │
│   │  a        Add new rule           J          Move item down       │  │
│   │  e        Edit rule              K          Move item up         │  │
│   │  d        Delete rule            s          Save changes         │  │
│   │  Space    Toggle enabled         u          Undo changes         │  │
│   │                                                                   │  │
│   └───────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│   Press ? or Esc to close                                               │
└─────────────────────────────────────────────────────────────────────────┘
```

## Implementation Phases

### Phase 1: Foundation (P0)

Core infrastructure needed by all features.

**Tasks:**
1. Extend RuntimeState with Tasks, Runs, Repos, Rules
2. Add WebSocket event handlers for new data types
3. Create PaneManager for focus and layout management
4. Create Modal system for overlays
5. Add keyboard navigation framework (capture ctrl-w sequences)

**Files:**
- `pkg/daemon/runtime.go` - Extend RuntimeState
- `pkg/tui/pane.go` - NEW: Pane interface and PaneManager
- `pkg/tui/modal.go` - NEW: Modal interface and ModalManager
- `pkg/tui/keybindings.go` - NEW: Keyboard handling
- `pkg/tui/client.go` - Extend WebSocket handlers

### Phase 2: Task Management (P0)

Beads pane with tree view and filtering.

**Tasks:**
1. Create BeadsPane component with tree rendering
2. Implement task tree traversal (j/k/h/l)
3. Add task filtering (priority, type, status, labels)
4. Connect bead selection to agent filtering
5. Add dependency visualization

**Files:**
- `pkg/tui/beads_pane.go` - NEW: Beads tree component
- `pkg/tui/task_tree.go` - NEW: Tree data structure
- `pkg/tui/task_filter.go` - NEW: Filter logic

### Phase 3: Enhanced Agent Pane (P1)

Filtering, search, and parent/child navigation.

**Tasks:**
1. Add status filter cycling (f key)
2. Add search functionality (/ key)
3. Implement bead-based agent filtering
4. Add parent/child navigation ([ and ] keys)

**Files:**
- `pkg/tui/agents_pane.go` - NEW: Refactored from dashboard.go
- `pkg/tui/search.go` - NEW: Search component

### Phase 4: Detail Pane (P1)

Collapsible sections for comprehensive agent details.

**Tasks:**
1. Create DetailPane with section management
2. Implement section collapsing (Tab/1-9 keys)
3. Add validation steps section
4. Add related agents section (parent/children)
5. Add tokens/cost breakdown section

**Files:**
- `pkg/tui/detail_pane.go` - NEW: Refactored from terminal.go
- `pkg/tui/sections.go` - NEW: Section definitions

### Phase 5: Run Management (P1)

Start, select, and configure runs.

**Tasks:**
1. Create RunSelectorModal for switching runs
2. Create RunConfigModal for starting new runs
3. Add run indicator to header
4. Wire up run start/stop commands

**Files:**
- `pkg/tui/run_selector.go` - NEW
- `pkg/tui/run_config.go` - NEW
- `pkg/tui/header.go` - NEW: Header component

### Phase 6: Repository Management (P1)

Multi-repo support.

**Tasks:**
1. Create RepoSelectorModal
2. Add repo indicator to header
3. Handle repo switching (full state refresh)

**Files:**
- `pkg/tui/repo_selector.go` - NEW

### Phase 7: Rules Management (P2)

View and edit rules.

**Tasks:**
1. Create RulesPane with list display
2. Implement reordering (J/K keys)
3. Create RuleEditorModal
4. Add save/undo functionality
5. Show unsaved indicator

**Files:**
- `pkg/tui/rules_pane.go` - NEW
- `pkg/tui/rule_editor.go` - NEW

### Phase 8: Polish (P2)

Layout, help, and UX refinements.

**Tasks:**
1. Implement resizable panes (ctrl-w H/L to grow/shrink)
2. Create comprehensive HelpModal
3. Add footer with mode indicator and hints
4. Add confirmations for destructive actions
5. Performance optimization for large agent counts

**Files:**
- `pkg/tui/layout.go` - NEW: Layout calculations
- `pkg/tui/help.go` - NEW: Help screen
- `pkg/tui/footer.go` - NEW: Footer component

## Testing Strategy

### Unit Tests

```go
// Test keyboard navigation
func TestPaneNavigation(t *testing.T) {
    pm := NewPaneManager(beads, agents, detail)

    // ctrl-w l should move focus right
    pm, _ = pm.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
    pm, _ = pm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

    assert.Equal(t, "agents", pm.FocusedPane().ID())
}

// Test tree navigation
func TestBeadsPaneTreeNav(t *testing.T) {
    pane := NewBeadsPane(tasks)

    // l expands, h collapses
    pane, _ = pane.Update(tea.KeyMsg{Runes: []rune{'l'}})
    assert.True(t, pane.expanded[pane.CurrentTask().ID])

    pane, _ = pane.Update(tea.KeyMsg{Runes: []rune{'h'}})
    assert.False(t, pane.expanded[pane.CurrentTask().ID])
}
```

### Integration Tests

```go
// Test full TUI with mock state
func TestDashboardIntegration(t *testing.T) {
    state := daemon.NewRuntimeState()
    state.AddAgent(&daemon.AgentState{ID: "test-1"})

    dashboard := NewDashboard(state, nil)

    // Simulate terminal size
    dashboard, _ = dashboard.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

    view := dashboard.View()
    assert.Contains(t, view, "test-1")
}
```

### Snapshot Tests

Use Bubble Tea's test utilities for visual regression:

```go
func TestDetailPaneSnapshot(t *testing.T) {
    pane := NewDetailPane()
    pane.SetAgent(mockAgent)

    golden.RequireEqual(t, []byte(pane.View()))
}
```

## Migration Path

1. **Phase 1-2**: Ship as feature flag (`canopy tui --experimental`)
2. **Phase 3-4**: Make new TUI the default, old available via `--legacy`
3. **Phase 5+**: Remove legacy TUI code

## File Structure

```
pkg/tui/
├── dashboard.go         # Main model (refactor to use panes)
├── pane.go              # Pane interface and PaneManager
├── modal.go             # Modal interface and ModalManager
├── keybindings.go       # Keyboard handling utilities
│
├── beads_pane.go        # Task tree component
├── agents_pane.go       # Agent list component
├── detail_pane.go       # Agent detail component
├── rules_pane.go        # Rules list component
│
├── header.go            # Header with selectors
├── footer.go            # Footer with hints
│
├── run_selector.go      # Run selection modal
├── run_config.go        # Run config modal
├── repo_selector.go     # Repo selection modal
├── rule_editor.go       # Rule editor modal
├── help.go              # Help modal
├── confirm.go           # Confirmation modal
│
├── task_tree.go         # Tree data structure
├── task_filter.go       # Task filtering logic
├── search.go            # Search component
├── sections.go          # Detail pane sections
├── layout.go            # Layout calculations
│
├── terminal.go          # Existing (refactor into detail_pane)
├── livefeed.go          # Existing (keep)
├── client.go            # Existing (extend for new events)
│
└── testdata/
    └── snapshots/       # Golden files for visual tests
```

## Acceptance Criteria

### Phase 1: Foundation
- [ ] RuntimeState extended with Tasks, Runs, Repos, Rules
- [ ] WebSocket events for new data types work
- [ ] ctrl-w + h/j/k/l switches panes
- [ ] Modal opens and closes with Esc
- [ ] 1-4 number keys focus panes

### Phase 2: Task Management
- [ ] Beads tree displays hierarchically
- [ ] j/k navigates, h/l collapses/expands
- [ ] Selecting a bead filters agents to that bead
- [ ] Filter menu accessible via 'f'
- [ ] Dependencies shown inline

### Phase 3: Enhanced Agents
- [ ] 'f' cycles status filter
- [ ] '/' opens search, n/N navigates results
- [ ] '[' and ']' navigate parent/child agents

### Phase 4: Detail Pane
- [ ] Sections collapse independently
- [ ] Tab cycles sections, 1-9 jumps
- [ ] Validation steps show status
- [ ] Related agents section navigable

### Phase 5: Run Management
- [ ] Run selector opens, j/k navigates, Enter selects
- [ ] Run config modal has all fields
- [ ] Header shows current run
- [ ] Can start new run from TUI

### Phase 6: Repository Management
- [ ] Repo selector works
- [ ] Switching repos refreshes all state
- [ ] Header shows current repo

### Phase 7: Rules Management
- [ ] Rules list displays
- [ ] J/K reorders rules
- [ ] 'a' adds, 'e' edits, 'd' deletes
- [ ] 's' saves, 'u' undoes
- [ ] Unsaved indicator shows

### Phase 8: Polish
- [ ] ctrl-w H/L resizes panes
- [ ] '?' shows help modal
- [ ] Footer shows mode and hints
- [ ] Performance acceptable with 100+ agents
