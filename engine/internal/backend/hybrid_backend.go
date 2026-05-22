package backend

import (
	"fmt"
	"strings"
	"sync"

	"github.com/dsswift/ion/engine/internal/auth"
	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/internal/utils"
)

// IsClaudeModel reports whether the given model ID should be routed to the
// CLI backend (claude-* prefix) rather than the API backend.
func IsClaudeModel(model string) bool {
	return strings.HasPrefix(model, "claude-")
}

// HybridBackend routes each run to either the CLI backend (for claude-* models,
// which use the Claude Code subscription) or the API backend (for all other
// models, which require provider API keys configured in engine.json).
//
// This lets Ion Desktop use OpenAI, Gemini, and other models alongside Claude
// without requiring separate engine instances or losing the Claude subscription benefit.
//
// The run-to-backend mapping is recorded at StartRun time and used for all
// subsequent operations (Cancel, IsRunning, WriteToStdin, Steer) on that run.
type HybridBackend struct {
	cli      *CliBackend
	api      *ApiBackend
	resolver *auth.Resolver

	mu        sync.Mutex
	runRoutes map[string]RunBackend // requestID -> inner backend that owns this run
}

// NewHybridBackend creates a HybridBackend with fresh CLI and API inner backends.
func NewHybridBackend() *HybridBackend {
	return &HybridBackend{
		cli:       NewCliBackend(),
		api:       NewApiBackend(),
		runRoutes: make(map[string]RunBackend),
	}
}

// SetAuthResolver propagates the auth resolver to the API inner backend.
// Called once at startup by cmd_serve.go.
func (h *HybridBackend) SetAuthResolver(r *auth.Resolver) {
	h.mu.Lock()
	h.resolver = r
	h.mu.Unlock()
	h.api.SetAuthResolver(r)
}

// BackendForModel returns the appropriate inner backend for the given model ID.
// claude-* models route to CLI; everything else routes to API.
func (h *HybridBackend) BackendForModel(model string) RunBackend {
	if IsClaudeModel(model) {
		return h.cli
	}
	return h.api
}

// InnerApiBackend returns the inner ApiBackend for callers that need
// ApiBackend-specific methods such as StartRunWithConfig or GetContextUsage.
func (h *HybridBackend) InnerApiBackend() *ApiBackend {
	return h.api
}

// InnerCliBackend returns the inner CliBackend.
func (h *HybridBackend) InnerCliBackend() *CliBackend {
	return h.cli
}

// NewChild returns a new HybridBackend suitable for child-agent dispatch.
// The auth resolver is propagated so child API-backend runs can resolve API keys.
func (h *HybridBackend) NewChild() *HybridBackend {
	child := NewHybridBackend()
	h.mu.Lock()
	r := h.resolver
	h.mu.Unlock()
	if r != nil {
		child.SetAuthResolver(r)
	}
	return child
}

// Steer routes a mid-run steering message to the backend that owns requestID.
// Returns true if the run is API-backed and the message was delivered, false
// if the run is CLI-backed (caller should fall back to WriteToStdin).
func (h *HybridBackend) Steer(requestID, message string) bool {
	b := h.route(requestID)
	if api, ok := b.(*ApiBackend); ok {
		return api.Steer(requestID, message)
	}
	return false
}

// RouteForRun returns the inner backend that is handling requestID, or nil
// if no mapping exists (run not yet started or already finished).
func (h *HybridBackend) RouteForRun(requestID string) RunBackend {
	h.mu.Lock()
	b := h.runRoutes[requestID]
	h.mu.Unlock()
	return b
}

// record saves the backend mapping for requestID.
func (h *HybridBackend) record(requestID string, b RunBackend) {
	h.mu.Lock()
	h.runRoutes[requestID] = b
	h.mu.Unlock()
}

// unrecord removes the run-to-backend mapping once the run exits.
func (h *HybridBackend) unrecord(requestID string) {
	h.mu.Lock()
	delete(h.runRoutes, requestID)
	h.mu.Unlock()
}

// route returns the recorded backend for requestID, falling back to api
// with a warning if no mapping is found.
func (h *HybridBackend) route(requestID string) RunBackend {
	h.mu.Lock()
	b, ok := h.runRoutes[requestID]
	h.mu.Unlock()
	if ok {
		return b
	}
	utils.Log("HybridBackend", "route: no recorded backend for "+requestID+", falling back to api")
	return h.api
}

// StartRun routes the run to CLI or API based on opts.Model and records the
// mapping so Cancel/IsRunning/WriteToStdin can find the right inner backend.
func (h *HybridBackend) StartRun(requestID string, opts types.RunOptions) {
	b := h.BackendForModel(opts.Model)
	h.record(requestID, b)
	utils.Log("HybridBackend", fmt.Sprintf("StartRun %s: routing to %T (model=%s)", requestID, b, opts.Model))
	b.StartRun(requestID, opts)
}

// StartRunWithConfig is the per-run-config variant used when session wiring
// has built a RunConfig (hooks, perm engine, MCP tools, etc.). For API-routed
// runs the config is forwarded; for CLI-routed runs it falls back to StartRun
// because CliBackend wires its own hooks via subprocess flags.
func (h *HybridBackend) StartRunWithConfig(requestID string, opts types.RunOptions, cfg *RunConfig) {
	b := h.BackendForModel(opts.Model)
	h.record(requestID, b)
	utils.Log("HybridBackend", fmt.Sprintf("StartRunWithConfig %s: routing to %T (model=%s)", requestID, b, opts.Model))
	if _, isApi := b.(*ApiBackend); isApi {
		h.api.StartRunWithConfig(requestID, opts, cfg)
	} else {
		b.StartRun(requestID, opts)
	}
}

// Cancel cancels the run on whichever inner backend owns it.
func (h *HybridBackend) Cancel(requestID string) bool {
	return h.route(requestID).Cancel(requestID)
}

// IsRunning checks the recorded backend for requestID; falls back to checking
// both inner backends if no mapping exists.
func (h *HybridBackend) IsRunning(requestID string) bool {
	h.mu.Lock()
	b, ok := h.runRoutes[requestID]
	h.mu.Unlock()
	if ok {
		return b.IsRunning(requestID)
	}
	return h.cli.IsRunning(requestID) || h.api.IsRunning(requestID)
}

// WriteToStdin delegates to the inner backend that owns the run.
// ApiBackend.WriteToStdin is a no-op; CliBackend pipes the message to the subprocess.
func (h *HybridBackend) WriteToStdin(requestID string, msg interface{}) error {
	return h.route(requestID).WriteToStdin(requestID, msg)
}

// FlushConversations flushes both inner backends.
func (h *HybridBackend) FlushConversations() {
	h.cli.FlushConversations()
	h.api.FlushConversations()
}

// OnNormalized registers the normalized-event callback on both inner backends.
func (h *HybridBackend) OnNormalized(fn func(string, types.NormalizedEvent)) {
	h.cli.OnNormalized(fn)
	h.api.OnNormalized(fn)
}

// OnExit registers the exit callback on both inner backends, wrapping it to
// clean up the run-to-backend mapping when a run finishes.
func (h *HybridBackend) OnExit(fn func(string, *int, *string, string)) {
	wrapped := func(runID string, code *int, signal *string, sessionID string) {
		h.unrecord(runID)
		fn(runID, code, signal, sessionID)
	}
	h.cli.OnExit(wrapped)
	h.api.OnExit(wrapped)
}

// OnError registers the error callback on both inner backends.
func (h *HybridBackend) OnError(fn func(string, error)) {
	h.cli.OnError(fn)
	h.api.OnError(fn)
}
