package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/persistence"
)

func TestRecoverOrphanedOverlays_NoPersistence(t *testing.T) {
	// Create daemon without persistence
	d := &Daemon{
		persistManager: NewPersistenceManager(false),
	}

	result, err := d.RecoverOrphanedOverlays(context.Background())
	if err != nil {
		t.Fatalf("RecoverOrphanedOverlays failed: %v", err)
	}

	// Should return empty result when persistence is disabled
	if result.Resumable != 0 || result.Cleaned != 0 || result.Failed != 0 {
		t.Errorf("expected empty result, got resumable=%d cleaned=%d failed=%d",
			result.Resumable, result.Cleaned, result.Failed)
	}
}

func TestRecoverOrphanedOverlays_WithOrphanedOverlays(t *testing.T) {
	// Create temp database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a test run and agent first (for foreign key)
	now := time.Now()
	run := &persistence.Run{
		ID:        "run-test",
		StartedAt: now,
		Status:    persistence.RunStatusRunning,
	}
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	agent := &persistence.Agent{
		ID:        "agent-test",
		RunID:     "run-test",
		TaskID:    "task-test",
		TaskTitle: "Test Task",
		Status:    persistence.AgentStatusRunning,
		StartedAt: now,
	}
	if err := store.CreateAgent(agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Create an active overlay
	overlay := &persistence.ActiveOverlay{
		AgentID:   "agent-test",
		TaskID:    "task-test",
		RunID:     "run-test",
		UpperDir:  filepath.Join(tmpDir, "upper"),
		MergedDir: filepath.Join(tmpDir, "merged"),
		LowerDir:  filepath.Join(tmpDir, "lower"),
		WorkDir:   filepath.Join(tmpDir, "work"),
		CreatedAt: now.Unix(),
		Status:    "active",
	}
	if err := store.TrackOverlay(overlay); err != nil {
		t.Fatalf("failed to track overlay: %v", err)
	}

	// Create the directories
	for _, dir := range []string{overlay.UpperDir, overlay.MergedDir, overlay.WorkDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create dir %s: %v", dir, err)
		}
	}

	// Create daemon with persistence
	d := &Daemon{
		persistManager: &PersistenceManager{
			store:   store,
			enabled: true,
		},
	}

	// Run recovery
	result, err := d.RecoverOrphanedOverlays(context.Background())
	if err != nil {
		t.Fatalf("RecoverOrphanedOverlays failed: %v", err)
	}

	// Should have cleaned 1 overlay (no session_id)
	if result.Cleaned != 1 {
		t.Errorf("expected 1 cleaned overlay, got %d", result.Cleaned)
	}
	if result.Resumable != 0 {
		t.Errorf("expected 0 resumable overlays, got %d", result.Resumable)
	}

	// Verify directories were removed
	for _, dir := range []string{overlay.UpperDir, overlay.WorkDir, overlay.MergedDir} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("expected dir %s to be removed", dir)
		}
	}

	// Verify overlay record was deleted
	overlays, err := store.GetOrphanedOverlays()
	if err != nil {
		t.Fatalf("failed to get orphaned overlays: %v", err)
	}
	if len(overlays) != 0 {
		t.Errorf("expected 0 orphaned overlays, got %d", len(overlays))
	}
}

func TestRecoverOrphanedOverlays_WithSessionID(t *testing.T) {
	// Create temp database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := persistence.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a test run and agent first (for foreign key)
	now := time.Now()
	run := &persistence.Run{
		ID:        "run-test",
		StartedAt: now,
		Status:    persistence.RunStatusRunning,
	}
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	agent := &persistence.Agent{
		ID:        "agent-test",
		RunID:     "run-test",
		TaskID:    "task-test",
		TaskTitle: "Test Task",
		Status:    persistence.AgentStatusRunning,
		StartedAt: now,
	}
	if err := store.CreateAgent(agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Create an active overlay WITH session_id
	overlay := &persistence.ActiveOverlay{
		AgentID:   "agent-test",
		TaskID:    "task-test",
		RunID:     "run-test",
		SessionID: "session-abc123", // Has session_id
		UpperDir:  filepath.Join(tmpDir, "upper"),
		MergedDir: filepath.Join(tmpDir, "merged"),
		LowerDir:  filepath.Join(tmpDir, "lower"),
		WorkDir:   filepath.Join(tmpDir, "work"),
		CreatedAt: now.Unix(),
		Status:    "active",
	}
	if err := store.TrackOverlay(overlay); err != nil {
		t.Fatalf("failed to track overlay: %v", err)
	}

	// Create the directories
	for _, dir := range []string{overlay.UpperDir, overlay.MergedDir, overlay.WorkDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create dir %s: %v", dir, err)
		}
	}

	// Create daemon with persistence
	d := &Daemon{
		persistManager: &PersistenceManager{
			store:   store,
			enabled: true,
		},
	}

	// Run recovery
	result, err := d.RecoverOrphanedOverlays(context.Background())
	if err != nil {
		t.Fatalf("RecoverOrphanedOverlays failed: %v", err)
	}

	// Should have 1 resumable overlay (has session_id)
	if result.Resumable != 1 {
		t.Errorf("expected 1 resumable overlay, got %d", result.Resumable)
	}
	// Cleaned should be 0 since it has session_id (though it's still cleaned up)
	if result.Cleaned != 0 {
		t.Errorf("expected 0 cleaned overlays, got %d", result.Cleaned)
	}
}
