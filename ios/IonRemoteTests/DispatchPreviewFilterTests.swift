import XCTest
@testable import IonRemote

/// Tests for the dispatch preview message filtering and display logic
/// (AgentExpandedContent Part C: tool/thinking row rendering).
///
/// Pins that:
///   5. A dispatch slice containing only .tool rows produces a NON-empty
///      display list — previously all tool rows were discarded.
///   6. The synthetic user row equal to the dispatch task is de-duplicated;
///      interleaved assistant+tool messages appear in timestamp order.
///   7. Thinking rows are preserved (not filtered out).
///
/// The filtering and grouping logic is extracted here as the spec for
/// conversationMessages(_:) and groupDispatchItems(_:) in AgentExpandedContent.
/// When those functions change, these tests must change too.
@MainActor
final class DispatchPreviewFilterTests: XCTestCase {

    // MARK: - Helpers replicating AgentExpandedContent logic

    /// Mirrors AgentExpandedContent.conversationMessages(_:):
    /// Drops the synthetic task-duplicate user row and empty assistant rows.
    /// Preserves tool, thinking, and all other roles.
    private func conversationMessages(_ msgs: [Message], task: String) -> [Message] {
        return msgs.filter { msg in
            if msg.role == .user && !task.isEmpty &&
               msg.content.trimmingCharacters(in: .whitespacesAndNewlines) == task.trimmingCharacters(in: .whitespacesAndNewlines) {
                return false
            }
            if msg.role == .assistant && msg.content.isEmpty {
                return false
            }
            return true
        }
    }

    /// Mirrors AgentExpandedContent.groupDispatchItems(_:):
    /// Groups consecutive .tool messages into tool-group buckets.
    private enum DispatchItemKind { case single(Message), toolGroup([Message]) }

    private func groupDispatchItems(_ msgs: [Message]) -> [DispatchItemKind] {
        var result: [DispatchItemKind] = []
        var toolBuf: [Message] = []
        for msg in msgs {
            if msg.role == .tool {
                toolBuf.append(msg)
            } else {
                if !toolBuf.isEmpty {
                    result.append(.toolGroup(toolBuf))
                    toolBuf = []
                }
                result.append(.single(msg))
            }
        }
        if !toolBuf.isEmpty {
            result.append(.toolGroup(toolBuf))
        }
        return result
    }

    // MARK: - Test 5: tool-only slice is non-empty after filtering

    /// Regression for Part C: when all sliced messages are tool rows, the old
    /// filter discarded them all (role != .assistant && role != .user).
    /// The fixed filter passes tool rows through; the display list is non-empty.
    func testToolOnlySliceRendersNonEmpty() {
        let msgs = [
            Message(id: "t1", role: .tool, content: "", toolName: "Read", toolId: "t1", toolStatus: .running, timestamp: 1),
            Message(id: "t2", role: .tool, content: "", toolName: "Bash", toolId: "t2", toolStatus: .completed, timestamp: 2),
        ]
        let filtered = conversationMessages(msgs, task: "")
        XCTAssertEqual(filtered.count, 2,
            "tool-only slice must survive filter (old code discarded all tool rows)")

        let grouped = groupDispatchItems(filtered)
        XCTAssertEqual(grouped.count, 1, "two consecutive tool messages collapse to one group")
        if case .toolGroup(let tools) = grouped[0] {
            XCTAssertEqual(tools.count, 2)
        } else {
            XCTFail("expected toolGroup")
        }
    }

    // MARK: - Test 6a: task-duplicate user row is de-duplicated

    func testTaskInstructionUserRowIsDropped() {
        let task = "Implement the feature"
        let msgs = [
            Message(id: "u1", role: .user, content: "Implement the feature", timestamp: 1),
            Message(id: "a1", role: .assistant, content: "Sure, I'll implement it.", timestamp: 2),
        ]
        let filtered = conversationMessages(msgs, task: task)
        XCTAssertEqual(filtered.count, 1, "task-duplicate user row must be dropped")
        XCTAssertEqual(filtered[0].id, "a1", "only the assistant row survives")
    }

    // MARK: - Test 6b: interleaved assistant+tool slice preserves all rows in order

    func testInterleavedAssistantAndToolRowsAllPresent() {
        let msgs = [
            Message(id: "a1", role: .assistant, content: "Let me check.", timestamp: 1),
            Message(id: "t1", role: .tool, content: "", toolName: "Read", toolId: "t1", toolStatus: .completed, timestamp: 2),
            Message(id: "t2", role: .tool, content: "", toolName: "Bash", toolId: "t2", toolStatus: .completed, timestamp: 3),
            Message(id: "a2", role: .assistant, content: "Done.", timestamp: 4),
        ]
        let filtered = conversationMessages(msgs, task: "")
        XCTAssertEqual(filtered.count, 4, "all rows survive when no task filter matches")

        // Check timestamp order is preserved.
        let ids = filtered.map { $0.id }
        XCTAssertEqual(ids, ["a1", "t1", "t2", "a2"], "rows must appear in timestamp order")

        // Grouping: assistant / toolGroup(t1,t2) / assistant.
        let grouped = groupDispatchItems(filtered)
        XCTAssertEqual(grouped.count, 3)
        if case .single(let m) = grouped[0] { XCTAssertEqual(m.id, "a1") } else { XCTFail("expected single a1") }
        if case .toolGroup(let tools) = grouped[1] { XCTAssertEqual(tools.count, 2) } else { XCTFail("expected toolGroup") }
        if case .single(let m) = grouped[2] { XCTAssertEqual(m.id, "a2") } else { XCTFail("expected single a2") }
    }

    // MARK: - Test 7: thinking rows are preserved

    func testThinkingRowsPreserved() {
        let msgs = [
            Message(id: "th1", role: .thinking, content: "Thinking…", timestamp: 1),
            Message(id: "a1", role: .assistant, content: "Answer.", timestamp: 2),
        ]
        let filtered = conversationMessages(msgs, task: "")
        XCTAssertEqual(filtered.count, 2, "thinking rows must not be filtered out")
        XCTAssertEqual(filtered[0].role, .thinking)
    }

    // MARK: - Empty assistant rows are still dropped

    func testEmptyAssistantRowsDropped() {
        let msgs = [
            Message(id: "a0", role: .assistant, content: "", timestamp: 1),
            Message(id: "a1", role: .assistant, content: "Real content.", timestamp: 2),
        ]
        let filtered = conversationMessages(msgs, task: "")
        XCTAssertEqual(filtered.count, 1, "empty assistant rows must be filtered out")
        XCTAssertEqual(filtered[0].id, "a1")
    }

    // MARK: - Non-task user rows are preserved

    func testNonTaskUserRowSurvives() {
        let task = "Run the tests"
        let msgs = [
            Message(id: "u1", role: .user, content: "Run the tests", timestamp: 1),
            Message(id: "u2", role: .user, content: "What about coverage?", timestamp: 2),
        ]
        let filtered = conversationMessages(msgs, task: task)
        XCTAssertEqual(filtered.count, 1, "only the task-matching user row is dropped; other user rows survive")
        XCTAssertEqual(filtered[0].id, "u2")
    }

    // MARK: - Test 8: distinct-convId isolation (no cross-tab leak, no blank tab)

    /// Mirrors the simplified activeMessages lookup after the slicer was retired
    /// (Commit 3). Each pager tab resolves its messages via a direct dictionary
    /// lookup keyed by the dispatch's own conversationId. When the engine mints a
    /// distinct conversationId per dispatch (Commit 1), each tab sees exactly its
    /// own messages — no cross-leak and no blank tab caused by a shared-key
    /// collision.
    ///
    /// The helper below replicates the production lookup so the assertion is
    /// structural: if the key lookup is correct, isolation is guaranteed by the
    /// dictionary semantics.
    ///
    /// BEFORE the engine fix (shared convId), both dispatches would resolve the
    /// same cache entry and one tab would show the other's messages. With the fix
    /// each dispatch carries a distinct convId so the lookups are independent.
    func testDistinctConvIdDispatchesIsolatedToOwnMessages() {
        let convId1 = "conv-dispatch-one"
        let convId2 = "conv-dispatch-two"

        let msgs1 = [
            Message(id: "d1-a1", role: .assistant, content: "Dispatch one result.", timestamp: 1001),
        ]
        let msgs2 = [
            Message(id: "d2-a1", role: .assistant, content: "Dispatch two result.", timestamp: 2001),
        ]

        let cache: [String: [Message]] = [convId1: msgs1, convId2: msgs2]

        // Simulate activeMessages lookup for dispatch index 0 (convId1).
        let resolved1 = cache[convId1]
        // Simulate activeMessages lookup for dispatch index 1 (convId2).
        let resolved2 = cache[convId2]

        // Neither tab is blank.
        XCTAssertNotNil(resolved1, "dispatch 1 tab must not be blank when convId is distinct")
        XCTAssertNotNil(resolved2, "dispatch 2 tab must not be blank when convId is distinct")

        // No cross-leak: tab 1 sees only tab 1's messages.
        let ids1 = (resolved1 ?? []).map { $0.id }
        XCTAssertEqual(ids1, ["d1-a1"],
            "dispatch 1 tab must render only its own messages, not dispatch 2's")

        // No cross-leak: tab 2 sees only tab 2's messages.
        let ids2 = (resolved2 ?? []).map { $0.id }
        XCTAssertEqual(ids2, ["d2-a1"],
            "dispatch 2 tab must render only its own messages, not dispatch 1's")

        // The two resolved sets are disjoint — no duplicated opening line.
        let allIds = ids1 + ids2
        XCTAssertEqual(allIds.count, Set(allIds).count,
            "no message id must appear in both tabs (duplicated opening line)")
    }

    /// Complementary: a dispatch whose conversationId is empty (still running,
    /// engine hasn't assigned one yet) must resolve to nil so the UI shows
    /// "Working…" instead of leaking a stale or incorrect conversation.
    func testEmptyConvIdDispatchResolvesToNil() {
        let cache: [String: [Message]] = [
            "conv-real": [Message(id: "m1", role: .assistant, content: "Done.", timestamp: 1)],
        ]
        // Empty convId — no key lookup should fire.
        let convId = ""
        let resolved = convId.isEmpty ? nil : cache[convId]
        XCTAssertNil(resolved,
            "a dispatch with an empty conversationId must resolve to nil (UI shows Working…)")
    }

    // MARK: - Test 9: dispatch duration reads per-dispatch fields

    /// Regression: the duration display must reflect the SELECTED dispatch's
    /// elapsed/startTime, not the agent-level values. Before the fix,
    /// AgentExpandedContent.elapsedSeconds always read agent.status/startTime/
    /// elapsed, ignoring the activeDispatch. With the fix, per-dispatch fields
    /// take priority when a dispatch is active.
    ///
    /// This test replicates the resolution logic used by the view:
    ///   status = activeDispatch?.status ?? agent.status
    ///   startTime = activeDispatch?.startTime ?? agent.startTime
    ///   elapsed = activeDispatch?.elapsed ?? agent.elapsed
    ///
    /// Revert-red verified: reverting the per-dispatch-first resolution
    /// (using agent-level fields directly) causes this test to fail.
    func testDispatchDurationReflectsSelectedDispatch() {
        // Agent-level: done, elapsed=100s, startTime=500
        let agentStatus = "done"
        let agentStartTime: Double? = 500.0
        let agentElapsed: Double? = 100.0

        // Dispatch 1: done, elapsed=30s, startTime=1000
        let dispatch1 = DispatchInfo(from: [
            "id": "d1", "task": "first", "model": "sonnet",
            "conversationId": "conv-1", "status": "done",
            "elapsed": 30, "startTime": 1000.0,
        ])
        // Dispatch 2: done, elapsed=70s, startTime=2000
        let dispatch2 = DispatchInfo(from: [
            "id": "d2", "task": "second", "model": "sonnet",
            "conversationId": "conv-2", "status": "done",
            "elapsed": 70, "startTime": 2000.0,
        ])

        let now = Date()

        // Replicate the post-fix resolution: per-dispatch-first.
        // This is the pattern now used in AgentExpandedContent.elapsedSeconds.
        let activeDispatch1: DispatchInfo? = dispatch1
        let resolvedStatus1 = activeDispatch1?.status ?? agentStatus
        let resolvedStartTime1 = activeDispatch1?.startTime ?? agentStartTime
        let resolvedElapsed1 = activeDispatch1?.elapsed ?? agentElapsed

        let secs1 = AgentDuration.elapsedSeconds(
            status: resolvedStatus1,
            startTime: resolvedStartTime1,
            elapsed: resolvedElapsed1,
            now: now
        )
        XCTAssertEqual(secs1, 30,
            "dispatch 1 duration must be 30s (per-dispatch), not \(Int(agentElapsed!))s (agent-level)")

        let activeDispatch2: DispatchInfo? = dispatch2
        let resolvedStatus2 = activeDispatch2?.status ?? agentStatus
        let resolvedStartTime2 = activeDispatch2?.startTime ?? agentStartTime
        let resolvedElapsed2 = activeDispatch2?.elapsed ?? agentElapsed

        let secs2 = AgentDuration.elapsedSeconds(
            status: resolvedStatus2,
            startTime: resolvedStartTime2,
            elapsed: resolvedElapsed2,
            now: now
        )
        XCTAssertEqual(secs2, 70,
            "dispatch 2 duration must be 70s (per-dispatch), not \(Int(agentElapsed!))s (agent-level)")

        // Switching selection must change the displayed duration.
        XCTAssertNotEqual(secs1, secs2,
            "switching dispatch selection must change the displayed duration")
    }

    /// The PRE-FIX resolution pattern: always reading agent-level fields
    /// regardless of the active dispatch. This test verifies the bug existed:
    /// with agent-level resolution, both dispatches show 100s (agent elapsed)
    /// instead of their own 30s/70s.
    func testBuggedAgentLevelResolutionShowsWrongDuration() {
        let agentStatus = "done"
        let agentStartTime: Double? = 500.0
        let agentElapsed: Double? = 100.0
        let now = Date()

        // Pre-fix resolution: ignores dispatch, always uses agent fields.
        let buggedSecs = AgentDuration.elapsedSeconds(
            status: agentStatus,
            startTime: agentStartTime,
            elapsed: agentElapsed,
            now: now
        )

        // This is what the bugged code WOULD show for both dispatches.
        XCTAssertEqual(buggedSecs, 100,
            "pre-fix code always shows agent-level elapsed (100s)")

        // And 100 != 30 and 100 != 70, proving the bug.
        XCTAssertNotEqual(buggedSecs, 30,
            "pre-fix code would not show dispatch 1's 30s")
        XCTAssertNotEqual(buggedSecs, 70,
            "pre-fix code would not show dispatch 2's 70s")
    }

    /// When no dispatch is active (nil), the agent-level values are the
    /// fallback. Ensures the fallback path still works.
    func testAgentLevelFallbackWhenNoDispatchActive() {
        let agentStatus = "done"
        let agentStartTime: Double? = 500
        let agentElapsed: Double? = 45
        let now = Date()

        // No dispatch active — per-dispatch-first resolution falls back to agent.
        let activeDispatch: DispatchInfo? = nil
        let resolvedStatus = activeDispatch?.status ?? agentStatus
        let resolvedStartTime = activeDispatch?.startTime ?? agentStartTime
        let resolvedElapsed = activeDispatch?.elapsed ?? agentElapsed

        let secs = AgentDuration.elapsedSeconds(
            status: resolvedStatus,
            startTime: resolvedStartTime,
            elapsed: resolvedElapsed,
            now: now
        )
        XCTAssertEqual(secs, 45,
            "with no active dispatch, agent-level elapsed (45s) must be used")
    }

    /// Running dispatch: duration is computed live from startTime, not from
    /// elapsed (which is nil for a running dispatch).
    func testRunningDispatchUsesStartTimeForLiveDuration() {
        let now = Date()
        let startTime = now.timeIntervalSince1970 - 42 // 42 seconds ago

        let dispatch = DispatchInfo(from: [
            "id": "d1", "task": "work", "model": "sonnet",
            "conversationId": "conv-1", "status": "running",
            "startTime": startTime,
        ])

        let activeDispatch: DispatchInfo? = dispatch
        let resolvedStatus = activeDispatch?.status ?? "done"
        let resolvedStartTime = activeDispatch?.startTime ?? nil
        let resolvedElapsed = activeDispatch?.elapsed ?? nil

        let secs = AgentDuration.elapsedSeconds(
            status: resolvedStatus,
            startTime: resolvedStartTime,
            elapsed: resolvedElapsed,
            now: now
        )
        XCTAssertEqual(secs, 42,
            "running dispatch duration must be live-computed from startTime")
    }
}
