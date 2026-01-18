package resolver

import (
	"strings"
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
		BaseCommit:      "abc123def456",
	}

	prompt := r.buildResolverPrompt(conflict)

	// Verify the prompt contains key information
	if prompt == "" {
		t.Error("Expected non-empty prompt")
	}

	// Check for task ID
	if !strings.Contains(prompt, "canopy-abc") {
		t.Error("Prompt should contain task ID")
	}

	// Check for task title
	if !strings.Contains(prompt, "Add feature X") {
		t.Error("Prompt should contain task title")
	}

	// Check for task description
	if !strings.Contains(prompt, "Implement feature X") {
		t.Error("Prompt should contain task description")
	}

	// Check for conflict resolution instructions
	if !strings.Contains(prompt, "Three-Way Merge Resolution") {
		t.Error("Prompt should contain three-way merge resolution header")
	}

	// Check for patch file references
	if !strings.Contains(prompt, ".canopy/conflict") {
		t.Error("Prompt should reference patch file location")
	}

	// Check for base commit reference
	if !strings.Contains(prompt, "abc123def456") {
		t.Error("Prompt should contain base commit hash")
	}

	// Check for three-way merge terminology
	if !strings.Contains(prompt, "BASE") || !strings.Contains(prompt, "OURS") || !strings.Contains(prompt, "THEIRS") {
		t.Error("Prompt should use BASE/OURS/THEIRS terminology")
	}

	// Check for concurrent changes explanation
	if !strings.Contains(prompt, "concurrent") {
		t.Error("Prompt should explain concurrent changes")
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
		WorkDir:  t.TempDir(),
		TempDir:  t.TempDir(),
		Verbose:  true,
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
