package session

import (
	"github.com/dsswift/ion/engine/internal/backend"
	"github.com/dsswift/ion/engine/internal/providers"
	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/internal/utils"
)

// resolvedBackend returns the inner backend that should handle a run for
// the given model. For non-hybrid backends (plain CliBackend or ApiBackend,
// or any test mock), it returns m.backend unchanged so existing type-
// assertion call sites continue to work without modification.
//
// For HybridBackend, it asks providers.GetModelInfo about the model and
// returns the inner *CliBackend when the resolved provider ID is
// "anthropic", or the inner *ApiBackend otherwise (including for
// unregistered models — those route to the API path where a clean
// provider error can surface).
//
// This is the single helper that localizes hybrid awareness inside the
// session package. Every former m.backend.(*backend.CliBackend) /
// (*backend.ApiBackend) type assertion is now resolvedBackend(model).(...).
func (m *Manager) resolvedBackend(model string) backend.RunBackend {
	h, ok := m.backend.(*backend.HybridBackend)
	if !ok {
		return m.backend
	}
	if info := providers.GetModelInfo(model); info != nil && info.ProviderID == "anthropic" {
		utils.Debug("Session", "resolvedBackend: model="+model+" providerID=anthropic → inner CliBackend")
		return h.InnerCli()
	}
	utils.Debug("Session", "resolvedBackend: model="+model+" → inner ApiBackend (default)")
	return h.InnerApi()
}

// newChildBackend returns a fresh RunBackend of the same kind as the
// Manager's parent backend. All child-agent dispatch paths must use this
// instead of calling NewApiBackend()/NewCliBackend() directly, so that
// backend: "cli" sessions produce CLI children and backend: "hybrid"
// sessions produce hybrid children whose inner *ApiBackend inherits the
// parent's auth resolver.
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
	case *backend.HybridBackend:
		// HybridBackend.NewChild propagates the auth resolver from the
		// parent's inner *ApiBackend so non-Claude child runs can resolve
		// provider credentials. Without this, dispatching ion_agent with
		// model: "gpt-4.1" under hybrid would fail silently.
		return b.NewChild()
	default:
		return backend.NewApiBackend()
	}
}

// startChildRun dispatches a child run with an optional RunConfig, choosing
// StartRunWithConfig when the concrete backend type supports it. This is
// the parallel of extcontext.startChild for callers inside the session
// package (currently prompt_agent_spawner.go) that need to thread per-run
// config — most importantly DefaultModel — into the child run so the
// runloop's existing fallback at runloop.go:57 can fire when the child's
// requested model doesn't resolve to a provider.
//
// When cfg is nil the function degrades to plain StartRun, preserving the
// pre-existing behaviour for callers that don't need per-run config.
//
// Detection is by interface assertion (configurableBackend) rather than
// concrete type switch so that test stubs can opt in by implementing
// StartRunWithConfig — the production *ApiBackend and *HybridBackend both
// satisfy the interface.
type configurableBackend interface {
	StartRunWithConfig(requestID string, options types.RunOptions, cfg *backend.RunConfig)
}

func startChildRun(child backend.RunBackend, reqID string, runOpts types.RunOptions, cfg *backend.RunConfig) {
	if cfg != nil {
		if cb, ok := child.(configurableBackend); ok {
			cb.StartRunWithConfig(reqID, runOpts, cfg)
			return
		}
	}
	// CliBackend, generic test stubs, or any backend that doesn't carry
	// RunConfig fall through to the plain interface method.
	child.StartRun(reqID, runOpts)
}
