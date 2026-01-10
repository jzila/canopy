# Agent Instructions

This project uses **bd** (beads) for issue tracking and **canopy** for parallel agent orchestration.

Run `bd onboard` to learn beads, and `canopy help` to learn the orchestrator.

## Git Setup
Before any git operations, load your identity:
```bash
git config user.name && git config user.email  # Verify config exists
```

## Commit Standards
- Format: `type: concise description` (feat, fix, refactor, test, docs, chore)
- Reference bead ID when relevant: `fix(canopy-abc): description`
- Run `go test ./...` before committing code changes
- Run `go build ./...` to verify compilation

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
