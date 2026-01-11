package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BwrapConfig holds configuration for bubblewrap sandbox
type BwrapConfig struct {
	// MergedDir is the overlay merged directory to expose as /workspace
	MergedDir string

	// Command is the command to run inside the sandbox
	Command string

	// Args are arguments to the command
	Args []string

	// Env is the environment variables to pass
	Env []string

	// ReadOnlyBinds are paths to bind-mount read-only (e.g., /nix/store)
	ReadOnlyBinds []string

	// ReadWriteBinds are paths to bind-mount read-write (e.g., cache dirs)
	ReadWriteBinds []string

	// MaxMemoryBytes limits virtual memory (0 = no limit)
	MaxMemoryBytes uint64

	// MaxProcesses limits number of processes (0 = no limit)
	MaxProcesses uint64

	// MaxOpenFiles limits file descriptors (0 = no limit)
	MaxOpenFiles uint64

	// SandboxConfig is the parsed .canopy/sandbox.toml config (optional)
	SandboxConfig *SandboxConfig
}

// DefaultReadOnlyBinds are system paths needed for most binaries
var DefaultReadOnlyBinds = []string{
	"/nix",       // Nix store (for nix-installed tools)
	"/usr",       // System binaries
	"/lib",       // Shared libraries
	"/lib64",     // 64-bit libraries (if exists)
	"/bin",       // Basic binaries
	"/etc/resolv.conf",      // DNS resolution
	"/etc/ssl/certs",        // SSL certificates
	"/etc/ca-certificates",  // CA certificates
}

// BwrapAvailable checks if bubblewrap is installed
func BwrapAvailable() bool {
	_, err := exec.LookPath("bwrap")
	return err == nil
}

// BuildBwrapCommand constructs a bwrap command with the given config
func BuildBwrapCommand(cfg *BwrapConfig) (*exec.Cmd, error) {
	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, fmt.Errorf("bwrap not found: %w", err)
	}

	args := []string{
		// Isolation flags
		"--unshare-user",
		"--unshare-pid",
		"--unshare-ipc",
		"--unshare-uts",
		"--die-with-parent",

		// Drop all capabilities
		"--cap-drop", "ALL",

		// Create a new /tmp
		"--tmpfs", "/tmp",

		// Bind the workspace
		"--bind", cfg.MergedDir, "/workspace",
		"--chdir", "/workspace",

		// Minimal /dev
		"--dev", "/dev",

		// Minimal /proc (for basic process info)
		"--proc", "/proc",
	}

	// Add read-only system binds (always needed)
	systemBinds := cfg.ReadOnlyBinds
	if len(systemBinds) == 0 {
		systemBinds = DefaultReadOnlyBinds
	}

	for _, bind := range systemBinds {
		// Only bind if source exists and is not blocked
		if _, err := os.Stat(bind); err == nil {
			if !isBlockedPath(bind, cfg.SandboxConfig) {
				args = append(args, "--ro-bind", bind, bind)
			}
		}
	}

	// Add paths from sandbox config if present
	if cfg.SandboxConfig != nil {
		// Add read-only tool paths from config
		for _, path := range cfg.SandboxConfig.GetAllReadOnlyPaths() {
			if _, err := os.Stat(path); err == nil {
				args = append(args, "--ro-bind", path, path)
			}
		}

		// Add read-write cache mounts from config
		for _, path := range cfg.SandboxConfig.GetAllCacheMounts() {
			if _, err := os.Stat(path); err == nil {
				args = append(args, "--bind", path, path)
			}
		}
	}

	// Add explicit read-write binds (from BwrapConfig, not sandbox.toml)
	for _, bind := range cfg.ReadWriteBinds {
		if _, err := os.Stat(bind); err == nil {
			if !isBlockedPath(bind, cfg.SandboxConfig) {
				args = append(args, "--bind", bind, bind)
			}
		}
	}

	// Get resource limits from config or use defaults
	maxMem := cfg.MaxMemoryBytes
	maxProcs := cfg.MaxProcesses
	maxFiles := cfg.MaxOpenFiles

	if cfg.SandboxConfig != nil {
		if cfg.SandboxConfig.Resources.MaxMemory != "" && maxMem == 0 {
			if parsed, err := ParseMemoryLimit(cfg.SandboxConfig.Resources.MaxMemory); err == nil {
				maxMem = parsed
			}
		}
		if cfg.SandboxConfig.Resources.MaxProcesses > 0 && maxProcs == 0 {
			maxProcs = uint64(cfg.SandboxConfig.Resources.MaxProcesses)
		}
		if cfg.SandboxConfig.Resources.MaxOpenFiles > 0 && maxFiles == 0 {
			maxFiles = uint64(cfg.SandboxConfig.Resources.MaxOpenFiles)
		}
	}

	// Add resource limits if specified
	if maxMem > 0 {
		args = append(args, "--rlimit", fmt.Sprintf("as=%d", maxMem))
	}
	if maxProcs > 0 {
		args = append(args, "--rlimit", fmt.Sprintf("nproc=%d", maxProcs))
	}
	if maxFiles > 0 {
		args = append(args, "--rlimit", fmt.Sprintf("nofile=%d", maxFiles))
	}

	// Add the command to run
	args = append(args, "--")
	args = append(args, cfg.Command)
	args = append(args, cfg.Args...)

	cmd := exec.Command(bwrapPath, args...)
	cmd.Dir = cfg.MergedDir

	// Set environment - HOME should point to /workspace inside sandbox
	cmd.Env = cfg.Env
	// Replace HOME with /workspace
	for i, e := range cmd.Env {
		if strings.HasPrefix(e, "HOME=") {
			cmd.Env[i] = "HOME=/workspace"
			break
		}
	}

	return cmd, nil
}

// isBlockedPath checks if a path is in the security blocklist
func isBlockedPath(path string, sandboxCfg *SandboxConfig) bool {
	// Check hardcoded blocklist
	for _, blocked := range SecurityBlocklist {
		blockedExpanded := ExpandPath(blocked)
		if path == blockedExpanded || strings.HasPrefix(path, blockedExpanded+"/") {
			return true
		}
	}

	// Check user-defined blocklist from config
	if sandboxCfg != nil {
		for _, blocked := range sandboxCfg.Security.Blocked {
			blockedExpanded := ExpandPath(blocked)
			if path == blockedExpanded || strings.HasPrefix(path, blockedExpanded+"/") {
				return true
			}
		}
	}

	return false
}

// ResolveCommandPath finds the full path to a command, searching common locations
func ResolveCommandPath(name string) (string, error) {
	// First try PATH
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}

	// Check common nix profile locations
	nixPaths := []string{
		filepath.Join(os.Getenv("HOME"), ".nix-profile/bin", name),
		"/nix/var/nix/profiles/default/bin/" + name,
		"/run/current-system/sw/bin/" + name,
	}

	for _, path := range nixPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("command not found: %s", name)
}
