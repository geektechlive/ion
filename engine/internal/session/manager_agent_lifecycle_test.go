package session

import (
	"context"
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/backend"
	"github.com/dsswift/ion/engine/internal/types"
)

// ---------------------------------------------------------------------------
// Agent lifecycle / engine_agent_state snapshot contract tests
//
// Background: engine_agent_state is a COMPLETE SNAPSHOT of every agent the
// engine considers live. Every code path that terminates an agent's run
// must transition the registry state to a terminal status (done/error/
// cancelled) so the next emitted snapshot is authoritative. Consumers
// (desktop, iOS, headless harnesses) replace their local view with the
// snapshot — they do not merge incremental updates and they do not retain
// entries the engine no longer endorses.
//
// These tests enforce the engine-side half of that contract: no
// termination path may leave a state in "running" once we emit.
//
// See docs/architecture/agent-state.md for the full spec.
// ---------------------------------------------------------------------------

// captureAgentStateEvents installs an event listener on the manager that
// records every engine_agent_state payload (copied so callers can mutate
// safely). Returns a pointer to the slice so the caller can inspect it
// after triggering events.
func captureAgentStateEvents(mgr *Manager) *[]types.EngineEvent {
	var captured []types.EngineEvent
	mgr.OnEvent(func(_ string, ev types.EngineEvent) {
		if ev.Type != "engine_agent_state" {
			return
		}
		// Copy the agents slice so later mutations to the registry don't
		// silently change recorded history.
		copyAgents := make([]types.AgentStateUpdate, len(ev.Agents))
		copy(copyAgents, ev.Agents)
		captured = append(captured, types.EngineEvent{
			Type:   ev.Type,
			Agents: copyAgents,
		})
	})
	return &captured
}

// assertNoRunningInLastSnapshot fails the test if the most recent
// engine_agent_state event still contains any agent with status "running"
// matching the given name.
func assertNoRunningInLastSnapshot(t *testing.T, captured []types.EngineEvent, name string) {
	t.Helper()
	if len(captured) == 0 {
		t.Fatal("expected at least one engine_agent_state event, got none")
	}
	last := captured[len(captured)-1]
	for _, a := range last.Agents {
		if a.Name == name && a.Status == "running" {
			t.Fatalf("agent %q still has status=running in last snapshot %+v", name, last.Agents)
		}
	}
}

// TestAbortAllDescendants_TransitionsEngineStatesToCancelled verifies that
// when the engine aborts descendant agents it not only kills the handles
// but also transitions the corresponding engine-managed state entries to
// "cancelled" — so the snapshot emitted afterward reflects the truth and
// any future ReconcileState call sees terminal status rather than stale
// "running" rows.
func TestAbortAllDescendants_TransitionsEngineStatesToCancelled(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("life-abort", defaultConfig())

	captured := captureAgentStateEvents(mgr)

	mgr.mu.Lock()
	s := mgr.sessions["life-abort"]
	// Register both handle (so ClearHandles finds them) and state (so
	// UpdateState can transition them). Mirrors what prompt_agent_spawner
	// does in production.
	s.agents.RegisterHandle("worker-1", types.AgentHandle{PID: 88881, ParentAgent: ""})
	s.agents.AppendState(types.AgentStateUpdate{
		Name:   "worker-1",
		Status: "running",
		Metadata: map[string]interface{}{
			"displayName": "Worker 1",
			"visibility":  "sticky",
			"invited":     true,
		},
	})
	s.agents.RegisterHandle("worker-2", types.AgentHandle{PID: 88882, ParentAgent: ""})
	s.agents.AppendState(types.AgentStateUpdate{
		Name:   "worker-2",
		Status: "running",
		Metadata: map[string]interface{}{
			"displayName": "Worker 2",
			"visibility":  "sticky",
			"invited":     true,
		},
	})
	mgr.mu.Unlock()

	mgr.abortAllDescendants("life-abort", "test_abort")

	// Last emission must reflect that both agents are cancelled.
	assertNoRunningInLastSnapshot(t, *captured, "worker-1")
	assertNoRunningInLastSnapshot(t, *captured, "worker-2")

	// And the registry itself must now report them as cancelled (so a
	// subsequent ReconcileState would re-broadcast terminal status).
	mgr.mu.RLock()
	snapshot := mgr.sessions["life-abort"].agents.MergedSnapshot()
	mgr.mu.RUnlock()
	if len(snapshot) != 2 {
		t.Fatalf("expected 2 state entries after abort, got %d", len(snapshot))
	}
	for _, a := range snapshot {
		if a.Status != "cancelled" {
			t.Errorf("agent %q status=%q, expected cancelled", a.Name, a.Status)
		}
	}
}

// TestReconcileState_AlwaysEmitsEvenWhenEmpty verifies that ReconcileState
// publishes an engine_agent_state event unconditionally, including the
// empty snapshot. A reconnecting client must always learn the current
// truth — "no agents are live" is as much a fact as "two agents are
// running" and the consumer needs to overwrite stale local state with
// either.
func TestReconcileState_AlwaysEmitsEvenWhenEmpty(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("recon-empty", defaultConfig())

	var emittedAgentState int
	mgr.OnEvent(func(_ string, ev types.EngineEvent) {
		if ev.Type == "engine_agent_state" {
			emittedAgentState++
		}
	})

	// No agents registered — registry snapshot is empty.
	mgr.ReconcileState("recon-empty")

	if emittedAgentState == 0 {
		t.Fatal("expected engine_agent_state event from ReconcileState even when snapshot is empty")
	}
}

// TestReconcileState_EmitsCurrentSnapshot verifies that ReconcileState
// emits whatever the registry currently holds (the contract's "complete
// snapshot" guarantee).
func TestReconcileState_EmitsCurrentSnapshot(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("recon-full", defaultConfig())

	captured := captureAgentStateEvents(mgr)

	mgr.mu.Lock()
	s := mgr.sessions["recon-full"]
	s.agents.AppendState(types.AgentStateUpdate{Name: "a", Status: "running"})
	s.agents.AppendState(types.AgentStateUpdate{Name: "b", Status: "done"})
	mgr.mu.Unlock()

	mgr.ReconcileState("recon-full")

	if len(*captured) == 0 {
		t.Fatal("expected engine_agent_state event from ReconcileState")
	}
	last := (*captured)[len(*captured)-1]
	if len(last.Agents) != 2 {
		t.Errorf("expected 2 agents in reconciled snapshot, got %d: %+v", len(last.Agents), last.Agents)
	}
}

// TestReconcileState_UnknownSessionNoEmit ensures we don't emit for a
// session that doesn't exist — that would confuse clients reconnecting
// to a session that has since been torn down.
func TestReconcileState_UnknownSessionNoEmit(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)

	var emitted bool
	mgr.OnEvent(func(_ string, _ types.EngineEvent) {
		emitted = true
	})

	mgr.ReconcileState("never-existed")

	if emitted {
		t.Fatal("expected no events for unknown session reconcile")
	}
}

// TestAgentLifecycle_ModelFallbackDoesNotPerturbSnapshots locks in the
// snapshot contract across the new ModelFallbackEvent termination path:
// when a dispatched specialist receives a ModelFallbackEvent from the
// child backend before completing, the parent's engine_agent_state
// snapshot sequence must still be exactly the canonical two emissions
// (running at start, done at completion). The fallback event is a
// workflow signal — it must not trigger an extra agent_state snapshot
// nor leave the agent in "running" status after termination.
//
// This pins docs/architecture/agent-state.md against the new termination
// path introduced by the grand-surfing-moth plan.
func TestAgentLifecycle_ModelFallbackDoesNotPerturbSnapshots(t *testing.T) {
	// Stub child emits a ModelFallbackEvent followed by TaskCompleteEvent.
	// The spawner's OnNormalized handler must ignore the fallback event
	// for agent_state purposes and emit exactly two snapshots: running
	// at start, done at completion.
	stub := &childStubBackend{
		resultText:        "specialist done",
		emitModelFallback: true,
	}
	mb := newMockBackend()
	mgr := NewManager(mb)
	mgr.childBackendOverride = func() backend.RunBackend { return stub }

	if _, err := mgr.StartSession("lifecycle-fallback", defaultConfig()); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	captured := captureAgentStateEvents(mgr)

	mgr.mu.Lock()
	s := mgr.sessions["lifecycle-fallback"]
	mgr.mu.Unlock()

	// Hook-capturing group lets the spawner fire agent_start / agent_end
	// without panicking; the test doesn't assert on hook ordering, only
	// on snapshot emissions.
	group, _ := installHookCapturingGroup(t)
	mgr.mu.Lock()
	s.extGroup = group
	mgr.mu.Unlock()

	runCfg := &backend.RunConfig{}
	mgr.wireAgentSpawner(s, "lifecycle-fallback", "claude-opus-4-7", group, runCfg)
	if runCfg.AgentSpawner == nil {
		t.Fatal("AgentSpawner not installed")
	}

	_, spawnErr := runCfg.AgentSpawner(
		context.Background(),
		"specialist",
		"task",
		"",
		"/tmp",
		"claude-sonnet-4-6",
	)
	if spawnErr != nil {
		t.Fatalf("spawner returned error: %v", spawnErr)
	}

	// Give the goroutine a brief moment to flush the final snapshot —
	// the spawner emits agent_state after onExit on its own goroutine,
	// not synchronously with the spawner closure return.
	time.Sleep(50 * time.Millisecond)

	// Snapshot count: the spawner emits one running snapshot at start
	// (reason=agent_start) and one terminal snapshot at end
	// (reason=agent_end). The intervening ModelFallbackEvent must not
	// trigger an additional emission.
	snapshots := *captured
	if len(snapshots) != 2 {
		var summary []string
		for _, snap := range snapshots {
			row := ""
			for _, a := range snap.Agents {
				row += a.Name + "=" + a.Status + " "
			}
			summary = append(summary, "["+row+"]")
		}
		t.Fatalf("expected exactly 2 agent_state snapshots (running → done) across the fallback path, got %d: %v", len(snapshots), summary)
	}

	// The spawner generates the agent name from the unique dispatch ID
	// (e.g. "agent-1") because the test registered no spec named
	// "specialist". Locate by single-entry-snapshot rather than by name.
	if len(snapshots[0].Agents) != 1 {
		t.Fatalf("first snapshot should contain exactly 1 agent, got %d: %+v", len(snapshots[0].Agents), snapshots[0].Agents)
	}
	if len(snapshots[1].Agents) != 1 {
		t.Fatalf("final snapshot should contain exactly 1 agent, got %d: %+v", len(snapshots[1].Agents), snapshots[1].Agents)
	}

	// First snapshot: agent is running.
	if got := snapshots[0].Agents[0].Status; got != "running" {
		t.Errorf("first snapshot status = %q, want %q", got, "running")
	}

	// Final snapshot: agent is done, never orphaned in running.
	finalStatus := snapshots[1].Agents[0].Status
	if finalStatus == "running" {
		t.Errorf("agent still running in final snapshot (snapshot contract violated): %+v", snapshots[1].Agents[0])
	}
	if finalStatus != "done" {
		t.Errorf("final snapshot status = %q, want %q", finalStatus, "done")
	}
}
