package session

// dispatch_persist_depth_test.go — tests that persistTerminalDispatches
// carries dispatchDepth and dispatchParentId from agent-state metadata onto
// the persisted AgentDispatchData record AND onto every dispatches[] member.

import (
	"testing"

	"github.com/dsswift/ion/engine/internal/conversation"
	"github.com/dsswift/ion/engine/internal/session/agents"
	"github.com/dsswift/ion/engine/internal/session/extcontext"
	"github.com/dsswift/ion/engine/internal/session/pending"
	"github.com/dsswift/ion/engine/internal/types"
)

// TestPersistDispatchDepth drives the real persistTerminalDispatches against
// an on-disk conversation. It seeds a terminal dispatch whose metadata carries
// dispatchDepth=2 and dispatchParentId="parent-abc", persists it, reloads the
// conversation from disk, and asserts:
//   - the persisted record carries DispatchDepth==2 and DispatchParentID=="parent-abc"
//   - each dispatches[] member also carries dispatchDepth==2 and dispatchParentId=="parent-abc"
//
// Before the Commit 1 fix (reading meta["dispatchDepth"]/meta["dispatchParentId"]
// and writing them onto the record and each member), this test is RED: the
// reloaded record's fields are the zero value (0 / "") because they were never
// persisted.
func TestPersistDispatchDepth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	convDir := home + "/.ion/conversations"

	const convID = "persist-depth-1"
	conv := conversation.CreateConversation(convID, "sys", "model")
	conversation.AddUserMessage(conv, "hello")
	if err := conversation.Save(conv, convDir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	m := newTestManager(t)
	s := &engineSession{
		key:              "k",
		conversationID:   convID,
		agents:           agents.NewRegistry(),
		dispatchRegistry: extcontext.NewDispatchRegistry(),
		pending:          pending.New(),
	}
	m.sessions["k"] = s

	// Seed a terminal dev-lead dispatch whose metadata carries the depth and
	// parent attribution the engine spawner stamps at dispatch time. The
	// dispatches[] member intentionally omits depth/parent so we can prove
	// persistTerminalDispatches stamps them onto each member.
	s.agents.AppendOrUpdateByID(types.AgentStateUpdate{
		Name:   "dev-lead",
		ID:     "d1",
		Status: "done",
		Metadata: map[string]interface{}{
			"task":             "lead the work",
			"conversationId":   "conv-d1",
			"dispatchDepth":    2,
			"dispatchParentId": "parent-abc",
			"dispatches": []interface{}{
				makeDispatchMember("d1", "conv-d1", "done"),
			},
		},
	}, func(existing *types.AgentStateUpdate) {})

	m.persistTerminalDispatches("k", convID)

	// Reload the conversation from disk to prove the fields survived the
	// marshal/unmarshal round-trip, not just the in-memory build.
	reloaded, err := conversation.Load(convID, convDir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	records := conversation.AgentDispatchEntries(reloaded)
	if len(records) != 1 {
		t.Fatalf("expected 1 persisted dispatch record, got %d", len(records))
	}

	rec := records[0]
	if rec.DispatchDepth != 2 {
		t.Errorf("record.DispatchDepth = %d, want 2", rec.DispatchDepth)
	}
	if rec.DispatchParentID != "parent-abc" {
		t.Errorf("record.DispatchParentID = %q, want parent-abc", rec.DispatchParentID)
	}

	// Each dispatches[] member must also carry the attribution.
	if len(rec.Dispatches) == 0 {
		t.Fatalf("expected at least 1 dispatches[] member, got 0")
	}
	for i, member := range rec.Dispatches {
		// After a JSON round-trip, numeric values decode as float64.
		gotDepth := metaInt(member, "dispatchDepth")
		if gotDepth != 2 {
			t.Errorf("dispatches[%d].dispatchDepth = %v (%d), want 2", i, member["dispatchDepth"], gotDepth)
		}
		gotParent, _ := member["dispatchParentId"].(string)
		if gotParent != "parent-abc" {
			t.Errorf("dispatches[%d].dispatchParentId = %q, want parent-abc", i, gotParent)
		}
	}
}
