package backend

import (
	"context"
	"testing"

	"github.com/dsswift/ion/engine/internal/types"
)

// Some OpenAI-compatible providers (observed: gpt-4o-mini via OpenRouter) emit
// content_block_stop more than once for a single tool call -- a trailing
// finish_reason chunk produces a second stop. The first stop parses the
// streamed arguments and resets the buffer; the second saw raw=="" and used to
// overwrite the input with {}, making every tool call arrive empty
// ("url is required") and looping the agent. processStream must preserve the
// already-parsed input on the duplicate stop.
func TestProcessStreamDuplicateStopPreservesToolInput(t *testing.T) {
	b := NewApiBackend()
	run := &activeRun{requestID: "t-run"}

	stopReason := "tool_use"
	seq := []types.LlmStreamEvent{
		{Type: "message_start", MessageInfo: &types.LlmStreamMessageInfo{ID: "m1"}},
		{Type: "content_block_start", BlockIndex: 0, ContentBlock: &types.LlmStreamContentBlock{Type: "tool_use", ID: "call_1", Name: "WebFetch"}},
		{Type: "content_block_delta", BlockIndex: 0, Delta: &types.LlmStreamDelta{Type: "input_json_delta", PartialJSON: `{"url":`}},
		{Type: "content_block_delta", BlockIndex: 0, Delta: &types.LlmStreamDelta{Type: "input_json_delta", PartialJSON: `"https://example.com"}`}},
		{Type: "content_block_stop", BlockIndex: 0}, // 1st stop: parses the URL
		{Type: "content_block_stop", BlockIndex: 0}, // 2nd (duplicate) stop: must NOT clobber
		{Type: "message_delta", Delta: &types.LlmStreamDelta{Type: "message_delta", StopReason: &stopReason}},
	}

	events := make(chan types.LlmStreamEvent, len(seq))
	for _, ev := range seq {
		events <- ev
	}
	close(events)
	errc := make(chan error, 1)
	close(errc)

	blocks, _, _, err := b.processStream(context.Background(), run, events, errc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) == 0 {
		t.Fatal("expected an assistant block")
	}
	if blocks[0].Input == nil {
		t.Fatal("tool input is nil (block never finalized)")
	}
	got, ok := blocks[0].Input["url"].(string)
	if !ok || got != "https://example.com" {
		t.Fatalf("tool input clobbered by duplicate stop: got Input=%v, want url=https://example.com", blocks[0].Input)
	}
}
