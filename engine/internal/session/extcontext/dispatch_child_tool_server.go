package extcontext

// dispatch_child_tool_server.go wires a per-dispatch ToolServer so that
// subprocess-backed child runs (CliBackend, CodexCliBackend) can call
// extension-registered tools via --mcp-config. This is the dispatch-path
// counterpart of session.Manager.wireToolServer (prompt_cli_hooks.go), which
// wires the root-session ToolServer from within the session package.
//
// Why a separate file: the session package imports extcontext (to build
// extension.Context values), so extcontext cannot import session without
// creating a circular dependency. The shared logic is maintained in parallel:
// buildChildToolAliasDirective and appendChildToolAliasDirective mirror
// their counterparts in session/prompt_cli_hooks.go — any change to the alias
// directive format must be applied to both. This is intentional; it is cheaper
// than restructuring the package graph.

import (
	"fmt"
	"strings"

	"github.com/dsswift/ion/engine/internal/backend"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/internal/utils"
)

// childNeedsMcpToolServer reports whether the given child backend requires a
// ToolServer (MCP socket) to expose extension tools. Subprocess-backed
// backends load tools via --mcp-config; in-process backends (ApiBackend) call
// tool handlers directly and do not need one.
func childNeedsMcpToolServer(child backend.RunBackend) bool {
	switch child.(type) {
	case *backend.CliBackend, *backend.CodexCliBackend:
		return true
	}
	return false
}

// BuildChildToolServer starts a ToolServer for a dispatched child subprocess
// run and sets runOpts.McpConfig so the child backend passes --mcp-config when
// spawning the claude subprocess. Returns the started *ToolServer on success;
// the caller is responsible for calling Stop() when the child run exits.
//
// Returns nil without modifying runOpts when:
//   - the child does not need MCP wiring (ApiBackend, test stubs), or
//   - tools is empty (extension provides no tools for this child session).
//
// This function is the root-cause fix for issue #981: before this wiring,
// every dispatched staff/goal agent received an empty McpConfig, making every
// harness-registered tool (surface_to_inbox, remember_fact, calendar tools,
// etc.) structurally unresolvable even when correctly listed in --allowedTools
// and registered by child-extension.ts.
func BuildChildToolServer(
	child backend.RunBackend,
	tools []extension.ToolDefinition,
	sa SessionAccessor,
	dispatchID string,
	childDepth int,
	registry *DispatchRegistry,
	runOpts *types.RunOptions,
) *backend.ToolServer {
	if !childNeedsMcpToolServer(child) {
		utils.Debug("Dispatch", fmt.Sprintf(
			"child ToolServer skipped (non-subprocess backend) dispatchId=%s", dispatchID))
		return nil
	}
	if len(tools) == 0 {
		utils.Debug("Dispatch", fmt.Sprintf(
			"child ToolServer skipped (no tools registered) dispatchId=%s", dispatchID))
		return nil
	}

	ts := backend.NewToolServer(dispatchID)
	for _, tool := range tools {
		capturedTool := tool
		handler := func(input map[string]interface{}) (*types.ToolResult, error) {
			ctx := NewExtContext(sa, ExtContextOpts{
				Depth:      childDepth,
				DispatchId: dispatchID,
				Registry:   registry,
			})
			return capturedTool.Execute(input, ctx)
		}
		ts.RegisterTool(capturedTool.Name, handler, capturedTool.Description, capturedTool.Parameters)
	}

	if err := ts.Start(); err != nil {
		utils.Log("Dispatch", fmt.Sprintf(
			"child ToolServer start failed dispatchId=%s: %v", dispatchID, err))
		return nil
	}

	mcpPath, err := ts.McpConfigPath(dispatchID)
	if err != nil {
		utils.Log("Dispatch", fmt.Sprintf(
			"child ToolServer MCP config failed dispatchId=%s: %v", dispatchID, err))
		ts.Stop()
		return nil
	}

	runOpts.McpConfig = mcpPath

	// Append the tool-alias directive so the model can invoke tools by their
	// bare names (e.g. "surface_to_inbox") rather than the full MCP-prefixed
	// form ("mcp__ion-extensions__surface_to_inbox"). Mirrors the directive
	// appended by session.Manager.wireToolServer for the root session.
	bareNames := make([]string, len(tools))
	for i, t := range tools {
		bareNames[i] = t.Name
	}
	directive := buildChildToolAliasDirective(bareNames)
	appendChildToolAliasDirective(runOpts, directive, bareNames)

	utils.Log("Dispatch", fmt.Sprintf(
		"child ToolServer started (%d tools) dispatchId=%s mcpConfig=%s",
		len(tools), dispatchID, mcpPath))
	return ts
}

// buildChildToolAliasDirective renders the system-prompt directive that maps
// bare extension tool names to their MCP-prefixed forms for a child session.
// Mirrors session.buildToolAliasDirective (prompt_cli_hooks.go); see package
// comment for why these are maintained in parallel.
func buildChildToolAliasDirective(bareNames []string) string {
	if len(bareNames) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Tool name aliases: when your instructions reference a bare tool name, " +
		"it is the same tool exposed under the MCP-prefixed name. " +
		"Use the prefixed name when calling the tool.")
	for _, name := range bareNames {
		fmt.Fprintf(&b, "\n- %s = mcp__%s__%s", name, backend.McpServerName, name)
	}
	return b.String()
}

// appendChildToolAliasDirective appends a non-empty tool-alias directive to
// runOpts.AppendSystemPrompt. Mirrors session.appendDirective
// (prompt_cli_hooks.go); see package comment for why these are maintained
// in parallel.
func appendChildToolAliasDirective(runOpts *types.RunOptions, directive string, names []string) {
	if directive == "" {
		utils.Log("Dispatch", "child tool alias directive skipped (no tools)")
		return
	}
	if runOpts.AppendSystemPrompt != "" {
		runOpts.AppendSystemPrompt += "\n\n"
	}
	runOpts.AppendSystemPrompt += directive
	utils.Log("Dispatch", fmt.Sprintf(
		"child tool alias directive built (%d tools: %s)",
		len(names), strings.Join(names, ", ")))
}
