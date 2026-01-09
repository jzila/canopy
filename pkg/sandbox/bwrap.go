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

	// MaxMemoryBytes limits virtual memory (0 = no limit)
	MaxMemoryBytes uint64

	// MaxProcesses limits number of processes (0 = no limit)
	MaxProcesses uint64

	// MaxOpenFiles limits file descriptors (0 = no limit)
	MaxOpenFiles uint64
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

	// Add read-only system binds
	binds := cfg.ReadOnlyBinds
	if len(binds) == 0 {
		binds = DefaultReadOnlyBinds
	}

	for _, bind := range binds {
		// Only bind if source exists
		if _, err := os.Stat(bind); err == nil {
			args = append(args, "--ro-bind", bind, bind)
		}
	}

	// Add resource limits if specified
	if cfg.MaxMemoryBytes > 0 {
		args = append(args, "--rlimit", fmt.Sprintf("as=%d", cfg.MaxMemoryBytes))
	}
	if cfg.MaxProcesses > 0 {
		args = append(args, "--rlimit", fmt.Sprintf("nproc=%d", cfg.MaxProcesses))
	}
	if cfg.MaxOpenFiles > 0 {
		args = append(args, "--rlimit", fmt.Sprintf("nofile=%d", cfg.MaxOpenFiles))
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
