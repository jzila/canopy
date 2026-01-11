package resolver

import (
	"testing"
)

func TestBuildResolverPrompt(t *testing.T) {
	r := &Resolver{
		config: &Config{
			Verbose: false,
		},
	}

	conflict := &ConflictContext{
		TaskID:          "canopy-abc",
		TaskTitle:       "Add feature X",
		TaskDescription: "Implement feature X with proper error handling",
		FailedPatches:   []string{"patch-content-1", "patch-content-2"},
		PatchErrors:     []string{"error 1", "error 2"},
		ParentAgentID:   "agent-canopy-abc",
	}

	prompt := r.buildResolverPrompt(conflict)

	// Verify the prompt contains key information
	if prompt == "" {
		t.Error("Expected non-empty prompt")
	}

	// Check for task ID
	if !contains(prompt, "canopy-abc") {
		t.Error("Prompt should contain task ID")
	}

	// Check for task title
	if !contains(prompt, "Add feature X") {
		t.Error("Prompt should contain task title")
	}

	// Check for task description
	if !contains(prompt, "Implement feature X") {
		t.Error("Prompt should contain task description")
	}

	// Check for conflict resolution instructions
	if !contains(prompt, "Merge Conflict Resolution") {
		t.Error("Prompt should contain conflict resolution header")
	}

	// Check for patch file references
	if !contains(prompt, ".canopy/conflict") {
		t.Error("Prompt should reference patch file location")
	}
}

func TestConflictContextParentAgentID(t *testing.T) {
	conflict := &ConflictContext{
		TaskID:        "canopy-xyz",
		ParentAgentID: "agent-canopy-xyz",
	}

	if conflict.ParentAgentID != "agent-canopy-xyz" {
		t.Errorf("Expected ParentAgentID to be 'agent-canopy-xyz', got '%s'", conflict.ParentAgentID)
	}
}

func TestResultResolverAgentID(t *testing.T) {
	result := &Result{
		Success:         true,
		ResolverAgentID: "canopy-abc-resolver",
	}

	if result.ResolverAgentID != "canopy-abc-resolver" {
		t.Errorf("Expected ResolverAgentID to be 'canopy-abc-resolver', got '%s'", result.ResolverAgentID)
	}
}

func TestNewResolver(t *testing.T) {
	config := &Config{
		WorkDir: "/tmp/test",
		TempDir: "/tmp/canopy",
		Verbose: true,
		UseBwrap: false,
	}

	r := New(config)

	if r == nil {
		t.Fatal("Expected New to return non-nil resolver")
	}

	if r.config != config {
		t.Error("Expected resolver config to match input config")
	}

	if r.executor == nil {
		t.Error("Expected resolver to have non-nil executor")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
