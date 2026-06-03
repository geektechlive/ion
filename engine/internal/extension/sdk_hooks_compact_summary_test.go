package extension

import (
	"testing"

	"github.com/dsswift/ion/engine/internal/types"
)

// TestFireCompactSummaryRequest_ReturnShapes pins the three accepted
// handler return shapes for the compact_summary_request hook:
//
//  1. CompactSummaryRequestResult value (the canonical shape).
//  2. *CompactSummaryRequestResult pointer (mirrors Go convention for
//     optional results — caller may want to express "no value" by
//     returning nil instead of a zero struct).
//  3. bare string (the natural shape for a single-line summariser that
//     just returns its text).
//
// All three flow through the same first-non-empty selection in
// SDK.FireCompactSummaryRequest. Without this coverage the first
// extension that returns a bare string and finds it silently dropped
// has no test to point at.
func TestFireCompactSummaryRequest_ReturnShapes(t *testing.T) {
	cases := []struct {
		name        string
		handler     HookHandler
		wantSummary string
		wantOK      bool
	}{
		{
			name: "result_value",
			handler: func(ctx *Context, payload interface{}) (interface{}, error) {
				return CompactSummaryRequestResult{Summary: "value-shape summary"}, nil
			},
			wantSummary: "value-shape summary",
			wantOK:      true,
		},
		{
			name: "result_pointer",
			handler: func(ctx *Context, payload interface{}) (interface{}, error) {
				return &CompactSummaryRequestResult{Summary: "pointer-shape summary"}, nil
			},
			wantSummary: "pointer-shape summary",
			wantOK:      true,
		},
		{
			name: "bare_string",
			handler: func(ctx *Context, payload interface{}) (interface{}, error) {
				return "bare-string summary", nil
			},
			wantSummary: "bare-string summary",
			wantOK:      true,
		},
		{
			name: "result_value_empty_falls_through",
			handler: func(ctx *Context, payload interface{}) (interface{}, error) {
				return CompactSummaryRequestResult{Summary: ""}, nil
			},
			wantSummary: "",
			wantOK:      false,
		},
		{
			name: "result_pointer_nil_falls_through",
			handler: func(ctx *Context, payload interface{}) (interface{}, error) {
				var nilResult *CompactSummaryRequestResult
				return nilResult, nil
			},
			wantSummary: "",
			wantOK:      false,
		},
		{
			name: "bare_string_empty_falls_through",
			handler: func(ctx *Context, payload interface{}) (interface{}, error) {
				return "", nil
			},
			wantSummary: "",
			wantOK:      false,
		},
		{
			name: "nil_return_falls_through",
			handler: func(ctx *Context, payload interface{}) (interface{}, error) {
				return nil, nil
			},
			wantSummary: "",
			wantOK:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sdk := NewSDK()
			sdk.On(HookCompactSummaryRequest, tc.handler)

			info := CompactSummaryRequestInfo{
				Strategy:     "auto",
				MessageCount: 0,
				Messages:     []types.LlmMessage{},
			}
			summary, ok := sdk.FireCompactSummaryRequest(testCtx(), info)
			if summary != tc.wantSummary {
				t.Errorf("summary = %q, want %q", summary, tc.wantSummary)
			}
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

// TestFireCompactSummaryRequest_FirstNonEmptyWins pins the
// last-writer semantics documented in sdk_hooks_session.go: the SDK
// scans handlers in registration order and returns the first non-empty
// summary. A handler that returns ("", false) does NOT block a later
// handler that returns a real summary.
func TestFireCompactSummaryRequest_FirstNonEmptyWins(t *testing.T) {
	sdk := NewSDK()

	// First handler returns no opinion.
	sdk.On(HookCompactSummaryRequest, func(ctx *Context, payload interface{}) (interface{}, error) {
		return "", nil
	})
	// Second handler returns a real summary.
	sdk.On(HookCompactSummaryRequest, func(ctx *Context, payload interface{}) (interface{}, error) {
		return "from-second", nil
	})

	info := CompactSummaryRequestInfo{
		Strategy:     "reactive",
		MessageCount: 0,
		Messages:     []types.LlmMessage{},
	}
	summary, ok := sdk.FireCompactSummaryRequest(testCtx(), info)
	if summary != "from-second" {
		t.Errorf("summary = %q, want %q (second handler's non-empty return should win after first's empty fall-through)", summary, "from-second")
	}
	if !ok {
		t.Errorf("ok = false, want true")
	}
}

// TestFireCompactSummaryRequest_NoHandlers pins the zero-handler
// degenerate case: no handlers registered means ("", false), which the
// runloop reads as "fall back to the regex fact extractor". This is
// the default state when no extension wires the hook.
func TestFireCompactSummaryRequest_NoHandlers(t *testing.T) {
	sdk := NewSDK()

	info := CompactSummaryRequestInfo{
		Strategy:     "auto",
		MessageCount: 0,
		Messages:     []types.LlmMessage{},
	}
	summary, ok := sdk.FireCompactSummaryRequest(testCtx(), info)
	if summary != "" {
		t.Errorf("summary = %q, want empty (no handlers means no opinion)", summary)
	}
	if ok {
		t.Errorf("ok = true, want false (no handlers means no opinion)")
	}
}
