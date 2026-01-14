package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate_NilConfig(t *testing.T) {
	var c *SandboxConfig
	if err := c.Validate(); err != nil {
		t.Errorf("expected nil error for nil config, got %v", err)
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	c := DefaultSandboxConfig()
	if err := c.Validate(); err != nil {
		t.Errorf("expected nil error for default config, got %v", err)
	}
}

func TestValidate_NetworkPolicy(t *testing.T) {
	tests := []struct {
		name    string
		network string
		wantErr bool
	}{
		{"allow is valid", "allow", false},
		{"deny is valid", "deny", false},
		{"proxy is valid", "proxy", false},
		{"empty is valid", "", false},
		{"invalid value", "block", true},
		{"case sensitive", "ALLOW", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &SandboxConfig{
				Sandbox: SandboxSettings{Network: tt.network},
			}
			err := c.ValidateWithoutPaths()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWithoutPaths() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_MemoryLimit(t *testing.T) {
	tests := []struct {
		name      string
		maxMemory string
		wantErr   bool
	}{
		{"valid GB", "4GB", false},
		{"valid MB", "512MB", false},
		{"valid KB", "1024KB", false},
		{"valid bytes", "1000", false},
		{"empty is valid", "", false},
		{"invalid value", "abc", true},
		{"negative value", "-4GB", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &SandboxConfig{
				Resources: ResourceSettings{MaxMemory: tt.maxMemory},
			}
			err := c.ValidateWithoutPaths()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWithoutPaths() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_DiskLimit(t *testing.T) {
	tests := []struct {
		name    string
		maxDisk string
		wantErr bool
	}{
		{"valid GB", "10GB", false},
		{"valid MB", "500MB", false},
		{"empty is valid", "", false},
		{"invalid value", "xyz", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &SandboxConfig{
				Resources: ResourceSettings{MaxDisk: tt.maxDisk},
			}
			err := c.ValidateWithoutPaths()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWithoutPaths() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_Timeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		wantErr bool
	}{
		{"valid minutes", "10m", false},
		{"valid hours", "1h", false},
		{"valid seconds", "30s", false},
		{"valid complex", "1h30m", false},
		{"empty is valid", "", false},
		{"invalid format", "10 minutes", true},
		{"invalid value", "abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &SandboxConfig{
				Resources: ResourceSettings{Timeout: tt.timeout},
			}
			err := c.ValidateWithoutPaths()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWithoutPaths() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_ResourceLimits(t *testing.T) {
	tests := []struct {
		name         string
		maxProcesses int
		maxOpenFiles int
		wantErr      bool
	}{
		{"positive values", 100, 1024, false},
		{"zero values", 0, 0, false},
		{"negative processes", -1, 1024, true},
		{"negative open files", 100, -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &SandboxConfig{
				Resources: ResourceSettings{
					MaxProcesses: tt.maxProcesses,
					MaxOpenFiles: tt.maxOpenFiles,
				},
			}
			err := c.ValidateWithoutPaths()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWithoutPaths() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_PathExistence(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "sandbox-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	existingPath := filepath.Join(tmpDir, "exists")
	if err := os.MkdirAll(existingPath, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	nonExistentPath := filepath.Join(tmpDir, "does-not-exist")

	tests := []struct {
		name    string
		paths   PathSettings
		wantErr bool
	}{
		{
			name: "existing path is valid",
			paths: PathSettings{
				ReadOnly: []string{existingPath},
			},
			wantErr: false,
		},
		{
			name: "non-existent path is invalid",
			paths: PathSettings{
				ReadOnly: []string{nonExistentPath},
			},
			wantErr: true,
		},
		{
			name: "empty paths is valid",
			paths: PathSettings{
				ReadOnly: []string{},
			},
			wantErr: false,
		},
		{
			name: "mixed paths - one invalid",
			paths: PathSettings{
				ReadOnly: []string{existingPath, nonExistentPath},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &SandboxConfig{
				Paths: tt.paths,
			}
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	c := &SandboxConfig{
		Sandbox: SandboxSettings{Network: "invalid"},
		Resources: ResourceSettings{
			MaxMemory:    "invalid",
			Timeout:      "invalid",
			MaxProcesses: -1,
		},
	}

	err := c.ValidateWithoutPaths()
	if err == nil {
		t.Fatal("expected error for invalid config")
	}

	errs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	if len(errs) != 4 {
		t.Errorf("expected 4 validation errors, got %d: %v", len(errs), errs)
	}
}

func TestValidationError_ErrorString(t *testing.T) {
	err := &ValidationError{
		Field:   "sandbox.network",
		Message: "invalid value",
	}

	expected := "sandbox.network: invalid value"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestValidationErrors_ErrorString(t *testing.T) {
	tests := []struct {
		name     string
		errs     ValidationErrors
		contains string
	}{
		{
			name:     "empty errors",
			errs:     ValidationErrors{},
			contains: "no validation errors",
		},
		{
			name: "single error",
			errs: ValidationErrors{
				{Field: "field1", Message: "message1"},
			},
			contains: "field1: message1",
		},
		{
			name: "multiple errors",
			errs: ValidationErrors{
				{Field: "field1", Message: "message1"},
				{Field: "field2", Message: "message2"},
			},
			contains: "multiple validation errors",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.errs.Error()
			if !contains(result, tt.contains) {
				t.Errorf("expected error string to contain %q, got %q", tt.contains, result)
			}
		})
	}
}

func TestLoadConfig_WithValidation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sandbox-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name: "valid config",
			content: `[sandbox]
enabled = true
network = "allow"

[resources]
max_memory = "4GB"
timeout = "10m"
`,
			wantErr: false,
		},
		{
			name: "invalid network",
			content: `[sandbox]
network = "invalid"
`,
			wantErr: true,
		},
		{
			name: "invalid memory format",
			content: `[resources]
max_memory = "invalid"
`,
			wantErr: true,
		},
		{
			name: "invalid timeout",
			content: `[resources]
timeout = "invalid"
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(configDir, "sandbox.toml")
			if err := os.WriteFile(configPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			_, err := LoadConfig(tmpDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadConfigWithoutValidation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sandbox-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Write config with non-existent path (would fail validation)
	content := `[paths]
read_only = ["/non/existent/path"]
`
	configPath := filepath.Join(configDir, "sandbox.toml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// LoadConfig should fail due to path validation
	_, err = LoadConfig(tmpDir)
	if err == nil {
		t.Error("LoadConfig() expected error for non-existent path")
	}

	// LoadConfigWithoutValidation should succeed
	cfg, err := LoadConfigWithoutValidation(tmpDir)
	if err != nil {
		t.Errorf("LoadConfigWithoutValidation() unexpected error: %v", err)
	}
	if cfg == nil {
		t.Error("LoadConfigWithoutValidation() returned nil config")
	}
}

func TestParseMemoryLimit(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
		wantErr  bool
	}{
		{"4GB", 4 * (1 << 30), false},
		{"512MB", 512 * (1 << 20), false},
		{"1024KB", 1024 * (1 << 10), false},
		{"1000B", 1000, false},
		{"1000", 1000, false},
		{"", 0, false},
		{"4gb", 4 * (1 << 30), false}, // case insensitive
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseMemoryLimit(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMemoryLimit(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != tt.expected {
				t.Errorf("ParseMemoryLimit(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func contains(s, substr string) bool {
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
