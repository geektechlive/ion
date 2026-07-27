package extcontext

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dsswift/ion/engine/internal/extension"
)

// TestBuildDispatchRunOptionsForwardsCliResumeSessionID pins the dispatch hop of
// the CLI resume chain:
//
//	DispatchAgentOpts.CliResumeSessionID
//	  -> buildDispatchRunOptions
//	  -> RunOptions.CliResumeSessionID
//	  -> backend.cliResumeArgs -> `claude --resume <uuid>`
//
// Before this field existed the chain was severed here: a harness could pass a
// child's prior Claude session UUID via DispatchAgentOpts.SessionID, but that
// field carries Ion's conversation-file identity and cliResumeArgs sources
// --resume *only* from RunOptions.CliResumeSessionID. Every dispatched child
// therefore spawned cold, permanently, with no harness-side workaround.
//
// The backend half of the chain (CliResumeSessionID -> --resume) is already
// pinned by internal/backend/cli_backend_args_test.go; this test covers the
// dispatch hop that was missing.
func TestBuildDispatchRunOptionsForwardsCliResumeSessionID(t *testing.T) {
	const claudeUUID = "11111111-2222-3333-4444-555555555555"
	const ionConvID = "1780093348767-c1c03e998388"

	opts := extension.DispatchAgentOpts{
		Name:               "researcher",
		Task:               "summarize the findings",
		SessionID:          ionConvID,
		CliResumeSessionID: claudeUUID,
	}

	runOpts := buildDispatchRunOptions(&opts, "claude-sonnet-4-6", "/tmp/project", context.Background())

	if runOpts.CliResumeSessionID != claudeUUID {
		t.Errorf("RunOptions.CliResumeSessionID = %q, want %q (child would spawn cold with no --resume)",
			runOpts.CliResumeSessionID, claudeUUID)
	}

	// The two ids occupy distinct identity spaces and must not bleed into each
	// other: Ion's conversation id passed to `claude --resume` exits 1.
	if runOpts.SessionID != ionConvID {
		t.Errorf("RunOptions.SessionID = %q, want %q (Ion conversation identity must be preserved separately)",
			runOpts.SessionID, ionConvID)
	}
}

// TestBuildDispatchRunOptionsOmitsCliResumeWhenUnset pins the additive contract:
// a caller that never sets the new field gets exactly the prior behavior, an
// empty CliResumeSessionID, which makes cliResumeArgs omit --resume so the child
// starts a fresh Claude session.
func TestBuildDispatchRunOptionsOmitsCliResumeWhenUnset(t *testing.T) {
	opts := extension.DispatchAgentOpts{
		Name:      "researcher",
		Task:      "summarize the findings",
		SessionID: "1780093348767-c1c03e998388",
	}

	runOpts := buildDispatchRunOptions(&opts, "claude-sonnet-4-6", "/tmp/project", context.Background())

	if runOpts.CliResumeSessionID != "" {
		t.Errorf("RunOptions.CliResumeSessionID = %q, want empty when the caller sets no resume id", runOpts.CliResumeSessionID)
	}
}

// TestDispatchOptsDecodeCliResumeSessionIDFromRPC pins the wire half of the
// chain. An SDK consumer never calls buildDispatchRunOptions directly: it calls
// ctx.dispatchAgent(opts) in TypeScript, the SDK sends the opts as JSON-RPC
// params, and the engine json.Unmarshals them into DispatchAgentOpts at
// extension.handleExtRequest ("ext/dispatch_agent"). The field therefore only
// reaches the child run if its Go struct tag names the same key the TS SDK
// emits. Rename one side and the field silently decodes to "" -- the child
// spawns cold, which is the original defect wearing a new hat.
//
// Scope of what this pins: encoding/json matches field names
// case-INsensitively, so this test does NOT catch a casing drift between the Go
// tag and the TS property (verified: changing the tag to "cliResumeSessionID"
// still decodes a "cliResumeSessionId" key). It catches a genuine rename, which
// is the failure mode that actually breaks the contract. Keeping the two spelled
// identically is a convention the reader upholds, not something this test can
// enforce.
//
// The literal key below is deliberately hardcoded rather than derived from the
// struct: it must name the same field as `cliResumeSessionId` in
// engine/extensions/sdk/ion-sdk/types.ts (DispatchAgentOpts) and in
// docs/extensions/sdk-typescript.md.
func TestDispatchOptsDecodeCliResumeSessionIDFromRPC(t *testing.T) {
	const claudeUUID = "11111111-2222-3333-4444-555555555555"
	const wireParams = `{
		"name": "researcher",
		"task": "now go deeper on the top three",
		"cliResumeSessionId": "11111111-2222-3333-4444-555555555555"
	}`

	var opts extension.DispatchAgentOpts
	if err := json.Unmarshal([]byte(wireParams), &opts); err != nil {
		t.Fatalf("unmarshal DispatchAgentOpts: %v", err)
	}

	if opts.CliResumeSessionID != claudeUUID {
		t.Fatalf("DispatchAgentOpts.CliResumeSessionID = %q, want %q -- the Go struct tag does not match the `cliResumeSessionId` key the TS SDK sends",
			opts.CliResumeSessionID, claudeUUID)
	}

	// And on through the dispatch hop to the value the CLI backend reads.
	runOpts := buildDispatchRunOptions(&opts, "claude-sonnet-4-6", "/tmp/project", context.Background())
	if runOpts.CliResumeSessionID != claudeUUID {
		t.Errorf("RunOptions.CliResumeSessionID = %q, want %q (wire -> run options round trip broken)",
			runOpts.CliResumeSessionID, claudeUUID)
	}
}
