package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/jzila/canopy/pkg/persistence"
)

// Legacy JSON history types (inlined from deprecated pkg/history)

// legacyRunStatus represents the overall status of a run in JSON history
type legacyRunStatus string

const (
	legacyRunStatusCompleted legacyRunStatus = "completed"
	legacyRunStatusFailed    legacyRunStatus = "failed"
	legacyRunStatusPartial   legacyRunStatus = "partial"
)

// legacyModelUsage represents per-model token usage and cost
type legacyModelUsage struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	CostUSD                  float64 `json:"cost_usd"`
}

// legacyTaskRecord represents a single task execution within a run
type legacyTaskRecord struct {
	ID            string                      `json:"id"`
	Title         string                      `json:"title"`
	Status        string                      `json:"status"`
	StartTime     time.Time                   `json:"start_time"`
	EndTime       time.Time                   `json:"end_time,omitempty"`
	DurationMS    int64                       `json:"duration_ms"`
	InputTokens   int                         `json:"input_tokens"`
	OutputTokens  int                         `json:"output_tokens"`
	CostUSD       float64                     `json:"cost_usd"`
	FilesChanged  int                         `json:"files_changed"`
	Commits       int                         `json:"commits"`
	Error         string                      `json:"error,omitempty"`
	ResultMessage string                      `json:"result_message,omitempty"`
	ModelUsage    map[string]legacyModelUsage `json:"model_usage,omitempty"`
}

// legacyRunRecord represents a complete orchestration run from JSON history
type legacyRunRecord struct {
	ID                       string             `json:"id"`
	StartTime                time.Time          `json:"start_time"`
	EndTime                  time.Time          `json:"end_time"`
	Status                   legacyRunStatus    `json:"status"`
	WorkDir                  string             `json:"work_dir"`
	TotalTasks               int                `json:"total_tasks"`
	CompletedTasks           int                `json:"completed_tasks"`
	FailedTasks              int                `json:"failed_tasks"`
	DurationSeconds          float64            `json:"duration_seconds"`
	TotalInputTokens         int                `json:"total_input_tokens"`
	TotalOutputTokens        int                `json:"total_output_tokens"`
	TotalCacheCreationTokens int                `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens     int                `json:"total_cache_read_tokens"`
	TotalCostUSD             float64            `json:"total_cost_usd"`
	TotalTurns               int                `json:"total_turns"`
	FilesChanged             int                `json:"files_changed"`
	GitCommits               int                `json:"git_commits"`
	Tasks                    []legacyTaskRecord `json:"tasks,omitempty"`
}

var (
	migrateHistoryDryRun bool
	migrateHistoryForce  bool
)

var migrateHistoryCmd = &cobra.Command{
	Use:   "migrate-history",
	Short: "Migrate JSON history files to SQLite database",
	Long: `Migrate run history from legacy JSON files to the SQLite database.

This command reads JSON history files from ~/.cache/canopy/history/ and imports
them into the SQLite database used by 'canopy history'.

The migration is idempotent - duplicate runs (by ID) are skipped. A migration
marker is stored in the database to prevent re-running unless --force is used.

EXAMPLES
  # Preview migration without making changes
  canopy migrate-history --dry-run

  # Run the migration
  canopy migrate-history

  # Force re-run migration (useful if JSON files were added later)
  canopy migrate-history --force`,
	RunE: runMigrateHistory,
}

func init() {
	migrateHistoryCmd.Flags().BoolVar(&migrateHistoryDryRun, "dry-run", false, "Preview migration without making changes")
	migrateHistoryCmd.Flags().BoolVar(&migrateHistoryForce, "force", false, "Run migration even if already completed")

	rootCmd.AddCommand(migrateHistoryCmd)
}

func runMigrateHistory(cmd *cobra.Command, args []string) error {
	// Find JSON history directory
	historyDir, err := getHistoryDir()
	if err != nil {
		return fmt.Errorf("failed to determine history directory: %w", err)
	}

	// Check if history directory exists
	if _, err := os.Stat(historyDir); os.IsNotExist(err) {
		fmt.Printf("No JSON history directory found at %s\n", historyDir)
		fmt.Println("Nothing to migrate.")
		return nil
	}

	// Read JSON files
	jsonRuns, err := readJSONHistoryFiles(historyDir)
	if err != nil {
		return fmt.Errorf("failed to read JSON history files: %w", err)
	}

	if len(jsonRuns) == 0 {
		fmt.Printf("No JSON history files found in %s\n", historyDir)
		fmt.Println("Nothing to migrate.")
		return nil
	}

	fmt.Printf("Found %d JSON history file(s) in %s\n", len(jsonRuns), historyDir)

	if migrateHistoryDryRun {
		fmt.Println("\n[DRY RUN] Would migrate the following runs:")
		for _, run := range jsonRuns {
			taskCount := len(run.Tasks)
			fmt.Printf("  - %s: %s (%d tasks, %s)\n",
				run.ID, run.Status, taskCount, formatCost(run.TotalCostUSD))
		}
		return nil
	}

	// Open persistence store
	store, err := persistence.NewStore()
	if err != nil {
		return fmt.Errorf("failed to open SQLite store: %w", err)
	}
	defer store.Close()

	// Check if migration already completed (unless --force)
	if !migrateHistoryForce {
		completed, err := isMigrationCompleted(store)
		if err != nil {
			return fmt.Errorf("failed to check migration status: %w", err)
		}
		if completed {
			fmt.Println("Migration already completed. Use --force to re-run.")
			return nil
		}
	}

	// Perform migration
	migrated, skipped, errors := migrateRuns(store, jsonRuns)

	// Report results
	fmt.Printf("\nMigration complete:\n")
	fmt.Printf("  Migrated: %d runs\n", migrated)
	fmt.Printf("  Skipped (already exist): %d runs\n", skipped)
	if errors > 0 {
		fmt.Printf("  Errors: %d runs\n", errors)
	}

	// Mark migration as completed
	if err := markMigrationCompleted(store); err != nil {
		return fmt.Errorf("failed to mark migration completed: %w", err)
	}

	return nil
}

// getHistoryDir returns the path to the JSON history directory
func getHistoryDir() (string, error) {
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		cacheDir = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheDir, "canopy", "history"), nil
}

// readJSONHistoryFiles reads all run-*.json files from the history directory
func readJSONHistoryFiles(dir string) ([]*legacyRunRecord, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var runs []*legacyRunRecord
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "run-") || !strings.HasSuffix(name, ".json") {
			continue
		}

		path := filepath.Join(dir, name)
		run, err := readJSONRunFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read %s: %v\n", name, err)
			continue
		}
		runs = append(runs, run)
	}

	return runs, nil
}

// readJSONRunFile reads a single JSON run file
func readJSONRunFile(path string) (*legacyRunRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var run legacyRunRecord
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &run, nil
}

// migrateRuns migrates JSON runs to SQLite, returning counts
func migrateRuns(store *persistence.Store, jsonRuns []*legacyRunRecord) (migrated, skipped, errors int) {
	for _, jsonRun := range jsonRuns {
		// Check if run already exists
		existing, err := store.GetRun(jsonRun.ID)
		if err == nil && existing != nil {
			skipped++
			if verbose {
				fmt.Printf("  Skipping %s (already exists)\n", jsonRun.ID)
			}
			continue
		}

		// Convert and insert
		if err := migrateRun(store, jsonRun); err != nil {
			fmt.Fprintf(os.Stderr, "Error migrating %s: %v\n", jsonRun.ID, err)
			errors++
			continue
		}

		migrated++
		if verbose {
			fmt.Printf("  Migrated %s\n", jsonRun.ID)
		}
	}
	return
}

// migrateRun converts a JSON run to SQLite format and inserts it
func migrateRun(store *persistence.Store, jsonRun *legacyRunRecord) error {
	// Convert legacyRunRecord to persistence.Run
	run := &persistence.Run{
		ID:                  jsonRun.ID,
		StartedAt:           jsonRun.StartTime,
		Status:              convertRunStatus(jsonRun.Status),
		Concurrency:         1, // Not tracked in JSON format
		TotalTasks:          jsonRun.TotalTasks,
		CompletedTasks:      jsonRun.CompletedTasks,
		FailedTasks:         jsonRun.FailedTasks,
		RepoPath:            jsonRun.WorkDir,
		TotalInputTokens:    jsonRun.TotalInputTokens,
		TotalOutputTokens:   jsonRun.TotalOutputTokens,
		CacheCreationTokens: jsonRun.TotalCacheCreationTokens,
		CacheReadTokens:     jsonRun.TotalCacheReadTokens,
		TotalCostUSD:        jsonRun.TotalCostUSD,
		TotalTurns:          jsonRun.TotalTurns,
		FilesChanged:        jsonRun.FilesChanged,
		GitCommits:          jsonRun.GitCommits,
		DurationSeconds:     jsonRun.DurationSeconds,
	}

	// Set finished time
	if !jsonRun.EndTime.IsZero() {
		run.FinishedAt = &jsonRun.EndTime
	}

	// Extract repo name from work dir
	if jsonRun.WorkDir != "" {
		run.RepoName = filepath.Base(jsonRun.WorkDir)
	}

	// Create the run
	if err := store.CreateRun(run); err != nil {
		return fmt.Errorf("failed to create run: %w", err)
	}

	// Convert and create agents for each task
	for _, task := range jsonRun.Tasks {
		agent := convertTaskToAgent(jsonRun.ID, &task)
		if err := store.CreateAgent(agent); err != nil {
			// Log but don't fail the entire run migration
			fmt.Fprintf(os.Stderr, "Warning: failed to create agent for task %s: %v\n", task.ID, err)
		}
	}

	return nil
}

// convertRunStatus converts legacyRunStatus to persistence.RunStatus
func convertRunStatus(status legacyRunStatus) persistence.RunStatus {
	switch status {
	case legacyRunStatusCompleted:
		return persistence.RunStatusCompleted
	case legacyRunStatusFailed:
		return persistence.RunStatusFailed
	case legacyRunStatusPartial:
		return persistence.RunStatusPartial
	default:
		return persistence.RunStatusFailed
	}
}

// convertTaskToAgent converts a legacyTaskRecord to a persistence.Agent
func convertTaskToAgent(runID string, task *legacyTaskRecord) *persistence.Agent {
	agent := &persistence.Agent{
		ID:              generateAgentID(runID, task.ID),
		RunID:           runID,
		TaskID:          task.ID,
		TaskTitle:       task.Title,
		Status:          convertTaskStatus(task.Status),
		StartedAt:       task.StartTime,
		DurationSeconds: float64(task.DurationMS) / 1000.0,
		InputTokens:     task.InputTokens,
		OutputTokens:    task.OutputTokens,
		TotalTokens:     task.InputTokens + task.OutputTokens,
		CostUSD:         task.CostUSD,
		FilesChanged:    task.FilesChanged,
		GitCommitsCreated: task.Commits,
		ErrorMessage:    task.Error,
		ResultMessage:   task.ResultMessage,
	}

	// Set finished time if available
	if !task.EndTime.IsZero() {
		agent.FinishedAt = &task.EndTime
	}

	// Extract cache tokens from model usage if available
	for _, usage := range task.ModelUsage {
		agent.CacheCreationTokens += usage.CacheCreationInputTokens
		agent.CacheReadTokens += usage.CacheReadInputTokens
	}

	return agent
}

// generateAgentID generates a unique agent ID based on run and task
func generateAgentID(runID, taskID string) string {
	// Use a deterministic ID based on run+task to allow idempotent migration
	// We use a UUID namespace to ensure uniqueness
	namespace := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8") // URL namespace
	name := fmt.Sprintf("canopy-agent:%s:%s", runID, taskID)
	return uuid.NewSHA1(namespace, []byte(name)).String()
}

// convertTaskStatus converts task status string to persistence.AgentStatus
func convertTaskStatus(status string) persistence.AgentStatus {
	switch strings.ToLower(status) {
	case "completed":
		return persistence.AgentStatusCompleted
	case "failed":
		return persistence.AgentStatusFailed
	case "running":
		return persistence.AgentStatusRunning
	case "timed_out":
		return persistence.AgentStatusTimedOut
	case "cancelled":
		return persistence.AgentStatusCancelled
	default:
		return persistence.AgentStatusFailed
	}
}

// isMigrationCompleted checks if JSON migration has already been run
func isMigrationCompleted(store *persistence.Store) (bool, error) {
	// We store a marker in the schema_migrations table with a special version
	// Version 1000 is reserved for JSON history migration
	const migrationMarkerVersion = 1000

	db := store.DB()
	if db == nil {
		return false, fmt.Errorf("database not available")
	}

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", migrationMarkerVersion).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// markMigrationCompleted marks the JSON migration as completed
func markMigrationCompleted(store *persistence.Store) error {
	const migrationMarkerVersion = 1000

	db := store.DB()
	if db == nil {
		return fmt.Errorf("database not available")
	}

	now := time.Now().Unix()
	_, err := db.Exec(
		"INSERT OR REPLACE INTO schema_migrations (version, applied_at) VALUES (?, ?)",
		migrationMarkerVersion, now,
	)
	return err
}
