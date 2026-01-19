package persistence

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// LiveFeedEvent represents a persisted live feed event.
// This mirrors the daemon.LiveFeedEvent structure for persistence.
type LiveFeedEvent struct {
	EventType string                 `json:"event_type"`
	RawData   map[string]interface{} `json:"data"`
}

var (
	// liveFeedMutexes protects concurrent writes to the same agent's JSONL file
	liveFeedMutexes sync.Map // map[string]*sync.Mutex
)

// getLiveFeedMutex returns a mutex for the given agent ID, creating one if needed.
func getLiveFeedMutex(agentID string) *sync.Mutex {
	if m, ok := liveFeedMutexes.Load(agentID); ok {
		return m.(*sync.Mutex)
	}
	newMutex := &sync.Mutex{}
	actual, _ := liveFeedMutexes.LoadOrStore(agentID, newMutex)
	return actual.(*sync.Mutex)
}

// GetLiveFeedDir returns the directory where live feed JSONL files are stored.
func GetLiveFeedDir() string {
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheDir, "canopy", "live_feed")
}

// getLiveFeedPath returns the path to the JSONL file for a given agent ID.
func getLiveFeedPath(agentID string) string {
	return filepath.Join(GetLiveFeedDir(), agentID+".jsonl")
}

// AppendLiveFeedEvent appends a single event to an agent's JSONL file.
// Thread-safe: uses per-agent mutex to protect concurrent writes.
func AppendLiveFeedEvent(agentID string, eventType string, rawData map[string]interface{}) error {
	if agentID == "" {
		return fmt.Errorf("agent ID is required")
	}

	mu := getLiveFeedMutex(agentID)
	mu.Lock()
	defer mu.Unlock()

	// Ensure directory exists
	dir := GetLiveFeedDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create live feed directory: %w", err)
	}

	path := getLiveFeedPath(agentID)

	// Open file for appending, create if not exists
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open live feed file: %w", err)
	}
	defer func() { _ = f.Close() }()

	event := LiveFeedEvent{
		EventType: eventType,
		RawData:   rawData,
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal live feed event: %w", err)
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write live feed event: %w", err)
	}

	return nil
}

// LoadLiveFeedEvents loads all events from an agent's JSONL file.
// Returns an empty slice if the file does not exist.
func LoadLiveFeedEvents(agentID string) ([]LiveFeedEvent, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent ID is required")
	}

	path := getLiveFeedPath(agentID)

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []LiveFeedEvent{}, nil
		}
		return nil, fmt.Errorf("failed to open live feed file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var events []LiveFeedEvent
	scanner := bufio.NewScanner(f)

	// Increase buffer size for potentially large lines
	const maxCapacity = 1024 * 1024 // 1MB per line
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event LiveFeedEvent
		if err := json.Unmarshal(line, &event); err != nil {
			// Log but continue - don't fail on corrupted lines
			continue
		}
		events = append(events, event)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading live feed file: %w", err)
	}

	return events, nil
}

// DeleteLiveFeedFile removes an agent's JSONL file.
// Returns nil if the file does not exist.
func DeleteLiveFeedFile(agentID string) error {
	if agentID == "" {
		return fmt.Errorf("agent ID is required")
	}

	path := getLiveFeedPath(agentID)

	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete live feed file: %w", err)
	}

	// Clean up mutex
	liveFeedMutexes.Delete(agentID)

	return nil
}

// LiveFeedFileExists checks if an agent's JSONL file exists.
func LiveFeedFileExists(agentID string) bool {
	if agentID == "" {
		return false
	}

	path := getLiveFeedPath(agentID)
	_, err := os.Stat(path)
	return err == nil
}
