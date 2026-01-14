package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jzila/canopy/pkg/daemon"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/runtime"
	"github.com/jzila/canopy/pkg/tui"
)

var (
	daemonPort      int
	daemonSocket    string
	daemonDevMode   bool
	daemonTUIMode   bool
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start canopy daemon server",
	Long: `Start the canopy daemon HTTP and IPC server.

The daemon provides:
  - HTTP REST API on the specified port (default 8080)
  - WebSocket endpoint for real-time updates at /ws
  - Unix socket IPC server for receiving events from 'canopy run'

Example:
  # Start daemon with defaults
  canopy daemon

  # Start on custom port and socket
  canopy daemon --port 9090 --ipc-socket /tmp/my-canopy.sock

  # Start in development mode
  canopy daemon --dev

  # Start with TUI dashboard
  canopy daemon --tui

  # Check if daemon is running
  canopy daemon status`,
	RunE: runDaemon,
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check daemon status",
	Long: `Check if the canopy daemon is running.

Reports the daemon's running status and PID.

Exit codes:
  0 - Daemon is running
  1 - Daemon is not running or error occurred`,
	RunE: runDaemonStatus,
}

func init() {
	daemonCmd.Flags().IntVar(&daemonPort, "port", 8080, "HTTP server port")
	daemonCmd.Flags().StringVar(&daemonSocket, "ipc-socket", "", "Unix socket path for IPC (default: runtime dir)")
	daemonCmd.Flags().BoolVar(&daemonDevMode, "dev", false, "Enable development mode")
	daemonCmd.Flags().BoolVar(&daemonTUIMode, "tui", false, "Enable TUI dashboard view")

	daemonCmd.AddCommand(daemonStatusCmd)
	rootCmd.AddCommand(daemonCmd)
}

func runDaemon(cmd *cobra.Command, args []string) error {
	// Use runtime socket path if not specified
	if daemonSocket == "" {
		daemonSocket = runtime.SocketPath("")
	}

	// Check if daemon is already running
	running, pid, err := daemon.IsRunning()
	if err != nil {
		return fmt.Errorf("failed to check daemon status: %w", err)
	}
	if running {
		return fmt.Errorf("daemon is already running (pid %d)", pid)
	}

	if verbose && !daemonTUIMode {
		fmt.Printf("Starting daemon on port %d with IPC socket %s\n", daemonPort, daemonSocket)
	}

	// Ensure runtime directory exists
	if err := runtime.EnsureDir(); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}

	// Create daemon configuration
	config := daemon.Config{
		Port:              daemonPort,
		SocketPath:        daemonSocket,
		EnablePersistence: true,
	}

	// Create IPC server factory that wraps ipc.NewServer
	// This avoids circular dependency issues between pkg/daemon and pkg/ipc
	ipcServerFactory := func(socketPath string, eventBus *daemon.EventBus) daemon.IPCServer {
		return ipc.NewServer(socketPath, eventBus)
	}

	// Create daemon with nil scheduler and beads client
	// These are optional - the daemon can run standalone for monitoring
	d := daemon.NewDaemon(config, ipcServerFactory, nil, nil)

	if daemonTUIMode {
		return runDaemonWithTUI(d)
	}

	// Start daemon (blocks until interrupted)
	if err := d.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon error: %v\n", err)
		return err
	}

	return nil
}

// runDaemonWithTUI starts the daemon and TUI dashboard concurrently
func runDaemonWithTUI(d *daemon.Daemon) error {
	// Initialize daemon components first (before Start which blocks)
	d.Init()

	// Get event bus and state from daemon for TUI
	eventBus := d.GetEventBus()
	state := d.GetRuntimeState()

	// Create TUI dashboard
	dashboard := tui.NewDashboard(state, eventBus)

	// Start daemon in background (non-blocking)
	errChan := make(chan error, 1)
	go func() {
		if err := d.Start(); err != nil {
			errChan <- err
		}
	}()

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Run TUI in a goroutine
	tuiDone := make(chan error, 1)
	go func() {
		tuiDone <- dashboard.Run()
	}()

	// Wait for TUI to exit, daemon error, or signal
	select {
	case err := <-errChan:
		return fmt.Errorf("daemon error: %w", err)
	case err := <-tuiDone:
		// TUI exited (user pressed q), stop daemon
		d.Stop()
		return err
	case <-sigChan:
		// Signal received, stop both
		d.Stop()
		return nil
	}
}

// runDaemonStatus checks if the daemon is running using the pidfile
func runDaemonStatus(cmd *cobra.Command, args []string) error {
	running, pid, err := daemon.IsRunning()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking daemon status: %v\n", err)
		os.Exit(1)
	}

	if running {
		fmt.Printf("Daemon is running (pid %d)\n", pid)
		return nil
	}

	fmt.Println("Daemon is not running")
	os.Exit(1)
	return nil // unreachable, but satisfies compiler
}
