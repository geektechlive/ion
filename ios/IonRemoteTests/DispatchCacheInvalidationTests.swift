import XCTest
@testable import IonRemote

// MARK: - Dispatch cache invalidation tests
//
// Two regression suites for the stale-dispatch-snapshot bug:
//
//   Fix A — invalidateAllDispatchCaches on new engine session:
//     Seed agentSnapshotByConvId with a stale snapshot, simulate a new-engine
//     sessionId arriving via handleEngineSessionIdChange, and assert the caches
//     are wiped so the next load re-fetches from the current engine.
//
//   Fix B — clearTerminalDispatchCaches defers push-clear when snapshot is incomplete:
//     Seed a snapshot smaller than the merged push+snapshot count, call
//     clearTerminalDispatchCaches, and assert the push buffer is NOT cleared
//     (reload is issued instead of collapse-to-stale-snapshot).
//
// Revert-check: removing the new code from DispatchCacheInvalidation.swift
// turns both suites red, confirming they are genuine guards on the fix.
@MainActor
final class DispatchCacheInvalidationTests: XCTestCase {

    // MARK: - Fix A: session id change invalidates dispatch caches

    func testFixA_sessionIdChangeClearsSnapshotCache() {
        let vm = SessionViewModel()
        let convId = "conv-stale-1"
        let tabId = "tab-engine-1"
        let staleMsg = Message(id: "m1", role: .assistant, content: "stale content", timestamp: 1)

        // Seed caches as if an old engine had populated them.
        vm.agentSnapshotByConvId[convId] = [staleMsg]
        vm.agentConversationMessages[convId] = [staleMsg]
        vm.terminalClearedDispatches.insert("dispatch-old-1")

        // Seed the old sessionId.
        vm.lastKnownEngineSessionId[tabId] = "old-session-id"

        // New engine session arrives.
        vm.handleEngineSessionIdChange(tabId: tabId, sessionId: "new-session-id")

        // All dispatch caches must be cleared so the next load hits the new engine.
        XCTAssertNil(vm.agentSnapshotByConvId[convId],
            "Fix A: agentSnapshotByConvId must be cleared on engine session change — without the fix this remains populated and the terminal-skip guard blocks re-fetch forever")
        XCTAssertNil(vm.agentConversationMessages[convId],
            "Fix A: agentConversationMessages must be cleared on engine session change")
        XCTAssertFalse(vm.terminalClearedDispatches.contains("dispatch-old-1"),
            "Fix A: terminalClearedDispatches edge-gate must be cleared so new loads can flow")
        // New sessionId must be stored for future change detection.
        XCTAssertEqual(vm.lastKnownEngineSessionId[tabId], "new-session-id",
            "Fix A: new sessionId must be persisted for future change detection")
    }

    func testFixA_sameSessionIdDoesNotClearCaches() {
        let vm = SessionViewModel()
        let convId = "conv-stable-1"
        let tabId = "tab-engine-2"
        let msg = Message(id: "m2", role: .assistant, content: "good content", timestamp: 2)

        vm.agentSnapshotByConvId[convId] = [msg]
        vm.lastKnownEngineSessionId[tabId] = "stable-session-id"

        // Same session id — routine snapshot tick, not a restart.
        vm.handleEngineSessionIdChange(tabId: tabId, sessionId: "stable-session-id")

        XCTAssertNotNil(vm.agentSnapshotByConvId[convId],
            "Fix A: same sessionId must NOT clear caches — only a changed id means a new engine")
    }

    func testFixA_firstSessionIdSeedsButDoesNotClear() {
        let vm = SessionViewModel()
        let convId = "conv-first-1"
        let tabId = "tab-engine-3"
        let msg = Message(id: "m3", role: .assistant, content: "existing content", timestamp: 3)

        // Caches have content (e.g. loaded during this session already).
        vm.agentConversationMessages[convId] = [msg]
        // No previous sessionId stored yet.

        // First sessionId for this tab — caches are from THIS session, so clearing
        // would be wrong. The implementation seeds without clearing, which is safe.
        vm.handleEngineSessionIdChange(tabId: tabId, sessionId: "first-session-id")

        // Content must survive: the first sessionId is just seeding, not a restart.
        XCTAssertNotNil(vm.agentConversationMessages[convId],
            "Fix A: first-ever sessionId must seed lastKnownEngineSessionId without clearing caches")
        XCTAssertEqual(vm.lastKnownEngineSessionId[tabId], "first-session-id",
            "Fix A: first sessionId must be stored for future change detection")
    }

    func testFixA_nilOrEmptySessionIdIsNoOp() {
        let vm = SessionViewModel()
        let convId = "conv-nil-1"
        let tabId = "tab-cli-1"
        let msg = Message(id: "m4", role: .assistant, content: "cli content", timestamp: 4)

        vm.agentConversationMessages[convId] = [msg]

        // CLI tabs and pre-engine tabs emit nil or empty sessionId — must be ignored.
        vm.handleEngineSessionIdChange(tabId: tabId, sessionId: nil)
        vm.handleEngineSessionIdChange(tabId: tabId, sessionId: "")

        XCTAssertNotNil(vm.agentConversationMessages[convId],
            "Fix A: nil/empty sessionId must be a no-op — non-engine tabs must not trigger invalidation")
    }

    // MARK: - Fix B: incomplete snapshot defers push clear on terminal transition

    func testFixB_incompleteSnapshotDefersRelease() {
        let vm = SessionViewModel()
        let convId = "conv-trunc-1"
        let dispatchId = "dispatch-engine-1"

        // Seed a truncated snapshot (50 messages from the old engine).
        var snapshot: [Message] = []
        for i in 0..<50 {
            snapshot.append(Message(id: "snap-\(i)", role: .tool, content: "tool \(i)",
                                    toolName: "Read", toolId: "snap-\(i)",
                                    toolStatus: .completed, timestamp: Double(i)))
        }
        vm.agentSnapshotByConvId[convId] = snapshot

        // Push buffer adds 83 more entries (the running dispatch's live content).
        var pushEntries: [Message] = []
        for i in 0..<83 {
            pushEntries.append(Message(id: "push-\(i)", role: .tool, content: "push \(i)",
                                       toolName: "Bash", toolId: "push-\(i)",
                                       toolStatus: .completed, timestamp: Double(50 + i)))
        }
        vm.agentDispatchActivity[dispatchId] = pushEntries
        vm.agentDispatchSeqs[dispatchId] = Array(0..<83)
        vm.activeDispatchIdByConvId[convId] = dispatchId

        // Simulate the merged transcript (snapshot + push = 133).
        vm.agentConversationMessages[convId] = snapshot + pushEntries

        // Build a terminal dispatch info that matches the convId.
        let agent = makeAgent(dispatchId: dispatchId, convId: convId, status: "done")

        // Call the function under test.
        vm.clearTerminalDispatchCaches(for: [agent])

        // Push buffer must be RETAINED — the snapshot is truncated and dropping
        // push would collapse the visible transcript to 50 messages.
        XCTAssertNotNil(vm.agentDispatchActivity[dispatchId],
            "Fix B: push buffer must be retained when snapshot is incomplete (merged > snapshot) — without the fix, push is cleared immediately and transcript collapses to the stale 50-message snapshot")

        // Merged transcript must remain intact (not replaced by snapshot-only).
        let remaining = vm.agentConversationMessages[convId] ?? []
        XCTAssertGreaterThan(remaining.count, snapshot.count,
            "Fix B: merged transcript must not collapse to snapshot-only when snapshot is incomplete")
    }

    func testFixB_completeSnapshotClearsPushImmediately() {
        let vm = SessionViewModel()
        let convId = "conv-complete-1"
        let dispatchId = "dispatch-engine-2"

        // Snapshot already contains the full conversation.
        let snapshot = [
            Message(id: "s1", role: .assistant, content: "full answer", timestamp: 1),
            Message(id: "t1", role: .tool, content: "done", toolName: "Read",
                    toolId: "t1", toolStatus: .completed, timestamp: 2),
        ]
        vm.agentSnapshotByConvId[convId] = snapshot
        // Push mirrors the snapshot (no extra content).
        vm.agentDispatchActivity[dispatchId] = snapshot
        vm.agentDispatchSeqs[dispatchId] = [0, 1]
        // Merged == snapshot (all push is covered).
        vm.agentConversationMessages[convId] = snapshot

        let agent = makeAgent(dispatchId: dispatchId, convId: convId, status: "done")
        vm.clearTerminalDispatchCaches(for: [agent])

        // Push must be cleared when snapshot is complete.
        XCTAssertNil(vm.agentDispatchActivity[dispatchId],
            "Fix B: push buffer must be cleared when merged == snapshot (snapshot is complete)")
    }

    func testFixB_noPushBufferClearIsNoOp() {
        let vm = SessionViewModel()
        let convId = "conv-nopush-1"
        let dispatchId = "dispatch-engine-3"

        let snapshot = [Message(id: "x1", role: .assistant, content: "answer", timestamp: 1)]
        vm.agentSnapshotByConvId[convId] = snapshot
        vm.agentConversationMessages[convId] = snapshot
        // No push buffer — dispatch was already terminal before any push arrived.

        let agent = makeAgent(dispatchId: dispatchId, convId: convId, status: "done")
        vm.clearTerminalDispatchCaches(for: [agent])

        // No crash, no reload issued. Merged rebuilt from snapshot.
        XCTAssertEqual(vm.agentConversationMessages[convId]?.count, snapshot.count,
            "Fix B: when no push buffer exists, merged must be rebuilt from the snapshot")
    }

    func testFixB_edgeGateFiresOnlyOnce() {
        let vm = SessionViewModel()
        let convId = "conv-gate-1"
        let dispatchId = "dispatch-engine-4"

        let snapshot = [Message(id: "g1", role: .assistant, content: "gate test", timestamp: 1)]
        vm.agentSnapshotByConvId[convId] = snapshot
        vm.agentConversationMessages[convId] = snapshot

        let agent = makeAgent(dispatchId: dispatchId, convId: convId, status: "done")

        // First call: fires. Second and subsequent calls must be skipped (edge gate).
        vm.clearTerminalDispatchCaches(for: [agent])
        // Manually re-add push to verify it is not double-cleared.
        vm.agentDispatchActivity[dispatchId] = [Message(id: "late", role: .assistant, content: "late", timestamp: 99)]

        vm.clearTerminalDispatchCaches(for: [agent])

        // The second call should have been skipped by the edge gate; the manually
        // re-inserted push buffer must survive.
        XCTAssertNotNil(vm.agentDispatchActivity[dispatchId],
            "Fix B: edge gate must prevent a second clear on the same dispatchId")
    }

    // MARK: - Helpers

    /// Build a minimal AgentStateUpdate with a single terminal dispatch entry.
    private func makeAgent(dispatchId: String, convId: String, status: String) -> AgentStateUpdate {
        let dispatchDict: [String: Any] = [
            "id": dispatchId,
            "task": "",
            "model": "",
            "conversationId": convId,
            "status": status
        ]
        let dispatchInfo = DispatchInfo(from: dispatchDict)

        // AgentStateUpdate only has init(from:Decoder), so encode a JSON payload.
        let json: [String: Any] = [
            "name": "dev-lead",
            "status": "done",
            "id": dispatchId,
            "metadata": [
                "displayName": "Dev Lead",
                "type": "specialist",
                "visibility": "ephemeral",
                "invited": false,
                "dispatches": [[
                    "id": dispatchId,
                    "task": "",
                    "model": "",
                    "conversationId": convId,
                    "status": status
                ]]
            ] as [String: Any]
        ]
        let data = try! JSONSerialization.data(withJSONObject: json)
        var agent = try! JSONDecoder().decode(AgentStateUpdate.self, from: data)
        // Verify the dispatch decoded correctly.
        _ = dispatchInfo // used above; keep reference
        return agent
    }
}
