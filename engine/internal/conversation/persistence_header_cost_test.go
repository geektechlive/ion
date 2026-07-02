package conversation

// persistence_header_cost_test.go — tests for LoadLlmHeaderCost, the
// lightweight header-only reader used by the aggregate-cost walk.

import (
	"errors"
	"testing"

	"github.com/dsswift/ion/engine/internal/types"
)

// TestLoadLlmHeaderCost_FreshConversation verifies a conversation with no
// recorded cost returns (0, nil) — a fresh conversation has no persisted cost,
// which is not an error.
func TestLoadLlmHeaderCost_FreshConversation(t *testing.T) {
	dir := t.TempDir()
	id := "fresh-cost"
	conv := CreateConversation(id, "system", "claude-sonnet-4-6")
	AddUserMessage(conv, "hello")
	if err := Save(conv, dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cost, err := LoadLlmHeaderCost(id, dir)
	if err != nil {
		t.Fatalf("LoadLlmHeaderCost: %v", err)
	}
	if cost != 0 {
		t.Errorf("cost = %f, want 0 for fresh conversation", cost)
	}
}

// TestLoadLlmHeaderCost_WithCost verifies a conversation with a recorded
// TotalCost reads that cost back from the header.
func TestLoadLlmHeaderCost_WithCost(t *testing.T) {
	dir := t.TempDir()
	id := "with-cost"
	conv := CreateConversation(id, "system", "claude-sonnet-4-6")
	AddUserMessage(conv, "hello")
	AddAssistantMessage(conv, []types.LlmContentBlock{{Type: "text", Text: "hi"}},
		types.LlmUsage{InputTokens: 100, OutputTokens: 50})
	conv.TotalCost = 0.1234
	if err := Save(conv, dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cost, err := LoadLlmHeaderCost(id, dir)
	if err != nil {
		t.Fatalf("LoadLlmHeaderCost: %v", err)
	}
	if cost < 0.1233 || cost > 0.1235 {
		t.Errorf("cost = %f, want ~0.1234", cost)
	}
}

// TestLoadLlmHeaderCost_NotFound verifies a missing conversation returns
// ErrNotFound.
func TestLoadLlmHeaderCost_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadLlmHeaderCost("does-not-exist", dir)
	if err == nil {
		t.Fatal("expected error for missing conversation, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
