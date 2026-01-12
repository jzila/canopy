package persistence

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
}

// Agent represents a single agent execution within a run
type Agent struct {
	ID                string      `json:"id"`
	RunID             string      `json:"run_id"`
	TaskID            string      `json:"task_id"`
	TaskTitle         string      `json:"task_title"`
	Status            AgentStatus `json:"status"`
	StartedAt         time.Time   `json:"started_at"`
	FinishedAt        *time.Time  `json:"finished_at,omitempty"`
	DurationSeconds   float64     `json:"duration_seconds,omitempty"`
	ExitCode          *int        `json:"exit_code,omitempty"`
	ErrorMessage      string      `json:"error_message,omitempty"`
	Stdout            string      `json:"stdout,omitempty"`
	Stderr            string      `json:"stderr,omitempty"`
	InputTokens       int         `json:"input_tokens"`
	OutputTokens      int         `json:"output_tokens"`
	TotalTokens       int         `json:"total_tokens"`
	CostUSD           float64     `json:"cost_usd"`
	FilesChanged      int         `json:"files_changed"`
	GitCommitsCreated int         `json:"git_commits_created"`
	RepoID            string      `json:"repo_id,omitempty"`
	Archived          bool        `json:"archived"`
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
	TotalRuns      int     `json:"total_runs"`
	CompletedRuns  int     `json:"completed_runs"`
	FailedRuns     int     `json:"failed_runs"`
	TotalAgents    int     `json:"total_agents"`
	TotalTokens    int     `json:"total_tokens"`
	TotalInputTokens  int  `json:"total_input_tokens"`
	TotalOutputTokens int  `json:"total_output_tokens"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	TotalDurationSeconds float64 `json:"total_duration_seconds"`
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

	// Configure connection pool
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)

	store := &Store{
		db:     db,
		dbPath: dbPath,
	}

	// Run migrations
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return store, nil
}

// Close closes the database connection
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
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
		INSERT INTO runs (id, started_at, finished_at, status, concurrency, git_branch, git_commit, total_tasks, completed_tasks, failed_tasks, repo_id, repo_path, repo_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			repo_name = ?
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
		run.ID,
	)
	return err
}

// GetRun retrieves a run by ID
func (s *Store) GetRun(id string) (*Run, error) {
	query := `
		SELECT id, started_at, finished_at, status, concurrency, git_branch, git_commit, total_tasks, completed_tasks, failed_tasks, repo_id, repo_path, repo_name
		FROM runs WHERE id = ?
	`
	row := s.db.QueryRow(query, id)
	return s.scanRun(row)
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
	listQuery := fmt.Sprintf(`
		SELECT id, started_at, finished_at, status, concurrency, git_branch, git_commit, total_tasks, completed_tasks, failed_tasks, repo_id, repo_path, repo_name
		FROM runs WHERE %s
		ORDER BY started_at DESC
		LIMIT ? OFFSET ?
	`, baseWhere)
	args = append(args, filter.Limit, filter.Offset)

	rows, err := s.db.Query(listQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query runs: %w", err)
	}
	defer rows.Close()

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

// CreateAgent creates a new agent record
func (s *Store) CreateAgent(agent *Agent) error {
	query := `
		INSERT INTO agents (id, run_id, task_id, task_title, status, started_at, finished_at, duration_seconds, exit_code, error_message, stdout, stderr, input_tokens, output_tokens, total_tokens, cost_usd, files_changed, git_commits_created, repo_id, archived)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		string(agent.Status),
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
		agent.CostUSD,
		agent.FilesChanged,
		agent.GitCommitsCreated,
		nullString(agent.RepoID),
		boolToInt(agent.Archived),
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
			finished_at = ?,
			duration_seconds = ?,
			exit_code = ?,
			error_message = ?,
			stdout = ?,
			stderr = ?,
			input_tokens = ?,
			output_tokens = ?,
			total_tokens = ?,
			cost_usd = ?,
			files_changed = ?,
			git_commits_created = ?,
			archived = ?
		WHERE id = ?
	`
	var finishedAt *int64
	if agent.FinishedAt != nil {
		ts := agent.FinishedAt.Unix()
		finishedAt = &ts
	}
	_, err := s.db.Exec(query,
		string(agent.Status),
		finishedAt,
		agent.DurationSeconds,
		agent.ExitCode,
		agent.ErrorMessage,
		agent.Stdout,
		agent.Stderr,
		agent.InputTokens,
		agent.OutputTokens,
		agent.TotalTokens,
		agent.CostUSD,
		agent.FilesChanged,
		agent.GitCommitsCreated,
		boolToInt(agent.Archived),
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

// GetAgent retrieves an agent by ID
func (s *Store) GetAgent(id string) (*Agent, error) {
	query := `
		SELECT id, run_id, task_id, task_title, status, started_at, finished_at, duration_seconds, exit_code, error_message, stdout, stderr, input_tokens, output_tokens, total_tokens, cost_usd, files_changed, git_commits_created, repo_id, archived
		FROM agents WHERE id = ?
	`
	row := s.db.QueryRow(query, id)
	return s.scanAgent(row)
}

// GetAgentsByRun retrieves all agents for a specific run
func (s *Store) GetAgentsByRun(runID string) ([]Agent, error) {
	query := `
		SELECT id, run_id, task_id, task_title, status, started_at, finished_at, duration_seconds, exit_code, error_message, stdout, stderr, input_tokens, output_tokens, total_tokens, cost_usd, files_changed, git_commits_created, repo_id, archived
		FROM agents WHERE run_id = ?
		ORDER BY started_at ASC
	`
	rows, err := s.db.Query(query, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to query agents: %w", err)
	}
	defer rows.Close()

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
func (s *Store) GetAllNonArchivedAgents() ([]Agent, error) {
	query := `
		SELECT id, run_id, task_id, task_title, status, started_at, finished_at, duration_seconds, exit_code, error_message, stdout, stderr, input_tokens, output_tokens, total_tokens, cost_usd, files_changed, git_commits_created, repo_id, archived
		FROM agents WHERE archived = 0
		ORDER BY started_at ASC
	`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query non-archived agents: %w", err)
	}
	defer rows.Close()

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
	query := `
		SELECT id, started_at, finished_at, status, concurrency, git_branch, git_commit, total_tasks, completed_tasks, failed_tasks, repo_id, repo_path, repo_name
		FROM runs WHERE status = 'running'
		ORDER BY started_at DESC
		LIMIT 1
	`
	row := s.db.QueryRow(query)
	return s.scanRun(row)
}

// GetMostRecentRun returns the most recent run regardless of status, or nil if none exists.
// This is used to restore historical state on daemon startup for display purposes.
func (s *Store) GetMostRecentRun() (*Run, error) {
	query := `
		SELECT id, started_at, finished_at, status, concurrency, git_branch, git_commit, total_tasks, completed_tasks, failed_tasks, repo_id, repo_path, repo_name
		FROM runs
		ORDER BY started_at DESC
		LIMIT 1
	`
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

// MarkOrphanedAgentsFailed marks all agents with status "starting" or "running" as "failed".
// This is used on daemon startup to handle agents that were interrupted by a crash.
// Returns the number of agents marked as failed.
func (s *Store) MarkOrphanedAgentsFailed() (int64, error) {
	now := time.Now().Unix()
	query := `
		UPDATE agents SET
			status = 'failed',
			finished_at = ?,
			error_message = 'daemon terminated unexpectedly'
		WHERE status IN ('starting', 'running')
	`
	result, err := s.db.Exec(query, now)
	if err != nil {
		return 0, fmt.Errorf("failed to mark orphaned agents: %w", err)
	}
	return result.RowsAffected()
}

// GetStats returns aggregate statistics, optionally filtered by time range
func (s *Store) GetStats(since *time.Time) (*AggregateStats, error) {
	// Query runs
	runsQuery := `
		SELECT
			COUNT(*) as total_runs,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as completed_runs,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed_runs
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
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query run stats: %w", err)
	}

	// Query agents
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

	return &stats, nil
}

// GetRunsByRepo retrieves all runs for a specific repository
func (s *Store) GetRunsByRepo(repoID string) ([]Run, error) {
	query := `
		SELECT id, started_at, finished_at, status, concurrency, git_branch, git_commit, total_tasks, completed_tasks, failed_tasks, repo_id, repo_path, repo_name
		FROM runs WHERE repo_id = ?
		ORDER BY started_at DESC
	`
	rows, err := s.db.Query(query, repoID)
	if err != nil {
		return nil, fmt.Errorf("failed to query runs by repo: %w", err)
	}
	defer rows.Close()

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
	// Query runs
	runsQuery := `
		SELECT
			COUNT(*) as total_runs,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as completed_runs,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed_runs
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
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query run stats by repo: %w", err)
	}

	// Query agents
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

	return &stats, nil
}

// RepoIDLookupFunc is a callback function used by MigrateOrphanedRepoIDs to look up
// repository IDs from paths. Returns (repoID, found).
type RepoIDLookupFunc func(path string) (string, bool)

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
			rows.Close()
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
	rows.Close()

	if len(orphanedRuns) == 0 {
		return 0, nil
	}

	// Begin transaction for atomic updates
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

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
	var errorMessage, stdout, stderr, repoID sql.NullString
	var archived sql.NullInt64

	err := row.Scan(
		&agent.ID,
		&agent.RunID,
		&agent.TaskID,
		&agent.TaskTitle,
		&status,
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
		&agent.CostUSD,
		&agent.FilesChanged,
		&agent.GitCommitsCreated,
		&repoID,
		&archived,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan agent: %w", err)
	}

	agent.Status = AgentStatus(status)
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
	agent.RepoID = repoID.String
	agent.Archived = archived.Valid && archived.Int64 == 1

	return &agent, nil
}

func (s *Store) scanAgentFromRows(rows *sql.Rows) (*Agent, error) {
	var agent Agent
	var startedAt, finishedAt sql.NullInt64
	var exitCode sql.NullInt64
	var status string
	var errorMessage, stdout, stderr, repoID sql.NullString
	var archived sql.NullInt64

	err := rows.Scan(
		&agent.ID,
		&agent.RunID,
		&agent.TaskID,
		&agent.TaskTitle,
		&status,
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
		&agent.CostUSD,
		&agent.FilesChanged,
		&agent.GitCommitsCreated,
		&repoID,
		&archived,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan agent: %w", err)
	}

	agent.Status = AgentStatus(status)
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
	agent.Archived = archived.Valid && archived.Int64 == 1
	agent.RepoID = repoID.String

	return &agent, nil
}
