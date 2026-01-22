package persistence

import (
	"database/sql"
	"fmt"
)

const currentSchemaVersion = 14

// migrate runs all pending database migrations
func (s *Store) migrate() error {
	// Create migrations table if it doesn't exist
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get current version
	var version int
	err = s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version)
	if err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}

	// Run pending migrations
	for v := version + 1; v <= currentSchemaVersion; v++ {
		if err := s.runMigration(v); err != nil {
			return fmt.Errorf("migration %d failed: %w", v, err)
		}
	}

	return nil
}

func (s *Store) runMigration(version int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	switch version {
	case 1:
		if err := s.migrateV1(tx); err != nil {
			return err
		}
	case 2:
		if err := s.migrateV2(tx); err != nil {
			return err
		}
	case 3:
		if err := s.migrateV3(tx); err != nil {
			return err
		}
	case 4:
		if err := s.migrateV4(tx); err != nil {
			return err
		}
	case 5:
		if err := s.migrateV5(tx); err != nil {
			return err
		}
	case 6:
		if err := s.migrateV6(tx); err != nil {
			return err
		}
	case 7:
		if err := s.migrateV7(tx); err != nil {
			return err
		}
	case 8:
		if err := s.migrateV8(tx); err != nil {
			return err
		}
	case 9:
		if err := s.migrateV9(tx); err != nil {
			return err
		}
	case 10:
		if err := s.migrateV10(tx); err != nil {
			return err
		}
	case 11:
		if err := s.migrateV11(tx); err != nil {
			return err
		}
	case 12:
		if err := s.migrateV12(tx); err != nil {
			return err
		}
	case 13:
		if err := s.migrateV13(tx); err != nil {
			return err
		}
	case 14:
		if err := s.migrateV14(tx); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown migration version: %d", version)
	}

	// Record migration
	_, err = tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, strftime('%s', 'now'))", version)
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit()
}

// migrateV1 creates the initial schema
func (s *Store) migrateV1(tx *sql.Tx) error {
	schema := `
		CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY,
			started_at INTEGER NOT NULL,
			finished_at INTEGER,
			status TEXT NOT NULL,
			concurrency INTEGER NOT NULL,
			git_branch TEXT,
			git_commit TEXT,
			total_tasks INTEGER DEFAULT 0,
			completed_tasks INTEGER DEFAULT 0,
			failed_tasks INTEGER DEFAULT 0
		);

		CREATE INDEX IF NOT EXISTS idx_runs_started_at ON runs(started_at DESC);
		CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);

		CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			task_title TEXT NOT NULL,
			status TEXT NOT NULL,
			started_at INTEGER NOT NULL,
			finished_at INTEGER,
			duration_seconds REAL,
			exit_code INTEGER,
			error_message TEXT,
			stdout TEXT,
			stderr TEXT,
			input_tokens INTEGER DEFAULT 0,
			output_tokens INTEGER DEFAULT 0,
			total_tokens INTEGER DEFAULT 0,
			cost_usd REAL DEFAULT 0.0,
			files_changed INTEGER DEFAULT 0,
			git_commits_created INTEGER DEFAULT 0,
			FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS idx_agents_run_id ON agents(run_id);
		CREATE INDEX IF NOT EXISTS idx_agents_started_at ON agents(started_at DESC);
		CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status);
	`

	_, err := tx.Exec(schema)
	return err
}

// migrateV2 adds repository ID columns to runs and agents tables
func (s *Store) migrateV2(tx *sql.Tx) error {
	migrations := []string{
		// Add repo columns to runs table (nullable for migration)
		`ALTER TABLE runs ADD COLUMN repo_id TEXT`,
		`ALTER TABLE runs ADD COLUMN repo_path TEXT`,
		`ALTER TABLE runs ADD COLUMN repo_name TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_runs_repo_id ON runs(repo_id)`,

		// Add repo_id to agents table
		`ALTER TABLE agents ADD COLUMN repo_id TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_agents_repo_id ON agents(repo_id)`,
	}

	for _, m := range migrations {
		if _, err := tx.Exec(m); err != nil {
			return fmt.Errorf("failed to execute migration: %s: %w", m, err)
		}
	}

	return nil
}

// migrateV3 adds archived column to agents table
func (s *Store) migrateV3(tx *sql.Tx) error {
	migrations := []string{
		// Add archived column to agents table (default false)
		`ALTER TABLE agents ADD COLUMN archived INTEGER DEFAULT 0`,
	}

	for _, m := range migrations {
		if _, err := tx.Exec(m); err != nil {
			return fmt.Errorf("failed to execute migration: %s: %w", m, err)
		}
	}

	return nil
}

// migrateV4 adds aggregate token/cost fields to runs table (for history deprecation)
func (s *Store) migrateV4(tx *sql.Tx) error {
	migrations := []string{
		// Add aggregate fields to runs table
		`ALTER TABLE runs ADD COLUMN total_input_tokens INTEGER DEFAULT 0`,
		`ALTER TABLE runs ADD COLUMN total_output_tokens INTEGER DEFAULT 0`,
		`ALTER TABLE runs ADD COLUMN cache_creation_tokens INTEGER DEFAULT 0`,
		`ALTER TABLE runs ADD COLUMN cache_read_tokens INTEGER DEFAULT 0`,
		`ALTER TABLE runs ADD COLUMN total_cost_usd REAL DEFAULT 0.0`,
		`ALTER TABLE runs ADD COLUMN total_turns INTEGER DEFAULT 0`,
		`ALTER TABLE runs ADD COLUMN files_changed INTEGER DEFAULT 0`,
		`ALTER TABLE runs ADD COLUMN git_commits INTEGER DEFAULT 0`,
		`ALTER TABLE runs ADD COLUMN duration_seconds REAL DEFAULT 0.0`,
	}

	for _, m := range migrations {
		if _, err := tx.Exec(m); err != nil {
			return fmt.Errorf("failed to execute migration: %s: %w", m, err)
		}
	}

	return nil
}

// migrateV5 adds cache token fields, num_turns, and result_message to agents table
func (s *Store) migrateV5(tx *sql.Tx) error {
	migrations := []string{
		// Add cache token fields to agents table
		`ALTER TABLE agents ADD COLUMN cache_creation_tokens INTEGER DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN cache_read_tokens INTEGER DEFAULT 0`,
		// Add num_turns and result_message
		`ALTER TABLE agents ADD COLUMN num_turns INTEGER DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN result_message TEXT`,
	}

	for _, m := range migrations {
		if _, err := tx.Exec(m); err != nil {
			return fmt.Errorf("failed to execute migration: %s: %w", m, err)
		}
	}

	return nil
}

// migrateV6 adds merge result tracking fields to agents table
func (s *Store) migrateV6(tx *sql.Tx) error {
	migrations := []string{
		// Add merge result fields to agents table
		`ALTER TABLE agents ADD COLUMN merge_status TEXT`,         // Final merge status (merged, failed, etc.)
		`ALTER TABLE agents ADD COLUMN merge_commits_applied INTEGER DEFAULT 0`, // Number of commits applied
		`ALTER TABLE agents ADD COLUMN merge_had_conflict INTEGER DEFAULT 0`,    // Whether conflict occurred (bool)
		`ALTER TABLE agents ADD COLUMN merge_resolver_spawned INTEGER DEFAULT 0`, // Whether resolver was spawned (bool)
		`ALTER TABLE agents ADD COLUMN merge_error TEXT`,          // Error message if merge failed
	}

	for _, m := range migrations {
		if _, err := tx.Exec(m); err != nil {
			return fmt.Errorf("failed to execute migration: %s: %w", m, err)
		}
	}

	return nil
}

// migrateV7 adds parent_agent_id column to agents table for resolver agent grouping
func (s *Store) migrateV7(tx *sql.Tx) error {
	migrations := []string{
		// Add parent_agent_id to track resolver agent relationships
		`ALTER TABLE agents ADD COLUMN parent_agent_id TEXT`,
		// Index for efficient child lookups
		`CREATE INDEX IF NOT EXISTS idx_agents_parent_agent_id ON agents(parent_agent_id)`,
	}

	for _, m := range migrations {
		if _, err := tx.Exec(m); err != nil {
			return fmt.Errorf("failed to execute migration: %s: %w", m, err)
		}
	}

	return nil
}

// migrateV8 backfills parent_agent_id for existing resolver agents
// Resolver agents have IDs ending in "-resolver" and share the same task_id as their parent
func (s *Store) migrateV8(tx *sql.Tx) error {
	// Find all resolver agents that don't have a parent_agent_id set
	// and match them with their parent agent in the same run by task_id
	//
	// Resolver agent convention: ID = "{taskID}-resolver" and task_id = original task_id
	// Parent agent: same run_id and task_id, but ID doesn't end in "-resolver"
	backfillSQL := `
		UPDATE agents
		SET parent_agent_id = (
			SELECT parent.id
			FROM agents parent
			WHERE parent.run_id = agents.run_id
			  AND parent.task_id = agents.task_id
			  AND parent.id != agents.id
			  AND parent.id NOT LIKE '%-resolver'
			LIMIT 1
		)
		WHERE agents.id LIKE '%-resolver'
		  AND (agents.parent_agent_id IS NULL OR agents.parent_agent_id = '')
	`

	_, err := tx.Exec(backfillSQL)
	if err != nil {
		return fmt.Errorf("failed to backfill parent_agent_id: %w", err)
	}

	return nil
}

// migrateV9 creates the tasks table for beads task persistence
func (s *Store) migrateV9(tx *sql.Tx) error {
	schema := `
		CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			repo_id TEXT,
			title TEXT NOT NULL,
			status TEXT NOT NULL,
			type TEXT,
			priority INTEGER DEFAULT 2,
			agent_id TEXT,
			updated_at INTEGER DEFAULT (strftime('%s', 'now'))
		);

		CREATE INDEX IF NOT EXISTS idx_tasks_repo_id ON tasks(repo_id);
		CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
	`

	_, err := tx.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create tasks table: %w", err)
	}

	return nil
}

// migrateV10 adds validation result tracking fields to agents table
func (s *Store) migrateV10(tx *sql.Tx) error {
	migrations := []string{
		// Add validation result fields to agents table
		`ALTER TABLE agents ADD COLUMN validation_status TEXT`,          // Overall validation status (pending, running, passed, failed, skipped)
		`ALTER TABLE agents ADD COLUMN validation_duration_ms INTEGER`,  // Total validation duration in milliseconds
		`ALTER TABLE agents ADD COLUMN validation_error TEXT`,           // Error message if validation failed
		`ALTER TABLE agents ADD COLUMN validation_steps TEXT`,           // JSON-encoded array of validation steps
	}

	for _, m := range migrations {
		if _, err := tx.Exec(m); err != nil {
			return fmt.Errorf("failed to execute migration: %s: %w", m, err)
		}
	}

	return nil
}

// migrateV11 adds repair agent tracking fields to agents table
func (s *Store) migrateV11(tx *sql.Tx) error {
	migrations := []string{
		// Add repair agent tracking fields to agents table
		`ALTER TABLE agents ADD COLUMN repair_attempts INTEGER DEFAULT 0`, // Number of repair attempts made
		`ALTER TABLE agents ADD COLUMN last_repair_output TEXT`,           // Output/error from last repair attempt
	}

	for _, m := range migrations {
		if _, err := tx.Exec(m); err != nil {
			return fmt.Errorf("failed to execute migration: %s: %w", m, err)
		}
	}

	return nil
}

// migrateV12 adds task_description column to agents table
func (s *Store) migrateV12(tx *sql.Tx) error {
	migrations := []string{
		// Add task_description to store the full task description (from beads)
		`ALTER TABLE agents ADD COLUMN task_description TEXT`,
	}

	for _, m := range migrations {
		if _, err := tx.Exec(m); err != nil {
			return fmt.Errorf("failed to execute migration: %s: %w", m, err)
		}
	}

	return nil
}

// migrateV13 adds session_id column to agents table for claude --resume support
func (s *Store) migrateV13(tx *sql.Tx) error {
	migrations := []string{
		// Add session_id to store Claude CLI session ID for resumability
		`ALTER TABLE agents ADD COLUMN session_id TEXT`,
		// Index for efficient session lookups (used for claude --resume)
		`CREATE INDEX IF NOT EXISTS idx_agents_session_id ON agents(session_id)`,
	}

	for _, m := range migrations {
		if _, err := tx.Exec(m); err != nil {
			return fmt.Errorf("failed to execute migration: %s: %w", m, err)
		}
	}

	return nil
}

// migrateV14 creates the active_overlays table for tracking overlay filesystems
func (s *Store) migrateV14(tx *sql.Tx) error {
	schema := `
		CREATE TABLE IF NOT EXISTS active_overlays (
			agent_id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			run_id TEXT NOT NULL,
			session_id TEXT,
			upper_dir TEXT NOT NULL,
			merged_dir TEXT NOT NULL,
			lower_dir TEXT NOT NULL,
			work_dir TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			status TEXT DEFAULT 'active',
			FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS idx_active_overlays_status ON active_overlays(status);
		CREATE INDEX IF NOT EXISTS idx_active_overlays_run_id ON active_overlays(run_id);
	`
	_, err := tx.Exec(schema)
	return err
}
