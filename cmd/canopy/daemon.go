package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/john/canopy/pkg/daemon"
	"github.com/john/canopy/pkg/ipc"
)

var (
	daemonPort      int
	daemonSocket    string
	daemonDevMode   bool
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
  canopy daemon --dev`,
	RunE: runDaemon,
}

func init() {
	daemonCmd.Flags().IntVar(&daemonPort, "port", 8080, "HTTP server port")
	daemonCmd.Flags().StringVar(&daemonSocket, "ipc-socket", "/tmp/canopyd.sock", "Unix socket path for IPC")
	daemonCmd.Flags().BoolVar(&daemonDevMode, "dev", false, "Enable development mode")

	rootCmd.AddCommand(daemonCmd)
}

func runDaemon(cmd *cobra.Command, args []string) error {
	if verbose {
		fmt.Printf("Starting daemon on port %d with IPC socket %s\n", daemonPort, daemonSocket)
	}

	// Create daemon configuration
	config := daemon.Config{
		Port:       daemonPort,
		SocketPath: daemonSocket,
	}

	// Create IPC server factory that wraps ipc.NewServer
	// This avoids circular dependency issues between pkg/daemon and pkg/ipc
	ipcServerFactory := func(socketPath string, eventBus *daemon.EventBus) daemon.IPCServer {
		return ipc.NewServer(socketPath, eventBus)
	}

	// Create daemon with nil scheduler and beads client
	// These are optional - the daemon can run standalone for monitoring
	d := daemon.NewDaemon(config, ipcServerFactory, nil, nil)

	// Start daemon (blocks until interrupted)
	if err := d.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon error: %v\n", err)
		return err
	}

	return nil
}
