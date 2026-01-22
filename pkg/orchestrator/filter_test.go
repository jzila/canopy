package orchestrator

import (
	"testing"

	"github.com/jzila/canopy/pkg/beads"
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
