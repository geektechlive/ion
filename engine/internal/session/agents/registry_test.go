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

// --- ClearRunningStatesExceptIDs ---

// TestRegistry_ClearRunningStatesExceptIDs verifies the ID-keyed retention:
// running slots whose unique ID is in the keep set survive; running slots whose
// ID is absent are swept; terminal slots always survive regardless of the keep
// set. This pins the fix for the nested-dispatch defect where a depth-2
// dispatch's running slot was swept by name-keyed retention and its terminal
// UpdateStateByID then landed nowhere ("agent stuck running").
func TestRegistry_ClearRunningStatesExceptIDs(t *testing.T) {
	r := NewRegistry()

	// Two running dispatches that share a name but have distinct IDs (the
	// nested / concurrent same-name case that name-keying collapses), plus an
	// orphan running slot and two terminal slots.
	r.AppendState(types.AgentStateUpdate{Name: "engine-dev", ID: "dispatch-engine-dev-keep", Status: "running"})
	r.AppendState(types.AgentStateUpdate{Name: "engine-dev", ID: "dispatch-engine-dev-sweep", Status: "running"})
	r.AppendState(types.AgentStateUpdate{Name: "orphan", ID: "dispatch-orphan", Status: "running"})
	r.AppendState(types.AgentStateUpdate{Name: "completed", ID: "dispatch-completed", Status: "done"})
	r.AppendState(types.AgentStateUpdate{Name: "failed", ID: "dispatch-failed", Status: "error"})

	// Keep only one of the two same-name running dispatches, by ID.
	r.ClearRunningStatesExceptIDs(map[string]bool{"dispatch-engine-dev-keep": true})

	// Inspect the raw store directly for ID-level assertions, since the merged
	// projection groups same-name entries.
	got := rawStateIDs(r)
	if !got["dispatch-engine-dev-keep"] {
		t.Error("running slot with kept ID should be preserved")
	}
	if got["dispatch-engine-dev-sweep"] {
		t.Error("running slot whose ID is not in the keep set should be swept (same name as a kept slot must not save it)")
	}
	if got["dispatch-orphan"] {
		t.Error("orphan running slot should be swept")
	}
	if !got["dispatch-completed"] || !got["dispatch-failed"] {
		t.Error("terminal slots must always survive regardless of the keep set")
	}
}

// TestRegistry_ClearRunningStatesExceptIDs_EmptyKeep verifies an empty keep set
// sweeps every running slot (identical to ClearRunningStates) while preserving
// terminal slots.
func TestRegistry_ClearRunningStatesExceptIDs_EmptyKeep(t *testing.T) {
	r := NewRegistry()
	r.AppendState(types.AgentStateUpdate{Name: "a", ID: "id-a", Status: "running"})
	r.AppendState(types.AgentStateUpdate{Name: "b", ID: "id-b", Status: "done"})

	r.ClearRunningStatesExceptIDs(map[string]bool{})

	got := rawStateIDs(r)
	if got["id-a"] {
		t.Error("running slot should be swept under empty keep set")
	}
	if !got["id-b"] {
		t.Error("terminal slot should survive empty keep set")
	}
}

// TestRegistry_ClearRunningStatesExceptIDsOrNames verifies the combined
// preservation: a running slot survives if its ID is in keepIDs OR its name is
// in keepNames, evaluated atomically. ID covers engine dispatch slots; name
// covers extension-roster rows that carry no engine dispatch ID.
func TestRegistry_ClearRunningStatesExceptIDsOrNames(t *testing.T) {
	r := NewRegistry()
	// Engine dispatch slot kept by ID (its name is NOT in keepNames).
	r.AppendState(types.AgentStateUpdate{Name: "engine-dev", ID: "dispatch-keep-by-id", Status: "running"})
	// Extension-roster row kept by name (it carries no dispatch ID).
	r.AppendState(types.AgentStateUpdate{Name: "roster-agent", ID: "", Status: "running"})
	// Running slot matching neither set -> swept.
	r.AppendState(types.AgentStateUpdate{Name: "stale", ID: "dispatch-stale", Status: "running"})
	// Terminal slot -> always survives.
	r.AppendState(types.AgentStateUpdate{Name: "done-agent", ID: "dispatch-done", Status: "done"})

	r.ClearRunningStatesExceptIDsOrNames(
		map[string]bool{"dispatch-keep-by-id": true},
		map[string]bool{"roster-agent": true},
	)

	got := rawStateNames(r)
	if !got["engine-dev"] {
		t.Error("dispatch slot kept by ID should survive (name not in keepNames)")
	}
	if !got["roster-agent"] {
		t.Error("roster row kept by name should survive (no dispatch ID)")
	}
	if got["stale"] {
		t.Error("running slot matching neither set should be swept")
	}
	if !got["done-agent"] {
		t.Error("terminal slot must always survive")
	}
}

// rawStateIDs returns the set of state IDs currently in the registry's
// underlying store (pre-merge), for ID-level assertions that the grouped
// MergedSnapshot projection would obscure.
func rawStateIDs(r *Registry) map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]bool, len(r.states))
	for _, s := range r.states {
		out[s.ID] = true
	}
	return out
}

// rawStateNames returns the set of state names currently in the registry's
// underlying store (pre-merge).
func rawStateNames(r *Registry) map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]bool, len(r.states))
	for _, s := range r.states {
		out[s.Name] = true
	}
	return out
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
// internal slot, so UpdateStateByID lands on the correct instance when each
// dispatch's agent_end fires.
//
// Under the old name-keyed AppendOrUpdate, the second dispatch overwrote the
// first's ID, making the first's terminal UpdateStateByID miss entirely. The
// snapshot kept one slot stuck as "running" forever, causing the desktop
// "waiting for N background agents" indicator to never clear.
//
// MergedSnapshot groups same-name entries into one AgentStateUpdate for emission,
// but the internal store stays ID-keyed. This test verifies the invariant:
// UpdateStateByID targets individual dispatches correctly, both reach terminal,
// and the grouped snapshot reflects the most-active status at each step.
func TestRegistry_AppendOrUpdateByID_ConcurrentSameName(t *testing.T) {
	r := NewRegistry()

	// Two dispatches of "engine-dev" with different IDs, both running.
	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "engine-dev",
		ID:     "dispatch-A",
		Status: "running",
		Metadata: map[string]interface{}{
			"task":       "implement feature X",
			"dispatches": []interface{}{map[string]interface{}{"id": "dispatch-A", "status": "running"}},
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
			"task":       "implement feature Y",
			"dispatches": []interface{}{map[string]interface{}{"id": "dispatch-B", "status": "running"}},
		},
	}, func(existing *types.AgentStateUpdate) {
		existing.Name = "engine-dev"
		existing.Status = "running"
	})

	// MergedSnapshot groups by name: one entry for "engine-dev".
	snap := r.MergedSnapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 grouped entry for same-name dispatches, got %d", len(snap))
	}
	if snap[0].Name != "engine-dev" {
		t.Errorf("grouped name = %q, want engine-dev", snap[0].Name)
	}
	if snap[0].Status != "running" {
		t.Errorf("grouped status = %q, want running (most-active)", snap[0].Status)
	}

	// Grouped entry should have both dispatches merged.
	dispatches, ok := snap[0].Metadata["dispatches"].([]interface{})
	if !ok {
		t.Fatalf("dispatches not found in grouped metadata")
	}
	if len(dispatches) != 2 {
		t.Fatalf("expected 2 dispatches in grouped entry, got %d", len(dispatches))
	}

	// Dispatch A finishes: UpdateStateByID(A, done).
	r.UpdateStateByID("dispatch-A", func(state *types.AgentStateUpdate) {
		state.Status = "done"
	})

	snap = r.MergedSnapshot()
	// Grouped: one entry, status "running" (B still running > A done).
	if len(snap) != 1 {
		t.Fatalf("expected 1 grouped entry, got %d", len(snap))
	}
	if snap[0].Status != "running" {
		t.Errorf("grouped status after A done = %q, want running (B still active)", snap[0].Status)
	}

	// Dispatch B finishes: UpdateStateByID(B, done).
	r.UpdateStateByID("dispatch-B", func(state *types.AgentStateUpdate) {
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

// --- Name-grouping projection tests ---

// TestRegistry_MergedSnapshot_NameGrouping verifies the name-grouping projection
// in MergedSnapshot: two dispatches of the same agent name produce ONE
// AgentStateUpdate in the emitted snapshot with merged dispatches[] and the
// most-active status as representative.
func TestRegistry_MergedSnapshot_NameGrouping(t *testing.T) {
	r := NewRegistry()

	// Register two dispatches of "dev-lead" with different IDs.
	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "dev-lead-1",
		Status: "done",
		Metadata: map[string]interface{}{
			"displayName": "Dev Lead",
			"task":        "first task",
			"model":       "claude-sonnet",
			"dispatches": []interface{}{
				map[string]interface{}{"id": "dev-lead-1", "task": "first task", "status": "done"},
			},
		},
	}, func(existing *types.AgentStateUpdate) {
		existing.Status = "done"
	})

	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "dev-lead-2",
		Status: "running",
		Metadata: map[string]interface{}{
			"displayName": "Dev Lead",
			"task":        "second task",
			"model":       "claude-opus",
			"lastWork":    "implementing feature",
			"dispatches": []interface{}{
				map[string]interface{}{"id": "dev-lead-2", "task": "second task", "status": "running"},
			},
		},
	}, func(existing *types.AgentStateUpdate) {
		existing.Status = "running"
	})

	snap := r.MergedSnapshot()

	// Must produce exactly ONE entry for "dev-lead".
	if len(snap) != 1 {
		t.Fatalf("expected 1 grouped entry, got %d", len(snap))
	}

	entry := snap[0]
	if entry.Name != "dev-lead" {
		t.Errorf("name = %q, want dev-lead", entry.Name)
	}

	// Most-active status: running > done.
	if entry.Status != "running" {
		t.Errorf("status = %q, want running (most-active)", entry.Status)
	}

	// Metadata should come from the most-active entry (dev-lead-2).
	if entry.Metadata["lastWork"] != "implementing feature" {
		t.Errorf("expected lastWork from most-active entry, got %v", entry.Metadata["lastWork"])
	}

	// dispatches[] must contain BOTH dispatch entries.
	dispatches, ok := entry.Metadata["dispatches"].([]interface{})
	if !ok {
		t.Fatalf("dispatches not found in metadata: %v", entry.Metadata)
	}
	if len(dispatches) != 2 {
		t.Fatalf("expected 2 dispatches, got %d", len(dispatches))
	}

	// Verify dispatch IDs in order.
	d0, _ := dispatches[0].(map[string]interface{})
	d1, _ := dispatches[1].(map[string]interface{})
	if d0["id"] != "dev-lead-1" {
		t.Errorf("first dispatch id = %v, want dev-lead-1", d0["id"])
	}
	if d1["id"] != "dev-lead-2" {
		t.Errorf("second dispatch id = %v, want dev-lead-2", d1["id"])
	}
}

// TestRegistry_MergedSnapshot_GroupingDoesNotMutateStore verifies that the
// name-grouping projection in MergedSnapshot is build-only: the internal
// r.states store stays ID-keyed, UpdateStateByID still targets individual
// slots, and a re-snapshot reflects the update correctly.
func TestRegistry_MergedSnapshot_GroupingDoesNotMutateStore(t *testing.T) {
	r := NewRegistry()

	// Register two same-name dispatches.
	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "dev-lead-1",
		Status: "running",
		Metadata: map[string]interface{}{
			"task": "task A",
			"dispatches": []interface{}{
				map[string]interface{}{"id": "dev-lead-1", "status": "running"},
			},
		},
	}, func(existing *types.AgentStateUpdate) {
		existing.Status = "running"
	})

	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "dev-lead-2",
		Status: "running",
		Metadata: map[string]interface{}{
			"task": "task B",
			"dispatches": []interface{}{
				map[string]interface{}{"id": "dev-lead-2", "status": "running"},
			},
		},
	}, func(existing *types.AgentStateUpdate) {
		existing.Status = "running"
	})

	// Take a snapshot (triggers grouping).
	snap1 := r.MergedSnapshot()
	if len(snap1) != 1 {
		t.Fatalf("expected 1 grouped entry, got %d", len(snap1))
	}

	// Mutate the snapshot's dispatches to prove it's a copy.
	if d, ok := snap1[0].Metadata["dispatches"].([]interface{}); ok {
		d[0] = map[string]interface{}{"id": "CORRUPTED"}
	}

	// UpdateStateByID should still work on the internal store.
	r.UpdateStateByID("dev-lead-1", func(state *types.AgentStateUpdate) {
		state.Status = "done"
	})

	// Re-snapshot: dev-lead-1 should be done, dev-lead-2 still running.
	snap2 := r.MergedSnapshot()
	if len(snap2) != 1 {
		t.Fatalf("expected 1 grouped entry on re-snapshot, got %d", len(snap2))
	}
	if snap2[0].Status != "running" {
		t.Errorf("grouped status = %q, want running (dev-lead-2 still active)", snap2[0].Status)
	}

	// Dispatches from the internal store should be intact (not corrupted).
	dispatches, ok := snap2[0].Metadata["dispatches"].([]interface{})
	if !ok {
		t.Fatalf("dispatches missing on re-snapshot")
	}
	if len(dispatches) != 2 {
		t.Fatalf("expected 2 dispatches on re-snapshot, got %d", len(dispatches))
	}
	d0, _ := dispatches[0].(map[string]interface{})
	if d0["id"] == "CORRUPTED" {
		t.Error("snapshot mutation leaked into internal store (not a true copy)")
	}
	if d0["id"] != "dev-lead-1" {
		t.Errorf("first dispatch id = %v, want dev-lead-1", d0["id"])
	}

	// Finish dev-lead-2 too.
	r.UpdateStateByID("dev-lead-2", func(state *types.AgentStateUpdate) {
		state.Status = "done"
	})

	snap3 := r.MergedSnapshot()
	if len(snap3) != 1 {
		t.Fatalf("expected 1 grouped entry, got %d", len(snap3))
	}
	if snap3[0].Status != "done" {
		t.Errorf("grouped status = %q, want done (both finished)", snap3[0].Status)
	}
}

// TestRegistry_MergedSnapshot_DifferentNamesNotGrouped verifies that entries
// with different names are NOT grouped, only same-name entries are merged.
func TestRegistry_MergedSnapshot_DifferentNamesNotGrouped(t *testing.T) {
	r := NewRegistry()

	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "d1",
		Status: "running",
		Metadata: map[string]interface{}{
			"dispatches": []interface{}{map[string]interface{}{"id": "d1"}},
		},
	}, func(existing *types.AgentStateUpdate) {})

	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "architect",
		ID:     "d2",
		Status: "done",
		Metadata: map[string]interface{}{
			"dispatches": []interface{}{map[string]interface{}{"id": "d2"}},
		},
	}, func(existing *types.AgentStateUpdate) {})

	snap := r.MergedSnapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries for different names, got %d", len(snap))
	}

	names := map[string]bool{}
	for _, s := range snap {
		names[s.Name] = true
	}
	if !names["dev-lead"] || !names["architect"] {
		t.Errorf("expected both dev-lead and architect, got %v", names)
	}
}

// TestRegistry_MergedSnapshot_StatusPriority verifies that the most-active
// status wins when merging same-name entries: running > error > done > cancelled.
func TestRegistry_MergedSnapshot_StatusPriority(t *testing.T) {
	tests := []struct {
		name     string
		statuses []string
		want     string
	}{
		{"running wins over done", []string{"done", "running"}, "running"},
		{"running wins over error", []string{"error", "running"}, "running"},
		{"error wins over done", []string{"done", "error"}, "error"},
		{"done wins over cancelled", []string{"cancelled", "done"}, "done"},
		{"running wins over all", []string{"cancelled", "done", "error", "running"}, "running"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			for i, status := range tt.statuses {
				r.AppendOrUpdateByID(types.AgentStateUpdate{
					Name:   "agent",
					ID:     fmt.Sprintf("d-%d", i),
					Status: status,
				}, func(existing *types.AgentStateUpdate) {})
			}
			snap := r.MergedSnapshot()
			if len(snap) != 1 {
				t.Fatalf("expected 1 grouped entry, got %d", len(snap))
			}
			if snap[0].Status != tt.want {
				t.Errorf("status = %q, want %q", snap[0].Status, tt.want)
			}
		})
	}
}

// TestRegistry_MergedSnapshot_GroupingDedupsByID verifies that groupByName
// de-duplicates the merged dispatches[] array by each entry's stable "id".
// This pins the fix for the persist -> rehydrate -> regroup amplification:
// two same-name slots whose arrays overlap on an id must yield a grouped row
// whose dispatches[] length equals the count of DISTINCT ids, not the sum.
func TestRegistry_MergedSnapshot_GroupingDedupsByID(t *testing.T) {
	r := NewRegistry()

	// Slot A holds ids d1, d2.
	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "d1",
		Status: "done",
		Metadata: map[string]interface{}{
			"dispatches": []interface{}{
				map[string]interface{}{"id": "d1", "status": "done"},
				map[string]interface{}{"id": "d2", "status": "done"},
			},
		},
	}, func(existing *types.AgentStateUpdate) {})

	// Slot B holds ids d2 (overlap), d3 — the duplication an amplified
	// rehydrate would have produced.
	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "d3",
		Status: "running",
		Metadata: map[string]interface{}{
			"dispatches": []interface{}{
				map[string]interface{}{"id": "d2", "status": "done"},
				map[string]interface{}{"id": "d3", "status": "running"},
			},
		},
	}, func(existing *types.AgentStateUpdate) {})

	snap := r.MergedSnapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 grouped entry, got %d", len(snap))
	}
	dispatches, ok := snap[0].Metadata["dispatches"].([]interface{})
	if !ok {
		t.Fatalf("dispatches not found: %v", snap[0].Metadata)
	}
	// Distinct ids are {d1, d2, d3} = 3, NOT the raw concatenation of 4.
	if len(dispatches) != 3 {
		t.Fatalf("expected 3 distinct dispatches, got %d: %v", len(dispatches), dispatches)
	}
	// First-seen order preserved: d1, d2, d3.
	wantOrder := []string{"d1", "d2", "d3"}
	for i, want := range wantOrder {
		m, _ := dispatches[i].(map[string]interface{})
		if m["id"] != want {
			t.Errorf("dispatch[%d] id = %v, want %s", i, m["id"], want)
		}
	}
}

// TestRegistry_MergedSnapshot_GroupingIsIdempotent verifies that grouping an
// array that already carries an instance does not grow it. Re-feeding a
// grouped row's dispatches[] back into a slot and re-grouping must hold the
// length fixed — the property that breaks the amplification loop.
func TestRegistry_MergedSnapshot_GroupingIsIdempotent(t *testing.T) {
	r := NewRegistry()

	merged := []interface{}{
		map[string]interface{}{"id": "d1", "status": "done"},
		map[string]interface{}{"id": "d2", "status": "done"},
	}
	// Two slots both carrying the SAME already-merged array (the shape a
	// double-restore would create).
	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name: "dev-lead", ID: "d1", Status: "done",
		Metadata: map[string]interface{}{"dispatches": merged},
	}, func(existing *types.AgentStateUpdate) {})
	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name: "dev-lead", ID: "d2", Status: "done",
		Metadata: map[string]interface{}{"dispatches": merged},
	}, func(existing *types.AgentStateUpdate) {})

	snap := r.MergedSnapshot()
	dispatches, _ := snap[0].Metadata["dispatches"].([]interface{})
	if len(dispatches) != 2 {
		t.Fatalf("expected 2 distinct dispatches after idempotent merge, got %d", len(dispatches))
	}
}

// TestRegistry_MergedSnapshot_GroupingKeepsIDlessEntries verifies that
// dispatch entries with no usable "id" are preserved (append fallback) rather
// than collapsed into one, so malformed members survive.
func TestRegistry_MergedSnapshot_GroupingKeepsIDlessEntries(t *testing.T) {
	r := NewRegistry()

	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name: "dev-lead", ID: "d1", Status: "done",
		Metadata: map[string]interface{}{
			"dispatches": []interface{}{
				map[string]interface{}{"task": "no id 1"},
				map[string]interface{}{"task": "no id 2"},
			},
		},
	}, func(existing *types.AgentStateUpdate) {})

	snap := r.MergedSnapshot()
	dispatches, _ := snap[0].Metadata["dispatches"].([]interface{})
	if len(dispatches) != 2 {
		t.Fatalf("expected 2 id-less dispatches preserved, got %d", len(dispatches))
	}
}
