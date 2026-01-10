package main

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"

	"github.com/john/canopy/pkg/daemon"
	"github.com/john/canopy/pkg/ipc"
)

var (
	port      int
	ipcSocket string
	dev       bool
)

var rootCmd = &cobra.Command{
	Use:   "canopyd",
	Short: "Canopy daemon - HTTP API and WebSocket server",
	Long: `canopyd is the background daemon for Canopy that provides:
- HTTP REST API for task management
- WebSocket server for real-time event streaming
- IPC server for receiving events from canopy run

The daemon runs until it receives SIGTERM or SIGINT.`,
	RunE: runDaemon,
}

func init() {
	rootCmd.Flags().IntVarP(&port, "port", "p", 8080, "HTTP server port")
	rootCmd.Flags().StringVar(&ipcSocket, "ipc-socket", "/tmp/canopyd.sock", "Unix socket path for IPC")
	rootCmd.Flags().BoolVar(&dev, "dev", false, "Development mode (serve static files from disk)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runDaemon(cmd *cobra.Command, args []string) error {
	// Configure daemon
	config := daemon.Config{
		Port:       port,
		SocketPath: ipcSocket,
	}

	// Create IPC server factory wrapper
	// The wrapper converts *ipc.Server to daemon.IPCServer interface
	ipcFactory := func(socketPath string, eventBus *daemon.EventBus) daemon.IPCServer {
		return ipc.NewServer(socketPath, eventBus)
	}

	// Create daemon with IPC server factory
	// We pass nil for scheduler and beadsClient since they're not yet implemented
	// TODO: Implement real scheduler and beads client
	d := daemon.NewDaemon(config, ipcFactory, nil, nil)

	// Print startup information
	log.Printf("Starting Canopy daemon")
	log.Printf("  HTTP server: http://localhost:%d", port)
	log.Printf("  IPC socket: %s", ipcSocket)
	if dev {
		log.Printf("  Mode: development (serving from disk)")
	}

	// Start daemon (blocks until signal received)
	if err := d.Start(); err != nil {
		return fmt.Errorf("daemon error: %w", err)
	}

	log.Println("Daemon shutdown complete")
	return nil
}
