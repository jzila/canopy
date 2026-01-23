package beads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Task represents a beads task returned from bd ready.
// This struct defines the minimal beads API surface that canopy depends on.
// Canopy treats beads as a minimal issue tracker with dependencies and MUST NOT
// depend on beads-specific features like gates, formulas, watchers, or GitHub integration.
type Task struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Type        string   `json:"type,omitempty"`        // bug, feature, task, chore
	Priority    int      `json:"priority,omitempty"`    // 0-4 (0=critical, 4=backlog)
	Status      string   `json:"status,omitempty"`      // open, in_progress, closed, deferred, needs-input
	Labels      []string `json:"labels,omitempty"`
	Assignee    string   `json:"assignee,omitempty"`
	Blockers    []string `json:"blockers,omitempty"`    // Tasks this task depends on (dependency IDs)
	BlockedBy   []string `json:"blocked_by,omitempty"`  // Alias for blockers
	Timeout     string   `json:"timeout,omitempty"`     // Per-task timeout (e.g., "5m", "30m", "1h")
	UpdatedAt   string   `json:"updated_at,omitempty"`  // ISO 8601 timestamp of last update
}

// BeadsClient defines the interface for interacting with the beads task tracker.
// This interface enables dependency injection and mocking for tests.
// All methods accept a context.Context as the first parameter for cancellation support.
type BeadsClient interface {
	// Ready returns all tasks with no open blockers
	Ready(ctx context.Context) ([]Task, error)

	// List returns all open tasks (status: open, in_progress, blocked)
	List(ctx context.Context) ([]Task, error)

	// ReadyWithArgs returns tasks with no open blockers using custom bd ready arguments
	ReadyWithArgs(ctx context.Context, args ...string) ([]Task, error)

	// Show returns detailed information about a task
	Show(ctx context.Context, taskID string) (*Task, error)

	// Start marks a task as in-progress
	Start(ctx context.Context, taskID string) error

	// Done marks a task as completed
	Done(ctx context.Context, taskID string) error

	// Fail marks a task as failed by closing it with a failure reason
	Fail(ctx context.Context, taskID string, reason string) error

	// NeedsInput marks a task as needing user input (paused state)
	// The sessionID is preserved so the task can be resumed later
	NeedsInput(ctx context.Context, taskID string, sessionID string, reason string) error

	// Create creates a new task and returns its ID
	Create(ctx context.Context, title string, priority int) (string, error)

	// CreateWithDescription creates a new task with a description and returns its ID
	CreateWithDescription(ctx context.Context, title, description string, priority int) (string, error)

	// AddDep adds a dependency: child is blocked by parent
	AddDep(ctx context.Context, child, parent string) error

	// GetDeps returns the task IDs that the given task depends on (its blockers)
	GetDeps(ctx context.Context, taskID string) ([]string, error)

	// Sync runs bd sync to commit and push beads changes
	Sync(ctx context.Context) error

	// AddComment adds a comment to a task
	AddComment(ctx context.Context, taskID, comment string) error
}

// Client wraps the bd CLI for programmatic access.
// It implements the BeadsClient interface.
// Client is safe for concurrent use from multiple goroutines.
type Client struct {
	bdPath  string
	workDir string
	mu      sync.Mutex // Serializes all bd CLI operations to prevent SQLite corruption
}

// Ensure Client implements BeadsClient
var _ BeadsClient = (*Client)(nil)

// NewClient creates a new beads client
func NewClient(workDir string) (*Client, error) {
	bdPath, err := exec.LookPath("bd")
	if err != nil {
		return nil, fmt.Errorf("bd not found in PATH: %w", err)
	}

	return &Client{
		bdPath:  bdPath,
		workDir: workDir,
	}, nil
}

// Ready returns all tasks with no open blockers
func (c *Client) Ready(ctx context.Context) ([]Task, error) {
	return c.ReadyWithArgs(ctx, "ready", "--json")
}

// List returns all open tasks (status: open, in_progress, blocked).
// This is used by the daemon to populate the UI with pending work on startup.
func (c *Client) List(ctx context.Context) ([]Task, error) {
	// Get open tasks (default excludes closed)
	out, err := c.run(ctx, "list", "--json", "--limit", "0")
	if err != nil {
		return nil, fmt.Errorf("bd list failed: %w", err)
	}

	out = strings.TrimSpace(out)
	if out == "" || out == "[]" {
		return nil, nil
	}

	var tasks []Task
	if err := json.Unmarshal([]byte(out), &tasks); err != nil {
		// Try parsing as newline-delimited JSON
		tasks = nil
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var task Task
			if err := json.Unmarshal([]byte(line), &task); err != nil {
				return nil, fmt.Errorf("failed to parse task: %w\nline: %s", err, line)
			}
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

// ReadyWithArgs returns tasks with no open blockers using custom bd ready arguments
func (c *Client) ReadyWithArgs(ctx context.Context, args ...string) ([]Task, error) {
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("bd ready failed: %w", err)
	}

	// Handle empty output
	out = strings.TrimSpace(out)
	if out == "" || out == "[]" {
		return nil, nil
	}

	var tasks []Task
	if err := json.Unmarshal([]byte(out), &tasks); err != nil {
		// Try parsing as newline-delimited JSON
		tasks = nil
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var task Task
			if err := json.Unmarshal([]byte(line), &task); err != nil {
				return nil, fmt.Errorf("failed to parse task: %w\nline: %s", err, line)
			}
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

// Start marks a task as in-progress
func (c *Client) Start(ctx context.Context, taskID string) error {
	_, err := c.run(ctx, "update", taskID, "--status", "in_progress")
	return err
}

// Done marks a task as completed
func (c *Client) Done(ctx context.Context, taskID string) error {
	_, err := c.run(ctx, "close", taskID)
	return err
}

// Fail marks a task as failed by reopening it so it can be retried.
// The failure reason is recorded in the reopen event.
func (c *Client) Fail(ctx context.Context, taskID string, reason string) error {
	_, err := c.run(ctx, "reopen", taskID, "--reason", "FAILED: "+reason)
	return err
}

// NeedsInput marks a task as needing user input by setting its status to needs-input.
// The sessionID is stored in the notes field so the task can be resumed later.
// This prevents the scheduler from picking up the task again until user provides input.
func (c *Client) NeedsInput(ctx context.Context, taskID string, sessionID string, reason string) error {
	// Update status to needs-input and store session ID in notes
	noteContent := fmt.Sprintf("NEEDS_INPUT: %s\nSession: %s", reason, sessionID)
	_, err := c.run(ctx, "update", taskID, "--status", "needs-input", "--notes", noteContent)
	return err
}

// Show returns detailed information about a task
func (c *Client) Show(ctx context.Context, taskID string) (*Task, error) {
	out, err := c.run(ctx, "show", taskID, "--json")
	if err != nil {
		return nil, fmt.Errorf("bd show failed: %w", err)
	}

	var task Task
	if err := json.Unmarshal([]byte(out), &task); err != nil {
		return nil, fmt.Errorf("failed to parse task: %w", err)
	}

	return &task, nil
}

// Create creates a new task
func (c *Client) Create(ctx context.Context, title string, priority int) (string, error) {
	out, err := c.run(ctx, "create", title, "-p", fmt.Sprintf("%d", priority), "--json")
	if err != nil {
		return "", fmt.Errorf("bd create failed: %w", err)
	}

	// Parse the created task ID from output
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		// Fallback: extract ID from non-JSON output
		out = strings.TrimSpace(out)
		if strings.HasPrefix(out, "bd-") {
			return strings.Fields(out)[0], nil
		}
		return "", fmt.Errorf("failed to parse task ID: %w", err)
	}

	return result.ID, nil
}

// CreateWithDescription creates a new task with a description
func (c *Client) CreateWithDescription(ctx context.Context, title, description string, priority int) (string, error) {
	out, err := c.run(ctx, "create", title, "-p", fmt.Sprintf("%d", priority), "-d", description, "--json")
	if err != nil {
		return "", fmt.Errorf("bd create failed: %w", err)
	}

	// Parse the created task ID from output
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		// Fallback: extract ID from non-JSON output
		out = strings.TrimSpace(out)
		if strings.HasPrefix(out, "bd-") {
			return strings.Fields(out)[0], nil
		}
		return "", fmt.Errorf("failed to parse task ID: %w", err)
	}

	return result.ID, nil
}

// AddDep adds a dependency: child is blocked by parent
func (c *Client) AddDep(ctx context.Context, child, parent string) error {
	_, err := c.run(ctx, "dep", "add", child, parent)
	return err
}

// GetDeps returns the task IDs that the given task depends on (its blockers)
func (c *Client) GetDeps(ctx context.Context, taskID string) ([]string, error) {
	task, err := c.Show(ctx, taskID)
	if err != nil {
		return nil, err
	}

	// Try both field names since beads might use either
	deps := task.Blockers
	if len(deps) == 0 {
		deps = task.BlockedBy
	}
	return deps, nil
}

// GetDependencies returns the dependency task IDs for a task
func (t *Task) GetDependencies() []string {
	if len(t.Blockers) > 0 {
		return t.Blockers
	}
	return t.BlockedBy
}

// GetTimeout parses and returns the timeout duration for the task.
// Returns 0 if not set (caller should use default).
func (t *Task) GetTimeout() time.Duration {
	if t == nil || t.Timeout == "" {
		return 0
	}
	d, err := time.ParseDuration(t.Timeout)
	if err != nil {
		return 0
	}
	return d
}

// Sync runs bd sync to commit and push beads changes
func (c *Client) Sync(ctx context.Context) error {
	_, err := c.run(ctx, "sync")
	return err
}

// AddComment adds a comment to a task
func (c *Client) AddComment(ctx context.Context, taskID, comment string) error {
	_, err := c.run(ctx, "comments", "add", taskID, comment)
	return err
}

// run executes a bd command and returns stdout.
// It serializes access to the bd CLI to prevent concurrent operations
// from corrupting the underlying SQLite database.
// The context is used for cancellation support.
func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cmd := exec.CommandContext(ctx, c.bdPath, args...)
	cmd.Dir = c.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Check if the context was cancelled
		if ctx.Err() != nil {
			return "", fmt.Errorf("command cancelled: %w", ctx.Err())
		}
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}

	return stdout.String(), nil
}
