package persistence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendAndLoadLiveFeedEvents(t *testing.T) {
	// Use temp directory for test
	tempDir := t.TempDir()
	originalCacheDir := os.Getenv("XDG_CACHE_HOME")
	os.Setenv("XDG_CACHE_HOME", tempDir)
	defer os.Setenv("XDG_CACHE_HOME", originalCacheDir)

	agentID := "test-agent-123"

	// Test appending events
	events := []struct {
		eventType string
		rawData   map[string]interface{}
	}{
		{
			eventType: "tool_use",
			rawData: map[string]interface{}{
				"tool":      "Read",
				"file_path": "/some/path/file.go",
			},
		},
		{
			eventType: "text",
			rawData: map[string]interface{}{
				"text":       "Hello world",
				"is_historic": false,
			},
		},
		{
			eventType: "file_change",
			rawData: map[string]interface{}{
				"action":    "modified",
				"file_path": "/another/file.txt",
			},
		},
	}

	for _, e := range events {
		if err := AppendLiveFeedEvent(agentID, e.eventType, e.rawData); err != nil {
			t.Fatalf("AppendLiveFeedEvent failed: %v", err)
		}
	}

	// Verify file exists
	if !LiveFeedFileExists(agentID) {
		t.Fatal("LiveFeedFileExists returned false, expected true")
	}

	// Load events
	loaded, err := LoadLiveFeedEvents(agentID)
	if err != nil {
		t.Fatalf("LoadLiveFeedEvents failed: %v", err)
	}

	if len(loaded) != len(events) {
		t.Fatalf("Expected %d events, got %d", len(events), len(loaded))
	}

	// Verify event data
	for i, e := range events {
		if loaded[i].EventType != e.eventType {
			t.Errorf("Event %d: expected event_type %q, got %q", i, e.eventType, loaded[i].EventType)
		}
		// Check specific fields
		for k, v := range e.rawData {
			if loaded[i].RawData[k] != v {
				t.Errorf("Event %d: expected %s=%v, got %v", i, k, v, loaded[i].RawData[k])
			}
		}
	}

	// Test delete
	if err := DeleteLiveFeedFile(agentID); err != nil {
		t.Fatalf("DeleteLiveFeedFile failed: %v", err)
	}

	if LiveFeedFileExists(agentID) {
		t.Fatal("LiveFeedFileExists returned true after delete, expected false")
	}

	// Load should return empty slice for non-existent file
	loaded, err = LoadLiveFeedEvents(agentID)
	if err != nil {
		t.Fatalf("LoadLiveFeedEvents failed for missing file: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("Expected 0 events after delete, got %d", len(loaded))
	}
}

func TestLiveFeedEmptyAgentID(t *testing.T) {
	if err := AppendLiveFeedEvent("", "test", nil); err == nil {
		t.Error("Expected error for empty agent ID, got nil")
	}

	_, err := LoadLiveFeedEvents("")
	if err == nil {
		t.Error("Expected error for empty agent ID, got nil")
	}

	if err := DeleteLiveFeedFile(""); err == nil {
		t.Error("Expected error for empty agent ID, got nil")
	}
}

func TestGetLiveFeedDir(t *testing.T) {
	// Test with XDG_CACHE_HOME set
	tempDir := t.TempDir()
	originalCacheDir := os.Getenv("XDG_CACHE_HOME")
	os.Setenv("XDG_CACHE_HOME", tempDir)
	defer os.Setenv("XDG_CACHE_HOME", originalCacheDir)

	dir := GetLiveFeedDir()
	expected := filepath.Join(tempDir, "canopy", "live_feed")
	if dir != expected {
		t.Errorf("Expected %q, got %q", expected, dir)
	}
}

func TestConcurrentAppend(t *testing.T) {
	// Use temp directory for test
	tempDir := t.TempDir()
	originalCacheDir := os.Getenv("XDG_CACHE_HOME")
	os.Setenv("XDG_CACHE_HOME", tempDir)
	defer os.Setenv("XDG_CACHE_HOME", originalCacheDir)

	agentID := "concurrent-test-agent"
	numGoroutines := 10
	eventsPerGoroutine := 10

	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			for j := 0; j < eventsPerGoroutine; j++ {
				err := AppendLiveFeedEvent(agentID, "text", map[string]interface{}{
					"text":        "event",
					"goroutine":   goroutineID,
					"event_index": j,
				})
				if err != nil {
					t.Errorf("AppendLiveFeedEvent failed: %v", err)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify all events were written
	loaded, err := LoadLiveFeedEvents(agentID)
	if err != nil {
		t.Fatalf("LoadLiveFeedEvents failed: %v", err)
	}

	expectedEvents := numGoroutines * eventsPerGoroutine
	if len(loaded) != expectedEvents {
		t.Errorf("Expected %d events, got %d", expectedEvents, len(loaded))
	}

	// Cleanup
	DeleteLiveFeedFile(agentID)
}
