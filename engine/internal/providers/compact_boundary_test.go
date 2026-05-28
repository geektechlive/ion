package providers

// compact_boundary_test.go pins the contract that the engine-internal
// `compact_boundary` LlmContentBlock variant — introduced in the
// gentle-knitting-cup compaction fix — must flatten to a wire-safe text
// block for every provider serializer.
//
// Why it matters: `compact_boundary` is a *structural marker* the
// runloop uses to anchor history slicing (see
// conversation.MessagesAfterLastCompactBoundary). Providers never see
// it on the wire — Anthropic rejects unknown content-block types, and
// OpenAI's content-part schema would silently drop it. The serializer
// must translate the typed marker into a normal text part carrying the
// rendered Summary.
//
// These tests are the live-API canary in unit-test form: they catch a
// regression where a future serializer refactor forgets the
// compact_boundary case and starts shipping malformed payloads to the
// provider.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dsswift/ion/engine/internal/types"
)

// boundaryBlock builds a compact_boundary content block carrying the
// given summary plus a representative spread of the structured metadata
// fields. The structured fields exist for engine-internal consumers
// (persistence, the runloop, the future renderer); the serializer
// tests assert they are NOT present on the wire after flattening.
func boundaryBlock(summary string) types.LlmContentBlock {
	return types.LlmContentBlock{
		Type:               "compact_boundary",
		Trigger:            "auto",
		MessagesSummarized: 12,
		MessagesBefore:     30,
		MessagesAfter:      19,
		ClearedBlocks:      4,
		TokensBefore:       180_000,
		Summary:            summary,
		FactCount:          5,
		RecentFiles:        []string{"/a.go", "/b.go"},
	}
}

// TestFormatAnthropicBlock_CompactBoundaryFlattensToText verifies the
// happy path: a populated compact_boundary block becomes a `{type:
// "text", text: <summary>}` map. No `trigger`, `clearedBlocks`,
// `recentFiles`, or any of the other structured metadata fields are
// allowed to leak onto the wire — they would be ignored at best and
// trigger an `invalid_request_error` at worst.
func TestFormatAnthropicBlock_CompactBoundaryFlattensToText(t *testing.T) {
	const summary = "## Decisions\n- Use Go\n\n## Files Modified\n- /src/main.go"

	out := formatAnthropicBlock(boundaryBlock(summary))
	if out == nil {
		t.Fatal("formatAnthropicBlock returned nil for compact_boundary")
	}

	if got, want := out["type"], "text"; got != want {
		t.Errorf("type = %v, want %q (boundary must flatten to text)", got, want)
	}
	if got, want := out["text"], summary; got != want {
		t.Errorf("text = %v, want %q (summary should pass through verbatim)", got, want)
	}

	// Whitelist what may appear on the wire. Any other key is a leak.
	allowed := map[string]struct{}{"type": {}, "text": {}}
	for k := range out {
		if _, ok := allowed[k]; !ok {
			t.Errorf("unexpected wire field %q on flattened boundary: %v", k, out[k])
		}
	}
}

// TestFormatAnthropicBlock_CompactBoundaryEmptySummaryFallback covers
// the defensive branch: a boundary with no rendered Summary still has
// to produce a non-empty text part because the Anthropic API rejects
// empty text blocks with `text content blocks must contain non-empty
// text`.
//
// The serializer substitutes a generic placeholder so the model still
// sees *some* marker that a compaction occurred, and the request stays
// well-formed.
func TestFormatAnthropicBlock_CompactBoundaryEmptySummaryFallback(t *testing.T) {
	out := formatAnthropicBlock(types.LlmContentBlock{Type: "compact_boundary"})
	if out == nil {
		t.Fatal("formatAnthropicBlock returned nil for empty boundary")
	}
	if got := out["type"]; got != "text" {
		t.Errorf("type = %v, want text", got)
	}
	text, _ := out["text"].(string)
	if text == "" {
		t.Error("text must be non-empty even when Summary is empty (Anthropic rejects empty text blocks)")
	}
}

// TestFormatAnthropicBlock_CompactBoundaryRoundTripsThroughJSON guards
// the actual wire format. The post-serialization map is the input to
// json.Marshal in the live request path — if json.Marshal can encode
// it and the result re-decodes to the same shape, the on-wire payload
// is well-formed.
func TestFormatAnthropicBlock_CompactBoundaryRoundTripsThroughJSON(t *testing.T) {
	out := formatAnthropicBlock(boundaryBlock("round-trip me"))

	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded["type"] != "text" {
		t.Errorf("decoded type = %v, want text", decoded["type"])
	}
	if decoded["text"] != "round-trip me" {
		t.Errorf("decoded text = %v, want round-trip me", decoded["text"])
	}
}

// TestFormatOpenAIMessages_CompactBoundaryEmitsTextPart pins the OpenAI
// translation for boundary blocks. The risk profile is different from
// Anthropic — OpenAI's user-message branch switches on block type and
// silently drops anything it doesn't recognise, so a missing case would
// erase the post-compaction summary from the model's view rather than
// fail loudly. This test makes the absence-bug detectable.
//
// Note: formatOpenAIMessages always prepends a system row (carrying the
// `system` argument, even when empty), so the user message lands at
// index 1, not 0.
func TestFormatOpenAIMessages_CompactBoundaryEmitsTextPart(t *testing.T) {
	const summary = "compaction summary"
	msgs := []types.LlmMessage{
		{
			Role:    "user",
			Content: []types.LlmContentBlock{boundaryBlock(summary)},
		},
	}

	out := formatOpenAIMessages("", msgs)
	// system row at [0], translated user message at [1].
	if len(out) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(out))
	}
	if out[0]["role"] != "system" {
		t.Errorf("out[0].role = %v, want system", out[0]["role"])
	}
	userMsg := out[1]
	if userMsg["role"] != "user" {
		t.Errorf("role = %v, want user", userMsg["role"])
	}

	parts, ok := userMsg["content"].([]map[string]any)
	if !ok {
		t.Fatalf("content type = %T, want []map[string]any", userMsg["content"])
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 content part, got %d (boundary should produce exactly one text part)", len(parts))
	}
	if parts[0]["type"] != "text" {
		t.Errorf("part type = %v, want text", parts[0]["type"])
	}
	if parts[0]["text"] != summary {
		t.Errorf("part text = %v, want %q", parts[0]["text"], summary)
	}
}

// TestFormatOpenAIMessages_CompactBoundaryEmptySummaryFallback mirrors
// the Anthropic empty-summary case for OpenAI. OpenAI accepts empty
// text parts so the strict requirement is weaker than Anthropic's, but
// the fallback exists to keep behaviour symmetric across providers.
func TestFormatOpenAIMessages_CompactBoundaryEmptySummaryFallback(t *testing.T) {
	msgs := []types.LlmMessage{
		{
			Role:    "user",
			Content: []types.LlmContentBlock{{Type: "compact_boundary"}},
		},
	}

	out := formatOpenAIMessages("", msgs)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(out))
	}
	userMsg := out[1]
	parts, _ := userMsg["content"].([]map[string]any)
	if len(parts) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(parts))
	}
	text, _ := parts[0]["text"].(string)
	if text == "" {
		t.Error("text fallback must be non-empty for symmetry with Anthropic path")
	}
}

// TestFormatAnthropicBlock_CompactBoundaryAfterJSONRoundTripStillFlattens
// catches a subtle production-only failure mode. Conversations are
// persisted as NDJSON and reloaded into LlmMessage.Content as
// []any of map[string]any (not the typed []LlmContentBlock the runloop
// builds). The provider sees the post-reload shape on the very next
// turn after a session resume.
//
// Without round-trip support the boundary would survive
// contentBlocks() → mapToContentBlock() (those copy the new
// metadata fields, verified by the existing TestBuildCompactBoundaryMessage_JSONRoundTrip)
// but the Summary field needs to actually arrive at formatAnthropicBlock.
// This test pins that wire of the pipeline end-to-end.
func TestFormatAnthropicBlock_CompactBoundaryAfterJSONRoundTripStillFlattens(t *testing.T) {
	const summary = "post-reload summary"
	original := types.LlmMessage{
		Role:    "user",
		Content: []types.LlmContentBlock{boundaryBlock(summary)},
	}

	// Encode + decode the way persistence does — the result is the
	// shape contentBlocks() sees on the first turn after a resume.
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded types.LlmMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Walk the same code path the live serializer uses.
	blocks := contentBlocks(decoded)
	if len(blocks) != 1 {
		t.Fatalf("contentBlocks: expected 1, got %d", len(blocks))
	}
	if blocks[0].Type != "compact_boundary" {
		t.Errorf("post-reload block type = %q, want compact_boundary", blocks[0].Type)
	}
	if blocks[0].Summary != summary {
		t.Errorf("post-reload Summary = %q, want %q", blocks[0].Summary, summary)
	}

	out := formatAnthropicBlock(blocks[0])
	if out == nil {
		t.Fatal("formatAnthropicBlock returned nil after JSON round-trip")
	}
	if out["type"] != "text" {
		t.Errorf("post-reload flattened type = %v, want text", out["type"])
	}
	if text, _ := out["text"].(string); !strings.Contains(text, summary) {
		t.Errorf("post-reload flattened text = %q, want it to contain %q", text, summary)
	}
}
