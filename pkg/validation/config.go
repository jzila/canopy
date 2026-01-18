// Package validation provides configuration for task output validation.
package validation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// ValidationConfig represents the .canopy/validation.toml configuration
type ValidationConfig struct {
	Validation ValidationSettings `toml:"validation"`
}

// ValidationSettings contains validation options
type ValidationSettings struct {
	// Enabled controls whether validation is active (default: false)
	Enabled bool `toml:"enabled"`
	// Strict mode fails the task if any validation step fails (default: false)
	Strict bool `toml:"strict"`
	// Timeout is the default timeout for all validation steps (e.g., "5m", "30s")
	Timeout string `toml:"timeout"`
	// Steps defines the validation steps to run
	Steps []StepConfig `toml:"steps"`
}

// StepConfig defines a single validation step
type StepConfig struct {
	// Name is a human-readable identifier for this step
	Name string `toml:"name"`
	// Command is the shell command to execute
	Command string `toml:"command"`
	// Timeout overrides the global timeout for this step (e.g., "2m", "10s")
	Timeout string `toml:"timeout"`
	// Required marks this step as mandatory; if true and it fails, validation fails
	Required bool `toml:"required"`
}

// DefaultValidationConfig returns a config with validation disabled
func DefaultValidationConfig() *ValidationConfig {
	return &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: false,
			Strict:  false,
			Timeout: "5m",
			Steps:   []StepConfig{},
		},
	}
}

// LoadValidationConfig loads validation configuration from .canopy/validation.toml
// Returns a default config with validation disabled if the file doesn't exist.
// Returns an error if the file exists but cannot be read, parsed, or is invalid.
func LoadValidationConfig(workDir string) (*ValidationConfig, error) {
	configPath := filepath.Join(workDir, ".canopy", "validation.toml")

	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return DefaultValidationConfig(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var config ValidationConfig
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Validate the loaded configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

// ValidationError contains details about a configuration validation failure
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return "no validation errors"
	}
	if len(e) == 1 {
		return e[0].Error()
	}
	var msgs []string
	for _, err := range e {
		msgs = append(msgs, err.Error())
	}
	return fmt.Sprintf("multiple validation errors:\n  - %s", strings.Join(msgs, "\n  - "))
}

// Validate checks the configuration for errors and returns all validation issues found.
// Returns nil if the configuration is valid.
func (c *ValidationConfig) Validate() error {
	if c == nil {
		return nil
	}

	var errs ValidationErrors

	// Validate global timeout format
	if c.Validation.Timeout != "" {
		if _, err := time.ParseDuration(c.Validation.Timeout); err != nil {
			errs = append(errs, ValidationError{
				Field:   "validation.timeout",
				Message: fmt.Sprintf("invalid duration %q (expected e.g., \"5m\", \"30s\", \"1h\")", c.Validation.Timeout),
			})
		}
	}

	// Validate each step
	for i, step := range c.Validation.Steps {
		stepPrefix := fmt.Sprintf("validation.steps[%d]", i)

		// Name is required
		if strings.TrimSpace(step.Name) == "" {
			errs = append(errs, ValidationError{
				Field:   stepPrefix + ".name",
				Message: "name is required",
			})
		}

		// Command is required
		if strings.TrimSpace(step.Command) == "" {
			errs = append(errs, ValidationError{
				Field:   stepPrefix + ".command",
				Message: "command is required",
			})
		}

		// Validate step timeout format if provided
		if step.Timeout != "" {
			if _, err := time.ParseDuration(step.Timeout); err != nil {
				errs = append(errs, ValidationError{
					Field:   stepPrefix + ".timeout",
					Message: fmt.Sprintf("invalid duration %q (expected e.g., \"2m\", \"30s\")", step.Timeout),
				})
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// GetTimeout parses and returns the global timeout duration from config.
// Returns the default (5 minutes) if not set or invalid.
func (c *ValidationConfig) GetTimeout() time.Duration {
	if c == nil || c.Validation.Timeout == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(c.Validation.Timeout)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

// GetStepTimeout returns the timeout for a specific step.
// Falls back to the global timeout if the step doesn't have one.
func (c *ValidationConfig) GetStepTimeout(step *StepConfig) time.Duration {
	if step.Timeout != "" {
		d, err := time.ParseDuration(step.Timeout)
		if err == nil {
			return d
		}
	}
	return c.GetTimeout()
}

// IsEnabled returns whether validation is enabled.
func (c *ValidationConfig) IsEnabled() bool {
	return c != nil && c.Validation.Enabled
}

// IsStrict returns whether strict mode is enabled.
func (c *ValidationConfig) IsStrict() bool {
	return c != nil && c.Validation.Strict
}

// SaveConfig saves validation configuration to .canopy/validation.toml
func SaveConfig(workDir string, config *ValidationConfig) error {
	configDir := filepath.Join(workDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	configPath := filepath.Join(configDir, "validation.toml")

	data, err := toml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Add header comment
	header := `# Canopy Validation Configuration
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
