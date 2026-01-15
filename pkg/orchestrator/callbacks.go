package orchestrator

import (
	"sync"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/scheduler"
)

// CallbackManager manages event callbacks with thread-safe registration
// and invocation. It implements scheduler.CallbackHandler.
//
// Callbacks are stored as a slice of EventCallbacks structs, allowing
// both internal (orchestrator) and user callbacks to be registered
// and invoked together.
type CallbackManager struct {
	mu        sync.RWMutex
	callbacks []*EventCallbacks
}

// NewCallbackManager creates a new CallbackManager.
func NewCallbackManager() *CallbackManager {
	return &CallbackManager{
		callbacks: make([]*EventCallbacks, 0, 2), // Typically internal + user
	}
}

// Register adds an EventCallbacks struct to be invoked on events.
// All non-nil callback functions in the struct will be called when
// the corresponding event occurs.
func (m *CallbackManager) Register(callbacks *EventCallbacks) {
	if callbacks == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = append(m.callbacks, callbacks)
}

// OnAgentStart implements scheduler.CallbackHandler.
// Invokes all registered OnAgentStart callbacks in a thread-safe manner.
func (m *CallbackManager) OnAgentStart(taskID string, task *beads.Task) {
	m.mu.RLock()
	// Copy slice to avoid holding lock during invocation
	callbacks := make([]*EventCallbacks, len(m.callbacks))
	copy(callbacks, m.callbacks)
	m.mu.RUnlock()

	for _, cb := range callbacks {
		cb.OnAgentStart(taskID, task)
	}
}

// OnOutput implements scheduler.CallbackHandler.
// Invokes all registered OnOutput callbacks in a thread-safe manner.
func (m *CallbackManager) OnOutput(taskID string, output string, isError bool) {
	m.mu.RLock()
	callbacks := make([]*EventCallbacks, len(m.callbacks))
	copy(callbacks, m.callbacks)
	m.mu.RUnlock()

	for _, cb := range callbacks {
		cb.OnOutput(taskID, output, isError)
	}
}

// OnLiveFeed implements scheduler.CallbackHandler.
// Invokes all registered OnLiveFeed callbacks in a thread-safe manner.
func (m *CallbackManager) OnLiveFeed(taskID string, event *agent.LiveFeedEvent) {
	m.mu.RLock()
	callbacks := make([]*EventCallbacks, len(m.callbacks))
	copy(callbacks, m.callbacks)
	m.mu.RUnlock()

	for _, cb := range callbacks {
		cb.OnLiveFeed(taskID, event)
	}
}

// OnDone implements scheduler.CallbackHandler.
// Invokes all registered OnDone callbacks in a thread-safe manner.
func (m *CallbackManager) OnDone(taskID string, result *agent.Result) {
	m.mu.RLock()
	callbacks := make([]*EventCallbacks, len(m.callbacks))
	copy(callbacks, m.callbacks)
	m.mu.RUnlock()

	for _, cb := range callbacks {
		cb.OnDone(taskID, result)
	}
}

// OnFail implements scheduler.CallbackHandler.
// Invokes all registered OnFail callbacks in a thread-safe manner.
func (m *CallbackManager) OnFail(taskID string, result *agent.Result) {
	m.mu.RLock()
	callbacks := make([]*EventCallbacks, len(m.callbacks))
	copy(callbacks, m.callbacks)
	m.mu.RUnlock()

	for _, cb := range callbacks {
		cb.OnFail(taskID, result)
	}
}

// Ensure CallbackManager implements scheduler.CallbackHandler at compile time.
var _ scheduler.CallbackHandler = (*CallbackManager)(nil)
