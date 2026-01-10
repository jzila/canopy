package ipc

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/jzila/canopy/pkg/runtime"
)

// GetClient returns an IPC client connected to the canopy daemon.
// If the daemon is not running, it attempts to start it automatically.
// Returns an error if the daemon cannot be started or connection fails.
func GetClient() (*Client, error) {
	socketPath := runtime.SocketPath("")

	// First, try to connect to existing daemon
	client, err := NewClient(socketPath)
	if err == nil {
		return client, nil
	}

	// Daemon not running, try to start it
	if err := spawnDaemon(); err != nil {
		return nil, fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait for socket to appear
	if err := waitForSocket(socketPath, 5*time.Second); err != nil {
		return nil, fmt.Errorf("daemon started but socket not available: %w", err)
	}

	// Connect to the newly started daemon
	client, err = NewClient(socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon after start: %w", err)
	}

	return client, nil
}

// spawnDaemon starts the canopy daemon in the background
func spawnDaemon() error {
	// Ensure runtime directory exists
	if err := runtime.EnsureDir(); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}

	// Get the path to the canopy binary
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Spawn daemon as detached process
	cmd := exec.Command(executable, "daemon")

	// Detach from parent process
	cmd.SysProcAttr = getSysProcAttr()

	// Redirect output to /dev/null for now
	// TODO: Consider redirecting to runtime.LogPath() when that's available
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to spawn daemon: %w", err)
	}

	// Don't wait for process - let it run detached
	return nil
}

// waitForSocket waits for the socket file to appear, with timeout
func waitForSocket(socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			// Socket exists, give it a moment to be ready
			time.Sleep(100 * time.Millisecond)
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	return fmt.Errorf("socket did not appear within %v", timeout)
}
