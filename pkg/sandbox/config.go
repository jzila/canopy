package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// SandboxConfig represents the .canopy/sandbox.toml configuration
type SandboxConfig struct {
	Sandbox   SandboxSettings   `toml:"sandbox"`
	Resources ResourceSettings  `toml:"resources"`
	Paths     PathSettings      `toml:"paths"`
	Security  SecuritySettings  `toml:"security"`
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

// DefaultSandboxConfig returns a minimal default configuration
func DefaultSandboxConfig() *SandboxConfig {
	return &SandboxConfig{
		Sandbox: SandboxSettings{
			Enabled: true,
			Network: "allow",
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
	}
}

// LoadConfig loads sandbox configuration from .canopy/sandbox.toml
// Returns nil if the file doesn't exist.
// Returns an error if the file exists but cannot be read, parsed, or is invalid.
func LoadConfig(workDir string) (*SandboxConfig, error) {
	configPath := filepath.Join(workDir, ".canopy", "sandbox.toml")

	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil, nil // No config file, return nil to indicate no sandbox config
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var config SandboxConfig
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Validate the loaded configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

// LoadConfigWithoutValidation loads sandbox configuration without validating paths.
// Use this when you need to load config in contexts where paths may not exist yet
// (e.g., during setup or migration).
func LoadConfigWithoutValidation(workDir string) (*SandboxConfig, error) {
	configPath := filepath.Join(workDir, ".canopy", "sandbox.toml")

	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var config SandboxConfig
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &config, nil
}

// ValidateWithoutPaths validates the configuration but skips path existence checks.
// Useful for validating config structure before paths are created.
func (c *SandboxConfig) ValidateWithoutPaths() error {
	if c == nil {
		return nil
	}

	var errs ValidationErrors

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

	// Validate timeout format
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

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// SaveConfig saves sandbox configuration to .canopy/sandbox.toml
func SaveConfig(workDir string, config *SandboxConfig) error {
	configDir := filepath.Join(workDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	configPath := filepath.Join(configDir, "sandbox.toml")

	data, err := toml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Add header comment
	header := `# Canopy Sandbox Configuration
# Generated by: canopy init
# Documentation: https://github.com/jzila/canopy/docs/SANDBOX-DESIGN.md

`
	return os.WriteFile(configPath, append([]byte(header), data...), 0644)
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
func (c *SandboxConfig) GetAllReadOnlyPaths() []string {
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
func (c *SandboxConfig) GetAllCopyConfigs() []string {
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
func (c *SandboxConfig) GetAllCacheMounts() []string {
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
func (c *SandboxConfig) isBlocked(path string) bool {
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

// GetTimeout parses and returns the timeout duration from config.
// Returns 0 if not set (caller should use default).
func (c *SandboxConfig) GetTimeout() time.Duration {
	if c == nil || c.Resources.Timeout == "" {
		return 0
	}
	d, err := time.ParseDuration(c.Resources.Timeout)
	if err != nil {
		return 0
	}
	return d
}

// Valid network policy values
const (
	NetworkAllow = "allow"
	NetworkDeny  = "deny"
	NetworkProxy = "proxy" // Future feature
)

// ValidNetworkPolicies lists all valid network policy values
var ValidNetworkPolicies = []string{NetworkAllow, NetworkDeny, NetworkProxy}

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
func (c *SandboxConfig) Validate() error {
	if c == nil {
		return nil
	}

	var errs ValidationErrors

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

	// Validate disk limit format (same format as memory)
	if c.Resources.MaxDisk != "" {
		if _, err := ParseMemoryLimit(c.Resources.MaxDisk); err != nil {
			errs = append(errs, ValidationError{
				Field:   "resources.max_disk",
				Message: fmt.Sprintf("invalid format %q (expected e.g., \"10GB\", \"500MB\")", c.Resources.MaxDisk),
			})
		}
	}

	// Validate timeout format
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

	// Validate paths exist (with warnings for non-existent paths)
	errs = append(errs, c.validatePaths("paths.read_only", c.Paths.ReadOnly)...)
	errs = append(errs, c.validatePaths("paths.copy_configs", c.Paths.CopyConfigs)...)
	errs = append(errs, c.validatePaths("paths.cache_mounts", c.Paths.CacheMounts)...)
	errs = append(errs, c.validatePaths("paths.extra.read_only", c.Paths.Extra.ReadOnly)...)
	errs = append(errs, c.validatePaths("paths.extra.copy_configs", c.Paths.Extra.CopyConfigs)...)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// validatePaths checks that paths exist and are accessible
func (c *SandboxConfig) validatePaths(fieldName string, paths []string) ValidationErrors {
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
