package extension

import (
	"encoding/json"
	"testing"
	"time"
)

func sendPromptPayload(t *testing.T, text string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ext/send_prompt",
		"params":  map[string]string{"text": text},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestExtSendPrompt_FallsBackToOnSendMessage_WhenNoHookCtx verifies the timer/scheduler
// path: no active hook dispatch ctx, but onSendMessage (wired by the session manager)
// is available and should be called.
func TestExtSendPrompt_FallsBackToOnSendMessage_WhenNoHookCtx(t *testing.T) {
	h := NewHost()
	// currentCtx is nil by default -- simulates a timer-fired call with no active hook.

	called := make(chan string, 1)
	h.SetOnSendMessage(func(text string) {
		called <- text
	})

	h.handleExtRequest("ext/send_prompt", 1, sendPromptPayload(t, "Deliver the morning brief now."))

	select {
	case got := <-called:
		if got != "Deliver the morning brief now." {
			t.Fatalf("expected prompt text, got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("onSendMessage was not called within 1s")
	}
}

// TestExtSendPrompt_UsesHookCtx_WhenActive verifies that ext/send_prompt routes
// through ctx.SendPrompt when an active hook dispatch context is present, and does
// not call onSendMessage.
func TestExtSendPrompt_UsesHookCtx_WhenActive(t *testing.T) {
	h := NewHost()

	hookCalled := make(chan string, 1)
	ctx := &Context{
		Cwd:   "/tmp",
		Model: &ModelRef{ID: "claude-sonnet-4-20250514", ContextWindow: 200000},
		SendPrompt: func(text, _ string) error {
			hookCalled <- text
			return nil
		},
	}
	h.currentCtx.Store(ctx)

	h.SetOnSendMessage(func(_ string) {
		t.Error("onSendMessage must not be called when hook ctx is active")
	})

	h.handleExtRequest("ext/send_prompt", 1, sendPromptPayload(t, "test hook path"))

	select {
	case got := <-hookCalled:
		if got != "test hook path" {
			t.Fatalf("expected 'test hook path', got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("ctx.SendPrompt was not called within 1s")
	}
}

// TestExtSendPrompt_ReturnsError_WhenNoSessionAtAll verifies that ext/send_prompt
// does not panic when neither hook ctx nor onSendMessage is set (daemon pre-session).
func TestExtSendPrompt_ReturnsError_WhenNoSessionAtAll(t *testing.T) {
	h := NewHost()
	// No ctx, no onSendMessage -- sendResponse is a no-op when stdin is nil.
	h.handleExtRequest("ext/send_prompt", 1, sendPromptPayload(t, "no session"))
	// Success = no panic or deadlock.
}

// TestExtSendPrompt_RejectsEmptyText verifies that an empty prompt is rejected
// before either path is attempted.
func TestExtSendPrompt_RejectsEmptyText(t *testing.T) {
	h := NewHost()
	h.SetOnSendMessage(func(_ string) {
		t.Error("onSendMessage must not be called for empty prompt")
	})

	data, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ext/send_prompt",
		"params":  map[string]string{"text": ""},
	})
	h.handleExtRequest("ext/send_prompt", 1, data)
	// Success = onSendMessage not called, no panic.
}
