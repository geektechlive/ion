package agents

import (
	"fmt"
	"sync"
	"testing"

	"github.com/dsswift/ion/engine/internal/types"
)

// --- AppendOrUpdate basic ---

// TestRegistry_AppendOrUpdate verifies the atomic append-or-update method:
// first call appends, second call with same name updates in place, and a
// different name appends separately.
func TestRegistry_AppendOrUpdate(t *testing.T) {
	r := NewRegistry()

	// First call: no existing entry → appends, returns false.
	reused := r.AppendOrUpdate(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "dispatch-1",
		Status: "running",
		Metadata: map[string]interface{}{
			"task": "first task",
		},
	}, func(existing *types.AgentStateUpdate) {
		existing.ID = "dispatch-1"
		existing.Status = "running"
	})

	if reused {
		t.Error("first call should return false (appended, not updated)")
	}
	snap := r.MergedSnapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry after first append, got %d", len(snap))
	}
	if snap[0].Name != "dev-lead" || snap[0].ID != "dispatch-1" {
		t.Errorf("unexpected entry: %+v", snap[0])
	}

	// Second call with same name → updates in place, returns true.
	reused = r.AppendOrUpdate(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "dispatch-2",
		Status: "running",
		Metadata: map[string]interface{}{
			"task": "second task",
		},
	}, func(existing *types.AgentStateUpdate) {
		existing.ID = "dispatch-2"
		existing.Status = "running"
		if existing.Metadata == nil {
			existing.Metadata = map[string]interface{}{}
		}
		existing.Metadata["task"] = "second task"
	})

	if !reused {
		t.Error("second call should return true (updated existing)")
	}
	snap = r.MergedSnapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry after update (no duplicate), got %d", len(snap))
	}
	if snap[0].ID != "dispatch-2" {
		t.Errorf("expected ID dispatch-2 after update, got %s", snap[0].ID)
	}
	if snap[0].Metadata["task"] != "second task" {
		t.Errorf("expected updated task, got %v", snap[0].Metadata["task"])
	}

	// Different name → appends separately.
	reused = r.AppendOrUpdate(types.AgentStateUpdate{
		Name:   "architect",
		ID:     "dispatch-3",
		Status: "running",
	}, func(existing *types.AgentStateUpdate) {
		existing.ID = "dispatch-3"
	})

	if reused {
		t.Error("different name should return false (appended)")
	}
	snap = r.MergedSnapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries after different-name append, got %d", len(snap))
	}
}

// TestRegistry_AppendOrUpdate_PreservesMetadata verifies that the updater
// can selectively modify fields while preserving others (e.g. conversationIds
// from a previous dispatch).
func TestRegistry_AppendOrUpdate_PreservesMetadata(t *testing.T) {
	r := NewRegistry()

	// Initial append with rich metadata.
	r.AppendOrUpdate(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "dispatch-1",
		Status: "done",
		Metadata: map[string]interface{}{
			"displayName":     "Dev Lead",
			"conversationIds": []interface{}{"conv-1"},
			"task":            "first task",
		},
	}, func(existing *types.AgentStateUpdate) {})

	// Re-dispatch: updater modifies task but preserves conversationIds.
	r.AppendOrUpdate(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "dispatch-2",
		Status: "running",
	}, func(existing *types.AgentStateUpdate) {
		existing.ID = "dispatch-2"
		existing.Status = "running"
		existing.Metadata["task"] = "second task"
		existing.Metadata["lastWork"] = ""
		delete(existing.Metadata, "elapsed")
	})

	snap := r.MergedSnapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	s := snap[0]
	if s.ID != "dispatch-2" {
		t.Errorf("expected dispatch-2, got %s", s.ID)
	}
	// conversationIds from first dispatch should be preserved.
	ids, ok := s.Metadata["conversationIds"].([]interface{})
	if !ok || len(ids) != 1 || ids[0] != "conv-1" {
		t.Errorf("expected preserved conversationIds, got %v", s.Metadata["conversationIds"])
	}
	// displayName should be preserved.
	if s.Metadata["displayName"] != "Dev Lead" {
		t.Errorf("expected preserved displayName, got %v", s.Metadata["displayName"])
	}
}

// --- Concurrent AppendOrUpdate ---

// TestRegistry_AppendOrUpdate_Concurrent launches N goroutines all calling
// AppendOrUpdate with the same agent name and asserts that MergedSnapshot
// contains exactly one entry with that name. This test catches the TOCTOU
// race that existed when FindStateIndex + AppendState were separate lock
// acquisitions.
func TestRegistry_AppendOrUpdate_Concurrent(t *testing.T) {
	const goroutines = 100
	r := NewRegistry()

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			r.AppendOrUpdate(types.AgentStateUpdate{
				Name:   "dev-lead",
				ID:     fmt.Sprintf("dispatch-%d", idx),
				Status: "running",
				Metadata: map[string]interface{}{
					"task": fmt.Sprintf("task-%d", idx),
				},
			}, func(existing *types.AgentStateUpdate) {
				existing.ID = fmt.Sprintf("dispatch-%d", idx)
				existing.Status = "running"
				if existing.Metadata == nil {
					existing.Metadata = map[string]interface{}{}
				}
				existing.Metadata["task"] = fmt.Sprintf("task-%d", idx)
			})
		}(i)
	}

	wg.Wait()

	snap := r.MergedSnapshot()
	count := 0
	for _, s := range snap {
		if s.Name == "dev-lead" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 dev-lead entry, got %d (TOCTOU race produced duplicates)", count)
	}
}

// TestRegistry_AppendOrUpdate_ConcurrentDifferentNames verifies that
// concurrent AppendOrUpdate calls with different names each produce
// exactly one entry.
func TestRegistry_AppendOrUpdate_ConcurrentDifferentNames(t *testing.T) {
	const goroutines = 50
	r := NewRegistry()

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("agent-%d", idx)
			r.AppendOrUpdate(types.AgentStateUpdate{
				Name:   name,
				ID:     fmt.Sprintf("dispatch-%d", idx),
				Status: "running",
			}, func(existing *types.AgentStateUpdate) {
				existing.ID = fmt.Sprintf("dispatch-%d", idx)
			})
		}(i)
	}

	wg.Wait()

	snap := r.MergedSnapshot()
	if len(snap) != goroutines {
		t.Errorf("expected %d entries for %d unique names, got %d", goroutines, goroutines, len(snap))
	}
}

// --- ClearRunningStatesExcept ---

// TestRegistry_ClearRunningStatesExcept verifies that the dispatch-aware
// variant of ClearRunningStates preserves running states whose names are
// in the keep set while clearing all other running states. Terminal states
// (done/error/cancelled) are always preserved regardless of the keep set.
func TestRegistry_ClearRunningStatesExcept(t *testing.T) {
	r := NewRegistry()

	// Mix of running, done, and error states.
	r.AppendState(types.AgentStateUpdate{Name: "bg-agent-1", Status: "running"})
	r.AppendState(types.AgentStateUpdate{Name: "bg-agent-2", Status: "running"})
	r.AppendState(types.AgentStateUpdate{Name: "orphan", Status: "running"})
	r.AppendState(types.AgentStateUpdate{Name: "completed-agent", Status: "done"})
	r.AppendState(types.AgentStateUpdate{Name: "failed-agent", Status: "error"})

	// Keep only bg-agent-1 and bg-agent-2 (active dispatches).
	keep := map[string]bool{"bg-agent-1": true, "bg-agent-2": true}
	r.ClearRunningStatesExcept(keep)

	snap := r.MergedSnapshot()
	// Expected: bg-agent-1 (kept), bg-agent-2 (kept), completed-agent (terminal),
	// failed-agent (terminal). orphan should be removed.
	if len(snap) != 4 {
		t.Fatalf("expected 4 entries, got %d: %v", len(snap), snap)
	}

	names := make(map[string]bool)
	for _, s := range snap {
		names[s.Name] = true
	}
	if names["orphan"] {
		t.Error("orphan running state should have been cleared")
	}
	if !names["bg-agent-1"] || !names["bg-agent-2"] {
		t.Error("kept background agents should be preserved")
	}
	if !names["completed-agent"] || !names["failed-agent"] {
		t.Error("terminal states should always be preserved")
	}
}

// TestRegistry_ClearRunningStatesExcept_EmptyKeep verifies that an empty
// keep set behaves identically to ClearRunningStates (removes all running).
func TestRegistry_ClearRunningStatesExcept_EmptyKeep(t *testing.T) {
	r := NewRegistry()

	r.AppendState(types.AgentStateUpdate{Name: "agent-a", Status: "running"})
	r.AppendState(types.AgentStateUpdate{Name: "agent-b", Status: "done"})

	r.ClearRunningStatesExcept(map[string]bool{})

	snap := r.MergedSnapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry (only terminal), got %d", len(snap))
	}
	if snap[0].Name != "agent-b" {
		t.Errorf("expected agent-b (done), got %s", snap[0].Name)
	}
}

// --- LookupExtDisplayName ---

// TestRegistry_LookupExtDisplayName verifies that LookupExtDisplayName
// searches the cached extension roster states and returns the displayName
// metadata value when present.
func TestRegistry_LookupExtDisplayName(t *testing.T) {
	t.Run("returns display name when present", func(t *testing.T) {
		r := NewRegistry()
		r.CacheExtStates([]types.AgentStateUpdate{
			{
				Name: "cloud-architect",
				Metadata: map[string]interface{}{
					"displayName": "Cloud Architect",
				},
			},
		})

		got := r.LookupExtDisplayName("cloud-architect")
		if got != "Cloud Architect" {
			t.Errorf("expected %q, got %q", "Cloud Architect", got)
		}
	})

	t.Run("returns empty when no displayName key", func(t *testing.T) {
		r := NewRegistry()
		r.CacheExtStates([]types.AgentStateUpdate{
			{
				Name:     "dev-lead",
				Metadata: map[string]interface{}{"type": "agent"},
			},
		})

		got := r.LookupExtDisplayName("dev-lead")
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("returns empty when name not found", func(t *testing.T) {
		r := NewRegistry()
		r.CacheExtStates([]types.AgentStateUpdate{
			{
				Name: "cloud-architect",
				Metadata: map[string]interface{}{
					"displayName": "Cloud Architect",
				},
			},
		})

		got := r.LookupExtDisplayName("nonexistent")
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
}

// --- AppendOrUpdateByID ---

// TestRegistry_AppendOrUpdateByID_ConcurrentSameName is the regression test for
// the phantom "running" agent bug: two concurrent dispatches of the same agent
// name (e.g. "engine-dev") with DIFFERENT dispatch IDs must each get their own
// slot, so UpdateStateByID lands on the correct instance when each dispatch's
// agent_end fires. Under the old name-keyed AppendOrUpdate, the second dispatch
// overwrote the first's ID, making the first's terminal UpdateStateByID miss
// entirely. The snapshot kept one slot stuck as "running" forever, causing the
// desktop "waiting for N background agents" indicator to never clear.
//
// This test MUST fail on name-keyed code (AppendOrUpdate) and pass on ID-keyed
// code (AppendOrUpdateByID). The red-then-green verification is documented in
// the commit message.
func TestRegistry_AppendOrUpdateByID_ConcurrentSameName(t *testing.T) {
	r := NewRegistry()

	// Two dispatches of "engine-dev" with different IDs, both running.
	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "engine-dev",
		ID:     "dispatch-A",
		Status: "running",
		Metadata: map[string]interface{}{
			"task": "implement feature X",
		},
	}, func(existing *types.AgentStateUpdate) {
		existing.Name = "engine-dev"
		existing.Status = "running"
	})

	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "engine-dev",
		ID:     "dispatch-B",
		Status: "running",
		Metadata: map[string]interface{}{
			"task": "implement feature Y",
		},
	}, func(existing *types.AgentStateUpdate) {
		existing.Name = "engine-dev"
		existing.Status = "running"
	})

	// Must have TWO slots, not one.
	snap := r.MergedSnapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 slots for concurrent same-name dispatches, got %d", len(snap))
	}

	// Both should be running.
	runningCount := 0
	for _, s := range snap {
		if s.Status == "running" {
			runningCount++
		}
	}
	if runningCount != 2 {
		t.Fatalf("expected 2 running, got %d", runningCount)
	}

	// Dispatch A finishes: UpdateStateByID(A, done).
	r.UpdateStateByID("dispatch-A", func(state *types.AgentStateUpdate) {
		state.Status = "done"
	})

	snap = r.MergedSnapshot()
	// A should be done, B should still be running.
	var aFound, bFound bool
	for _, s := range snap {
		if s.ID == "dispatch-A" {
			aFound = true
			if s.Status != "done" {
				t.Errorf("dispatch-A status = %q, want done", s.Status)
			}
		}
		if s.ID == "dispatch-B" {
			bFound = true
			if s.Status != "running" {
				t.Errorf("dispatch-B status = %q, want running", s.Status)
			}
		}
	}
	if !aFound {
		t.Error("dispatch-A not found in snapshot after UpdateStateByID")
	}
	if !bFound {
		t.Error("dispatch-B not found in snapshot after UpdateStateByID")
	}

	// Dispatch B finishes: UpdateStateByID(B, done).
	r.UpdateStateByID("dispatch-B", func(state *types.AgentStateUpdate) {
		state.Status = "done"
	})

	snap = r.MergedSnapshot()
	runningCount = 0
	for _, s := range snap {
		if s.Status == "running" {
			runningCount++
		}
	}
	if runningCount != 0 {
		t.Errorf("expected 0 running after both done, got %d (phantom running bug)", runningCount)
	}
}

// TestRegistry_AppendOrUpdateByID_SingleDispatchLifecycle verifies the simple
// case: a single dispatch start -> done leaves zero running agents.
func TestRegistry_AppendOrUpdateByID_SingleDispatchLifecycle(t *testing.T) {
	r := NewRegistry()

	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "docs-lead",
		ID:     "dispatch-D1",
		Status: "running",
		Metadata: map[string]interface{}{
			"task": "update docs",
		},
	}, func(existing *types.AgentStateUpdate) {
		existing.Name = "docs-lead"
		existing.Status = "running"
	})

	snap := r.MergedSnapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(snap))
	}
	if snap[0].Status != "running" {
		t.Errorf("status = %q, want running", snap[0].Status)
	}

	// Finish the dispatch.
	r.UpdateStateByID("dispatch-D1", func(state *types.AgentStateUpdate) {
		state.Status = "done"
	})

	snap = r.MergedSnapshot()
	runningCount := 0
	for _, s := range snap {
		if s.Status == "running" {
			runningCount++
		}
	}
	if runningCount != 0 {
		t.Errorf("expected 0 running after done, got %d", runningCount)
	}
}

// TestRegistry_UpdateState_AllMatches verifies that UpdateState applies the
// updater to ALL entries with the given name (not just the first), which is
// necessary for the abort path when multiple ID-keyed slots share a name.
func TestRegistry_UpdateState_AllMatches(t *testing.T) {
	r := NewRegistry()

	// Two slots with the same name (ID-keyed dispatch path).
	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "engine-dev",
		ID:     "dispatch-A",
		Status: "running",
	}, func(existing *types.AgentStateUpdate) {})

	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "engine-dev",
		ID:     "dispatch-B",
		Status: "running",
	}, func(existing *types.AgentStateUpdate) {})

	// UpdateState by name should hit both.
	r.UpdateState("engine-dev", func(state *types.AgentStateUpdate) {
		state.Status = "cancelled"
	})

	snap := r.MergedSnapshot()
	for _, s := range snap {
		if s.Name == "engine-dev" && s.Status != "cancelled" {
			t.Errorf("slot id=%s status=%q, want cancelled (abort must cancel all)", s.ID, s.Status)
		}
	}
}
