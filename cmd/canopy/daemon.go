package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/daemon"
	"github.com/jzila/canopy/pkg/events"
	"github.com/jzila/canopy/pkg/ipc"
	"github.com/jzila/canopy/pkg/logging"
	"github.com/jzila/canopy/pkg/runtime"
	"github.com/jzila/canopy/pkg/tui"
)

var (
	daemonPort      int
	daemonSocket    string
	daemonDevMode   bool
	daemonTUIMode   bool
	daemonAddr      string // explicit daemon address for remote TUI connection
	daemonLogLevel  string // log level (debug, info, warn, error)
	daemonLogJSON   bool   // enable JSON log output
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

  # Start with TUI dashboard (starts new daemon if not running)
  canopy daemon --tui

  # Connect TUI to existing daemon (auto-detects if running)
  canopy daemon --tui

  # Connect TUI to daemon at specific address
  canopy daemon --tui --daemon-addr localhost:8080

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

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start daemon in background",
	Long: `Start the canopy daemon as a background process.

The daemon will be started in detached mode with output redirected
to the log file at ~/.cache/canopy/daemon.log.

If the daemon is already running, this command will report an error.

Example:
  # Start daemon in background
  canopy daemon start

  # Check status after starting
  canopy daemon status`,
	RunE: runDaemonStart,
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop running daemon",
	Long: `Stop the canopy daemon gracefully.

Sends SIGTERM to the daemon process and waits for it to exit.
Cleans up the pidfile after the daemon stops.

Exit codes:
  0 - Daemon stopped successfully
  1 - Daemon is not running or error occurred`,
	RunE: runDaemonStop,
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart daemon (stop + start)",
	Long: `Restart the canopy daemon by stopping any running instance and starting fresh.

If the daemon is running, sends SIGTERM and waits for it to exit.
Then starts a new daemon process in the background.

If no daemon is running, simply starts a new daemon.

Example:
  # Restart daemon
  canopy daemon restart

  # Check status after restart
  canopy daemon status`,
	RunE: runDaemonRestart,
}

var daemonLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show daemon logs",
	Long: `Show daemon log output.

Displays log entries from the daemon log file at ~/.cache/canopy/daemon.log.

Example:
  # Show last 50 lines (default)
  canopy daemon logs

  # Show last 100 lines
  canopy daemon logs -n 100

  # Follow log output (like tail -f)
  canopy daemon logs -f

  # Follow with last 20 lines of context
  canopy daemon logs -f -n 20`,
	RunE: runDaemonLogs,
}

var (
	logsFollow bool
	logsLines  int
)

func init() {
	daemonCmd.Flags().IntVar(&daemonPort, "port", 8080, "HTTP server port")
	daemonCmd.Flags().StringVar(&daemonSocket, "ipc-socket", "", "Unix socket path for IPC (default: runtime dir)")
	daemonCmd.Flags().BoolVar(&daemonDevMode, "dev", false, "Enable development mode")
	daemonCmd.Flags().BoolVar(&daemonTUIMode, "tui", false, "Enable TUI dashboard view")
	daemonCmd.Flags().StringVar(&daemonAddr, "daemon-addr", "", "Connect TUI to daemon at specified address (e.g., localhost:8080)")
	daemonCmd.Flags().StringVar(&daemonLogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	daemonCmd.Flags().BoolVar(&daemonLogJSON, "log-json", false, "Output logs in JSON format")

	daemonLogsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log file (like tail -f)")
	daemonLogsCmd.Flags().IntVarP(&logsLines, "lines", "n", 50, "Number of lines to show")

	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
	daemonCmd.AddCommand(daemonLogsCmd)
	rootCmd.AddCommand(daemonCmd)
}

func runDaemon(cmd *cobra.Command, args []string) error {
	// Use runtime socket path if not specified
	if daemonSocket == "" {
		daemonSocket = runtime.SocketPath("")
	}

	// Configure logging based on flags
	logLevel, err := logging.ParseLevel(daemonLogLevel)
	if err != nil {
		return fmt.Errorf("invalid log level: %w", err)
	}

	logConfig := logging.Config{
		Level:     logLevel,
		JSON:      daemonLogJSON,
		Component: "daemon",
	}

	// Set up file logging (writes to both stderr and log file)
	cleanup, err := logging.SetupFileLogging(logConfig)
	if err != nil {
		// Fall back to stderr-only logging
		logging.Configure(logConfig)
		logging.Warn("failed to setup file logging, using stderr only", "error", err)
	} else {
		defer cleanup()
	}

	// If --daemon-addr is specified with --tui, connect directly to that daemon
	if daemonAddr != "" {
		if !daemonTUIMode {
			return fmt.Errorf("--daemon-addr requires --tui flag")
		}
		return runRemoteTUI(daemonAddr)
	}

	// Check if daemon is already running
	running, pid, err := daemon.IsRunning()
	if err != nil {
		return fmt.Errorf("failed to check daemon status: %w", err)
	}

	// If daemon is running and --tui is specified, connect to existing daemon
	if running && daemonTUIMode {
		addr := fmt.Sprintf("localhost:%d", daemonPort)
		if verbose {
			fmt.Printf("Daemon is already running (pid %d), connecting to %s...\n", pid, addr)
		}
		return runRemoteTUI(addr)
	}

	// If daemon is running without --tui, error out
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
	ipcServerFactory := func(socketPath string, eventBus *events.EventBus) daemon.IPCServer {
		return ipc.NewServer(socketPath, eventBus)
	}

	// Create daemon with nil scheduler and beads client
	// These are optional - the daemon can run standalone for monitoring
	d := daemon.NewDaemon(config, ipcServerFactory, nil, nil)

	// Set up beads client factory for loading tasks from repositories
	// The factory creates beads clients on-demand for different repos
	d.SetBeadsClientFactory(func(repoPath string) (daemon.BeadsClientInterface, error) {
		return beads.NewClient(repoPath)
	})

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

// runRemoteTUI starts the TUI dashboard connected to a remote daemon via WebSocket
func runRemoteTUI(addr string) error {
	return tui.RunRemote(addr)
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

// runDaemonStart starts the daemon in background mode
func runDaemonStart(cmd *cobra.Command, args []string) error {
	// Check if daemon is already running
	running, pid, err := daemon.IsRunning()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking daemon status: %v\n", err)
		os.Exit(1)
	}
	if running {
		fmt.Fprintf(os.Stderr, "Daemon is already running (pid %d)\n", pid)
		os.Exit(1)
	}

	// Spawn daemon in background
	if err := ipc.SpawnDaemon(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Daemon started")
	return nil
}

// runDaemonStop stops the running daemon gracefully
func runDaemonStop(cmd *cobra.Command, args []string) error {
	// Get PID first for reporting
	running, pid, err := daemon.IsRunning()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking daemon status: %v\n", err)
		os.Exit(1)
	}

	if !running {
		fmt.Println("Daemon is not running")
		os.Exit(1)
	}

	fmt.Printf("Stopping daemon (pid %d)...\n", pid)

	// Stop with 5 second timeout
	if err := daemon.StopDaemon(5 * time.Second); err != nil {
		if errors.Is(err, daemon.ErrDaemonNotRunning) {
			// Race condition: daemon exited between check and stop
			fmt.Println("Daemon stopped")
			return nil
		}
		fmt.Fprintf(os.Stderr, "Error stopping daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Daemon stopped")
	return nil
}

// runDaemonRestart stops any running daemon and starts a fresh one
func runDaemonRestart(cmd *cobra.Command, args []string) error {
	// Check if daemon is running
	running, pid, err := daemon.IsRunning()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking daemon status: %v\n", err)
		os.Exit(1)
	}

	// Stop if running
	if running {
		fmt.Printf("Stopping daemon (pid %d)...\n", pid)
		if err := daemon.StopDaemon(5 * time.Second); err != nil {
			if !errors.Is(err, daemon.ErrDaemonNotRunning) {
				fmt.Fprintf(os.Stderr, "Error stopping daemon: %v\n", err)
				os.Exit(1)
			}
		}
		fmt.Println("Daemon stopped")
	}

	// Start daemon
	if err := ipc.SpawnDaemon(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Daemon started")
	return nil
}

// runDaemonLogs shows daemon log output
func runDaemonLogs(cmd *cobra.Command, args []string) error {
	if logsFollow {
		return daemon.TailFollow(logsLines)
	}
	return daemon.TailLines(logsLines)
}
