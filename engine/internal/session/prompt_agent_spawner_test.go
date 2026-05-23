package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/backend"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/types"
)

// ---------------------------------------------------------------------------
// wireAgentSpawner -> agent_start / agent_end hook wiring tests
//
// These tests lock in the fix for #126: prior to this wiring, FireAgentStart
// and FireAgentEnd had zero call sites in the engine, so user extensions
// that registered agent_start/agent_end handlers received nothing at
// runtime. The spawner closure must fire both hooks on the parent
// extension group for every terminal path -- success, child error, and
// parent cancellation -- so observers can reliably pair start with end.
// ---------------------------------------------------------------------------

// childStubBackend is an in-process RunBackend used to drive the agent
// spawner closure without spinning up a real ApiBackend or CliBackend.
// Callers configure the events it should emit on StartRun and whether
// completion is immediate or gated on an explicit Release() call (for
// cancellation testing).
type childStubBackend struct {
	mu sync.Mutex

	onNorm  func(string, types.NormalizedEvent)
	onExit  func(string, *int, *string, string)
	onError func(string, error)

	// resultText is emitted as TaskCompleteEvent.Result before exit
	// (when releaseGate is nil, i.e. immediate completion).
	resultText string
	// childErr, when non-nil, is delivered via onError after StartRun
	// returns control.
	childErr error
	// releaseGate, when non-nil, holds StartRun's exit emission until
	// Release() (or Cancel) closes the channel. Used for the cancellation
	// path where we need the parent context to expire before the child
	// finishes naturally.
	releaseGate chan struct{}

	startedRequestID string
	cancelCalled     bool
}

func (c *childStubBackend) StartRun(requestID string, _ types.RunOptions) {
	c.mu.Lock()
	c.startedRequestID = requestID
	onNorm := c.onNorm
	onExit := c.onExit
	gate := c.releaseGate
	result := c.resultText
	errToEmit := c.childErr
	c.mu.Unlock()

	go func() {
		// Wait for the gate when the test wants to hold the child open
		// (cancellation path). Nil gate = fire immediately (happy path
		// and child-error path).
		if gate != nil {
			<-gate
		}
		if onNorm != nil && result != "" {
			onNorm(requestID, types.NormalizedEvent{
				Data: &types.TaskCompleteEvent{
					Result:    result,
					SessionID: "child-conv-id",
				},
			})
		}
		if errToEmit != nil {
			c.mu.Lock()
			fn := c.onError
			c.mu.Unlock()
			if fn != nil {
				fn(requestID, errToEmit)
			}
		}
		if onExit != nil {
			zero := 0
			onExit(requestID, &zero, nil, "child-conv-id")
		}
	}()
}

func (c *childStubBackend) Cancel(_ string) bool {
	c.mu.Lock()
	c.cancelCalled = true
	gate := c.releaseGate
	c.mu.Unlock()
	// Release the gate so the StartRun goroutine can complete its
	// exit emission -- the spawner closure blocks waiting for doneCh
	// even after Cancel, mirroring production behavior.
	if gate != nil {
		select {
		case <-gate:
			// already closed
		default:
			close(gate)
		}
	}
	return true
}

func (c *childStubBackend) IsRunning(_ string) bool                                    { return false }
func (c *childStubBackend) WriteToStdin(_ string, _ interface{}) error                 { return nil }
func (c *childStubBackend) FlushConversations()                                        {}
func (c *childStubBackend) OnNormalized(fn func(string, types.NormalizedEvent))        { c.onNorm = fn }
func (c *childStubBackend) OnExit(fn func(string, *int, *string, string))              { c.onExit = fn }
func (c *childStubBackend) OnError(fn func(string, error))                             { c.onError = fn }

// installHookCapturingHost registers in-memory agent_start / agent_end
// handlers on a fresh ExtensionGroup and returns the group together with
// pointers the test can inspect after the spawner runs.
type capturedAgentInfo struct {
	mu        sync.Mutex
	startCalls []extension.AgentInfo
	endCalls   []extension.AgentInfo
}

func (c *capturedAgentInfo) starts() []extension.AgentInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]extension.AgentInfo, len(c.startCalls))
	copy(out, c.startCalls)
	return out
}

func (c *capturedAgentInfo) ends() []extension.AgentInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]extension.AgentInfo, len(c.endCalls))
	copy(out, c.endCalls)
	return out
}

func installHookCapturingGroup(t *testing.T) (*extension.ExtensionGroup, *capturedAgentInfo) {
	t.Helper()
	cap := &capturedAgentInfo{}
	host := extension.NewHost()
	host.SDK().On(extension.HookAgentStart, func(_ *extension.Context, payload interface{}) (interface{}, error) {
		info, ok := payload.(extension.AgentInfo)
		if !ok {
			t.Errorf("agent_start payload not AgentInfo: %T", payload)
			return nil, nil
		}
		cap.mu.Lock()
		cap.startCalls = append(cap.startCalls, info)
		cap.mu.Unlock()
		return nil, nil
	})
	host.SDK().On(extension.HookAgentEnd, func(_ *extension.Context, payload interface{}) (interface{}, error) {
		info, ok := payload.(extension.AgentInfo)
		if !ok {
			t.Errorf("agent_end payload not AgentInfo: %T", payload)
			return nil, nil
		}
		cap.mu.Lock()
		cap.endCalls = append(cap.endCalls, info)
		cap.mu.Unlock()
		return nil, nil
	})
	group := extension.NewExtensionGroup()
	group.Add(host)
	return group, cap
}

// runSpawnerOnce wires the agent spawner with a stub child backend, attaches
// the given extension group to the parent session, then invokes the closure
// once with the supplied parent context and prompt. Returns the spawner's
// result string, the capture of agent_start/agent_end payloads, and the
// spawner's error (error last per Go convention / staticcheck ST1008).
func runSpawnerOnce(t *testing.T, stub *childStubBackend, parentCtx context.Context, prompt string) (string, *capturedAgentInfo, error) {
	t.Helper()
	mb := newMockBackend()
	mgr := NewManager(mb)
	mgr.childBackendOverride = func() backend.RunBackend { return stub }

	if _, err := mgr.StartSession("spawner-test", defaultConfig()); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	mgr.mu.Lock()
	s := mgr.sessions["spawner-test"]
	mgr.mu.Unlock()

	group, cap := installHookCapturingGroup(t)
	mgr.mu.Lock()
	s.extGroup = group
	mgr.mu.Unlock()

	runCfg := &backend.RunConfig{}
	mgr.wireAgentSpawner(s, "spawner-test", "claude-sonnet", group, runCfg)
	if runCfg.AgentSpawner == nil {
		t.Fatal("wireAgentSpawner did not install AgentSpawner closure")
	}

	result, err := runCfg.AgentSpawner(parentCtx, "", prompt, "test-display", "/tmp", "")
	return result, cap, err
}

func TestWireAgentSpawner_FiresAgentStartAndEnd_OnSuccess(t *testing.T) {
	stub := &childStubBackend{resultText: "child output"}
	result, cap, err := runSpawnerOnce(t, stub, context.Background(), "do thing")
	if err != nil {
		t.Fatalf("spawner returned error: %v", err)
	}
	if result != "child output" {
		t.Fatalf("expected child output, got %q", result)
	}

	starts := cap.starts()
	ends := cap.ends()
	if len(starts) != 1 {
		t.Fatalf("expected 1 agent_start, got %d (%+v)", len(starts), starts)
	}
	if len(ends) != 1 {
		t.Fatalf("expected 1 agent_end, got %d (%+v)", len(ends), ends)
	}
	if starts[0].Task != "do thing" {
		t.Errorf("agent_start.Task = %q, want %q", starts[0].Task, "do thing")
	}
	if starts[0].Name == "" {
		t.Error("agent_start.Name must not be empty")
	}
	if ends[0].Name != starts[0].Name {
		t.Errorf("agent_end.Name = %q, want match agent_start.Name %q", ends[0].Name, starts[0].Name)
	}
	if ends[0].Task != "do thing" {
		t.Errorf("agent_end.Task = %q, want %q", ends[0].Task, "do thing")
	}
}

func TestWireAgentSpawner_FiresAgentEnd_OnChildError(t *testing.T) {
	stub := &childStubBackend{childErr: errors.New("child blew up")}
	_, cap, err := runSpawnerOnce(t, stub, context.Background(), "risky task")
	if err == nil || err.Error() != "child blew up" {
		t.Fatalf("expected child error to propagate, got %v", err)
	}

	if got := len(cap.starts()); got != 1 {
		t.Fatalf("expected 1 agent_start on error path, got %d", got)
	}
	if got := len(cap.ends()); got != 1 {
		t.Fatalf("expected 1 agent_end on error path, got %d", got)
	}
}

func TestWireAgentSpawner_FiresAgentEnd_OnCancellation(t *testing.T) {
	// Gate keeps the child "running" until parent cancellation triggers
	// the spawner's Cancel() path, which releases the gate.
	stub := &childStubBackend{
		resultText:  "never delivered",
		releaseGate: make(chan struct{}),
	}

	parentCtx, cancel := context.WithCancel(context.Background())
	// Cancel the parent context after a small delay so the spawner is
	// inside its select{} when cancellation lands.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, cap, err := runSpawnerOnce(t, stub, parentCtx, "cancellable task")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from cancelled spawner, got %v", err)
	}

	if got := len(cap.starts()); got != 1 {
		t.Fatalf("expected 1 agent_start on cancellation path, got %d", got)
	}
	if got := len(cap.ends()); got != 1 {
		t.Fatalf("expected 1 agent_end on cancellation path (lifecycle pairing), got %d", got)
	}
}

// TestWireAgentSpawner_NilExtGroup_DoesNotPanic verifies the
// nil/empty-extGroup guard inside the spawner closure. Sessions without
// extensions are the common case and must not panic on agent dispatch.
func TestWireAgentSpawner_NilExtGroup_DoesNotPanic(t *testing.T) {
	stub := &childStubBackend{resultText: "ok"}
	mb := newMockBackend()
	mgr := NewManager(mb)
	mgr.childBackendOverride = func() backend.RunBackend { return stub }

	if _, err := mgr.StartSession("no-ext", defaultConfig()); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	mgr.mu.Lock()
	s := mgr.sessions["no-ext"]
	mgr.mu.Unlock()

	runCfg := &backend.RunConfig{}
	mgr.wireAgentSpawner(s, "no-ext", "claude-sonnet", nil, runCfg)
	if runCfg.AgentSpawner == nil {
		t.Fatal("AgentSpawner closure was not installed")
	}

	result, err := runCfg.AgentSpawner(context.Background(), "", "task", "", "/tmp", "")
	if err != nil {
		t.Fatalf("spawner returned error with nil extGroup: %v", err)
	}
	if result != "ok" {
		t.Fatalf("expected ok, got %q", result)
	}
}
