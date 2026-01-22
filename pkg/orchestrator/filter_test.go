package orchestrator

import (
	"testing"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/config"
)

func TestFilterTasksByMaxPriority(t *testing.T) {
	tasks := []beads.Task{
		{ID: "task-1", Title: "P0 critical task", Priority: 0},
		{ID: "task-2", Title: "P1 high task", Priority: 1},
		{ID: "task-3", Title: "P2 medium task", Priority: 2},
		{ID: "task-4", Title: "P3 low task", Priority: 3},
		{ID: "task-5", Title: "P4 backlog task", Priority: 4},
	}

	tests := []struct {
		name        string
		maxPriority int
		expected    []string // expected task IDs
	}{
		{
			name:        "no filter (maxPriority -1)",
			maxPriority: -1,
			expected:    []string{"task-1", "task-2", "task-3", "task-4", "task-5"},
		},
		{
			name:        "P0 only",
			maxPriority: 0,
			expected:    []string{"task-1"},
		},
		{
			name:        "P0 and P1",
			maxPriority: 1,
			expected:    []string{"task-1", "task-2"},
		},
		{
			name:        "P0, P1, P2",
			maxPriority: 2,
			expected:    []string{"task-1", "task-2", "task-3"},
		},
		{
			name:        "P0 through P3",
			maxPriority: 3,
			expected:    []string{"task-1", "task-2", "task-3", "task-4"},
		},
		{
			name:        "all priorities (P4)",
			maxPriority: 4,
			expected:    []string{"task-1", "task-2", "task-3", "task-4", "task-5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := filterTasksByMaxPriority(tasks, tt.maxPriority)

			if len(filtered) != len(tt.expected) {
				t.Errorf("filterTasksByMaxPriority() returned %d tasks, want %d", len(filtered), len(tt.expected))
				return
			}

			for i, task := range filtered {
				if task.ID != tt.expected[i] {
					t.Errorf("filterTasksByMaxPriority()[%d].ID = %s, want %s", i, task.ID, tt.expected[i])
				}
			}
		})
	}
}

func TestFilterTasksByMaxPriorityEmptyInput(t *testing.T) {
	var tasks []beads.Task

	filtered := filterTasksByMaxPriority(tasks, 2)
	if len(filtered) != 0 {
		t.Errorf("filterTasksByMaxPriority() with empty input returned %d tasks, want 0", len(filtered))
	}
}

func TestFilterTasksByMaxPriorityNoneMatch(t *testing.T) {
	tasks := []beads.Task{
		{ID: "task-1", Title: "P3 low task", Priority: 3},
		{ID: "task-2", Title: "P4 backlog task", Priority: 4},
	}

	filtered := filterTasksByMaxPriority(tasks, 1)
	if len(filtered) != 0 {
		t.Errorf("filterTasksByMaxPriority() with no matching tasks returned %d tasks, want 0", len(filtered))
	}
}

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
