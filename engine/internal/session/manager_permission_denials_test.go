package session

import (
	"testing"

	"github.com/dsswift/ion/engine/internal/session/agents"
	"github.com/dsswift/ion/engine/internal/session/pending"
	"github.com/dsswift/ion/engine/internal/types"
)

// ---------------------------------------------------------------------------
// ReconcileState pending-permission-denial retention contract tests
//
// Background: AskUserQuestion and ExitPlanMode are interactive tool calls
// the engine intercepts and reports as PermissionDenials on the
// TaskCompleteEvent. The engine retains the most recent task_complete's
// denial slice on engineSession.lastPermissionDenials so that the next
// ReconcileState call can surface them on the engine_status snapshot.
//
// If a consumer re-attaches to a session that is still blocked on an
// unanswered denial, the engine_status emitted by ReconcileState must
// carry the retained PermissionDenials — otherwise the snapshot would
// drop the field while the session is still in the same state. The
// engine does not know or care how a consumer interprets the field;
// it only guarantees the field's presence and authority.
//
// These tests pin that contract on the engine side. See
// docs/engine-grounding.md §4 (snapshot contract) for the re-attach rule.
// ---------------------------------------------------------------------------

// TestReconcileState_RetainsPermissionDenials verifies that pending
// AskUserQuestion / ExitPlanMode denials captured from a TaskCompleteEvent
// are re-emitted by ReconcileState as part of the engine_status snapshot.
func TestReconcileState_RetainsPermissionDenials(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("recon-denials", defaultConfig())

	// Seed the session with retained denials, simulating the post-state
	// after a TaskCompleteEvent that carried an AskUserQuestion intercept.
	mgr.mu.Lock()
	s := mgr.sessions["recon-denials"]
	s.lastPermissionDenials = []types.PermissionDenial{
		{
			ToolUseID: "tu-1",
			ToolName:  "AskUserQuestion",
			ToolInput: map[string]any{"question": "Pick one", "options": []string{"A", "B"}},
		},
	}
	mgr.mu.Unlock()

	// Collect the engine_status emitted by ReconcileState.
	var statusEvents []types.EngineEvent
	mgr.OnEvent(func(_ string, ev types.EngineEvent) {
		if ev.Type == "engine_status" {
			statusEvents = append(statusEvents, ev)
		}
	})

	mgr.ReconcileState("recon-denials")

	if len(statusEvents) == 0 {
		t.Fatal("expected engine_status event from ReconcileState")
	}
	last := statusEvents[len(statusEvents)-1]
	if last.Fields == nil {
		t.Fatal("expected non-nil StatusFields on reconciled status")
	}
	if len(last.Fields.PermissionDenials) != 1 {
		t.Fatalf("expected 1 PermissionDenial in reconciled status, got %d: %+v", len(last.Fields.PermissionDenials), last.Fields.PermissionDenials)
	}
	d := last.Fields.PermissionDenials[0]
	if d.ToolName != "AskUserQuestion" {
		t.Errorf("expected ToolName=AskUserQuestion, got %q", d.ToolName)
	}
	if d.ToolUseID != "tu-1" {
		t.Errorf("expected ToolUseID=tu-1, got %q", d.ToolUseID)
	}
	q, _ := d.ToolInput["question"].(string)
	if q != "Pick one" {
		t.Errorf("expected ToolInput.question='Pick one', got %q", q)
	}
}

// TestReconcileState_EmitsEmptyDenialsWhenNonePending verifies the
// snapshot contract holds in the zero-state direction too: a session
// with no retained denials emits engine_status with empty (nil)
// PermissionDenials, not a stale value.
func TestReconcileState_EmitsEmptyDenialsWhenNonePending(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("recon-clean", defaultConfig())

	var statusEvents []types.EngineEvent
	mgr.OnEvent(func(_ string, ev types.EngineEvent) {
		if ev.Type == "engine_status" {
			statusEvents = append(statusEvents, ev)
		}
	})

	mgr.ReconcileState("recon-clean")

	if len(statusEvents) == 0 {
		t.Fatal("expected engine_status event from ReconcileState")
	}
	last := statusEvents[len(statusEvents)-1]
	if last.Fields == nil {
		t.Fatal("expected non-nil StatusFields on reconciled status")
	}
	if len(last.Fields.PermissionDenials) != 0 {
		t.Errorf("expected empty PermissionDenials in clean session, got %d: %+v", len(last.Fields.PermissionDenials), last.Fields.PermissionDenials)
	}
}

// TestTaskComplete_RetainsPermissionDenialsOnSession verifies that the
// session captures the PermissionDenials field from a TaskCompleteEvent
// flowing through handleNormalizedEvent. This is the "write half" of the
// retention contract — ReconcileState reads what TaskComplete wrote.
func TestTaskComplete_RetainsPermissionDenialsOnSession(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("tc-retain", defaultConfig())

	// Wire a runID -> session key so handleNormalizedEvent resolves.
	mgr.mu.Lock()
	s := mgr.sessions["tc-retain"]
	s.requestID = "run-tc-retain"
	mgr.mu.Unlock()

	// Drive a TaskCompleteEvent with PermissionDenials through the full
	// handleNormalizedEvent path.
	mgr.handleNormalizedEvent("run-tc-retain", types.NormalizedEvent{
		Data: &types.TaskCompleteEvent{
			Result:    "intercepted",
			SessionID: "conv-1",
			PermissionDenials: []types.PermissionDenial{
				{ToolUseID: "tu-2", ToolName: "AskUserQuestion", ToolInput: map[string]any{"question": "q?"}},
			},
		},
	})

	mgr.mu.RLock()
	got := s.lastPermissionDenials
	mgr.mu.RUnlock()

	if len(got) != 1 {
		t.Fatalf("expected 1 retained denial, got %d: %+v", len(got), got)
	}
	if got[0].ToolName != "AskUserQuestion" || got[0].ToolUseID != "tu-2" {
		t.Errorf("retained denial mismatch: %+v", got[0])
	}
}

// TestTaskComplete_ClearsRetainedDenialsWhenEmpty verifies that a clean
// task_complete (no denials) REPLACES any previously retained denials.
// Snapshot semantics: the most recent task_complete is authoritative.
// A session that finished cleanly has no outstanding question.
func TestTaskComplete_ClearsRetainedDenialsWhenEmpty(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("tc-clear", defaultConfig())

	mgr.mu.Lock()
	s := mgr.sessions["tc-clear"]
	s.requestID = "run-tc-clear"
	// Pre-seed retained denials from a prior task_complete.
	s.lastPermissionDenials = []types.PermissionDenial{
		{ToolUseID: "tu-old", ToolName: "AskUserQuestion"},
	}
	mgr.mu.Unlock()

	// Drive a CLEAN TaskCompleteEvent (no denials).
	mgr.handleNormalizedEvent("run-tc-clear", types.NormalizedEvent{
		Data: &types.TaskCompleteEvent{
			Result:    "ok",
			SessionID: "conv-2",
			// PermissionDenials intentionally nil
		},
	})

	mgr.mu.RLock()
	got := s.lastPermissionDenials
	mgr.mu.RUnlock()

	if len(got) != 0 {
		t.Errorf("expected retained denials to be cleared by clean task_complete, got %d: %+v", len(got), got)
	}
}

// TestReconcileState_PendingDenialsArePerSession verifies that retained
// denials are scoped to the session that owns them — a reconcile on
// session A must not re-emit denials retained on session B.
func TestReconcileState_PendingDenialsArePerSession(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("sess-a", defaultConfig())
	_, _ = mgr.StartSession("sess-b", defaultConfig())

	mgr.mu.Lock()
	mgr.sessions["sess-a"].lastPermissionDenials = []types.PermissionDenial{
		{ToolUseID: "tu-a", ToolName: "AskUserQuestion"},
	}
	// sess-b has no denials.
	mgr.mu.Unlock()

	// Track per-key status emissions.
	statusByKey := map[string][]types.EngineEvent{}
	mgr.OnEvent(func(key string, ev types.EngineEvent) {
		if ev.Type == "engine_status" {
			statusByKey[key] = append(statusByKey[key], ev)
		}
	})

	mgr.ReconcileState("sess-b")
	mgr.ReconcileState("sess-a")

	if len(statusByKey["sess-a"]) == 0 || len(statusByKey["sess-b"]) == 0 {
		t.Fatalf("expected status emitted for both sessions: a=%d b=%d", len(statusByKey["sess-a"]), len(statusByKey["sess-b"]))
	}
	lastA := statusByKey["sess-a"][len(statusByKey["sess-a"])-1]
	lastB := statusByKey["sess-b"][len(statusByKey["sess-b"])-1]

	if lastA.Fields == nil || len(lastA.Fields.PermissionDenials) != 1 {
		t.Errorf("expected sess-a to carry 1 denial, got %+v", lastA.Fields)
	}
	if lastB.Fields == nil || len(lastB.Fields.PermissionDenials) != 0 {
		t.Errorf("expected sess-b to carry 0 denials, got %+v", lastB.Fields)
	}
}

// makeSessionWithDenials is a small helper to assemble an engineSession
// with retained denials for tests that don't need to go through the full
// handleNormalizedEvent translation path.
func makeSessionWithDenials(key string, denials []types.PermissionDenial) *engineSession {
	return &engineSession{
		key:                   key,
		config:                defaultConfig(),
		agents:                agents.NewRegistry(),
		childPIDs:             make(map[int]struct{}),
		pending:               pending.New(),
		lastPermissionDenials: denials,
	}
}

// Compile-time guarantee that the helper above is referenced (it's
// available for future tests that need it; the linter would otherwise
// flag it as unused if left dangling).
var _ = makeSessionWithDenials
