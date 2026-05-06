// Package agents manages agent handles, specs, and state pills for a single
// session. Thread-safe with its own mutex.
package agents

import (
	"sync"

	"github.com/dsswift/ion/engine/internal/types"
)

// Registry manages agent handles, specs, and state pills for a single session.
type Registry struct {
	mu             sync.RWMutex
	handles        map[string]types.AgentHandle
	specs          map[string]types.AgentSpec
	states         []types.AgentStateUpdate
	lastExtStates  []types.AgentStateUpdate
}

// NewRegistry creates a ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{
		handles: make(map[string]types.AgentHandle),
		specs:   make(map[string]types.AgentSpec),
	}
}

// --- Handles ---

// RegisterHandle registers an agent handle by name.
func (r *Registry) RegisterHandle(name string, handle types.AgentHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handles[name] = handle
}

// DeregisterHandle removes an agent handle by name.
func (r *Registry) DeregisterHandle(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.handles, name)
}

// LookupHandle returns the handle for the given name, if registered.
func (r *Registry) LookupHandle(name string) (types.AgentHandle, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handles[name]
	return h, ok
}

// AllHandles returns a snapshot of every handle. The caller may read the map
// safely; mutations require going through Register/Deregister.
func (r *Registry) AllHandles() map[string]types.AgentHandle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]types.AgentHandle, len(r.handles))
	for k, v := range r.handles {
		out[k] = v
	}
	return out
}

// ClearHandles removes every handle and returns their PIDs so the caller can
// kill the processes.
func (r *Registry) ClearHandles() (pids []int, names []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, handle := range r.handles {
		pids = append(pids, handle.PID)
		names = append(names, name)
	}
	r.handles = make(map[string]types.AgentHandle)
	return pids, names
}

// HandleCount returns the number of registered handles.
func (r *Registry) HandleCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handles)
}

// --- Specs ---

// RegisterSpec registers an agent spec. Does nothing if spec.Name is empty.
func (r *Registry) RegisterSpec(spec types.AgentSpec) {
	if spec.Name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specs[spec.Name] = spec
}

// DeregisterSpec removes an agent spec by name.
func (r *Registry) DeregisterSpec(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.specs, name)
}

// LookupSpec returns the spec for the given name, if registered.
func (r *Registry) LookupSpec(name string) (types.AgentSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.specs[name]
	return spec, ok
}

// AllSpecNames returns the names of all registered specs.
func (r *Registry) AllSpecNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.specs))
	for n := range r.specs {
		out = append(out, n)
	}
	return out
}

// --- States ---

// AppendState appends an agent state update.
func (r *Registry) AppendState(state types.AgentStateUpdate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, state)
}

// UpdateState finds a state by name and applies the updater function.
func (r *Registry) UpdateState(name string, updater func(*types.AgentStateUpdate)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.states {
		if r.states[i].Name == name {
			updater(&r.states[i])
			return
		}
	}
}

// ClearStates removes all states.
func (r *Registry) ClearStates() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = nil
}

// MergedSnapshot returns a combined slice of extension-managed states and
// engine-managed states.
func (r *Registry) MergedSnapshot() []types.AgentStateUpdate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	merged := make([]types.AgentStateUpdate, 0, len(r.lastExtStates)+len(r.states))
	merged = append(merged, r.lastExtStates...)
	merged = append(merged, r.states...)
	return merged
}

// CacheExtStates caches the most recent extension-emitted agent states.
func (r *Registry) CacheExtStates(states []types.AgentStateUpdate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastExtStates = make([]types.AgentStateUpdate, len(states))
	copy(r.lastExtStates, states)
}

// LastExtStates returns the cached extension agent states.
func (r *Registry) LastExtStates() []types.AgentStateUpdate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]types.AgentStateUpdate, len(r.lastExtStates))
	copy(out, r.lastExtStates)
	return out
}

// IsDescendant checks if agent is a descendant of ancestor in the handle
// registry. Uses the ParentAgent chain with cycle protection.
func (r *Registry) IsDescendant(agent, ancestor string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return isDescendant(r.handles, agent, ancestor)
}

// isDescendant checks if agent is a descendant of ancestor in the given map.
func isDescendant(registry map[string]types.AgentHandle, agent, ancestor string) bool {
	visited := make(map[string]bool)
	current := agent
	for {
		handle, ok := registry[current]
		if !ok || handle.ParentAgent == "" {
			return false
		}
		if handle.ParentAgent == ancestor {
			return true
		}
		if visited[handle.ParentAgent] {
			return false // cycle protection
		}
		visited[current] = true
		current = handle.ParentAgent
	}
}
