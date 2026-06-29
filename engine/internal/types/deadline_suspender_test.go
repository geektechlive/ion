package types

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestDeadlineSuspender_FiresWhenNotPaused pins that an un-paused suspender
// cancels the context at the deadline (baseline = it still works as a timeout).
func TestDeadlineSuspender_FiresWhenNotPaused(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ds := NewDeadlineSuspender(40*time.Millisecond, cancel)
	defer ds.Stop()

	select {
	case <-ctx.Done():
		// expected: deadline fired
	case <-time.After(500 * time.Millisecond):
		t.Fatal("deadline did not fire within 500ms")
	}
}

// TestDeadlineSuspender_PauseSuspendsDeadline pins the core behavior: while
// paused, the deadline does NOT fire even well past the original timeout. This
// is the property that lets an indefinite human-wait survive inside a tool.
// Revert-check: removing Pause() makes ctx cancel at the deadline and this fails.
func TestDeadlineSuspender_PauseSuspendsDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ds := NewDeadlineSuspender(40*time.Millisecond, cancel)
	defer ds.Stop()

	ds.Pause()

	// Wait well past the original 40ms deadline. While paused it must not fire.
	select {
	case <-ctx.Done():
		t.Fatal("deadline fired while paused — human-wait would be capped")
	case <-time.After(200 * time.Millisecond):
		// expected: still alive
	}

	// After resume, the deadline re-arms with the FULL timeout, so the tool
	// gets its complete finite budget for remaining work.
	ds.Resume()
	select {
	case <-ctx.Done():
		// expected: fires again after resume
	case <-time.After(500 * time.Millisecond):
		t.Fatal("deadline did not re-arm after resume")
	}
}

// TestDeadlineSuspender_ReferenceCounted pins that nested/sequential pauses need
// matching resumes before the deadline re-arms.
func TestDeadlineSuspender_ReferenceCounted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ds := NewDeadlineSuspender(40*time.Millisecond, cancel)
	defer ds.Stop()

	ds.Pause()
	ds.Pause()
	ds.Resume() // one outstanding pause remains — still suspended

	select {
	case <-ctx.Done():
		t.Fatal("deadline fired with one outstanding pause remaining")
	case <-time.After(200 * time.Millisecond):
		// expected: still suspended
	}

	ds.Resume() // refcount back to 0 — re-arms
	select {
	case <-ctx.Done():
		// expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("deadline did not re-arm after final resume")
	}
}

// TestDeadlineSuspender_NilSafe pins that a nil handle (returned for
// non-positive timeouts, e.g. the Agent tool) is a safe no-op.
func TestDeadlineSuspender_NilSafe(t *testing.T) {
	ds := NewDeadlineSuspender(0, func() {})
	if ds != nil {
		t.Fatal("expected nil handle for non-positive timeout")
	}
	// None of these may panic.
	ds.Pause()
	ds.Resume()
	ds.Stop()
}

// TestDeadlineSuspender_StopPreventsFire pins that Stop releases the timer so a
// completed tool's deadline never fires afterward.
func TestDeadlineSuspender_StopPreventsFire(t *testing.T) {
	fired := make(chan struct{}, 1)
	ds := NewDeadlineSuspender(40*time.Millisecond, func() { fired <- struct{}{} })
	ds.Stop()
	select {
	case <-fired:
		t.Fatal("deadline fired after Stop")
	case <-time.After(150 * time.Millisecond):
		// expected: stopped
	}
}

// TestDeadlineSuspender_ContextThreading pins the ctx helpers round-trip and
// that a missing suspender is a nil no-op.
func TestDeadlineSuspender_ContextThreading(t *testing.T) {
	if got := DeadlineSuspenderFrom(context.Background()); got != nil {
		t.Error("expected nil suspender from bare context")
	}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	ds := NewDeadlineSuspender(time.Minute, cancel)
	defer ds.Stop()
	ctx := WithDeadlineSuspender(context.Background(), ds)
	got := DeadlineSuspenderFrom(ctx)
	if got == nil {
		t.Fatal("expected suspender from threaded context")
	}
	// Pause/Resume reachable through the interface.
	got.Pause()
	got.Resume()
}

// TestDeadlineSuspender_ConcurrentPauseResume exercises the mutex under -race.
func TestDeadlineSuspender_ConcurrentPauseResume(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	ds := NewDeadlineSuspender(time.Hour, cancel)
	defer ds.Stop()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ds.Pause()
			ds.Resume()
		}()
	}
	wg.Wait()
}
