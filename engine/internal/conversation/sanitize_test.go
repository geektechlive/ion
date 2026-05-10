package conversation

import (
	"testing"

	"github.com/dsswift/ion/engine/internal/types"
)

// TestSanitize_NilToolUseInputCoerced ensures sanitize coerces a nil
// tool_use.Input to an empty map. Without this, a poisoned conversation
// would replay forever — the API rejects messages whose tool_use.input
// is not a JSON object.
func TestSanitize_NilToolUseInputCoerced(t *testing.T) {
	msgs := []types.LlmMessage{
		{
			Role: "user",
			Content: []types.LlmContentBlock{
				{Type: "text", Text: "hi"},
			},
		},
		{
			Role: "assistant",
			Content: []types.LlmContentBlock{
				{Type: "tool_use", ID: "tool_1", Name: "ops", Input: nil},
			},
		},
		{
			Role: "user",
			Content: []types.LlmContentBlock{
				{Type: "tool_result", ToolUseID: "tool_1", Content: "ok"},
			},
		},
	}

	out := SanitizeMessages(msgs)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}

	blocks, ok := out[1].Content.([]types.LlmContentBlock)
	if !ok {
		t.Fatalf("assistant content not block slice: %T", out[1].Content)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Input == nil {
		t.Fatalf("expected Input to be coerced to empty map, got nil")
	}
	if len(blocks[0].Input) != 0 {
		t.Fatalf("expected Input to be empty map, got %v", blocks[0].Input)
	}
}

// TestSanitize_ContentToBlockSliceNilInput ensures that a tool_use block
// loaded from JSON with a non-object input value (e.g. string, null,
// missing) is still coerced to an empty map by Pass 1.
func TestSanitize_ContentToBlockSliceNilInput(t *testing.T) {
	// Simulate JSON that round-tripped via []interface{} where the input
	// field was a string (not a map) — contentToBlockSlice drops it,
	// leaving Input nil. Sanitize must repair this before serialization.
	rawContent := []interface{}{
		map[string]interface{}{
			"type":  "tool_use",
			"id":    "tool_2",
			"name":  "ops",
			"input": "not-a-dict",
		},
	}
	msgs := []types.LlmMessage{
		{Role: "user", Content: []types.LlmContentBlock{{Type: "text", Text: "go"}}},
		{Role: "assistant", Content: rawContent},
		{Role: "user", Content: []types.LlmContentBlock{{Type: "tool_result", ToolUseID: "tool_2", Content: "done"}}},
	}

	out := SanitizeMessages(msgs)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	blocks, ok := out[1].Content.([]types.LlmContentBlock)
	if !ok {
		t.Fatalf("expected normalized block slice, got %T", out[1].Content)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Input == nil {
		t.Fatalf("expected Input to be coerced to empty map, got nil")
	}
}

// TestSanitize_ServerToolUsePairingKept ensures properly paired server_tool_use
// and web_search_tool_result blocks within the same assistant message are preserved.
func TestSanitize_ServerToolUsePairingKept(t *testing.T) {
	msgs := []types.LlmMessage{
		{Role: "user", Content: []types.LlmContentBlock{{Type: "text", Text: "search for Go testing"}}},
		{
			Role: "assistant",
			Content: []types.LlmContentBlock{
				{Type: "text", Text: "Let me search for that."},
				{Type: "server_tool_use", ID: "srvtoolu_1", Name: "web_search", Input: map[string]any{"query": "Go testing"}},
				{Type: "web_search_tool_result", ToolUseID: "srvtoolu_1", Content: `[{"type":"web_search_result"}]`},
				{Type: "text", Text: "Here are the results."},
			},
		},
	}

	out := SanitizeMessages(msgs)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	blocks, ok := out[1].Content.([]types.LlmContentBlock)
	if !ok {
		t.Fatalf("expected block slice, got %T", out[1].Content)
	}
	if len(blocks) != 4 {
		t.Fatalf("expected all 4 blocks preserved, got %d", len(blocks))
	}
	if blocks[1].Type != "server_tool_use" {
		t.Fatalf("expected server_tool_use at index 1, got %s", blocks[1].Type)
	}
	if blocks[2].Type != "web_search_tool_result" {
		t.Fatalf("expected web_search_tool_result at index 2, got %s", blocks[2].Type)
	}
}

// TestSanitize_OrphanedServerToolUseRemoved ensures a server_tool_use block
// without a matching web_search_tool_result is stripped, while other blocks are kept.
func TestSanitize_OrphanedServerToolUseRemoved(t *testing.T) {
	msgs := []types.LlmMessage{
		{Role: "user", Content: []types.LlmContentBlock{{Type: "text", Text: "search"}}},
		{
			Role: "assistant",
			Content: []types.LlmContentBlock{
				{Type: "text", Text: "Searching..."},
				{Type: "server_tool_use", ID: "srvtoolu_orphan", Name: "web_search", Input: map[string]any{"query": "test"}},
			},
		},
	}

	out := SanitizeMessages(msgs)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	blocks, ok := out[1].Content.([]types.LlmContentBlock)
	if !ok {
		t.Fatalf("expected block slice, got %T", out[1].Content)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block (text only), got %d", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Fatalf("expected text block, got %s", blocks[0].Type)
	}
}

// TestSanitize_OrphanedWebSearchResultRemoved ensures a web_search_tool_result
// block without a matching server_tool_use is stripped.
func TestSanitize_OrphanedWebSearchResultRemoved(t *testing.T) {
	msgs := []types.LlmMessage{
		{Role: "user", Content: []types.LlmContentBlock{{Type: "text", Text: "hi"}}},
		{
			Role: "assistant",
			Content: []types.LlmContentBlock{
				{Type: "text", Text: "Here you go."},
				{Type: "web_search_tool_result", ToolUseID: "srvtoolu_ghost", Content: `[{"type":"web_search_result"}]`},
			},
		},
	}

	out := SanitizeMessages(msgs)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	blocks, ok := out[1].Content.([]types.LlmContentBlock)
	if !ok {
		t.Fatalf("expected block slice, got %T", out[1].Content)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block (text only), got %d", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Fatalf("expected text block, got %s", blocks[0].Type)
	}
}

// TestSanitize_MixedClientAndServerTools reproduces the exact bug scenario:
// assistant message has tool_use (Agent) + server_tool_use (web_search, no result),
// followed by a user message with tool_result for the Agent call only.
// The orphaned server_tool_use must be stripped while the matched tool_use is kept.
func TestSanitize_MixedClientAndServerTools(t *testing.T) {
	msgs := []types.LlmMessage{
		{Role: "user", Content: []types.LlmContentBlock{{Type: "text", Text: "analyze this"}}},
		{
			Role: "assistant",
			Content: []types.LlmContentBlock{
				{Type: "text", Text: "I'll search and run an agent."},
				{Type: "tool_use", ID: "toolu_agent1", Name: "Agent", Input: map[string]any{"prompt": "do stuff"}},
				{Type: "server_tool_use", ID: "srvtoolu_ws1", Name: "web_search", Input: map[string]any{"query": "test"}},
			},
		},
		{
			Role: "user",
			Content: []types.LlmContentBlock{
				{Type: "tool_result", ToolUseID: "toolu_agent1", Content: "agent done"},
			},
		},
	}

	out := SanitizeMessages(msgs)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}

	// Check assistant message: should have text + tool_use, NO server_tool_use
	blocks, ok := out[1].Content.([]types.LlmContentBlock)
	if !ok {
		t.Fatalf("expected block slice, got %T", out[1].Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (text + tool_use), got %d", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Fatalf("expected text at index 0, got %s", blocks[0].Type)
	}
	if blocks[1].Type != "tool_use" {
		t.Fatalf("expected tool_use at index 1, got %s", blocks[1].Type)
	}

	// Check user message: tool_result should still be present
	userBlocks, ok := out[2].Content.([]types.LlmContentBlock)
	if !ok {
		t.Fatalf("expected block slice, got %T", out[2].Content)
	}
	if len(userBlocks) != 1 || userBlocks[0].Type != "tool_result" {
		t.Fatalf("expected tool_result in user message, got %v", userBlocks)
	}
}

// TestSanitize_ServerToolUseNilInputCoerced ensures server_tool_use with nil
// Input is coerced to an empty map, matching the behavior for tool_use.
func TestSanitize_ServerToolUseNilInputCoerced(t *testing.T) {
	msgs := []types.LlmMessage{
		{Role: "user", Content: []types.LlmContentBlock{{Type: "text", Text: "go"}}},
		{
			Role: "assistant",
			Content: []types.LlmContentBlock{
				{Type: "text", Text: "Searching."},
				{Type: "server_tool_use", ID: "srvtoolu_nil", Name: "web_search", Input: nil},
				{Type: "web_search_tool_result", ToolUseID: "srvtoolu_nil", Content: `[]`},
			},
		},
	}

	out := SanitizeMessages(msgs)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	blocks, ok := out[1].Content.([]types.LlmContentBlock)
	if !ok {
		t.Fatalf("expected block slice, got %T", out[1].Content)
	}

	// Find the server_tool_use block
	var found bool
	for _, b := range blocks {
		if b.Type == "server_tool_use" {
			found = true
			if b.Input == nil {
				t.Fatalf("expected Input to be coerced to empty map, got nil")
			}
			if len(b.Input) != 0 {
				t.Fatalf("expected empty map, got %v", b.Input)
			}
		}
	}
	if !found {
		t.Fatalf("server_tool_use block not found in output")
	}
}

// TestSanitize_OnlyOrphanedServerToolUseDropsMessage ensures an assistant
// message containing ONLY an orphaned server_tool_use is dropped entirely.
func TestSanitize_OnlyOrphanedServerToolUseDropsMessage(t *testing.T) {
	msgs := []types.LlmMessage{
		{Role: "user", Content: []types.LlmContentBlock{{Type: "text", Text: "search"}}},
		{
			Role: "assistant",
			Content: []types.LlmContentBlock{
				{Type: "server_tool_use", ID: "srvtoolu_only", Name: "web_search", Input: map[string]any{"query": "x"}},
			},
		},
		{Role: "user", Content: []types.LlmContentBlock{{Type: "text", Text: "hello"}}},
	}

	out := SanitizeMessages(msgs)
	// The assistant message should be dropped entirely since it becomes empty
	if len(out) != 2 {
		t.Fatalf("expected 2 messages (assistant dropped), got %d", len(out))
	}
	if out[0].Role != "user" || out[1].Role != "user" {
		t.Fatalf("expected both remaining messages to be user, got %s and %s", out[0].Role, out[1].Role)
	}
}
