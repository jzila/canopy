// Package config provides general configuration for canopy.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Config represents the .canopy/config.toml configuration
type Config struct {
	Resolver ResolverSettings `toml:"resolver"`
}

// ResolverSettings contains resolver agent configuration
type ResolverSettings struct {
	// Timeout is the timeout for resolver agent operations (e.g., "10m", "15m", "1h")
	// Default: 10 minutes
	Timeout string `toml:"timeout"`
}

// DefaultConfig returns a config with default values
func DefaultConfig() *Config {
	return &Config{
		Resolver: ResolverSettings{
			Timeout: "10m",
		},
	}
}

// LoadConfig loads configuration from .canopy/config.toml
// Returns a default config if the file doesn't exist.
// Returns an error if the file exists but cannot be read, parsed, or is invalid.
func LoadConfig(workDir string) (*Config, error) {
	configPath := filepath.Join(workDir, ".canopy", "config.toml")

	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var config Config
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
func (c *Config) Validate() error {
	if c == nil {
		return nil
	}

	var errs ValidationErrors

	// Validate resolver timeout format
	if c.Resolver.Timeout != "" {
		if _, err := time.ParseDuration(c.Resolver.Timeout); err != nil {
			errs = append(errs, ValidationError{
				Field:   "resolver.timeout",
				Message: fmt.Sprintf("invalid duration %q (expected e.g., \"10m\", \"15m\", \"1h\")", c.Resolver.Timeout),
			})
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// GetResolverTimeout parses and returns the resolver timeout duration from config.
// Returns 0 if not set (caller should use default).
func (c *Config) GetResolverTimeout() time.Duration {
	if c == nil || c.Resolver.Timeout == "" {
		return 0
	}
	d, err := time.ParseDuration(c.Resolver.Timeout)
	if err != nil {
		return 0
	}
	return d
}

// SaveConfig saves configuration to .canopy/config.toml
func SaveConfig(workDir string, config *Config) error {
	configDir := filepath.Join(workDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	configPath := filepath.Join(configDir, "config.toml")

	data, err := toml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Add header comment
	header := `# Canopy Configuration
# General configuration for canopy orchestration.
#
# Documentation: https://github.com/jzila/canopy/docs/CONFIGURATION.md

`
	return os.WriteFile(configPath, append([]byte(header), data...), 0644)
}
