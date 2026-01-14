package persistence

import (
	"database/sql"
	"fmt"
)

const currentSchemaVersion = 4

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
	defer tx.Rollback()

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
