package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	if config.Resolver.Timeout != "10m" {
		t.Errorf("expected default resolver timeout to be '10m', got %q", config.Resolver.Timeout)
	}
}

func TestLoadConfigFileNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	config, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.Resolver.Timeout != "10m" {
		t.Errorf("expected default resolver timeout '10m', got %q", config.Resolver.Timeout)
	}
}

func TestLoadConfigValid(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configContent := `[resolver]
timeout = "15m"
`
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	config, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.Resolver.Timeout != "15m" {
		t.Errorf("expected resolver timeout '15m', got %q", config.Resolver.Timeout)
	}
}

func TestLoadConfigInvalidTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configContent := `[resolver]
timeout = "invalid"
`
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for invalid timeout, got nil")
	}
}

func TestGetResolverTimeout(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected time.Duration
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: 0,
		},
		{
			name:     "empty timeout",
			config:   &Config{Resolver: ResolverSettings{Timeout: ""}},
			expected: 0,
		},
		{
			name:     "10 minutes",
			config:   &Config{Resolver: ResolverSettings{Timeout: "10m"}},
			expected: 10 * time.Minute,
		},
		{
			name:     "30 minutes",
			config:   &Config{Resolver: ResolverSettings{Timeout: "30m"}},
			expected: 30 * time.Minute,
		},
		{
			name:     "1 hour",
			config:   &Config{Resolver: ResolverSettings{Timeout: "1h"}},
			expected: time.Hour,
		},
		{
			name:     "invalid format returns 0",
			config:   &Config{Resolver: ResolverSettings{Timeout: "invalid"}},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetResolverTimeout()
			if got != tt.expected {
				t.Errorf("GetResolverTimeout() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSaveConfig(t *testing.T) {
	tmpDir := t.TempDir()
	config := &Config{
		Resolver: ResolverSettings{Timeout: "20m"},
	}

	if err := SaveConfig(tmpDir, config); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Verify the file was created
	configPath := filepath.Join(tmpDir, ".canopy", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read saved config: %v", err)
	}

	// Verify content contains expected values
	content := string(data)
	if !contains(content, "20m") {
		t.Errorf("expected saved config to contain '20m', got:\n%s", content)
	}
}

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

func TestDefaultRulesSettings(t *testing.T) {
	rules := DefaultRulesSettings()

	if rules.PriorityMin != 0 {
		t.Errorf("expected default priority_min to be 0, got %d", rules.PriorityMin)
	}
	if rules.PriorityMax != -1 {
		t.Errorf("expected default priority_max to be -1, got %d", rules.PriorityMax)
	}
	if rules.Assignee != "*" {
		t.Errorf("expected default assignee to be '*', got %q", rules.Assignee)
	}
	if rules.StopWhenEmpty {
		t.Error("expected default stop_when_empty to be false")
	}
}

func TestRulesSettingsValidatePriority(t *testing.T) {
	tests := []struct {
		name        string
		rules       RulesSettings
		expectError bool
	}{
		{
			name:        "valid default",
			rules:       DefaultRulesSettings(),
			expectError: false,
		},
		{
			name: "valid priority range",
			rules: RulesSettings{
				PriorityMin: 1,
				PriorityMax: 3,
			},
			expectError: false,
		},
		{
			name: "priority_min negative",
			rules: RulesSettings{
				PriorityMin: -1,
				PriorityMax: 4,
			},
			expectError: true,
		},
		{
			name: "priority_min > 4",
			rules: RulesSettings{
				PriorityMin: 5,
				PriorityMax: -1,
			},
			expectError: true,
		},
		{
			name: "priority_max invalid",
			rules: RulesSettings{
				PriorityMin: 0,
				PriorityMax: 5,
			},
			expectError: true,
		},
		{
			name: "priority_min > priority_max",
			rules: RulesSettings{
				PriorityMin: 3,
				PriorityMax: 1,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.rules.Validate()
			if tt.expectError && len(errs) == 0 {
				t.Error("expected validation error, got none")
			}
			if !tt.expectError && len(errs) > 0 {
				t.Errorf("unexpected validation error: %v", errs)
			}
		})
	}
}

func TestRulesSettingsValidateTypes(t *testing.T) {
	tests := []struct {
		name        string
		rules       RulesSettings
		expectError bool
	}{
		{
			name: "valid types",
			rules: RulesSettings{
				Types: []string{"bug", "feature", "task"},
			},
			expectError: false,
		},
		{
			name: "invalid type",
			rules: RulesSettings{
				Types: []string{"bug", "invalid"},
			},
			expectError: true,
		},
		{
			name: "valid exclude_types",
			rules: RulesSettings{
				ExcludeTypes: []string{"epic"},
			},
			expectError: false,
		},
		{
			name: "invalid exclude_type",
			rules: RulesSettings{
				ExcludeTypes: []string{"notavalidtype"},
			},
			expectError: true,
		},
		{
			name: "overlap between types and exclude_types",
			rules: RulesSettings{
				Types:        []string{"bug", "feature"},
				ExcludeTypes: []string{"bug"},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.rules.Validate()
			if tt.expectError && len(errs) == 0 {
				t.Error("expected validation error, got none")
			}
			if !tt.expectError && len(errs) > 0 {
				t.Errorf("unexpected validation error: %v", errs)
			}
		})
	}
}

func TestRulesSettingsValidateLabels(t *testing.T) {
	tests := []struct {
		name        string
		rules       RulesSettings
		expectError bool
	}{
		{
			name: "valid labels",
			rules: RulesSettings{
				Labels:        []string{"frontend", "backend"},
				ExcludeLabels: []string{"wip"},
			},
			expectError: false,
		},
		{
			name: "overlap between labels and exclude_labels",
			rules: RulesSettings{
				Labels:        []string{"frontend", "wip"},
				ExcludeLabels: []string{"wip"},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.rules.Validate()
			if tt.expectError && len(errs) == 0 {
				t.Error("expected validation error, got none")
			}
			if !tt.expectError && len(errs) > 0 {
				t.Errorf("unexpected validation error: %v", errs)
			}
		})
	}
}

func TestRulesSettingsValidateConcurrency(t *testing.T) {
	tests := []struct {
		name        string
		rules       RulesSettings
		expectError bool
	}{
		{
			name: "valid max_concurrent",
			rules: RulesSettings{
				MaxConcurrent: 4,
			},
			expectError: false,
		},
		{
			name: "negative max_concurrent",
			rules: RulesSettings{
				MaxConcurrent: -1,
			},
			expectError: true,
		},
		{
			name: "valid max_concurrent_per_type",
			rules: RulesSettings{
				MaxConcurrentPerType: map[string]int{"bug": 2},
			},
			expectError: false,
		},
		{
			name: "negative max_concurrent_per_type",
			rules: RulesSettings{
				MaxConcurrentPerType: map[string]int{"bug": -1},
			},
			expectError: true,
		},
		{
			name: "invalid type in max_concurrent_per_type",
			rules: RulesSettings{
				MaxConcurrentPerType: map[string]int{"invalid": 2},
			},
			expectError: true,
		},
		{
			name: "negative max_concurrent_per_label",
			rules: RulesSettings{
				MaxConcurrentPerLabel: map[string]int{"frontend": -1},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.rules.Validate()
			if tt.expectError && len(errs) == 0 {
				t.Error("expected validation error, got none")
			}
			if !tt.expectError && len(errs) > 0 {
				t.Errorf("unexpected validation error: %v", errs)
			}
		})
	}
}

func TestRulesSettingsValidateCustomRules(t *testing.T) {
	tests := []struct {
		name        string
		rules       RulesSettings
		expectError bool
	}{
		{
			name: "valid custom rule",
			rules: RulesSettings{
				Custom: []CustomRule{
					{Name: "test", Condition: "priority > 1", Action: "skip"},
				},
			},
			expectError: false,
		},
		{
			name: "missing name",
			rules: RulesSettings{
				Custom: []CustomRule{
					{Condition: "priority > 1", Action: "skip"},
				},
			},
			expectError: true,
		},
		{
			name: "missing condition",
			rules: RulesSettings{
				Custom: []CustomRule{
					{Name: "test", Action: "skip"},
				},
			},
			expectError: true,
		},
		{
			name: "missing action",
			rules: RulesSettings{
				Custom: []CustomRule{
					{Name: "test", Condition: "priority > 1"},
				},
			},
			expectError: true,
		},
		{
			name: "invalid action",
			rules: RulesSettings{
				Custom: []CustomRule{
					{Name: "test", Condition: "priority > 1", Action: "invalid"},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.rules.Validate()
			if tt.expectError && len(errs) == 0 {
				t.Error("expected validation error, got none")
			}
			if !tt.expectError && len(errs) > 0 {
				t.Errorf("unexpected validation error: %v", errs)
			}
		})
	}
}

func TestLoadConfigWithRules(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configContent := `[resolver]
timeout = "10m"

[rules]
priority_min = 0
priority_max = 2
types = ["bug", "task"]
exclude_types = ["epic"]
labels = ["frontend"]
exclude_labels = ["wip"]
assignee = "*"
stop_when_empty = false
max_concurrent = 4
`
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Rules.PriorityMin != 0 {
		t.Errorf("expected priority_min 0, got %d", cfg.Rules.PriorityMin)
	}
	if cfg.Rules.PriorityMax != 2 {
		t.Errorf("expected priority_max 2, got %d", cfg.Rules.PriorityMax)
	}
	if len(cfg.Rules.Types) != 2 || cfg.Rules.Types[0] != "bug" || cfg.Rules.Types[1] != "task" {
		t.Errorf("unexpected types: %v", cfg.Rules.Types)
	}
	if len(cfg.Rules.ExcludeTypes) != 1 || cfg.Rules.ExcludeTypes[0] != "epic" {
		t.Errorf("unexpected exclude_types: %v", cfg.Rules.ExcludeTypes)
	}
	if len(cfg.Rules.Labels) != 1 || cfg.Rules.Labels[0] != "frontend" {
		t.Errorf("unexpected labels: %v", cfg.Rules.Labels)
	}
	if len(cfg.Rules.ExcludeLabels) != 1 || cfg.Rules.ExcludeLabels[0] != "wip" {
		t.Errorf("unexpected exclude_labels: %v", cfg.Rules.ExcludeLabels)
	}
	if cfg.Rules.Assignee != "*" {
		t.Errorf("expected assignee '*', got %q", cfg.Rules.Assignee)
	}
	if cfg.Rules.MaxConcurrent != 4 {
		t.Errorf("expected max_concurrent 4, got %d", cfg.Rules.MaxConcurrent)
	}
}

func TestLoadConfigWithCustomRules(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configContent := `[resolver]
timeout = "10m"

[rules]
priority_max = -1
assignee = "*"

[[rules.custom]]
name = "incident-mode"
enabled = false
condition = "priority > 1"
action = "skip"
reason = "Incident mode active"

[[rules.custom]]
name = "skip-chores"
condition = "type == chore"
action = "skip"
`
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Rules.Custom) != 2 {
		t.Fatalf("expected 2 custom rules, got %d", len(cfg.Rules.Custom))
	}

	rule1 := cfg.Rules.Custom[0]
	if rule1.Name != "incident-mode" {
		t.Errorf("expected rule name 'incident-mode', got %q", rule1.Name)
	}
	if rule1.Enabled == nil || *rule1.Enabled != false {
		t.Error("expected rule to be disabled")
	}
	if rule1.Condition != "priority > 1" {
		t.Errorf("unexpected condition: %q", rule1.Condition)
	}
	if rule1.Action != "skip" {
		t.Errorf("unexpected action: %q", rule1.Action)
	}
	if rule1.Reason != "Incident mode active" {
		t.Errorf("unexpected reason: %q", rule1.Reason)
	}

	rule2 := cfg.Rules.Custom[1]
	if rule2.Name != "skip-chores" {
		t.Errorf("expected rule name 'skip-chores', got %q", rule2.Name)
	}
	if rule2.Enabled != nil {
		t.Error("expected rule.Enabled to be nil (defaults to enabled)")
	}
}
