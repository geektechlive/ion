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
		// parent's inner *ApiBackend so non-Claude, non-Codex child runs can
		// resolve provider credentials. Without this, dispatching ion_agent
		// with model: "gpt-4.1" under hybrid would fail silently.
		return b.NewChild()
	default:
		return backend.NewApiBackend()
	}
}

// progressBumpable is satisfied by any backend that exposes the run-progress
// watchdog clock for refreshing. *ApiBackend implements it directly; the
// HybridBackend's parent runs live on its inner *ApiBackend (returned by
// InnerApi), which also implements it. CliBackend and test stubs do not, so
// the bump degrades to a no-op for them.
type progressBumpable interface {
	BumpRunProgress(requestID string)
}

// bumpParentProgress refreshes the parent run's run-progress watchdog clock for
// the given session's active run. It is the session-side half of the dispatch
// liveness fix: a healthy child agent's events flow on the *child* backend and
// never reach the parent run's emit(), so the parent — parked in the deadline-
// exempt Agent tool call — would have a stalling progress clock once the
// self-emitted ToolStalledEvent advisory stopped counting as progress (see
// ApiBackend.emitWithoutProgress). Calling this on every genuine child event
// reports the child's liveness to the parent's clock.
//
// Resolution: read the session's current requestID under the Manager lock (the
// same guard SendAbort uses), then ask the resolved parent backend to bump it.
// No-op when there is no active run or the backend cannot bump (CLI / stubs).
//
// The bump targets the parent ApiBackend directly rather than going through
// resolvedBackend(model): for HybridBackend the parent run is always on the
// inner *ApiBackend (the CLI inner does not run the run-progress watchdog), so
// InnerApi() is the correct and only target.
func (m *Manager) bumpParentProgress(s *engineSession) {
	if s == nil {
		return
	}
	m.mu.Lock()
	rid := s.requestID
	m.mu.Unlock()
	if rid == "" {
		return
	}

	target := m.backend
	if h, ok := m.backend.(*backend.HybridBackend); ok {
		target = h.InnerApi()
	}
	if pb, ok := target.(progressBumpable); ok {
		pb.BumpRunProgress(rid)
	}
}

// humanWaitSuspendable is satisfied by any backend that lets the run-progress
// watchdog be paused for an intentional, indefinite human-wait (an elicitation
// or a permission dialog awaiting a user decision). *ApiBackend implements it
// directly; the HybridBackend's parent runs live on its inner *ApiBackend
// (InnerApi), which also implements it. CliBackend and test stubs do not, so the
// begin/end pair degrades to a no-op for them.
type humanWaitSuspendable interface {
	BeginHumanWait(requestID string)
	EndHumanWait(requestID string)
}

// beginHumanWait tells the parent run's run-progress watchdog that the given
// session's active run is entering an intentional, indefinite human-wait, so the
// watchdog must not cancel it for idleness while a user decision is pending. Must
// be paired with endHumanWait on every exit path (use defer at the call site).
//
// Resolution mirrors bumpParentProgress exactly: read the session's current
// requestID under the Manager lock, resolve the parent ApiBackend (InnerApi for
// HybridBackend — the CLI inner does not run the run-progress watchdog), and
// forward. No-op when there is no active run or the backend cannot suspend
// (CLI / stubs); the indefinite-human-wait guarantee for those backends is
// enforced by their own dialog layers, not by this watchdog.
func (m *Manager) beginHumanWait(s *engineSession) {
	if s == nil {
		return
	}
	m.mu.Lock()
	rid := s.requestID
	m.mu.Unlock()
	if rid == "" {
		return
	}

	target := m.backend
	if h, ok := m.backend.(*backend.HybridBackend); ok {
		target = h.InnerApi()
	}
	if hw, ok := target.(humanWaitSuspendable); ok {
		hw.BeginHumanWait(rid)
	}
}

// endHumanWait is the matched half of beginHumanWait: it tells the parent run's
// run-progress watchdog that a human-wait span has ended, resuming the watchdog
// (with a fresh idle window) once the run's last open human-wait closes. Safe to
// defer; same resolution and no-op semantics as beginHumanWait.
func (m *Manager) endHumanWait(s *engineSession) {
	if s == nil {
		return
	}
	m.mu.Lock()
	rid := s.requestID
	m.mu.Unlock()
	if rid == "" {
		return
	}

	target := m.backend
	if h, ok := m.backend.(*backend.HybridBackend); ok {
		target = h.InnerApi()
	}
	if hw, ok := target.(humanWaitSuspendable); ok {
		hw.EndHumanWait(rid)
	}
}
