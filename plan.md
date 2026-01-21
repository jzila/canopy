# Implementation Plan: Refactor `canopy run` CLI to Delegate to Daemon

## Overview

Transform `canopy run` from an orchestration owner to a thin client that delegates orchestrator lifecycle to the daemon, while preserving backward compatibility.

## Current State Analysis

The codebase already has substantial infrastructure in place:

1. **OrchestratorManager** (`pkg/daemon/orchestrator_manager.go`) - Already exists and manages orchestrator lifecycle within the daemon
2. **OrchestrationHandler** (`pkg/daemon/orchestration_handler.go`) - REST API endpoints already implemented:
   - `POST /api/orchestrator/run` - Start a run
   - `POST /api/orchestrator/run/stop` - Stop a run
   - `GET /api/orchestrator/runs` - List all runs
   - `GET /api/orchestrator/runs/:id` - Get run status
3. **RunConfig** (`pkg/daemon/orchestrator_manager.go:19-32`) - Configuration struct already defined
4. **Singleton enforcement** - Already enforces one run per repository

**What's missing:**
- CLI doesn't use the daemon API - it creates the orchestrator directly
- No WebSocket event streaming to CLI
- No `--detach`, `--attach`, `--stop`, `--status` CLI flags
- No deprecation warnings for old flags

## Implementation Steps

### Step 1: Add Helper Functions for HTTP Client Communication

**File:** `cmd/canopy/run.go`

Add HTTP client helpers to communicate with daemon API:

```go
// startRunViaDaemon sends a run request to the daemon and returns the run ID.
func startRunViaDaemon(ctx context.Context, config daemon.RunConfig) (string, error)

// stopRunViaDaemon sends a stop request to the daemon.
func stopRunViaDaemon(ctx context.Context, runID string) error

// getRunStatusViaDaemon queries the daemon for run status.
func getRunStatusViaDaemon(ctx context.Context, runID string) (*daemon.RunStatusWire, error)

// getActiveRunForRepo queries the daemon for active run on a repo path.
func getActiveRunForRepo(ctx context.Context, repoPath string) (*daemon.RunStatusWire, error)
```

### Step 2: Add New CLI Flags

**File:** `cmd/canopy/run.go`

Add new flags in `init()`:

```go
var (
    detach    bool   // Start run and exit immediately
    attachID  string // Attach to existing run by ID
    stopRun   bool   // Stop active run for current repo
    statusRun bool   // Show active run status
)

func init() {
    // ... existing flags ...
    runCmd.Flags().BoolVar(&detach, "detach", false, "Start run and exit immediately (daemon continues)")
    runCmd.Flags().StringVar(&attachID, "attach", "", "Attach to existing run to stream output")
    runCmd.Flags().BoolVar(&stopRun, "stop", false, "Stop active run for current repo")
    runCmd.Flags().BoolVar(&statusRun, "status", false, "Show active run status for current repo")
}
```

### Step 3: Refactor `runOrchestrator` to Delegate to Daemon

**File:** `cmd/canopy/run.go`

Restructure the main function:

```go
func runOrchestrator(cmd *cobra.Command, args []string) error {
    // Handle special modes first
    if stopRun {
        return handleStopRun(ctx, absWorkdir)
    }
    if statusRun {
        return handleShowStatus(ctx, absWorkdir)
    }
    if attachID != "" {
        return handleAttachRun(ctx, attachID)
    }

    // Connect to daemon (required)
    // ... existing daemon connection code ...

    // Build RunConfig from flags
    config := buildRunConfig(absWorkdir, outputDir, repoID)

    // Start run via daemon API
    runID, err := startRunViaDaemon(ctx, config)
    if err != nil {
        return handleStartError(err)
    }

    if detach {
        fmt.Printf("Run started: %s\n", runID)
        return nil
    }

    // Stream events via WebSocket until run completes
    return streamRunEvents(ctx, runID)
}
```

### Step 4: Implement WebSocket Event Streaming

**File:** `cmd/canopy/run.go`

Add WebSocket client to stream run events:

```go
// streamRunEvents connects to the daemon WebSocket and streams events
// until the run completes or is cancelled.
func streamRunEvents(ctx context.Context, runID string) error {
    // Connect to ws://localhost:5555/ws
    // Filter events by run_id
    // Format and print events to stdout (same format as current output)
    // Handle reconnection gracefully
    // Return when run completes
}
```

The event types to handle:
- `agent_started` - Print "Starting agent {task_title}"
- `agent_output` - Print stdout/stderr to console
- `agent_live_feed` - Optional: show real-time tool use
- `agent_completed` - Print completion summary
- `run_completed` - Print final stats and return

### Step 5: Add Stop and Status Command Handlers

**File:** `cmd/canopy/run.go`

```go
// handleStopRun stops the active run for the current repository.
func handleStopRun(ctx context.Context, workDir string) error {
    // GET /api/orchestrator/runs to find active run for this repo
    // POST /api/orchestrator/run/stop with run_id
    // Print confirmation message
}

// handleShowStatus shows the status of the active run.
func handleShowStatus(ctx context.Context, workDir string) error {
    // GET /api/orchestrator/runs and filter by repo path
    // Print status details (tasks done, failed, duration, etc.)
}

// handleAttachRun attaches to an existing run to stream output.
func handleAttachRun(ctx context.Context, runID string) error {
    // Verify run exists and is active
    // Start streaming events via WebSocket
}
```

### Step 6: Add API Endpoint for Active Run by Repo Path

**File:** `pkg/daemon/orchestration_handler.go`

Add new endpoint to query active run by repo path:

```go
// HandleGetActiveRun handles GET /api/orchestrator/active?repo_path=...
func (h *OrchestrationHandler) HandleGetActiveRun(w http.ResponseWriter, r *http.Request)
```

Update `RouteOrchestrator` to route this new endpoint.

### Step 7: Handle Ctrl+C (Signal Handling)

**File:** `cmd/canopy/run.go`

The signal handler needs adjustment:
- If `--detach`: just exit (daemon continues)
- If attached/streaming: graceful disconnect, print message, exit
- Option: add `--stop-on-exit` flag to stop the daemon run when CLI exits

### Step 8: Backward Compatibility

The existing CLI interface should continue to work:
- `canopy run` - Works (delegates to daemon, streams output)
- `canopy run -c 8` - Works (passes concurrency to daemon)
- `canopy run --sandbox` - Works (passes use_bwrap to daemon)
- `canopy run --dry-run` - Works (still handled locally in CLI for preview)
- `canopy run --resume` - Works (handled locally, resumes agents)

### Step 9: Update Dry-Run to Stay Local

Dry-run shows execution plan without actually running - this can stay in the CLI:

```go
if dryRun {
    // Keep existing dry-run logic that shows what would execute
    // Don't delegate to daemon for this
    return handleDryRun(ctx, absWorkdir)
}
```

## File Changes Summary

| File | Changes |
|------|---------|
| `cmd/canopy/run.go` | Major refactor: delegate to daemon, add WebSocket streaming, new flags |
| `pkg/daemon/orchestration_handler.go` | Add `/api/orchestrator/active` endpoint |
| `pkg/daemon/server.go` | Route new endpoint |

## API Changes

### New Endpoint

```
GET /api/orchestrator/active?repo_path=/path/to/repo

Response:
{
  "success": true,
  "run": {
    "id": "run-abc123",
    "repo_path": "/path/to/repo",
    "status": "running",
    "start_time": 1234567890,
    "tasks_total": 5,
    "tasks_done": 2,
    "tasks_failed": 0
  }
}
```

### Existing Endpoints (unchanged)

- `POST /api/orchestrator/run` - Start run (already exists)
- `POST /api/orchestrator/run/stop` - Stop run (already exists)
- `GET /api/orchestrator/runs` - List runs (already exists)
- `GET /api/orchestrator/runs/:id` - Get run status (already exists)

## Testing Strategy

1. **Unit Tests:**
   - Test HTTP client helpers (mock responses)
   - Test flag parsing logic

2. **Integration Tests:**
   - Test full flow: `canopy run` → daemon → WebSocket streaming → completion
   - Test `--detach` mode
   - Test `--stop` command
   - Test signal handling (Ctrl+C behavior)

3. **Manual Testing:**
   - Run with multiple concurrent agents
   - Test reconnection if daemon restarts
   - Verify output format matches current behavior

## Migration Path

No breaking changes - existing commands work identically. New flags provide additional capabilities:

- `canopy run` - Same behavior (now via daemon)
- `canopy run --detach` - New: start and exit
- `canopy run --attach <id>` - New: reattach to run
- `canopy run --stop` - New: stop current repo's run
- `canopy run --status` - New: show run status

## Dependencies

This task depends on the daemon already owning the orchestrator lifecycle, which is implemented in `pkg/daemon/orchestrator_manager.go`. The existing infrastructure is sufficient.

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| WebSocket disconnection | Implement reconnection with backoff |
| Daemon not running | Auto-start daemon (existing behavior) |
| Run state lost on daemon restart | Persistence already handles this via SQLite |
| Output format changes | Keep same terminal output format |

## Estimated Scope

- **Files modified:** 3
- **New functions:** ~10
- **Complexity:** Medium (mainly plumbing, existing infrastructure)
