package orchestrator

import (
	"sync"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/scheduler"
)

// CallbackID uniquely identifies a registered callback for unregistration.
type CallbackID int

// CallbackManager manages event callbacks with thread-safe registration,
// unregistration, and invocation. It implements scheduler.CallbackHandler.
type CallbackManager struct {
	mu sync.RWMutex

	nextID CallbackID

	// Registered callbacks by type
	onAgentStart map[CallbackID]func(taskID string, task *beads.Task)
	onOutput     map[CallbackID]func(taskID string, output string, isError bool)
	onLiveFeed   map[CallbackID]func(taskID string, event *agent.LiveFeedEvent)
	onDone       map[CallbackID]func(taskID string, result *agent.Result)
	onFail       map[CallbackID]func(taskID string, result *agent.Result)
}

// NewCallbackManager creates a new CallbackManager.
func NewCallbackManager() *CallbackManager {
	return &CallbackManager{
		onAgentStart: make(map[CallbackID]func(taskID string, task *beads.Task)),
		onOutput:     make(map[CallbackID]func(taskID string, output string, isError bool)),
		onLiveFeed:   make(map[CallbackID]func(taskID string, event *agent.LiveFeedEvent)),
		onDone:       make(map[CallbackID]func(taskID string, result *agent.Result)),
		onFail:       make(map[CallbackID]func(taskID string, result *agent.Result)),
	}
}

// RegisterOnAgentStart registers a callback for agent start events.
// Returns a CallbackID that can be used to unregister the callback.
func (m *CallbackManager) RegisterOnAgentStart(fn func(taskID string, task *beads.Task)) CallbackID {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextID
	m.nextID++
	m.onAgentStart[id] = fn
	return id
}

// RegisterOnOutput registers a callback for output events.
func (m *CallbackManager) RegisterOnOutput(fn func(taskID string, output string, isError bool)) CallbackID {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextID
	m.nextID++
	m.onOutput[id] = fn
	return id
}

// RegisterOnLiveFeed registers a callback for live feed events.
func (m *CallbackManager) RegisterOnLiveFeed(fn func(taskID string, event *agent.LiveFeedEvent)) CallbackID {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextID
	m.nextID++
	m.onLiveFeed[id] = fn
	return id
}

// RegisterOnDone registers a callback for task completion events.
func (m *CallbackManager) RegisterOnDone(fn func(taskID string, result *agent.Result)) CallbackID {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextID
	m.nextID++
	m.onDone[id] = fn
	return id
}

// RegisterOnFail registers a callback for task failure events.
func (m *CallbackManager) RegisterOnFail(fn func(taskID string, result *agent.Result)) CallbackID {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextID
	m.nextID++
	m.onFail[id] = fn
	return id
}

// Unregister removes a callback by its ID from all callback types.
// This is safe to call even if the ID doesn't exist.
func (m *CallbackManager) Unregister(id CallbackID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.onAgentStart, id)
	delete(m.onOutput, id)
	delete(m.onLiveFeed, id)
	delete(m.onDone, id)
	delete(m.onFail, id)
}

// RegisterAll registers all callbacks from an EventCallbacks struct.
// Returns a slice of CallbackIDs that can be used to unregister all callbacks.
func (m *CallbackManager) RegisterAll(callbacks *EventCallbacks) []CallbackID {
	if callbacks == nil {
		return nil
	}

	var ids []CallbackID

	if callbacks.OnAgentStartFn != nil {
		ids = append(ids, m.RegisterOnAgentStart(callbacks.OnAgentStartFn))
	}
	if callbacks.OnOutputFn != nil {
		ids = append(ids, m.RegisterOnOutput(callbacks.OnOutputFn))
	}
	if callbacks.OnLiveFeedFn != nil {
		ids = append(ids, m.RegisterOnLiveFeed(callbacks.OnLiveFeedFn))
	}
	if callbacks.OnDoneFn != nil {
		ids = append(ids, m.RegisterOnDone(callbacks.OnDoneFn))
	}
	if callbacks.OnFailFn != nil {
		ids = append(ids, m.RegisterOnFail(callbacks.OnFailFn))
	}

	return ids
}

// UnregisterAll removes multiple callbacks by their IDs.
func (m *CallbackManager) UnregisterAll(ids []CallbackID) {
	for _, id := range ids {
		m.Unregister(id)
	}
}

// OnAgentStart implements scheduler.CallbackHandler.
// Invokes all registered OnAgentStart callbacks in a thread-safe manner.
func (m *CallbackManager) OnAgentStart(taskID string, task *beads.Task) {
	m.mu.RLock()
	// Copy callbacks to avoid holding lock during invocation
	callbacks := make([]func(taskID string, task *beads.Task), 0, len(m.onAgentStart))
	for _, fn := range m.onAgentStart {
		callbacks = append(callbacks, fn)
	}
	m.mu.RUnlock()

	for _, fn := range callbacks {
		fn(taskID, task)
	}
}

// OnOutput implements scheduler.CallbackHandler.
// Invokes all registered OnOutput callbacks in a thread-safe manner.
func (m *CallbackManager) OnOutput(taskID string, output string, isError bool) {
	m.mu.RLock()
	callbacks := make([]func(taskID string, output string, isError bool), 0, len(m.onOutput))
	for _, fn := range m.onOutput {
		callbacks = append(callbacks, fn)
	}
	m.mu.RUnlock()

	for _, fn := range callbacks {
		fn(taskID, output, isError)
	}
}

// OnLiveFeed implements scheduler.CallbackHandler.
// Invokes all registered OnLiveFeed callbacks in a thread-safe manner.
func (m *CallbackManager) OnLiveFeed(taskID string, event *agent.LiveFeedEvent) {
	m.mu.RLock()
	callbacks := make([]func(taskID string, event *agent.LiveFeedEvent), 0, len(m.onLiveFeed))
	for _, fn := range m.onLiveFeed {
		callbacks = append(callbacks, fn)
	}
	m.mu.RUnlock()

	for _, fn := range callbacks {
		fn(taskID, event)
	}
}

// OnDone implements scheduler.CallbackHandler.
// Invokes all registered OnDone callbacks in a thread-safe manner.
func (m *CallbackManager) OnDone(taskID string, result *agent.Result) {
	m.mu.RLock()
	callbacks := make([]func(taskID string, result *agent.Result), 0, len(m.onDone))
	for _, fn := range m.onDone {
		callbacks = append(callbacks, fn)
	}
	m.mu.RUnlock()

	for _, fn := range callbacks {
		fn(taskID, result)
	}
}

// OnFail implements scheduler.CallbackHandler.
// Invokes all registered OnFail callbacks in a thread-safe manner.
func (m *CallbackManager) OnFail(taskID string, result *agent.Result) {
	m.mu.RLock()
	callbacks := make([]func(taskID string, result *agent.Result), 0, len(m.onFail))
	for _, fn := range m.onFail {
		callbacks = append(callbacks, fn)
	}
	m.mu.RUnlock()

	for _, fn := range callbacks {
		fn(taskID, result)
	}
}

// Ensure CallbackManager implements scheduler.CallbackHandler at compile time.
var _ scheduler.CallbackHandler = (*CallbackManager)(nil)
