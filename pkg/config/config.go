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
	Rules    RulesSettings    `toml:"rules"`
}

// RulesSettings contains task selection rules for filtering which tasks to run.
// All filtering uses explicit criteria - no fuzzy/natural language parsing.
type RulesSettings struct {
	// Priority filter (inclusive range, 0-4)
	// PriorityMin is the minimum priority to include (default: 0)
	PriorityMin int `toml:"priority_min"`
	// PriorityMax is the maximum priority to include (default: 4, -1 = no filter)
	PriorityMax int `toml:"priority_max"`

	// Type filters
	// Types is a whitelist of task types to include (empty = all types)
	Types []string `toml:"types"`
	// ExcludeTypes is a blacklist of task types to exclude
	ExcludeTypes []string `toml:"exclude_types"`

	// Label filters
	// Labels is a whitelist of labels - tasks must have at least one (empty = all)
	Labels []string `toml:"labels"`
	// ExcludeLabels is a blacklist of labels - tasks with any of these are excluded
	ExcludeLabels []string `toml:"exclude_labels"`

	// Assignee filter
	// "" = unassigned only, "*" = any assignee, specific value = exact match
	Assignee string `toml:"assignee"`

	// Behavior
	// StopWhenEmpty stops the run when no tasks match (for watch mode)
	StopWhenEmpty bool `toml:"stop_when_empty"`

	// Concurrency limits
	// MaxConcurrent overrides the global concurrency limit
	MaxConcurrent int `toml:"max_concurrent"`
	// MaxConcurrentPerType limits concurrent tasks by type (e.g., {"bug": 2})
	MaxConcurrentPerType map[string]int `toml:"max_concurrent_per_type"`
	// MaxConcurrentPerLabel limits concurrent tasks by label (e.g., {"frontend": 1})
	MaxConcurrentPerLabel map[string]int `toml:"max_concurrent_per_label"`

	// Custom rules for complex conditions
	Custom []CustomRule `toml:"custom"`
}

// CustomRule defines a named rule with a condition and action.
// This allows for more complex filtering logic beyond simple whitelists/blacklists.
type CustomRule struct {
	// Name is a human-readable identifier for the rule
	Name string `toml:"name"`
	// Enabled controls whether the rule is active (default: true if not specified)
	Enabled *bool `toml:"enabled"`
	// Condition is a simple expression like "priority > 1"
	// Supported: priority, type, assignee with operators: ==, !=, <, >, <=, >=
	Condition string `toml:"condition"`
	// Action is what to do when condition matches: "skip" or "include"
	Action string `toml:"action"`
	// Reason is shown when the rule causes a task to be skipped
	Reason string `toml:"reason"`
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
		Rules: DefaultRulesSettings(),
	}
}

// DefaultRulesSettings returns rules settings with sensible defaults
func DefaultRulesSettings() RulesSettings {
	return RulesSettings{
		PriorityMin:   0,
		PriorityMax:   -1, // -1 = no filter (include all priorities)
		Assignee:      "*", // "*" = any assignee
		StopWhenEmpty: false,
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

	// Validate rules settings
	errs = append(errs, c.Rules.Validate()...)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// validTaskTypes are the allowed task types per beads core schema
var validTaskTypes = map[string]bool{
	"bug":     true,
	"feature": true,
	"task":    true,
	"chore":   true,
	"epic":    true, // Epics are also valid types
}

// validCustomRuleActions are the allowed actions for custom rules
var validCustomRuleActions = map[string]bool{
	"skip":    true,
	"include": true,
}

// Validate checks the rules settings for errors
func (r *RulesSettings) Validate() ValidationErrors {
	var errs ValidationErrors

	// Validate priority range
	if r.PriorityMin < 0 || r.PriorityMin > 4 {
		errs = append(errs, ValidationError{
			Field:   "rules.priority_min",
			Message: fmt.Sprintf("must be 0-4, got %d", r.PriorityMin),
		})
	}
	if r.PriorityMax != -1 && (r.PriorityMax < 0 || r.PriorityMax > 4) {
		errs = append(errs, ValidationError{
			Field:   "rules.priority_max",
			Message: fmt.Sprintf("must be -1 (no filter) or 0-4, got %d", r.PriorityMax),
		})
	}
	if r.PriorityMax != -1 && r.PriorityMin > r.PriorityMax {
		errs = append(errs, ValidationError{
			Field:   "rules.priority_min",
			Message: fmt.Sprintf("priority_min (%d) cannot be greater than priority_max (%d)", r.PriorityMin, r.PriorityMax),
		})
	}

	// Validate type filters contain only valid types
	for _, t := range r.Types {
		if !validTaskTypes[t] {
			errs = append(errs, ValidationError{
				Field:   "rules.types",
				Message: fmt.Sprintf("invalid type %q (allowed: bug, feature, task, chore, epic)", t),
			})
		}
	}
	for _, t := range r.ExcludeTypes {
		if !validTaskTypes[t] {
			errs = append(errs, ValidationError{
				Field:   "rules.exclude_types",
				Message: fmt.Sprintf("invalid type %q (allowed: bug, feature, task, chore, epic)", t),
			})
		}
	}

	// Check for overlap between types and exclude_types
	if len(r.Types) > 0 && len(r.ExcludeTypes) > 0 {
		typeSet := make(map[string]bool)
		for _, t := range r.Types {
			typeSet[t] = true
		}
		for _, t := range r.ExcludeTypes {
			if typeSet[t] {
				errs = append(errs, ValidationError{
					Field:   "rules.exclude_types",
					Message: fmt.Sprintf("type %q appears in both types and exclude_types", t),
				})
			}
		}
	}

	// Check for overlap between labels and exclude_labels
	if len(r.Labels) > 0 && len(r.ExcludeLabels) > 0 {
		labelSet := make(map[string]bool)
		for _, l := range r.Labels {
			labelSet[l] = true
		}
		for _, l := range r.ExcludeLabels {
			if labelSet[l] {
				errs = append(errs, ValidationError{
					Field:   "rules.exclude_labels",
					Message: fmt.Sprintf("label %q appears in both labels and exclude_labels", l),
				})
			}
		}
	}

	// Validate concurrency limits are positive
	if r.MaxConcurrent < 0 {
		errs = append(errs, ValidationError{
			Field:   "rules.max_concurrent",
			Message: fmt.Sprintf("must be >= 0, got %d", r.MaxConcurrent),
		})
	}
	for typeName, limit := range r.MaxConcurrentPerType {
		if limit < 0 {
			errs = append(errs, ValidationError{
				Field:   "rules.max_concurrent_per_type",
				Message: fmt.Sprintf("limit for type %q must be >= 0, got %d", typeName, limit),
			})
		}
		if !validTaskTypes[typeName] {
			errs = append(errs, ValidationError{
				Field:   "rules.max_concurrent_per_type",
				Message: fmt.Sprintf("invalid type %q (allowed: bug, feature, task, chore, epic)", typeName),
			})
		}
	}
	for labelName, limit := range r.MaxConcurrentPerLabel {
		if limit < 0 {
			errs = append(errs, ValidationError{
				Field:   "rules.max_concurrent_per_label",
				Message: fmt.Sprintf("limit for label %q must be >= 0, got %d", labelName, limit),
			})
		}
	}

	// Validate custom rules
	for i, rule := range r.Custom {
		if rule.Name == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("rules.custom[%d].name", i),
				Message: "name is required",
			})
		}
		if rule.Condition == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("rules.custom[%d].condition", i),
				Message: "condition is required",
			})
		}
		if rule.Action == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("rules.custom[%d].action", i),
				Message: "action is required",
			})
		} else if !validCustomRuleActions[rule.Action] {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("rules.custom[%d].action", i),
				Message: fmt.Sprintf("invalid action %q (allowed: skip, include)", rule.Action),
			})
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
