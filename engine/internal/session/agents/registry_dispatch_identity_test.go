package agents

// registry_dispatch_identity_test.go — tests that groupByName preserves the
// per-dispatch identity (dispatchId) of same-name dispatches in the emitted
// engine_agent_state snapshot, so concurrent same-name dispatches remain
// distinct, ID-addressable entries rather than collapsing anonymously.

import (
	"testing"

	"github.com/dsswift/ion/engine/internal/types"
)

// TestGroupByNameDistinctDispatches creates two concurrent dispatches with the
// same agent name but distinct dispatch ids and distinct parent ids, drives
// them through the snapshot/groupByName projection, and asserts both dispatches
// appear as distinct, ID-addressable entries whose dispatchId is non-empty.
//
// Before the Commit 3 fix (stamping an explicit dispatchId onto each merged
// dispatch member in groupByName), the members carry only "id" and a consumer
// keying on "dispatchId" sees empty strings — the two same-name dispatches are
// indistinguishable. After the fix, each member exposes a non-empty dispatchId
// mirrored from its stable id, plus its own parent/depth attribution.
func TestGroupByNameDistinctDispatches(t *testing.T) {
	r := NewRegistry()

	// Dispatch A: same name "dev-lead", id "d1", parent "root", depth 1.
	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "d1",
		Status: "running",
		Metadata: map[string]interface{}{
			"task":             "first branch",
			"dispatchDepth":    1,
			"dispatchParentId": "root",
			"dispatches": []interface{}{
				map[string]interface{}{
					"id":               "d1",
					"status":           "running",
					"dispatchDepth":    1,
					"dispatchParentId": "root",
				},
			},
		},
	}, func(existing *types.AgentStateUpdate) {})

	// Dispatch B: same name "dev-lead", distinct id "d2", distinct parent
	// "orchestrator", depth 2 — genuinely concurrent, distinct lineage.
	r.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "d2",
		Status: "running",
		Metadata: map[string]interface{}{
			"task":             "second branch",
			"dispatchDepth":    2,
			"dispatchParentId": "orchestrator",
			"dispatches": []interface{}{
				map[string]interface{}{
					"id":               "d2",
					"status":           "running",
					"dispatchDepth":    2,
					"dispatchParentId": "orchestrator",
				},
			},
		},
	}, func(existing *types.AgentStateUpdate) {})

	snap := r.MergedSnapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 grouped dev-lead row, got %d: %+v", len(snap), snap)
	}

	dispatches, ok := snap[0].Metadata["dispatches"].([]interface{})
	if !ok {
		t.Fatalf("dispatches[] not found in snapshot metadata: %v", snap[0].Metadata)
	}
	if len(dispatches) != 2 {
		t.Fatalf("expected 2 distinct dispatch members, got %d: %v", len(dispatches), dispatches)
	}

	// Index members by their dispatchId to prove each is ID-addressable and
	// that neither dispatchId is empty.
	byDispatchID := make(map[string]map[string]interface{})
	for i, entry := range dispatches {
		m, ok := entry.(map[string]interface{})
		if !ok {
			t.Fatalf("dispatch member %d is not a map: %T", i, entry)
		}
		did, _ := m["dispatchId"].(string)
		if did == "" {
			t.Errorf("dispatch member %d has empty dispatchId: %v", i, m)
			continue
		}
		if _, dup := byDispatchID[did]; dup {
			t.Errorf("duplicate dispatchId %q — members are not distinct", did)
		}
		byDispatchID[did] = m
	}

	// Both distinct dispatches must be present and addressable by id.
	d1, ok1 := byDispatchID["d1"]
	if !ok1 {
		t.Fatalf("dispatch d1 not addressable in snapshot: %v", byDispatchID)
	}
	d2, ok2 := byDispatchID["d2"]
	if !ok2 {
		t.Fatalf("dispatch d2 not addressable in snapshot: %v", byDispatchID)
	}

	// Each member retains its own distinct parent lineage — the whole point of
	// preserving per-dispatch identity.
	if p, _ := d1["dispatchParentId"].(string); p != "root" {
		t.Errorf("d1 dispatchParentId = %q, want root", p)
	}
	if p, _ := d2["dispatchParentId"].(string); p != "orchestrator" {
		t.Errorf("d2 dispatchParentId = %q, want orchestrator", p)
	}

	// Existing "id" key is preserved (additive change — nothing removed).
	if id, _ := d1["id"].(string); id != "d1" {
		t.Errorf("d1 lost its stable id: got %q", id)
	}
	if id, _ := d2["id"].(string); id != "d2" {
		t.Errorf("d2 lost its stable id: got %q", id)
	}
}
