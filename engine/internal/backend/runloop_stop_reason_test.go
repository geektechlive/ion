package backend

import (
	"testing"

	"github.com/dsswift/ion/engine/internal/types"
)

// A bare "error" stop reason reaching the run loop's switch must surface as a
// real failure: an ErrorEvent plus a non-zero exit, never a silent code-0
// success (which would be indistinguishable from a staffer with nothing to
// say).
func TestHandleErrorStopReasonSurfacesFailure(t *testing.T) {
	b := NewApiBackend()

	var gotEvent *types.ErrorEvent
	b.OnNormalized(func(_ string, ev types.NormalizedEvent) {
		if ee, ok := ev.Data.(*types.ErrorEvent); ok {
			gotEvent = ee
		}
	})
	var exitCode *int
	b.OnExit(func(_ string, code *int, _ *string, _ string) {
		exitCode = code
	})

	run := &activeRun{requestID: "t-run"}
	handled := b.handleErrorStopReason(run, "conv-1", "error", 2)

	if !handled {
		t.Fatal("expected handleErrorStopReason to handle an \"error\" stop reason")
	}
	if gotEvent == nil {
		t.Fatal("expected an ErrorEvent to be emitted")
	}
	if !gotEvent.IsError || gotEvent.ErrorCode != "provider_stream_error" {
		t.Errorf("unexpected ErrorEvent: %+v", gotEvent)
	}
	if exitCode == nil || *exitCode == 0 {
		t.Errorf("expected non-zero exit code, got %v", exitCode)
	}
}

// A normal stop reason must not be intercepted: handleErrorStopReason returns
// false and emits nothing, leaving the existing switch behavior intact.
func TestHandleErrorStopReasonIgnoresNonError(t *testing.T) {
	b := NewApiBackend()

	emitted := false
	b.OnNormalized(func(_ string, _ types.NormalizedEvent) { emitted = true })
	exited := false
	b.OnExit(func(_ string, _ *int, _ *string, _ string) { exited = true })

	run := &activeRun{requestID: "t-run"}
	if b.handleErrorStopReason(run, "conv-1", "end_turn", 0) {
		t.Fatal("end_turn must not be handled as an error")
	}
	if emitted {
		t.Error("no event should be emitted for a non-error stop reason")
	}
	if exited {
		t.Error("no exit should be emitted for a non-error stop reason")
	}
}
