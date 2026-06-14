package backend

import (
	"testing"

	"github.com/dsswift/ion/engine/internal/conversation"
	"github.com/dsswift/ion/engine/internal/providers"
	"github.com/dsswift/ion/engine/internal/types"
)

// resolveContextWindow must never return 0 from a registry entry that lacks a
// usable window. A 0 window drove compaction's targetTokens to 0, truncating
// the conversation to nothing every turn (the gpt-4o-mini-via-OpenRouter
// failure). It must fall back to conversation.DefaultContext instead.
func TestResolveContextWindow(t *testing.T) {
	// Registry entry with a real window -> use it verbatim.
	providers.RegisterModel("test-windowed-model", types.ModelInfo{ProviderID: "openrouter", ContextWindow: 128000})
	if got := resolveContextWindow("test-windowed-model"); got != 128000 {
		t.Errorf("windowed model: want 128000, got %d", got)
	}

	// Registry entry with ContextWindow==0 (catalog gap) -> DefaultContext, not 0.
	providers.RegisterModel("test-zero-window-model", types.ModelInfo{ProviderID: "openrouter", ContextWindow: 0})
	if got := resolveContextWindow("test-zero-window-model"); got != conversation.DefaultContext {
		t.Errorf("zero-window model: want DefaultContext=%d, got %d", conversation.DefaultContext, got)
	}

	// Model absent from the registry -> DefaultContext.
	if got := resolveContextWindow("totally-unknown-model-xyz"); got != conversation.DefaultContext {
		t.Errorf("unknown model: want DefaultContext=%d, got %d", conversation.DefaultContext, got)
	}
}
