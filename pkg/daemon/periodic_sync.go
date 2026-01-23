package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jzila/canopy/pkg/events"
	"github.com/jzila/canopy/pkg/logging"
	"github.com/jzila/canopy/pkg/persistence"
)

// PeriodicSyncConfig holds configuration for the periodic sync manager.
type PeriodicSyncConfig struct {
	// Interval between sync operations. Default: 30 seconds.
	Interval time.Duration

	// Enabled controls whether periodic sync is active. Default: true.
	Enabled bool

	// SyncAllRepos controls whether to sync all registered repos or just the active one.
	// Default: false (only sync active repo).
	SyncAllRepos bool
}

// DefaultPeriodicSyncConfig returns the default periodic sync configuration.
func DefaultPeriodicSyncConfig() PeriodicSyncConfig {
	return PeriodicSyncConfig{
		Interval:     30 * time.Second,
		Enabled:      true,
		SyncAllRepos: false,
	}
}

// PeriodicSyncManager periodically syncs beads state from the filesystem
// into RuntimeState and broadcasts updates to connected clients.
type PeriodicSyncManager struct {
	config PeriodicSyncConfig

	// Dependencies
	daemon *Daemon // For accessing loadTasksFromBeads

	// Lifecycle
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	running  atomic.Bool
	stopOnce sync.Once
}

// NewPeriodicSyncManager creates a new periodic sync manager.
func NewPeriodicSyncManager(
	config PeriodicSyncConfig,
	daemon *Daemon,
) *PeriodicSyncManager {
	return &PeriodicSyncManager{
		config: config,
		daemon: daemon,
	}
}

// Start begins the periodic sync background goroutine.
// This is non-blocking and returns immediately.
func (m *PeriodicSyncManager) Start() {
	if !m.config.Enabled {
		logging.Debug("periodic sync disabled")
		return
	}

	if m.running.Swap(true) {
		// Already running
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	m.wg.Add(1)
	go m.run(ctx)

	logging.Info("periodic sync started", "interval", m.config.Interval)
}

// Stop stops the periodic sync manager gracefully.
func (m *PeriodicSyncManager) Stop() {
	m.stopOnce.Do(func() {
		if m.cancel != nil {
			m.cancel()
		}
		m.wg.Wait()
		m.running.Store(false)
		logging.Debug("periodic sync stopped")
	})
}

// run is the main loop for periodic sync.
func (m *PeriodicSyncManager) run(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sync(ctx)
		}
	}
}

// sync performs a single sync operation.
func (m *PeriodicSyncManager) sync(ctx context.Context) {
	if m.daemon == nil {
		return
	}

	if m.config.SyncAllRepos {
		m.syncAllRepos(ctx)
	} else {
		m.syncActiveRepo(ctx)
	}
}

// syncActiveRepo syncs only the currently active repository.
func (m *PeriodicSyncManager) syncActiveRepo(ctx context.Context) {
	repoID := m.daemon.GetActiveRepositoryID()
	if repoID == "" {
		logging.Debug("periodic sync: no active repository")
		return
	}

	if err := m.syncRepo(ctx, repoID); err != nil {
		logging.Warn("periodic sync failed for active repo",
			"repo_id", repoID,
			"error", err)
	}
}

// syncAllRepos syncs all registered repositories.
func (m *PeriodicSyncManager) syncAllRepos(ctx context.Context) {
	repos, err := m.daemon.ListRepositories()
	if err != nil {
		logging.Warn("periodic sync: failed to list repositories", "error", err)
		return
	}

	for _, repo := range repos {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := m.syncRepo(ctx, repo.ID); err != nil {
			logging.Warn("periodic sync failed for repo",
				"repo_id", repo.ID,
				"repo_name", repo.Name,
				"error", err)
		}
	}
}

// syncRepo syncs a single repository by ID.
func (m *PeriodicSyncManager) syncRepo(ctx context.Context, repoID string) error {
	client, err := m.daemon.getBeadsClient(repoID)
	if err != nil {
		return err
	}
	if client == nil {
		return nil
	}

	// List tasks from beads
	tasks, err := client.List(ctx)
	if err != nil {
		return err
	}

	state := m.daemon.GetRuntimeState()
	if state == nil {
		return nil
	}

	// Get persistence store
	var store *persistence.Store
	if m.daemon.persistManager != nil {
		store = m.daemon.persistManager.GetStore()
	}

	// Track if any tasks changed
	changed := false
	existingTasks := state.GetTasksForRepo(repoID)
	existingByID := make(map[string]struct{}, len(existingTasks))
	for _, t := range existingTasks {
		existingByID[t.ID] = struct{}{}
	}

	for i := range tasks {
		// Check if task is new or changed
		if _, exists := existingByID[tasks[i].ID]; !exists {
			changed = true
		}

		state.AddTaskWithRepo(&tasks[i], repoID)

		// Persist to SQLite
		if store != nil {
			pTask := &persistence.Task{
				ID:       tasks[i].ID,
				RepoID:   repoID,
				Title:    tasks[i].Title,
				Status:   tasks[i].Status,
				Priority: tasks[i].Priority,
			}
			if err := store.UpsertTask(pTask); err != nil {
				logging.Warn("periodic sync: failed to persist task",
					"task_id", tasks[i].ID,
					"error", err)
			}
		}
	}

	// Broadcast state sync if anything changed
	if changed {
		eventBus := m.daemon.GetEventBus()
		if eventBus != nil {
			snapshot := m.daemon.GetState()
			eventBus.Publish(events.Event{
				Type:      events.EventStateSync,
				Timestamp: time.Now(),
				Payload:   snapshot,
			})
		}
	}

	return nil
}

// IsRunning returns whether the periodic sync manager is currently running.
func (m *PeriodicSyncManager) IsRunning() bool {
	return m.running.Load()
}

// GetConfig returns a copy of the current configuration.
func (m *PeriodicSyncManager) GetConfig() PeriodicSyncConfig {
	return m.config
}
