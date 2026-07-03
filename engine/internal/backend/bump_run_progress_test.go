package backend

import (
	"testing"
	"time"
)

// TestBumpRunProgressAdvancesClock pins the BumpRunProgress seam used by the
// dispatch/spawn layer to report child liveness to the parent run's
// run-progress watchdog. It must advance lastProgressAt for an active run.
//
// This is the second half of the matched pair documented on
// ApiBackend.emitWithoutProgress: the parent's self-emitted stall advisory
// stops counting as progress, and the child's genuine activity (reported via
// BumpRunProgress) starts counting. Without this, a healthy long dispatch would
// trip the run-stall watchdog once the stall emit stopped resetting the clock.
func TestBumpRunProgressAdvancesClock(t *testing.T) {
	b := NewApiBackend()
	const requestID = "req-bump-progress"

	// Register a minimal active run with a deliberately stale progress clock.
	stale := time.Now().Add(-time.Hour).UnixNano()
	run := &activeRun{requestID: requestID}
	run.lastProgressAt.Store(stale)
	b.mu.Lock()
	b.activeRuns[requestID] = run
	b.mu.Unlock()

	before := run.lastProgressAt.Load()
	b.BumpRunProgress(requestID)
	after := run.lastProgressAt.Load()

	if after <= before {
		t.Errorf("BumpRunProgress did not advance lastProgressAt: before=%d after=%d", before, after)
	}
	if after <= stale {
		t.Errorf("BumpRunProgress left a stale clock: stale=%d after=%d", stale, after)
	}
}

// TestBumpRunProgressUnknownRunIsNoop verifies BumpRunProgress is safe to call
// for a requestID with no active run (e.g. the parent run already exited while
// a late child event arrives). It must not panic and must not register a run.
func TestBumpRunProgressUnknownRunIsNoop(t *testing.T) {
	b := NewApiBackend()

	// Must not panic.
	b.BumpRunProgress("no-such-run")

	b.mu.Lock()
	_, exists := b.activeRuns["no-such-run"]
	n := len(b.activeRuns)
	b.mu.Unlock()
	if exists {
		t.Error("BumpRunProgress must not create an activeRuns entry for an unknown run")
	}
	if n != 0 {
		t.Errorf("expected no active runs, got %d", n)
	}
}
