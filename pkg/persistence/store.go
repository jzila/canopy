package persistence

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jzila/canopy/pkg/types"
	_ "modernc.org/sqlite"
)

// Store provides SQLite-based persistence for canopy run history
type Store struct {
	db     *sql.DB
	dbPath string
}

// RunStatus represents the status of a run
type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
	RunStatusPartial   RunStatus = "partial" // Some tasks succeeded, some failed
)

// AgentStatus represents the status of an agent
type AgentStatus string

const (
	AgentStatusStarting  AgentStatus = "starting"
	AgentStatusRunning   AgentStatus = "running"
	AgentStatusCompleted AgentStatus = "completed"
	AgentStatusFailed    AgentStatus = "failed"
	AgentStatusTimedOut  AgentStatus = "timed_out"
	AgentStatusCancelled AgentStatus = "cancelled"
)

// Run represents a single canopy orchestration run
type Run struct {
	ID             string     `json:"id"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Status         RunStatus  `json:"status"`
	Concurrency    int        `json:"concurrency"`
	GitBranch      string     `json:"git_branch,omitempty"`
	GitCommit      string     `json:"git_commit,omitempty"`
	TotalTasks     int        `json:"total_tasks"`
	CompletedTasks int        `json:"completed_tasks"`
	FailedTasks    int        `json:"failed_tasks"`
	RepoID         string     `json:"repo_id,omitempty"`
	RepoPath       string     `json:"repo_path,omitempty"`
	RepoName       string     `json:"repo_name,omitempty"`
	// Aggregate statistics (populated at run completion)
	TotalInputTokens     int     `json:"total_input_tokens"`
	TotalOutputTokens    int     `json:"total_output_tokens"`
	CacheCreationTokens  int     `json:"cache_creation_tokens"`
	CacheReadTokens      int     `json:"cache_read_tokens"`
	TotalCostUSD         float64 `json:"total_cost_usd"`
	TotalTurns           int     `json:"total_turns"`
	FilesChanged         int     `json:"files_changed"`
	GitCommits           int     `json:"git_commits"`
	DurationSeconds      float64 `json:"duration_seconds"`
}

// MergeStatus is an alias to types.MergeStatus for backwards compatibility.
// The persistence layer uses final MergeStatus values (None, Merged, Failed, Resolved,
// Skipped, MergedNeedsRepair) but shares the same underlying type for consistency.
type MergeStatus = types.MergeStatus

// MergeStatus constants - aliases to types package for backwards compatibility.
// Note: persistence uses final statuses (not queue states like pending, merging).
const (
	MergeStatusNone             = types.MergeStatusNone
	MergeStatusMerged           = types.MergeStatusMerged
	MergeStatusFailed           = types.MergeStatusFailed
	MergeStatusResolved         = types.MergeStatusResolved
	MergeStatusSkipped          = types.MergeStatusSkipped
	MergeStatusMergedNeedsRepair = types.MergeStatusMergedNeedsRepair
)

// Task represents a beads task persisted in the database
type Task struct {
	ID        string `json:"id"`
	RepoID    string `json:"repo_id,omitempty"`
	Title     string `json:"title"`
	Status    string `json:"status"` // ready, in_progress, completed, failed, blocked, needs-input
	Type      string `json:"type,omitempty"`
	Priority  int    `json:"priority"`
	AgentID   string `json:"agent_id,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

// AgentCommit represents a git commit made by an agent, persisted in the database
type AgentCommit struct {
	ID           int64    `json:"id"`
	AgentID      string   `json:"agent_id"`
	Hash         string   `json:"hash"`
	ShortHash    string   `json:"short_hash"`
	Message      string   `json:"message"`
	Author       string   `json:"author,omitempty"`
	AuthorEmail  string   `json:"author_email,omitempty"`
	Timestamp    string   `json:"timestamp,omitempty"`
	FilesChanged []string `json:"files_changed,omitempty"`
	CreatedAt    int64    `json:"created_at,omitempty"`
}

// ActiveOverlay represents an overlay filesystem being tracked for agent resumability
type ActiveOverlay struct {
	AgentID   string `json:"agent_id"`
	TaskID    string `json:"task_id"`
	RunID     string `json:"run_id"`
	SessionID string `json:"session_id,omitempty"`
	UpperDir  string `json:"upper_dir"`
	MergedDir string `json:"merged_dir"`
	LowerDir  string `json:"lower_dir"`
	WorkDir   string `json:"work_dir"`
	CreatedAt int64  `json:"created_at"`
	Status    string `json:"status"` // active, completed, orphaned
}

// RunConfig represents saved orchestrator configuration for a repository.
// Note: Task selection parameters (priority_max, types, labels) are stored in
// RulesSettings and persisted via the rules API, not in RunConfig.
type RunConfig struct {
	RepoID      string `json:"repo_id"`
	Concurrency int    `json:"concurrency"`
	PriorityMax int    `json:"priority_max"` // Max priority filter for task selection
	UseBwrap    bool   `json:"use_bwrap"`
	MaxRetries  int    `json:"max_retries"`
	UpdatedAt   int64  `json:"updated_at,omitempty"`
}

// Agent represents a single agent execution within a run
type Agent struct {
	ID              string      `json:"id"`
	RunID           string      `json:"run_id"`
	TaskID          string      `json:"task_id"`
	TaskTitle       string      `json:"task_title"`
	TaskDescription string      `json:"task_description,omitempty"` // Full task description from beads
	Status          AgentStatus `json:"status"`
	LifecycleState  string      `json:"lifecycle_state,omitempty"` // Unified lifecycle state (source of truth)
	StartedAt           time.Time   `json:"started_at"`
	FinishedAt          *time.Time  `json:"finished_at,omitempty"`
	DurationSeconds     float64     `json:"duration_seconds,omitempty"`
	ExitCode            *int        `json:"exit_code,omitempty"`
	ErrorMessage        string      `json:"error_message,omitempty"`
	Stdout              string      `json:"stdout,omitempty"`
	Stderr              string      `json:"stderr,omitempty"`
	InputTokens         int         `json:"input_tokens"`
	OutputTokens        int         `json:"output_tokens"`
	TotalTokens         int         `json:"total_tokens"`
	CacheCreationTokens int         `json:"cache_creation_tokens"`
	CacheReadTokens     int         `json:"cache_read_tokens"`
	CostUSD             float64     `json:"cost_usd"`
	FilesChanged        int         `json:"files_changed"`
	GitCommitsCreated   int         `json:"git_commits_created"`
	NumTurns            int         `json:"num_turns"`
	ResultMessage       string      `json:"result_message,omitempty"`
	RepoID              string      `json:"repo_id,omitempty"`
	Archived            bool        `json:"archived"`
	ParentAgentID       string      `json:"parent_agent_id,omitempty"` // ID of parent agent for resolver agents
	// Merge result fields
	MergeStatus          MergeStatus `json:"merge_status,omitempty"`
	MergeCommitsApplied  int         `json:"merge_commits_applied"`
	MergeHadConflict     bool        `json:"merge_had_conflict"`
	MergeResolverSpawned bool        `json:"merge_resolver_spawned"`
	MergeError           string      `json:"merge_error,omitempty"`
	// Validation result fields
	ValidationStatus   string `json:"validation_status,omitempty"`      // Overall status: "pending", "running", "passed", "failed", "skipped", "repairing"
	ValidationDuration int64  `json:"validation_duration_ms,omitempty"` // Total validation duration in milliseconds
	ValidationError    string `json:"validation_error,omitempty"`       // Error message if validation failed
	ValidationSteps    string `json:"validation_steps,omitempty"`       // JSON-encoded array of validation steps
	// Repair agent tracking fields
	RepairAttempts   int    `json:"repair_attempts"`              // Number of repair attempts made (0 = no repairs attempted)
	LastRepairOutput string `json:"last_repair_output,omitempty"` // Output/error from the last repair attempt
	// Session tracking for claude --resume support
	SessionID string `json:"session_id,omitempty"` // Claude CLI session ID for resumability
	// Task retry tracking fields
	Attempt    int `json:"attempt"`     // Current attempt number (1 = first try)
	MaxRetries int `json:"max_retries"` // Max retry attempts configured
}

// RunFilter specifies criteria for querying runs
type RunFilter struct {
	Since  *time.Time // Only runs started after this time
	Before *time.Time // Only runs started before this time
	Status RunStatus  // Filter by status (empty means all)
	RepoID string     // Filter by repository ID (empty means all)
	Limit  int        // Max results (0 means default of 50)
	Offset int        // Pagination offset
}

// RunListResult contains paginated run query results
type RunListResult struct {
	Runs       []Run `json:"runs"`
	Pagination struct {
		Total  int `json:"total"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	} `json:"pagination"`
}

// AggregateStats contains overall statistics across runs
type AggregateStats struct {
	TotalRuns                int     `json:"total_runs"`
	CompletedRuns            int     `json:"completed_runs"`
	FailedRuns               int     `json:"failed_runs"`
	PartialRuns              int     `json:"partial_runs"`
	TotalAgents              int     `json:"total_agents"`
	TotalTasks               int     `json:"total_tasks"`
	CompletedTasks           int     `json:"completed_tasks"`
	FailedTasks              int     `json:"failed_tasks"`
	TotalTokens              int     `json:"total_tokens"`
	TotalInputTokens         int     `json:"total_input_tokens"`
	TotalOutputTokens        int     `json:"total_output_tokens"`
	TotalCacheCreationTokens int     `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens     int     `json:"total_cache_read_tokens"`
	TotalCostUSD             float64 `json:"total_cost_usd"`
	TotalDurationSeconds     float64 `json:"total_duration_seconds"`
	FilesChanged             int     `json:"files_changed"`
	GitCommits               int     `json:"git_commits"`
	// Resolver statistics (aggregated from agents with merge_had_conflict=true)
	TotalConflicts       int     `json:"total_conflicts"`
	ResolvedConflicts    int     `json:"resolved_conflicts"`
	FailedResolutions    int     `json:"failed_resolutions"`
	AvgResolutionSeconds float64 `json:"avg_resolution_seconds"`
}

// NewStore creates a new Store with the default database path
func NewStore() (*Store, error) {
	dbPath := getDefaultDBPath()
	return NewStoreWithPath(dbPath)
}

// NewStoreWithPath creates a new Store with a custom database path
func NewStoreWithPath(dbPath string) (*Store, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database with WAL mode and timeout
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_timeout=5000&_foreign_keys=on", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for SQLite with WAL mode
	// WAL mode enables concurrent readers with a single writer. Multiple connections
	// allow parallel reads while writes are serialized by SQLite itself. The DSN
	// _timeout=5000 parameter handles write contention by retrying for 5 seconds.
	// This avoids application-level serialization bottlenecks while SQLite manages
	// write locking internally.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	store := &Store{
		db:     db,
		dbPath: dbPath,
	}

	// Run migrations
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return store, nil
}

// Close closes the database connection, performing a final checkpoint first
func (s *Store) Close() error {
	if s.db != nil {
		// Run a TRUNCATE checkpoint to merge WAL into main database file
		// This ensures clean shutdown with no WAL files left behind
		if _, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			// Log checkpoint failure but continue with close - non-fatal
			// The WAL file will be cleaned up on next open
			fmt.Fprintf(os.Stderr, "warning: failed to checkpoint WAL: %v\n", err)
		}
		return s.db.Close()
	}
	return nil
}

// DB returns the underlying database connection for advanced operations.
// This is primarily used by migration tools that need direct database access.
// Most code should use the Store methods rather than accessing the DB directly.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Tx represents an active database transaction.
// Use Store.BeginTx() to start a transaction, then call Commit() or Rollback().
type Tx struct {
	tx    *sql.Tx
	store *Store
}

// BeginTx starts a new database transaction.
// The returned Tx must be committed with Commit() or rolled back with Rollback().
// Example usage:
//
//	tx, err := store.BeginTx()
//	if err != nil { return err }
//	defer func() { _ = tx.Rollback() }() // no-op if already committed
//
//	// perform operations...
//	if err := tx.CreateRun(run); err != nil { return err }
//	if err := tx.CreateAgent(agent); err != nil { return err }
//
//	return tx.Commit()
func (s *Store) BeginTx() (*Tx, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	return &Tx{tx: tx, store: s}, nil
}

// Commit commits the transaction.
func (t *Tx) Commit() error {
	if err := t.tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// Rollback rolls back the transaction.
// Rollback is a no-op if the transaction has already been committed.
func (t *Tx) Rollback() error {
	return t.tx.Rollback()
}

// CreateRun creates a new run record within the transaction.
func (t *Tx) CreateRun(run *Run) error {
	query := `
		INSERT INTO runs (id, started_at, finished_at, status, concurrency, git_branch, git_commit, total_tasks, completed_tasks, failed_tasks, repo_id, repo_path, repo_name, total_input_tokens, total_output_tokens, cache_creation_tokens, cache_read_tokens, total_cost_usd, total_turns, files_changed, git_commits, duration_seconds)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	var finishedAt *int64
	if run.FinishedAt != nil {
		ts := run.FinishedAt.Unix()
		finishedAt = &ts
	}
	_, err := t.tx.Exec(query,
		run.ID,
		run.StartedAt.Unix(),
		finishedAt,
		string(run.Status),
		run.Concurrency,
		run.GitBranch,
		run.GitCommit,
		run.TotalTasks,
		run.CompletedTasks,
		run.FailedTasks,
		nullString(run.RepoID),
		nullString(run.RepoPath),
		nullString(run.RepoName),
		run.TotalInputTokens,
		run.TotalOutputTokens,
		run.CacheCreationTokens,
		run.CacheReadTokens,
		run.TotalCostUSD,
		run.TotalTurns,
		run.FilesChanged,
		run.GitCommits,
		run.DurationSeconds,
	)
	return err
}

// CreateAgent creates a new agent record within the transaction.
// If an agent with the same ID already exists (e.g., retrying a failed task),
// the existing record is updated with the new values.
func (t *Tx) CreateAgent(agent *Agent) error {
	query := `
		INSERT INTO agents (id, run_id, task_id, task_title, task_description, status, lifecycle_state, started_at, finished_at, duration_seconds, exit_code, error_message, stdout, stderr, input_tokens, output_tokens, total_tokens, cache_creation_tokens, cache_read_tokens, cost_usd, files_changed, git_commits_created, num_turns, result_message, repo_id, archived, parent_agent_id, merge_status, merge_commits_applied, merge_had_conflict, merge_resolver_spawned, merge_error, validation_status, validation_duration_ms, validation_error, validation_steps, session_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			run_id = excluded.run_id,
			task_description = excluded.task_description,
			status = excluded.status,
			lifecycle_state = excluded.lifecycle_state,
			started_at = excluded.started_at,
			finished_at = excluded.finished_at,
			duration_seconds = excluded.duration_seconds,
			exit_code = excluded.exit_code,
			error_message = excluded.error_message,
			stdout = excluded.stdout,
			stderr = excluded.stderr,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			total_tokens = excluded.total_tokens,
			cache_creation_tokens = excluded.cache_creation_tokens,
			cache_read_tokens = excluded.cache_read_tokens,
			cost_usd = excluded.cost_usd,
			files_changed = excluded.files_changed,
			git_commits_created = excluded.git_commits_created,
			num_turns = excluded.num_turns,
			result_message = excluded.result_message,
			parent_agent_id = excluded.parent_agent_id,
			merge_status = excluded.merge_status,
			merge_commits_applied = excluded.merge_commits_applied,
			merge_had_conflict = excluded.merge_had_conflict,
			merge_resolver_spawned = excluded.merge_resolver_spawned,
			merge_error = excluded.merge_error,
			validation_status = excluded.validation_status,
			validation_duration_ms = excluded.validation_duration_ms,
			validation_error = excluded.validation_error,
			validation_steps = excluded.validation_steps,
			session_id = excluded.session_id
	`
	var finishedAt *int64
	if agent.FinishedAt != nil {
		ts := agent.FinishedAt.Unix()
		finishedAt = &ts
	}
	_, err := t.tx.Exec(query,
		agent.ID,
		agent.RunID,
		agent.TaskID,
		agent.TaskTitle,
		nullString(agent.TaskDescription),
		string(agent.Status),
		nullString(agent.LifecycleState),
		agent.StartedAt.Unix(),
		finishedAt,
		agent.DurationSeconds,
		agent.ExitCode,
		agent.ErrorMessage,
		agent.Stdout,
		agent.Stderr,
		agent.InputTokens,
		agent.OutputTokens,
		agent.TotalTokens,
		agent.CacheCreationTokens,
		agent.CacheReadTokens,
		agent.CostUSD,
		agent.FilesChanged,
		agent.GitCommitsCreated,
		agent.NumTurns,
		agent.ResultMessage,
		nullString(agent.RepoID),
		boolToInt(agent.Archived),
		nullString(agent.ParentAgentID),
		nullString(string(agent.MergeStatus)),
		agent.MergeCommitsApplied,
		boolToInt(agent.MergeHadConflict),
		boolToInt(agent.MergeResolverSpawned),
		nullString(agent.MergeError),
		nullString(agent.ValidationStatus),
		agent.ValidationDuration,
		nullString(agent.ValidationError),
		nullString(agent.ValidationSteps),
		nullString(agent.SessionID),
	)
	return err
}

// Checkpoint runs a WAL checkpoint to merge the write-ahead log into the main database.
// This should be called periodically to prevent unbounded WAL growth.
// mode can be: PASSIVE (non-blocking), FULL (waits for readers), TRUNCATE (resets WAL)
func (s *Store) Checkpoint(mode string) error {
	if mode == "" {
		mode = "PASSIVE"
	}
	_, err := s.db.Exec(fmt.Sprintf("PRAGMA wal_checkpoint(%s)", mode))
	if err != nil {
		return fmt.Errorf("checkpoint failed: %w", err)
	}
	return nil
}

// getDefaultDBPath returns the default database path based on XDG_CACHE_HOME
func getDefaultDBPath() string {
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheDir, "canopy", "runs.db")
}

// CreateRun creates a new run record
func (s *Store) CreateRun(run *Run) error {
	query := `
		INSERT INTO runs (id, started_at, finished_at, status, concurrency, git_branch, git_commit, total_tasks, completed_tasks, failed_tasks, repo_id, repo_path, repo_name, total_input_tokens, total_output_tokens, cache_creation_tokens, cache_read_tokens, total_cost_usd, total_turns, files_changed, git_commits, duration_seconds)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	var finishedAt *int64
	if run.FinishedAt != nil {
		ts := run.FinishedAt.Unix()
		finishedAt = &ts
	}
	_, err := s.db.Exec(query,
		run.ID,
		run.StartedAt.Unix(),
		finishedAt,
		string(run.Status),
		run.Concurrency,
		run.GitBranch,
		run.GitCommit,
		run.TotalTasks,
		run.CompletedTasks,
		run.FailedTasks,
		nullString(run.RepoID),
		nullString(run.RepoPath),
		nullString(run.RepoName),
		run.TotalInputTokens,
		run.TotalOutputTokens,
		run.CacheCreationTokens,
		run.CacheReadTokens,
		run.TotalCostUSD,
		run.TotalTurns,
		run.FilesChanged,
		run.GitCommits,
		run.DurationSeconds,
	)
	return err
}

// nullString returns nil for empty strings, otherwise the string pointer
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// UpdateRun updates an existing run record
func (s *Store) UpdateRun(run *Run) error {
	query := `
		UPDATE runs SET
			finished_at = ?,
			status = ?,
			total_tasks = ?,
			completed_tasks = ?,
			failed_tasks = ?,
			repo_id = ?,
			repo_path = ?,
			repo_name = ?,
			total_input_tokens = ?,
			total_output_tokens = ?,
			cache_creation_tokens = ?,
			cache_read_tokens = ?,
			total_cost_usd = ?,
			total_turns = ?,
			files_changed = ?,
			git_commits = ?,
			duration_seconds = ?
		WHERE id = ?
	`
	var finishedAt *int64
	if run.FinishedAt != nil {
		ts := run.FinishedAt.Unix()
		finishedAt = &ts
	}
	_, err := s.db.Exec(query,
		finishedAt,
		string(run.Status),
		run.TotalTasks,
		run.CompletedTasks,
		run.FailedTasks,
		nullString(run.RepoID),
		nullString(run.RepoPath),
		nullString(run.RepoName),
		run.TotalInputTokens,
		run.TotalOutputTokens,
		run.CacheCreationTokens,
		run.CacheReadTokens,
		run.TotalCostUSD,
		run.TotalTurns,
		run.FilesChanged,
		run.GitCommits,
		run.DurationSeconds,
		run.ID,
	)
	return err
}

// runColumns lists all columns for run queries
const runColumns = `id, started_at, finished_at, status, concurrency, git_branch, git_commit, total_tasks, completed_tasks, failed_tasks, repo_id, repo_path, repo_name, total_input_tokens, total_output_tokens, cache_creation_tokens, cache_read_tokens, total_cost_usd, total_turns, files_changed, git_commits, duration_seconds`

// GetRun retrieves a run by ID
func (s *Store) GetRun(id string) (*Run, error) {
	query := `SELECT ` + runColumns + ` FROM runs WHERE id = ?`
	row := s.db.QueryRow(query, id)
	return s.scanRun(row)
}

// FindRunByPrefix finds a run by ID prefix (for short ID lookup).
// Returns an error if no matches found or if the prefix is ambiguous (matches multiple runs).
func (s *Store) FindRunByPrefix(prefix string) (*Run, error) {
	query := `SELECT ` + runColumns + ` FROM runs WHERE id LIKE ? ORDER BY started_at DESC`
	rows, err := s.db.Query(query, prefix+"%")
	if err != nil {
		return nil, fmt.Errorf("failed to query runs by prefix: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var matches []*Run
	for rows.Next() {
		run, err := s.scanRunFromRows(rows)
		if err != nil {
			return nil, err
		}
		matches = append(matches, run)
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("run not found: %s", prefix)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous run ID prefix: %s (matches %d runs)", prefix, len(matches))
	}

	return matches[0], nil
}

// ListRuns queries runs with optional filtering and pagination
func (s *Store) ListRuns(filter RunFilter) (*RunListResult, error) {
	// Apply defaults
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}

	// Build query
	baseWhere := "1=1"
	args := []interface{}{}

	if filter.Since != nil {
		baseWhere += " AND started_at >= ?"
		args = append(args, filter.Since.Unix())
	}
	if filter.Before != nil {
		baseWhere += " AND started_at < ?"
		args = append(args, filter.Before.Unix())
	}
	if filter.Status != "" {
		baseWhere += " AND status = ?"
		args = append(args, string(filter.Status))
	}
	if filter.RepoID != "" {
		baseWhere += " AND repo_id = ?"
		args = append(args, filter.RepoID)
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM runs WHERE %s", baseWhere)
	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("failed to count runs: %w", err)
	}

	// Get paginated results
	listQuery := fmt.Sprintf(`SELECT `+runColumns+` FROM runs WHERE %s ORDER BY started_at DESC LIMIT ? OFFSET ?`, baseWhere)
	args = append(args, filter.Limit, filter.Offset)

	rows, err := s.db.Query(listQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	runs := []Run{}
	for rows.Next() {
		run, err := s.scanRunFromRows(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *run)
	}

	result := &RunListResult{
		Runs: runs,
	}
	result.Pagination.Total = total
	result.Pagination.Limit = filter.Limit
	result.Pagination.Offset = filter.Offset

	return result, nil
}

// CreateAgent creates a new agent record.
// If an agent with the same ID already exists (e.g., retrying a failed task),
// the existing record is updated with the new values.
func (s *Store) CreateAgent(agent *Agent) error {
	query := `
		INSERT INTO agents (id, run_id, task_id, task_title, task_description, status, lifecycle_state, started_at, finished_at, duration_seconds, exit_code, error_message, stdout, stderr, input_tokens, output_tokens, total_tokens, cache_creation_tokens, cache_read_tokens, cost_usd, files_changed, git_commits_created, num_turns, result_message, repo_id, archived, parent_agent_id, merge_status, merge_commits_applied, merge_had_conflict, merge_resolver_spawned, merge_error, validation_status, validation_duration_ms, validation_error, validation_steps, session_id, attempt, max_retries)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			run_id = excluded.run_id,
			task_description = excluded.task_description,
			status = excluded.status,
			lifecycle_state = excluded.lifecycle_state,
			started_at = excluded.started_at,
			finished_at = excluded.finished_at,
			duration_seconds = excluded.duration_seconds,
			exit_code = excluded.exit_code,
			error_message = excluded.error_message,
			stdout = excluded.stdout,
			stderr = excluded.stderr,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			total_tokens = excluded.total_tokens,
			cache_creation_tokens = excluded.cache_creation_tokens,
			cache_read_tokens = excluded.cache_read_tokens,
			cost_usd = excluded.cost_usd,
			files_changed = excluded.files_changed,
			git_commits_created = excluded.git_commits_created,
			num_turns = excluded.num_turns,
			result_message = excluded.result_message,
			parent_agent_id = excluded.parent_agent_id,
			merge_status = excluded.merge_status,
			merge_commits_applied = excluded.merge_commits_applied,
			merge_had_conflict = excluded.merge_had_conflict,
			merge_resolver_spawned = excluded.merge_resolver_spawned,
			merge_error = excluded.merge_error,
			validation_status = excluded.validation_status,
			validation_duration_ms = excluded.validation_duration_ms,
			validation_error = excluded.validation_error,
			validation_steps = excluded.validation_steps,
			session_id = excluded.session_id,
			attempt = excluded.attempt,
			max_retries = excluded.max_retries
	`
	var finishedAt *int64
	if agent.FinishedAt != nil {
		ts := agent.FinishedAt.Unix()
		finishedAt = &ts
	}
	_, err := s.db.Exec(query,
		agent.ID,
		agent.RunID,
		agent.TaskID,
		agent.TaskTitle,
		nullString(agent.TaskDescription),
		string(agent.Status),
		nullString(agent.LifecycleState),
		agent.StartedAt.Unix(),
		finishedAt,
		agent.DurationSeconds,
		agent.ExitCode,
		agent.ErrorMessage,
		agent.Stdout,
		agent.Stderr,
		agent.InputTokens,
		agent.OutputTokens,
		agent.TotalTokens,
		agent.CacheCreationTokens,
		agent.CacheReadTokens,
		agent.CostUSD,
		agent.FilesChanged,
		agent.GitCommitsCreated,
		agent.NumTurns,
		agent.ResultMessage,
		nullString(agent.RepoID),
		boolToInt(agent.Archived),
		nullString(agent.ParentAgentID),
		nullString(string(agent.MergeStatus)),
		agent.MergeCommitsApplied,
		boolToInt(agent.MergeHadConflict),
		boolToInt(agent.MergeResolverSpawned),
		nullString(agent.MergeError),
		nullString(agent.ValidationStatus),
		agent.ValidationDuration,
		nullString(agent.ValidationError),
		nullString(agent.ValidationSteps),
		nullString(agent.SessionID),
		agent.Attempt,
		agent.MaxRetries,
	)
	return err
}

// boolToInt converts a bool to an int (0 or 1) for SQLite storage
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// UpdateAgent updates an existing agent record
func (s *Store) UpdateAgent(agent *Agent) error {
	query := `
		UPDATE agents SET
			status = ?,
			lifecycle_state = ?,
			finished_at = ?,
			duration_seconds = ?,
			exit_code = ?,
			error_message = ?,
			stdout = ?,
			stderr = ?,
			input_tokens = ?,
			output_tokens = ?,
			total_tokens = ?,
			cache_creation_tokens = ?,
			cache_read_tokens = ?,
			cost_usd = ?,
			files_changed = ?,
			git_commits_created = ?,
			num_turns = ?,
			result_message = ?,
			archived = ?,
			merge_status = ?,
			merge_commits_applied = ?,
			merge_had_conflict = ?,
			merge_resolver_spawned = ?,
			merge_error = ?,
			validation_status = ?,
			validation_duration_ms = ?,
			validation_error = ?,
			validation_steps = ?,
			repair_attempts = ?,
			last_repair_output = ?,
			session_id = ?
		WHERE id = ?
	`
	var finishedAt *int64
	if agent.FinishedAt != nil {
		ts := agent.FinishedAt.Unix()
		finishedAt = &ts
	}
	_, err := s.db.Exec(query,
		string(agent.Status),
		nullString(agent.LifecycleState),
		finishedAt,
		agent.DurationSeconds,
		agent.ExitCode,
		agent.ErrorMessage,
		agent.Stdout,
		agent.Stderr,
		agent.InputTokens,
		agent.OutputTokens,
		agent.TotalTokens,
		agent.CacheCreationTokens,
		agent.CacheReadTokens,
		agent.CostUSD,
		agent.FilesChanged,
		agent.GitCommitsCreated,
		agent.NumTurns,
		agent.ResultMessage,
		boolToInt(agent.Archived),
		nullString(string(agent.MergeStatus)),
		agent.MergeCommitsApplied,
		boolToInt(agent.MergeHadConflict),
		boolToInt(agent.MergeResolverSpawned),
		nullString(agent.MergeError),
		nullString(agent.ValidationStatus),
		agent.ValidationDuration,
		nullString(agent.ValidationError),
		nullString(agent.ValidationSteps),
		agent.RepairAttempts,
		nullString(agent.LastRepairOutput),
		nullString(agent.SessionID),
		agent.ID,
	)
	return err
}

// SetAgentArchived updates only the archived status of an agent
func (s *Store) SetAgentArchived(agentID string, archived bool) error {
	query := `UPDATE agents SET archived = ? WHERE id = ?`
	_, err := s.db.Exec(query, boolToInt(archived), agentID)
	return err
}

// UpdateAgentCompletion updates only the completion-related fields of an agent.
// This does NOT update merge/validation/repair fields, preserving any previously
// persisted merge status. Use this when handling agent completion events.
func (s *Store) UpdateAgentCompletion(agent *Agent) error {
	query := `
		UPDATE agents SET
			status = ?,
			lifecycle_state = ?,
			finished_at = ?,
			duration_seconds = ?,
			exit_code = ?,
			error_message = ?,
			stdout = ?,
			stderr = ?,
			input_tokens = ?,
			output_tokens = ?,
			total_tokens = ?,
			cache_creation_tokens = ?,
			cache_read_tokens = ?,
			cost_usd = ?,
			files_changed = ?,
			git_commits_created = ?,
			num_turns = ?,
			result_message = ?,
			session_id = ?
		WHERE id = ?
	`
	var finishedAt *int64
	if agent.FinishedAt != nil {
		ts := agent.FinishedAt.Unix()
		finishedAt = &ts
	}
	_, err := s.db.Exec(query,
		string(agent.Status),
		nullString(agent.LifecycleState),
		finishedAt,
		agent.DurationSeconds,
		agent.ExitCode,
		agent.ErrorMessage,
		agent.Stdout,
		agent.Stderr,
		agent.InputTokens,
		agent.OutputTokens,
		agent.TotalTokens,
		agent.CacheCreationTokens,
		agent.CacheReadTokens,
		agent.CostUSD,
		agent.FilesChanged,
		agent.GitCommitsCreated,
		agent.NumTurns,
		agent.ResultMessage,
		nullString(agent.SessionID),
		agent.ID,
	)
	return err
}

// UpdateAgentMergeResult updates only the merge result fields of an agent
func (s *Store) UpdateAgentMergeResult(agentID string, mergeStatus MergeStatus, commitsApplied int, hadConflict, resolverSpawned bool, mergeError string) error {
	query := `
		UPDATE agents SET
			merge_status = ?,
			merge_commits_applied = ?,
			merge_had_conflict = ?,
			merge_resolver_spawned = ?,
			merge_error = ?
		WHERE id = ?
	`
	_, err := s.db.Exec(query,
		nullString(string(mergeStatus)),
		commitsApplied,
		boolToInt(hadConflict),
		boolToInt(resolverSpawned),
		nullString(mergeError),
		agentID,
	)
	return err
}

// UpdateAgentValidationResult updates only the validation result fields of an agent
func (s *Store) UpdateAgentValidationResult(agentID string, validationStatus string, validationDuration int64, validationError string, validationSteps string) error {
	query := `
		UPDATE agents SET
			validation_status = ?,
			validation_duration_ms = ?,
			validation_error = ?,
			validation_steps = ?
		WHERE id = ?
	`
	_, err := s.db.Exec(query,
		nullString(validationStatus),
		validationDuration,
		nullString(validationError),
		nullString(validationSteps),
		agentID,
	)
	return err
}

// UpdateAgentRepairState updates only the repair-related fields of an agent
func (s *Store) UpdateAgentRepairState(agentID string, repairAttempts int, lastRepairOutput string, validationStatus string) error {
	query := `
		UPDATE agents SET
			repair_attempts = ?,
			last_repair_output = ?,
			validation_status = ?
		WHERE id = ?
	`
	_, err := s.db.Exec(query,
		repairAttempts,
		nullString(lastRepairOutput),
		nullString(validationStatus),
		agentID,
	)
	return err
}

// UpdateAgentLifecycleState updates only the lifecycle_state field of an agent.
// This is the primary method for persisting lifecycle state transitions.
func (s *Store) UpdateAgentLifecycleState(agentID string, lifecycleState string) error {
	query := `UPDATE agents SET lifecycle_state = ? WHERE id = ?`
	_, err := s.db.Exec(query, nullString(lifecycleState), agentID)
	return err
}

// agentColumns lists all columns for agent queries
const agentColumns = `id, run_id, task_id, task_title, task_description, status, lifecycle_state, started_at, finished_at, duration_seconds, exit_code, error_message, stdout, stderr, input_tokens, output_tokens, total_tokens, cache_creation_tokens, cache_read_tokens, cost_usd, files_changed, git_commits_created, num_turns, result_message, repo_id, archived, parent_agent_id, merge_status, merge_commits_applied, merge_had_conflict, merge_resolver_spawned, merge_error, validation_status, validation_duration_ms, validation_error, validation_steps, repair_attempts, last_repair_output, session_id, attempt, max_retries`

// agentColumnsWithPrefix returns the agent columns with a table alias prefix.
// This is used for queries with JOINs to disambiguate column names.
func agentColumnsWithPrefix(prefix string) string {
	cols := []string{
		"id", "run_id", "task_id", "task_title", "task_description", "status", "lifecycle_state",
		"started_at", "finished_at", "duration_seconds", "exit_code", "error_message", "stdout", "stderr",
		"input_tokens", "output_tokens", "total_tokens", "cache_creation_tokens",
		"cache_read_tokens", "cost_usd", "files_changed", "git_commits_created",
		"num_turns", "result_message", "repo_id", "archived", "parent_agent_id",
		"merge_status", "merge_commits_applied", "merge_had_conflict",
		"merge_resolver_spawned", "merge_error",
		"validation_status", "validation_duration_ms", "validation_error", "validation_steps",
		"repair_attempts", "last_repair_output", "session_id", "attempt", "max_retries",
	}
	result := make([]string, len(cols))
	for i, col := range cols {
		result[i] = prefix + col
	}
	return strings.Join(result, ", ")
}

// GetAgent retrieves an agent by ID
func (s *Store) GetAgent(id string) (*Agent, error) {
	query := `SELECT ` + agentColumns + ` FROM agents WHERE id = ?`
	row := s.db.QueryRow(query, id)
	return s.scanAgent(row)
}

// GetAgentsByRun retrieves all agents for a specific run
func (s *Store) GetAgentsByRun(runID string) ([]Agent, error) {
	query := `SELECT ` + agentColumns + ` FROM agents WHERE run_id = ? ORDER BY started_at ASC`
	rows, err := s.db.Query(query, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to query agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	agents := []Agent{}
	for rows.Next() {
		agent, err := s.scanAgentFromRows(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, *agent)
	}
	return agents, nil
}

// GetAllNonArchivedAgents retrieves all agents where archived = 0, across all runs.
// This is used to restore full agent history on daemon startup.
// Children of archived parents are also excluded (inherited archive status).
func (s *Store) GetAllNonArchivedAgents() ([]Agent, error) {
	// Use a LEFT JOIN to check if the parent agent is archived.
	// We exclude agents where:
	// - The agent itself is archived (a.archived = 1)
	// - The agent has a parent that is archived (p.archived = 1)
	// This implements inherited archive status - children of archived parents
	// are implicitly archived without needing to update the database.
	query := `SELECT ` + agentColumnsWithPrefix("a.") + `
		FROM agents a
		LEFT JOIN agents p ON a.parent_agent_id = p.id
		WHERE a.archived = 0
		  AND (a.parent_agent_id = '' OR a.parent_agent_id IS NULL OR p.archived = 0)
		ORDER BY a.started_at ASC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query non-archived agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	agents := []Agent{}
	for rows.Next() {
		agent, err := s.scanAgentFromRows(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, *agent)
	}
	return agents, nil
}

// GetAllAgents retrieves all agents across all runs, including archived ones.
// This is used to restore full agent history on daemon startup so that
// archived agents can be un-archived by the user.
func (s *Store) GetAllAgents() ([]Agent, error) {
	query := `SELECT ` + agentColumns + ` FROM agents ORDER BY started_at ASC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	agents := []Agent{}
	for rows.Next() {
		agent, err := s.scanAgentFromRows(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, *agent)
	}
	return agents, nil
}

// GetRunningRun returns the most recent run with status "running", or nil if none exists.
// This is used to restore state on daemon startup.
func (s *Store) GetRunningRun() (*Run, error) {
	query := `SELECT ` + runColumns + ` FROM runs WHERE status = 'running' ORDER BY started_at DESC LIMIT 1`
	row := s.db.QueryRow(query)
	return s.scanRun(row)
}

// GetMostRecentRun returns the most recent run regardless of status, or nil if none exists.
// This is used to restore historical state on daemon startup for display purposes.
func (s *Store) GetMostRecentRun() (*Run, error) {
	query := `SELECT ` + runColumns + ` FROM runs ORDER BY started_at DESC LIMIT 1`
	row := s.db.QueryRow(query)
	return s.scanRun(row)
}

// MarkOrphanedRunsFailed marks all runs with status "running" as "failed".
// This is used on daemon startup to handle runs that were interrupted by a crash.
// Returns the number of runs marked as failed.
func (s *Store) MarkOrphanedRunsFailed() (int64, error) {
	now := time.Now().Unix()
	query := `
		UPDATE runs SET
			status = 'failed',
			finished_at = ?
		WHERE status = 'running'
	`
	result, err := s.db.Exec(query, now)
	if err != nil {
		return 0, fmt.Errorf("failed to mark orphaned runs: %w", err)
	}
	return result.RowsAffected()
}

// MarkOrphanedAgentsFailed marks all agents with status "starting" or "running" as "failed",
// and also marks agents stuck in non-terminal merge-related lifecycle states as "failed".
// This handles agents interrupted by daemon crashes during merge, validation, or repair.
// Returns the number of agents marked as failed.
func (s *Store) MarkOrphanedAgentsFailed() (int64, error) {
	now := time.Now().Unix()
	query := `
		UPDATE agents SET
			status = 'failed',
			lifecycle_state = 'failed',
			finished_at = ?,
			error_message = 'daemon terminated unexpectedly'
		WHERE status IN ('starting', 'running')
		   OR lifecycle_state IN ('merging', 'queued_for_merge', 'validating', 'repairing', 'resolving')
	`
	result, err := s.db.Exec(query, now)
	if err != nil {
		return 0, fmt.Errorf("failed to mark orphaned agents: %w", err)
	}
	return result.RowsAffected()
}

// GetStats returns aggregate statistics, optionally filtered by time range
func (s *Store) GetStats(since *time.Time) (*AggregateStats, error) {
	// Query runs for run-level stats
	runsQuery := `
		SELECT
			COUNT(*) as total_runs,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as completed_runs,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed_runs,
			SUM(CASE WHEN status = 'partial' THEN 1 ELSE 0 END) as partial_runs,
			COALESCE(SUM(total_tasks), 0) as total_tasks,
			COALESCE(SUM(completed_tasks), 0) as completed_tasks,
			COALESCE(SUM(failed_tasks), 0) as failed_tasks,
			COALESCE(SUM(files_changed), 0) as files_changed,
			COALESCE(SUM(git_commits), 0) as git_commits
		FROM runs
	`
	args := []interface{}{}
	if since != nil {
		runsQuery += " WHERE started_at >= ?"
		args = append(args, since.Unix())
	}

	var stats AggregateStats
	err := s.db.QueryRow(runsQuery, args...).Scan(
		&stats.TotalRuns,
		&stats.CompletedRuns,
		&stats.FailedRuns,
		&stats.PartialRuns,
		&stats.TotalTasks,
		&stats.CompletedTasks,
		&stats.FailedTasks,
		&stats.FilesChanged,
		&stats.GitCommits,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query run stats: %w", err)
	}

	// Query agents for token/cost stats (agents have the authoritative data)
	agentsQuery := `
		SELECT
			COUNT(*) as total_agents,
			COALESCE(SUM(input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(output_tokens), 0) as total_output_tokens,
			COALESCE(SUM(total_tokens), 0) as total_tokens,
			COALESCE(SUM(cost_usd), 0) as total_cost_usd,
			COALESCE(SUM(duration_seconds), 0) as total_duration_seconds
		FROM agents
	`
	if since != nil {
		agentsQuery += " WHERE started_at >= ?"
	}

	err = s.db.QueryRow(agentsQuery, args...).Scan(
		&stats.TotalAgents,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalTokens,
		&stats.TotalCostUSD,
		&stats.TotalDurationSeconds,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent stats: %w", err)
	}

	// Query resolver stats from agents that had conflicts
	// merge_had_conflict=true indicates a conflict was detected
	// merge_status='merged' indicates successful resolution
	// merge_status='failed' indicates failed resolution
	resolverQuery := `
		SELECT
			COALESCE(SUM(CASE WHEN merge_had_conflict = 1 THEN 1 ELSE 0 END), 0) as total_conflicts,
			COALESCE(SUM(CASE WHEN merge_had_conflict = 1 AND merge_status = 'merged' THEN 1 ELSE 0 END), 0) as resolved_conflicts,
			COALESCE(SUM(CASE WHEN merge_had_conflict = 1 AND merge_status = 'failed' THEN 1 ELSE 0 END), 0) as failed_resolutions,
			COALESCE(AVG(CASE WHEN merge_had_conflict = 1 AND merge_status IN ('merged', 'failed') THEN duration_seconds ELSE NULL END), 0) as avg_resolution_seconds
		FROM agents
	`
	if since != nil {
		resolverQuery += " WHERE started_at >= ?"
	}

	err = s.db.QueryRow(resolverQuery, args...).Scan(
		&stats.TotalConflicts,
		&stats.ResolvedConflicts,
		&stats.FailedResolutions,
		&stats.AvgResolutionSeconds,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query resolver stats: %w", err)
	}

	return &stats, nil
}

// GetRunsByRepo retrieves all runs for a specific repository
func (s *Store) GetRunsByRepo(repoID string) ([]Run, error) {
	query := `SELECT ` + runColumns + ` FROM runs WHERE repo_id = ? ORDER BY started_at DESC`
	rows, err := s.db.Query(query, repoID)
	if err != nil {
		return nil, fmt.Errorf("failed to query runs by repo: %w", err)
	}
	defer func() { _ = rows.Close() }()

	runs := []Run{}
	for rows.Next() {
		run, err := s.scanRunFromRows(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *run)
	}
	return runs, nil
}

// GetStatsByRepo returns aggregate statistics for a specific repository, optionally filtered by time range
func (s *Store) GetStatsByRepo(repoID string, since *time.Time) (*AggregateStats, error) {
	// Query runs for run-level stats
	runsQuery := `
		SELECT
			COUNT(*) as total_runs,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as completed_runs,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed_runs,
			SUM(CASE WHEN status = 'partial' THEN 1 ELSE 0 END) as partial_runs,
			COALESCE(SUM(total_tasks), 0) as total_tasks,
			COALESCE(SUM(completed_tasks), 0) as completed_tasks,
			COALESCE(SUM(failed_tasks), 0) as failed_tasks,
			COALESCE(SUM(files_changed), 0) as files_changed,
			COALESCE(SUM(git_commits), 0) as git_commits
		FROM runs
		WHERE repo_id = ?
	`
	args := []interface{}{repoID}
	if since != nil {
		runsQuery += " AND started_at >= ?"
		args = append(args, since.Unix())
	}

	var stats AggregateStats
	err := s.db.QueryRow(runsQuery, args...).Scan(
		&stats.TotalRuns,
		&stats.CompletedRuns,
		&stats.FailedRuns,
		&stats.PartialRuns,
		&stats.TotalTasks,
		&stats.CompletedTasks,
		&stats.FailedTasks,
		&stats.FilesChanged,
		&stats.GitCommits,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query run stats by repo: %w", err)
	}

	// Query agents for token/cost stats (agents have the authoritative data)
	agentsQuery := `
		SELECT
			COUNT(*) as total_agents,
			COALESCE(SUM(input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(output_tokens), 0) as total_output_tokens,
			COALESCE(SUM(total_tokens), 0) as total_tokens,
			COALESCE(SUM(cost_usd), 0) as total_cost_usd,
			COALESCE(SUM(duration_seconds), 0) as total_duration_seconds
		FROM agents
		WHERE repo_id = ?
	`
	if since != nil {
		agentsQuery += " AND started_at >= ?"
	}

	err = s.db.QueryRow(agentsQuery, args...).Scan(
		&stats.TotalAgents,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalTokens,
		&stats.TotalCostUSD,
		&stats.TotalDurationSeconds,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent stats by repo: %w", err)
	}

	// Query resolver stats from agents that had conflicts
	resolverQuery := `
		SELECT
			COALESCE(SUM(CASE WHEN merge_had_conflict = 1 THEN 1 ELSE 0 END), 0) as total_conflicts,
			COALESCE(SUM(CASE WHEN merge_had_conflict = 1 AND merge_status = 'merged' THEN 1 ELSE 0 END), 0) as resolved_conflicts,
			COALESCE(SUM(CASE WHEN merge_had_conflict = 1 AND merge_status = 'failed' THEN 1 ELSE 0 END), 0) as failed_resolutions,
			COALESCE(AVG(CASE WHEN merge_had_conflict = 1 AND merge_status IN ('merged', 'failed') THEN duration_seconds ELSE NULL END), 0) as avg_resolution_seconds
		FROM agents
		WHERE repo_id = ?
	`
	if since != nil {
		resolverQuery += " AND started_at >= ?"
	}

	err = s.db.QueryRow(resolverQuery, args...).Scan(
		&stats.TotalConflicts,
		&stats.ResolvedConflicts,
		&stats.FailedResolutions,
		&stats.AvgResolutionSeconds,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query resolver stats by repo: %w", err)
	}

	return &stats, nil
}

// RepoIDLookupFunc is a callback function used by MigrateOrphanedRepoIDs to look up
// repository IDs from paths. Returns (repoID, found).
type RepoIDLookupFunc func(path string) (string, bool)

// DeleteRun deletes a run and all its associated agents by run ID.
// Returns an error if the run does not exist.
func (s *Store) DeleteRun(runID string) error {
	// First check if the run exists
	run, err := s.GetRun(runID)
	if err != nil {
		return err
	}
	if run == nil {
		return fmt.Errorf("run not found: %s", runID)
	}

	// Delete in a transaction for atomicity
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Delete associated agents first
	if _, err := tx.Exec("DELETE FROM agents WHERE run_id = ?", runID); err != nil {
		return fmt.Errorf("failed to delete agents: %w", err)
	}

	// Delete the run
	if _, err := tx.Exec("DELETE FROM runs WHERE id = ?", runID); err != nil {
		return fmt.Errorf("failed to delete run: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// MigrateOrphanedRepoIDs attempts to populate repo_id for runs that have NULL repo_id.
// It uses the provided lookup function to find repository IDs from paths.
// Returns the number of runs updated.
// This operation runs in a transaction to ensure atomicity.
func (s *Store) MigrateOrphanedRepoIDs(lookupFn RepoIDLookupFunc) (int64, error) {
	// Get runs with NULL repo_id that have a repo_path set
	query := `SELECT id, repo_path FROM runs WHERE repo_id IS NULL AND repo_path IS NOT NULL AND repo_path != ''`
	rows, err := s.db.Query(query)
	if err != nil {
		return 0, fmt.Errorf("failed to query orphaned runs: %w", err)
	}

	// Collect all data first to avoid holding open cursor during updates
	type orphanedRun struct {
		runID    string
		repoPath string
		repoID   string
	}
	var orphanedRuns []orphanedRun
	for rows.Next() {
		var runID, repoPath string
		if err := rows.Scan(&runID, &repoPath); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("failed to scan orphaned run: %w", err)
		}

		// Look up repo ID for this path
		repoID, found := lookupFn(repoPath)
		if found {
			orphanedRuns = append(orphanedRuns, orphanedRun{
				runID:    runID,
				repoPath: repoPath,
				repoID:   repoID,
			})
		}
	}
	_ = rows.Close()

	if len(orphanedRuns) == 0 {
		return 0, nil
	}

	// Begin transaction for atomic updates
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Use bulk updates to avoid N+1 queries
	// Group runs by repo_id for efficient updates
	repoIDToRunIDs := make(map[string][]string)
	for _, run := range orphanedRuns {
		repoIDToRunIDs[run.repoID] = append(repoIDToRunIDs[run.repoID], run.runID)
	}

	var updated int64
	for repoID, runIDs := range repoIDToRunIDs {
		// Build IN clause for bulk update
		placeholders := make([]string, len(runIDs))
		args := make([]interface{}, 0, len(runIDs)+1)
		args = append(args, repoID)

		for i, runID := range runIDs {
			placeholders[i] = "?"
			args = append(args, runID)
		}

		inClause := "(" + strings.Join(placeholders, ",") + ")"

		// Bulk update runs
		runQuery := fmt.Sprintf("UPDATE runs SET repo_id = ? WHERE id IN %s", inClause)
		result, err := tx.Exec(runQuery, args...)
		if err != nil {
			return 0, fmt.Errorf("failed to bulk update run repo_ids: %w", err)
		}

		rowsAffected, _ := result.RowsAffected()
		updated += rowsAffected

		// Bulk update associated agents
		agentQuery := fmt.Sprintf("UPDATE agents SET repo_id = ? WHERE run_id IN %s", inClause)
		_, err = tx.Exec(agentQuery, args...)
		if err != nil {
			return 0, fmt.Errorf("failed to bulk update agent repo_ids: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return updated, nil
}

// Helper functions for scanning rows

func (s *Store) scanRun(row *sql.Row) (*Run, error) {
	var run Run
	var startedAt, finishedAt sql.NullInt64
	var status string
	var repoID, repoPath, repoName sql.NullString

	err := row.Scan(
		&run.ID,
		&startedAt,
		&finishedAt,
		&status,
		&run.Concurrency,
		&run.GitBranch,
		&run.GitCommit,
		&run.TotalTasks,
		&run.CompletedTasks,
		&run.FailedTasks,
		&repoID,
		&repoPath,
		&repoName,
		&run.TotalInputTokens,
		&run.TotalOutputTokens,
		&run.CacheCreationTokens,
		&run.CacheReadTokens,
		&run.TotalCostUSD,
		&run.TotalTurns,
		&run.FilesChanged,
		&run.GitCommits,
		&run.DurationSeconds,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan run: %w", err)
	}

	run.Status = RunStatus(status)
	run.StartedAt = time.Unix(startedAt.Int64, 0)
	if finishedAt.Valid {
		t := time.Unix(finishedAt.Int64, 0)
		run.FinishedAt = &t
	}
	run.RepoID = repoID.String
	run.RepoPath = repoPath.String
	run.RepoName = repoName.String

	return &run, nil
}

func (s *Store) scanRunFromRows(rows *sql.Rows) (*Run, error) {
	var run Run
	var startedAt, finishedAt sql.NullInt64
	var status string
	var repoID, repoPath, repoName sql.NullString

	err := rows.Scan(
		&run.ID,
		&startedAt,
		&finishedAt,
		&status,
		&run.Concurrency,
		&run.GitBranch,
		&run.GitCommit,
		&run.TotalTasks,
		&run.CompletedTasks,
		&run.FailedTasks,
		&repoID,
		&repoPath,
		&repoName,
		&run.TotalInputTokens,
		&run.TotalOutputTokens,
		&run.CacheCreationTokens,
		&run.CacheReadTokens,
		&run.TotalCostUSD,
		&run.TotalTurns,
		&run.FilesChanged,
		&run.GitCommits,
		&run.DurationSeconds,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan run: %w", err)
	}

	run.Status = RunStatus(status)
	run.StartedAt = time.Unix(startedAt.Int64, 0)
	if finishedAt.Valid {
		t := time.Unix(finishedAt.Int64, 0)
		run.FinishedAt = &t
	}
	run.RepoID = repoID.String
	run.RepoPath = repoPath.String
	run.RepoName = repoName.String

	return &run, nil
}

func (s *Store) scanAgent(row *sql.Row) (*Agent, error) {
	var agent Agent
	var startedAt, finishedAt sql.NullInt64
	var exitCode sql.NullInt64
	var status string
	var lifecycleState sql.NullString
	var taskDescription sql.NullString
	var errorMessage, stdout, stderr, resultMessage, repoID, parentAgentID sql.NullString
	var archived sql.NullInt64
	var mergeStatus, mergeError sql.NullString
	var mergeCommitsApplied, mergeHadConflict, mergeResolverSpawned sql.NullInt64
	var validationStatus, validationError, validationSteps sql.NullString
	var validationDuration sql.NullInt64
	var repairAttempts sql.NullInt64
	var lastRepairOutput sql.NullString
	var sessionID sql.NullString
	var attempt, maxRetries sql.NullInt64

	err := row.Scan(
		&agent.ID,
		&agent.RunID,
		&agent.TaskID,
		&agent.TaskTitle,
		&taskDescription,
		&status,
		&lifecycleState,
		&startedAt,
		&finishedAt,
		&agent.DurationSeconds,
		&exitCode,
		&errorMessage,
		&stdout,
		&stderr,
		&agent.InputTokens,
		&agent.OutputTokens,
		&agent.TotalTokens,
		&agent.CacheCreationTokens,
		&agent.CacheReadTokens,
		&agent.CostUSD,
		&agent.FilesChanged,
		&agent.GitCommitsCreated,
		&agent.NumTurns,
		&resultMessage,
		&repoID,
		&archived,
		&parentAgentID,
		&mergeStatus,
		&mergeCommitsApplied,
		&mergeHadConflict,
		&mergeResolverSpawned,
		&mergeError,
		&validationStatus,
		&validationDuration,
		&validationError,
		&validationSteps,
		&repairAttempts,
		&lastRepairOutput,
		&sessionID,
		&attempt,
		&maxRetries,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan agent: %w", err)
	}

	agent.Status = AgentStatus(status)
	agent.LifecycleState = lifecycleState.String
	agent.TaskDescription = taskDescription.String
	agent.StartedAt = time.Unix(startedAt.Int64, 0)
	if finishedAt.Valid {
		t := time.Unix(finishedAt.Int64, 0)
		agent.FinishedAt = &t
	}
	if exitCode.Valid {
		ec := int(exitCode.Int64)
		agent.ExitCode = &ec
	}
	agent.ErrorMessage = errorMessage.String
	agent.Stdout = stdout.String
	agent.Stderr = stderr.String
	agent.ResultMessage = resultMessage.String
	agent.RepoID = repoID.String
	agent.Archived = archived.Valid && archived.Int64 == 1
	agent.ParentAgentID = parentAgentID.String
	agent.MergeStatus = MergeStatus(mergeStatus.String)
	agent.MergeCommitsApplied = int(mergeCommitsApplied.Int64)
	agent.MergeHadConflict = mergeHadConflict.Valid && mergeHadConflict.Int64 == 1
	agent.MergeResolverSpawned = mergeResolverSpawned.Valid && mergeResolverSpawned.Int64 == 1
	agent.MergeError = mergeError.String
	agent.ValidationStatus = validationStatus.String
	agent.ValidationDuration = validationDuration.Int64
	agent.ValidationError = validationError.String
	agent.ValidationSteps = validationSteps.String
	agent.RepairAttempts = int(repairAttempts.Int64)
	agent.LastRepairOutput = lastRepairOutput.String
	agent.SessionID = sessionID.String
	agent.Attempt = int(attempt.Int64)
	agent.MaxRetries = int(maxRetries.Int64)

	return &agent, nil
}

func (s *Store) scanAgentFromRows(rows *sql.Rows) (*Agent, error) {
	var agent Agent
	var startedAt, finishedAt sql.NullInt64
	var exitCode sql.NullInt64
	var status string
	var lifecycleState sql.NullString
	var taskDescription sql.NullString
	var errorMessage, stdout, stderr, resultMessage, repoID, parentAgentID sql.NullString
	var archived sql.NullInt64
	var mergeStatus, mergeError sql.NullString
	var mergeCommitsApplied, mergeHadConflict, mergeResolverSpawned sql.NullInt64
	var validationStatus, validationError, validationSteps sql.NullString
	var validationDuration sql.NullInt64
	var repairAttempts sql.NullInt64
	var lastRepairOutput sql.NullString
	var sessionID sql.NullString
	var attempt, maxRetries sql.NullInt64

	err := rows.Scan(
		&agent.ID,
		&agent.RunID,
		&agent.TaskID,
		&agent.TaskTitle,
		&taskDescription,
		&status,
		&lifecycleState,
		&startedAt,
		&finishedAt,
		&agent.DurationSeconds,
		&exitCode,
		&errorMessage,
		&stdout,
		&stderr,
		&agent.InputTokens,
		&agent.OutputTokens,
		&agent.TotalTokens,
		&agent.CacheCreationTokens,
		&agent.CacheReadTokens,
		&agent.CostUSD,
		&agent.FilesChanged,
		&agent.GitCommitsCreated,
		&agent.NumTurns,
		&resultMessage,
		&repoID,
		&archived,
		&parentAgentID,
		&mergeStatus,
		&mergeCommitsApplied,
		&mergeHadConflict,
		&mergeResolverSpawned,
		&mergeError,
		&validationStatus,
		&validationDuration,
		&validationError,
		&validationSteps,
		&repairAttempts,
		&lastRepairOutput,
		&sessionID,
		&attempt,
		&maxRetries,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan agent: %w", err)
	}

	agent.Status = AgentStatus(status)
	agent.LifecycleState = lifecycleState.String
	agent.TaskDescription = taskDescription.String
	agent.StartedAt = time.Unix(startedAt.Int64, 0)
	if finishedAt.Valid {
		t := time.Unix(finishedAt.Int64, 0)
		agent.FinishedAt = &t
	}
	if exitCode.Valid {
		ec := int(exitCode.Int64)
		agent.ExitCode = &ec
	}
	agent.ErrorMessage = errorMessage.String
	agent.Stdout = stdout.String
	agent.Stderr = stderr.String
	agent.ResultMessage = resultMessage.String
	agent.Archived = archived.Valid && archived.Int64 == 1
	agent.RepoID = repoID.String
	agent.ParentAgentID = parentAgentID.String
	agent.MergeStatus = MergeStatus(mergeStatus.String)
	agent.MergeCommitsApplied = int(mergeCommitsApplied.Int64)
	agent.MergeHadConflict = mergeHadConflict.Valid && mergeHadConflict.Int64 == 1
	agent.MergeResolverSpawned = mergeResolverSpawned.Valid && mergeResolverSpawned.Int64 == 1
	agent.MergeError = mergeError.String
	agent.ValidationStatus = validationStatus.String
	agent.ValidationDuration = validationDuration.Int64
	agent.ValidationError = validationError.String
	agent.ValidationSteps = validationSteps.String
	agent.RepairAttempts = int(repairAttempts.Int64)
	agent.LastRepairOutput = lastRepairOutput.String
	agent.SessionID = sessionID.String
	agent.Attempt = int(attempt.Int64)
	agent.MaxRetries = int(maxRetries.Int64)

	return &agent, nil
}

// taskColumns lists all columns for task queries
const taskColumns = `id, repo_id, title, status, type, priority, agent_id, updated_at`

// UpsertTask creates or updates a task record
func (s *Store) UpsertTask(task *Task) error {
	query := `
		INSERT INTO tasks (id, repo_id, title, status, type, priority, agent_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, strftime('%s', 'now'))
		ON CONFLICT(id) DO UPDATE SET
			repo_id = excluded.repo_id,
			title = excluded.title,
			status = excluded.status,
			type = excluded.type,
			priority = excluded.priority,
			agent_id = excluded.agent_id,
			updated_at = strftime('%s', 'now')
	`
	_, err := s.db.Exec(query,
		task.ID,
		nullString(task.RepoID),
		task.Title,
		task.Status,
		nullString(task.Type),
		task.Priority,
		nullString(task.AgentID),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert task: %w", err)
	}
	return nil
}

// GetTask retrieves a task by ID
func (s *Store) GetTask(id string) (*Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE id = ?`
	row := s.db.QueryRow(query, id)
	return s.scanTask(row)
}

// GetTasks retrieves all tasks for a specific repository
func (s *Store) GetTasks(repoID string) ([]Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE repo_id = ? ORDER BY priority ASC, updated_at DESC`
	rows, err := s.db.Query(query, repoID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tasks := []Task{}
	for rows.Next() {
		task, err := s.scanTaskFromRows(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *task)
	}
	return tasks, nil
}

// GetTasksByStatus retrieves all tasks with a specific status
func (s *Store) GetTasksByStatus(status string) ([]Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE status = ? ORDER BY priority ASC, updated_at DESC`
	rows, err := s.db.Query(query, status)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks by status: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tasks := []Task{}
	for rows.Next() {
		task, err := s.scanTaskFromRows(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *task)
	}
	return tasks, nil
}

// GetAllTasks retrieves all tasks across all repositories
func (s *Store) GetAllTasks() ([]Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks ORDER BY priority ASC, updated_at DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tasks := []Task{}
	for rows.Next() {
		task, err := s.scanTaskFromRows(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *task)
	}
	return tasks, nil
}

// DeleteTask deletes a task by ID
func (s *Store) DeleteTask(id string) error {
	_, err := s.db.Exec("DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}
	return nil
}

// DeleteTasksByRepo deletes all tasks for a specific repository
func (s *Store) DeleteTasksByRepo(repoID string) error {
	_, err := s.db.Exec("DELETE FROM tasks WHERE repo_id = ?", repoID)
	if err != nil {
		return fmt.Errorf("failed to delete tasks by repo: %w", err)
	}
	return nil
}

func (s *Store) scanTask(row *sql.Row) (*Task, error) {
	var task Task
	var repoID, taskType, agentID sql.NullString
	var updatedAt sql.NullInt64

	err := row.Scan(
		&task.ID,
		&repoID,
		&task.Title,
		&task.Status,
		&taskType,
		&task.Priority,
		&agentID,
		&updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan task: %w", err)
	}

	task.RepoID = repoID.String
	task.Type = taskType.String
	task.AgentID = agentID.String
	task.UpdatedAt = updatedAt.Int64

	return &task, nil
}

func (s *Store) scanTaskFromRows(rows *sql.Rows) (*Task, error) {
	var task Task
	var repoID, taskType, agentID sql.NullString
	var updatedAt sql.NullInt64

	err := rows.Scan(
		&task.ID,
		&repoID,
		&task.Title,
		&task.Status,
		&taskType,
		&task.Priority,
		&agentID,
		&updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan task: %w", err)
	}

	task.RepoID = repoID.String
	task.Type = taskType.String
	task.AgentID = agentID.String
	task.UpdatedAt = updatedAt.Int64

	return &task, nil
}

// TrackOverlay records an active overlay filesystem for potential recovery
func (s *Store) TrackOverlay(overlay *ActiveOverlay) error {
	query := `
		INSERT INTO active_overlays (agent_id, task_id, run_id, session_id, upper_dir, merged_dir, lower_dir, work_dir, created_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			task_id = excluded.task_id,
			run_id = excluded.run_id,
			session_id = excluded.session_id,
			upper_dir = excluded.upper_dir,
			merged_dir = excluded.merged_dir,
			lower_dir = excluded.lower_dir,
			work_dir = excluded.work_dir,
			created_at = excluded.created_at,
			status = excluded.status
	`
	_, err := s.db.Exec(query,
		overlay.AgentID,
		overlay.TaskID,
		overlay.RunID,
		nullString(overlay.SessionID),
		overlay.UpperDir,
		overlay.MergedDir,
		overlay.LowerDir,
		overlay.WorkDir,
		overlay.CreatedAt,
		overlay.Status,
	)
	if err != nil {
		return fmt.Errorf("failed to track overlay: %w", err)
	}
	return nil
}

// UpdateOverlaySessionID updates the session_id for an active overlay
func (s *Store) UpdateOverlaySessionID(agentID, sessionID string) error {
	query := `UPDATE active_overlays SET session_id = ? WHERE agent_id = ?`
	_, err := s.db.Exec(query, nullString(sessionID), agentID)
	if err != nil {
		return fmt.Errorf("failed to update overlay session_id: %w", err)
	}
	return nil
}

// MarkOverlayCompleted marks an overlay as completed (ready for cleanup)
func (s *Store) MarkOverlayCompleted(agentID string) error {
	query := `UPDATE active_overlays SET status = 'completed' WHERE agent_id = ?`
	_, err := s.db.Exec(query, agentID)
	if err != nil {
		return fmt.Errorf("failed to mark overlay completed: %w", err)
	}
	return nil
}

// UpdateOverlayStatus updates the status of an overlay (active, completed, orphaned)
func (s *Store) UpdateOverlayStatus(agentID, status string) error {
	query := `UPDATE active_overlays SET status = ? WHERE agent_id = ?`
	_, err := s.db.Exec(query, status, agentID)
	if err != nil {
		return fmt.Errorf("failed to update overlay status: %w", err)
	}
	return nil
}

// GetActiveOverlays retrieves all overlays with status 'active'
func (s *Store) GetActiveOverlays() ([]*ActiveOverlay, error) {
	query := `SELECT agent_id, task_id, run_id, session_id, upper_dir, merged_dir, lower_dir, work_dir, created_at, status FROM active_overlays WHERE status = 'active'`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query active overlays: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var overlays []*ActiveOverlay
	for rows.Next() {
		overlay, err := s.scanOverlayFromRows(rows)
		if err != nil {
			return nil, err
		}
		overlays = append(overlays, overlay)
	}
	return overlays, nil
}

// GetOrphanedOverlays retrieves all overlays with status 'orphaned'
func (s *Store) GetOrphanedOverlays() ([]*ActiveOverlay, error) {
	query := `SELECT agent_id, task_id, run_id, session_id, upper_dir, merged_dir, lower_dir, work_dir, created_at, status FROM active_overlays WHERE status = 'orphaned'`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query orphaned overlays: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var overlays []*ActiveOverlay
	for rows.Next() {
		overlay, err := s.scanOverlayFromRows(rows)
		if err != nil {
			return nil, err
		}
		overlays = append(overlays, overlay)
	}
	return overlays, nil
}

// MarkOverlaysOrphaned marks all active overlays as orphaned (for daemon restart recovery)
func (s *Store) MarkOverlaysOrphaned() (int64, error) {
	query := `UPDATE active_overlays SET status = 'orphaned' WHERE status = 'active'`
	result, err := s.db.Exec(query)
	if err != nil {
		return 0, fmt.Errorf("failed to mark overlays orphaned: %w", err)
	}
	return result.RowsAffected()
}

// DeleteOverlay removes an overlay record from the database
func (s *Store) DeleteOverlay(agentID string) error {
	query := `DELETE FROM active_overlays WHERE agent_id = ?`
	_, err := s.db.Exec(query, agentID)
	if err != nil {
		return fmt.Errorf("failed to delete overlay: %w", err)
	}
	return nil
}

// DeleteCompletedOverlays removes all overlay records with status 'completed'
func (s *Store) DeleteCompletedOverlays() (int64, error) {
	query := `DELETE FROM active_overlays WHERE status = 'completed'`
	result, err := s.db.Exec(query)
	if err != nil {
		return 0, fmt.Errorf("failed to delete completed overlays: %w", err)
	}
	return result.RowsAffected()
}

func (s *Store) scanOverlayFromRows(rows *sql.Rows) (*ActiveOverlay, error) {
	var overlay ActiveOverlay
	var sessionID sql.NullString

	err := rows.Scan(
		&overlay.AgentID,
		&overlay.TaskID,
		&overlay.RunID,
		&sessionID,
		&overlay.UpperDir,
		&overlay.MergedDir,
		&overlay.LowerDir,
		&overlay.WorkDir,
		&overlay.CreatedAt,
		&overlay.Status,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan overlay: %w", err)
	}

	overlay.SessionID = sessionID.String
	return &overlay, nil
}

// DefaultRunConfig returns a RunConfig with default values
func DefaultRunConfig(repoID string) *RunConfig {
	return &RunConfig{
		RepoID:      repoID,
		Concurrency: 4,
		PriorityMax: 4,
		UseBwrap:    true,
		MaxRetries:  3,
	}
}

// GetRunConfig retrieves the run configuration for a repository.
// Returns a default configuration if no record exists for the repo.
func (s *Store) GetRunConfig(repoID string) (*RunConfig, error) {
	query := `SELECT repo_id, concurrency, max_priority, use_bwrap, max_retries, updated_at FROM run_configs WHERE repo_id = ?`
	row := s.db.QueryRow(query, repoID)

	var config RunConfig
	var useBwrap int
	var updatedAt sql.NullInt64

	err := row.Scan(
		&config.RepoID,
		&config.Concurrency,
		&config.PriorityMax,
		&useBwrap,
		&config.MaxRetries,
		&updatedAt,
	)
	if err == sql.ErrNoRows {
		// Return default config if not found
		return DefaultRunConfig(repoID), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get run config: %w", err)
	}

	config.UseBwrap = useBwrap == 1
	config.UpdatedAt = updatedAt.Int64

	return &config, nil
}

// SaveRunConfig creates or updates the run configuration for a repository.
// Uses upsert pattern to handle both new and existing configurations.
func (s *Store) SaveRunConfig(repoID string, config *RunConfig) error {
	query := `
		INSERT INTO run_configs (repo_id, concurrency, max_priority, use_bwrap, max_retries, updated_at)
		VALUES (?, ?, ?, ?, ?, strftime('%s', 'now'))
		ON CONFLICT(repo_id) DO UPDATE SET
			concurrency = excluded.concurrency,
			max_priority = excluded.max_priority,
			use_bwrap = excluded.use_bwrap,
			max_retries = excluded.max_retries,
			updated_at = excluded.updated_at
	`
	_, err := s.db.Exec(query,
		repoID,
		config.Concurrency,
		config.PriorityMax,
		boolToInt(config.UseBwrap),
		config.MaxRetries,
	)
	if err != nil {
		return fmt.Errorf("failed to save run config: %w", err)
	}
	return nil
}

// CreateAgentCommit persists a git commit made by an agent.
// The files_changed field is stored as a comma-separated string.
func (s *Store) CreateAgentCommit(commit *AgentCommit) error {
	query := `
		INSERT INTO agent_commits (agent_id, hash, short_hash, message, author, author_email, timestamp, files_changed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	filesChanged := strings.Join(commit.FilesChanged, ",")
	_, err := s.db.Exec(query,
		commit.AgentID,
		commit.Hash,
		commit.ShortHash,
		commit.Message,
		nullString(commit.Author),
		nullString(commit.AuthorEmail),
		nullString(commit.Timestamp),
		nullString(filesChanged),
	)
	if err != nil {
		return fmt.Errorf("failed to create agent commit: %w", err)
	}
	return nil
}

// GetAgentCommits retrieves all commits for a specific agent, ordered by creation time.
func (s *Store) GetAgentCommits(agentID string) ([]AgentCommit, error) {
	query := `SELECT id, agent_id, hash, short_hash, message, author, author_email, timestamp, files_changed, created_at
		FROM agent_commits WHERE agent_id = ? ORDER BY created_at ASC`
	rows, err := s.db.Query(query, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent commits: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var commits []AgentCommit
	for rows.Next() {
		commit, err := s.scanAgentCommitFromRows(rows)
		if err != nil {
			return nil, err
		}
		commits = append(commits, *commit)
	}
	return commits, nil
}

// GetAllAgentCommits retrieves all commits for multiple agents at once.
// Returns a map of agent_id -> []AgentCommit for efficient bulk loading.
func (s *Store) GetAllAgentCommits(agentIDs []string) (map[string][]AgentCommit, error) {
	if len(agentIDs) == 0 {
		return make(map[string][]AgentCommit), nil
	}

	// Build IN clause
	placeholders := make([]string, len(agentIDs))
	args := make([]interface{}, len(agentIDs))
	for i, id := range agentIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`SELECT id, agent_id, hash, short_hash, message, author, author_email, timestamp, files_changed, created_at
		FROM agent_commits WHERE agent_id IN (%s) ORDER BY created_at ASC`,
		strings.Join(placeholders, ","))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent commits: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]AgentCommit)
	for rows.Next() {
		commit, err := s.scanAgentCommitFromRows(rows)
		if err != nil {
			return nil, err
		}
		result[commit.AgentID] = append(result[commit.AgentID], *commit)
	}
	return result, nil
}

func (s *Store) scanAgentCommitFromRows(rows *sql.Rows) (*AgentCommit, error) {
	var commit AgentCommit
	var author, authorEmail, timestamp, filesChanged sql.NullString
	var createdAt sql.NullInt64

	err := rows.Scan(
		&commit.ID,
		&commit.AgentID,
		&commit.Hash,
		&commit.ShortHash,
		&commit.Message,
		&author,
		&authorEmail,
		&timestamp,
		&filesChanged,
		&createdAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan agent commit: %w", err)
	}

	commit.Author = author.String
	commit.AuthorEmail = authorEmail.String
	commit.Timestamp = timestamp.String
	commit.CreatedAt = createdAt.Int64

	// Parse comma-separated files_changed
	if filesChanged.Valid && filesChanged.String != "" {
		commit.FilesChanged = strings.Split(filesChanged.String, ",")
	}

	return &commit, nil
}
