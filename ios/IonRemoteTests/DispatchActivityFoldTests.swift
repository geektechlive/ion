import XCTest
@testable import IonRemote

/// Convergence/dedupe test for the iOS live dispatched-agent transcript
/// (architecture C — push + slow full-snapshot reconcile). Mirrors the desktop
/// agent-dispatch-activity.test.ts. Pins that:
///   1. push deltas fold by stable key (toolId for tools, seq for text),
///   2. a tool_start→tool_end pair collapses to ONE entry, updated in place,
///   3. a file-backed snapshot reconciles with in-flight push entries to
///      exactly one entry per logical item — no duplicate, no dropped partial.
///   4. two distinct text blocks whose content strings share a prefix are NOT
///      collapsed — identity is exact content equality, mirroring the desktop
///      reconcileActivity in agent-dispatch-activity.ts.
///   5. entries sort by (ts primary, seq tiebreaker), matching desktop
///      activityMessages; ts-absent entries fall back to seq order.
///
/// Reverting the fold/reconcile in SessionViewModel+EngineEvents.swift turns
/// this red, which is what keeps push and reconcile from double-rendering.
@MainActor
final class DispatchActivityFoldTests: XCTestCase {

    private let convId = "child-conv-1"
    // Each test uses a stable dispatchAgentId that matches the dispatch.
    // Tests that exercise multi-dispatch isolation use distinct ids.
    private let dispatchId = "dispatch-agent-1"

    func testToolStartEndFoldsToOneEntryUpdatedInPlace() {
        let vm = SessionViewModel()
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "tool_start", seq: 1, ts: 100, toolName: "Read", toolId: "tool-1", textDelta: nil, isError: false)
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "tool_end", seq: 2, ts: 101, toolName: nil, toolId: "tool-1", textDelta: nil, isError: false)

        let msgs = vm.agentConversationMessages[convId] ?? []
        let toolMsgs = msgs.filter { $0.role == .tool }
        XCTAssertEqual(toolMsgs.count, 1, "tool_start + tool_end (same toolId) must collapse to one entry")
        XCTAssertEqual(toolMsgs.first?.toolId, "tool-1")
        XCTAssertEqual(toolMsgs.first?.toolName, "Read")
        XCTAssertEqual(toolMsgs.first?.toolStatus, .completed)
    }

    func testToolEndErrorStatus() {
        let vm = SessionViewModel()
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "tool_start", seq: 1, ts: 100, toolName: "Bash", toolId: "t", textDelta: nil, isError: false)
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "tool_end", seq: 2, ts: 101, toolName: nil, toolId: "t", textDelta: nil, isError: true)
        XCTAssertEqual(vm.agentConversationMessages[convId]?.first(where: { $0.role == .tool })?.toolStatus, .error)
    }

    func testCoalescedTextFoldsToOneEntry() {
        let vm = SessionViewModel()
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text", seq: 3, ts: 200, toolName: nil, toolId: nil, textDelta: "hello", isError: false)
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text", seq: 3, ts: 200, toolName: nil, toolId: nil, textDelta: "hello world", isError: false)
        let textMsgs = (vm.agentConversationMessages[convId] ?? []).filter { $0.role == .assistant }
        XCTAssertEqual(textMsgs.count, 1, "a coalesced text run sharing a seq slot must update in place")
        XCTAssertEqual(textMsgs.first?.content, "hello world")
    }

    // MARK: - Ordering: ts primary, seq tiebreaker
    //
    // Each test is constructed so it FAILS on unfixed code (before the sort
    // key change). The exact discriminator is noted per test.

    /// ts-primary ordering: seq 3 with ts=100 emitted before seq 1 with ts=200
    /// must render seq-3 first. The old seq-only sort placed seq 1 first → red.
    func testOrderingTsPrimary() {
        let vm = SessionViewModel()
        // Emit out of seq order: lower seq has a higher ts.
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text",       seq: 3, ts: 100, toolName: nil,    toolId: nil,    textDelta: "early-ts",  isError: false)
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "tool_start", seq: 1, ts: 200, toolName: "Read", toolId: "t-ts", textDelta: nil,        isError: false)

        let msgs = vm.agentConversationMessages[convId] ?? []
        // ts=100 (assistant, seq 3) must appear before ts=200 (tool, seq 1).
        // Unfixed code (seq-only): tool (seq 1) first → XCTAssertEqual(msgs[0].role, .assistant) fails.
        XCTAssertEqual(msgs.count, 2, "must have exactly two entries")
        XCTAssertEqual(msgs[0].role, .assistant, "ts=100 entry must sort first (ts primary) — seq-only sort fails this")
        XCTAssertEqual(msgs[1].role, .tool,      "ts=200 entry must sort second")
    }

    /// Equal-ts tiebreaker: when ts is identical, seq determines order.
    func testOrderingEqualTsFallsBackToSeq() {
        let vm = SessionViewModel()
        // Both entries share ts=1000. Seq is the tiebreaker.
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text",       seq: 5, ts: 1000, toolName: nil,    toolId: nil,     textDelta: "seq-5", isError: false)
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "tool_start", seq: 2, ts: 1000, toolName: "Bash", toolId: "t-seq", textDelta: nil,     isError: false)

        let msgs = vm.agentConversationMessages[convId] ?? []
        // seq 2 (tool) before seq 5 (text) when ts is identical.
        XCTAssertEqual(msgs.count, 2)
        XCTAssertEqual(msgs[0].role, .tool,      "seq=2 must sort first when ts is equal (tiebreaker)")
        XCTAssertEqual(msgs[1].role, .assistant, "seq=5 must sort second when ts is equal")
    }

    /// ts-absent fallback: when ts is nil/0 for all entries, ordering degrades
    /// to seq order, preserving pre-ts behavior.
    func testOrderingTsAbsentFallsBackToSeq() {
        let vm = SessionViewModel()
        // No ts provided — both entries have effective ts=0.
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text",       seq: 3, ts: nil, toolName: nil,   toolId: nil, textDelta: "second", isError: false)
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "tool_start", seq: 1, ts: nil, toolName: "Read", toolId: "t", textDelta: nil,     isError: false)

        let msgs = vm.agentConversationMessages[convId] ?? []
        // Both ts=0; seq is effective sort: seq 1 (tool) before seq 3 (text).
        XCTAssertEqual(msgs.count, 2)
        XCTAssertEqual(msgs[0].role, .tool,      "seq=1 must sort first when ts is absent for all entries")
        XCTAssertEqual(msgs[1].role, .assistant, "seq=3 must sort second when ts is absent for all entries")
    }

    /// Reconnect-survival regression: fold ascending-ts deltas, then simulate a
    /// re-dispatch scenario where the engine issues higher ts values for new entries
    /// that happen to interleave with earlier seq values. Higher-ts entries must
    /// still sort after all lower-ts entries regardless of their seq values.
    ///
    /// Models the case where a second dispatch batch arrives after a reconnect:
    /// new entries have strictly higher ts but may reuse low seq numbers for tools
    /// (distinct toolIds keep them separate). Higher-ts entries must sort last.
    func testReconnectSurvivalHigherTsAfterSeqReset() {
        let vm = SessionViewModel()
        let reconnectDispatchId = "dispatch-reconnect"

        // First dispatch batch: ts=1000, seq=1..2 (ascending order).
        vm.handleDispatchActivity(dispatchAgentId: reconnectDispatchId, conversationId: convId, kind: "tool_start", seq: 1, ts: 1000, toolName: "Read", toolId: "tool-a", textDelta: nil, isError: false)
        vm.handleDispatchActivity(dispatchAgentId: reconnectDispatchId, conversationId: convId, kind: "text",       seq: 2, ts: 1001, toolName: nil,    toolId: nil,       textDelta: "first batch text", isError: false)

        // After a reconnect, the engine issues new entries with strictly higher
        // ts values (>= 2000). The new tool has a distinct toolId (not a dedupe).
        // The new text uses a distinct seq (3) so it doesn't update seq=2 in place.
        vm.handleDispatchActivity(dispatchAgentId: reconnectDispatchId, conversationId: convId, kind: "tool_start", seq: 1, ts: 2000, toolName: "Bash", toolId: "tool-b", textDelta: nil, isError: false)
        vm.handleDispatchActivity(dispatchAgentId: reconnectDispatchId, conversationId: convId, kind: "text",       seq: 3, ts: 2001, toolName: nil,    toolId: nil,       textDelta: "second batch text", isError: false)

        let msgs = vm.agentConversationMessages[convId] ?? []
        // Must have 4 entries (two tools + two text; tool-a and tool-b have distinct toolIds,
        // text entries at seq=2 and seq=3 are distinct).
        XCTAssertEqual(msgs.count, 4, "all four distinct entries must survive")

        // All ts=1000/1001 entries must precede all ts=2000/2001 entries.
        // Unfixed (seq-only): tool-b (seq=1) would interleave with tool-a (seq=1),
        // and ordering by seq alone would be ambiguous → ts-primary sort is required.
        let firstTwo = msgs.prefix(2)
        let lastTwo = msgs.suffix(2)
        XCTAssertTrue(firstTwo.allSatisfy { ($0.timestamp ?? 0) < 2000 },
            "first two entries must have ts < 2000 (lower-ts batch first) — seq-only sort fails this")
        XCTAssertTrue(lastTwo.allSatisfy { ($0.timestamp ?? 0) >= 2000 },
            "last two entries must have ts >= 2000 (higher-ts batch last)")
    }

    // MARK: - Existing reconcile tests (ts: monotonic mirror of seq for dedupe-only assertions)

    func testReconcileDropsPushToolAlreadyInSnapshot() {
        let vm = SessionViewModel()
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "tool_start", seq: 1, ts: 100, toolName: "Read", toolId: "tool-1", textDelta: nil, isError: false)
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "tool_end", seq: 2, ts: 101, toolName: nil, toolId: "tool-1", textDelta: nil, isError: false)
        let snapshot = [Message(id: "tool-1", role: .tool, content: "file body", toolName: "Read", toolId: "tool-1", toolStatus: .completed, timestamp: 1)]
        vm.handleAgentConversationHistory(agentName: "dev", conversationId: convId, messages: snapshot)

        let merged = vm.agentConversationMessages[convId] ?? []
        let toolEntries = merged.filter { $0.toolId == "tool-1" }
        XCTAssertEqual(toolEntries.count, 1, "snapshot tool must not duplicate the push tool")
        XCTAssertEqual(toolEntries.first?.content, "file body", "snapshot version (with content) wins")
    }

    func testReconcilePreservesInFlightPartial() {
        let vm = SessionViewModel()
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "tool_start", seq: 1, ts: 100, toolName: "Read", toolId: "tool-1", textDelta: nil, isError: false)
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "tool_end", seq: 2, ts: 101, toolName: nil, toolId: "tool-1", textDelta: nil, isError: false)
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text", seq: 3, ts: 102, toolName: nil, toolId: nil, textDelta: "still thinking...", isError: false)
        let snapshot = [Message(id: "tool-1", role: .tool, content: "done", toolName: "Read", toolId: "tool-1", toolStatus: .completed, timestamp: 1)]
        vm.handleAgentConversationHistory(agentName: "dev", conversationId: convId, messages: snapshot)

        let merged = vm.agentConversationMessages[convId] ?? []
        XCTAssertEqual(merged.filter { $0.toolId == "tool-1" }.count, 1)
        XCTAssertEqual(merged.filter { $0.content == "still thinking..." }.count, 1, "in-flight partial must survive reconcile")
    }

    // MARK: - Bug A regression: finalized push text + finalized snapshot text → one entry

    func testBugA_finalizedPushTextDroppedWhenSnapshotHasSameContent() {
        let vm = SessionViewModel()
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text", seq: 5, ts: 500, toolName: nil, toolId: nil, textDelta: "I'll investiga", isError: false)
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text", seq: 5, ts: 500, toolName: nil, toolId: nil, textDelta: "I'll investigate the auto-group feature", isError: false)

        let snapshot = [
            Message(id: "msg-42", role: .assistant, content: "I'll investigate the auto-group feature", timestamp: 1_782_575_563_415)
        ]
        vm.handleAgentConversationHistory(agentName: "dev", conversationId: convId, messages: snapshot)

        let merged = vm.agentConversationMessages[convId] ?? []
        let assistantEntries = merged.filter { $0.role == .assistant }
        XCTAssertEqual(assistantEntries.count, 1,
            "push entry with finalized text equal to the snapshot must collapse to ONE entry (Bug A)")
        XCTAssertEqual(assistantEntries.first?.content, "I'll investigate the auto-group feature",
            "snapshot's version must be the one retained")
    }

    func testBugA_inFlightPartialBeyondSnapshotSurvives() {
        let vm = SessionViewModel()
        let snapshot = [
            Message(id: "msg-10", role: .assistant, content: "Earlier finalized analysis.", timestamp: 1_000_000_000)
        ]
        vm.handleAgentConversationHistory(agentName: "dev", conversationId: convId, messages: snapshot)

        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text", seq: 7, ts: 700, toolName: nil, toolId: nil, textDelta: "Now I'll check the file sizes...", isError: false)

        let merged = vm.agentConversationMessages[convId] ?? []
        let assistantEntries = merged.filter { $0.role == .assistant }
        XCTAssertEqual(assistantEntries.count, 2,
            "in-flight push text not present in snapshot must survive reconcile")
        XCTAssertTrue(assistantEntries.contains { $0.content == "Earlier finalized analysis." },
            "snapshot assistant message must be present")
        XCTAssertTrue(assistantEntries.contains { $0.content == "Now I'll check the file sizes..." },
            "in-flight push partial must survive when its content is absent from the snapshot")
    }

    // MARK: - Prefix-collapse regression (was broken by prefix-matching heuristic)

    func testPrefixCollapseRegression_distinctBlocksBothSurvive() {
        let vm = SessionViewModel()

        let snapshot = [
            Message(id: "msg-20", role: .assistant, content: "Let me check.", timestamp: 1_000)
        ]
        vm.handleAgentConversationHistory(agentName: "dev", conversationId: convId, messages: snapshot)

        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text", seq: 9, ts: 900, toolName: nil, toolId: nil, textDelta: "Let me check the engine logs", isError: false)

        let merged = vm.agentConversationMessages[convId] ?? []
        let assistantEntries = merged.filter { $0.role == .assistant }
        XCTAssertEqual(assistantEntries.count, 2,
            "two distinct blocks must both appear — prefix-matching incorrectly collapsed them to one")
        XCTAssertTrue(assistantEntries.contains { $0.content == "Let me check." },
            "snapshot block must be present")
        XCTAssertTrue(assistantEntries.contains { $0.content == "Let me check the engine logs" },
            "in-flight second block must survive — its content differs from the snapshot block")
    }

    // MARK: - Bug B regression: running dispatch reload not skipped; terminal dispatch cleared

    func testBugB_runningDispatchReloadIsNotSkipped() {
        let vm = SessionViewModel()

        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text", seq: 1, ts: 100, toolName: nil, toolId: nil, textDelta: "partial...", isError: false)
        XCTAssertNotNil(vm.agentConversationMessages[convId], "pre-condition: cache is populated from push activity")

        let runningAgent = makeAgent(name: "dev", convId: convId, dispatchId: dispatchId, dispatchStatus: "running")

        vm.loadAgentDispatchConversation(agent: runningAgent, conversationId: convId)
        XCTAssertTrue(vm.agentConversationLoading.contains(convId),
            "loadAgentDispatchConversation must enqueue a reload for a running dispatch even when cache is non-nil (Bug B)")
    }

    func testBugB_terminalDispatchReloadIsSkipped() {
        let vm = SessionViewModel()

        let finalSnapshot = [Message(id: "msg-final", role: .assistant, content: "Done.", timestamp: 1)]
        vm.handleAgentConversationHistory(agentName: "dev", conversationId: convId, messages: finalSnapshot)
        XCTAssertNotNil(vm.agentSnapshotByConvId[convId], "pre-condition: snapshot authority must be cached")

        let doneAgent = makeAgent(name: "dev", convId: convId, dispatchId: dispatchId, dispatchStatus: "done")

        vm.loadAgentDispatchConversation(agent: doneAgent, conversationId: convId)
        XCTAssertFalse(vm.agentConversationLoading.contains(convId),
            "loadAgentDispatchConversation must skip reload for a terminal dispatch whose snapshot is already cached")
    }

    // MARK: - Terminal clear regression (must not regress fix 1776d25e)

    func testTerminalClearRetainsSnapshotAndTranscript() {
        let vm = SessionViewModel()

        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "tool_start", seq: 1, ts: 100, toolName: "Read", toolId: "tool-99", textDelta: nil, isError: false)
        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text", seq: 2, ts: 101, toolName: nil, toolId: nil, textDelta: "working...", isError: false)

        let snap = [Message(id: "snap-1", role: .assistant, content: "Done analysis.", timestamp: 1_000)]
        vm.handleAgentConversationHistory(agentName: "dev", conversationId: convId, messages: snap)

        XCTAssertNotNil(vm.agentDispatchActivity[dispatchId], "pre-condition: push cache must be set (keyed by dispatchId)")
        XCTAssertNotNil(vm.agentSnapshotByConvId[convId], "pre-condition: snapshot cache must be set")
        XCTAssertNotNil(vm.agentConversationMessages[convId], "pre-condition: merged messages must be set")

        let mergedBefore = vm.agentConversationMessages[convId] ?? []
        let doneAgent = makeAgent(name: "dev", convId: convId, dispatchId: dispatchId, dispatchStatus: "done")
        vm.clearTerminalDispatchCaches(for: [doneAgent])

        // The snapshot has 1 message but the merged transcript had 3 (1 snapshot + 2 push entries).
        // Fix B: when merged > snapshot, push is RETAINED and a reload is triggered rather than
        // collapsing to the truncated snapshot. The snapshot authority stays intact in both paths.
        XCTAssertNotNil(vm.agentSnapshotByConvId[convId],
            "snapshot authority must be RETAINED — it is the file-backed truth and needed for reopen/re-merge")
        XCTAssertNotNil(vm.agentConversationMessages[convId],
            "merged messages must be RETAINED (no flicker-to-empty on popup reopen)")

        let mergedAfter = vm.agentConversationMessages[convId] ?? []
        XCTAssertGreaterThanOrEqual(mergedAfter.count, snap.count,
            "merged messages after terminal clear must be at least as complete as the snapshot")
        // The push buffer must be retained because the snapshot (1 msg) is smaller than
        // the merged transcript (3 msgs). Fix B defers the clear to avoid content loss.
        XCTAssertNotNil(vm.agentDispatchActivity[dispatchId],
            "push cache must be RETAINED when merged > snapshot: Fix B defers clear to prevent transcript collapse (pre-fix: push was cleared immediately, collapsing 3-msg view to 1-msg snapshot)")
        _ = mergedBefore // silence unused warning
    }

    // MARK: - New regression tests for terminal-clear fix

    func testTerminalClearIsEdgeTriggered() {
        let vm = SessionViewModel()

        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text", seq: 1, ts: 100, toolName: nil, toolId: nil, textDelta: "done text", isError: false)
        let snap = [Message(id: "s1", role: .assistant, content: "done text", timestamp: 1)]
        vm.handleAgentConversationHistory(agentName: "dev", conversationId: convId, messages: snap)

        let doneAgent = makeAgent(name: "dev", convId: convId, dispatchId: dispatchId, dispatchStatus: "done")

        vm.clearTerminalDispatchCaches(for: [doneAgent])
        XCTAssertNil(vm.agentDispatchActivity[dispatchId], "first call must clear the push cache")
        XCTAssertTrue(vm.terminalClearedDispatches.contains(dispatchId),
            "dispatchId must be recorded in terminalClearedDispatches after first clear")

        let sentinelMsg = Message(id: "sentinel", role: .assistant, content: "re-inserted", timestamp: 2)
        vm.agentDispatchActivity[dispatchId] = [sentinelMsg]

        vm.clearTerminalDispatchCaches(for: [doneAgent])
        XCTAssertNotNil(vm.agentDispatchActivity[dispatchId],
            "second clearTerminalDispatchCaches call must not re-clear (edge-triggered, not level-triggered)")
    }

    func testTerminalDispatchWithMissingSnapshotAllowsLoad() {
        let vm = SessionViewModel()

        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text", seq: 1, ts: 100, toolName: nil, toolId: nil, textDelta: "output", isError: false)
        XCTAssertNotNil(vm.agentConversationMessages[convId], "pre-condition: merged cache present from push")
        XCTAssertNil(vm.agentSnapshotByConvId[convId], "pre-condition: snapshot authority must be absent")

        let doneAgent = makeAgent(name: "dev", convId: convId, dispatchId: dispatchId, dispatchStatus: "done")

        vm.loadAgentDispatchConversation(agent: doneAgent, conversationId: convId)
        XCTAssertTrue(vm.agentConversationLoading.contains(convId),
            "loadAgentDispatchConversation must enqueue a load for a terminal dispatch whose snapshot authority is absent")
    }

    // MARK: - Rekeying regression tests (dispatchAgentId isolation)

    func testTwoStreamsSharedConvIdSeparateBuffers() {
        let vm = SessionViewModel()
        let dispatchIdA = "dispatch-A"
        let dispatchIdB = "dispatch-B"
        let sharedConvId = "shared-conv"

        vm.handleDispatchActivity(dispatchAgentId: dispatchIdA, conversationId: sharedConvId, kind: "text", seq: 1, ts: 100, toolName: nil, toolId: nil, textDelta: "from A", isError: false)
        vm.handleDispatchActivity(dispatchAgentId: dispatchIdB, conversationId: sharedConvId, kind: "text", seq: 1, ts: 100, toolName: nil, toolId: nil, textDelta: "from B", isError: false)

        let bufferA = vm.agentDispatchActivity[dispatchIdA] ?? []
        let bufferB = vm.agentDispatchActivity[dispatchIdB] ?? []

        XCTAssertEqual(bufferA.count, 1, "dispatch A must have exactly one push entry")
        XCTAssertEqual(bufferB.count, 1, "dispatch B must have exactly one push entry")
        XCTAssertFalse(bufferA.contains { $0.content == "from B" },
            "dispatch A buffer must not contain dispatch B's entry")
        XCTAssertFalse(bufferB.contains { $0.content == "from A" },
            "dispatch B buffer must not contain dispatch A's entry")
    }

    func testActiveMergedTranscriptShowsOnlyOwnPushEntries() {
        let vm = SessionViewModel()
        let dispatchIdA = "dispatch-A"
        let dispatchIdB = "dispatch-B"
        let sharedConvId = "shared-conv-2"

        vm.handleDispatchActivity(dispatchAgentId: dispatchIdA, conversationId: sharedConvId, kind: "text", seq: 1, ts: 100, toolName: nil, toolId: nil, textDelta: "A output 1", isError: false)
        vm.handleDispatchActivity(dispatchAgentId: dispatchIdA, conversationId: sharedConvId, kind: "text", seq: 2, ts: 101, toolName: nil, toolId: nil, textDelta: "A output 2", isError: false)

        vm.handleDispatchActivity(dispatchAgentId: dispatchIdB, conversationId: sharedConvId, kind: "text", seq: 1, ts: 200, toolName: nil, toolId: nil, textDelta: "B output", isError: false)

        let mergedMsgs = vm.agentConversationMessages[sharedConvId] ?? []
        let contents = mergedMsgs.map { $0.content }
        XCTAssertTrue(contents.contains("B output"), "active dispatch B's text must be in merged transcript")
        XCTAssertFalse(contents.contains("A output 1"), "stale dispatch A's text must not appear in dispatch B's merged transcript")
        XCTAssertFalse(contents.contains("A output 2"), "stale dispatch A's text must not appear in dispatch B's merged transcript")
    }

    func testTerminalClearAfterRekeyRetainsSnapshot() {
        let vm = SessionViewModel()

        vm.handleDispatchActivity(dispatchAgentId: dispatchId, conversationId: convId, kind: "text", seq: 1, ts: 100, toolName: nil, toolId: nil, textDelta: "work", isError: false)
        let snap = [Message(id: "s1", role: .assistant, content: "final", timestamp: 1)]
        vm.handleAgentConversationHistory(agentName: "dev", conversationId: convId, messages: snap)

        let doneAgent = makeAgent(name: "dev", convId: convId, dispatchId: dispatchId, dispatchStatus: "done")
        vm.clearTerminalDispatchCaches(for: [doneAgent])

        // The snapshot (1 msg) is smaller than the merged transcript (2 msgs: 1 snap + 1 push entry).
        // Fix B: push is retained and a reload is triggered. Snapshot authority stays.
        XCTAssertNotNil(vm.agentSnapshotByConvId[convId], "snapshot (by convId) must be RETAINED after rekeying terminal clear")
        // Push is retained because merged (2) > snapshot (1): Fix B defers the clear.
        XCTAssertNotNil(vm.agentDispatchActivity[dispatchId],
            "push buffer must be RETAINED when merged > snapshot (Fix B defers clear to avoid transcript collapse)")
    }

    // MARK: - Helpers

    private func makeAgent(name: String, convId: String, dispatchId: String, dispatchStatus: String) -> AgentStateUpdate {
        let json = """
        {
          "name": "\(name)",
          "status": "\(dispatchStatus)",
          "metadata": {
            "displayName": "\(name)",
            "type": "specialist",
            "visibility": "always",
            "invited": true,
            "dispatches": [
              {
                "id": "\(dispatchId)",
                "task": "test task",
                "model": "claude-opus-4-8",
                "conversationId": "\(convId)",
                "status": "\(dispatchStatus)",
                "elapsed": 1.0
              }
            ]
          }
        }
        """
        let data = json.data(using: .utf8)!
        return try! JSONDecoder().decode(AgentStateUpdate.self, from: data)
    }
}
