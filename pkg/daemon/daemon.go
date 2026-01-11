package daemon

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jzila/canopy/pkg/persistence"
)

// Config holds configuration for the daemon
type Config struct {
	Port              int    // HTTP server port
	SocketPath        string // Unix socket path for IPC
	EnablePersistence bool   // Enable SQLite persistence for run history
}

// IPCServer defines the interface for IPC server operations
// This avoids circular dependency with pkg/ipc
type IPCServer interface {
	Start() error
	Stop() error
}

// IPCServerFactory creates an IPC server given an EventBus
// This allows the daemon to create the IPC server after initializing the EventBus
// without directly importing pkg/ipc (which would create a circular dependency)
type IPCServerFactory func(socketPath string, eventBus *EventBus) IPCServer

// Daemon orchestrates all daemon components: IPC server, HTTP server, EventBus, and State
type Daemon struct {
	config           Config
	ipcServer        IPCServer
	ipcServerFactory IPCServerFactory
	httpServer       *Server
	eventBus         *EventBus
	state            *RuntimeState
	scheduler        SchedulerInterface
	beadsClient      BeadsClientInterface

	// Persistence components (optional, controlled by EnablePersistence config)
	persistenceStore           *persistence.Store
	persistenceHandler         *PersistenceHandler
	persistenceUnsubscribe     func()
}

// NewDaemon creates a new daemon instance with the given configuration
// ipcServerFactory is a function that creates an IPC server, typically: ipc.NewServer
func NewDaemon(config Config, ipcServerFactory IPCServerFactory, scheduler SchedulerInterface, beadsClient BeadsClientInterface) *Daemon {
	return &Daemon{
		config:           config,
		ipcServerFactory: ipcServerFactory,
		scheduler:        scheduler,
		beadsClient:      beadsClient,
	}
}

// Init initializes daemon components without starting servers
// Call this before Start() if you need access to EventBus or RuntimeState
func (d *Daemon) Init() {
	if d.eventBus == nil {
		d.eventBus = NewEventBus()
	}
	if d.state == nil {
		d.state = NewRuntimeState()
	}

	// Initialize persistence if enabled
	if d.config.EnablePersistence && d.persistenceStore == nil {
		store, err := persistence.NewStore()
		if err != nil {
			log.Printf("Warning: failed to initialize persistence store: %v", err)
		} else {
			d.persistenceStore = store
			d.persistenceHandler = NewPersistenceHandler(store, d.eventBus)
			d.persistenceUnsubscribe = d.persistenceHandler.Start()
			log.Println("Persistence enabled - run history will be saved to SQLite")
		}
	}
}

// Start initializes and starts all daemon components
// Blocks until a termination signal is received
func (d *Daemon) Start() error {
	log.Println("Starting Canopy daemon...")

	// Initialize components if not already done (allows pre-initialization via Init())
	d.Init()
	log.Println("EventBus initialized")
	log.Println("RuntimeState initialized")

	// Subscribe RuntimeState to EventBus to update from IPC events
	unsubscribeState := d.state.SubscribeToEventBus(d.eventBus)
	defer unsubscribeState()
	log.Println("RuntimeState subscribed to EventBus")

	// Initialize HTTP server (REST API + WebSocket)
	// Pass persistence store if available (for /api/runs endpoints)
	d.httpServer = NewServerWithPersistence(d.config.Port, d.state, d.eventBus, d.scheduler, d.beadsClient, d.persistenceStore)

	// Create IPC server (receives events from canopy run)
	// The factory function creates an ipc.Server with the EventBus we just initialized
	d.ipcServer = d.ipcServerFactory(d.config.SocketPath, d.eventBus)

	// Start IPC server
	if err := d.ipcServer.Start(); err != nil {
		return fmt.Errorf("failed to start IPC server: %w", err)
	}
	log.Printf("IPC server listening on %s", d.config.SocketPath)

	// Start HTTP server in a goroutine (it blocks in ListenAndServe)
	httpErrChan := make(chan error, 1)
	go func() {
		if err := d.httpServer.Start(); err != nil {
			httpErrChan <- err
		}
	}()

	// Wait for termination signal or HTTP server error
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-httpErrChan:
		log.Printf("HTTP server error: %v", err)
		// Stop IPC server before returning
		d.ipcServer.Stop()
		return err
	case sig := <-sigChan:
		log.Printf("Received signal: %v", sig)
		return d.Stop()
	}
}

// Stop gracefully shuts down all daemon components
func (d *Daemon) Stop() error {
	log.Println("Stopping Canopy daemon...")

	var firstErr error

	// Stop IPC server (stops accepting new connections)
	if d.ipcServer != nil {
		log.Println("Stopping IPC server...")
		if err := d.ipcServer.Stop(); err != nil {
			log.Printf("IPC server stop error: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	// Stop HTTP server (closes WebSocket connections)
	if d.httpServer != nil {
		log.Println("Stopping HTTP server...")
		if err := d.httpServer.Stop(); err != nil {
			log.Printf("HTTP server stop error: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	// Stop persistence handler and close store
	if d.persistenceUnsubscribe != nil {
		log.Println("Stopping persistence handler...")
		d.persistenceUnsubscribe()
	}
	if d.persistenceStore != nil {
		log.Println("Closing persistence store...")
		if err := d.persistenceStore.Close(); err != nil {
			log.Printf("Persistence store close error: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	log.Println("Daemon stopped")
	return firstErr
}

// BroadcastEvent publishes an event to the EventBus
// This allows external components to inject events into the system
func (d *Daemon) BroadcastEvent(event Event) {
	if d.eventBus != nil {
		d.eventBus.Publish(event)
	}
}

// GetEventBus returns the EventBus instance
// Returns nil if Start() has not been called yet
func (d *Daemon) GetEventBus() *EventBus {
	return d.eventBus
}

// GetState returns a snapshot of the current runtime state
func (d *Daemon) GetState() RuntimeState {
	if d.state != nil {
		return d.state.GetSnapshot()
	}
	return RuntimeState{}
}

// GetRuntimeState returns the RuntimeState instance for direct access
// This is useful for components that need to subscribe to state changes
// Returns nil if Start() has not been called yet
func (d *Daemon) GetRuntimeState() *RuntimeState {
	return d.state
}
