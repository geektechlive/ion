package session

import (
	"context"
	"testing"
	"time"
)

// session_root_context_test.go — tests for the per-session cancellation
// root introduced for the unified abort tree (#232). These pin the new
// behavior: SendAbort and StopSession cancel the session root, and a
// context derived from RunOptions.ParentCtx (the root) observes Done().

// TestSendPrompt_RearmsRootAfterAbort is the critical regression test for the
// 1782088921498-960b064fe896 wedge: after an abort cancels the session root,
// the NEXT prompt must run on a LIVE root, not the dead one. Before the
// rearmRootContextIfCancelled fix, newSessionRootContext was only called once
// at StartSession, so the second run's ParentCtx was the already-cancelled
// root and the run exited instantly with signal=cancelled — wedging the
// session until an engine restart.
//
// Revert rearmRootContextIfCancelled (or its call site in SendPrompt) and this
// test goes red: the second run's ParentCtx is born cancelled (Err() != nil).
func TestSendPrompt_RearmsRootAfterAbort(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("rearm", defaultConfig())

	// First prompt → first run.
	_ = mgr.SendPrompt("rearm", "first", nil)
	first := mb.startedInOrder()
	if len(first) != 1 {
		t.Fatalf("expected 1 run after first prompt, got %d", len(first))
	}

	// User (or the desktop stuck-tab watchdog) aborts. This cancels the
	// session root permanently.
	mgr.SendAbort("rearm")

	// The cancelled run unwinds and the backend reports exit with the
	// cooperative cancel signal (mirrors runloop.go emitExit on ctx cancel).
	// handleRunExit clears s.requestID so the next prompt can dispatch.
	mb.emitExit(first[0], intPtr(0), strPtr("cancelled"), "")

	// Second prompt → second run. This is the `resume` the user typed.
	_ = mgr.SendPrompt("rearm", "second", nil)
	order := mb.startedInOrder()
	if len(order) != 2 {
		t.Fatalf("expected 2 runs after resume, got %d (the post-abort run failed to dispatch — session wedged)", len(order))
	}

	opts, ok := mb.getStarted(order[1])
	if !ok {
		t.Fatal("second run options not captured")
	}
	if opts.ParentCtx == nil {
		t.Fatal("second run ParentCtx is nil (run would be orphaned)")
	}
	// THE ASSERTION: the second run's threaded root must be LIVE. Before the
	// fix this is the cancelled first root, so Err() is non-nil and the run
	// would exit immediately with signal=cancelled.
	if err := opts.ParentCtx.Err(); err != nil {
		t.Fatalf("second run ParentCtx is already cancelled (%v) — session wedged after abort; root was not re-armed", err)
	}

	// And a context derived from it must still be live, then cancel only on a
	// NEW abort — proving the re-armed root is fully functional.
	derived, cancel := context.WithCancel(opts.ParentCtx)
	defer cancel()
	select {
	case <-derived.Done():
		t.Fatal("derived context cancelled immediately — re-armed root is not live")
	default:
	}
	mgr.SendAbort("rearm")
	select {
	case <-derived.Done():
		// expected: the new abort cancels the re-armed root
	case <-time.After(2 * time.Second):
		t.Fatal("re-armed root did not cancel on a fresh SendAbort")
	}
}

// TestRearmRootContext_NoOpWhenLive asserts the re-arm is a no-op when the
// root is still live: back-to-back prompts with no intervening abort must keep
// the SAME root so in-flight descendants are never reparented.
func TestRearmRootContext_NoOpWhenLive(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("noop-rearm", defaultConfig())

	mgr.mu.RLock()
	s := mgr.sessions["noop-rearm"]
	mgr.mu.RUnlock()
	if s == nil {
		t.Fatal("session not found")
	}

	mgr.mu.Lock()
	before := s.rootCtx
	s.rearmRootContextIfCancelled()
	after := s.rootCtx
	mgr.mu.Unlock()

	if before != after {
		t.Fatal("rearmRootContextIfCancelled replaced a LIVE root; it must be a no-op when Err()==nil")
	}
}

// TestSendAbort_CancelsSessionRoot asserts that a user abort cancels the
// session's cancellation root, so any operation that derived its context
// from the root observes ctx.Done(). This is the structural guarantee that
// makes "hit Stop, kill everything" work: backend runs, dispatched agents,
// and in-flight llmCall all derive from this root.
func TestSendAbort_CancelsSessionRoot(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("abort-root", defaultConfig())

	// Capture the session root and derive a child context the way a
	// backend run / llmCall / dispatch would.
	mgr.mu.RLock()
	s := mgr.sessions["abort-root"]
	mgr.mu.RUnlock()
	if s == nil {
		t.Fatal("session not found after StartSession")
	}
	derived, cancel := context.WithCancel(s.rootContext())
	defer cancel()

	select {
	case <-derived.Done():
		t.Fatal("derived context cancelled before abort")
	default:
	}

	mgr.SendAbort("abort-root")

	select {
	case <-derived.Done():
		// expected: root cancellation cascaded to the derived context
	case <-time.After(2 * time.Second):
		t.Fatal("derived context not cancelled within 2s of SendAbort")
	}
}

// TestStopSession_CancelsSessionRoot asserts session teardown cancels the
// root the same way abort does, so descendants are torn down with the
// session.
func TestStopSession_CancelsSessionRoot(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("stop-root", defaultConfig())

	mgr.mu.RLock()
	s := mgr.sessions["stop-root"]
	mgr.mu.RUnlock()
	if s == nil {
		t.Fatal("session not found after StartSession")
	}
	derived, cancel := context.WithCancel(s.rootContext())
	defer cancel()

	if err := mgr.StopSession("stop-root"); err != nil {
		t.Fatalf("StopSession: %v", err)
	}

	select {
	case <-derived.Done():
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("derived context not cancelled within 2s of StopSession")
	}
}

// TestSendPrompt_ThreadsParentCtx asserts the main-session dispatch threads
// the session root onto RunOptions.ParentCtx, so the backend run derives
// from it. Without this wiring the run would be orphaned on Background and a
// session abort could not cascade to it.
func TestSendPrompt_ThreadsParentCtx(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("parentctx", defaultConfig())
	_ = mgr.SendPrompt("parentctx", "go", nil)

	keys := mb.startedKeys()
	if len(keys) == 0 {
		t.Fatal("expected a run to start")
	}
	opts, ok := mb.getStarted(keys[0])
	if !ok {
		t.Fatal("started run options not captured")
	}
	if opts.ParentCtx == nil {
		t.Fatal("expected RunOptions.ParentCtx to be set to the session root; got nil (run would be orphaned on Background)")
	}

	// The threaded context must be the session's live root: cancelling the
	// session (abort) must cancel a context derived from opts.ParentCtx.
	derived, cancel := context.WithCancel(opts.ParentCtx)
	defer cancel()
	mgr.SendAbort("parentctx")
	select {
	case <-derived.Done():
		// expected: the threaded ParentCtx is the real session root
	case <-time.After(2 * time.Second):
		t.Fatal("ParentCtx did not cancel on SendAbort; threaded context is not the session root")
	}
}
