package backend

import (
	"strings"

	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/internal/utils"
)

// cliSettingSourcesArgs returns the `--setting-sources` argument for a CLI run,
// or nil when the run must inherit claude's own default settings discovery.
//
// The three branches map opts.CliSettingSources (a *[]string — see that field's
// doc for why the pointer is load-bearing) onto argv:
//
//   - nil: returns nil. The flag is omitted and claude discovers settings as it
//     always has. This is the default every existing caller gets.
//   - non-nil, empty: returns {"--setting-sources="}. Claude loads no settings
//     scope at all, which drops the instruction-file chain, the agent-spec
//     listing, and user-scope MCP declarations from the subprocess context.
//   - non-nil, populated: returns {"--setting-sources=user,project"} etc.
//
// The value is deliberately emitted as a SINGLE argv element with an embedded
// `=`, not as the two-element pair {"--setting-sources", "<value>"}. exec.Command
// passes argv verbatim with no shell in between, and a flag parser handed an
// empty *following* argument can read the value as missing and consume the next
// flag instead. The `=` form makes the empty value unambiguous.
//
// Both branches log so the decision is reconstructible from ~/.ion/engine.log
// alone: an absent --setting-sources is otherwise indistinguishable between
// "the consumer asked for default discovery" and "the engine dropped a value it
// was handed".
//
// Note on the permission hook: opts.HookSettingsPath rides claude's separate
// `--settings <file>` flag, which is unaffected by this one. Restricting or
// emptying the source list does not disable ion's PreToolUse permission wiring.
func cliSettingSourcesArgs(opts types.RunOptions) []string {
	if opts.CliSettingSources == nil {
		utils.Log("CliBackend", "setting sources: omitting --setting-sources (unset; claude uses its default settings discovery)")
		return nil
	}
	joined := strings.Join(*opts.CliSettingSources, ",")
	if joined == "" {
		utils.Log("CliBackend", "setting sources: --setting-sources= (explicitly empty; claude loads no settings scope)")
	} else {
		utils.Log("CliBackend", "setting sources: --setting-sources="+joined)
	}
	return []string{"--setting-sources=" + joined}
}

// cliResumeArgs returns the `--resume <uuid>` argument pair for a CLI run,
// or nil when the run must start a fresh claude session.
//
// The resume id is sourced *only* from opts.CliResumeSessionID — the
// claude-native session UUID the manager captured from a previous run's
// SessionInitEvent/TaskCompleteEvent. It is never sourced from
// opts.SessionID (Ion's `{millis}-{12hex}` conversation-file identity),
// which the claude CLI rejects with exit code 1.
//
// Contract:
//   - First run of a session (CliResumeSessionID == ""): returns nil, so the
//     backend omits --resume and claude starts a fresh session.
//   - Subsequent runs (CliResumeSessionID set): returns {"--resume", "<uuid>"}.
//
// Both branches log so the resume decision is reconstructible from
// ~/.ion/engine.log alone.
func cliResumeArgs(opts types.RunOptions) []string {
	if opts.CliResumeSessionID != "" {
		utils.Log("CliBackend", "resume: --resume "+opts.CliResumeSessionID)
		return []string{"--resume", opts.CliResumeSessionID}
	}
	// First run of this session: no claude UUID captured yet. Omitting
	// --resume is mandatory — claude rejects a missing/invalid resume id.
	// SessionID (Ion's conversation id) is intentionally NOT used here.
	utils.Log("CliBackend", "resume: omitting --resume (first CLI run, no captured claude session UUID; sessionID="+opts.SessionID+")")
	return nil
}
