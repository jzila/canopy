package repairagent

import (
	"os"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/validation"
)

func TestMakeAgentID(t *testing.T) {
	tests := []struct {
		name       string
		runID      string
		taskID     string
		attemptNum int
		want       string
	}{
		{
			name:       "normal case",
			runID:      "run-12345678-abcd",
			taskID:     "task-xyz",
			attemptNum: 1,
			want:       "agent-run-1234-task-xyz-repair-1",
		},
		{
			name:       "short runID",
			runID:      "abc",
			taskID:     "task-123",
			attemptNum: 2,
			want:       "agent-abc-task-123-repair-2",
		},
		{
			name:       "empty runID fallback",
			runID:      "",
			taskID:     "task-456",
			attemptNum: 1,
			want:       "task-456-repair-1",
		},
		{
			name:       "exactly 8 char runID",
			runID:      "12345678",
			taskID:     "task-foo",
			attemptNum: 3,
			want:       "agent-12345678-task-foo-repair-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RepairAgent{
				config: &Config{
					RunID: tt.runID,
				},
			}
			got := r.makeAgentID(tt.taskID, tt.attemptNum)
			if got != tt.want {
				t.Errorf("makeAgentID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewRepairAgent(t *testing.T) {
	config := &Config{
		WorkDir: "/tmp/test",
		TempDir: "/tmp/temp",
		Verbose: true,
		RunID:   "test-run-id",
		RepoID:  "test-repo-id",
	}

	agent := New(config)

	if agent == nil {
		t.Fatal("New() returned nil")
	}
	if agent.config != config {
		t.Error("New() did not set config correctly")
	}
	if agent.executor == nil {
		t.Error("New() did not create executor")
	}
	if agent.ipcClient != nil {
		t.Error("New() should not set ipcClient by default")
	}
}

func TestSetters(t *testing.T) {
	config := &Config{
		WorkDir: "/tmp/test",
	}
	agent := New(config)

	// Test SetRunID
	agent.SetRunID("new-run-id")
	if agent.config.RunID != "new-run-id" {
		t.Errorf("SetRunID() did not update config, got %q", agent.config.RunID)
	}

	// Test SetRepoID
	agent.SetRepoID("new-repo-id")
	if agent.config.RepoID != "new-repo-id" {
		t.Errorf("SetRepoID() did not update config, got %q", agent.config.RepoID)
	}
}

func TestCleanupRepairContext(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a repair context to clean up
	ctx := &RepairContext{
		TaskID:    "test-task",
		TaskTitle: "Test Task",
		FailedStep: &validation.StepResult{
			Name:    "test",
			Command: "test cmd",
		},
		MergedDiff:        "some diff",
		RepairAttempt:     1,
		MaxRepairAttempts: 3,
	}

	// Write context files
	err := WriteRepairContext(tmpDir, ctx)
	if err != nil {
		t.Fatalf("WriteRepairContext() failed: %v", err)
	}

	// Verify files exist
	repairDir := tmpDir + "/.canopy/repair"
	if _, err := statDir(repairDir); err != nil {
		t.Fatalf("repair directory should exist: %v", err)
	}

	// Clean up
	err = CleanupRepairContext(tmpDir)
	if err != nil {
		t.Fatalf("CleanupRepairContext() failed: %v", err)
	}

	// Verify cleanup
	if _, err := statDir(repairDir); err == nil {
		t.Error("repair directory should not exist after cleanup")
	}
}

func TestCleanupRepairContext_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()

	// Should not error on non-existent directory
	err := CleanupRepairContext(tmpDir)
	if err != nil {
		t.Errorf("CleanupRepairContext() should not error on non-existent dir: %v", err)
	}
}

func TestResultFields(t *testing.T) {
	result := &Result{
		Success:       true,
		Error:         "",
		Duration:      5 * time.Second,
		RepairAgentID: "agent-test-repair-1",
	}

	if !result.Success {
		t.Error("result.Success should be true")
	}
	if result.Duration != 5*time.Second {
		t.Error("result.Duration not set correctly")
	}
	if result.RepairAgentID != "agent-test-repair-1" {
		t.Error("result.RepairAgentID not set correctly")
	}
}

func TestConfigFields(t *testing.T) {
	config := &Config{
		WorkDir: "/work",
		TempDir: "/temp",
		Verbose: true,
		UseBwrap: false,
		RepoID:  "repo-123",
		RunID:   "run-456",
	}

	if config.WorkDir != "/work" {
		t.Error("WorkDir not set correctly")
	}
	if config.TempDir != "/temp" {
		t.Error("TempDir not set correctly")
	}
	if !config.Verbose {
		t.Error("Verbose not set correctly")
	}
	if config.UseBwrap {
		t.Error("UseBwrap should be false")
	}
	if config.RepoID != "repo-123" {
		t.Error("RepoID not set correctly")
	}
	if config.RunID != "run-456" {
		t.Error("RunID not set correctly")
	}
}

// statDir is a helper to check if a directory exists
func statDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}
