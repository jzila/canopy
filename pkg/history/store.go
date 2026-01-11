// Package history provides persistent storage for canopy run history.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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

// NewStore creates a new history store using the default data directory
func NewStore() (*Store, error) {
	dataDir, err := defaultDataDir()
	if err != nil {
		return nil, err
	}
	return NewStoreWithDir(dataDir)
}

// NewStoreWithDir creates a new history store with a custom data directory
func NewStoreWithDir(dataDir string) (*Store, error) {
	historyDir := filepath.Join(dataDir, dirName)
	if err := os.MkdirAll(historyDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create history directory: %w", err)
	}
	return &Store{dataDir: historyDir}, nil
}

// defaultDataDir returns the platform-appropriate data directory
func defaultDataDir() (string, error) {
	// Try XDG_DATA_HOME first (Linux)
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, "canopy"), nil
	}

	// Fall back to home directory
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Use platform-appropriate location
	// On macOS: ~/Library/Application Support/canopy
	// On Linux: ~/.local/share/canopy
	if _, err := os.Stat("/Library"); err == nil {
		return filepath.Join(home, "Library", "Application Support", "canopy"), nil
	}
	return filepath.Join(home, ".local", "share", "canopy"), nil
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
