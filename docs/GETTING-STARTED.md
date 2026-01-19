# Getting Started with Canopy

This guide walks you through setting up and using Canopy to parallelize your Claude Code workflows.

## Prerequisites

Before starting, ensure you have:

1. **Linux or macOS** - Canopy uses filesystem overlays (OverlayFS on Linux, APFS clones on macOS). On macOS, your working directory must be on an APFS volume (the default for modern Macs).
2. **Go 1.24+** - For building from source
3. **Claude Code CLI** - Installed and authenticated
4. **beads** - Task tracking system

### Install Claude Code

```bash
# Install Claude Code CLI
curl -fsSL https://claude.ai/install | bash

# Authenticate
claude auth
```

### Install beads

```bash
# Clone and build beads
git clone https://github.com/steveyegge/beads
cd beads
go build -o bd ./cmd/bd
sudo mv bd /usr/local/bin/

# Verify
bd version
```

## Installation

### Build from Source

```bash
# Clone repository
git clone https://github.com/jzila/canopy
cd canopy

# Build
go build -o canopy ./cmd/canopy

# Add to PATH
export PATH="$PATH:$(pwd)"

# Verify
canopy version
```

### Optional: Install bubblewrap

For full sandbox isolation:

```bash
# Debian/Ubuntu
sudo apt install bubblewrap

# Fedora
sudo dnf install bubblewrap

# Arch
sudo pacman -S bubblewrap

# Verify
bwrap --version
```

## Tutorial: Your First Parallel Run

Let's walk through a complete example of using Canopy.

### Step 1: Initialize Your Project

```bash
# Create a test project
mkdir myproject && cd myproject
git init

# Initialize beads with a separate sync branch
# IMPORTANT: beads MUST use a separate branch, not main
# Overlays will corrupt beads state if it's on the main branch
bd init --branch beads-sync

# Create a simple file
echo "# My Project" > README.md
git add . && git commit -m "Initial commit"
```

### Step 2: Create Parallel Tasks

Create some tasks that can run in parallel:

```bash
# Create independent tasks
bd create --title "Add contributing guidelines" --type task --priority 2
bd create --title "Add license file" --type task --priority 2
bd create --title "Add .gitignore" --type task --priority 2

# View tasks
bd list
```

Output:
```
ID           TYPE   PRI  STATUS  TITLE
canopy-a1b2  task   P2   open    Add contributing guidelines
canopy-c3d4  task   P2   open    Add license file
canopy-e5f6  task   P2   open    Add .gitignore
```

### Step 3: Check Ready Tasks

```bash
bd ready
```

All three tasks should be ready (no blockers).

### Step 4: Run Canopy

```bash
# Dry run first to see what would happen
canopy run --dry-run

# Execute with 3 concurrent agents
canopy run -c 3 -v
```

You'll see output like:
```
Starting orchestrator with 3 concurrent agents
Fetching ready tasks from beads...
Found 3 ready tasks

Starting agent-canopy-a1b2: Add contributing guidelines
Starting agent-canopy-c3d4: Add license file
Starting agent-canopy-e5f6: Add .gitignore

agent-canopy-e5f6 completed (12.3s, $0.05)
agent-canopy-c3d4 completed (15.1s, $0.06)
agent-canopy-a1b2 completed (18.7s, $0.08)

Merging changes...
All agents merged successfully

Run completed:
  Tasks: 3 completed, 0 failed
  Duration: 18.7s
  Cost: $0.19
```

### Step 5: Review Changes

```bash
# See what was created
git status
git log --oneline -5

# View the files
ls -la
```

## Working with Dependencies

Tasks often have dependencies. Canopy respects these automatically.

### Create Dependent Tasks

```bash
# Create a design task (must complete first)
bd create --title "Design API schema" --type task --priority 0

# Create implementation tasks that depend on design
bd create --title "Implement user endpoints" --type task --priority 1
bd create --title "Implement auth endpoints" --type task --priority 1

# Add dependencies (implementation depends on design)
bd dep add canopy-xxxx canopy-yyyy  # user endpoints depend on design
bd dep add canopy-zzzz canopy-yyyy  # auth endpoints depend on design
```

### Run with Dependencies

```bash
canopy run -c 4 -v
```

Execution order:
1. **Batch 1**: Design API schema (only ready task)
2. **Batch 2**: Implement user endpoints + Implement auth endpoints (parallel, after design completes)

## Monitoring with the Dashboard

### Start the TUI Dashboard

```bash
# Start daemon with TUI
canopy daemon --tui
```

The dashboard shows:
- Active agents and their status
- Output streaming in real-time
- Merge queue status
- Cost and token usage

### Run in Another Terminal

```bash
# In a separate terminal
canopy run -c 4
```

Watch the dashboard update in real-time.

### Alternative: Background Daemon

```bash
# Start daemon in background
canopy daemon start

# Check status
canopy daemon status

# View logs
canopy daemon logs -f

# Stop daemon
canopy daemon stop
```

## Using Sandbox Isolation

For enhanced security, use the `--sandbox` flag:

### Initialize Sandbox Config

```bash
canopy init
```

This wizard:
1. Detects your project type (Node, Rust, Go, Python, etc.)
2. Discovers required tools
3. Detects validation commands (build, test)
4. Generates `.canopy/sandbox.toml` and `.canopy/validation.toml`

### Run with Sandbox

```bash
canopy run --sandbox -c 4
```

With `--sandbox`:
- Full namespace isolation (user, PID, IPC, UTS)
- All capabilities dropped
- Resource limits enforced
- Sensitive directories blocked (~/.ssh, ~/.aws, etc.)

## Post-Merge Validation

Canopy can automatically validate merged changes by running build, test, and lint commands. If validation fails, a repair agent attempts to fix the issues automatically.

### Enable Validation

During `canopy init`, you'll be asked about validation. You can also create `.canopy/validation.toml` manually:

```toml
[validation]
enabled = true
strict = false          # Keep merge on failure, file issue
timeout = "5m"
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
```

### How Validation Works

After each successful merge:

1. **Validation runs** - Each step executes sequentially
2. **On failure** - A repair agent spawns to fix the issue
3. **Repair attempts** - Agent sees error output and tries to fix it
4. **Retry validation** - If repair succeeds, validation re-runs
5. **Exhaustion** - After max attempts, a high-priority bead is filed

### Repair Agent Flow

When validation fails, the repair agent:
- Sees the failed step's output and exit code
- Has access to the merged diff
- Knows what previous repair attempts tried
- Commits fixes directly to the repository

Example repair cycle:
```
Merge applied -> Validation runs "npm test"
  -> Test fails: "TypeError: undefined is not a function"
  -> Repair agent spawned (attempt 1/3)
  -> Agent fixes the bug, commits
  -> Validation re-runs "npm test"
  -> Tests pass -> Task marked complete
```

### Validation Modes

| Mode | Behavior on Failure |
|------|---------------------|
| **Lenient** (default) | Merge kept, issue filed for manual fix |
| **Strict** | Merge reverted after all repair attempts fail |

Use strict mode for critical projects where broken code must never reach main:
```toml
[validation]
strict = true
```

### Viewing Validation Status

The dashboard shows validation status in real-time:
- `running` - Validation executing
- `passed` - All steps succeeded
- `failed` - Step failed, repair may be in progress
- `repairing` - Repair agent attempting fix

When repair is exhausted, a bead titled "Repair exhausted: {task}" is created with full context about what failed and what was tried.

See [Validation Guide](VALIDATION.md) for comprehensive documentation.

## Agentic Initialization

When using Canopy with AI agents (like Claude Code), use the agentic init flow for structured configuration.

### How Agentic Init Works

The agentic init flow provides a JSON-based interface for AI agents to:

1. **Detect** the project environment and available validation commands
2. **Present** questions to the user via the agent's UI (e.g., `AskUserQuestion`)
3. **Apply** the user's answers to create configuration files

### Step 1: Detection

```bash
canopy init --detect
```

Returns JSON with project analysis:
```json
{
  "project": {"type": "Go", "root": "/path/to/project", "markers": ["go.mod"]},
  "sandbox": {"tools": ["~/.goenv"], "configs": ["~/.gitconfig"], "caches": ["~/.cache/go-build"]},
  "validation": {
    "suggested": [
      {"name": "build", "command": "go build ./...", "confidence": "high"},
      {"name": "test", "command": "go test ./...", "confidence": "high"}
    ]
  }
}
```

### Step 2: Get Questionnaire

```bash
canopy init --agent
```

Returns the detection plus a structured questionnaire:
```json
{
  "detection": { /* same as --detect */ },
  "questions": [
    {
      "id": "confirm_validation",
      "question": "I detected build and test commands. Enable post-merge validation?",
      "type": "single_select",
      "options": [
        {"value": "yes", "label": "Yes, validate after each merge"},
        {"value": "no", "label": "No, skip validation"}
      ],
      "default": "yes",
      "depends_on": null
    }
    /* ... more questions */
  ]
}
```

### Step 3: Apply Answers

After collecting user responses, apply them:

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

### Example: AI Agent Integration

Here's how an AI agent might use agentic init:

```python
import subprocess
import json

# Step 1: Get questionnaire
result = subprocess.run(["canopy", "init", "--agent"], capture_output=True, text=True)
questionnaire = json.loads(result.stdout)

# Step 2: Present questions to user (agent-specific)
answers = {}
for q in questionnaire["questions"]:
    # Check dependencies
    if q["depends_on"]:
        dep = q["depends_on"]
        if answers.get(dep["question_id"]) != dep["value"]:
            continue  # Skip this question

    # Present question to user and collect answer
    answer = ask_user(q["question"], q["options"], q["type"])
    answers[q["id"]] = answer

# Step 3: Apply answers
result = subprocess.run(
    ["canopy", "init", "--apply", json.dumps(answers)],
    capture_output=True, text=True
)
apply_result = json.loads(result.stdout)
print(f"Created: {apply_result['files_created']}")
```

### Question Types

| Type | Description | Answer Format |
|------|-------------|---------------|
| `single_select` | Pick one option | `"value"` string |
| `multi_select` | Pick multiple options | `["value1", "value2"]` array |
| `freeform` | Free text input | `"text"` string |

### Conditional Questions

Questions have a `depends_on` field that specifies when to show them:
- `null` = always show
- `{"question_id": "x", "value": "y"}` = show only if question `x` was answered with `y`

This allows agents to skip irrelevant questions (e.g., don't ask about validation mode if validation is disabled).

## Filtering Tasks

### Soft Filter with Prompt

```bash
# Guide agents to focus on specific work
canopy run --prompt "Only work on P0 and P1 tasks"

# Or focus on a specific area
canopy run --prompt "Focus on authentication-related tasks"
```

### Hard Filter by Priority

```bash
# Only run P0 tasks (critical)
canopy run --max-priority 0

# Only P0-P2 tasks (exclude backlog)
canopy run --max-priority 2
```

## Handling Failures

### Automatic Retries

By default, failed tasks retry up to 3 times:

```bash
# Default: 3 retries
canopy run

# No retries
canopy run --max-retries 0

# Infinite retries (careful!)
canopy run --max-retries -1
```

### Viewing History

```bash
# View recent runs
canopy history

# Show statistics
canopy stats
```

## Example: Feature Development

Here's a realistic workflow for developing a feature:

```bash
# 1. Create the epic
bd create --title "User authentication system" --type epic --priority 1

# 2. Break down into tasks
bd create --title "Design auth database schema" --type task --priority 0
bd create --title "Implement password hashing" --type task --priority 1
bd create --title "Create login endpoint" --type task --priority 1
bd create --title "Create signup endpoint" --type task --priority 1
bd create --title "Add session management" --type task --priority 2
bd create --title "Write auth tests" --type task --priority 2

# 3. Set up dependencies
# Login/signup depend on schema and hashing
bd dep add canopy-login canopy-schema
bd dep add canopy-login canopy-hashing
bd dep add canopy-signup canopy-schema
bd dep add canopy-signup canopy-hashing

# Sessions depend on login
bd dep add canopy-sessions canopy-login

# Tests depend on implementation
bd dep add canopy-tests canopy-login
bd dep add canopy-tests canopy-signup
bd dep add canopy-tests canopy-sessions

# Epic depends on all tasks
bd dep add canopy-epic canopy-tests

# 4. View dependency graph
bd show canopy-epic

# 5. Run in parallel
canopy run -c 4 -v
```

Execution flow:
1. **Batch 1**: Design schema (P0, no blockers)
2. **Batch 2**: Password hashing (P1, schema done)
3. **Batch 3**: Login + Signup (parallel, schema + hashing done)
4. **Batch 4**: Session management (login done)
5. **Batch 5**: Tests (all implementation done)
6. **Complete**: Close epic after verifying all tasks

## Tips and Best Practices

### Task Decomposition

- **Independent subtasks**: Maximize parallelism
- **Clear dependencies**: Model actual constraints
- **Reasonable granularity**: Not too small, not too large
- **Descriptive titles**: Agents use these for context

### Resource Management

- **Concurrency**: Start with 4, scale based on API limits
- **Retries**: Use 3 for flaky tasks, 0 for deterministic ones
- **Monitoring**: Use the dashboard for large runs

### Security

- **Use `--sandbox`**: For untrusted or experimental work
- **Review `.canopy/sandbox.toml`**: Customize for your tools
- **Check security blocklist**: Ensure sensitive paths are blocked

## Next Steps

- Read the [Architecture](ARCHITECTURE.md) for system design details
- Review [Commands](COMMANDS.md) for full CLI reference
- Check [Configuration](CONFIGURATION.md) for customization options
- Study [Sandbox Design](SANDBOX-DESIGN.md) for security details
