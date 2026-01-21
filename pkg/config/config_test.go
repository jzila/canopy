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

func TestLoadConsolidatedConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Write a consolidated config with all sections
	configContent := `[resolver]
timeout = "15m"

[sandbox]
enabled = true
network = "allow"

[resources]
max_memory = "8GB"
max_processes = 200

[validation]
enabled = true
strict = true
timeout = "10m"

[[validation.steps]]
name = "build"
command = "go build ./..."
required = true
`
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	config, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all sections were loaded
	if config.Resolver.Timeout != "15m" {
		t.Errorf("expected resolver timeout '15m', got %q", config.Resolver.Timeout)
	}
	if !config.Sandbox.Enabled {
		t.Error("expected sandbox to be enabled")
	}
	if config.Sandbox.Network != "allow" {
		t.Errorf("expected network 'allow', got %q", config.Sandbox.Network)
	}
	if config.Resources.MaxMemory != "8GB" {
		t.Errorf("expected max_memory '8GB', got %q", config.Resources.MaxMemory)
	}
	if config.Resources.MaxProcesses != 200 {
		t.Errorf("expected max_processes 200, got %d", config.Resources.MaxProcesses)
	}
	if !config.Validation.Enabled {
		t.Error("expected validation to be enabled")
	}
	if !config.Validation.Strict {
		t.Error("expected strict mode to be enabled")
	}
	if len(config.Validation.Steps) != 1 {
		t.Errorf("expected 1 validation step, got %d", len(config.Validation.Steps))
	}
	if config.Validation.Steps[0].Name != "build" {
		t.Errorf("expected step name 'build', got %q", config.Validation.Steps[0].Name)
	}
}

func TestLoadLegacySandboxConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Write legacy sandbox.toml
	sandboxContent := `[sandbox]
enabled = true
network = "deny"

[resources]
max_memory = "2GB"
max_processes = 50
`
	sandboxPath := filepath.Join(configDir, "sandbox.toml")
	if err := os.WriteFile(sandboxPath, []byte(sandboxContent), 0644); err != nil {
		t.Fatalf("failed to write sandbox file: %v", err)
	}

	// Should load legacy file with deprecation warning
	config, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.Sandbox.Network != "deny" {
		t.Errorf("expected network 'deny' from legacy config, got %q", config.Sandbox.Network)
	}
	if config.Resources.MaxMemory != "2GB" {
		t.Errorf("expected max_memory '2GB' from legacy config, got %q", config.Resources.MaxMemory)
	}
}

func TestLoadLegacyValidationConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Write legacy validation.toml
	validationContent := `[validation]
enabled = true
strict = false
timeout = "3m"

[[validation.steps]]
name = "test"
command = "go test ./..."
required = true
`
	validationPath := filepath.Join(configDir, "validation.toml")
	if err := os.WriteFile(validationPath, []byte(validationContent), 0644); err != nil {
		t.Fatalf("failed to write validation file: %v", err)
	}

	// Should load legacy file with deprecation warning
	config, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !config.Validation.Enabled {
		t.Error("expected validation to be enabled from legacy config")
	}
	if config.Validation.Timeout != "3m" {
		t.Errorf("expected timeout '3m' from legacy config, got %q", config.Validation.Timeout)
	}
	if len(config.Validation.Steps) != 1 {
		t.Errorf("expected 1 validation step from legacy config, got %d", len(config.Validation.Steps))
	}
}

func TestConsolidatedConfigPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Write consolidated config
	configContent := `[sandbox]
enabled = true
network = "allow"

[resources]
max_memory = "8GB"
`
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Write legacy sandbox.toml with different values
	sandboxContent := `[sandbox]
network = "deny"

[resources]
max_memory = "2GB"
`
	sandboxPath := filepath.Join(configDir, "sandbox.toml")
	if err := os.WriteFile(sandboxPath, []byte(sandboxContent), 0644); err != nil {
		t.Fatalf("failed to write sandbox file: %v", err)
	}

	// Consolidated config should take precedence
	config, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have values from consolidated config, not legacy
	if config.Sandbox.Network != "allow" {
		t.Errorf("expected network 'allow' from consolidated config, got %q", config.Sandbox.Network)
	}
	if config.Resources.MaxMemory != "8GB" {
		t.Errorf("expected max_memory '8GB' from consolidated config, got %q", config.Resources.MaxMemory)
	}
}

func TestGetValidationTimeout(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected time.Duration
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: 5 * time.Minute,
		},
		{
			name:     "empty timeout",
			config:   &Config{},
			expected: 5 * time.Minute,
		},
		{
			name:     "custom timeout",
			config:   &Config{Validation: ValidationSettings{Timeout: "10m"}},
			expected: 10 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetValidationTimeout()
			if got != tt.expected {
				t.Errorf("GetValidationTimeout() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsValidationEnabled(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected bool
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: false,
		},
		{
			name:     "disabled",
			config:   &Config{Validation: ValidationSettings{Enabled: false}},
			expected: false,
		},
		{
			name:     "enabled",
			config:   &Config{Validation: ValidationSettings{Enabled: true}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.IsValidationEnabled()
			if got != tt.expected {
				t.Errorf("IsValidationEnabled() = %v, want %v", got, tt.expected)
			}
		})
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
