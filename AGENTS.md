# Agent Instructions

This project uses **bd** (beads) for issue tracking and **canopy** for parallel agent orchestration.

Run `bd onboard` to learn beads, and `canopy help` to learn the orchestrator.

## Git Setup
Before any git operations, verify identity is configured:
```bash
git config user.name && git config user.email  # Must both exist
```
If missing, check `~/.gitconfig` or set locally for this repo.

## Commit Standards
- Format: `type: concise description` (feat, fix, refactor, test, docs, chore)
- Reference bead ID when relevant: `fix(canopy-abc): description`
- Run `go test ./...` before committing code changes
- Run `go build ./...` to verify compilation

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
