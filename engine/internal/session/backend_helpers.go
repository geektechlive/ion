package session

import (
	"github.com/dsswift/ion/engine/internal/backend"
)

// resolvedBackend returns the backend that will handle a run for the given model.
// For HybridBackend, this delegates to BackendForModel so the caller gets the
// concrete inner backend (CliBackend or ApiBackend) and can apply the right path.
// For all other backend types, m.backend is returned unchanged.
func (m *Manager) resolvedBackend(model string) backend.RunBackend {
	if h, ok := m.backend.(*backend.HybridBackend); ok {
		return h.BackendForModel(model)
	}
	return m.backend
}

// newChildBackend returns a fresh RunBackend of the same kind as the
// Manager's parent backend. All child-agent dispatch paths must use this
// instead of calling NewApiBackend()/NewCliBackend() directly, so that
// backend: "cli" sessions produce CLI children and backend: "hybrid" sessions
// propagate the auth resolver to child API-backend runs.
func (m *Manager) newChildBackend() backend.RunBackend {
	if h, ok := m.backend.(*backend.HybridBackend); ok {
		return h.NewChild()
	}
	if _, ok := m.backend.(*backend.CliBackend); ok {
		return backend.NewCliBackend()
	}
	return backend.NewApiBackend()
}
