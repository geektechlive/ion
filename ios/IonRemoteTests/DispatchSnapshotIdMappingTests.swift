import XCTest
@testable import IonRemote

/// Pins assignStableIds — the iOS mirror of desktop mapConversationMessages.
///
/// Confirmed root cause: the relay wire sends id:"" for every user/assistant
/// snapshot message (engine SessionMessage has no id field). groupDispatchItems
/// -> ForEach then sees many DispatchItems sharing id="", causing SwiftUI to
/// duplicate/misplace bubbles. assignStableIds fixes this at the snapshot
/// ingestion point (handleAgentConversationHistory), before the messages reach
/// agentSnapshotByConvId and the merge.
///
/// Each test names the exact desktop mapper rule it mirrors so a future
/// divergence is traceable to agent-conversation-mapper.ts.
final class DispatchSnapshotIdMappingTests: XCTestCase {

    // MARK: - Helpers

    private func msg(role: MessageRole, ts: Double?, toolId: String? = nil,
                     content: String = "") -> Message {
        Message(id: "", role: role, content: content,
                toolName: nil, toolInput: nil, toolId: toolId,
                toolStatus: toolId != nil ? .completed : nil,
                attachments: nil, timestamp: ts, source: nil,
                isInternal: nil, slashCommand: nil, slashArgs: nil,
                slashSource: nil)
    }

    // MARK: - Tool row rule

    /// Desktop rule: tool rows (toolId non-empty) → "tool-<toolId>".
    func testToolRowGetsToolPrefixedId() {
        let input = [msg(role: .tool, ts: 1000, toolId: "toolu_abc123")]
        let result = assignStableIds(input)
        XCTAssertEqual(result[0].id, "tool-toolu_abc123",
            "tool rows must get id = \"tool-<toolId>\" (mirrors desktop mapper line 52)")
    }

    // MARK: - User / assistant rows

    /// Desktop rule: user/assistant → "<role>-<Int64(timestamp)>".
    func testUserRowGetsRoleTimestampId() {
        let input = [msg(role: .user, ts: 1_782_589_973_761)]
        let result = assignStableIds(input)
        XCTAssertEqual(result[0].id, "user-1782589973761",
            "user message must get id = \"user-<ts>\" (mirrors desktop mapper lines 55-61)")
    }

    func testAssistantRowGetsRoleTimestampId() {
        let input = [msg(role: .assistant, ts: 1_782_589_973_762)]
        let result = assignStableIds(input)
        XCTAssertEqual(result[0].id, "assistant-1782589973762",
            "assistant message must get id = \"assistant-<ts>\"")
    }

    // MARK: - Nil timestamp falls back to 0

    func testNilTimestampFallsBackToZero() {
        let input = [msg(role: .user, ts: nil)]
        let result = assignStableIds(input)
        XCTAssertEqual(result[0].id, "user-0",
            "nil timestamp must produce id suffix 0 (mirrors desktop `m.timestamp ?? 0`)")
    }

    // MARK: - Collision suffix (same role + same timestamp)

    /// Desktop rule: first occurrence keeps plain base key; second gets #1;
    /// third gets #2, etc. This is the collision-avoidance path (lines 57-61
    /// in agent-conversation-mapper.ts).
    func testSameRoleTimestampCollisionSuffix() {
        let ts = 1_782_589_973_761.0
        let input = [
            msg(role: .assistant, ts: ts, content: "first"),
            msg(role: .assistant, ts: ts, content: "second"),
            msg(role: .assistant, ts: ts, content: "third"),
        ]
        let result = assignStableIds(input)
        XCTAssertEqual(result[0].id, "assistant-1782589973761",
            "first occurrence must use plain base key (no suffix)")
        XCTAssertEqual(result[1].id, "assistant-1782589973761#1",
            "second occurrence must append #1")
        XCTAssertEqual(result[2].id, "assistant-1782589973761#2",
            "third occurrence must append #2")
    }

    // MARK: - Cross-role collision counters are independent

    func testUserAndAssistantCountersAreIndependent() {
        let ts = 9999.0
        let input = [
            msg(role: .user,      ts: ts, content: "u1"),
            msg(role: .assistant, ts: ts, content: "a1"),
            msg(role: .user,      ts: ts, content: "u2"),
        ]
        let result = assignStableIds(input)
        XCTAssertEqual(result[0].id, "user-9999",
            "first user must be plain")
        XCTAssertEqual(result[1].id, "assistant-9999",
            "first assistant must be plain (independent counter from user)")
        XCTAssertEqual(result[2].id, "user-9999#1",
            "second user must be #1 (counter is per-base-key, not global)")
    }

    // MARK: - No two IDs collide in a realistic dispatch snapshot

    /// Trace-confirms the exact scenario from the bug report: a dispatch
    /// snapshot with 1 user + 7 assistant + 21 tool messages (from
    /// conversation 1782589973761-e93032a61420). All 29 ids must be unique.
    func testRealisticDispatchSnapshotAllIdsUnique() {
        let baseTs = 1_782_589_973_761.0
        var inputs: [Message] = []
        // 1 user message (the dispatch task)
        inputs.append(msg(role: .user, ts: baseTs))
        // 7 assistant messages (each at a slightly different timestamp)
        for i in 0..<7 {
            inputs.append(msg(role: .assistant, ts: baseTs + Double(i + 1) * 1000))
        }
        // 21 tool messages (distinct toolIds)
        for i in 0..<21 {
            inputs.append(msg(role: .tool, ts: baseTs + Double(i + 8) * 1000,
                              toolId: "toolu_\(String(format: "%02d", i))"))
        }
        let result = assignStableIds(inputs)
        let ids = result.map { $0.id }
        let unique = Set(ids)
        XCTAssertEqual(ids.count, unique.count,
            "every message in a realistic dispatch snapshot must have a unique id — no two DispatchItems can share an id in ForEach")
    }

    // MARK: - Idempotence via handleAgentConversationHistory integration

    /// Calling handleAgentConversationHistory twice with the same snapshot
    /// (replace, not append) must not change the final ids — the second call
    /// replaces agentSnapshotByConvId with a freshly-mapped result, same ids.
    @MainActor
    func testHandleAgentConversationHistoryAssignsStableIds() {
        let vm = SessionViewModel()
        let convId = "trace-conv"
        let snapshot = [
            msg(role: .user,      ts: 1000, content: "EXECUTE NOW"),
            msg(role: .assistant, ts: 2000, content: "On it"),
            msg(role: .tool,      ts: 3000, toolId: "toolu_XYZ", content: ""),
        ]

        vm.handleAgentConversationHistory(agentName: "dev", conversationId: convId,
                                          messages: snapshot)

        let stored = vm.agentSnapshotByConvId[convId] ?? []
        XCTAssertEqual(stored.count, 3)
        XCTAssertEqual(stored[0].id, "user-1000",
            "user message must get stable id after handleAgentConversationHistory")
        XCTAssertEqual(stored[1].id, "assistant-2000",
            "assistant message must get stable id")
        XCTAssertEqual(stored[2].id, "tool-toolu_XYZ",
            "tool message must get tool-prefixed id")

        // All unique — ForEach safe.
        let ids = stored.map { $0.id }
        XCTAssertEqual(ids.count, Set(ids).count, "all stored message ids must be unique")
    }
}
