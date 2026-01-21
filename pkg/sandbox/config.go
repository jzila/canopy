package sandbox

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
	// SandboxConfig is deprecated. Use config.Config instead.
	// Deprecated: Use config.Config and access sandbox fields directly.
	SandboxConfig = legacySandboxConfig

	// SandboxSettings is deprecated. Use config.SandboxSettings instead.
	SandboxSettings = config.SandboxSettings

	// ResourceSettings is deprecated. Use config.ResourceSettings instead.
	ResourceSettings = config.ResourceSettings

	// PathSettings is deprecated. Use config.PathSettings instead.
	PathSettings = config.PathSettings

	// ExtraPathSettings is deprecated. Use config.ExtraPathSettings instead.
	ExtraPathSettings = config.ExtraPathSettings

	// SecuritySettings is deprecated. Use config.SecuritySettings instead.
	SecuritySettings = config.SecuritySettings

	// ValidationError is deprecated. Use config.ValidationError instead.
	ValidationError = config.ValidationError

	// ValidationErrors is deprecated. Use config.ValidationErrors instead.
	ValidationErrors = config.ValidationErrors
)

// legacySandboxConfig is the old sandbox config structure used for backward compatibility
type legacySandboxConfig struct {
	Sandbox   config.SandboxSettings  `toml:"sandbox"`
	Resources config.ResourceSettings `toml:"resources"`
	Paths     config.PathSettings     `toml:"paths"`
	Security  config.SecuritySettings `toml:"security"`
}

// Constant aliases for backward compatibility
const (
	// NetworkAllow is deprecated. Use config.NetworkAllow instead.
	NetworkAllow = config.NetworkAllow
	// NetworkDeny is deprecated. Use config.NetworkDeny instead.
	NetworkDeny = config.NetworkDeny
	// NetworkProxy is deprecated. Use config.NetworkProxy instead.
	NetworkProxy = config.NetworkProxy
)

// Variable aliases for backward compatibility
var (
	// ValidNetworkPolicies is deprecated. Use config.ValidNetworkPolicies instead.
	ValidNetworkPolicies = config.ValidNetworkPolicies
	// SecurityBlocklist is deprecated. Use config.SecurityBlocklist instead.
	SecurityBlocklist = config.SecurityBlocklist
)

// Function aliases for backward compatibility
var (
	// ExpandPath is deprecated. Use config.ExpandPath instead.
	ExpandPath = config.ExpandPath
	// ParseMemoryLimit is deprecated. Use config.ParseMemoryLimit instead.
	ParseMemoryLimit = config.ParseMemoryLimit
)

// DefaultSandboxConfig returns a minimal default configuration.
// Deprecated: Use config.DefaultConfig() instead.
func DefaultSandboxConfig() *SandboxConfig {
	def := config.DefaultConfig()
	return &SandboxConfig{
		Sandbox:   def.Sandbox,
		Resources: def.Resources,
		Paths:     def.Paths,
		Security:  def.Security,
	}
}

// LoadConfig loads sandbox configuration from .canopy/sandbox.toml
// Returns nil if the file doesn't exist.
// Returns an error if the file exists but cannot be read, parsed, or is invalid.
//
// Deprecated: Use config.LoadConfig() instead and access sandbox fields directly.
// This function only loads from the legacy sandbox.toml file.
func LoadConfig(workDir string) (*SandboxConfig, error) {
	configPath := filepath.Join(workDir, ".canopy", "sandbox.toml")

	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil, nil // No config file, return nil to indicate no sandbox config
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg SandboxConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Validate the loaded configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// LoadConfigWithoutValidation loads sandbox configuration without validating paths.
// Use this when you need to load config in contexts where paths may not exist yet
// (e.g., during setup or migration).
//
// Deprecated: Use config.LoadConfigWithoutValidation() instead.
func LoadConfigWithoutValidation(workDir string) (*SandboxConfig, error) {
	configPath := filepath.Join(workDir, ".canopy", "sandbox.toml")

	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg SandboxConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}

// ValidateWithoutPaths validates the configuration but skips path existence checks.
// Useful for validating config structure before paths are created.
func (c *SandboxConfig) ValidateWithoutPaths() error {
	if c == nil {
		return nil
	}

	// Convert to config.Config and validate without paths
	cfg := &config.Config{
		Sandbox:   c.Sandbox,
		Resources: c.Resources,
		Paths:     c.Paths,
		Security:  c.Security,
	}
	return cfg.ValidateWithoutPaths()
}

// SaveConfig saves sandbox configuration to .canopy/sandbox.toml
//
// Deprecated: Use config.SaveConfig() instead to save the unified config.toml.
func SaveConfig(workDir string, cfg *SandboxConfig) error {
	configDir := filepath.Join(workDir, ".canopy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	configPath := filepath.Join(configDir, "sandbox.toml")

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Add header comment
	header := `# Canopy Sandbox Configuration
# DEPRECATED: This file is deprecated. Configuration should be in config.toml.
# Generated by: canopy init
# Documentation: https://github.com/jzila/canopy/docs/SANDBOX-DESIGN.md

`
	return os.WriteFile(configPath, append([]byte(header), data...), 0644)
}

// GetAllReadOnlyPaths returns all read-only paths with ~ expanded
func (c *SandboxConfig) GetAllReadOnlyPaths() []string {
	cfg := &config.Config{
		Paths:    c.Paths,
		Security: c.Security,
	}
	return cfg.GetAllReadOnlyPaths()
}

// GetAllCopyConfigs returns all config paths to copy, with ~ expanded
func (c *SandboxConfig) GetAllCopyConfigs() []string {
	cfg := &config.Config{
		Paths:    c.Paths,
		Security: c.Security,
	}
	return cfg.GetAllCopyConfigs()
}

// GetAllCacheMounts returns all cache mount paths with ~ expanded
func (c *SandboxConfig) GetAllCacheMounts() []string {
	cfg := &config.Config{
		Paths:    c.Paths,
		Security: c.Security,
	}
	return cfg.GetAllCacheMounts()
}

// GetTimeout parses and returns the timeout duration from config.
// Returns 0 if not set (caller should use default).
func (c *SandboxConfig) GetTimeout() time.Duration {
	if c == nil {
		return 0
	}
	cfg := &config.Config{
		Resources: c.Resources,
	}
	return cfg.GetResourcesTimeout()
}

// Validate checks the configuration for errors and returns all validation issues found.
// Returns nil if the configuration is valid.
func (c *SandboxConfig) Validate() error {
	if c == nil {
		return nil
	}

	// Convert to config.Config and validate
	cfg := &config.Config{
		Sandbox:   c.Sandbox,
		Resources: c.Resources,
		Paths:     c.Paths,
		Security:  c.Security,
	}
	return cfg.Validate()
}
