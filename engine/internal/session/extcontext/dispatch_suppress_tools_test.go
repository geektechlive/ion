package extcontext

// dispatch_suppress_tools_test.go pins the dispatched-child tool-suppression
// chain:
//
//	DispatchAgentOpts.SuppressTools        (SDK / JSON-RPC input)
//	  -> buildDispatchRunOptions           (dispatch_runopts.go)
//	  -> RunOptions.SuppressTools
//	  -> startChild -> backend.StartRun    (dispatch_child_setup.go)
//	  -> `claude --disallowedTools a,b,c`  (CliBackend.runProcess)
//
// Before this field existed the chain was severed at the second hop. A root
// session gets RunOptions.SuppressTools populated from ctx.suppressTool() in
// session.dispatchPrompt, but a dispatched child never passes through that
// function -- startChild calls backend.StartRun directly. So a child's
// SuppressTools was unconditionally empty and no harness could deny a built-in
// to a dispatched agent, with no workaround available on the harness side.
//
// Why that mattered: every dispatched child spawns with --permission-mode
// bypassPermissions, under which the claude CLI treats --allowedTools as
// advisory for built-ins. Verified empirically on Claude Code 2.1.220 -- a child
// given AllowedTools{"Read","Glob","Grep"} still executed Bash. --disallowedTools
// is honored regardless of permission mode, which makes SuppressTools the only
// field that can actually deny a built-in to a dispatched child.
//
// Primary revert-check: delete the `if len(opts.SuppressTools) > 0 { ... }`
// block in buildDispatchRunOptions and
// TestBuildDispatchRunOptions_ForwardsSuppressTools goes red. (Deleting the
// struct field instead produces a compile error, not a red test.)

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/types"
)

// TestBuildDispatchRunOptions_ForwardsSuppressTools is the primary regression
// test: it covers the exact hop that was missing.
func TestBuildDispatchRunOptions_ForwardsSuppressTools(t *testing.T) {
	denied := []string{"Bash", "Write", "Edit"}

	opts := extension.DispatchAgentOpts{
		Name:          "researcher",
		Task:          "summarize the findings",
		SuppressTools: denied,
	}

	runOpts := buildDispatchRunOptions(&opts, "claude-sonnet-4-6", "/tmp/project", context.Background())

	if !reflect.DeepEqual(runOpts.SuppressTools, denied) {
		t.Errorf("RunOptions.SuppressTools = %v, want %v -- the child would spawn with no --disallowedTools and keep every built-in",
			runOpts.SuppressTools, denied)
	}
}

// TestBuildDispatchRunOptions_OmitsSuppressToolsWhenUnset pins the additive
// contract: a caller that never sets the field gets exactly the prior behavior,
// an empty denylist, so CliBackend.runProcess omits --disallowedTools entirely
// and the child keeps every tool its backend offers.
func TestBuildDispatchRunOptions_OmitsSuppressToolsWhenUnset(t *testing.T) {
	opts := extension.DispatchAgentOpts{
		Name: "researcher",
		Task: "summarize the findings",
	}

	runOpts := buildDispatchRunOptions(&opts, "claude-sonnet-4-6", "/tmp/project", context.Background())

	if len(runOpts.SuppressTools) != 0 {
		t.Errorf("RunOptions.SuppressTools = %v, want empty when the caller suppresses nothing", runOpts.SuppressTools)
	}
}

// TestBuildDispatchRunOptions_SuppressAndAllowlistCoexist pins the precedence
// contract at the layer where it is actually decided.
//
// The engine does NOT reconcile the two lists: it forwards both verbatim and
// lets the backend apply them. That is deliberate, and this test pins it,
// because a "helpful" reconciliation here (e.g. subtracting SuppressTools from
// AllowedTools, or dropping a name found in both) would move a policy decision
// into the engine and change what the backend sees.
//
// The resulting behavior, verified on Claude Code 2.1.220, is deny-wins: a tool
// named in both lists is withheld from the child's schema entirely -- a child
// spawned with `--allowedTools Bash --disallowedTools Bash` could not call Bash
// and did not even see it offered. The API backend reaches the same verdict by a
// different route: buildToolDefs applies the allowlist filter first and the
// suppression filter second, so suppression always has the last word.
func TestBuildDispatchRunOptions_SuppressAndAllowlistCoexist(t *testing.T) {
	allowed := []string{"Read", "Glob", "Bash"}
	denied := []string{"Bash"}

	opts := extension.DispatchAgentOpts{
		Name:          "researcher",
		Task:          "summarize the findings",
		AllowedTools:  allowed,
		SuppressTools: denied,
	}

	runOpts := buildDispatchRunOptions(&opts, "claude-sonnet-4-6", "/tmp/project", context.Background())

	if !reflect.DeepEqual(runOpts.AllowedTools, allowed) {
		t.Errorf("RunOptions.AllowedTools = %v, want %v -- the engine must not rewrite the allowlist to reconcile it with the denylist",
			runOpts.AllowedTools, allowed)
	}
	if !reflect.DeepEqual(runOpts.SuppressTools, denied) {
		t.Errorf("RunOptions.SuppressTools = %v, want %v -- a name present in AllowedTools must still reach the denylist",
			runOpts.SuppressTools, denied)
	}
}

// TestDispatchOptsDecodeSuppressToolsFromRPC pins the wire half of the chain.
//
// An SDK consumer never calls buildDispatchRunOptions directly: it calls
// ctx.dispatchAgent(opts) in TypeScript, the SDK sends the opts as JSON-RPC
// params, and the engine json.Unmarshals them into DispatchAgentOpts at
// extension.handleExtRequest ("ext/dispatch_agent"). The field therefore only
// reaches the child if its Go struct tag names the same key the TS SDK emits.
// Rename one side and the denylist silently decodes to nil -- the child spawns
// with every tool, which is the original defect wearing a new hat, and it fails
// open rather than closed.
//
// The literal key below is deliberately hardcoded rather than derived from the
// struct: it must name the same field as `suppressTools` in
// engine/extensions/sdk/ion-sdk/types.ts (DispatchAgentOpts) and in
// docs/extensions/sdk-typescript.md.
func TestDispatchOptsDecodeSuppressToolsFromRPC(t *testing.T) {
	const wireParams = `{
		"name": "researcher",
		"task": "summarize these files",
		"allowedTools": ["Read", "Glob", "Grep"],
		"suppressTools": ["Bash", "Write", "Edit", "NotebookEdit"]
	}`

	var opts extension.DispatchAgentOpts
	if err := json.Unmarshal([]byte(wireParams), &opts); err != nil {
		t.Fatalf("unmarshal DispatchAgentOpts: %v", err)
	}

	want := []string{"Bash", "Write", "Edit", "NotebookEdit"}
	if !reflect.DeepEqual(opts.SuppressTools, want) {
		t.Fatalf("DispatchAgentOpts.SuppressTools = %v, want %v -- the Go struct tag does not match the `suppressTools` key the TS SDK sends",
			opts.SuppressTools, want)
	}

	// And on through the dispatch hop to the value the backend reads.
	runOpts := buildDispatchRunOptions(&opts, "claude-sonnet-4-6", "/tmp/project", context.Background())
	if !reflect.DeepEqual(runOpts.SuppressTools, want) {
		t.Errorf("RunOptions.SuppressTools = %v, want %v (wire -> run options round trip broken)",
			runOpts.SuppressTools, want)
	}
}

// --- final hop: startChild -> backend.StartRun ---

// suppressCapturingBackend records the RunOptions handed to StartRun so a test
// can assert on what the backend actually received, rather than on what the
// dispatch layer intended to send.
type suppressCapturingBackend struct {
	mu       sync.Mutex
	captured types.RunOptions
	started  bool
}

func (d *suppressCapturingBackend) OnNormalized(func(string, types.NormalizedEvent)) {}
func (d *suppressCapturingBackend) OnExit(func(string, *int, *string, string))       {}
func (d *suppressCapturingBackend) OnError(func(string, error))                      {}
func (d *suppressCapturingBackend) Cancel(string) bool                               { return false }
func (d *suppressCapturingBackend) IsRunning(string) bool                            { return false }
func (d *suppressCapturingBackend) WriteToStdin(string, interface{}) error           { return nil }
func (d *suppressCapturingBackend) FlushConversations()                              {}

func (d *suppressCapturingBackend) StartRun(_ string, opts types.RunOptions) {
	d.mu.Lock()
	d.captured = opts
	d.started = true
	d.mu.Unlock()
}

// TestStartChild_ForwardsSuppressToolsToBackend pins the last hop of the chain.
//
// The earlier tests stop at buildDispatchRunOptions, which proves intent but not
// delivery: between assembly and StartRun, dispatch_agent.go hands &runOpts to
// BuildChildToolServer, which mutates the same struct (it writes McpConfig and
// AppendSystemPrompt, and reads AllowedTools). A future change there that
// rewrote the tool fields would sever the chain downstream of every assertion
// above while leaving them all green. This test closes that gap by asserting on
// the RunOptions the backend actually receives.
func TestStartChild_ForwardsSuppressToolsToBackend(t *testing.T) {
	denied := []string{"Bash", "Write"}
	child := &suppressCapturingBackend{}

	runOpts := types.RunOptions{
		Prompt:        "summarize the findings",
		Model:         "claude-sonnet-4-6",
		AllowedTools:  []string{"Read", "Glob", "Grep"},
		SuppressTools: denied,
	}

	startChild(child, "req-suppress-1", runOpts, nil)

	child.mu.Lock()
	defer child.mu.Unlock()

	if !child.started {
		t.Fatal("startChild did not reach the backend's StartRun")
	}
	if !reflect.DeepEqual(child.captured.SuppressTools, denied) {
		t.Errorf("backend received SuppressTools = %v, want %v -- the denylist did not survive the final dispatch hop",
			child.captured.SuppressTools, denied)
	}
}
