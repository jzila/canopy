package validation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidate_NilConfig(t *testing.T) {
	var c *ValidationConfig
	if err := c.Validate(); err != nil {
		t.Errorf("expected nil error for nil config, got %v", err)
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	c := DefaultValidationConfig()
	if err := c.Validate(); err != nil {
		t.Errorf("expected nil error for default config, got %v", err)
	}
}

func TestValidate_GlobalTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		wantErr bool
	}{
		{"valid minutes", "5m", false},
		{"valid hours", "1h", false},
		{"valid seconds", "30s", false},
		{"valid complex", "1h30m", false},
		{"empty is valid", "", false},
		{"invalid format", "5 minutes", true},
		{"invalid value", "abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ValidationConfig{
				Validation: ValidationSettings{Timeout: tt.timeout},
			}
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_StepName(t *testing.T) {
	tests := []struct {
		name    string
		step    StepConfig
		wantErr bool
	}{
		{
			name: "valid name",
			step: StepConfig{
				Name:    "typecheck",
				Command: "npm run type-check",
			},
			wantErr: false,
		},
		{
			name: "empty name",
			step: StepConfig{
				Name:    "",
				Command: "npm run type-check",
			},
			wantErr: true,
		},
		{
			name: "whitespace-only name",
			step: StepConfig{
				Name:    "   ",
				Command: "npm run type-check",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ValidationConfig{
				Validation: ValidationSettings{
					Steps: []StepConfig{tt.step},
				},
			}
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_StepCommand(t *testing.T) {
	tests := []struct {
		name    string
		step    StepConfig
		wantErr bool
	}{
		{
			name: "valid command",
			step: StepConfig{
				Name:    "test",
				Command: "go test ./...",
			},
			wantErr: false,
		},
		{
			name: "empty command",
			step: StepConfig{
				Name:    "test",
				Command: "",
			},
			wantErr: true,
		},
		{
			name: "whitespace-only command",
			step: StepConfig{
				Name:    "test",
				Command: "   ",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ValidationConfig{
				Validation: ValidationSettings{
					Steps: []StepConfig{tt.step},
				},
			}
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_StepTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		wantErr bool
	}{
		{"valid minutes", "2m", false},
		{"valid seconds", "30s", false},
		{"valid complex", "5m30s", false},
		{"empty is valid", "", false},
		{"invalid format", "2 minutes", true},
		{"invalid value", "xyz", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ValidationConfig{
				Validation: ValidationSettings{
					Steps: []StepConfig{
						{
							Name:    "test",
							Command: "go test",
							Timeout: tt.timeout,
						},
					},
				},
			}
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_MultipleSteps(t *testing.T) {
	c := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Timeout: "10m",
			Steps: []StepConfig{
				{Name: "lint", Command: "npm run lint", Required: true},
				{Name: "test", Command: "npm test", Timeout: "5m"},
				{Name: "build", Command: "npm run build", Required: true},
			},
		},
	}

	if err := c.Validate(); err != nil {
		t.Errorf("expected nil error for valid multi-step config, got %v", err)
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	c := &ValidationConfig{
		Validation: ValidationSettings{
			Timeout: "invalid",
			Steps: []StepConfig{
				{Name: "", Command: ""},
				{Name: "test", Command: "cmd", Timeout: "invalid"},
			},
		},
	}

	err := c.Validate()
	if err == nil {
		t.Fatal("expected error for invalid config")
	}

	errs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	// Expect: global timeout invalid, step[0].name missing, step[0].command missing, step[1].timeout invalid
	if len(errs) != 4 {
		t.Errorf("expected 4 validation errors, got %d: %v", len(errs), errs)
	}
}

func TestValidationError_ErrorString(t *testing.T) {
	err := &ValidationError{
		Field:   "validation.timeout",
		Message: "invalid duration",
	}

	expected := "validation.timeout: invalid duration"
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
			if !strings.Contains(result, tt.contains) {
				t.Errorf("expected error string to contain %q, got %q", tt.contains, result)
			}
		})
	}
}

func TestLoadValidationConfig_FileNotExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validation-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg, err := LoadValidationConfig(tmpDir)
	if err != nil {
		t.Errorf("LoadValidationConfig() unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadValidationConfig() returned nil config")
	}
	// Should return default config with validation disabled
	if cfg.Validation.Enabled {
		t.Error("expected validation to be disabled by default")
	}
}

func TestLoadValidationConfig_ValidFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validation-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	content := `[validation]
enabled = true
strict = true
timeout = "10m"

[[validation.steps]]
name = "typecheck"
command = "npm run type-check"
timeout = "2m"
required = true

[[validation.steps]]
name = "lint"
command = "npm run lint"
required = false
`
	configPath := filepath.Join(configDir, "validation.toml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadValidationConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadValidationConfig() error: %v", err)
	}

	if !cfg.Validation.Enabled {
		t.Error("expected validation.enabled = true")
	}
	if !cfg.Validation.Strict {
		t.Error("expected validation.strict = true")
	}
	if cfg.Validation.Timeout != "10m" {
		t.Errorf("expected validation.timeout = 10m, got %s", cfg.Validation.Timeout)
	}
	if len(cfg.Validation.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(cfg.Validation.Steps))
	}

	// Check first step
	step0 := cfg.Validation.Steps[0]
	if step0.Name != "typecheck" {
		t.Errorf("expected step[0].name = typecheck, got %s", step0.Name)
	}
	if step0.Command != "npm run type-check" {
		t.Errorf("expected step[0].command = 'npm run type-check', got %s", step0.Command)
	}
	if step0.Timeout != "2m" {
		t.Errorf("expected step[0].timeout = 2m, got %s", step0.Timeout)
	}
	if !step0.Required {
		t.Error("expected step[0].required = true")
	}

	// Check second step
	step1 := cfg.Validation.Steps[1]
	if step1.Name != "lint" {
		t.Errorf("expected step[1].name = lint, got %s", step1.Name)
	}
	if step1.Required {
		t.Error("expected step[1].required = false")
	}
}

func TestLoadValidationConfig_InvalidTOML(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validation-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Invalid TOML syntax
	content := `[validation
enabled = true
`
	configPath := filepath.Join(configDir, "validation.toml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err = LoadValidationConfig(tmpDir)
	if err == nil {
		t.Error("LoadValidationConfig() expected error for invalid TOML")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestLoadValidationConfig_InvalidValidation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validation-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Valid TOML but invalid configuration
	content := `[validation]
timeout = "invalid"

[[validation.steps]]
name = ""
command = ""
`
	configPath := filepath.Join(configDir, "validation.toml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err = LoadValidationConfig(tmpDir)
	if err == nil {
		t.Error("LoadValidationConfig() expected error for invalid config")
	}
	if !strings.Contains(err.Error(), "invalid config") {
		t.Errorf("expected invalid config error, got: %v", err)
	}
}

func TestGetTimeout(t *testing.T) {
	tests := []struct {
		name     string
		config   *ValidationConfig
		expected time.Duration
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: 5 * time.Minute,
		},
		{
			name: "empty timeout",
			config: &ValidationConfig{
				Validation: ValidationSettings{Timeout: ""},
			},
			expected: 5 * time.Minute,
		},
		{
			name: "valid timeout",
			config: &ValidationConfig{
				Validation: ValidationSettings{Timeout: "10m"},
			},
			expected: 10 * time.Minute,
		},
		{
			name: "complex timeout",
			config: &ValidationConfig{
				Validation: ValidationSettings{Timeout: "1h30m"},
			},
			expected: 90 * time.Minute,
		},
		{
			name: "invalid timeout falls back to default",
			config: &ValidationConfig{
				Validation: ValidationSettings{Timeout: "invalid"},
			},
			expected: 5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetTimeout()
			if result != tt.expected {
				t.Errorf("GetTimeout() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetStepTimeout(t *testing.T) {
	globalConfig := &ValidationConfig{
		Validation: ValidationSettings{Timeout: "10m"},
	}

	tests := []struct {
		name     string
		step     *StepConfig
		expected time.Duration
	}{
		{
			name:     "step with no timeout uses global",
			step:     &StepConfig{Name: "test", Command: "go test"},
			expected: 10 * time.Minute,
		},
		{
			name:     "step with empty timeout uses global",
			step:     &StepConfig{Name: "test", Command: "go test", Timeout: ""},
			expected: 10 * time.Minute,
		},
		{
			name:     "step with valid timeout overrides global",
			step:     &StepConfig{Name: "test", Command: "go test", Timeout: "2m"},
			expected: 2 * time.Minute,
		},
		{
			name:     "step with invalid timeout falls back to global",
			step:     &StepConfig{Name: "test", Command: "go test", Timeout: "invalid"},
			expected: 10 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := globalConfig.GetStepTimeout(tt.step)
			if result != tt.expected {
				t.Errorf("GetStepTimeout() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		config   *ValidationConfig
		expected bool
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: false,
		},
		{
			name: "enabled = false",
			config: &ValidationConfig{
				Validation: ValidationSettings{Enabled: false},
			},
			expected: false,
		},
		{
			name: "enabled = true",
			config: &ValidationConfig{
				Validation: ValidationSettings{Enabled: true},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsEnabled()
			if result != tt.expected {
				t.Errorf("IsEnabled() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsStrict(t *testing.T) {
	tests := []struct {
		name     string
		config   *ValidationConfig
		expected bool
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: false,
		},
		{
			name: "strict = false",
			config: &ValidationConfig{
				Validation: ValidationSettings{Strict: false},
			},
			expected: false,
		},
		{
			name: "strict = true",
			config: &ValidationConfig{
				Validation: ValidationSettings{Strict: true},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsStrict()
			if result != tt.expected {
				t.Errorf("IsStrict() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDefaultValidationConfig(t *testing.T) {
	cfg := DefaultValidationConfig()

	if cfg == nil {
		t.Fatal("DefaultValidationConfig() returned nil")
	}
	if cfg.Validation.Enabled {
		t.Error("expected default validation.enabled = false")
	}
	if cfg.Validation.Strict {
		t.Error("expected default validation.strict = false")
	}
	if cfg.Validation.Timeout != "5m" {
		t.Errorf("expected default validation.timeout = 5m, got %s", cfg.Validation.Timeout)
	}
	if len(cfg.Validation.Steps) != 0 {
		t.Errorf("expected default validation.steps to be empty, got %d steps", len(cfg.Validation.Steps))
	}
}

func TestSaveConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validation-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Strict:  true,
			Timeout: "10m",
			Steps: []StepConfig{
				{Name: "test", Command: "go test ./...", Timeout: "5m", Required: true},
			},
		},
	}

	if err := SaveConfig(tmpDir, cfg); err != nil {
		t.Fatalf("SaveConfig() error: %v", err)
	}

	// Verify file was created
	configPath := filepath.Join(tmpDir, ".canopy", "validation.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("SaveConfig() did not create config file")
	}

	// Load it back and verify
	loaded, err := LoadValidationConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadValidationConfig() error after save: %v", err)
	}

	if !loaded.Validation.Enabled {
		t.Error("loaded config has wrong enabled value")
	}
	if !loaded.Validation.Strict {
		t.Error("loaded config has wrong strict value")
	}
	if loaded.Validation.Timeout != "10m" {
		t.Errorf("loaded config has wrong timeout: %s", loaded.Validation.Timeout)
	}
	if len(loaded.Validation.Steps) != 1 {
		t.Fatalf("loaded config has wrong number of steps: %d", len(loaded.Validation.Steps))
	}
	if loaded.Validation.Steps[0].Name != "test" {
		t.Errorf("loaded config step has wrong name: %s", loaded.Validation.Steps[0].Name)
	}
}

func TestSaveConfig_CreatesDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validation-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Ensure .canopy directory doesn't exist
	configDir := filepath.Join(tmpDir, ".canopy")
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatal("expected .canopy directory to not exist")
	}

	cfg := DefaultValidationConfig()
	if err := SaveConfig(tmpDir, cfg); err != nil {
		t.Fatalf("SaveConfig() error: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Error("SaveConfig() did not create .canopy directory")
	}
}
