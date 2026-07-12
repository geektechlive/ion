package extcontext

// dispatch_child_tool_server_test.go pins that BuildChildToolServer correctly
// wires a per-dispatch ToolServer for subprocess-backed child runs. These are
// the regression tests for issue #981 (dispatched agents receive empty
// McpConfig, making all harness-registered ion tools unresolvable).
//
// Primary revert-check: remove the `runOpts.McpConfig = mcpPath` assignment in
// BuildChildToolServer and TestBuildChildToolServer_SetsRunOptsMcpConfig goes
// red with "RunOptions.McpConfig must be non-empty after BuildChildToolServer".
//
// Hybrid revert-check: remove the *backend.HybridBackend case from
// childNeedsMcpToolServer (restoring "return false" for hybrid children) and
// TestBuildChildToolServer_HybridClaude goes red because McpConfig stays empty
// even though the claude-sonnet-4-6 model routes to the inner CliBackend.

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
		model string
		want  bool
	}{
		{"CliBackend", backend.NewCliBackend(), "", true},
		{"ApiBackend", backend.NewApiBackend(), "", false},
		// HybridBackend with an anthropic model routes to the inner CliBackend
		// (subprocess) so MCP wiring is required.
		{"HybridBackend/claude-sonnet-4-6", backend.NewHybridBackend(), "claude-sonnet-4-6", true},
		// HybridBackend with an unknown model routes to the inner ApiBackend
		// (in-process) so no MCP wiring is needed.
		{"HybridBackend/unknown-model", backend.NewHybridBackend(), "unknown-test-model", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := childNeedsMcpToolServer(tc.child, tc.model)
			if got != tc.want {
				t.Errorf("childNeedsMcpToolServer(%T, %q) = %v, want %v",
					tc.child, tc.model, got, tc.want)
			}
		})
	}
}

// TestBuildChildToolServer_HybridClaude is the primary regression test for
// the hybrid-backend case of issue #981. It verifies that a HybridBackend
// child whose model routes to the inner CliBackend (claude-sonnet-4-6 →
// providerID="anthropic" → *CliBackend) receives a non-empty McpConfig.
//
// This is the production configuration on the Mini: engine.json sets
// backend:"hybrid" and defaultModel:"claude-sonnet-4-6". Before the fix,
// childNeedsMcpToolServer returned false for all HybridBackend children,
// so every dispatched staff/goal agent on the Mini got an empty McpConfig
// and could not resolve any harness-registered ion tool.
//
// Revert-check: remove the *backend.HybridBackend case from
// childNeedsMcpToolServer and this test fails at the McpConfig emptiness
// assertion.
func TestBuildChildToolServer_HybridClaude(t *testing.T) {
	child := backend.NewHybridBackend()
	tools := []extension.ToolDefinition{surfaceToInboxTool()}
	sa := &depthTestAccessor{}
	runOpts := types.RunOptions{Model: "claude-sonnet-4-6"}

	ts := BuildChildToolServer(child, tools, sa, "dispatch-hybrid-claude-test", 1, nil, &runOpts)
	if ts == nil {
		t.Fatal("expected non-nil ToolServer for HybridBackend child with claude-sonnet-4-6 " +
			"(routes to inner CliBackend, needs MCP): issue #981 hybrid case")
	}
	t.Cleanup(ts.Stop)

	if runOpts.McpConfig == "" {
		t.Fatal("RunOptions.McpConfig must be non-empty for HybridBackend/claude-sonnet-4-6 " +
			"child: issue #981 hybrid case")
	}

	// Verify the MCP config file exists and references the server name.
	data, err := os.ReadFile(runOpts.McpConfig)
	if err != nil {
		t.Fatalf("McpConfig file not readable at %q: %v", runOpts.McpConfig, err)
	}
	if !strings.Contains(string(data), backend.McpServerName) {
		t.Errorf("McpConfig does not reference server %q; content: %s",
			backend.McpServerName, data)
	}
}

// TestBuildChildToolServer_HybridApiModel verifies that a HybridBackend child
// whose model routes to the inner ApiBackend (in-process tool execution) does
// not receive a ToolServer.
func TestBuildChildToolServer_HybridApiModel(t *testing.T) {
	child := backend.NewHybridBackend()
	tools := []extension.ToolDefinition{surfaceToInboxTool()}
	runOpts := types.RunOptions{Model: "unknown-test-model"}

	ts := BuildChildToolServer(child, tools, &depthTestAccessor{},
		"dispatch-hybrid-api-test", 1, nil, &runOpts)
	if ts != nil {
		ts.Stop()
		t.Fatal("expected nil ToolServer for HybridBackend child with unknown model " +
			"(routes to inner ApiBackend, no MCP needed)")
	}
	if runOpts.McpConfig != "" {
		t.Errorf("McpConfig must remain empty for HybridBackend/ApiBackend route; got %q",
			runOpts.McpConfig)
	}
}
