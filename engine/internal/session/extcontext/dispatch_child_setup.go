package extcontext

import (
	"fmt"

	"github.com/dsswift/ion/engine/internal/backend"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/internal/utils"
)

// loadChildExtension loads the child extension if specified in opts. Returns
// the Host (nil if no extension or load failed). Modifies opts.SystemPrompt
// in-place if the extension provides additional system prompt content.
// childDepth and childDispatchId are passed through so extension contexts
// built for this child carry the correct dispatch ancestry. It fires
// session_start and before_agent_start so the child's system prompt is
// composed before the run begins.
//
// Split out of dispatch_agent.go (same package) to keep that file under the
// 800-line cap; see dispatch_lifecycle_callbacks.go for the same rationale.
func loadChildExtension(sa SessionAccessor, registry *DispatchRegistry, opts *extension.DispatchAgentOpts, model, projectPath string, childDepth int, childDispatchId string) *extension.Host {
	if opts.ExtensionDir == "" {
		return nil
	}

	childExtHost := extension.NewHost()
	if cfg := sa.EngineConfig(); cfg != nil && cfg.Timeouts != nil {
		childExtHost.SetRPCTimeout(cfg.Timeouts.ExtensionRpc())
	}
	extCfg := &extension.ExtensionConfig{
		ExtensionDir:     opts.ExtensionDir,
		Model:            model,
		WorkingDirectory: projectPath,
	}
	// Make nested dispatch working-directory resolution observable: the child
	// extension is configured with the resolved projectPath here, so log it
	// alongside the dispatch id and depth for the child.
	utils.Debug("Dispatch", fmt.Sprintf(
		"dispatch child setup: agent=%q workingDirectory=%q dispatchId=%s depth=%d session=%s",
		opts.Name, projectPath, childDispatchId, childDepth, sa.SessionKey(),
	))
	if err := childExtHost.Load(opts.ExtensionDir, extCfg); err != nil {
		utils.Log("Session", "child extension load failed: "+err.Error())
		return nil
	}

	// Fire session_start on child extension.
	childCtx := NewExtContext(sa, ExtContextOpts{
		Depth:      childDepth,
		DispatchId: childDispatchId,
		Registry:   registry,
	})
	_ = childExtHost.FireSessionStart(childCtx)

	// Wire before_agent_start for system prompt.
	basCtx := NewExtContext(sa, ExtContextOpts{
		Depth:      childDepth,
		DispatchId: childDispatchId,
		Registry:   registry,
	})
	extSysPrompt, _, _ := childExtHost.FireBeforeAgentStart(basCtx, extension.AgentInfo{
		Name: opts.Name,
		Task: opts.Task,
	})
	if extSysPrompt != "" {
		if opts.SystemPrompt != "" {
			opts.SystemPrompt = opts.SystemPrompt + "\n\n" + extSysPrompt
		} else {
			opts.SystemPrompt = extSysPrompt
		}
	}

	return childExtHost
}

// configurableBackend is satisfied by any backend that can accept a per-run
// RunConfig. Detection is by interface assertion (not a concrete type switch)
// so that any backend implementing StartRunWithConfig — the production
// *ApiBackend and *HybridBackend, plus test stubs that opt in — threads the
// config through. The prior concrete type switch silently dropped the
// RunConfig for any other backend type (a wrapped backend, a test stub),
// which lost DefaultModel threading and the AgentSpawner. Mirrors the
// session-package startChildRun in backend_helpers.go.
type configurableBackend interface {
	StartRunWithConfig(requestID string, options types.RunOptions, cfg *backend.RunConfig)
}

// startChild dispatches the child run on the appropriate backend. When a
// RunConfig is supplied and the backend can accept it, the config is threaded
// through (carrying DefaultModel, the AgentSpawner for nested dispatch, hooks,
// etc.); otherwise the run degrades to the plain StartRun path.
func startChild(child backend.RunBackend, reqID string, runOpts types.RunOptions, cfg *backend.RunConfig) {
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

// logDispatchWorkdir emits the resolved working directory for a dispatch,
// including which branch supplied it (source=opts when the caller passed
// ProjectPath, source=fallback when it was inherited from the parent session).
// This closes the dispatch cwd logging gap: the root session logs its cwd at
// start (start_session.go), but a dispatched child's resolved cwd was
// previously never logged. Lives here (rather than inline in dispatch_agent.go)
// to keep that file under the 800-line cap; it is a pure log addition.
func logDispatchWorkdir(agentName, projectPath, source, dispatchID string, depth int, sessionKey string) {
	utils.Log("Dispatch", fmt.Sprintf(
		"dispatch working directory resolved: agent=%q path=%q source=%s dispatchId=%s depth=%d session=%s",
		agentName, projectPath, source, dispatchID, depth, sessionKey,
	))
}
