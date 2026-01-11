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

			// Restore state from database if there was a running run
			if err := d.restoreStateFromDB(); err != nil {
				log.Printf("Warning: failed to restore state from database: %v", err)
			}
		}
	}
}

// restoreStateFromDB loads any running run and its agents from the database
// into the RuntimeState. This allows the daemon to resume displaying state
// after a restart.
func (d *Daemon) restoreStateFromDB() error {
	if d.persistenceStore == nil {
		return nil
	}

	// Get any running run
	run, err := d.persistenceStore.GetRunningRun()
	if err != nil {
		return fmt.Errorf("failed to get running run: %w", err)
	}
	if run == nil {
		log.Println("No running run found in database, starting fresh")
		return nil
	}

	log.Printf("Restoring state from run %s (started %s)", run.ID, run.StartedAt.Format("2006-01-02 15:04:05"))

	// Update RuntimeState start time to match the run
	d.state.StartTime = run.StartedAt

	// Load all agents for this run
	agents, err := d.persistenceStore.GetAgentsByRun(run.ID)
	if err != nil {
		return fmt.Errorf("failed to get agents for run %s: %w", run.ID, err)
	}

	// Convert persistence.Agent to daemon.AgentState and add to RuntimeState
	for _, pAgent := range agents {
		agentState := d.convertPersistenceAgentToState(&pAgent)
		d.state.AddAgent(agentState)
		log.Printf("  Restored agent %s (%s): %s", pAgent.ID, pAgent.TaskID, pAgent.Status)
	}

	// Update stats after restoring all agents
	d.state.UpdateStats()

	log.Printf("Restored %d agents from previous run", len(agents))
	return nil
}

// convertPersistenceAgentToState converts a persistence.Agent to a daemon.AgentState
func (d *Daemon) convertPersistenceAgentToState(pAgent *persistence.Agent) *AgentState {
	agent := &AgentState{
		ID:        pAgent.ID,
		TaskID:    pAgent.TaskID,
		TaskTitle: pAgent.TaskTitle,
		Status:    convertPersistenceStatus(pAgent.Status),
		StartTime: pAgent.StartedAt,
		Duration:  pAgent.DurationSeconds,
		TokenUsage: TokenUsage{
			InputTokens:  pAgent.InputTokens,
			OutputTokens: pAgent.OutputTokens,
			TotalTokens:  pAgent.TotalTokens,
			CostUSD:      pAgent.CostUSD,
		},
		Changes: pAgent.FilesChanged,
		Commits: pAgent.GitCommitsCreated,
		Error:   pAgent.ErrorMessage,
	}

	if pAgent.FinishedAt != nil {
		agent.EndTime = pAgent.FinishedAt
	}

	if pAgent.ExitCode != nil {
		agent.ExitCode = *pAgent.ExitCode
	}

	// Restore stdout/stderr if available
	if pAgent.Stdout != "" || pAgent.Stderr != "" {
		agent.Output.Stdout = pAgent.Stdout
		agent.Output.Stderr = pAgent.Stderr
	}

	return agent
}

// convertPersistenceStatus converts persistence.AgentStatus to daemon.AgentStatus
func convertPersistenceStatus(status persistence.AgentStatus) AgentStatus {
	switch status {
	case persistence.AgentStatusStarting:
		return AgentStatusStarting
	case persistence.AgentStatusRunning:
		return AgentStatusRunning
	case persistence.AgentStatusCompleted:
		return AgentStatusCompleted
	case persistence.AgentStatusFailed:
		return AgentStatusFailed
	case persistence.AgentStatusTimedOut:
		return AgentStatusTimedOut
	case persistence.AgentStatusCancelled:
		return AgentStatusCancelled
	default:
		return AgentStatusRunning
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
