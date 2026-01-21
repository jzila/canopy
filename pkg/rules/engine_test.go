package rules

import (
	"testing"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/config"
)

func TestEngineEvaluatePriorityRange(t *testing.T) {
	tests := []struct {
		name       string
		config     *config.RulesSettings
		task       *beads.Task
		wantSkip   bool
		wantReason string
	}{
		{
			name: "within range",
			config: &config.RulesSettings{
				PriorityMin: 0,
				PriorityMax: 2,
				Assignee:    "*",
			},
			task:     &beads.Task{ID: "1", Priority: 1},
			wantSkip: false,
		},
		{
			name: "below minimum",
			config: &config.RulesSettings{
				PriorityMin: 2,
				PriorityMax: 4,
				Assignee:    "*",
			},
			task:       &beads.Task{ID: "1", Priority: 1},
			wantSkip:   true,
			wantReason: "below priority minimum",
		},
		{
			name: "above maximum",
			config: &config.RulesSettings{
				PriorityMin: 0,
				PriorityMax: 2,
				Assignee:    "*",
			},
			task:       &beads.Task{ID: "1", Priority: 3},
			wantSkip:   true,
			wantReason: "above priority maximum",
		},
		{
			name: "no max filter (-1)",
			config: &config.RulesSettings{
				PriorityMin: 0,
				PriorityMax: -1,
				Assignee:    "*",
			},
			task:     &beads.Task{ID: "1", Priority: 4},
			wantSkip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(tt.config)
			result := engine.Evaluate(tt.task, nil, nil)

			if result.Skip != tt.wantSkip {
				t.Errorf("Skip = %v, want %v", result.Skip, tt.wantSkip)
			}
			if tt.wantSkip && result.SkipReason != tt.wantReason {
				t.Errorf("SkipReason = %q, want %q", result.SkipReason, tt.wantReason)
			}
		})
	}
}

func TestEngineEvaluateTypeFilters(t *testing.T) {
	tests := []struct {
		name       string
		config     *config.RulesSettings
		task       *beads.Task
		wantSkip   bool
		wantReason string
	}{
		{
			name: "type in whitelist",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Types:       []string{"bug", "feature"},
				Assignee:    "*",
			},
			task:     &beads.Task{ID: "1", Type: "bug"},
			wantSkip: false,
		},
		{
			name: "type not in whitelist",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Types:       []string{"bug", "feature"},
				Assignee:    "*",
			},
			task:       &beads.Task{ID: "1", Type: "chore"},
			wantSkip:   true,
			wantReason: "type not in whitelist",
		},
		{
			name: "type in blacklist",
			config: &config.RulesSettings{
				PriorityMax:  -1,
				ExcludeTypes: []string{"epic"},
				Assignee:     "*",
			},
			task:       &beads.Task{ID: "1", Type: "epic"},
			wantSkip:   true,
			wantReason: "type in blacklist",
		},
		{
			name: "type not in blacklist",
			config: &config.RulesSettings{
				PriorityMax:  -1,
				ExcludeTypes: []string{"epic"},
				Assignee:     "*",
			},
			task:     &beads.Task{ID: "1", Type: "bug"},
			wantSkip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(tt.config)
			result := engine.Evaluate(tt.task, nil, nil)

			if result.Skip != tt.wantSkip {
				t.Errorf("Skip = %v, want %v", result.Skip, tt.wantSkip)
			}
			if tt.wantSkip && result.SkipReason != tt.wantReason {
				t.Errorf("SkipReason = %q, want %q", result.SkipReason, tt.wantReason)
			}
		})
	}
}

func TestEngineEvaluateLabelFilters(t *testing.T) {
	tests := []struct {
		name       string
		config     *config.RulesSettings
		task       *beads.Task
		wantSkip   bool
		wantReason string
	}{
		{
			name: "has matching label",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Labels:      []string{"frontend", "backend"},
				Assignee:    "*",
			},
			task:     &beads.Task{ID: "1", Labels: []string{"frontend"}},
			wantSkip: false,
		},
		{
			name: "no matching label",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Labels:      []string{"frontend"},
				Assignee:    "*",
			},
			task:       &beads.Task{ID: "1", Labels: []string{"backend"}},
			wantSkip:   true,
			wantReason: "no matching label in whitelist",
		},
		{
			name: "has excluded label",
			config: &config.RulesSettings{
				PriorityMax:   -1,
				ExcludeLabels: []string{"wip"},
				Assignee:      "*",
			},
			task:       &beads.Task{ID: "1", Labels: []string{"frontend", "wip"}},
			wantSkip:   true,
			wantReason: "has excluded label",
		},
		{
			name: "no excluded label",
			config: &config.RulesSettings{
				PriorityMax:   -1,
				ExcludeLabels: []string{"wip"},
				Assignee:      "*",
			},
			task:     &beads.Task{ID: "1", Labels: []string{"frontend"}},
			wantSkip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(tt.config)
			result := engine.Evaluate(tt.task, nil, nil)

			if result.Skip != tt.wantSkip {
				t.Errorf("Skip = %v, want %v", result.Skip, tt.wantSkip)
			}
			if tt.wantSkip && result.SkipReason != tt.wantReason {
				t.Errorf("SkipReason = %q, want %q", result.SkipReason, tt.wantReason)
			}
		})
	}
}

func TestEngineEvaluateAssigneeFilters(t *testing.T) {
	tests := []struct {
		name       string
		config     *config.RulesSettings
		task       *beads.Task
		wantSkip   bool
		wantReason string
	}{
		{
			name: "any assignee (*)",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
			},
			task:     &beads.Task{ID: "1", Assignee: "alice"},
			wantSkip: false,
		},
		{
			name: "unassigned only - has assignee",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "",
			},
			task:       &beads.Task{ID: "1", Assignee: "alice"},
			wantSkip:   true,
			wantReason: "task is assigned",
		},
		{
			name: "unassigned only - unassigned",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "",
			},
			task:     &beads.Task{ID: "1", Assignee: ""},
			wantSkip: false,
		},
		{
			name: "specific assignee - matches",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "alice",
			},
			task:     &beads.Task{ID: "1", Assignee: "alice"},
			wantSkip: false,
		},
		{
			name: "specific assignee - no match",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "alice",
			},
			task:       &beads.Task{ID: "1", Assignee: "bob"},
			wantSkip:   true,
			wantReason: `assignee is "bob", want "alice"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(tt.config)
			result := engine.Evaluate(tt.task, nil, nil)

			if result.Skip != tt.wantSkip {
				t.Errorf("Skip = %v, want %v", result.Skip, tt.wantSkip)
			}
			if tt.wantSkip && result.SkipReason != tt.wantReason {
				t.Errorf("SkipReason = %q, want %q", result.SkipReason, tt.wantReason)
			}
		})
	}
}

func TestEngineEvaluateConcurrencyLimits(t *testing.T) {
	tests := []struct {
		name          string
		config        *config.RulesSettings
		task          *beads.Task
		inFlightTasks map[string]*beads.Task
		wantSkip      bool
		wantReason    string
	}{
		{
			name: "type limit not reached",
			config: &config.RulesSettings{
				PriorityMax:          -1,
				Assignee:             "*",
				MaxConcurrentPerType: map[string]int{"bug": 2},
			},
			task: &beads.Task{ID: "3", Type: "bug"},
			inFlightTasks: map[string]*beads.Task{
				"1": {ID: "1", Type: "bug"},
			},
			wantSkip: false,
		},
		{
			name: "type limit reached",
			config: &config.RulesSettings{
				PriorityMax:          -1,
				Assignee:             "*",
				MaxConcurrentPerType: map[string]int{"bug": 2},
			},
			task: &beads.Task{ID: "3", Type: "bug"},
			inFlightTasks: map[string]*beads.Task{
				"1": {ID: "1", Type: "bug"},
				"2": {ID: "2", Type: "bug"},
			},
			wantSkip:   true,
			wantReason: `type "bug" at concurrency limit (2)`,
		},
		{
			name: "label limit not reached",
			config: &config.RulesSettings{
				PriorityMax:           -1,
				Assignee:              "*",
				MaxConcurrentPerLabel: map[string]int{"frontend": 1},
			},
			task: &beads.Task{ID: "2", Labels: []string{"frontend"}},
			inFlightTasks: map[string]*beads.Task{
				"1": {ID: "1", Labels: []string{"backend"}},
			},
			wantSkip: false,
		},
		{
			name: "label limit reached",
			config: &config.RulesSettings{
				PriorityMax:           -1,
				Assignee:              "*",
				MaxConcurrentPerLabel: map[string]int{"frontend": 1},
			},
			task: &beads.Task{ID: "2", Labels: []string{"frontend"}},
			inFlightTasks: map[string]*beads.Task{
				"1": {ID: "1", Labels: []string{"frontend"}},
			},
			wantSkip:   true,
			wantReason: `label "frontend" at concurrency limit (1)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(tt.config)
			result := engine.Evaluate(tt.task, nil, tt.inFlightTasks)

			if result.Skip != tt.wantSkip {
				t.Errorf("Skip = %v, want %v", result.Skip, tt.wantSkip)
			}
			if tt.wantSkip && result.SkipReason != tt.wantReason {
				t.Errorf("SkipReason = %q, want %q", result.SkipReason, tt.wantReason)
			}
		})
	}
}

func TestEngineEvaluateCustomRules(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name        string
		config      *config.RulesSettings
		task        *beads.Task
		wantSkip    bool
		wantReason  string
		wantBoost   int
		wantAllow   bool
	}{
		{
			name: "skip rule matches",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []config.CustomRule{
					{Name: "no-low-priority", Condition: "priority > 2", Action: "skip", Reason: "Skipping low priority"},
				},
			},
			task:       &beads.Task{ID: "1", Priority: 3},
			wantSkip:   true,
			wantReason: "Skipping low priority",
		},
		{
			name: "skip rule with default reason",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []config.CustomRule{
					{Name: "no-low-priority", Condition: "priority > 2", Action: "skip"},
				},
			},
			task:       &beads.Task{ID: "1", Priority: 3},
			wantSkip:   true,
			wantReason: "Skipped by rule: no-low-priority",
		},
		{
			name: "skip rule does not match",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []config.CustomRule{
					{Name: "no-low-priority", Condition: "priority > 2", Action: "skip"},
				},
			},
			task:      &beads.Task{ID: "1", Priority: 1},
			wantSkip:  false,
			wantAllow: true,
		},
		{
			name: "disabled rule",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []config.CustomRule{
					{Name: "disabled", Enabled: &falseVal, Condition: "priority >= 0", Action: "skip"},
				},
			},
			task:      &beads.Task{ID: "1", Priority: 1},
			wantSkip:  false,
			wantAllow: true,
		},
		{
			name: "enabled rule",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []config.CustomRule{
					{Name: "enabled", Enabled: &trueVal, Condition: "priority > 2", Action: "skip"},
				},
			},
			task:       &beads.Task{ID: "1", Priority: 3},
			wantSkip:   true,
			wantReason: "Skipped by rule: enabled",
		},
		{
			name: "allow rule short-circuits",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []config.CustomRule{
					{Name: "always-allow-bugs", Condition: "type == bug", Action: "allow"},
					{Name: "skip-all", Condition: "priority >= 0", Action: "skip"},
				},
			},
			task:      &beads.Task{ID: "1", Type: "bug", Priority: 3},
			wantSkip:  false,
			wantAllow: true,
		},
		{
			name: "include rule continues",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []config.CustomRule{
					{Name: "include-bugs", Condition: "type == bug", Action: "include"},
					{Name: "skip-high-priority", Condition: "priority > 2", Action: "skip"},
				},
			},
			task:       &beads.Task{ID: "1", Type: "bug", Priority: 3},
			wantSkip:   true,
			wantReason: "Skipped by rule: skip-high-priority",
		},
		{
			name: "boost rule adds boost",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []config.CustomRule{
					{Name: "boost-bugs", Condition: "type == bug", Action: "boost:3"},
				},
			},
			task:      &beads.Task{ID: "1", Type: "bug", Priority: 2},
			wantSkip:  false,
			wantAllow: true,
			wantBoost: 3,
		},
		{
			name: "multiple boost rules accumulate",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []config.CustomRule{
					{Name: "boost-bugs", Condition: "type == bug", Action: "boost:2"},
					{Name: "boost-urgent", Condition: "priority <= 1", Action: "boost:3"},
				},
			},
			task:      &beads.Task{ID: "1", Type: "bug", Priority: 1},
			wantSkip:  false,
			wantAllow: true,
			wantBoost: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(tt.config)
			result := engine.Evaluate(tt.task, nil, nil)

			if result.Skip != tt.wantSkip {
				t.Errorf("Skip = %v, want %v", result.Skip, tt.wantSkip)
			}
			if tt.wantSkip && result.SkipReason != tt.wantReason {
				t.Errorf("SkipReason = %q, want %q", result.SkipReason, tt.wantReason)
			}
			if !tt.wantSkip && result.Allow != tt.wantAllow {
				t.Errorf("Allow = %v, want %v", result.Allow, tt.wantAllow)
			}
			if result.BoostAmount != tt.wantBoost {
				t.Errorf("BoostAmount = %d, want %d", result.BoostAmount, tt.wantBoost)
			}
		})
	}
}

func TestEngineRuntimeRules(t *testing.T) {
	cfg := &config.RulesSettings{
		PriorityMax: -1,
		Assignee:    "*",
	}

	engine := NewEngine(cfg)

	// Initially, all tasks should be allowed
	task := &beads.Task{ID: "1", Priority: 3}
	result := engine.Evaluate(task, nil, nil)
	if result.Skip {
		t.Error("Expected task to be allowed before adding rule")
	}

	// Add a runtime rule
	engine.AddRule(config.CustomRule{
		Name:      "runtime-skip",
		Condition: "priority > 2",
		Action:    "skip",
		Reason:    "Runtime rule",
	})

	// Now the task should be skipped
	result = engine.Evaluate(task, nil, nil)
	if !result.Skip {
		t.Error("Expected task to be skipped after adding rule")
	}
	if result.SkipReason != "Runtime rule" {
		t.Errorf("SkipReason = %q, want %q", result.SkipReason, "Runtime rule")
	}

	// Remove the rule
	removed := engine.RemoveRule("runtime-skip")
	if !removed {
		t.Error("RemoveRule returned false, expected true")
	}

	// Task should be allowed again
	result = engine.Evaluate(task, nil, nil)
	if result.Skip {
		t.Error("Expected task to be allowed after removing rule")
	}

	// Removing non-existent rule should return false
	removed = engine.RemoveRule("non-existent")
	if removed {
		t.Error("RemoveRule returned true for non-existent rule")
	}
}

func TestEngineClearRuntimeRules(t *testing.T) {
	cfg := &config.RulesSettings{
		PriorityMax: -1,
		Assignee:    "*",
	}

	engine := NewEngine(cfg)

	engine.AddRule(config.CustomRule{Name: "rule1", Condition: "priority > 0", Action: "skip"})
	engine.AddRule(config.CustomRule{Name: "rule2", Condition: "priority > 1", Action: "skip"})

	task := &beads.Task{ID: "1", Priority: 1}
	result := engine.Evaluate(task, nil, nil)
	if !result.Skip {
		t.Error("Expected task to be skipped with runtime rules")
	}

	engine.ClearRuntimeRules()

	result = engine.Evaluate(task, nil, nil)
	if result.Skip {
		t.Error("Expected task to be allowed after clearing rules")
	}
}

func TestEngineNilConfig(t *testing.T) {
	engine := NewEngine(nil)

	task := &beads.Task{ID: "1", Priority: 4, Type: "epic"}
	result := engine.Evaluate(task, nil, nil)

	if !result.Allow {
		t.Error("Expected Allow = true with nil config")
	}
	if result.Skip {
		t.Error("Expected Skip = false with nil config")
	}
}

func TestParseAction(t *testing.T) {
	tests := []struct {
		action      string
		wantType    actionType
		wantBoost   int
		wantLimit   int
	}{
		{"skip", actionSkip, 0, 0},
		{"include", actionInclude, 0, 0},
		{"allow", actionAllow, 0, 0},
		{"boost:5", actionBoost, 5, 0},
		{"boost:10", actionBoost, 10, 0},
		{"limit:3", actionLimit, 0, 3},
		{"limit:1", actionLimit, 0, 1},
		{"unknown", actionInclude, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			result := parseAction(tt.action)
			if result.actionType != tt.wantType {
				t.Errorf("actionType = %v, want %v", result.actionType, tt.wantType)
			}
			if result.boostAmount != tt.wantBoost {
				t.Errorf("boostAmount = %d, want %d", result.boostAmount, tt.wantBoost)
			}
			if result.limitMax != tt.wantLimit {
				t.Errorf("limitMax = %d, want %d", result.limitMax, tt.wantLimit)
			}
		})
	}
}
