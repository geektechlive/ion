package backend

import (
	"context"
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/types"
)

// Human-wait watchdog exemption tests.
//
// These pin the fix for the indefinite-human-wait incident
// (1782060832205-836960a71da9 / 3d580dc5): a run parked inside an intentional
// elicitation / permission human-wait must NOT be cancelled by the run-progress
// watchdog for idleness, because the human-wait is indefinite by default. The
// exemption is reference-counted on activeRun.humanWaitDepth via
// BeginHumanWait / EndHumanWait, and consulted in runProgressWatchdog's idle
// branch. When the last human-wait ends the clock resumes with a fresh window.

// newWatchdogTestRun builds a minimal active run with a real cancellable context
// and a deliberately stale progress clock, registered on the backend so the
// watchdog's activeRuns lookup finds it. Returns the run plus a func reporting
// whether the run's context has been cancelled (i.e. the watchdog fired).
func newWatchdogTestRun(t *testing.T, b *ApiBackend, requestID string) (*activeRun, func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	run := &activeRun{
		requestID:            requestID,
		cancel:               cancel,
		progressWatchdogStop: make(chan struct{}),
	}
	// Stale clock: an hour ago, far beyond any test threshold, so the watchdog
	// would cancel immediately if not for the human-wait exemption.
	run.lastProgressAt.Store(time.Now().Add(-time.Hour).UnixNano())
	b.mu.Lock()
	b.activeRuns[requestID] = run
	b.mu.Unlock()
	t.Cleanup(func() {
		// Stop the watchdog goroutine and drop the run.
		b.mu.Lock()
		delete(b.activeRuns, requestID)
		b.mu.Unlock()
		select {
		case <-run.progressWatchdogStop:
		default:
			close(run.progressWatchdogStop)
		}
		cancel()
	})
	cancelled := func() bool {
		select {
		case <-ctx.Done():
			return true
		default:
			return false
		}
	}
	return run, cancelled
}

// TestRunStallExemptDuringHumanWait pins the core guarantee: while a run is in an
// active human-wait, the watchdog does NOT cancel it despite an arbitrarily stale
// progress clock, and once the human-wait ends the watchdog resumes and the run
// is cancelled.
//
// Revert-check: without the `run.humanWaitDepth.Load() > 0` exemption branch in
// runProgressWatchdog, the first assertion fails — the run is cancelled while the
// human is still being awaited (the incident defect).
func TestRunStallExemptDuringHumanWait(t *testing.T) {
	// Tight threshold + fast tick so the test runs in tens of ms. The stale
	// clock (-1h) is already far past any threshold.
	withFastWatchdogTick(t, 5*time.Millisecond)

	b := NewApiBackend()
	const requestID = "req-humanwait-exempt"
	run, cancelled := newWatchdogTestRun(t, b, requestID)

	// Enter a human-wait BEFORE the watchdog starts observing.
	b.BeginHumanWait(requestID)
	if got := run.humanWaitDepth.Load(); got != 1 {
		t.Fatalf("BeginHumanWait: humanWaitDepth = %d, want 1", got)
	}

	// Run the watchdog with a sub-threshold so any idle run would be cancelled
	// instantly were it not for the exemption.
	run.cfg = &RunConfig{Timeouts: &types.TimeoutsConfig{RunStallMs: 10}}
	go b.runProgressWatchdog(run)

	// Give the watchdog many ticks. It must NOT cancel while in the human-wait.
	time.Sleep(80 * time.Millisecond)
	if cancelled() {
		t.Fatal("watchdog cancelled a run that is in an active human-wait — indefinite-human-wait guarantee violated")
	}

	// End the human-wait. The clock is reset (fresh window) by EndHumanWait, but
	// no further progress lands, so after one threshold + tick the watchdog must
	// cancel.
	b.EndHumanWait(requestID)
	if got := run.humanWaitDepth.Load(); got != 0 {
		t.Fatalf("EndHumanWait: humanWaitDepth = %d, want 0", got)
	}

	deadline := time.After(2 * time.Second)
	for !cancelled() {
		select {
		case <-deadline:
			t.Fatal("watchdog did not cancel the run after the human-wait ended — exemption did not resume")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestHumanWaitDepthReferenceCounted pins that overlapping/nested human-waits are
// reference-counted: the watchdog stays exempt until the LAST wait ends, not the
// first.
func TestHumanWaitDepthReferenceCounted(t *testing.T) {
	withFastWatchdogTick(t, 5*time.Millisecond)

	b := NewApiBackend()
	const requestID = "req-humanwait-refcount"
	run, cancelled := newWatchdogTestRun(t, b, requestID)

	// Two overlapping waits (e.g. an extension hook elicits while a dialog is
	// open).
	b.BeginHumanWait(requestID)
	b.BeginHumanWait(requestID)
	if got := run.humanWaitDepth.Load(); got != 2 {
		t.Fatalf("two BeginHumanWait: humanWaitDepth = %d, want 2", got)
	}

	run.cfg = &RunConfig{Timeouts: &types.TimeoutsConfig{RunStallMs: 10}}
	go b.runProgressWatchdog(run)

	// End only the first wait. Depth is still 1, so the watchdog must remain
	// exempt.
	b.EndHumanWait(requestID)
	if got := run.humanWaitDepth.Load(); got != 1 {
		t.Fatalf("after one EndHumanWait: humanWaitDepth = %d, want 1", got)
	}
	time.Sleep(60 * time.Millisecond)
	if cancelled() {
		t.Fatal("watchdog cancelled while one human-wait was still open — reference counting broken")
	}

	// End the last wait. Now the watchdog resumes and cancels the idle run.
	b.EndHumanWait(requestID)
	deadline := time.After(2 * time.Second)
	for !cancelled() {
		select {
		case <-deadline:
			t.Fatal("watchdog did not cancel after the last human-wait ended")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestEndHumanWaitResetsClock pins that EndHumanWait stamps lastProgressAt on the
// depth 1 → 0 transition, so the machine work resuming after a human-wait gets a
// full fresh RunStall() window rather than being charged for the human's
// think-time.
func TestEndHumanWaitResetsClock(t *testing.T) {
	b := NewApiBackend()
	const requestID = "req-humanwait-clockreset"

	stale := time.Now().Add(-time.Hour).UnixNano()
	run := &activeRun{requestID: requestID}
	run.lastProgressAt.Store(stale)
	b.mu.Lock()
	b.activeRuns[requestID] = run
	b.mu.Unlock()

	b.BeginHumanWait(requestID)
	// During the wait the clock is untouched (the watchdog skips the idle check
	// instead of relying on a fresh clock).
	if run.lastProgressAt.Load() != stale {
		t.Errorf("BeginHumanWait must not touch lastProgressAt: got %d want %d", run.lastProgressAt.Load(), stale)
	}
	b.EndHumanWait(requestID)
	if run.lastProgressAt.Load() <= stale {
		t.Errorf("EndHumanWait did not reset the clock on depth 0: lastProgressAt=%d stale=%d", run.lastProgressAt.Load(), stale)
	}
}

// TestHumanWaitUnknownRunIsNoop verifies Begin/EndHumanWait are safe for a
// requestID with no active run (the run exited while a late dialog teardown
// fires). Must not panic and must not register a run.
func TestHumanWaitUnknownRunIsNoop(t *testing.T) {
	b := NewApiBackend()

	b.BeginHumanWait("no-such-run")
	b.EndHumanWait("no-such-run")

	b.mu.Lock()
	n := len(b.activeRuns)
	b.mu.Unlock()
	if n != 0 {
		t.Errorf("Begin/EndHumanWait created activeRuns entries for an unknown run: got %d", n)
	}
}

// TestEndHumanWaitUnmatchedFloorsAtZero verifies a stray EndHumanWait (no
// matching Begin) cannot drive humanWaitDepth negative and permanently disarm
// the watchdog — the depth is floored at 0.
func TestEndHumanWaitUnmatchedFloorsAtZero(t *testing.T) {
	b := NewApiBackend()
	const requestID = "req-humanwait-unmatched"
	run := &activeRun{requestID: requestID}
	b.mu.Lock()
	b.activeRuns[requestID] = run
	b.mu.Unlock()

	b.EndHumanWait(requestID) // unmatched
	if got := run.humanWaitDepth.Load(); got != 0 {
		t.Errorf("unmatched EndHumanWait drove depth to %d, want floored at 0", got)
	}
}

// TestHumanWaitOnHybridInnerApi pins the parity requirement that the
// HybridBackend's run-progress watchdog lives on its inner *ApiBackend: a
// human-wait registered against the inner backend (where the session layer routes
// it via InnerApi) lands on that inner run's humanWaitDepth.
func TestHumanWaitOnHybridInnerApi(t *testing.T) {
	hybrid := NewHybridBackend()
	inner := hybrid.InnerApi()
	const requestID = "req-humanwait-hybrid"

	run := &activeRun{requestID: requestID}
	inner.mu.Lock()
	inner.activeRuns[requestID] = run
	inner.mu.Unlock()

	inner.BeginHumanWait(requestID)
	if got := run.humanWaitDepth.Load(); got != 1 {
		t.Fatalf("hybrid inner BeginHumanWait: depth = %d, want 1", got)
	}
	inner.EndHumanWait(requestID)
	if got := run.humanWaitDepth.Load(); got != 0 {
		t.Fatalf("hybrid inner EndHumanWait: depth = %d, want 0", got)
	}
}
