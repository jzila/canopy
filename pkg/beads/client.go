package beads

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Task represents a beads task returned from bd ready
type Task struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Priority    int      `json:"priority,omitempty"`
	Status      string   `json:"status,omitempty"`
	Blockers    []string `json:"blockers,omitempty"`    // Tasks this task depends on
	BlockedBy   []string `json:"blocked_by,omitempty"`  // Alias for blockers
}

// Client wraps the bd CLI for programmatic access
type Client struct {
	bdPath  string
	workDir string
}

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
func (c *Client) Ready() ([]Task, error) {
	return c.ReadyWithArgs("ready", "--json")
}

// ReadyWithArgs returns tasks with no open blockers using custom bd ready arguments
func (c *Client) ReadyWithArgs(args ...string) ([]Task, error) {
	out, err := c.run(args...)
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
func (c *Client) Start(taskID string) error {
	_, err := c.run("update", taskID, "--status", "in_progress")
	return err
}

// Done marks a task as completed
func (c *Client) Done(taskID string) error {
	_, err := c.run("close", taskID)
	return err
}

// Fail marks a task as failed by closing it with a failure reason
func (c *Client) Fail(taskID string, reason string) error {
	// beads doesn't have a "failed" status - valid statuses are:
	// open, in_progress, blocked, deferred, closed
	// We close the task and record the failure reason in notes
	_, err := c.run("close", taskID, "--reason", "FAILED: "+reason)
	return err
}

// Show returns detailed information about a task
func (c *Client) Show(taskID string) (*Task, error) {
	out, err := c.run("show", taskID, "--json")
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
func (c *Client) Create(title string, priority int) (string, error) {
	out, err := c.run("create", title, "-p", fmt.Sprintf("%d", priority), "--json")
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
func (c *Client) AddDep(child, parent string) error {
	_, err := c.run("dep", "add", child, parent)
	return err
}

// GetDeps returns the task IDs that the given task depends on (its blockers)
func (c *Client) GetDeps(taskID string) ([]string, error) {
	task, err := c.Show(taskID)
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

// MergeSlotResult represents the result of a merge-slot operation
type MergeSlotResult struct {
	Available bool     `json:"available"`
	Holder    string   `json:"holder,omitempty"`
	Waiters   []string `json:"waiters,omitempty"`
	Acquired  bool     `json:"acquired,omitempty"`
	Released  bool     `json:"released,omitempty"`
}

// MergeSlotAcquire tries to acquire the merge slot for exclusive access.
// If wait is true and the slot is held, adds to the waiters queue.
// Returns acquired=true if slot was acquired, false otherwise.
func (c *Client) MergeSlotAcquire(holder string, wait bool) (*MergeSlotResult, error) {
	args := []string{"merge-slot", "acquire", "--json"}
	if holder != "" {
		args = append(args, "--holder", holder)
	}
	if wait {
		args = append(args, "--wait")
	}

	out, err := c.run(args...)
	if err != nil {
		// If the slot is held, bd returns an error but may still have useful JSON
		// Try to parse as MergeSlotResult anyway
		out = strings.TrimSpace(out)
		if out == "" {
			return nil, fmt.Errorf("merge-slot acquire failed: %w", err)
		}
	}

	out = strings.TrimSpace(out)
	if out == "" {
		// Successful acquire with no JSON output means we got the slot
		return &MergeSlotResult{Acquired: true}, nil
	}

	var result MergeSlotResult
	if jsonErr := json.Unmarshal([]byte(out), &result); jsonErr != nil {
		if err != nil {
			return nil, fmt.Errorf("merge-slot acquire failed: %w", err)
		}
		return nil, fmt.Errorf("failed to parse merge-slot result: %w", jsonErr)
	}

	return &result, err
}

// MergeSlotRelease releases the merge slot after merge is complete.
func (c *Client) MergeSlotRelease(holder string) error {
	args := []string{"merge-slot", "release"}
	if holder != "" {
		args = append(args, "--holder", holder)
	}

	_, err := c.run(args...)
	return err
}

// MergeSlotCheck checks if the merge slot is available.
func (c *Client) MergeSlotCheck() (*MergeSlotResult, error) {
	out, err := c.run("merge-slot", "check", "--json")
	if err != nil {
		return nil, fmt.Errorf("merge-slot check failed: %w", err)
	}

	out = strings.TrimSpace(out)
	if out == "" {
		return &MergeSlotResult{Available: true}, nil
	}

	var result MergeSlotResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return nil, fmt.Errorf("failed to parse merge-slot result: %w", err)
	}

	return &result, nil
}

// Sync runs bd sync to commit and push beads changes
func (c *Client) Sync() error {
	_, err := c.run("sync")
	return err
}

// run executes a bd command and returns stdout
func (c *Client) run(args ...string) (string, error) {
	cmd := exec.Command(c.bdPath, args...)
	cmd.Dir = c.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}

	return stdout.String(), nil
}
