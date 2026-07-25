package extcontext

// dispatch_child_tool_allowlist_test.go pins that BuildChildToolServer scopes a
// dispatched child's MCP surface to its declared AllowedTools.
//
// Before this fix, every extension tool was registered on the child's ToolServer
// regardless of the agent spec's `tools:` frontmatter. The child could not call
// the extras (the backend's own allowlist filtered them at call time), but their
// schemas and alias-directive lines still occupied the child's system prompt.
//
// Primary revert-check: delete the filterToolsByAllowlist call in
// BuildChildToolServer and TestBuildChildToolServer_AllowlistScopesTools goes red
// with "queue_for_brief must not be registered".

import (
	"strings"
	"testing"

	"github.com/dsswift/ion/engine/internal/backend"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/types"
)

// namedTool returns a minimal ToolDefinition under the given name, so a test can
// assemble a multi-tool registration set cheaply.
func namedTool(name string) extension.ToolDefinition {
	return extension.ToolDefinition{
		Name:        name,
		Description: name + " description",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Execute: func(params interface{}, ctx *extension.Context) (*types.ToolResult, error) {
			return &types.ToolResult{Content: "ok"}, nil
		},
	}
}

// threeTools mirrors the shape of a real dispatch: the harness registers its
// whole tool set, while an agent spec names only a subset.
func threeTools() []extension.ToolDefinition {
	return []extension.ToolDefinition{
		surfaceToInboxTool(),
		namedTool("queue_for_brief"),
		namedTool("remember_fact"),
	}
}

// TestBuildChildToolServer_AllowlistScopesTools is the primary regression test.
// An agent spec naming two of three extension tools (plus built-ins, which match
// no extension tool) must produce an alias directive covering exactly those two.
//
// Revert-check: removing the filterToolsByAllowlist call makes the
// queue_for_brief assertion fail.
func TestBuildChildToolServer_AllowlistScopesTools(t *testing.T) {
	child := backend.NewCliBackend()
	sa := &depthTestAccessor{}
	runOpts := types.RunOptions{
		// Shaped like real `tools:` frontmatter: built-ins alongside bare
		// extension tool names.
		AllowedTools: []string{"Read", "Bash", "surface_to_inbox", "remember_fact"},
	}

	ts := BuildChildToolServer(child, threeTools(), sa, "dispatch-allowlist", 1, nil, &runOpts)
	if ts == nil {
		t.Fatal("expected non-nil ToolServer when the allowlist permits some extension tools")
	}
	t.Cleanup(ts.Stop)

	directive := runOpts.AppendSystemPrompt
	for _, kept := range []string{"surface_to_inbox", "remember_fact"} {
		if !strings.Contains(directive, kept) {
			t.Errorf("allowlisted tool %q must be registered; directive=%q", kept, directive)
		}
	}
	// The load-bearing assertion: an excluded tool must not reach the child's
	// system prompt. This is the token cost the fix removes.
	if strings.Contains(directive, "queue_for_brief") {
		t.Errorf("queue_for_brief must not be registered (absent from AllowedTools); directive=%q", directive)
	}
}

// TestBuildChildToolServer_EmptyAllowlistRegistersAll pins the documented
// "empty means no restriction" contract from buildDispatchRunOptions. This is
// the pre-existing behavior for callers that never set AllowedTools.
func TestBuildChildToolServer_EmptyAllowlistRegistersAll(t *testing.T) {
	child := backend.NewCliBackend()
	sa := &depthTestAccessor{}
	var runOpts types.RunOptions

	ts := BuildChildToolServer(child, threeTools(), sa, "dispatch-allowlist-empty", 1, nil, &runOpts)
	if ts == nil {
		t.Fatal("expected non-nil ToolServer for an unrestricted child")
	}
	t.Cleanup(ts.Stop)

	for _, name := range []string{"surface_to_inbox", "queue_for_brief", "remember_fact"} {
		if !strings.Contains(runOpts.AppendSystemPrompt, name) {
			t.Errorf("empty allowlist must register every tool; %q missing", name)
		}
	}
}

// TestBuildChildToolServer_WildcardRegistersAll pins that a caller may opt back
// into the full tool set explicitly, without reverting to an empty allowlist.
func TestBuildChildToolServer_WildcardRegistersAll(t *testing.T) {
	child := backend.NewCliBackend()
	sa := &depthTestAccessor{}
	runOpts := types.RunOptions{
		AllowedTools: []string{"Read", "mcp__" + backend.McpServerName + "__*"},
	}

	ts := BuildChildToolServer(child, threeTools(), sa, "dispatch-allowlist-wildcard", 1, nil, &runOpts)
	if ts == nil {
		t.Fatal("expected non-nil ToolServer for a wildcard allowlist")
	}
	t.Cleanup(ts.Stop)

	for _, name := range []string{"surface_to_inbox", "queue_for_brief", "remember_fact"} {
		if !strings.Contains(runOpts.AppendSystemPrompt, name) {
			t.Errorf("wildcard allowlist must register every tool; %q missing", name)
		}
	}
}

// TestBuildChildToolServer_PrefixedAllowlistMatches pins that the MCP-prefixed
// spelling is accepted, since that is the form the model actually calls and the
// form a caller may reasonably pass through.
func TestBuildChildToolServer_PrefixedAllowlistMatches(t *testing.T) {
	child := backend.NewCliBackend()
	sa := &depthTestAccessor{}
	runOpts := types.RunOptions{
		AllowedTools: []string{"mcp__" + backend.McpServerName + "__surface_to_inbox"},
	}

	ts := BuildChildToolServer(child, threeTools(), sa, "dispatch-allowlist-prefixed", 1, nil, &runOpts)
	if ts == nil {
		t.Fatal("expected non-nil ToolServer when a prefixed name is allowlisted")
	}
	t.Cleanup(ts.Stop)

	if !strings.Contains(runOpts.AppendSystemPrompt, "surface_to_inbox") {
		t.Errorf("prefixed allowlist entry must match its tool; directive=%q", runOpts.AppendSystemPrompt)
	}
	if strings.Contains(runOpts.AppendSystemPrompt, "queue_for_brief") {
		t.Errorf("queue_for_brief must not be registered; directive=%q", runOpts.AppendSystemPrompt)
	}
}

// TestBuildChildToolServer_AllowlistExcludesEverything pins the built-ins-only
// case: a spec naming no extension tools gets no MCP server at all, so the child
// spawns without --mcp-config and pays none of the schema cost.
func TestBuildChildToolServer_AllowlistExcludesEverything(t *testing.T) {
	child := backend.NewCliBackend()
	sa := &depthTestAccessor{}
	runOpts := types.RunOptions{
		AllowedTools: []string{"Read", "Glob", "Grep"},
	}

	ts := BuildChildToolServer(child, threeTools(), sa, "dispatch-allowlist-none", 1, nil, &runOpts)
	if ts != nil {
		t.Cleanup(ts.Stop)
		t.Fatal("expected nil ToolServer when the allowlist names no extension tool")
	}
	if runOpts.McpConfig != "" {
		t.Errorf("McpConfig must stay empty when no extension tool is registered, got %q", runOpts.McpConfig)
	}
	if runOpts.AppendSystemPrompt != "" {
		t.Errorf("no alias directive should be appended, got %q", runOpts.AppendSystemPrompt)
	}
}
