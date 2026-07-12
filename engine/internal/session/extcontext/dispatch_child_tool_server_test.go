package extcontext

// dispatch_child_tool_server_test.go pins that BuildChildToolServer correctly
// wires a per-dispatch ToolServer for subprocess-backed child runs. These are
// the regression tests for issue #981 (dispatched agents receive empty
// McpConfig, making all harness-registered ion tools unresolvable).
//
// Revert-check: remove the `runOpts.McpConfig = mcpPath` assignment in
// BuildChildToolServer and TestBuildChildToolServer_SetsRunOptsMcpConfig goes
// red with "RunOptions.McpConfig must be non-empty after BuildChildToolServer".

import (
	"os"
	"strings"
	"testing"

	"github.com/dsswift/ion/engine/internal/backend"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/types"
)

// surfaceToInboxTool returns a ToolDefinition that represents the
// surface_to_inbox tool registered by the Jarvis harness. Used to verify that
// a real-world tool name passes through BuildChildToolServer correctly.
func surfaceToInboxTool() extension.ToolDefinition {
	return extension.ToolDefinition{
		Name:        "surface_to_inbox",
		Description: "Surface a message to the inbox for later review.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{"type": "string"},
			},
		},
		Execute: func(params interface{}, ctx *extension.Context) (*types.ToolResult, error) {
			return &types.ToolResult{Content: "ok"}, nil
		},
	}
}

// TestBuildChildToolServer_SetsRunOptsMcpConfig is the primary regression test
// for issue #981. It verifies that:
//   - BuildChildToolServer returns a non-nil ToolServer for a CliBackend child.
//   - RunOptions.McpConfig is non-empty after the call.
//   - The written config file references the ion-extensions MCP server name.
//   - The tool alias directive mentions the registered tool (surface_to_inbox).
//
// Revert-check: removing `runOpts.McpConfig = mcpPath` in BuildChildToolServer
// causes this test to fail at the McpConfig emptiness assertion.
func TestBuildChildToolServer_SetsRunOptsMcpConfig(t *testing.T) {
	child := backend.NewCliBackend()
	tools := []extension.ToolDefinition{surfaceToInboxTool()}
	sa := &depthTestAccessor{}
	var runOpts types.RunOptions

	ts := BuildChildToolServer(child, tools, sa, "dispatch-test-981", 1, nil, &runOpts)
	if ts == nil {
		t.Fatal("expected non-nil ToolServer for CliBackend child with tools")
	}
	t.Cleanup(ts.Stop)

	// Primary assertion: McpConfig must be set so the child subprocess receives
	// --mcp-config when spawned. This is the exact field that was always empty
	// before the fix (issue #981).
	if runOpts.McpConfig == "" {
		t.Fatal("RunOptions.McpConfig must be non-empty after BuildChildToolServer: issue #981")
	}

	// The MCP config file must exist on disk and name the ion-extensions server
	// so claude -p knows where to connect.
	data, err := os.ReadFile(runOpts.McpConfig)
	if err != nil {
		t.Fatalf("McpConfig file not readable at %q: %v", runOpts.McpConfig, err)
	}
	if !strings.Contains(string(data), backend.McpServerName) {
		t.Errorf("McpConfig file does not reference server %q; content: %s",
			backend.McpServerName, data)
	}

	// The tool alias directive must name surface_to_inbox so the model can call
	// it by bare name. Without this directive the model sees the tool only as
	// mcp__ion-extensions__surface_to_inbox and cannot match it to instructions
	// that reference the bare name.
	if !strings.Contains(runOpts.AppendSystemPrompt, "surface_to_inbox") {
		t.Errorf("tool alias directive should mention surface_to_inbox; "+
			"AppendSystemPrompt=%q", runOpts.AppendSystemPrompt)
	}
}

// TestBuildChildToolServer_NilForApiBackend verifies that in-process backends
// (ApiBackend) are correctly skipped: they execute tools directly without MCP
// and must not receive a ToolServer.
func TestBuildChildToolServer_NilForApiBackend(t *testing.T) {
	child := backend.NewApiBackend()
	tools := []extension.ToolDefinition{surfaceToInboxTool()}
	var runOpts types.RunOptions

	ts := BuildChildToolServer(child, tools, &depthTestAccessor{}, "dispatch-api-test", 1, nil, &runOpts)
	if ts != nil {
		ts.Stop()
		t.Fatal("expected nil ToolServer for ApiBackend child (in-process tool execution)")
	}
	if runOpts.McpConfig != "" {
		t.Errorf("McpConfig must remain empty for ApiBackend child; got %q", runOpts.McpConfig)
	}
}

// TestBuildChildToolServer_NilForEmptyTools verifies that no ToolServer is
// started when the child extension provides no tools, and that McpConfig
// remains empty so --mcp-config is not passed to the subprocess.
func TestBuildChildToolServer_NilForEmptyTools(t *testing.T) {
	child := backend.NewCliBackend()
	var runOpts types.RunOptions

	ts := BuildChildToolServer(child, nil, &depthTestAccessor{}, "dispatch-empty-test", 1, nil, &runOpts)
	if ts != nil {
		ts.Stop()
		t.Fatal("expected nil ToolServer when tool slice is empty")
	}
	if runOpts.McpConfig != "" {
		t.Errorf("McpConfig must remain empty when no tools; got %q", runOpts.McpConfig)
	}
}

// TestChildNeedsMcpToolServer_TypeSwitch verifies the CliBackend/CodexCliBackend
// detection that gates ToolServer creation. This is a direct unit test of the
// childNeedsMcpToolServer predicate so its logic is independently pinned.
func TestChildNeedsMcpToolServer_TypeSwitch(t *testing.T) {
	cases := []struct {
		name  string
		child backend.RunBackend
		want  bool
	}{
		{"CliBackend", backend.NewCliBackend(), true},
		{"ApiBackend", backend.NewApiBackend(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := childNeedsMcpToolServer(tc.child)
			if got != tc.want {
				t.Errorf("childNeedsMcpToolServer(%T) = %v, want %v", tc.child, got, tc.want)
			}
		})
	}
}
