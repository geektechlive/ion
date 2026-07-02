package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dsswift/ion/engine/internal/conversation"
	"github.com/dsswift/ion/engine/internal/types"
)

// ---------------------------------------------------------------------------
// ComputeAndEmitContextBreakdown tests
// ---------------------------------------------------------------------------
//
// Three scenarios per the plan:
//
//   1. Fresh (empty) conversation: emits breakdown; conversation category
//      is zero (no messages on disk yet).
//
//   2. Historical conversation: emits a breakdown whose conversation category
//      token count is non-zero (messages loaded from disk).
//
//   3. Unknown key: does not panic and emits no event.

// TestComputeAndEmitContextBreakdown_FreshSession checks that an empty
// conversation produces a non-nil breakdown with zero conversation tokens.
func TestComputeAndEmitContextBreakdown_FreshSession(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)

	// Wire a runtime config with a default model so the breakdown can resolve
	// token counts via local BPE (no provider needed).
	mgr.SetConfig(&types.EngineRuntimeConfig{
		DefaultModel: "claude-opus-4-5",
	})

	ec := newEventCollector(mgr)

	cfg := types.EngineConfig{
		ProfileID:        "test",
		WorkingDirectory: t.TempDir(),
	}
	if _, err := mgr.StartSession("fresh", cfg); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	mgr.ComputeAndEmitContextBreakdown("fresh")

	breakdowns := ec.byType("engine_context_breakdown")
	if len(breakdowns) == 0 {
		t.Fatal("expected engine_context_breakdown event, got none")
	}

	ev := breakdowns[len(breakdowns)-1].event
	bd := ev.ContextBreakdown
	if bd == nil {
		t.Fatal("ContextBreakdown payload is nil")
	}

	// Fresh session: no conversation messages, so conversation tokens == 0.
	conversationTokens := 0
	for _, cat := range bd.Categories {
		if cat.Kind == "conversation" {
			conversationTokens += cat.Tokens
		}
	}
	if conversationTokens != 0 {
		t.Errorf("fresh session: expected 0 conversation tokens, got %d", conversationTokens)
	}
}

// TestComputeAndEmitContextBreakdown_HistoricalSession checks that a session
// with on-disk messages produces a breakdown with non-zero conversation tokens.
func TestComputeAndEmitContextBreakdown_HistoricalSession(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)

	mgr.SetConfig(&types.EngineRuntimeConfig{
		DefaultModel: "claude-opus-4-5",
	})

	// Write a conversation file with a user message.
	convDir := filepath.Join(os.Getenv("HOME"), ".ion", "conversations")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sessionID := "test-hist-cbd-" + t.Name()
	conv := conversation.CreateConversation(sessionID, "You are a test assistant.", "claude-opus-4-5")
	conversation.AddUserMessage(conv, []types.LlmContentBlock{{Type: "text", Text: "Hello, what is the capital of France?"}})
	if err := conversation.Save(conv, ""); err != nil {
		t.Fatalf("Save conversation: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(convDir, sessionID+".llm.jsonl"))
		_ = os.Remove(filepath.Join(convDir, sessionID+".tree.jsonl"))
	})

	ec := newEventCollector(mgr)

	cfg := types.EngineConfig{
		ProfileID:        "test",
		WorkingDirectory: t.TempDir(),
	}
	if _, err := mgr.StartSession("hist", cfg); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	// Inject the conversationID directly (mirrors what load_session_history does).
	mgr.mu.Lock()
	if s, ok := mgr.sessions["hist"]; ok {
		s.conversationID = sessionID
	}
	mgr.mu.Unlock()

	mgr.ComputeAndEmitContextBreakdown("hist")

	breakdowns := ec.byType("engine_context_breakdown")
	if len(breakdowns) == 0 {
		t.Fatal("expected engine_context_breakdown event, got none")
	}

	ev := breakdowns[len(breakdowns)-1].event
	bd := ev.ContextBreakdown
	if bd == nil {
		t.Fatal("ContextBreakdown payload is nil")
	}

	// Historical session: conversation messages loaded, so tokens > 0.
	conversationTokens := 0
	for _, cat := range bd.Categories {
		if cat.Kind == "conversation" {
			conversationTokens += cat.Tokens
		}
	}
	if conversationTokens == 0 {
		t.Error("historical session: expected non-zero conversation tokens, got 0")
	}
}

// TestComputeAndEmitContextBreakdown_UnknownKey checks that a missing session
// key does not panic and emits no event.
func TestComputeAndEmitContextBreakdown_UnknownKey(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	ec := newEventCollector(mgr)

	// Should not panic; a Warn log fires internally.
	mgr.ComputeAndEmitContextBreakdown("no-such-key")

	breakdowns := ec.byType("engine_context_breakdown")
	if len(breakdowns) != 0 {
		t.Errorf("expected no event for unknown key, got %d", len(breakdowns))
	}
}
