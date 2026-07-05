package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/conversation"
	"github.com/dsswift/ion/engine/internal/protocol"
	"github.com/dsswift/ion/engine/internal/types"
)

// seedConversation creates a conversation with n user+assistant message pairs
// in dir and returns the conversation ID.
func seedConversation(t *testing.T, dir string, id string, n int) string {
	t.Helper()
	conv := conversation.CreateConversation(id, "test prompt", "claude-3-haiku")
	for i := 0; i < n; i++ {
		conversation.AddUserMessage(conv, fmt.Sprintf("user message %d", i))
		conversation.AddAssistantMessage(conv,
			[]types.LlmContentBlock{{Type: "text", Text: fmt.Sprintf("assistant reply %d", i)}},
			types.LlmUsage{InputTokens: 10, OutputTokens: 10},
		)
	}
	if err := conversation.Save(conv, dir); err != nil {
		t.Fatalf("seedConversation Save: %v", err)
	}
	return id
}

// sendGetConversation issues a get_conversation command over conn and returns
// the parsed PaginatedMessages result.
func sendGetConversation(t *testing.T, srv *Server, convID string, offset, limit int) *protocol.ServerResult {
	t.Helper()
	conn := dialServer(t, srv)
	defer conn.Close()

	sendJSON(t, conn, map[string]interface{}{
		"cmd":       "get_conversation",
		"requestId": "req-" + convID,
		"key":       convID,
		"offset":    offset,
		"limit":     limit,
	})

	lines := readLines(t, conn, 10, 1*time.Second)
	res := findResult(t, lines)
	if res == nil {
		t.Fatalf("get_conversation: no result in response lines: %v", lines)
	}
	return res
}

// parseMessageCount extracts the message count from a ServerResult whose Data
// holds a PaginatedMessages JSON object. Because json.Unmarshal decodes
// ServerResult.Data (type any) as map[string]interface{}, we re-marshal to
// JSON and then decode into the typed struct.
func parseMessageCount(t *testing.T, res *protocol.ServerResult) int {
	t.Helper()
	if res.Data == nil {
		t.Fatal("get_conversation result has nil Data")
	}
	raw, err := json.Marshal(res.Data)
	if err != nil {
		t.Fatalf("re-marshal Data: %v", err)
	}
	var payload struct {
		Messages []json.RawMessage `json:"messages"`
		Total    int               `json:"total"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal paginated payload: %v", err)
	}
	return len(payload.Messages)
}

// TestGetConversation_LimitZeroReturnsAll is the regression test for the iOS
// dispatch 50-message cap.
//
// Root cause: dispatch.go previously clamped limit <= 0 to 50, but the desktop
// relay handler (engine.ts:99) passes limit=0 meaning "all messages." Callers
// with a shared conversation carrying more than 50 messages (e.g. dev-lead with
// 7 dispatches in one conversation) would get only the first 50, leaving every
// dispatch whose time-window fell after message #50 rendering empty on iOS.
//
// Fix: pass limit through unchanged; LoadMessagesPaginated already treats
// limit=0 as unbounded (list.go:388: "if limit > 0 && ...").
//
// Revert-check: restoring "if limit <= 0 { limit = 50 }" makes this test fail
// because the 60-message conversation is returned as 50 instead of 60.
func TestGetConversation_LimitZeroReturnsAll(t *testing.T) {
	// Redirect HOME so Load() resolves to our temp dir, not ~/.ion.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	convDir := filepath.Join(tmpHome, ".ion", "conversations")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatalf("MkdirAll convDir: %v", err)
	}

	// Seed 60 user+assistant pairs = 120 SessionMessages (above the old cap of 50).
	const pairs = 60
	convID := seedConversation(t, convDir, "test-limit-zero-60pairs", pairs)

	mb := newMockBackend()
	srv := newShortPathTestServer(t, mb)

	// --- limit=0 must return ALL 120 messages ---
	res := sendGetConversation(t, srv, convID, 0, 0)
	if res.Error != "" {
		t.Fatalf("get_conversation error: %s", res.Error)
	}
	got := parseMessageCount(t, res)
	// Each pair produces 2 SessionMessages (user + assistant text).
	want := pairs * 2
	if got != want {
		t.Errorf("limit=0: got %d messages, want %d (old 50-clamp would return 50)", got, want)
	}

	// --- limit=10 must still return exactly 10 (positive-limit path unchanged) ---
	res10 := sendGetConversation(t, srv, convID, 0, 10)
	if res10.Error != "" {
		t.Fatalf("get_conversation (limit=10) error: %s", res10.Error)
	}
	got10 := parseMessageCount(t, res10)
	if got10 != 10 {
		t.Errorf("limit=10: got %d messages, want 10", got10)
	}

	// --- limit=-1 must also return all (negative → unbounded, same as 0) ---
	resNeg := sendGetConversation(t, srv, convID, 0, -1)
	if resNeg.Error != "" {
		t.Fatalf("get_conversation (limit=-1) error: %s", resNeg.Error)
	}
	gotNeg := parseMessageCount(t, resNeg)
	if gotNeg != want {
		t.Errorf("limit=-1: got %d messages, want %d", gotNeg, want)
	}
}

// TestGetConversation_OffsetAndLimitCombined verifies that offset + positive limit
// together still page correctly (not a regression path, but protects the
// unchanged positive-limit branch).
func TestGetConversation_OffsetAndLimitCombined(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	convDir := filepath.Join(tmpHome, ".ion", "conversations")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatalf("MkdirAll convDir: %v", err)
	}

	// 30 pairs = 60 SessionMessages.
	const pairs = 30
	convID := seedConversation(t, convDir, "test-offset-limit-30pairs", pairs)

	mb := newMockBackend()
	srv := newShortPathTestServer(t, mb)

	// offset=10, limit=15 → messages 10..24
	res := sendGetConversation(t, srv, convID, 10, 15)
	if res.Error != "" {
		t.Fatalf("get_conversation error: %s", res.Error)
	}
	got := parseMessageCount(t, res)
	if got != 15 {
		t.Errorf("offset=10 limit=15: got %d messages, want 15", got)
	}
}
