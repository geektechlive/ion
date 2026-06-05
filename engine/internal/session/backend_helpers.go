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
// For HybridBackend, it routes by resolved provider ID:
//   - "anthropic" → inner *CliBackend (Claude subscription)
//   - "openai"    → inner *CodexCliBackend (Codex CLI subprocess)
//   - everything else → inner *ApiBackend (HTTP provider keys)
//
// The lookup tries the model registry first (GetModelInfo), then falls
// back to prefix-based resolution (ProviderNameForModel) for models not
// yet registered (e.g. newly released gpt-* or o-series).
//
// This is the single helper that localizes hybrid awareness inside the
// session package. Every former m.backend.(*backend.CliBackend) /
// (*backend.ApiBackend) type assertion is now resolvedBackend(model).(...).
func (m *Manager) resolvedBackend(model string) backend.RunBackend {
	h, ok := m.backend.(*backend.HybridBackend)
	if !ok {
		return m.backend
	}
	providerID := ""
	if info := providers.GetModelInfo(model); info != nil {
		providerID = info.ProviderID
	} else {
		providerID = providers.ProviderNameForModel(model)
	}
	switch providerID {
	case "anthropic":
		utils.Debug("Session", "resolvedBackend: model="+model+" providerID=anthropic → inner CliBackend")
		return h.InnerCli()
	case "openai":
		utils.Debug("Session", "resolvedBackend: model="+model+" providerID=openai → inner CodexCliBackend")
		return h.InnerCodex()
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
