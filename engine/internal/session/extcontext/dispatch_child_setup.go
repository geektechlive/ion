package extcontext

import (
	"fmt"
	"strings"

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
	logChildSuppressTools(runOpts, reqID)
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

// logChildSuppressTools records both branches of a dispatched child's tool
// suppression decision: the tools the harness denied this child, or the fact
// that it denied none.
//
// Deliberately logged from startChild rather than from the dispatch assembly in
// dispatch_agent.go. startChild is the last hop before backend.StartRun, so the
// slice logged here is the value that actually reaches the backend — after
// BuildChildToolServer has run over the same runOpts. Logging at assembly time
// would record intent; logging here records fact. (It also keeps dispatch_agent.go
// at its 800-line cap, which has no headroom for another call site.)
//
// backend.CliBackend.runProcess logs the other half of the chain — whether --disallowedTools
// was actually emitted onto the child's argv. Without the no-suppression branch
// below, a log reader seeing no --disallowedTools cannot distinguish "the harness
// asked for nothing" from "the engine dropped what it was given", which is the
// ambiguity that made the original gap invisible: every dispatched child silently
// ran with an empty denylist and nothing in the log said so.
//
// AllowedTools is logged alongside because the two interact and the interaction
// is counter-intuitive: under the --permission-mode bypassPermissions that every
// dispatched child runs with, the allowlist is advisory for built-ins while the
// denylist binds. A reader diagnosing "why could this child still call Bash"
// needs both lists in one line to see that Bash was absent from the denylist,
// not merely absent from the allowlist.
func logChildSuppressTools(runOpts types.RunOptions, reqID string) {
	if len(runOpts.SuppressTools) > 0 {
		utils.Log("Dispatch", fmt.Sprintf(
			"child tool suppression: denying %d tool(s) [%s] reqId=%s allowedTools=[%s]",
			len(runOpts.SuppressTools), strings.Join(runOpts.SuppressTools, ","),
			reqID, strings.Join(runOpts.AllowedTools, ","),
		))
		return
	}
	utils.Log("Dispatch", fmt.Sprintf(
		"child tool suppression: none requested, child keeps every tool its backend offers reqId=%s allowedTools=[%s]",
		reqID, strings.Join(runOpts.AllowedTools, ","),
	))
}

// logChildCliResume records both branches of a dispatched child's Claude-resume
// decision: whether the caller supplied a Claude session UUID for the child to
// resume, or whether the child will spawn a fresh Claude session.
//
// backend.cliResumeArgs logs the other half of this chain (whether --resume was
// actually emitted onto the subprocess argv). Without the line below, a log
// reader seeing no --resume cannot tell whether the harness supplied no id or
// the engine dropped one it was given. Together the two make the resume path
// reconstructible from ~/.ion/engine.log alone.
//
// ionSessionID is logged on the no-resume branch specifically because it is the
// id most likely to be misrouted here: it is Ion's conversation-file identity,
// NOT a Claude UUID, and `claude --resume <ion-id>` exits 1. Seeing it in the
// log next to an absent resume id makes that confusion diagnosable.
//
// Lives here alongside logDispatchWorkdir (rather than inline in
// dispatch_agent.go) to keep that file under the 800-line cap.
func logChildCliResume(cliResumeSessionID, ionSessionID, agentName, dispatchID string, depth int, sessionKey string) {
	if cliResumeSessionID != "" {
		utils.Log("Dispatch", fmt.Sprintf(
			"child cli resume: forwarding claude session uuid=%s agent=%q dispatchId=%s depth=%d session=%s",
			cliResumeSessionID, agentName, dispatchID, depth, sessionKey,
		))
		return
	}
	utils.Log("Dispatch", fmt.Sprintf(
		"child cli resume: none supplied, child starts a fresh claude session agent=%q dispatchId=%s depth=%d ionSessionId=%q session=%s",
		agentName, dispatchID, depth, ionSessionID, sessionKey,
	))
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
