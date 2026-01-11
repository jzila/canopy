package orchestrator

import (
	"reflect"
	"testing"
)

func TestParsePrompt(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		expected *PromptFilter
	}{
		{
			name:   "empty prompt",
			prompt: "",
			expected: &PromptFilter{
				Priority:          -1,
				StopAfterPriority: -1,
			},
		},
		{
			name:   "P0 filter",
			prompt: "Only work on P0 issues",
			expected: &PromptFilter{
				Priority:          0,
				StopAfterPriority: -1,
			},
		},
		{
			name:   "P1 filter",
			prompt: "Focus on P1 items",
			expected: &PromptFilter{
				Priority:          1,
				StopAfterPriority: -1,
			},
		},
		{
			name:   "tasks only",
			prompt: "Only work on tasks",
			expected: &PromptFilter{
				Priority:          -1,
				Type:              "task",
				StopAfterPriority: -1,
			},
		},
		{
			name:   "bugs only",
			prompt: "Focus on bugs",
			expected: &PromptFilter{
				Priority:          -1,
				Type:              "bug",
				StopAfterPriority: -1,
			},
		},
		{
			name:   "features only",
			prompt: "Work on features",
			expected: &PromptFilter{
				Priority:          -1,
				Type:              "feature",
				StopAfterPriority: -1,
			},
		},
		{
			name:   "stop after P1s",
			prompt: "Stop after completing all P1s",
			expected: &PromptFilter{
				Priority:          -1,
				StopAfterPriority: 1,
				StopCondition:     "Stop after completing all P1s",
			},
		},
		{
			name:   "stop after P0s",
			prompt: "Stop after P0",
			expected: &PromptFilter{
				Priority:          -1,
				StopAfterPriority: 0,
				StopCondition:     "Stop after P0",
			},
		},
		{
			name:   "stop after tasks",
			prompt: "Stop after completing all tasks",
			expected: &PromptFilter{
				Priority:          -1,
				StopAfterPriority: -1,
				StopAfterType:     "task",
				StopCondition:     "Stop after completing all tasks",
			},
		},
		{
			name:   "P0 tasks with stop condition",
			prompt: "Work on P0 tasks and stop after completing all tasks",
			expected: &PromptFilter{
				Priority:          0,
				Type:              "task",
				StopAfterPriority: -1,
				StopAfterType:     "task",
				StopCondition:     "Work on P0 tasks and stop after completing all tasks",
			},
		},
		{
			name:   "merge requests",
			prompt: "Focus on merge requests",
			expected: &PromptFilter{
				Priority:          -1,
				Type:              "merge-request",
				StopAfterPriority: -1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePrompt(tt.prompt)
			if err != nil {
				t.Errorf("ParsePrompt() error = %v", err)
				return
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("ParsePrompt() = %+v, want %+v", got, tt.expected)
			}
		})
	}
}

func TestBuildBdReadyArgs(t *testing.T) {
	tests := []struct {
		name     string
		filter   *PromptFilter
		expected []string
	}{
		{
			name: "no filters",
			filter: &PromptFilter{
				Priority: -1,
			},
			expected: []string{"ready", "--json"},
		},
		{
			name: "priority filter",
			filter: &PromptFilter{
				Priority: 0,
			},
			expected: []string{"ready", "--json", "--priority", "0"},
		},
		{
			name: "type filter",
			filter: &PromptFilter{
				Priority: -1,
				Type:     "task",
			},
			expected: []string{"ready", "--json", "--type", "task"},
		},
		{
			name: "combined filters",
			filter: &PromptFilter{
				Priority: 1,
				Type:     "bug",
			},
			expected: []string{"ready", "--json", "--priority", "1", "--type", "bug"},
		},
		{
			name: "with labels",
			filter: &PromptFilter{
				Priority: -1,
				Labels:   []string{"frontend", "urgent"},
			},
			expected: []string{"ready", "--json", "--label", "frontend", "--label", "urgent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.BuildBdReadyArgs()
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("BuildBdReadyArgs() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestShouldStop(t *testing.T) {
	tests := []struct {
		name           string
		filter         *PromptFilter
		remainingTasks int
		expected       bool
	}{
		{
			name: "no stop condition",
			filter: &PromptFilter{
				StopCondition: "",
			},
			remainingTasks: 5,
			expected:       false,
		},
		{
			name: "stop after priority with tasks remaining",
			filter: &PromptFilter{
				StopCondition:     "Stop after P0",
				StopAfterPriority: 0,
			},
			remainingTasks: 3,
			expected:       false,
		},
		{
			name: "stop after priority with no tasks remaining",
			filter: &PromptFilter{
				StopCondition:     "Stop after P0",
				StopAfterPriority: 0,
			},
			remainingTasks: 0,
			expected:       true,
		},
		{
			name: "stop after type with tasks remaining",
			filter: &PromptFilter{
				StopCondition: "Stop after tasks",
				StopAfterType: "task",
			},
			remainingTasks: 2,
			expected:       false,
		},
		{
			name: "stop after type with no tasks remaining",
			filter: &PromptFilter{
				StopCondition: "Stop after tasks",
				StopAfterType: "task",
			},
			remainingTasks: 0,
			expected:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.ShouldStop(tt.remainingTasks)
			if got != tt.expected {
				t.Errorf("ShouldStop(%d) = %v, want %v", tt.remainingTasks, got, tt.expected)
			}
		})
	}
}
