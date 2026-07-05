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
	if len(opts.FallbackChain) > 0 {
		// Walk these alternative models on overload (typically the tail of a
		// resolved tier chain). Empty leaves the child relying solely on the
		// DefaultModel threading for the unresolvable-model case.
		runOpts.FallbackChain = opts.FallbackChain
	}
	if opts.SessionID != "" {
		runOpts.SessionID = opts.SessionID
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
