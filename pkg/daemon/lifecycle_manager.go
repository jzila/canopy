package daemon

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jzila/canopy/pkg/events"
	"github.com/jzila/canopy/pkg/logging"
)

// LifecycleManager handles daemon lifecycle including signal handling,
// PID file management, and coordinated start/stop of components.
type LifecycleManager struct {
	ipcServer    IPCServer
	httpServer   *Server
	sigChan      chan os.Signal
	shutdownChan chan struct{}
}

// NewLifecycleManager creates a new LifecycleManager.
func NewLifecycleManager() *LifecycleManager {
	return &LifecycleManager{
		shutdownChan: make(chan struct{}),
	}
}

// SetIPCServer sets the IPC server to manage.
func (m *LifecycleManager) SetIPCServer(server IPCServer) {
	m.ipcServer = server
}

// SetHTTPServer sets the HTTP server to manage.
func (m *LifecycleManager) SetHTTPServer(server *Server) {
	m.httpServer = server
}

// SetupSignalHandling sets up signal handlers for graceful shutdown.
// Returns a channel that receives when a shutdown signal is received.
func (m *LifecycleManager) SetupSignalHandling() <-chan os.Signal {
	m.sigChan = make(chan os.Signal, 1)
	signal.Notify(m.sigChan, os.Interrupt, syscall.SIGTERM)
	return m.sigChan
}

// CleanupStalePidFile removes any stale PID file from a previous crash.
func (m *LifecycleManager) CleanupStalePidFile() error {
	cleaned, err := CleanStalePidFile()
	if err != nil {
		logging.Warn("failed to check stale pidfile", "error", err)
		return err
	}
	if cleaned {
		logging.Info("cleaned up stale pidfile from previous crash")
	}
	return nil
}

// WritePidFile writes the current process PID to the pidfile.
func (m *LifecycleManager) WritePidFile() error {
	if err := WritePidFile(); err != nil {
		return fmt.Errorf("failed to write pidfile: %w", err)
	}
	logging.Debug("pidfile written")
	return nil
}

// RemovePidFile removes the pidfile.
func (m *LifecycleManager) RemovePidFile() {
	if err := RemovePidFile(); err != nil {
		logging.Warn("failed to remove pidfile", "error", err)
	} else {
		logging.Debug("pidfile removed")
	}
}

// StartIPC starts the IPC server.
func (m *LifecycleManager) StartIPC() error {
	if m.ipcServer == nil {
		return nil
	}

	if err := m.ipcServer.Start(); err != nil {
		return fmt.Errorf("failed to start IPC server: %w", err)
	}
	return nil
}

// StartHTTP starts the HTTP server in a goroutine and returns an error channel.
func (m *LifecycleManager) StartHTTP() <-chan error {
	errChan := make(chan error, 1)
	if m.httpServer == nil {
		return errChan
	}

	go func() {
		if err := m.httpServer.Start(); err != nil {
			errChan <- err
		}
	}()

	return errChan
}

// StopIPC stops the IPC server.
func (m *LifecycleManager) StopIPC() error {
	if m.ipcServer == nil {
		return nil
	}

	logging.Debug("stopping IPC server")
	if err := m.ipcServer.Stop(); err != nil {
		logging.Error("IPC server stop error", "error", err)
		return err
	}
	return nil
}

// StopHTTP stops the HTTP server.
func (m *LifecycleManager) StopHTTP() error {
	if m.httpServer == nil {
		return nil
	}

	logging.Debug("stopping HTTP server")
	if err := m.httpServer.Stop(); err != nil {
		logging.Error("HTTP server stop error", "error", err)
		return err
	}
	return nil
}

// Stop performs graceful shutdown of all managed components.
// Returns the first error encountered, but attempts to stop all components.
func (m *LifecycleManager) Stop() error {
	logging.Info("stopping managed components")

	// Remove pidfile first (before any other cleanup that might fail)
	m.RemovePidFile()

	var firstErr error

	// Stop IPC server
	if err := m.StopIPC(); err != nil && firstErr == nil {
		firstErr = err
	}

	// Stop HTTP server
	if err := m.StopHTTP(); err != nil && firstErr == nil {
		firstErr = err
	}

	return firstErr
}

// Shutdown signals that shutdown is requested.
func (m *LifecycleManager) Shutdown() {
	close(m.shutdownChan)
}

// ShutdownRequested returns a channel that is closed when shutdown is requested.
func (m *LifecycleManager) ShutdownRequested() <-chan struct{} {
	return m.shutdownChan
}

// ComponentInitializer provides methods for initializing daemon components.
type ComponentInitializer struct {
	config           Config
	ipcServerFactory IPCServerFactory
	scheduler        SchedulerInterface
	repoManager      *RepositoryManager
	persistManager   *PersistenceManager
}

// NewComponentInitializer creates a new ComponentInitializer.
func NewComponentInitializer(
	config Config,
	ipcServerFactory IPCServerFactory,
	scheduler SchedulerInterface,
	repoManager *RepositoryManager,
	persistManager *PersistenceManager,
) *ComponentInitializer {
	return &ComponentInitializer{
		config:           config,
		ipcServerFactory: ipcServerFactory,
		scheduler:        scheduler,
		repoManager:      repoManager,
		persistManager:   persistManager,
	}
}

// CreateEventBus creates a new EventBus.
func (c *ComponentInitializer) CreateEventBus() *EventBus {
	return events.NewEventBus()
}

// CreateRuntimeState creates a new RuntimeState.
func (c *ComponentInitializer) CreateRuntimeState() *RuntimeState {
	return NewRuntimeState()
}

// CreateIPCServer creates an IPC server using the factory.
func (c *ComponentInitializer) CreateIPCServer(eventBus *EventBus) IPCServer {
	if c.ipcServerFactory == nil {
		return nil
	}
	return c.ipcServerFactory(c.config.SocketPath, eventBus)
}

// CreateHTTPServer creates an HTTP server with all dependencies wired up.
func (c *ComponentInitializer) CreateHTTPServer(
	state *RuntimeState,
	eventBus *EventBus,
	daemon *Daemon,
) *Server {
	var persistStore PersistenceStoreInterface
	if c.persistManager != nil && c.persistManager.IsEnabled() {
		persistStore = c.persistManager.GetStore()
	}

	return NewServerWithDaemon(
		c.config.Port,
		state,
		eventBus,
		c.scheduler,
		c.repoManager.GetDefaultClient(),
		persistStore,
		daemon,
	)
}
