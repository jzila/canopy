// Package validation provides configuration for task output validation.
package validation

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jzila/canopy/pkg/config"
	"github.com/pelletier/go-toml/v2"
)

// Type aliases for backward compatibility.
// These types are now defined in pkg/config and should be used from there.
// These aliases are deprecated and will be removed in a future version.
type (
	// ValidationConfig is deprecated. Use config.Config instead.
	// Deprecated: Use config.Config and access validation fields directly.
	ValidationConfig = legacyValidationConfig

	// ValidationSettings is deprecated. Use config.ValidationSettings instead.
	ValidationSettings = config.ValidationSettings

	// StepConfig is deprecated. Use config.StepConfig instead.
	StepConfig = config.StepConfig

	// ValidationError is deprecated. Use config.ValidationError instead.
	ValidationError = config.ValidationError

	// ValidationErrors is deprecated. Use config.ValidationErrors instead.
	ValidationErrors = config.ValidationErrors
)

// legacyValidationConfig is the old validation config structure used for backward compatibility
type legacyValidationConfig struct {
	Validation config.ValidationSettings `toml:"validation"`
}

// DefaultValidationConfig returns a config with validation disabled.
// Deprecated: Use config.DefaultConfig() instead.
func DefaultValidationConfig() *ValidationConfig {
	def := config.DefaultConfig()
	return &ValidationConfig{
		Validation: def.Validation,
	}
}

// LoadValidationConfig loads validation configuration from .canopy/validation.toml
// Returns a default config with validation disabled if the file doesn't exist.
// Returns an error if the file exists but cannot be read, parsed, or is invalid.
//
// Deprecated: Use config.LoadConfig() instead and access validation fields directly.
func LoadValidationConfig(workDir string) (*ValidationConfig, error) {
	configPath := filepath.Join(workDir, ".canopy", "validation.toml")

	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return DefaultValidationConfig(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg ValidationConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Validate the loaded configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// Validate checks the configuration for errors and returns all validation issues found.
// Returns nil if the configuration is valid.
func (c *ValidationConfig) Validate() error {
	if c == nil {
		return nil
	}

	// Convert to config.Config and validate
	cfg := &config.Config{
		Validation: c.Validation,
	}
	return cfg.ValidateWithoutPaths()
}

// GetTimeout parses and returns the global timeout duration from config.
// Returns the default (5 minutes) if not set or invalid.
func (c *ValidationConfig) GetTimeout() time.Duration {
	if c == nil {
		return 5 * time.Minute
	}
	cfg := &config.Config{
		Validation: c.Validation,
	}
	return cfg.GetValidationTimeout()
}

// GetStepTimeout returns the timeout for a specific step.
// Falls back to the global timeout if the step doesn't have one.
func (c *ValidationConfig) GetStepTimeout(step *StepConfig) time.Duration {
	cfg := &config.Config{
		Validation: c.Validation,
	}
	return cfg.GetStepTimeout(step)
}

// IsEnabled returns whether validation is enabled.
func (c *ValidationConfig) IsEnabled() bool {
	return c != nil && c.Validation.Enabled
}

// IsStrict returns whether strict mode is enabled.
func (c *ValidationConfig) IsStrict() bool {
	return c != nil && c.Validation.Strict
}

// GetMaxRepairAttempts returns the maximum number of repair attempts.
// Returns the default (3) if not set or config is nil.
func (c *ValidationConfig) GetMaxRepairAttempts() int {
	if c == nil || c.Validation.MaxRepairAttempts <= 0 {
		return 3
	}
	return c.Validation.MaxRepairAttempts
}

// SaveConfig saves validation configuration to .canopy/validation.toml
//
// Deprecated: Use config.SaveConfig() instead to save the unified config.toml.
func SaveConfig(workDir string, cfg *ValidationConfig) error {
	configDir := filepath.Join(workDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	configPath := filepath.Join(configDir, "validation.toml")

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Add header comment
	header := `# Canopy Validation Configuration
# DEPRECATED: This file is deprecated. Configuration should be in config.toml.
# Defines validation steps to run after task completion.
#
# Example:
#   [validation]
#   enabled = true
#   strict = false
#   timeout = "5m"
#
#   [[validation.steps]]
#   name = "typecheck"
#   command = "npm run type-check"
#   timeout = "2m"
#   required = true
#
#   [[validation.steps]]
#   name = "lint"
#   command = "npm run lint"
#   required = false

`
	return os.WriteFile(configPath, append([]byte(header), data...), 0644)
}
