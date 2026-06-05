package session

import (
	"github.com/dsswift/ion/engine/internal/backend"
	"github.com/dsswift/ion/engine/internal/providers"
	"github.com/dsswift/ion/engine/internal/utils"
)

// resolvedBackend returns the inner backend that should handle a run for
// the given model. For non-hybrid backends (plain CliBackend or ApiBackend,
// or any test mock), it returns m.backend unchanged so existing type-
// assertion call sites continue to work without modification.
//
// For HybridBackend, it routes by resolved provider ID:
//   - "anthropic" → inner *CliBackend (Claude subscription)
//   - "openai"    → inner *CodexCliBackend (Codex CLI subprocess)
//   - everything else → inner *ApiBackend (HTTP provider keys)
//
// This is the single helper that localizes hybrid awareness inside the
// session package. Every former m.backend.(*backend.CliBackend) /
// (*backend.ApiBackend) type assertion is now resolvedBackend(model).(...).
func (m *Manager) resolvedBackend(model string) backend.RunBackend {
	h, ok := m.backend.(*backend.HybridBackend)
	if !ok {
		return m.backend
	}
	info := providers.GetModelInfo(model)
	if info != nil {
		switch info.ProviderID {
		case "anthropic":
			utils.Debug("Session", "resolvedBackend: model="+model+" providerID=anthropic → inner CliBackend")
			return h.InnerCli()
		case "openai":
			utils.Debug("Session", "resolvedBackend: model="+model+" providerID=openai → inner CodexCliBackend")
			return h.InnerCodex()
		}
	}
	utils.Debug("Session", "resolvedBackend: model="+model+" → inner ApiBackend (default)")
	return h.InnerApi()
}

// isSubprocessBackend reports whether b is a CLI subprocess backend (either
// *CliBackend or *CodexCliBackend). Session-layer functions that should fire
// for any subprocess backend — notably fireBeforePromptCli and
// fireCliTurnHooks — use this instead of a single *CliBackend type assertion.
func isSubprocessBackend(b backend.RunBackend) bool {
	switch b.(type) {
	case *backend.CliBackend, *backend.CodexCliBackend:
		return true
	}
	return false
}

// newChildBackend returns a fresh RunBackend of the same kind as the
// Manager's parent backend. All child-agent dispatch paths must use this
// instead of calling NewApiBackend()/NewCliBackend() directly, so that
// backend: "cli" sessions produce CLI children, backend: "codex" produces
// Codex children, and backend: "hybrid" sessions produce hybrid children
// whose inner *ApiBackend inherits the parent's auth resolver.
//
// When m.childBackendOverride is set (test-only), the override factory
// runs instead. This is the only injection point unit tests have to
// substitute a stubbed child backend for the spawner closure.
func (m *Manager) newChildBackend() backend.RunBackend {
	if m.childBackendOverride != nil {
		return m.childBackendOverride()
	}
	switch b := m.backend.(type) {
	case *backend.CliBackend:
		return backend.NewCliBackend()
	case *backend.CodexCliBackend:
		return backend.NewCodexCliBackend()
	case *backend.HybridBackend:
		// HybridBackend.NewChild propagates the auth resolver from the
		// parent's inner *ApiBackend so non-Claude, non-Codex child runs can
		// resolve provider credentials. Without this, dispatching ion_agent
		// with model: "gpt-4.1" under hybrid would fail silently.
		return b.NewChild()
	default:
		return backend.NewApiBackend()
	}
}
