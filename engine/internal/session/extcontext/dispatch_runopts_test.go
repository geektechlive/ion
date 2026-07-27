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

// TestBuildDispatchRunOptionsForwardsCliSettingSources pins the dispatch hop of
// the settings-source chain:
//
//	DispatchAgentOpts.CliSettingSources
//	  -> buildDispatchRunOptions
//	  -> RunOptions.CliSettingSources
//	  -> backend.cliSettingSourcesArgs -> `claude --setting-sources=<joined>`
//
// Without this hop the SDK field decodes correctly off the wire and then dies in
// the engine: the child spawns with Claude's full settings chain loaded no matter
// what the harness asked for, and no error surfaces anywhere.
//
// The backend half of the chain (CliSettingSources -> argv) is pinned by
// internal/backend/cli_backend_args_test.go; this covers the dispatch hop.
func TestBuildDispatchRunOptionsForwardsCliSettingSources(t *testing.T) {
	sources := []string{"project"}

	opts := extension.DispatchAgentOpts{
		Name:              "researcher",
		Task:              "summarize the findings",
		CliSettingSources: &sources,
	}

	runOpts := buildDispatchRunOptions(&opts, "claude-sonnet-4-6", "/tmp/project", context.Background())

	if runOpts.CliSettingSources == nil {
		t.Fatalf("RunOptions.CliSettingSources = nil, want %v (child would load claude's full settings chain)", sources)
	}
	if got := *runOpts.CliSettingSources; len(got) != 1 || got[0] != "project" {
		t.Errorf("RunOptions.CliSettingSources = %v, want %v", got, sources)
	}
}

// TestBuildDispatchRunOptionsForwardsEmptyCliSettingSources is the case the
// whole field exists for, and the one a length-based guard would silently break.
//
// An empty-but-non-nil slice means "load no settings scope at all" -- the
// instruction that strips the instruction-file chain, the agent-spec listing,
// and user-scope MCP declarations from the child's context. If the dispatch hop
// guarded on `len(opts.CliSettingSources) > 0` (the shape used by the
// AllowedTools and FallbackChain branches immediately above it), this
// instruction would be dropped on the floor and the child would spawn with
// everything loaded, exactly as though the harness had said nothing.
func TestBuildDispatchRunOptionsForwardsEmptyCliSettingSources(t *testing.T) {
	empty := []string{}

	opts := extension.DispatchAgentOpts{
		Name:              "researcher",
		Task:              "summarize the findings",
		CliSettingSources: &empty,
	}

	runOpts := buildDispatchRunOptions(&opts, "claude-sonnet-4-6", "/tmp/project", context.Background())

	if runOpts.CliSettingSources == nil {
		t.Fatalf("RunOptions.CliSettingSources = nil, want a non-nil empty slice -- " +
			"an explicit 'load nothing' was collapsed into 'unset', so the child " +
			"still loads claude's full settings chain")
	}
	if got := *runOpts.CliSettingSources; len(got) != 0 {
		t.Errorf("RunOptions.CliSettingSources = %v, want an empty slice", got)
	}
}

// TestBuildDispatchRunOptionsOmitsCliSettingSourcesWhenUnset pins the additive
// contract: a caller that never sets the field gets a nil pointer, which makes
// cliSettingSourcesArgs omit --setting-sources entirely, which is byte-for-byte
// the argv every existing caller produced before this field existed.
func TestBuildDispatchRunOptionsOmitsCliSettingSourcesWhenUnset(t *testing.T) {
	opts := extension.DispatchAgentOpts{
		Name: "researcher",
		Task: "summarize the findings",
	}

	runOpts := buildDispatchRunOptions(&opts, "claude-sonnet-4-6", "/tmp/project", context.Background())

	if runOpts.CliSettingSources != nil {
		t.Errorf("RunOptions.CliSettingSources = %v, want nil when the caller sets nothing", *runOpts.CliSettingSources)
	}
}

// TestDispatchOptsDecodeCliSettingSourcesFromRPC pins the wire half of the
// chain, and specifically the JSON behavior the pointer depends on.
//
// An SDK consumer never calls buildDispatchRunOptions directly: it calls
// ctx.dispatchAgent(opts) in TypeScript, the SDK sends the opts as JSON-RPC
// params, and the engine json.Unmarshals them into DispatchAgentOpts. Two
// distinct things have to hold for the feature to work end to end:
//
//  1. The Go struct tag names the same key the TS SDK emits (`cliSettingSources`).
//     Rename either side and the field decodes to nil -- the child spawns with
//     everything loaded, which is the original problem wearing a new hat.
//  2. `"cliSettingSources": []` decodes to a NON-NIL pointer to an empty slice,
//     not to nil. This is what lets an explicit "load nothing" survive the wire
//     at all. It is the single assumption the pointer design rests on, and it is
//     invisible in the Go-only tests above, so it is asserted here directly.
//
// The literal keys below are deliberately hardcoded rather than derived from the
// struct: they must name the same field as `cliSettingSources` in
// engine/extensions/sdk/ion-sdk/types.ts and docs/extensions/sdk-typescript.md.
func TestDispatchOptsDecodeCliSettingSourcesFromRPC(t *testing.T) {
	t.Run("empty array decodes to non-nil empty slice", func(t *testing.T) {
		const wireParams = `{
			"name": "researcher",
			"task": "summarize the findings",
			"cliSettingSources": []
		}`

		var opts extension.DispatchAgentOpts
		if err := json.Unmarshal([]byte(wireParams), &opts); err != nil {
			t.Fatalf("unmarshal DispatchAgentOpts: %v", err)
		}

		if opts.CliSettingSources == nil {
			t.Fatal("DispatchAgentOpts.CliSettingSources = nil for a `[]` wire value -- " +
				"an explicit 'load no settings scope' cannot survive the JSON-RPC boundary, " +
				"so the feature is a silent no-op for its primary use case")
		}
		if got := *opts.CliSettingSources; len(got) != 0 {
			t.Errorf("DispatchAgentOpts.CliSettingSources = %v, want an empty slice", got)
		}

		// And on through the dispatch hop to the value the CLI backend reads.
		runOpts := buildDispatchRunOptions(&opts, "claude-sonnet-4-6", "/tmp/project", context.Background())
		if runOpts.CliSettingSources == nil || len(*runOpts.CliSettingSources) != 0 {
			t.Errorf("RunOptions.CliSettingSources = %v, want a non-nil empty slice (wire -> run options round trip broken)",
				runOpts.CliSettingSources)
		}
	})

	t.Run("populated array decodes and forwards", func(t *testing.T) {
		const wireParams = `{
			"name": "researcher",
			"task": "summarize the findings",
			"cliSettingSources": ["user", "project"]
		}`

		var opts extension.DispatchAgentOpts
		if err := json.Unmarshal([]byte(wireParams), &opts); err != nil {
			t.Fatalf("unmarshal DispatchAgentOpts: %v", err)
		}

		if opts.CliSettingSources == nil {
			t.Fatal("DispatchAgentOpts.CliSettingSources = nil -- the Go struct tag does not " +
				"match the `cliSettingSources` key the TS SDK sends")
		}
		if got := *opts.CliSettingSources; len(got) != 2 || got[0] != "user" || got[1] != "project" {
			t.Errorf("DispatchAgentOpts.CliSettingSources = %v, want [user project]", got)
		}
	})

	t.Run("absent property decodes to nil", func(t *testing.T) {
		const wireParams = `{"name": "researcher", "task": "summarize the findings"}`

		var opts extension.DispatchAgentOpts
		if err := json.Unmarshal([]byte(wireParams), &opts); err != nil {
			t.Fatalf("unmarshal DispatchAgentOpts: %v", err)
		}

		if opts.CliSettingSources != nil {
			t.Errorf("DispatchAgentOpts.CliSettingSources = %v for an absent property, want nil -- "+
				"absent must stay distinguishable from an explicit empty array", *opts.CliSettingSources)
		}
	})
}
