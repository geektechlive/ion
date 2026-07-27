package extcontext

import (
	"context"

	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/types"
)

// buildDispatchRunOptions assembles the types.RunOptions for a dispatched
// child run from the dispatch opts, the resolved model, the project path, and
// the cancellation parent context. Extracted from BuildDispatchAgentFunc
// (dispatch_agent.go) to keep that file under the 800-line cap; the logic is
// otherwise identical to the inline assembly it replaced.
//
// dispatchParentCtx is the cancellation parent the child run derives from:
// opts.ParentCtx when the caller supplied one (e.g. the orchestrator's
// per-tool-call context), else the session cancellation root. Threading it
// here makes in-process backends (ApiBackend) cascade an abort; process-backed
// backends (CliBackend) are additionally reaped by PID kill in the manager's
// abortAllDescendants.
func buildDispatchRunOptions(opts *extension.DispatchAgentOpts, model, projectPath string, dispatchParentCtx context.Context) types.RunOptions {
	runOpts := types.RunOptions{
		Prompt:      opts.Task,
		Model:       model,
		ProjectPath: projectPath,
		ParentCtx:   dispatchParentCtx,
		// Mark every dispatched child as a subagent so the early-stop
		// continuation gate skips it by default. Dispatched agents have tight
		// remits and should not be poked to keep working after they stop. This
		// unifies the dispatch path with the orchestrator's Agent-tool path,
		// which has always set IsSubagent for the same reason; the two paths
		// now share this single dispatch implementation.
		IsSubagent: true,
	}
	if opts.SystemPrompt != "" {
		runOpts.AppendSystemPrompt = opts.SystemPrompt
	}
	if len(opts.AllowedTools) > 0 {
		// Scope the child to the caller-supplied tool allowlist (e.g. a
		// matched agent spec's declared tools). Empty means no restriction --
		// the child inherits the engine's default set.
		runOpts.AllowedTools = opts.AllowedTools
	}
	if len(opts.SuppressTools) > 0 {
		// Deny these tools to the child. This is the hop that was missing: a
		// root session gets SuppressTools populated from ctx.suppressTool() in
		// session.dispatchPrompt, but a dispatched child never passes through
		// that function -- startChild calls backend.StartRun directly -- so
		// before this field existed a child's SuppressTools was always empty and
		// no harness could deny it a built-in.
		//
		// Downstream: the CLI backend emits `--disallowedTools` from this slice
		// (backend.CliBackend.runProcess), which the claude CLI honors even under
		// --permission-mode bypassPermissions -- the mode every dispatched child
		// runs under, and the reason AllowedTools alone cannot deny a built-in.
		// The API backend filters these names out of the child's tool schema
		// (backend.buildToolDefs). Empty leaves both backends unrestricted,
		// exactly as before.
		runOpts.SuppressTools = opts.SuppressTools
	}
	if len(opts.FallbackChain) > 0 {
		// Walk these alternative models on overload (typically the tail of a
		// resolved tier chain). Empty leaves the child relying solely on the
		// DefaultModel threading for the unresolvable-model case.
		runOpts.FallbackChain = opts.FallbackChain
	}
	if opts.SessionID != "" {
		runOpts.SessionID = opts.SessionID
	}
	if opts.CliResumeSessionID != "" {
		// Claude's own session UUID, a different identity space from SessionID
		// above. The CLI backend turns this into `claude --resume <uuid>` for the
		// child subprocess (backend.cliResumeArgs); the API backend ignores it.
		// Empty leaves the child spawning a fresh Claude session -- the behavior
		// every caller had before this field existed.
		runOpts.CliResumeSessionID = opts.CliResumeSessionID
	}
	if opts.MaxTurns > 0 {
		runOpts.MaxTurns = opts.MaxTurns
	}
	if opts.PlanMode {
		runOpts.PlanMode = true
		if opts.PlanFilePath != "" {
			runOpts.PlanFilePath = opts.PlanFilePath
		}
		if len(opts.PlanModeTools) > 0 {
			runOpts.PlanModeTools = opts.PlanModeTools
		}
	}
	return runOpts
}
