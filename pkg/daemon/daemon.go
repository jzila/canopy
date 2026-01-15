package daemon

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/jzila/canopy/pkg/events"
	"github.com/jzila/canopy/pkg/logging"
	"github.com/jzila/canopy/pkg/persistence"
	"github.com/jzila/canopy/pkg/repository"
)

// Config holds configuration for the daemon
type Config struct {
	Port              int    // HTTP server port
	SocketPath        string // Unix socket path for IPC
	EnablePersistence bool   // Enable SQLite persistence for run history
}

// IPCServer defines the interface for IPC server operations
type IPCServer interface {
	Start() error
	Stop() error
}

// IPCServerFactory creates an IPC server given an EventBus
type IPCServerFactory func(socketPath string, eventBus *events.EventBus) IPCServer

// BeadsClientFactory creates a BeadsClientInterface for a given repository path.
// This allows lazy creation of beads clients for different repositories.
type BeadsClientFactory func(repoPath string) (BeadsClientInterface, error)

// Daemon orchestrates all daemon components: IPC server, HTTP server, EventBus, and State
type Daemon struct {
	config           Config
	ipcServer        IPCServer
	ipcServerFactory IPCServerFactory
	httpServer       *Server
	eventBus         *EventBus
	state            *RuntimeState
	scheduler        SchedulerInterface
	beadsClient      BeadsClientInterface // Default beads client (for CWD)

	// Repository management
	repoMu            sync.RWMutex
	activeRepoID      string                              // Currently active repository ID
	beadsClients      map[string]BeadsClientInterface     // repoID -> client (lazy loaded)
	beadsClientFactory BeadsClientFactory                 // Factory for creating new beads clients

	// Persistence components (optional, controlled by EnablePersistence config)
	persistenceStore       *persistence.Store
	persistenceHandler     *PersistenceHandler
	persistenceUnsubscribe func()
}

// NewDaemon creates a new daemon instance with the given configuration
// ipcServerFactory is a function that creates an IPC server, typically: ipc.NewServer
func NewDaemon(config Config, ipcServerFactory IPCServerFactory, scheduler SchedulerInterface, beadsClient BeadsClientInterface) *Daemon {
	return &Daemon{
		config:           config,
		ipcServerFactory: ipcServerFactory,
		scheduler:        scheduler,
		beadsClient:      beadsClient,
		beadsClients:     make(map[string]BeadsClientInterface),
	}
}

// SetBeadsClientFactory sets the factory function for creating beads clients.
// This allows lazy creation of beads clients for different repositories.
func (d *Daemon) SetBeadsClientFactory(factory BeadsClientFactory) {
	d.repoMu.Lock()
	defer d.repoMu.Unlock()
	d.beadsClientFactory = factory
}

// SetActiveRepository sets the currently active repository by ID.
// Returns an error if the repository ID is not found in the registry.
// This also reloads tasks from the new repository's beads database.
func (d *Daemon) SetActiveRepository(repoID string) error {
	// Verify the repository exists
	repo, err := repository.FromID(repoID)
	if err != nil {
		return fmt.Errorf("failed to lookup repository: %w", err)
	}
	if repo == nil {
		return fmt.Errorf("repository not found: %s", repoID)
	}

	d.repoMu.Lock()
	d.activeRepoID = repoID
	d.repoMu.Unlock()

	logging.Info("active repository set", "repo_name", repo.Name, "repo_id", repoID)

	// Reload tasks from the new repository's beads database
	if d.state != nil {
		// Clear existing tasks and load tasks from the new repository
		d.state.ClearTasksForRepo("")
		if err := d.loadTasksFromBeads(); err != nil {
			logging.Warn("failed to load tasks for repository", "repo_id", repoID, "error", err)
		}
	}

	return nil
}

// GetActiveRepository returns the currently active repository.
// Returns nil if no repository is active.
func (d *Daemon) GetActiveRepository() *repository.Repository {
	d.repoMu.RLock()
	repoID := d.activeRepoID
	d.repoMu.RUnlock()

	if repoID == "" {
		return nil
	}

	repo, err := repository.FromID(repoID)
	if err != nil {
		logging.Warn("failed to lookup active repository", "repo_id", repoID, "error", err)
		return nil
	}
	return repo
}

// GetActiveRepositoryID returns the ID of the currently active repository.
// Returns empty string if no repository is active.
func (d *Daemon) GetActiveRepositoryID() string {
	d.repoMu.RLock()
	defer d.repoMu.RUnlock()
	return d.activeRepoID
}

// ListRepositories returns all registered repositories.
func (d *Daemon) ListRepositories() ([]repository.Repository, error) {
	return repository.List()
}

// getBeadsClient returns a beads client for the specified repository ID.
// If no client exists for that repo, it creates one lazily using the factory.
// Returns the default beads client if repoID is empty.
func (d *Daemon) getBeadsClient(repoID string) (BeadsClientInterface, error) {
	// Return default client if no repo specified
	if repoID == "" {
		return d.beadsClient, nil
	}

	// Check if we already have a client for this repo
	d.repoMu.RLock()
	client, exists := d.beadsClients[repoID]
	d.repoMu.RUnlock()

	if exists {
		return client, nil
	}

	// Need to create a new client - lookup repo path
	repo, err := repository.FromID(repoID)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup repository: %w", err)
	}
	if repo == nil {
		return nil, fmt.Errorf("repository not found: %s", repoID)
	}

	// Create client using factory
	d.repoMu.Lock()
	defer d.repoMu.Unlock()

	// Double-check after acquiring write lock
	if client, exists := d.beadsClients[repoID]; exists {
		return client, nil
	}

	// No factory available - return nil (no beads support for this repo)
	if d.beadsClientFactory == nil {
		logging.Warn("no beads client factory configured", "repo_id", repoID)
		return nil, nil
	}

	client, err = d.beadsClientFactory(repo.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to create beads client for repo %s: %w", repoID, err)
	}

	d.beadsClients[repoID] = client
	logging.Info("created beads client for repository", "repo_name", repo.Name, "repo_id", repoID)
	return client, nil
}

// getActiveBeadsClient returns the beads client for the currently active repository.
// Falls back to the default beads client if no repository is active.
func (d *Daemon) getActiveBeadsClient() (BeadsClientInterface, error) {
	d.repoMu.RLock()
	repoID := d.activeRepoID
	d.repoMu.RUnlock()

	if repoID == "" {
		return d.beadsClient, nil
	}

	return d.getBeadsClient(repoID)
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
			logging.Warn("failed to initialize persistence store", "error", err)
		} else {
			d.persistenceStore = store
			d.persistenceHandler = NewPersistenceHandler(store, d.eventBus)
			d.persistenceUnsubscribe = d.persistenceHandler.Start()
			logging.Info("persistence enabled - run history will be saved to SQLite")

			// Restore state from database if there was a running run
			if err := d.restoreStateFromDB(); err != nil {
				logging.Warn("failed to restore state from database", "error", err)
			}
		}
	}

	// Load pending tasks from beads database to populate the UI
	if err := d.loadTasksFromBeads(); err != nil {
		logging.Warn("failed to load tasks from beads", "error", err)
	}
}

// restoreStateFromDB loads the most recent run and its agents from the database
// into the RuntimeState. This allows the daemon to display historical data
// after a restart.
//
// On startup, any runs or agents that were still "running" when the daemon
// terminated are marked as failed, since they cannot be resumed.
//
// The active repository is set based on the most recent run's repo context,
// which enables proper beads client selection for task loading.
func (d *Daemon) restoreStateFromDB() error {
	if d.persistenceStore == nil {
		logging.Debug("state restoration skipped - persistence store is nil")
		return nil
	}

	logging.Debug("beginning database state restoration")

	// First, mark any orphaned runs/agents as failed.
	// These are runs/agents that were still "running" when the daemon crashed or was killed.
	// They cannot be resumed, so we mark them as failed to maintain data integrity.
	if err := d.markOrphanedStatesAsFailed(); err != nil {
		return fmt.Errorf("failed to mark orphaned states: %w", err)
	}

	// Get the most recent run regardless of status to set the active repository
	run, err := d.persistenceStore.GetMostRecentRun()
	if err != nil {
		return fmt.Errorf("failed to get most recent run: %w", err)
	}
	if run != nil {
		logging.Debug("most recent run found",
			"run_id", run.ID,
			"status", run.Status,
			"started_at", run.StartedAt.Format("2006-01-02 15:04:05"))

		// Set the active repository based on the most recent run's repo context
		// This allows loadTasksFromBeads to use the correct beads client
		if run.RepoID != "" {
			d.repoMu.Lock()
			d.activeRepoID = run.RepoID
			d.repoMu.Unlock()
			logging.Debug("set active repository from run", "repo_name", run.RepoName, "repo_id", run.RepoID)
		}

		// Update RuntimeState start time to match the most recent run
		d.state.StartTime = run.StartedAt
	} else {
		logging.Debug("no previous runs found in database")
	}

	// Load ALL non-archived agents across all runs (not just the most recent run)
	logging.Debug("querying non-archived agents from database")
	agents, err := d.persistenceStore.GetAllNonArchivedAgents()
	if err != nil {
		return fmt.Errorf("failed to get non-archived agents: %w", err)
	}

	if len(agents) == 0 {
		logging.Debug("no agents found in database, starting fresh")
		return nil
	}

	logging.Debug("found agents to restore", "count", len(agents))

	// Convert persistence.Agent to daemon.AgentState and add to RuntimeState
	for _, pAgent := range agents {
		agentState := d.convertPersistenceAgentToState(&pAgent)
		d.state.AddAgent(agentState)
		logging.Debug("restored agent",
			"agent_id", pAgent.ID,
			"task_id", pAgent.TaskID,
			"status", pAgent.Status,
			"run_id", pAgent.RunID)
	}

	// Update stats after restoring all agents
	d.state.UpdateStats()

	logging.Info("restored agents from database", "count", len(agents))
	return nil
}

// markOrphanedStatesAsFailed marks any orphaned runs and agents as failed.
// This handles the case where the daemon was terminated (crash, kill, etc.)
// while a run was in progress. Since those runs cannot be resumed, we mark
// them as failed to maintain data integrity.
func (d *Daemon) markOrphanedStatesAsFailed() error {
	// Mark orphaned agents first (agents in "starting" or "running" state)
	agentCount, err := d.persistenceStore.MarkOrphanedAgentsFailed()
	if err != nil {
		return fmt.Errorf("failed to mark orphaned agents: %w", err)
	}
	if agentCount > 0 {
		logging.Warn("marked orphaned agents as failed (daemon terminated unexpectedly)", "count", agentCount)
	}

	// Mark orphaned runs (runs in "running" state)
	runCount, err := d.persistenceStore.MarkOrphanedRunsFailed()
	if err != nil {
		return fmt.Errorf("failed to mark orphaned runs: %w", err)
	}
	if runCount > 0 {
		logging.Warn("marked orphaned runs as failed (daemon terminated unexpectedly)", "count", runCount)
	}

	return nil
}

// loadTasksFromBeads loads pending tasks from the beads database into RuntimeState.
// This populates the UI with available work when the daemon starts.
// Uses the active repository's beads client if one is set, otherwise falls back
// to the default beads client.
func (d *Daemon) loadTasksFromBeads() error {
	// Get the active beads client (repo-specific or default)
	client, err := d.getActiveBeadsClient()
	if err != nil {
		return fmt.Errorf("failed to get beads client: %w", err)
	}
	if client == nil {
		return nil
	}

	// Get active repo ID for tagging tasks
	repoID := d.GetActiveRepositoryID()

	tasks, err := client.List()
	if err != nil {
		return fmt.Errorf("failed to list tasks from beads: %w", err)
	}

	if len(tasks) == 0 {
		logging.Debug("no pending tasks found in beads database")
		return nil
	}

	// Add each task to the RuntimeState with repo context
	for i := range tasks {
		d.state.AddTaskWithRepo(&tasks[i], repoID)
	}

	logging.Info("loaded pending tasks from beads database", "count", len(tasks))
	return nil
}

// convertPersistenceAgentToState converts a persistence.Agent to a daemon.AgentState
func (d *Daemon) convertPersistenceAgentToState(pAgent *persistence.Agent) *AgentState {
	agent := &AgentState{
		ID:        pAgent.ID,
		RunID:     pAgent.RunID,
		TaskID:    pAgent.TaskID,
		TaskTitle: pAgent.TaskTitle,
		RepoID:    pAgent.RepoID,
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

	// Generate synthetic live feed events from historical data
	// This allows the UI to display something meaningful for historical agents
	agent.LiveFeedEvents = d.generateHistoricalLiveFeedEvents(pAgent)

	return agent
}

// generateHistoricalLiveFeedEvents creates synthetic live feed events from persisted agent data.
// Since live feed events aren't persisted to the database, we reconstruct meaningful events
// from the available data (result message, error message, completion status) to provide
// visibility into historical agent executions.
func (d *Daemon) generateHistoricalLiveFeedEvents(pAgent *persistence.Agent) []LiveFeedEvent {
	events := []LiveFeedEvent{}

	// Add a "historical" marker event so the UI knows these are reconstructed
	events = append(events, LiveFeedEvent{
		EventType: "text",
		Data: map[string]interface{}{
			"text":        "[Historical session - live feed events were not recorded]",
			"is_historic": true,
		},
	})

	// If we have a result message, add it as a text event
	if pAgent.ResultMessage != "" {
		events = append(events, LiveFeedEvent{
			EventType: "text",
			Data: map[string]interface{}{
				"text":        pAgent.ResultMessage,
				"is_historic": true,
			},
		})
	}

	// Add an agent_completed event with available metrics
	completionData := map[string]interface{}{
		"files_changed":   pAgent.FilesChanged,
		"commits_created": pAgent.GitCommitsCreated,
		"is_historic":     true,
	}

	if pAgent.ErrorMessage != "" {
		completionData["error"] = pAgent.ErrorMessage
	}

	if pAgent.ResultMessage != "" {
		completionData["result_message"] = pAgent.ResultMessage
	}

	events = append(events, LiveFeedEvent{
		EventType: "agent_completed",
		Data:      completionData,
	})

	return events
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
	logging.Info("starting canopy daemon")

	// Clean up any stale pidfile from previous crash
	if cleaned, err := CleanStalePidFile(); err != nil {
		logging.Warn("failed to check stale pidfile", "error", err)
	} else if cleaned {
		logging.Info("cleaned up stale pidfile from previous crash")
	}

	// Write pidfile
	if err := WritePidFile(); err != nil {
		return fmt.Errorf("failed to write pidfile: %w", err)
	}
	logging.Debug("pidfile written")

	// Initialize components if not already done (allows pre-initialization via Init())
	d.Init()
	logging.Debug("eventbus initialized")
	logging.Debug("runtime state initialized")

	// Subscribe RuntimeState to EventBus to update from IPC events
	unsubscribeState := d.state.SubscribeToEventBus(d.eventBus)
	defer unsubscribeState()
	logging.Debug("runtime state subscribed to eventbus")

	// Initialize HTTP server (REST API + WebSocket)
	// Pass persistence store if available (for /api/runs endpoints)
	d.httpServer = NewServerWithDaemon(d.config.Port, d.state, d.eventBus, d.scheduler, d.beadsClient, d.persistenceStore, d)

	// Create IPC server (receives events from canopy run)
	// The factory function creates an ipc.Server with the EventBus we just initialized
	d.ipcServer = d.ipcServerFactory(d.config.SocketPath, d.eventBus)

	// Start IPC server
	if err := d.ipcServer.Start(); err != nil {
		return fmt.Errorf("failed to start IPC server: %w", err)
	}
	logging.Info("IPC server listening", "socket", d.config.SocketPath)

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
		logging.Error("HTTP server error", "error", err)
		// Stop IPC server before returning
		d.ipcServer.Stop()
		return err
	case sig := <-sigChan:
		logging.Info("received shutdown signal", "signal", sig)
		return d.Stop()
	}
}

// Stop gracefully shuts down all daemon components
func (d *Daemon) Stop() error {
	logging.Info("stopping canopy daemon")

	// Remove pidfile first (before any other cleanup that might fail)
	if err := RemovePidFile(); err != nil {
		logging.Warn("failed to remove pidfile", "error", err)
	} else {
		logging.Debug("pidfile removed")
	}

	var firstErr error

	// Stop IPC server (stops accepting new connections)
	if d.ipcServer != nil {
		logging.Debug("stopping IPC server")
		if err := d.ipcServer.Stop(); err != nil {
			logging.Error("IPC server stop error", "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	// Stop HTTP server (closes WebSocket connections)
	if d.httpServer != nil {
		logging.Debug("stopping HTTP server")
		if err := d.httpServer.Stop(); err != nil {
			logging.Error("HTTP server stop error", "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	// Stop persistence handler and close store
	if d.persistenceUnsubscribe != nil {
		logging.Debug("stopping persistence handler")
		d.persistenceUnsubscribe()
	}
	if d.persistenceStore != nil {
		logging.Debug("closing persistence store")
		if err := d.persistenceStore.Close(); err != nil {
			logging.Error("persistence store close error", "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	logging.Info("daemon stopped")
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
