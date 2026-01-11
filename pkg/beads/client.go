package beads

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
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

// List returns all open tasks (status: open, in_progress, blocked).
// This is used by the daemon to populate the UI with pending work on startup.
func (c *Client) List() ([]Task, error) {
	// Get open tasks (default excludes closed)
	out, err := c.run("list", "--json", "--limit", "0")
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

// SlotHolderInfo contains parsed metadata about the slot holder.
// The holder field is encoded as "taskID|PID|timestamp" for staleness detection.
type SlotHolderInfo struct {
	TaskID    string    // Original task ID
	PID       int       // Process ID of the holder
	Timestamp time.Time // When the slot was acquired
	Raw       string    // Raw holder string (for compatibility)
}

// DefaultSlotTimeout is the default timeout after which a slot is considered stale.
const DefaultSlotTimeout = 10 * time.Minute

// EncodeSlotHolder creates a holder string with embedded PID and timestamp.
// Format: "taskID|PID|unixTimestamp"
func EncodeSlotHolder(taskID string) string {
	return fmt.Sprintf("%s|%d|%d", taskID, os.Getpid(), time.Now().Unix())
}

// DecodeSlotHolder parses a holder string to extract task ID, PID, and timestamp.
// Returns nil if the holder string is not in the expected format (backward compatible).
func DecodeSlotHolder(holder string) *SlotHolderInfo {
	if holder == "" {
		return nil
	}

	parts := strings.Split(holder, "|")
	if len(parts) != 3 {
		// Old format or simple holder - return with just raw info
		return &SlotHolderInfo{
			TaskID: holder,
			Raw:    holder,
		}
	}

	pid, err := strconv.Atoi(parts[1])
	if err != nil {
		return &SlotHolderInfo{TaskID: parts[0], Raw: holder}
	}

	ts, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return &SlotHolderInfo{TaskID: parts[0], PID: pid, Raw: holder}
	}

	return &SlotHolderInfo{
		TaskID:    parts[0],
		PID:       pid,
		Timestamp: time.Unix(ts, 0),
		Raw:       holder,
	}
}

// IsStale returns true if the slot holder is considered stale based on timeout.
func (s *SlotHolderInfo) IsStale(timeout time.Duration) bool {
	if s == nil || s.Timestamp.IsZero() {
		// Can't determine staleness without timestamp - assume not stale for safety
		return false
	}
	return time.Since(s.Timestamp) > timeout
}

// IsProcessDead checks if the holder process is no longer running.
// Returns false if PID is not available or process status can't be determined.
func (s *SlotHolderInfo) IsProcessDead() bool {
	if s == nil || s.PID == 0 {
		return false
	}

	// On Unix, sending signal 0 checks if process exists without affecting it
	proc, err := os.FindProcess(s.PID)
	if err != nil {
		return true // Can't find process, consider it dead
	}

	// Try to send signal 0 to check if process exists
	// Signal(syscall.Signal(0)) is the proper way to test process existence
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		// EPERM means process exists but we don't have permission - still alive
		// ESRCH means no such process - dead
		return true
	}

	return false
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

// GetHolderInfo parses the holder string to extract task ID, PID, and timestamp.
func (r *MergeSlotResult) GetHolderInfo() *SlotHolderInfo {
	return DecodeSlotHolder(r.Holder)
}

// IsSlotStale checks if the current slot holder is stale (timed out or dead process).
// Returns true if the slot should be force-released.
func (c *Client) IsSlotStale(timeout time.Duration) (bool, *SlotHolderInfo, error) {
	result, err := c.MergeSlotCheck()
	if err != nil {
		return false, nil, err
	}

	if result.Available {
		return false, nil, nil // Slot is available, not stale
	}

	info := result.GetHolderInfo()
	if info == nil {
		return false, nil, nil
	}

	// Check if process is dead (crashed orchestrator)
	if info.IsProcessDead() {
		return true, info, nil
	}

	// Check if slot has timed out
	if info.IsStale(timeout) {
		return true, info, nil
	}

	return false, info, nil
}

// MergeSlotForceRelease forcibly releases a stale merge slot.
// This should only be called after verifying the slot is stale via IsSlotStale.
func (c *Client) MergeSlotForceRelease(reason string) error {
	// We need to release without holder verification
	// The bd merge-slot release command allows releasing without --holder for force release
	args := []string{"merge-slot", "release"}

	_, err := c.run(args...)
	return err
}

// MergeSlotCleanupStale checks for and cleans up any stale merge slots.
// This should be called on orchestrator startup for crash recovery.
// Returns (cleaned, holderInfo, error) where cleaned indicates if a stale slot was released.
func (c *Client) MergeSlotCleanupStale(timeout time.Duration) (bool, *SlotHolderInfo, error) {
	isStale, info, err := c.IsSlotStale(timeout)
	if err != nil {
		return false, nil, err
	}

	if !isStale {
		return false, info, nil
	}

	// Force release the stale slot
	if err := c.MergeSlotForceRelease("stale slot cleanup"); err != nil {
		return false, info, fmt.Errorf("failed to force-release stale slot: %w", err)
	}

	return true, info, nil
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
