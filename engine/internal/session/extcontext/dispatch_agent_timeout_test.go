package extcontext

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/backend"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/mcp"
	"github.com/dsswift/ion/engine/internal/types"
)

// hangingBackend is a RunBackend that never calls OnExit, simulating a child
// that hangs indefinitely. Cancel records the call so tests can assert it.
type hangingBackend struct {
	mu         sync.Mutex
	onExitF    func(string, *int, *string, string)
	cancelled  atomic.Bool
}

func (b *hangingBackend) StartRun(_ string, _ types.RunOptions) {}

func (b *hangingBackend) Cancel(_ string) bool {
	b.cancelled.Store(true)
	// Fire OnExit so the bridge goroutine inside BuildDispatchAgentFunc can
	// unblock and the WaitGroup completes. Without this the bridge goroutine
	// would be leaked, which is acceptable in production (the backend contract),
	// but in tests we must clean up to avoid goroutine leaks that affect the
	// test runner.
	b.mu.Lock()
	fn := b.onExitF
	b.mu.Unlock()
	if fn != nil {
		go fn("", nil, nil, "")
	}
	return true
}

func (b *hangingBackend) IsRunning(_ string) bool         { return true }
func (b *hangingBackend) WriteToStdin(_ string, _ interface{}) error { return nil }
func (b *hangingBackend) FlushConversations()             {}

func (b *hangingBackend) OnNormalized(_ func(string, types.NormalizedEvent)) {}

func (b *hangingBackend) OnExit(fn func(string, *int, *string, string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onExitF = fn
}

func (b *hangingBackend) OnError(_ func(string, error)) {}

// minimalSessionAccessor satisfies SessionAccessor with a configurable
// NewChildBackend so tests can inject the hangingBackend.
type minimalSessionAccessor struct {
	childBackend backend.RunBackend
}

func (s *minimalSessionAccessor) SessionKey() string      { return "test-session" }
func (s *minimalSessionAccessor) WorkingDirectory() string { return "/tmp" }
func (s *minimalSessionAccessor) Emit(_ types.EngineEvent) {}
func (s *minimalSessionAccessor) SendAbort()               {}
func (s *minimalSessionAccessor) SendPrompt(_ string, _ string) error { return nil }
func (s *minimalSessionAccessor) Elicit(_ extension.ElicitationRequestInfo) (map[string]interface{}, bool, error) {
	return nil, false, nil
}
func (s *minimalSessionAccessor) SuppressTool(_ string)                               {}
func (s *minimalSessionAccessor) CacheExtAgentStates(_ []types.AgentStateUpdate)      {}
func (s *minimalSessionAccessor) RegisterAgent(_ string, _ types.AgentHandle)         {}
func (s *minimalSessionAccessor) DeregisterAgent(_ string)                            {}
func (s *minimalSessionAccessor) RegisterAgentSpec(_ types.AgentSpec)                 {}
func (s *minimalSessionAccessor) DeregisterAgentSpec(_ string)                        {}
func (s *minimalSessionAccessor) LookupAgentSpec(_ string) (types.AgentSpec, bool)    { return types.AgentSpec{}, false }
func (s *minimalSessionAccessor) ExtGroup() *extension.ExtensionGroup                 { return nil }
func (s *minimalSessionAccessor) ExtConfig() *extension.ExtensionConfig               { return nil }
func (s *minimalSessionAccessor) ProcRegistry() *extension.ProcessRegistry            { return nil }
func (s *minimalSessionAccessor) NewChildBackend() backend.RunBackend                 { return s.childBackend }
func (s *minimalSessionAccessor) EngineConfig() *types.EngineRuntimeConfig            { return nil }
func (s *minimalSessionAccessor) ResolveTier(_ string) string                         { return "" }
func (s *minimalSessionAccessor) PermissionCheck(_ string, _ map[string]interface{}) (string, string) {
	return "allow", ""
}
func (s *minimalSessionAccessor) McpConnections() []*mcp.Connection { return nil }
func (s *minimalSessionAccessor) SearchHistory(_ string, _ int) []extension.HistoryMatch {
	return nil
}
func (s *minimalSessionAccessor) TranslateEvent(_ types.NormalizedEvent, _ int) types.EngineEvent {
	return types.EngineEvent{}
}

// TestDispatchAgentFunc_TimeoutCancelsChild verifies that when opts.Context
// carries a short deadline, BuildDispatchAgentFunc:
//   - returns within the expected window (50ms deadline + 50ms slack),
//   - returns an error containing "timed out",
//   - calls Cancel on the child backend.
func TestDispatchAgentFunc_TimeoutCancelsChild(t *testing.T) {
	child := &hangingBackend{}
	sa := &minimalSessionAccessor{childBackend: child}

	dispatchFn := BuildDispatchAgentFunc(sa)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := dispatchFn(extension.DispatchAgentOpts{
		Name:    "test-agent",
		Task:    "do something",
		Context: ctx,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error from timed-out dispatch, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected error to contain 'timed out', got: %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("expected dispatch to return within 200ms, took %v", elapsed)
	}
	if !child.cancelled.Load() {
		t.Error("expected child backend Cancel to be called on timeout")
	}
}
