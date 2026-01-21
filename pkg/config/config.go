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

// Config represents the unified .canopy/config.toml configuration
type Config struct {
	Resolver   ResolverSettings   `toml:"resolver"`
	Sandbox    SandboxSettings    `toml:"sandbox"`
	Resources  ResourceSettings   `toml:"resources"`
	Paths      PathSettings       `toml:"paths"`
	Security   SecuritySettings   `toml:"security"`
	Validation ValidationSettings `toml:"validation"`
}

// ResolverSettings contains resolver agent configuration
type ResolverSettings struct {
	// Timeout is the timeout for resolver agent operations (e.g., "10m", "15m", "1h")
	// Default: 10 minutes
	Timeout string `toml:"timeout"`
}

// SandboxSettings contains general sandbox options
type SandboxSettings struct {
	// Enabled controls whether bwrap sandboxing is active
	Enabled bool `toml:"enabled"`
	// Network access policy: "allow", "deny", or "proxy" (future)
	Network string `toml:"network"`
}

// ResourceSettings contains resource limits for each agent
type ResourceSettings struct {
	// MaxMemory limits virtual memory (e.g., "4GB", "2GB")
	MaxMemory string `toml:"max_memory"`
	// MaxProcesses limits the number of processes
	MaxProcesses int `toml:"max_processes"`
	// MaxOpenFiles limits file descriptors
	MaxOpenFiles int `toml:"max_open_files"`
	// MaxDisk limits disk usage (future feature)
	MaxDisk string `toml:"max_disk"`
	// Timeout is the default execution timeout for agents (e.g., "10m", "30m", "1h")
	// If not set, defaults to 10 minutes
	Timeout string `toml:"timeout"`
}

// PathSettings contains paths to expose to agents
type PathSettings struct {
	// ReadOnly paths are tool paths exposed read-only (e.g., ~/.cargo, ~/.nvm)
	ReadOnly []string `toml:"read_only"`
	// CopyConfigs are config files copied to the overlay (agent gets a copy)
	CopyConfigs []string `toml:"copy_configs"`
	// CacheMounts are cache directories mounted read-write for performance
	CacheMounts []string `toml:"cache_mounts"`
	// Extra allows custom paths
	Extra ExtraPathSettings `toml:"extra"`
}

// ExtraPathSettings contains user-defined custom paths
type ExtraPathSettings struct {
	ReadOnly    []string `toml:"read_only"`
	CopyConfigs []string `toml:"copy_configs"`
}

// SecuritySettings contains security-related configuration
type SecuritySettings struct {
	// Blocked paths are never exposed (in addition to built-in blocklist)
	Blocked []string `toml:"blocked"`
}

// ValidationSettings contains validation options
type ValidationSettings struct {
	// Enabled controls whether validation is active (default: false)
	Enabled bool `toml:"enabled"`
	// Strict mode fails the task if any validation step fails (default: false)
	Strict bool `toml:"strict"`
	// Timeout is the default timeout for all validation steps (e.g., "5m", "30s")
	Timeout string `toml:"timeout"`
	// MaxRepairAttempts is the maximum number of repair attempts before giving up (default: 3)
	MaxRepairAttempts int `toml:"max_repair_attempts"`
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

// Valid network policy values
const (
	NetworkAllow = "allow"
	NetworkDeny  = "deny"
	NetworkProxy = "proxy" // Future feature
)

// ValidNetworkPolicies lists all valid network policy values
var ValidNetworkPolicies = []string{NetworkAllow, NetworkDeny, NetworkProxy}

// SecurityBlocklist contains paths that are NEVER exposed regardless of config
var SecurityBlocklist = []string{
	"~/.ssh",
	"~/.gnupg",
	"~/.aws",
	"~/.azure",
	"~/.config/gcloud",
	"~/.kube",
	"~/.docker/config.json",
	"~/.netrc",
	"~/.password-store",
	"~/.local/share/keyrings",
	"/etc/shadow",
	"/etc/sudoers",
}

// DefaultConfig returns a config with default values
func DefaultConfig() *Config {
	return &Config{
		Resolver: ResolverSettings{
			Timeout: "10m",
		},
		Sandbox: SandboxSettings{
			Enabled: true,
			Network: NetworkAllow,
		},
		Resources: ResourceSettings{
			MaxMemory:    "4GB",
			MaxProcesses: 100,
			MaxOpenFiles: 1024,
			MaxDisk:      "10GB",
		},
		Paths: PathSettings{
			ReadOnly:    []string{},
			CopyConfigs: []string{},
			CacheMounts: []string{},
		},
		Security: SecuritySettings{
			Blocked: []string{
				"~/.ssh",
				"~/.gnupg",
				"~/.aws",
				"~/.azure",
				"~/.config/gcloud",
			},
		},
		Validation: ValidationSettings{
			Enabled:           false,
			Strict:            false,
			Timeout:           "5m",
			MaxRepairAttempts: 3,
			Steps:             []StepConfig{},
		},
	}
}

// LoadConfig loads configuration from .canopy/config.toml
// It first tries to load the consolidated config.toml. If that doesn't have
// sandbox or validation settings, it falls back to loading legacy separate files
// (sandbox.toml, validation.toml) with deprecation warnings.
// Returns a default config if no files exist.
func LoadConfig(workDir string) (*Config, error) {
	configPath := filepath.Join(workDir, ".canopy", "config.toml")
	sandboxPath := filepath.Join(workDir, ".canopy", "sandbox.toml")
	validationPath := filepath.Join(workDir, ".canopy", "validation.toml")

	config := DefaultConfig()
	var loadedFromMain bool

	// Try to load consolidated config.toml
	data, err := os.ReadFile(configPath)
	if err == nil {
		if err := toml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("parse config.toml: %w", err)
		}
		loadedFromMain = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read config.toml: %w", err)
	}

	// Check for legacy sandbox.toml (for backward compatibility)
	if _, err := os.Stat(sandboxPath); err == nil {
		// Only load if sandbox section wasn't in main config
		if !loadedFromMain || !config.hasSandboxSettings() {
			fmt.Fprintf(os.Stderr, "warning: sandbox.toml is deprecated, merge into config.toml\n")
			if err := loadLegacySandbox(sandboxPath, config); err != nil {
				return nil, fmt.Errorf("load sandbox.toml: %w", err)
			}
		}
	}

	// Check for legacy validation.toml (for backward compatibility)
	if _, err := os.Stat(validationPath); err == nil {
		// Only load if validation section wasn't in main config
		if !loadedFromMain || !config.hasValidationSettings() {
			fmt.Fprintf(os.Stderr, "warning: validation.toml is deprecated, merge into config.toml\n")
			if err := loadLegacyValidation(validationPath, config); err != nil {
				return nil, fmt.Errorf("load validation.toml: %w", err)
			}
		}
	}

	// Validate the loaded configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return config, nil
}

// hasSandboxSettings checks if sandbox settings were explicitly configured
func (c *Config) hasSandboxSettings() bool {
	// Check if any non-default sandbox values are set
	return len(c.Paths.ReadOnly) > 0 ||
		len(c.Paths.CopyConfigs) > 0 ||
		len(c.Paths.CacheMounts) > 0 ||
		c.Resources.MaxMemory != "" ||
		c.Resources.MaxProcesses > 0
}

// hasValidationSettings checks if validation settings were explicitly configured
func (c *Config) hasValidationSettings() bool {
	return len(c.Validation.Steps) > 0 || c.Validation.Enabled
}

// legacySandboxConfig mirrors the old sandbox.toml structure for loading
type legacySandboxConfig struct {
	Sandbox   SandboxSettings  `toml:"sandbox"`
	Resources ResourceSettings `toml:"resources"`
	Paths     PathSettings     `toml:"paths"`
	Security  SecuritySettings `toml:"security"`
}

// loadLegacySandbox loads from the old sandbox.toml format
func loadLegacySandbox(path string, config *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var legacy legacySandboxConfig
	if err := toml.Unmarshal(data, &legacy); err != nil {
		return err
	}

	config.Sandbox = legacy.Sandbox
	config.Resources = legacy.Resources
	config.Paths = legacy.Paths
	config.Security = legacy.Security

	return nil
}

// legacyValidationConfig mirrors the old validation.toml structure for loading
type legacyValidationConfig struct {
	Validation ValidationSettings `toml:"validation"`
}

// loadLegacyValidation loads from the old validation.toml format
func loadLegacyValidation(path string, config *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var legacy legacyValidationConfig
	if err := toml.Unmarshal(data, &legacy); err != nil {
		return err
	}

	config.Validation = legacy.Validation

	return nil
}

// LoadConfigWithoutValidation loads configuration without validating paths.
// Use this when you need to load config in contexts where paths may not exist yet
// (e.g., during setup or migration).
func LoadConfigWithoutValidation(workDir string) (*Config, error) {
	configPath := filepath.Join(workDir, ".canopy", "config.toml")
	sandboxPath := filepath.Join(workDir, ".canopy", "sandbox.toml")
	validationPath := filepath.Join(workDir, ".canopy", "validation.toml")

	config := DefaultConfig()
	var loadedFromMain bool

	// Try to load consolidated config.toml
	data, err := os.ReadFile(configPath)
	if err == nil {
		if err := toml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("parse config.toml: %w", err)
		}
		loadedFromMain = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read config.toml: %w", err)
	}

	// Check for legacy sandbox.toml
	if _, err := os.Stat(sandboxPath); err == nil {
		if !loadedFromMain || !config.hasSandboxSettings() {
			if err := loadLegacySandbox(sandboxPath, config); err != nil {
				return nil, fmt.Errorf("load sandbox.toml: %w", err)
			}
		}
	}

	// Check for legacy validation.toml
	if _, err := os.Stat(validationPath); err == nil {
		if !loadedFromMain || !config.hasValidationSettings() {
			if err := loadLegacyValidation(validationPath, config); err != nil {
				return nil, fmt.Errorf("load validation.toml: %w", err)
			}
		}
	}

	return config, nil
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

	// Validate network policy
	if c.Sandbox.Network != "" {
		validNetwork := false
		for _, valid := range ValidNetworkPolicies {
			if c.Sandbox.Network == valid {
				validNetwork = true
				break
			}
		}
		if !validNetwork {
			errs = append(errs, ValidationError{
				Field:   "sandbox.network",
				Message: fmt.Sprintf("invalid value %q, must be one of: %s", c.Sandbox.Network, strings.Join(ValidNetworkPolicies, ", ")),
			})
		}
	}

	// Validate memory limit format
	if c.Resources.MaxMemory != "" {
		if _, err := ParseMemoryLimit(c.Resources.MaxMemory); err != nil {
			errs = append(errs, ValidationError{
				Field:   "resources.max_memory",
				Message: fmt.Sprintf("invalid format %q (expected e.g., \"4GB\", \"512MB\")", c.Resources.MaxMemory),
			})
		}
	}

	// Validate disk limit format
	if c.Resources.MaxDisk != "" {
		if _, err := ParseMemoryLimit(c.Resources.MaxDisk); err != nil {
			errs = append(errs, ValidationError{
				Field:   "resources.max_disk",
				Message: fmt.Sprintf("invalid format %q (expected e.g., \"10GB\", \"500MB\")", c.Resources.MaxDisk),
			})
		}
	}

	// Validate resources timeout format
	if c.Resources.Timeout != "" {
		if _, err := time.ParseDuration(c.Resources.Timeout); err != nil {
			errs = append(errs, ValidationError{
				Field:   "resources.timeout",
				Message: fmt.Sprintf("invalid duration %q (expected e.g., \"10m\", \"1h\", \"30s\")", c.Resources.Timeout),
			})
		}
	}

	// Validate resource limits are positive
	if c.Resources.MaxProcesses < 0 {
		errs = append(errs, ValidationError{
			Field:   "resources.max_processes",
			Message: "must be non-negative",
		})
	}
	if c.Resources.MaxOpenFiles < 0 {
		errs = append(errs, ValidationError{
			Field:   "resources.max_open_files",
			Message: "must be non-negative",
		})
	}

	// Validate paths exist
	errs = append(errs, c.validatePaths("paths.read_only", c.Paths.ReadOnly)...)
	errs = append(errs, c.validatePaths("paths.copy_configs", c.Paths.CopyConfigs)...)
	errs = append(errs, c.validatePaths("paths.cache_mounts", c.Paths.CacheMounts)...)
	errs = append(errs, c.validatePaths("paths.extra.read_only", c.Paths.Extra.ReadOnly)...)
	errs = append(errs, c.validatePaths("paths.extra.copy_configs", c.Paths.Extra.CopyConfigs)...)

	// Validate validation timeout format
	if c.Validation.Timeout != "" {
		if _, err := time.ParseDuration(c.Validation.Timeout); err != nil {
			errs = append(errs, ValidationError{
				Field:   "validation.timeout",
				Message: fmt.Sprintf("invalid duration %q (expected e.g., \"5m\", \"30s\", \"1h\")", c.Validation.Timeout),
			})
		}
	}

	// Validate each validation step
	for i, step := range c.Validation.Steps {
		stepPrefix := fmt.Sprintf("validation.steps[%d]", i)

		if strings.TrimSpace(step.Name) == "" {
			errs = append(errs, ValidationError{
				Field:   stepPrefix + ".name",
				Message: "name is required",
			})
		}

		if strings.TrimSpace(step.Command) == "" {
			errs = append(errs, ValidationError{
				Field:   stepPrefix + ".command",
				Message: "command is required",
			})
		}

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

// ValidateWithoutPaths validates the configuration but skips path existence checks.
// Useful for validating config structure before paths are created.
func (c *Config) ValidateWithoutPaths() error {
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

	// Validate network policy
	if c.Sandbox.Network != "" {
		validNetwork := false
		for _, valid := range ValidNetworkPolicies {
			if c.Sandbox.Network == valid {
				validNetwork = true
				break
			}
		}
		if !validNetwork {
			errs = append(errs, ValidationError{
				Field:   "sandbox.network",
				Message: fmt.Sprintf("invalid value %q, must be one of: %s", c.Sandbox.Network, strings.Join(ValidNetworkPolicies, ", ")),
			})
		}
	}

	// Validate memory limit format
	if c.Resources.MaxMemory != "" {
		if _, err := ParseMemoryLimit(c.Resources.MaxMemory); err != nil {
			errs = append(errs, ValidationError{
				Field:   "resources.max_memory",
				Message: fmt.Sprintf("invalid format %q (expected e.g., \"4GB\", \"512MB\")", c.Resources.MaxMemory),
			})
		}
	}

	// Validate disk limit format
	if c.Resources.MaxDisk != "" {
		if _, err := ParseMemoryLimit(c.Resources.MaxDisk); err != nil {
			errs = append(errs, ValidationError{
				Field:   "resources.max_disk",
				Message: fmt.Sprintf("invalid format %q (expected e.g., \"10GB\", \"500MB\")", c.Resources.MaxDisk),
			})
		}
	}

	// Validate resources timeout format
	if c.Resources.Timeout != "" {
		if _, err := time.ParseDuration(c.Resources.Timeout); err != nil {
			errs = append(errs, ValidationError{
				Field:   "resources.timeout",
				Message: fmt.Sprintf("invalid duration %q (expected e.g., \"10m\", \"1h\", \"30s\")", c.Resources.Timeout),
			})
		}
	}

	// Validate resource limits are positive
	if c.Resources.MaxProcesses < 0 {
		errs = append(errs, ValidationError{
			Field:   "resources.max_processes",
			Message: "must be non-negative",
		})
	}
	if c.Resources.MaxOpenFiles < 0 {
		errs = append(errs, ValidationError{
			Field:   "resources.max_open_files",
			Message: "must be non-negative",
		})
	}

	// Validate validation timeout format
	if c.Validation.Timeout != "" {
		if _, err := time.ParseDuration(c.Validation.Timeout); err != nil {
			errs = append(errs, ValidationError{
				Field:   "validation.timeout",
				Message: fmt.Sprintf("invalid duration %q (expected e.g., \"5m\", \"30s\", \"1h\")", c.Validation.Timeout),
			})
		}
	}

	// Validate each validation step
	for i, step := range c.Validation.Steps {
		stepPrefix := fmt.Sprintf("validation.steps[%d]", i)

		if strings.TrimSpace(step.Name) == "" {
			errs = append(errs, ValidationError{
				Field:   stepPrefix + ".name",
				Message: "name is required",
			})
		}

		if strings.TrimSpace(step.Command) == "" {
			errs = append(errs, ValidationError{
				Field:   stepPrefix + ".command",
				Message: "command is required",
			})
		}

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

// validatePaths checks that paths exist and are accessible
func (c *Config) validatePaths(fieldName string, paths []string) ValidationErrors {
	var errs ValidationErrors
	for _, p := range paths {
		expanded := ExpandPath(p)
		if _, err := os.Stat(expanded); err != nil {
			if os.IsNotExist(err) {
				errs = append(errs, ValidationError{
					Field:   fieldName,
					Message: fmt.Sprintf("path does not exist: %s", p),
				})
			} else if os.IsPermission(err) {
				errs = append(errs, ValidationError{
					Field:   fieldName,
					Message: fmt.Sprintf("path not accessible (permission denied): %s", p),
				})
			}
		}
	}
	return errs
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

// GetResourcesTimeout parses and returns the resources timeout duration from config.
// Returns 0 if not set (caller should use default).
func (c *Config) GetResourcesTimeout() time.Duration {
	if c == nil || c.Resources.Timeout == "" {
		return 0
	}
	d, err := time.ParseDuration(c.Resources.Timeout)
	if err != nil {
		return 0
	}
	return d
}

// GetValidationTimeout parses and returns the validation timeout duration from config.
// Returns the default (5 minutes) if not set or invalid.
func (c *Config) GetValidationTimeout() time.Duration {
	if c == nil || c.Validation.Timeout == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(c.Validation.Timeout)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

// GetStepTimeout returns the timeout for a specific validation step.
// Falls back to the global validation timeout if the step doesn't have one.
func (c *Config) GetStepTimeout(step *StepConfig) time.Duration {
	if step.Timeout != "" {
		d, err := time.ParseDuration(step.Timeout)
		if err == nil {
			return d
		}
	}
	return c.GetValidationTimeout()
}

// GetMaxRepairAttempts returns the maximum number of repair attempts.
// Returns the default (3) if not set or config is nil.
func (c *Config) GetMaxRepairAttempts() int {
	if c == nil || c.Validation.MaxRepairAttempts <= 0 {
		return 3
	}
	return c.Validation.MaxRepairAttempts
}

// IsValidationEnabled returns whether validation is enabled.
func (c *Config) IsValidationEnabled() bool {
	return c != nil && c.Validation.Enabled
}

// IsValidationStrict returns whether strict mode is enabled.
func (c *Config) IsValidationStrict() bool {
	return c != nil && c.Validation.Strict
}

// ExpandPath expands ~ to the home directory
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// GetAllReadOnlyPaths returns all read-only paths with ~ expanded
func (c *Config) GetAllReadOnlyPaths() []string {
	var paths []string

	for _, p := range c.Paths.ReadOnly {
		expanded := ExpandPath(p)
		if !c.isBlocked(expanded) {
			paths = append(paths, expanded)
		}
	}

	for _, p := range c.Paths.Extra.ReadOnly {
		expanded := ExpandPath(p)
		if !c.isBlocked(expanded) {
			paths = append(paths, expanded)
		}
	}

	return paths
}

// GetAllCopyConfigs returns all config paths to copy, with ~ expanded
func (c *Config) GetAllCopyConfigs() []string {
	var paths []string

	for _, p := range c.Paths.CopyConfigs {
		expanded := ExpandPath(p)
		if !c.isBlocked(expanded) {
			paths = append(paths, expanded)
		}
	}

	for _, p := range c.Paths.Extra.CopyConfigs {
		expanded := ExpandPath(p)
		if !c.isBlocked(expanded) {
			paths = append(paths, expanded)
		}
	}

	return paths
}

// GetAllCacheMounts returns all cache mount paths with ~ expanded
func (c *Config) GetAllCacheMounts() []string {
	var paths []string

	for _, p := range c.Paths.CacheMounts {
		expanded := ExpandPath(p)
		if !c.isBlocked(expanded) {
			paths = append(paths, expanded)
		}
	}

	return paths
}

// isBlocked checks if a path is in the security blocklist
func (c *Config) isBlocked(path string) bool {
	// Check hardcoded blocklist
	for _, blocked := range SecurityBlocklist {
		blockedExpanded := ExpandPath(blocked)
		if path == blockedExpanded || strings.HasPrefix(path, blockedExpanded+"/") {
			return true
		}
	}

	// Check user-defined blocklist
	for _, blocked := range c.Security.Blocked {
		blockedExpanded := ExpandPath(blocked)
		if path == blockedExpanded || strings.HasPrefix(path, blockedExpanded+"/") {
			return true
		}
	}

	return false
}

// ParseMemoryLimit parses a memory string like "4GB" into bytes
func ParseMemoryLimit(s string) (uint64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, nil
	}

	var multiplier uint64 = 1
	var numStr string

	switch {
	case strings.HasSuffix(s, "GB"):
		multiplier = 1 << 30
		numStr = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		multiplier = 1 << 20
		numStr = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "KB"):
		multiplier = 1 << 10
		numStr = strings.TrimSuffix(s, "KB")
	case strings.HasSuffix(s, "B"):
		numStr = strings.TrimSuffix(s, "B")
	default:
		numStr = s
	}

	var num uint64
	_, err := fmt.Sscanf(numStr, "%d", &num)
	if err != nil {
		return 0, fmt.Errorf("invalid memory limit: %s", s)
	}

	return num * multiplier, nil
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
# Unified configuration for canopy orchestration.
#
# This file consolidates sandbox, validation, and resolver settings.
# Legacy sandbox.toml and validation.toml files are deprecated.
#
# Documentation: https://github.com/jzila/canopy/docs/CONFIGURATION.md

`
	return os.WriteFile(configPath, append([]byte(header), data...), 0644)
}
