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
