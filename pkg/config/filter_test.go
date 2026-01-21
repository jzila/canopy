package config

import (
	"testing"

	"github.com/jzila/canopy/pkg/beads"
)

func TestTaskFilterPriority(t *testing.T) {
	tasks := []beads.Task{
		{ID: "1", Priority: 0},
		{ID: "2", Priority: 1},
		{ID: "3", Priority: 2},
		{ID: "4", Priority: 3},
		{ID: "5", Priority: 4},
	}

	tests := []struct {
		name     string
		rules    RulesSettings
		expected []string
	}{
		{
			name: "no filter (default)",
			rules: RulesSettings{
				PriorityMin: 0,
				PriorityMax: -1,
				Assignee:    "*",
			},
			expected: []string{"1", "2", "3", "4", "5"},
		},
		{
			name: "max priority 2",
			rules: RulesSettings{
				PriorityMin: 0,
				PriorityMax: 2,
				Assignee:    "*",
			},
			expected: []string{"1", "2", "3"},
		},
		{
			name: "min priority 2",
			rules: RulesSettings{
				PriorityMin: 2,
				PriorityMax: -1,
				Assignee:    "*",
			},
			expected: []string{"3", "4", "5"},
		},
		{
			name: "priority range 1-3",
			rules: RulesSettings{
				PriorityMin: 1,
				PriorityMax: 3,
				Assignee:    "*",
			},
			expected: []string{"2", "3", "4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewTaskFilter(&tt.rules)
			result := filter.FilterTasks(tasks)

			if len(result) != len(tt.expected) {
				t.Errorf("FilterTasks() returned %d tasks, want %d", len(result), len(tt.expected))
				return
			}

			for i, task := range result {
				if task.ID != tt.expected[i] {
					t.Errorf("FilterTasks()[%d].ID = %s, want %s", i, task.ID, tt.expected[i])
				}
			}
		})
	}
}

func TestTaskFilterType(t *testing.T) {
	tasks := []beads.Task{
		{ID: "1", Type: "bug"},
		{ID: "2", Type: "feature"},
		{ID: "3", Type: "task"},
		{ID: "4", Type: "chore"},
		{ID: "5", Type: "epic"},
	}

	tests := []struct {
		name     string
		rules    RulesSettings
		expected []string
	}{
		{
			name: "no filter",
			rules: RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
			},
			expected: []string{"1", "2", "3", "4", "5"},
		},
		{
			name: "only bugs",
			rules: RulesSettings{
				PriorityMax: -1,
				Types:       []string{"bug"},
				Assignee:    "*",
			},
			expected: []string{"1"},
		},
		{
			name: "bugs and tasks",
			rules: RulesSettings{
				PriorityMax: -1,
				Types:       []string{"bug", "task"},
				Assignee:    "*",
			},
			expected: []string{"1", "3"},
		},
		{
			name: "exclude epic",
			rules: RulesSettings{
				PriorityMax:  -1,
				ExcludeTypes: []string{"epic"},
				Assignee:     "*",
			},
			expected: []string{"1", "2", "3", "4"},
		},
		{
			name: "exclude epic and chore",
			rules: RulesSettings{
				PriorityMax:  -1,
				ExcludeTypes: []string{"epic", "chore"},
				Assignee:     "*",
			},
			expected: []string{"1", "2", "3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewTaskFilter(&tt.rules)
			result := filter.FilterTasks(tasks)

			if len(result) != len(tt.expected) {
				t.Errorf("FilterTasks() returned %d tasks, want %d", len(result), len(tt.expected))
				return
			}

			for i, task := range result {
				if task.ID != tt.expected[i] {
					t.Errorf("FilterTasks()[%d].ID = %s, want %s", i, task.ID, tt.expected[i])
				}
			}
		})
	}
}

func TestTaskFilterLabels(t *testing.T) {
	tasks := []beads.Task{
		{ID: "1", Labels: []string{"frontend"}},
		{ID: "2", Labels: []string{"backend"}},
		{ID: "3", Labels: []string{"frontend", "urgent"}},
		{ID: "4", Labels: []string{"backend", "wip"}},
		{ID: "5", Labels: nil},
	}

	tests := []struct {
		name     string
		rules    RulesSettings
		expected []string
	}{
		{
			name: "no filter",
			rules: RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
			},
			expected: []string{"1", "2", "3", "4", "5"},
		},
		{
			name: "only frontend",
			rules: RulesSettings{
				PriorityMax: -1,
				Labels:      []string{"frontend"},
				Assignee:    "*",
			},
			expected: []string{"1", "3"},
		},
		{
			name: "frontend or backend",
			rules: RulesSettings{
				PriorityMax: -1,
				Labels:      []string{"frontend", "backend"},
				Assignee:    "*",
			},
			expected: []string{"1", "2", "3", "4"},
		},
		{
			name: "exclude wip",
			rules: RulesSettings{
				PriorityMax:   -1,
				ExcludeLabels: []string{"wip"},
				Assignee:      "*",
			},
			expected: []string{"1", "2", "3", "5"},
		},
		{
			name: "frontend but not urgent",
			rules: RulesSettings{
				PriorityMax:   -1,
				Labels:        []string{"frontend"},
				ExcludeLabels: []string{"urgent"},
				Assignee:      "*",
			},
			expected: []string{"1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewTaskFilter(&tt.rules)
			result := filter.FilterTasks(tasks)

			if len(result) != len(tt.expected) {
				t.Errorf("FilterTasks() returned %d tasks, want %d", len(result), len(tt.expected))
				return
			}

			for i, task := range result {
				if task.ID != tt.expected[i] {
					t.Errorf("FilterTasks()[%d].ID = %s, want %s", i, task.ID, tt.expected[i])
				}
			}
		})
	}
}

func TestTaskFilterAssignee(t *testing.T) {
	tasks := []beads.Task{
		{ID: "1", Assignee: ""},
		{ID: "2", Assignee: "alice"},
		{ID: "3", Assignee: "bob"},
		{ID: "4", Assignee: ""},
	}

	tests := []struct {
		name     string
		rules    RulesSettings
		expected []string
	}{
		{
			name: "any assignee (*)",
			rules: RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
			},
			expected: []string{"1", "2", "3", "4"},
		},
		{
			name: "unassigned only",
			rules: RulesSettings{
				PriorityMax: -1,
				Assignee:    "",
			},
			expected: []string{"1", "4"},
		},
		{
			name: "specific assignee",
			rules: RulesSettings{
				PriorityMax: -1,
				Assignee:    "alice",
			},
			expected: []string{"2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewTaskFilter(&tt.rules)
			result := filter.FilterTasks(tasks)

			if len(result) != len(tt.expected) {
				t.Errorf("FilterTasks() returned %d tasks, want %d", len(result), len(tt.expected))
				return
			}

			for i, task := range result {
				if task.ID != tt.expected[i] {
					t.Errorf("FilterTasks()[%d].ID = %s, want %s", i, task.ID, tt.expected[i])
				}
			}
		})
	}
}

func TestTaskFilterCustomRules(t *testing.T) {
	tasks := []beads.Task{
		{ID: "1", Priority: 0, Type: "bug"},
		{ID: "2", Priority: 1, Type: "feature"},
		{ID: "3", Priority: 2, Type: "task"},
		{ID: "4", Priority: 3, Type: "bug"},
	}

	trueVal := true
	falseVal := false

	tests := []struct {
		name     string
		rules    RulesSettings
		expected []string
	}{
		{
			name: "skip priority > 1",
			rules: RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []CustomRule{
					{Name: "skip-low-priority", Condition: "priority > 1", Action: "skip"},
				},
			},
			expected: []string{"1", "2"},
		},
		{
			name: "disabled rule",
			rules: RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []CustomRule{
					{Name: "skip-low-priority", Enabled: &falseVal, Condition: "priority > 1", Action: "skip"},
				},
			},
			expected: []string{"1", "2", "3", "4"},
		},
		{
			name: "enabled rule",
			rules: RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []CustomRule{
					{Name: "skip-low-priority", Enabled: &trueVal, Condition: "priority > 1", Action: "skip"},
				},
			},
			expected: []string{"1", "2"},
		},
		{
			name: "skip type == bug",
			rules: RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []CustomRule{
					{Name: "skip-bugs", Condition: "type == bug", Action: "skip"},
				},
			},
			expected: []string{"2", "3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewTaskFilter(&tt.rules)
			result := filter.FilterTasks(tasks)

			if len(result) != len(tt.expected) {
				t.Errorf("FilterTasks() returned %d tasks, want %d", len(result), len(tt.expected))
				return
			}

			for i, task := range result {
				if task.ID != tt.expected[i] {
					t.Errorf("FilterTasks()[%d].ID = %s, want %s", i, task.ID, tt.expected[i])
				}
			}
		})
	}
}

func TestEvaluateCondition(t *testing.T) {
	task := &beads.Task{
		Priority: 2,
		Type:     "bug",
		Assignee: "alice",
	}

	tests := []struct {
		condition string
		expected  bool
	}{
		// Priority conditions
		{"priority == 2", true},
		{"priority != 2", false},
		{"priority < 3", true},
		{"priority > 1", true},
		{"priority <= 2", true},
		{"priority >= 2", true},
		{"priority < 2", false},
		{"priority > 2", false},

		// Type conditions
		{"type == bug", true},
		{"type != bug", false},
		{"type == feature", false},

		// Assignee conditions
		{"assignee == alice", true},
		{"assignee != alice", false},
		{"assignee == bob", false},

		// Invalid conditions
		{"invalid condition", false},
		{"unknown_field == value", false},
		{"priority == invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			result := evaluateCondition(tt.condition, task)
			if result != tt.expected {
				t.Errorf("evaluateCondition(%q) = %v, want %v", tt.condition, result, tt.expected)
			}
		})
	}
}

func TestTaskFilterNilRules(t *testing.T) {
	tasks := []beads.Task{
		{ID: "1"},
		{ID: "2"},
	}

	filter := NewTaskFilter(nil)
	result := filter.FilterTasks(tasks)

	if len(result) != 2 {
		t.Errorf("FilterTasks() with nil rules returned %d tasks, want 2", len(result))
	}
}

func TestTaskFilterEmpty(t *testing.T) {
	var tasks []beads.Task

	rules := RulesSettings{
		PriorityMax: -1,
		Assignee:    "*",
	}

	filter := NewTaskFilter(&rules)
	result := filter.FilterTasks(tasks)

	if len(result) != 0 {
		t.Errorf("FilterTasks() with empty input returned %d tasks, want 0", len(result))
	}
}

func TestGetSkipReason(t *testing.T) {
	task := &beads.Task{Priority: 3}

	tests := []struct {
		name     string
		rules    RulesSettings
		expected string
	}{
		{
			name: "no custom rules",
			rules: RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
			},
			expected: "",
		},
		{
			name: "skip rule with reason",
			rules: RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []CustomRule{
					{Name: "low-priority", Condition: "priority > 2", Action: "skip", Reason: "Skipping low priority tasks"},
				},
			},
			expected: "Skipping low priority tasks",
		},
		{
			name: "skip rule without reason",
			rules: RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []CustomRule{
					{Name: "low-priority", Condition: "priority > 2", Action: "skip"},
				},
			},
			expected: "Skipped by rule: low-priority",
		},
		{
			name: "non-matching rule",
			rules: RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []CustomRule{
					{Name: "high-priority", Condition: "priority < 2", Action: "skip", Reason: "test"},
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewTaskFilter(&tt.rules)
			result := filter.GetSkipReason(task)
			if result != tt.expected {
				t.Errorf("GetSkipReason() = %q, want %q", result, tt.expected)
			}
		})
	}
}
