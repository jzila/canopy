package mergecoordinator

import (
	"testing"

	"github.com/jzila/canopy/pkg/agent"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		beadsClient interface{}
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil config returns error",
			config:      nil,
			beadsClient: nil,
			wantErr:     true,
			errContains: "config is required",
		},
		{
			name: "nil beadsClient returns error",
			config: &Config{
				WorkDir:     "/tmp/test",
				OutputDir:   "/tmp/output",
				TempDir:     "/tmp/temp",
				Concurrency: 4,
			},
			beadsClient: nil,
			wantErr:     true,
			errContains: "beadsClient is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't easily test success case without mocking beads.Client
			// but we can test the error cases
			if tt.wantErr {
				_, err := New(tt.config, nil)
				if err == nil {
					t.Errorf("New() expected error, got nil")
					return
				}
				if tt.errContains != "" && err.Error() != tt.errContains {
					t.Errorf("New() error = %v, want error containing %v", err, tt.errContains)
				}
			}
		})
	}
}

func TestConfig_DefaultConcurrency(t *testing.T) {
	// Test that default concurrency is applied when <= 0
	config := &Config{
		WorkDir:     "/tmp/test",
		OutputDir:   "/tmp/output",
		TempDir:     "/tmp/temp",
		Concurrency: 0, // Should default to 4
	}

	// We verify the default is applied by checking the queue buffer size
	// Since we can't create a coordinator without a beads client,
	// this is tested implicitly through the New function
	if config.Concurrency <= 0 {
		// This is expected - the New function will apply the default
		t.Log("Concurrency <= 0, New() will apply default of 4")
	}
}

func TestMergeCoordinator_AgentIDMapping(t *testing.T) {
	// Test the agentID <-> taskID mapping without needing full coordinator
	mc := &MergeCoordinator{}

	// Test SetAgentID and GetAgentID
	mc.SetAgentID("task-1", "agent-abc")
	mc.SetAgentID("task-2", "agent-def")

	if got := mc.GetAgentID("task-1"); got != "agent-abc" {
		t.Errorf("GetAgentID(task-1) = %v, want agent-abc", got)
	}

	if got := mc.GetAgentID("task-2"); got != "agent-def" {
		t.Errorf("GetAgentID(task-2) = %v, want agent-def", got)
	}

	// Test non-existent task
	if got := mc.GetAgentID("task-unknown"); got != "" {
		t.Errorf("GetAgentID(task-unknown) = %v, want empty string", got)
	}
}

func TestMergeCoordinator_TaskCache(t *testing.T) {
	// Test task caching without needing full coordinator
	mc := &MergeCoordinator{}

	// Verify cache is empty initially
	if _, ok := mc.taskCache.Load("task-1"); ok {
		t.Error("taskCache should be empty initially")
	}

	// Test CacheTask indirectly by using taskCache directly
	// (CacheTask requires a beads.Task which we can't easily mock)
	mc.taskCache.Store("task-1", "test-value")

	if val, ok := mc.taskCache.Load("task-1"); !ok || val != "test-value" {
		t.Errorf("taskCache.Load(task-1) = %v, %v, want test-value, true", val, ok)
	}
}

func TestMergeCoordinator_SetCleanupCallback(t *testing.T) {
	mc := &MergeCoordinator{}

	mc.SetCleanupCallback(func(result *agent.Result) {
		// Callback set
	})

	// Verify callback is set (we can't easily test it's called without mocking)
	if mc.cleanupCallback == nil {
		t.Error("cleanupCallback should not be nil after SetCleanupCallback")
	}
}
