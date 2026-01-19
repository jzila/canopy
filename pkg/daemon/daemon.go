package daemon

import (
	"context"
	"fmt"

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

// Daemon orchestrates all daemon components through focused managers.
// It coordinates:
//   - RepositoryManager: repository and beads client management
//   - PersistenceManager: persistence layer coordination
//   - LifecycleManager: signal handling, PID files, start/stop
//   - RuntimeState: agent and task state tracking
//   - EventBus: event distribution
type Daemon struct {
	config Config

	// Core components
	eventBus *EventBus
	state    *RuntimeState

	// Managers (single responsibility)
	repoManager    *RepositoryManager
	persistManager *PersistenceManager
	lifecycle      *LifecycleManager

	// Server components (managed by lifecycle)
	ipcServer        IPCServer
	ipcServerFactory IPCServerFactory
	httpServer       *Server
	scheduler        SchedulerInterface
}

// NewDaemon creates a new daemon instance with the given configuration.
// ipcServerFactory is a function that creates an IPC server, typically: ipc.NewServer
func NewDaemon(config Config, ipcServerFactory IPCServerFactory, scheduler SchedulerInterface, beadsClient BeadsClientInterface) *Daemon {
	return &Daemon{
		config:           config,
		ipcServerFactory: ipcServerFactory,
		scheduler:        scheduler,
		repoManager:      NewRepositoryManager(beadsClient),
		persistManager:   NewPersistenceManager(config.EnablePersistence),
		lifecycle:        NewLifecycleManager(),
	}
}

// SetBeadsClientFactory sets the factory function for creating beads clients.
// This allows lazy creation of beads clients for different repositories.
func (d *Daemon) SetBeadsClientFactory(factory BeadsClientFactory) {
	d.repoManager.SetClientFactory(factory)
}

// SetActiveRepository sets the currently active repository by ID.
// Returns an error if the repository ID is not found in the registry.
// This also reloads tasks from the new repository's beads database.
func (d *Daemon) SetActiveRepository(repoID string) error {
	// Get the old repo ID BEFORE switching, so we know if we're actually changing repos
	var oldRepoID string
	if d.repoManager != nil {
		oldRepoID = d.repoManager.GetActiveRepositoryID()
	}

	if err := d.repoManager.SetActiveRepository(repoID); err != nil {
		return err
	}

	// Reload tasks from the new repository's beads database
	if d.state != nil {
		// Only clear old repo's tasks when ACTUALLY switching repos (not re-activating the same one)
		if oldRepoID != "" && oldRepoID != repoID {
			d.state.ClearTasksForRepo(oldRepoID)
		}
		if err := d.loadTasksFromBeads(); err != nil {
			logging.Warn("failed to load tasks for repository", "repo_id", repoID, "error", err)
		}
	}

	return nil
}

// GetActiveRepository returns the currently active repository.
// Returns nil if no repository is active.
func (d *Daemon) GetActiveRepository() *repository.Repository {
	return d.repoManager.GetActiveRepository()
}

// GetActiveRepositoryID returns the ID of the currently active repository.
// Returns empty string if no repository is active.
func (d *Daemon) GetActiveRepositoryID() string {
	return d.repoManager.GetActiveRepositoryID()
}

// ListRepositories returns all registered repositories.
func (d *Daemon) ListRepositories() ([]repository.Repository, error) {
	return d.repoManager.ListRepositories()
}

// getBeadsClient returns a beads client for the specified repository ID.
// If no client exists for that repo, it creates one lazily using the factory.
// Returns the default beads client if repoID is empty.
func (d *Daemon) getBeadsClient(repoID string) (BeadsClientInterface, error) {
	return d.repoManager.GetClient(repoID)
}

// getActiveBeadsClient returns the beads client for the currently active repository.
// Falls back to the default beads client if no repository is active.
func (d *Daemon) getActiveBeadsClient() (BeadsClientInterface, error) {
	return d.repoManager.GetActiveClient()
}

// Init initializes daemon components without starting servers.
// Call this before Start() if you need access to EventBus or RuntimeState.
func (d *Daemon) Init() {
	if d.eventBus == nil {
		d.eventBus = NewEventBus()
	}
	if d.state == nil {
		d.state = NewRuntimeState()
	}

	// Initialize managers if nil (for backwards compatibility with tests)
	if d.repoManager == nil {
		d.repoManager = NewRepositoryManager(nil)
	}
	if d.persistManager == nil {
		d.persistManager = NewPersistenceManager(d.config.EnablePersistence)
	}
	if d.lifecycle == nil {
		d.lifecycle = NewLifecycleManager()
	}

	// Initialize persistence if enabled
	if d.config.EnablePersistence && d.persistManager != nil {
		if err := d.persistManager.Initialize(d.eventBus); err != nil {
			logging.Warn("failed to initialize persistence", "error", err)
		} else if d.persistManager.IsEnabled() {
			// Restore state from database
			if err := d.restoreStateFromDB(); err != nil {
				logging.Warn("failed to restore state from database", "error", err)
			}
		}
	}

	// Load pending tasks from beads database to populate the UI
	if d.repoManager != nil {
		if err := d.loadTasksFromBeads(); err != nil {
			logging.Warn("failed to load tasks from beads", "error", err)
		}
	}
}

// restoreStateFromDB loads state from the database into RuntimeState.
func (d *Daemon) restoreStateFromDB() error {
	restored, err := d.persistManager.RestoreState()
	if err != nil {
		return err
	}

	if restored == nil {
		return nil
	}

	// Set active repository from the most recent run
	if restored.Run != nil && restored.Run.RepoID != "" {
		d.repoManager.SetActiveRepositoryDirect(restored.Run.RepoID)
		logging.Debug("set active repository from run",
			"repo_name", restored.Run.RepoName,
			"repo_id", restored.Run.RepoID)
	}

	// Apply restored state to RuntimeState
	ApplyRestoredState(d.state, restored)

	return nil
}

// loadTasksFromBeads loads pending tasks from the beads database into RuntimeState
// and persists them to the SQLite tasks table for the dashboard.
func (d *Daemon) loadTasksFromBeads() error {
	client, err := d.getActiveBeadsClient()
	if err != nil {
		return fmt.Errorf("failed to get beads client: %w", err)
	}
	if client == nil {
		return nil
	}

	repoID := d.GetActiveRepositoryID()

	tasks, err := client.List(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list tasks from beads: %w", err)
	}

	if len(tasks) == 0 {
		logging.Debug("no pending tasks found in beads database")
		return nil
	}

	// Get persistence store for persisting tasks to SQLite
	store := d.persistManager.GetStore()

	for i := range tasks {
		d.state.AddTaskWithRepo(&tasks[i], repoID)

		// Persist to SQLite so the dashboard can display tasks
		if store != nil {
			pTask := &persistence.Task{
				ID:       tasks[i].ID,
				RepoID:   repoID,
				Title:    tasks[i].Title,
				Status:   tasks[i].Status,
				Priority: tasks[i].Priority,
			}
			if err := store.UpsertTask(pTask); err != nil {
				logging.Warn("failed to persist task to database", "task_id", tasks[i].ID, "error", err)
			}
		}
	}

	logging.Info("loaded pending tasks from beads database", "count", len(tasks))

	// Reconcile closed beads - update tasks in runs.db that show as failed
	// but have been closed in beads
	if err := d.reconcileClosedBeads(); err != nil {
		logging.Warn("failed to reconcile closed beads", "error", err)
	}

	return nil
}

// reconcileClosedBeads updates task status in runs.db for tasks that are marked
// as "failed" but have been closed in beads. This handles the case where a task
// fails, is later manually closed in beads, and the daemon restarts - without
// this reconciliation, the dashboard would show stale "failed" status.
func (d *Daemon) reconcileClosedBeads() error {
	client, err := d.getActiveBeadsClient()
	if err != nil {
		return fmt.Errorf("failed to get beads client: %w", err)
	}
	if client == nil {
		return nil
	}

	store := d.persistManager.GetStore()
	if store == nil {
		return nil
	}

	// Get tasks with "failed" status from runs.db
	failedTasks, err := store.GetTasksByStatus("failed")
	if err != nil {
		return fmt.Errorf("failed to get failed tasks: %w", err)
	}

	if len(failedTasks) == 0 {
		return nil
	}

	ctx := context.Background()
	var reconciled int

	for _, task := range failedTasks {
		// Check current status in beads
		beadTask, err := client.Show(ctx, task.ID)
		if err != nil {
			// Task might not exist in beads anymore - log and continue
			logging.Debug("failed to get task from beads, skipping", "task_id", task.ID, "error", err)
			continue
		}

		// If the task is closed in beads, update runs.db to show "completed"
		if beadTask.Status == "closed" {
			task.Status = "completed"
			if err := store.UpsertTask(&task); err != nil {
				logging.Warn("failed to update reconciled task", "task_id", task.ID, "error", err)
				continue
			}

			// Also update RuntimeState if the task exists there
			d.state.UpdateTaskStatus(task.ID, "completed", "")
			reconciled++
		}
	}

	if reconciled > 0 {
		logging.Info("reconciled closed beads with runs.db", "count", reconciled)
	}

	return nil
}

// Start initializes and starts all daemon components.
// Blocks until a termination signal is received.
func (d *Daemon) Start() error {
	logging.Info("starting canopy daemon")

	// Clean up any stale pidfile from previous crash
	d.lifecycle.CleanupStalePidFile()

	// Write pidfile
	if err := d.lifecycle.WritePidFile(); err != nil {
		return err
	}

	// Initialize components if not already done
	d.Init()
	logging.Debug("eventbus initialized")
	logging.Debug("runtime state initialized")

	// Subscribe RuntimeState to EventBus to update from IPC events
	unsubscribeState := d.state.SubscribeToEventBus(d.eventBus)
	defer unsubscribeState()
	logging.Debug("runtime state subscribed to eventbus")

	// Initialize HTTP server
	d.httpServer = NewServerWithDaemon(
		d.config.Port,
		d.state,
		d.eventBus,
		d.scheduler,
		d.repoManager.GetDefaultClient(),
		d.persistManager.GetStore(),
		d,
	)
	d.lifecycle.SetHTTPServer(d.httpServer)

	// Create IPC server
	d.ipcServer = d.ipcServerFactory(d.config.SocketPath, d.eventBus)
	d.lifecycle.SetIPCServer(d.ipcServer)

	// Start IPC server
	if err := d.lifecycle.StartIPC(); err != nil {
		return err
	}
	logging.Info("IPC server listening", "socket", d.config.SocketPath)

	// Start HTTP server in a goroutine
	httpErrChan := d.lifecycle.StartHTTP()

	// Wait for termination signal or HTTP server error
	sigChan := d.lifecycle.SetupSignalHandling()

	select {
	case err := <-httpErrChan:
		logging.Error("HTTP server error", "error", err)
		d.lifecycle.StopIPC()
		return err
	case sig := <-sigChan:
		logging.Info("received shutdown signal", "signal", sig)
		return d.Stop()
	}
}

// Stop gracefully shuts down all daemon components.
func (d *Daemon) Stop() error {
	logging.Info("stopping canopy daemon")

	var firstErr error

	// Stop lifecycle-managed components (IPC, HTTP, pidfile)
	if err := d.lifecycle.Stop(); err != nil && firstErr == nil {
		firstErr = err
	}

	// Stop persistence
	if err := d.persistManager.Close(); err != nil {
		logging.Error("persistence close error", "error", err)
		if firstErr == nil {
			firstErr = err
		}
	}

	logging.Info("daemon stopped")
	return firstErr
}

// BroadcastEvent publishes an event to the EventBus.
// This allows external components to inject events into the system.
func (d *Daemon) BroadcastEvent(event Event) {
	if d.eventBus != nil {
		d.eventBus.Publish(event)
	}
}

// GetEventBus returns the EventBus instance.
// Returns nil if Start() has not been called yet.
func (d *Daemon) GetEventBus() *EventBus {
	return d.eventBus
}

// GetState returns a snapshot of the current runtime state.
func (d *Daemon) GetState() RuntimeStateSnapshot {
	if d.state != nil {
		return d.state.GetSnapshot()
	}
	return RuntimeStateSnapshot{}
}

// GetRuntimeState returns the RuntimeState instance for direct access.
// This is useful for components that need to subscribe to state changes.
// Returns nil if Start() has not been called yet.
func (d *Daemon) GetRuntimeState() *RuntimeState {
	return d.state
}

// GetPersistenceStore returns the persistence store for external access.
// Returns nil if persistence is disabled.
func (d *Daemon) GetPersistenceStore() *PersistenceManager {
	return d.persistManager
}

// newDaemonForTest creates a daemon instance for testing with pre-configured components.
// This is used internally by tests to bypass normal initialization.
func newDaemonForTest(config Config, store *persistence.Store, beadsClient BeadsClientInterface) *Daemon {
	d := &Daemon{
		config:         config,
		eventBus:       NewEventBus(),
		state:          NewRuntimeState(),
		repoManager:    NewRepositoryManager(beadsClient),
		persistManager: NewPersistenceManager(config.EnablePersistence),
		lifecycle:      NewLifecycleManager(),
	}

	// Wire up the store if provided
	if store != nil {
		d.persistManager.store = store
	}

	return d
}
