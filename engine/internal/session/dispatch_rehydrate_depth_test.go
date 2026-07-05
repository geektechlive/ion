package session

// dispatch_rehydrate_depth_test.go — tests that rehydrateDispatchState
// restores dispatchDepth and dispatchParentId onto the reloaded agent-state
// metadata after an engine restart / cold load.

import (
	"testing"

	"github.com/dsswift/ion/engine/internal/conversation"
	"github.com/dsswift/ion/engine/internal/session/agents"
	"github.com/dsswift/ion/engine/internal/session/extcontext"
	"github.com/dsswift/ion/engine/internal/session/pending"
	"github.com/dsswift/ion/engine/internal/types"
)

// TestRehydrateDispatchDepth cold-loads a conversation that carries a
// persisted depth-2 dispatch (DispatchDepth=2, DispatchParentID="parent-abc")
// and drives the real rehydrateDispatchState. It asserts the returned agent
// state's metadata carries dispatchDepth==2 and dispatchParentId=="parent-abc".
//
// Before the Commit 2 fix (adding metadata["dispatchDepth"] = d.DispatchDepth
// and metadata["dispatchParentId"] = d.DispatchParentID in rehydrateDispatchState),
// this test goes RED: the rehydrated metadata omits both keys, so the reads
// below yield the zero values (depth 0, parentId "") and the assertions fail.
// That before/after behavior was verified by temporarily removing those two
// lines and observing the failures:
//
//	dispatchDepth = 0, want 2
//	dispatchParentId = "", want parent-abc
func TestRehydrateDispatchDepth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	convDir := home + "/.ion/conversations"

	const convID = "rehydrate-depth-1"
	conv := conversation.CreateConversation(convID, "sys", "model")
	conversation.AddUserMessage(conv, "hello")

	// Persist a depth-2 dispatch record directly (a minimal fixture standing in
	// for what Commit 1's persistTerminalDispatches would have written to disk).
	conv.Entries = append(conv.Entries, conversation.SessionEntry{
		ID: "d1", ParentID: nil,
		Type: conversation.EntryAgentDispatch, Timestamp: 1000,
		Data: conversation.AgentDispatchData{
			AgentName:        "dev-lead",
			AgentID:          "d1",
			Task:             "lead the work",
			Status:           "done",
			ConversationID:   "conv-d1",
			DispatchDepth:    2,
			DispatchParentID: "parent-abc",
			Dispatches: []map[string]interface{}{
				makeDispatchMember("d1", "conv-d1", "done"),
			},
		},
	})
	if err := conversation.Save(conv, convDir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Cold load: fresh registry simulates an engine restart / session reload.
	m := newTestManager(t)
	s := &engineSession{
		key:              "k",
		conversationID:   convID,
		agents:           agents.NewRegistry(),
		dispatchRegistry: extcontext.NewDispatchRegistry(),
		pending:          pending.New(),
	}
	m.sessions["k"] = s

	m.rehydrateDispatchState(s, "k")

	snap := s.agents.MergedSnapshot()
	var devLead *types.AgentStateUpdate
	for i := range snap {
		if snap[i].Name == "dev-lead" {
			devLead = &snap[i]
			break
		}
	}
	if devLead == nil {
		t.Fatalf("dev-lead not found in snapshot: %+v", snap)
	}

	gotDepth := metaInt(devLead.Metadata, "dispatchDepth")
	if gotDepth != 2 {
		t.Errorf("dispatchDepth = %v (%d), want 2", devLead.Metadata["dispatchDepth"], gotDepth)
	}
	gotParent, _ := devLead.Metadata["dispatchParentId"].(string)
	if gotParent != "parent-abc" {
		t.Errorf("dispatchParentId = %q, want parent-abc", gotParent)
	}
}
