package orchestrator

import (
	"testing"

	"github.com/jzila/canopy/pkg/config"
)

// Tests for mergeRulesSettings function.
// Note: The legacy filterTasksByMaxPriority function was removed - all filtering
// now goes through the rules engine via filterTasks() -> filterTasksWithEngine().

func TestMergeRulesSettings_NoOverrides(t *testing.T) {
	base := &config.RulesSettings{
		PriorityMin: 0,
		PriorityMax: 2,
		Types:       []string{"bug", "feature"},
		Assignee:    "john",
	}

	// When no CLI overrides, should use base as-is
	result := mergeRulesSettings(base, nil, nil)

	if result.PriorityMax != 2 {
		t.Errorf("PriorityMax = %d, want 2", result.PriorityMax)
	}
	if len(result.Types) != 2 || result.Types[0] != "bug" {
		t.Errorf("Types = %v, want [bug feature]", result.Types)
	}
	if result.Assignee != "john" {
		t.Errorf("Assignee = %q, want %q", result.Assignee, "john")
	}
}

func TestMergeRulesSettings_WithOverrides(t *testing.T) {
	base := &config.RulesSettings{
		PriorityMin: 0,
		PriorityMax: 2,
		Types:       []string{"bug", "feature"},
		Assignee:    "john",
		Labels:      []string{"backend"},
	}

	cliRules := &config.RulesSettings{
		PriorityMax: 1,            // Override to 1
		Types:       []string{},   // Empty (not set)
		Assignee:    "",           // Explicitly set to unassigned
		Labels:      []string{},   // Empty (not set)
	}

	overrides := &RulesOverrides{
		PriorityMax: true, // Explicitly set
		Assignee:    true, // Explicitly set to ""
		// Types and Labels not set
	}

	result := mergeRulesSettings(base, cliRules, overrides)

	// PriorityMax should be overridden to 1
	if result.PriorityMax != 1 {
		t.Errorf("PriorityMax = %d, want 1 (overridden)", result.PriorityMax)
	}

	// Assignee should be overridden to "" (unassigned)
	if result.Assignee != "" {
		t.Errorf("Assignee = %q, want %q (overridden to unassigned)", result.Assignee, "")
	}

	// Types should remain from base (not overridden)
	if len(result.Types) != 2 || result.Types[0] != "bug" {
		t.Errorf("Types = %v, want [bug feature] (from base)", result.Types)
	}

	// Labels should remain from base (not overridden)
	if len(result.Labels) != 1 || result.Labels[0] != "backend" {
		t.Errorf("Labels = %v, want [backend] (from base)", result.Labels)
	}
}

func TestMergeRulesSettings_LegacyFallback(t *testing.T) {
	// Test legacy behavior when overrides is nil but cliRules is provided
	base := &config.RulesSettings{
		PriorityMin: 0,
		PriorityMax: 2,
		Types:       []string{"bug"},
		Assignee:    "john",
	}

	cliRules := &config.RulesSettings{
		PriorityMax: 1,                     // Non-default, should override
		Types:       []string{"task"},      // Non-empty, should override
		Assignee:    "*",                   // Default, should NOT override
	}

	// No overrides tracking (legacy behavior)
	result := mergeRulesSettings(base, cliRules, nil)

	if result.PriorityMax != 1 {
		t.Errorf("PriorityMax = %d, want 1 (overridden)", result.PriorityMax)
	}

	if len(result.Types) != 1 || result.Types[0] != "task" {
		t.Errorf("Types = %v, want [task] (overridden)", result.Types)
	}

	// Assignee should remain "john" because "*" is the default value
	if result.Assignee != "john" {
		t.Errorf("Assignee = %q, want %q (not overridden)", result.Assignee, "john")
	}
}
