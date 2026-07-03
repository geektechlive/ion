package session

// aggregate_cost_test.go — tests for ComputeAggregateCost, the on-demand
// dispatch-tree cost walk that backs the aggregateCostUsd field on the
// context_breakdown event.

import (
	"testing"

	"github.com/dsswift/ion/engine/internal/conversation"
)

// writeConvWithCost creates and persists a split-format conversation with the
// given id, cost, and dispatch child conversation IDs. Each entry in children
// becomes an AgentDispatchData with a single ConversationID. It returns the
// conversation id for convenience.
func writeConvWithCost(t *testing.T, dir, id string, cost float64, children ...string) string {
	t.Helper()
	conv := conversation.CreateConversation(id, "system", "claude-sonnet-4-6")
	// A non-empty Entries list triggers the split save path.
	conversation.AddUserMessage(conv, "prompt")
	conv.TotalCost = cost

	for _, childID := range children {
		dispatch := conversation.AgentDispatchData{
			AgentName:       "worker",
			AgentID:         id + "-dispatch-" + childID,
			Status:          "done",
			ConversationID:  childID,
			ConversationIDs: []string{childID},
		}
		conv.Entries = append(conv.Entries, conversation.SessionEntry{
			ID:       dispatch.AgentID,
			Type:     conversation.EntryAgentDispatch,
			Data:     dispatch,
			ParentID: nil,
		})
	}

	if err := conversation.Save(conv, dir); err != nil {
		t.Fatalf("Save %s: %v", id, err)
	}
	return id
}

// TestComputeAggregateCost_EmptyConvID verifies an empty conversation ID
// returns (0, nil) immediately without touching disk.
func TestComputeAggregateCost_EmptyConvID(t *testing.T) {
	total, err := ComputeAggregateCost("", nil, "")
	if err != nil {
		t.Fatalf("ComputeAggregateCost: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %f, want 0 for empty convID", total)
	}
}

// TestComputeAggregateCost_FreshConversation verifies a conversation with no
// cost and no dispatches aggregates to 0.
func TestComputeAggregateCost_FreshConversation(t *testing.T) {
	dir := t.TempDir()
	writeConvWithCost(t, dir, "fresh", 0)

	total, err := ComputeAggregateCost("fresh", nil, dir)
	if err != nil {
		t.Fatalf("ComputeAggregateCost: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %f, want 0", total)
	}
}

// TestComputeAggregateCost_TwoChildren verifies a historical top-level
// conversation with two child dispatches sums own + child1 + child2.
func TestComputeAggregateCost_TwoChildren(t *testing.T) {
	dir := t.TempDir()
	writeConvWithCost(t, dir, "child1", 0.02)
	writeConvWithCost(t, dir, "child2", 0.03)
	writeConvWithCost(t, dir, "top", 0.10, "child1", "child2")

	total, err := ComputeAggregateCost("top", nil, dir)
	if err != nil {
		t.Fatalf("ComputeAggregateCost: %v", err)
	}
	want := 0.15
	if total < want-1e-9 || total > want+1e-9 {
		t.Errorf("total = %f, want %f", total, want)
	}
}

// TestComputeAggregateCost_NTier verifies transitive descent: top -> child ->
// grandchild produces the three-way sum.
func TestComputeAggregateCost_NTier(t *testing.T) {
	dir := t.TempDir()
	writeConvWithCost(t, dir, "grandchild", 0.05)
	writeConvWithCost(t, dir, "child", 0.03, "grandchild")
	writeConvWithCost(t, dir, "top", 0.10, "child")

	total, err := ComputeAggregateCost("top", nil, dir)
	if err != nil {
		t.Fatalf("ComputeAggregateCost: %v", err)
	}
	want := 0.18
	if total < want-1e-9 || total > want+1e-9 {
		t.Errorf("total = %f, want %f", total, want)
	}
}

// TestComputeAggregateCost_CycleAndDupGuard verifies a conversation ID that
// appears in multiple dispatch entries (and self-references) is counted once.
func TestComputeAggregateCost_CycleAndDupGuard(t *testing.T) {
	dir := t.TempDir()
	// child referenced twice from top; and top references itself (cycle).
	writeConvWithCost(t, dir, "child", 0.04)

	conv := conversation.CreateConversation("top", "system", "claude-sonnet-4-6")
	conversation.AddUserMessage(conv, "prompt")
	conv.TotalCost = 0.10
	// Two dispatch entries pointing at the same child, plus a self-reference.
	for _, target := range []string{"child", "child", "top"} {
		dispatch := conversation.AgentDispatchData{
			AgentName:      "worker",
			AgentID:        "top-dispatch-" + target + "-" + randSuffix(),
			Status:         "done",
			ConversationID: target,
		}
		conv.Entries = append(conv.Entries, conversation.SessionEntry{
			ID:   dispatch.AgentID,
			Type: conversation.EntryAgentDispatch,
			Data: dispatch,
		})
	}
	if err := conversation.Save(conv, dir); err != nil {
		t.Fatalf("Save top: %v", err)
	}

	total, err := ComputeAggregateCost("top", nil, dir)
	if err != nil {
		t.Fatalf("ComputeAggregateCost: %v", err)
	}
	want := 0.14 // top(0.10) + child(0.04), each counted once.
	if total < want-1e-9 || total > want+1e-9 {
		t.Errorf("total = %f, want %f (dup/cycle not deduped)", total, want)
	}
}

// TestComputeAggregateCost_LiveConvIDsDedup verifies a conversation ID present
// in both the persisted tree and the liveConvIDs list is counted once.
func TestComputeAggregateCost_LiveConvIDsDedup(t *testing.T) {
	dir := t.TempDir()
	writeConvWithCost(t, dir, "child", 0.04)
	writeConvWithCost(t, dir, "live-only", 0.06)
	writeConvWithCost(t, dir, "top", 0.10, "child")

	// "child" appears in both the tree and liveConvIDs; "live-only" is only in
	// liveConvIDs (an in-flight dispatch whose tree entry is not yet persisted).
	total, err := ComputeAggregateCost("top", []string{"child", "live-only"}, dir)
	if err != nil {
		t.Fatalf("ComputeAggregateCost: %v", err)
	}
	want := 0.20 // top(0.10) + child(0.04) + live-only(0.06); child once.
	if total < want-1e-9 || total > want+1e-9 {
		t.Errorf("total = %f, want %f", total, want)
	}
}

// randSuffix returns a short unique-ish suffix so AgentIDs in the dup test are
// distinct even when they point at the same conversation.
var randCounter int

func randSuffix() string {
	randCounter++
	return string(rune('a' + (randCounter % 26)))
}
