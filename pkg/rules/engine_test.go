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
		name       string
		config     *config.RulesSettings
		task       *beads.Task
		wantSkip   bool
		wantReason string
		wantAllow  bool
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
			wantReason: "Denied by rule: no-low-priority",
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
			wantReason: "Denied by rule: enabled",
		},
		{
			name: "allow rule continues to next rule",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []config.CustomRule{
					{Name: "allow-bugs", Condition: "type == bug", Action: "allow"},
					{Name: "deny-all", Condition: "priority >= 0", Action: "deny"},
				},
			},
			// ALLOW continues evaluation, then DENY rejects the task
			task:       &beads.Task{ID: "1", Type: "bug", Priority: 3},
			wantSkip:   true,
			wantReason: "Denied by rule: deny-all",
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
			wantReason: "Denied by rule: skip-high-priority",
		},
		{
			name: "deny rule with deny action",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []config.CustomRule{
					{Name: "deny-bugs", Condition: "type == bug", Action: "deny"},
				},
			},
			task:       &beads.Task{ID: "1", Type: "bug", Priority: 2},
			wantSkip:   true,
			wantReason: "Denied by rule: deny-bugs",
		},
		{
			name: "allow rule continues evaluation",
			config: &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom: []config.CustomRule{
					{Name: "allow-bugs", Condition: "type == bug", Action: "allow"},
					{Name: "deny-all", Condition: "priority >= 0", Action: "deny"},
				},
			},
			task:       &beads.Task{ID: "1", Type: "bug", Priority: 1},
			wantSkip:   true,
			wantReason: "Denied by rule: deny-all",
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
		action     string
		wantAction Action
	}{
		{"deny", ActionDeny},
		{"skip", ActionDeny},     // backwards compatibility
		{"allow", ActionAllow},
		{"include", ActionAllow}, // backwards compatibility
		{"unknown", ActionAllow}, // unknown defaults to allow
		{"boost:5", ActionAllow}, // deprecated, defaults to allow
		{"limit:3", ActionAllow}, // deprecated, defaults to allow
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			result := parseAction(tt.action)
			if result != tt.wantAction {
				t.Errorf("parseAction(%q) = %v, want %v", tt.action, result, tt.wantAction)
			}
		})
	}
}

func TestEnginePersistRule(t *testing.T) {
	cfg := &config.RulesSettings{
		PriorityMax: -1,
		Assignee:    "*",
	}

	engine := NewEngine(cfg)

	// Add a runtime rule
	enabled := true
	engine.AddRule(config.CustomRule{
		Name:      "test-persist",
		Condition: "priority > 2",
		Action:    "skip",
		Reason:    "Test persist",
		Enabled:   &enabled,
	})

	// Verify it's in runtime rules
	runtimeRules := engine.GetRuntimeRules()
	if len(runtimeRules) != 1 {
		t.Fatalf("Expected 1 runtime rule, got %d", len(runtimeRules))
	}
	if runtimeRules[0].Name != "test-persist" {
		t.Errorf("Expected rule name 'test-persist', got %q", runtimeRules[0].Name)
	}

	// Verify it's marked as not persisted in GetRule
	rule := engine.GetRule("test-persist")
	if rule.Persisted {
		t.Error("Expected rule to not be persisted")
	}

	// Persist the rule
	persistedRule, err := engine.PersistRule("test-persist")
	if err != nil {
		t.Fatalf("PersistRule failed: %v", err)
	}
	if persistedRule.Name != "test-persist" {
		t.Errorf("Expected persisted rule name 'test-persist', got %q", persistedRule.Name)
	}

	// Verify it's moved from runtime to config
	runtimeRules = engine.GetRuntimeRules()
	if len(runtimeRules) != 0 {
		t.Errorf("Expected 0 runtime rules after persist, got %d", len(runtimeRules))
	}

	// Verify it's now persisted
	rule = engine.GetRule("test-persist")
	if rule == nil {
		t.Fatal("Expected to find rule after persist")
	}
	if !rule.Persisted {
		t.Error("Expected rule to be persisted after persist")
	}

	// Verify it still works (task should be skipped)
	task := &beads.Task{ID: "1", Priority: 3}
	result := engine.Evaluate(task, nil, nil)
	if !result.Skip {
		t.Error("Expected task to be skipped by persisted rule")
	}
}

func TestEnginePersistRule_NotFound(t *testing.T) {
	cfg := &config.RulesSettings{
		PriorityMax: -1,
		Assignee:    "*",
	}

	engine := NewEngine(cfg)

	// Try to persist a non-existent rule
	_, err := engine.PersistRule("non-existent")
	if err == nil {
		t.Error("Expected error for non-existent rule")
	}
}

func TestEnginePersistRule_AlreadyPersisted(t *testing.T) {
	cfg := &config.RulesSettings{
		PriorityMax: -1,
		Assignee:    "*",
		Custom: []config.CustomRule{
			{Name: "existing-config", Condition: "priority > 1", Action: "skip"},
		},
	}

	engine := NewEngine(cfg)

	// Try to persist an already persisted rule
	_, err := engine.PersistRule("existing-config")
	if err == nil {
		t.Error("Expected error for already persisted rule")
	}
}

func TestEnginePersistAllRules(t *testing.T) {
	cfg := &config.RulesSettings{
		PriorityMax: -1,
		Assignee:    "*",
	}

	engine := NewEngine(cfg)

	// Add multiple runtime rules
	engine.AddRule(config.CustomRule{Name: "rule1", Condition: "priority > 2", Action: "skip"})
	engine.AddRule(config.CustomRule{Name: "rule2", Condition: "type == bug", Action: "skip"})
	engine.AddRule(config.CustomRule{Name: "rule3", Condition: "priority <= 1", Action: "allow"})

	// Verify we have 3 runtime rules
	runtimeRules := engine.GetRuntimeRules()
	if len(runtimeRules) != 3 {
		t.Fatalf("Expected 3 runtime rules, got %d", len(runtimeRules))
	}

	// Persist all rules
	persisted, err := engine.PersistAllRules()
	if err != nil {
		t.Fatalf("PersistAllRules failed: %v", err)
	}
	if len(persisted) != 3 {
		t.Errorf("Expected 3 persisted rules, got %d", len(persisted))
	}

	// Verify runtime rules are empty
	runtimeRules = engine.GetRuntimeRules()
	if len(runtimeRules) != 0 {
		t.Errorf("Expected 0 runtime rules after persist all, got %d", len(runtimeRules))
	}

	// Verify all rules are now persisted
	for _, name := range persisted {
		rule := engine.GetRule(name)
		if rule == nil {
			t.Errorf("Rule %q not found after persist", name)
			continue
		}
		if !rule.Persisted {
			t.Errorf("Rule %q is not persisted, expected persisted", name)
		}
	}
}

func TestEnginePersistAllRules_Empty(t *testing.T) {
	cfg := &config.RulesSettings{
		PriorityMax: -1,
		Assignee:    "*",
	}

	engine := NewEngine(cfg)

	// Persist all rules when there are none
	persisted, err := engine.PersistAllRules()
	if err != nil {
		t.Fatalf("PersistAllRules failed: %v", err)
	}
	if persisted != nil {
		t.Errorf("Expected nil for empty persist, got %v", persisted)
	}
}

func TestEngineGetConfigForPersistence(t *testing.T) {
	cfg := &config.RulesSettings{
		PriorityMin: 1,
		PriorityMax: 3,
		Assignee:    "testuser",
		Custom: []config.CustomRule{
			{Name: "existing", Condition: "priority > 1", Action: "skip"},
		},
	}

	engine := NewEngine(cfg)

	// Add a runtime rule and persist it
	engine.AddRule(config.CustomRule{Name: "new-rule", Condition: "type == bug", Action: "skip"})
	_, _ = engine.PersistRule("new-rule")

	// Get config for persistence
	persistConfig := engine.GetConfigForPersistence()
	if persistConfig == nil {
		t.Fatal("Expected non-nil config")
	}

	// Verify basic settings
	if persistConfig.PriorityMin != 1 {
		t.Errorf("PriorityMin = %d, want 1", persistConfig.PriorityMin)
	}
	if persistConfig.PriorityMax != 3 {
		t.Errorf("PriorityMax = %d, want 3", persistConfig.PriorityMax)
	}
	if persistConfig.Assignee != "testuser" {
		t.Errorf("Assignee = %q, want 'testuser'", persistConfig.Assignee)
	}

	// Verify both rules are present
	if len(persistConfig.Custom) != 2 {
		t.Errorf("Expected 2 custom rules, got %d", len(persistConfig.Custom))
	}

	// Verify the returned config is a copy (modifying it shouldn't affect the engine)
	persistConfig.PriorityMax = 10
	originalConfig := engine.GetConfigSettings()
	if originalConfig.PriorityMax != 3 {
		t.Error("GetConfigForPersistence didn't return a copy")
	}
}

func TestEngineGetSnapshot_ListPersisted(t *testing.T) {
	tests := []struct {
		name           string
		configRules    []config.CustomRule
		runtimeOps     func(*Engine) // operations to perform after engine creation
		wantPersisted  bool
		wantRulesCount int
	}{
		{
			name:           "empty config - list is persisted",
			configRules:    nil,
			runtimeOps:     nil,
			wantPersisted:  true,
			wantRulesCount: 0,
		},
		{
			name: "config rules only - list is persisted",
			configRules: []config.CustomRule{
				{Name: "rule1", Condition: "priority > 1", Action: "deny"},
				{Name: "rule2", Condition: "type == bug", Action: "allow"},
			},
			runtimeOps:     nil,
			wantPersisted:  true,
			wantRulesCount: 2,
		},
		{
			name: "runtime rule added - list is NOT persisted",
			configRules: []config.CustomRule{
				{Name: "rule1", Condition: "priority > 1", Action: "deny"},
			},
			runtimeOps: func(e *Engine) {
				e.AddRule(config.CustomRule{Name: "runtime-rule", Condition: "type == bug", Action: "allow"})
			},
			wantPersisted:  false,
			wantRulesCount: 2,
		},
		{
			name: "config rule removed - list is NOT persisted",
			configRules: []config.CustomRule{
				{Name: "rule1", Condition: "priority > 1", Action: "deny"},
				{Name: "rule2", Condition: "type == bug", Action: "allow"},
			},
			runtimeOps: func(e *Engine) {
				e.RemoveRule("rule2")
			},
			wantPersisted:  false,
			wantRulesCount: 1,
		},
		{
			name: "rules reordered - list is NOT persisted",
			configRules: []config.CustomRule{
				{Name: "rule1", Condition: "priority > 1", Action: "deny"},
				{Name: "rule2", Condition: "type == bug", Action: "allow"},
			},
			runtimeOps: func(e *Engine) {
				// Remove both and add in different order
				e.RemoveRule("rule1")
				e.RemoveRule("rule2")
				e.AddRule(config.CustomRule{Name: "rule2", Condition: "type == bug", Action: "allow"})
				e.AddRule(config.CustomRule{Name: "rule1", Condition: "priority > 1", Action: "deny"})
			},
			wantPersisted:  false,
			wantRulesCount: 2,
		},
		{
			name: "runtime rule added then persisted - list is persisted",
			configRules: []config.CustomRule{
				{Name: "rule1", Condition: "priority > 1", Action: "deny"},
			},
			runtimeOps: func(e *Engine) {
				e.AddRule(config.CustomRule{Name: "runtime-rule", Condition: "type == bug", Action: "allow"})
				_, _ = e.PersistRule("runtime-rule")
			},
			wantPersisted:  true,
			wantRulesCount: 2,
		},
		{
			name:        "multiple runtime rules added then all persisted - list is persisted",
			configRules: nil,
			runtimeOps: func(e *Engine) {
				e.AddRule(config.CustomRule{Name: "rule1", Condition: "priority > 1", Action: "deny"})
				e.AddRule(config.CustomRule{Name: "rule2", Condition: "type == bug", Action: "allow"})
				_, _ = e.PersistAllRules()
			},
			wantPersisted:  true,
			wantRulesCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom:      tt.configRules,
			}

			engine := NewEngine(cfg)
			if tt.runtimeOps != nil {
				tt.runtimeOps(engine)
			}

			snapshot := engine.GetSnapshot()

			if snapshot.Persisted != tt.wantPersisted {
				t.Errorf("snapshot.Persisted = %v, want %v", snapshot.Persisted, tt.wantPersisted)
			}
			if len(snapshot.Rules) != tt.wantRulesCount {
				t.Errorf("len(snapshot.Rules) = %d, want %d", len(snapshot.Rules), tt.wantRulesCount)
			}
		})
	}
}

func TestEngineGetSnapshot_PerRulePersisted(t *testing.T) {
	cfg := &config.RulesSettings{
		PriorityMax: -1,
		Assignee:    "*",
		Custom: []config.CustomRule{
			{Name: "config-rule", Condition: "priority > 1", Action: "deny"},
		},
	}

	engine := NewEngine(cfg)
	engine.AddRule(config.CustomRule{Name: "runtime-rule", Condition: "type == bug", Action: "allow"})

	snapshot := engine.GetSnapshot()

	// Should have 2 rules total
	if len(snapshot.Rules) != 2 {
		t.Fatalf("Expected 2 rules, got %d", len(snapshot.Rules))
	}

	// First rule (from config) should be persisted
	if snapshot.Rules[0].Name != "config-rule" {
		t.Errorf("Expected first rule to be 'config-rule', got %q", snapshot.Rules[0].Name)
	}
	if !snapshot.Rules[0].Persisted {
		t.Error("Expected config-rule to be persisted")
	}

	// Second rule (runtime) should NOT be persisted
	if snapshot.Rules[1].Name != "runtime-rule" {
		t.Errorf("Expected second rule to be 'runtime-rule', got %q", snapshot.Rules[1].Name)
	}
	if snapshot.Rules[1].Persisted {
		t.Error("Expected runtime-rule to NOT be persisted")
	}

	// List-level persisted should be false (since we have a runtime rule)
	if snapshot.Persisted {
		t.Error("Expected list-level Persisted to be false with runtime rule")
	}
}

func TestEngineReorderRule(t *testing.T) {
	tests := []struct {
		name          string
		initialRules  []config.CustomRule
		reorderName   string
		newPosition   int
		wantError     bool
		wantErrSubstr string
		wantOrder     []string // expected rule names in order after reorder
	}{
		{
			name: "move rule from first to last position",
			initialRules: []config.CustomRule{
				{Name: "rule1", Condition: "priority > 1", Action: "deny"},
				{Name: "rule2", Condition: "type == bug", Action: "allow"},
				{Name: "rule3", Condition: "type == feature", Action: "allow"},
			},
			reorderName: "rule1",
			newPosition: 2,
			wantOrder:   []string{"rule2", "rule3", "rule1"},
		},
		{
			name: "move rule from last to first position",
			initialRules: []config.CustomRule{
				{Name: "rule1", Condition: "priority > 1", Action: "deny"},
				{Name: "rule2", Condition: "type == bug", Action: "allow"},
				{Name: "rule3", Condition: "type == feature", Action: "allow"},
			},
			reorderName: "rule3",
			newPosition: 0,
			wantOrder:   []string{"rule3", "rule1", "rule2"},
		},
		{
			name: "move rule to middle position",
			initialRules: []config.CustomRule{
				{Name: "rule1", Condition: "priority > 1", Action: "deny"},
				{Name: "rule2", Condition: "type == bug", Action: "allow"},
				{Name: "rule3", Condition: "type == feature", Action: "allow"},
			},
			reorderName: "rule1",
			newPosition: 1,
			wantOrder:   []string{"rule2", "rule1", "rule3"},
		},
		{
			name: "move rule to same position (no-op)",
			initialRules: []config.CustomRule{
				{Name: "rule1", Condition: "priority > 1", Action: "deny"},
				{Name: "rule2", Condition: "type == bug", Action: "allow"},
			},
			reorderName: "rule1",
			newPosition: 0,
			wantOrder:   []string{"rule1", "rule2"},
		},
		{
			name: "rule not found",
			initialRules: []config.CustomRule{
				{Name: "rule1", Condition: "priority > 1", Action: "deny"},
			},
			reorderName:   "nonexistent",
			newPosition:   0,
			wantError:     true,
			wantErrSubstr: "not found",
		},
		{
			name: "position out of range (negative)",
			initialRules: []config.CustomRule{
				{Name: "rule1", Condition: "priority > 1", Action: "deny"},
			},
			reorderName:   "rule1",
			newPosition:   -1,
			wantError:     true,
			wantErrSubstr: "out of range",
		},
		{
			name: "position out of range (too high)",
			initialRules: []config.CustomRule{
				{Name: "rule1", Condition: "priority > 1", Action: "deny"},
				{Name: "rule2", Condition: "type == bug", Action: "allow"},
			},
			reorderName:   "rule1",
			newPosition:   5,
			wantError:     true,
			wantErrSubstr: "out of range",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.RulesSettings{
				PriorityMax: -1,
				Assignee:    "*",
				Custom:      tt.initialRules,
			}

			engine := NewEngine(cfg)
			err := engine.ReorderRule(tt.reorderName, tt.newPosition)

			if tt.wantError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErrSubstr != "" && !contains([]string{err.Error()}, tt.wantErrSubstr) {
					// Check using strings.Contains instead
					if !containsSubstring(err.Error(), tt.wantErrSubstr) {
						t.Errorf("error %q does not contain %q", err.Error(), tt.wantErrSubstr)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			snapshot := engine.GetSnapshot()
			if len(snapshot.Rules) != len(tt.wantOrder) {
				t.Fatalf("got %d rules, want %d", len(snapshot.Rules), len(tt.wantOrder))
			}

			for i, wantName := range tt.wantOrder {
				if snapshot.Rules[i].Name != wantName {
					t.Errorf("rule[%d].Name = %q, want %q", i, snapshot.Rules[i].Name, wantName)
				}
			}
		})
	}
}

// containsSubstring checks if s contains substr
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestEngineReorderRule_AffectsListPersisted(t *testing.T) {
	// Create engine with two config rules
	cfg := &config.RulesSettings{
		PriorityMax: -1,
		Assignee:    "*",
		Custom: []config.CustomRule{
			{Name: "rule1", Condition: "priority > 1", Action: "deny"},
			{Name: "rule2", Condition: "type == bug", Action: "allow"},
		},
	}

	engine := NewEngine(cfg)

	// Initially, list should be persisted (matches config snapshot)
	snapshot := engine.GetSnapshot()
	if !snapshot.Persisted {
		t.Fatal("expected list to be persisted initially")
	}

	// Reorder rules
	if err := engine.ReorderRule("rule1", 1); err != nil {
		t.Fatalf("ReorderRule failed: %v", err)
	}

	// After reorder, list should NOT be persisted (order changed)
	snapshot = engine.GetSnapshot()
	if snapshot.Persisted {
		t.Error("expected list to NOT be persisted after reordering")
	}

	// Verify order changed
	if snapshot.Rules[0].Name != "rule2" || snapshot.Rules[1].Name != "rule1" {
		t.Errorf("unexpected order: %v, %v", snapshot.Rules[0].Name, snapshot.Rules[1].Name)
	}

	// Individual rules should still show as persisted (they exist in config)
	if !snapshot.Rules[0].Persisted || !snapshot.Rules[1].Persisted {
		t.Error("individual rules should still be marked as persisted")
	}
}
