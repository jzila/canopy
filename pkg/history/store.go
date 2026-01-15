// Package history provides persistent storage for canopy run history.
//
// Deprecated: This package is deprecated in favor of pkg/persistence which
// provides SQLite-based storage with better performance, transaction support,
// and unified data management. Use the 'canopy migrate-history' command to
// migrate existing JSON history files to the SQLite database.
//
// The pkg/persistence package provides:
//   - Single unified persistence approach (SQLite)
//   - Proper transaction boundaries via BeginTx/Commit/Rollback
//   - Thread-safe concurrent access with WAL mode
//   - Clear data ownership (runs own agents)
//
// Migration path:
//  1. Run 'canopy migrate-history' to import JSON files into SQLite
//  2. Update code to use pkg/persistence.Store instead of pkg/history.Store
//  3. Remove references to this package
package history

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jzila/canopy/pkg/logging"
)

const (
	// dirName is the subdirectory for history data
	dirName = "history"
	// runPrefix is the prefix for run files
	runPrefix = "run-"
	// fileSuffix is the file extension for run files
	fileSuffix = ".json"
)

// RunStatus represents the overall status of a run
type RunStatus string

const (
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusPartial   RunStatus = "partial" // Some tasks succeeded, some failed
)

// TaskRecord represents a single task execution within a run
type TaskRecord struct {
	ID            string                 `json:"id"`
	Title         string                 `json:"title"`
	Status        string                 `json:"status"` // completed, failed
	StartTime     time.Time              `json:"start_time"`
	EndTime       time.Time              `json:"end_time,omitempty"`
	DurationMS    int64                  `json:"duration_ms"`
	InputTokens   int                    `json:"input_tokens"`
	OutputTokens  int                    `json:"output_tokens"`
	CostUSD       float64                `json:"cost_usd"`
	FilesChanged  int                    `json:"files_changed"`
	Commits       int                    `json:"commits"`
	Error         string                 `json:"error,omitempty"`
	ResultMessage string                 `json:"result_message,omitempty"`
	ModelUsage    map[string]ModelUsage  `json:"model_usage,omitempty"`
}

// ModelUsage represents per-model token usage and cost
type ModelUsage struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	CostUSD                  float64 `json:"cost_usd"`
}

// RunRecord represents a complete orchestration run
type RunRecord struct {
	ID                       string       `json:"id"`
	StartTime                time.Time    `json:"start_time"`
	EndTime                  time.Time    `json:"end_time"`
	Status                   RunStatus    `json:"status"`
	WorkDir                  string       `json:"work_dir"`
	TotalTasks               int          `json:"total_tasks"`
	CompletedTasks           int          `json:"completed_tasks"`
	FailedTasks              int          `json:"failed_tasks"`
	DurationSeconds          float64      `json:"duration_seconds"`
	TotalInputTokens         int          `json:"total_input_tokens"`
	TotalOutputTokens        int          `json:"total_output_tokens"`
	TotalCacheCreationTokens int          `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens     int          `json:"total_cache_read_tokens"`
	TotalCostUSD             float64      `json:"total_cost_usd"`
	TotalTurns               int          `json:"total_turns"`
	FilesChanged             int          `json:"files_changed"`
	GitCommits               int          `json:"git_commits"`
	Tasks                    []TaskRecord `json:"tasks,omitempty"`
}

// Store provides persistent storage for run history
type Store struct {
	dataDir string
}

// NewStore creates a new history store using the default data directory.
// It will migrate history from the old location if it exists and the new
// location is empty.
func NewStore() (*Store, error) {
	newDir, err := defaultDataDir()
	if err != nil {
		return nil, err
	}

	// Check for old location and migrate if needed
	oldDir, err := oldDataDir()
	if err == nil && oldDir != newDir {
		if err := migrateHistory(oldDir, newDir); err != nil {
			logging.Warn("failed to migrate history", "error", err)
		}
	}

	return NewStoreWithDir(newDir)
}

// NewStoreWithDir creates a new history store with a custom data directory
func NewStoreWithDir(dataDir string) (*Store, error) {
	historyDir := filepath.Join(dataDir, dirName)
	if err := os.MkdirAll(historyDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create history directory: %w", err)
	}
	return &Store{dataDir: historyDir}, nil
}

// defaultDataDir returns the cache directory for canopy data.
// Per the persistence invariant, ALL canopy persistence MUST live at
// $XDG_CACHE_HOME/canopy/ or ~/.cache/canopy/
func defaultDataDir() (string, error) {
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		cacheDir = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheDir, "canopy"), nil
}

// oldDataDir returns the old data directory locations for migration purposes.
func oldDataDir() (string, error) {
	// Check XDG_DATA_HOME first
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, "canopy"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Check macOS location
	if _, err := os.Stat("/Library"); err == nil {
		return filepath.Join(home, "Library", "Application Support", "canopy"), nil
	}
	// Linux location
	return filepath.Join(home, ".local", "share", "canopy"), nil
}

// migrateHistory copies history files from the old location to the new location.
// It only migrates if the old history exists and the new history directory is empty.
// Old files are left in place for the user to delete manually.
func migrateHistory(oldDir, newDir string) error {
	oldHistoryDir := filepath.Join(oldDir, dirName)
	newHistoryDir := filepath.Join(newDir, dirName)

	// Check if old history exists
	oldEntries, err := os.ReadDir(oldHistoryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No old history to migrate
		}
		return fmt.Errorf("failed to read old history directory: %w", err)
	}

	// Count old run files
	var oldRunFiles []os.DirEntry
	for _, entry := range oldEntries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), runPrefix) && strings.HasSuffix(entry.Name(), fileSuffix) {
			oldRunFiles = append(oldRunFiles, entry)
		}
	}
	if len(oldRunFiles) == 0 {
		return nil // No run files to migrate
	}

	// Check if new history already has files (don't overwrite existing data)
	newEntries, err := os.ReadDir(newHistoryDir)
	if err == nil {
		for _, entry := range newEntries {
			if !entry.IsDir() && strings.HasPrefix(entry.Name(), runPrefix) && strings.HasSuffix(entry.Name(), fileSuffix) {
				return nil // New location already has history, don't migrate
			}
		}
	}

	// Create new history directory
	if err := os.MkdirAll(newHistoryDir, 0755); err != nil {
		return fmt.Errorf("failed to create new history directory: %w", err)
	}

	// Copy files
	var copiedCount int
	for _, entry := range oldRunFiles {
		oldPath := filepath.Join(oldHistoryDir, entry.Name())
		newPath := filepath.Join(newHistoryDir, entry.Name())

		if err := copyFile(oldPath, newPath); err != nil {
			return fmt.Errorf("failed to copy %s: %w", entry.Name(), err)
		}
		copiedCount++
	}

	// Verify migration succeeded
	if copiedCount != len(oldRunFiles) {
		return fmt.Errorf("migration incomplete: copied %d of %d files", copiedCount, len(oldRunFiles))
	}

	logging.Info("migrated history files", "count", copiedCount, "from", oldHistoryDir, "to", newHistoryDir)
	return nil
}

// copyFile copies a single file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return dstFile.Sync()
}

// Save persists a run record to disk
func (s *Store) Save(run *RunRecord) error {
	filename := runFilename(run.ID)
	path := filepath.Join(s.dataDir, filename)

	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal run record: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write run record: %w", err)
	}

	return nil
}

// Get retrieves a run record by ID
func (s *Store) Get(runID string) (*RunRecord, error) {
	filename := runFilename(runID)
	path := filepath.Join(s.dataDir, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("run not found: %s", runID)
		}
		return nil, fmt.Errorf("failed to read run record: %w", err)
	}

	var run RunRecord
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("failed to parse run record: %w", err)
	}

	return &run, nil
}

// ListOptions specifies filters for listing runs
type ListOptions struct {
	Since  time.Time // Only runs after this time
	Status RunStatus // Filter by status (empty = all)
	Limit  int       // Maximum number of results (0 = unlimited)
}

// List returns run records matching the specified options
func (s *Store) List(opts ListOptions) ([]*RunRecord, error) {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read history directory: %w", err)
	}

	var runs []*RunRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), runPrefix) || !strings.HasSuffix(entry.Name(), fileSuffix) {
			continue
		}

		path := filepath.Join(s.dataDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue // Skip files we can't read
		}

		var run RunRecord
		if err := json.Unmarshal(data, &run); err != nil {
			continue // Skip files we can't parse
		}

		// Apply filters
		if !opts.Since.IsZero() && run.StartTime.Before(opts.Since) {
			continue
		}
		if opts.Status != "" && run.Status != opts.Status {
			continue
		}

		runs = append(runs, &run)
	}

	// Sort by start time, newest first
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartTime.After(runs[j].StartTime)
	})

	// Apply limit
	if opts.Limit > 0 && len(runs) > opts.Limit {
		runs = runs[:opts.Limit]
	}

	return runs, nil
}

// AggregateStats represents aggregate statistics across multiple runs
type AggregateStats struct {
	TotalRuns                int           `json:"total_runs"`
	CompletedRuns            int           `json:"completed_runs"`
	FailedRuns               int           `json:"failed_runs"`
	PartialRuns              int           `json:"partial_runs"`
	TotalTasks               int           `json:"total_tasks"`
	CompletedTasks           int           `json:"completed_tasks"`
	FailedTasks              int           `json:"failed_tasks"`
	TotalInputTokens         int           `json:"total_input_tokens"`
	TotalOutputTokens        int           `json:"total_output_tokens"`
	TotalCacheCreationTokens int           `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens     int           `json:"total_cache_read_tokens"`
	TotalCostUSD             float64       `json:"total_cost_usd"`
	TotalDuration            time.Duration `json:"total_duration"`
	FilesChanged             int           `json:"files_changed"`
	GitCommits               int           `json:"git_commits"`
}

// Stats calculates aggregate statistics for runs matching the options
func (s *Store) Stats(opts ListOptions) (*AggregateStats, error) {
	runs, err := s.List(opts)
	if err != nil {
		return nil, err
	}

	stats := &AggregateStats{}
	for _, run := range runs {
		stats.TotalRuns++
		switch run.Status {
		case RunStatusCompleted:
			stats.CompletedRuns++
		case RunStatusFailed:
			stats.FailedRuns++
		case RunStatusPartial:
			stats.PartialRuns++
		}

		stats.TotalTasks += run.TotalTasks
		stats.CompletedTasks += run.CompletedTasks
		stats.FailedTasks += run.FailedTasks
		stats.TotalInputTokens += run.TotalInputTokens
		stats.TotalOutputTokens += run.TotalOutputTokens
		stats.TotalCacheCreationTokens += run.TotalCacheCreationTokens
		stats.TotalCacheReadTokens += run.TotalCacheReadTokens
		stats.TotalCostUSD += run.TotalCostUSD
		stats.TotalDuration += time.Duration(run.DurationSeconds * float64(time.Second))
		stats.FilesChanged += run.FilesChanged
		stats.GitCommits += run.GitCommits
	}

	return stats, nil
}

// Delete removes a run record by ID
func (s *Store) Delete(runID string) error {
	filename := runFilename(runID)
	path := filepath.Join(s.dataDir, filename)

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("run not found: %s", runID)
		}
		return fmt.Errorf("failed to delete run record: %w", err)
	}

	return nil
}

// runFilename generates the filename for a run ID
func runFilename(runID string) string {
	// Handle both full IDs and short IDs
	if strings.HasPrefix(runID, runPrefix) {
		return runID + fileSuffix
	}
	return runPrefix + runID + fileSuffix
}

// FindByPrefix finds a run by ID prefix (for short ID lookup)
func (s *Store) FindByPrefix(prefix string) (*RunRecord, error) {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read history directory: %w", err)
	}

	var matches []string
	searchPrefix := runPrefix + prefix

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), fileSuffix) {
			continue
		}
		if strings.HasPrefix(entry.Name(), searchPrefix) {
			matches = append(matches, entry.Name())
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("run not found: %s", prefix)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous run ID prefix: %s (matches %d runs)", prefix, len(matches))
	}

	// Extract full ID from filename
	fullID := strings.TrimPrefix(strings.TrimSuffix(matches[0], fileSuffix), runPrefix)
	return s.Get(fullID)
}
