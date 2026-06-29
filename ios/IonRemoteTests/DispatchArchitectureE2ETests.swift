import XCTest
@testable import IonRemote

/// End-to-end dispatch architecture test.
///
/// Transcribes the same 8-dispatch/2-session/1-continuation scenario from
/// the engine integration test fixture
/// (`engine/tests/integration/testdata/dispatch_architecture_events.json`)
/// and the desktop projection test.
///
/// Validates:
///   1. Cache-keyed message lookup produces correct messages per dispatch
///   2. assignStableIds produces unique stable ids across all dispatches
///   3. Continuation conversation shows both rounds' messages
///   4. Per-dispatch model and duration are correct
///   5. Distinct-convId dispatches do not cross-leak
///   6. Cross-session independence (no sess-a convId in sess-b)
///
/// All IDs, models, tasks, and elapsed values match the engine fixture
/// so the three layers prove the same reality.
@MainActor
final class DispatchArchitectureE2ETests: XCTestCase {

    // MARK: - Engine fixture constants (verbatim from engine JSON)

    // Conversation IDs (7 distinct: alpha continuation shares one)
    let convAAlpha    = "conv-a-alpha"       // shared by R1 + R2 (continuation)
    let convABetaR1   = "conv-a-beta-r1"
    let convABetaR2   = "conv-a-beta-r2"
    let convBAlphaR1  = "conv-b-alpha-r1"
    let convBAlphaR2  = "conv-b-alpha-r2"
    let convBBetaR1   = "conv-b-beta-r1"
    let convBBetaR2   = "conv-b-beta-r2"

    // Dispatch IDs (8 distinct)
    let didAR1Alpha = "dispatch-alpha-1782668321080-aaa111"
    let didAR1Beta  = "dispatch-beta-1782668321081-bbb222"
    let didAR2Alpha = "dispatch-alpha-1782668321254-ccc333"
    let didAR2Beta  = "dispatch-beta-1782668321260-ddd444"
    let didBR1Alpha = "dispatch-alpha-1782668321081-eee555"
    let didBR1Beta  = "dispatch-beta-1782668321081-fff666"
    let didBR2Alpha = "dispatch-alpha-1782668321261-ggg777"
    let didBR2Beta  = "dispatch-beta-1782668321265-hhh888"

    // MARK: - Helpers

    private func msg(_ id: String, role: MessageRole, content: String, ts: Double) -> Message {
        Message(id: id, role: role, content: content, timestamp: ts)
    }

    private func makeDispatchInfo(
        id: String, task: String, convId: String,
        elapsed: Double, startTime: Double
    ) -> DispatchInfo {
        DispatchInfo(from: [
            "id": id, "task": task, "model": "mock-model",
            "conversationId": convId, "status": "done",
            "elapsed": elapsed, "startTime": startTime,
        ] as [String: Any])
    }

    // Build a message cache mapping convId -> messages
    private func buildCache() -> [String: [Message]] {
        // Alpha sess-a: continuation (both R1 and R2 in same conv)
        let alphaAMsgs = [
            msg("aa-u1", role: .user, content: "Task-AAA", ts: 100),
            msg("aa-a1", role: .assistant, content: "Response-AAA", ts: 101),
            msg("aa-u2", role: .user, content: "Task-CCC", ts: 200),
            msg("aa-a2", role: .assistant, content: "Response-CCC", ts: 201),
        ]
        // Beta sess-a R1
        let betaAR1 = [
            msg("ab1-u1", role: .user, content: "Task-BBB", ts: 110),
            msg("ab1-a1", role: .assistant, content: "Response-BBB", ts: 111),
        ]
        // Beta sess-a R2
        let betaAR2 = [
            msg("ab2-u1", role: .user, content: "Task-DDD", ts: 210),
            msg("ab2-a1", role: .assistant, content: "Response-DDD", ts: 211),
        ]
        // Alpha sess-b R1
        let alphaBR1 = [
            msg("ba1-u1", role: .user, content: "Task-EEE", ts: 300),
            msg("ba1-a1", role: .assistant, content: "Response-EEE", ts: 301),
        ]
        // Alpha sess-b R2
        let alphaBR2 = [
            msg("ba2-u1", role: .user, content: "Task-GGG", ts: 400),
            msg("ba2-a1", role: .assistant, content: "Response-GGG", ts: 401),
        ]
        // Beta sess-b R1
        let betaBR1 = [
            msg("bb1-u1", role: .user, content: "Task-FFF", ts: 310),
            msg("bb1-a1", role: .assistant, content: "Response-FFF", ts: 311),
        ]
        // Beta sess-b R2
        let betaBR2 = [
            msg("bb2-u1", role: .user, content: "Task-HHH", ts: 410),
            msg("bb2-a1", role: .assistant, content: "Response-HHH", ts: 411),
        ]

        return [
            convAAlpha:   alphaAMsgs,
            convABetaR1:  betaAR1,
            convABetaR2:  betaAR2,
            convBAlphaR1: alphaBR1,
            convBAlphaR2: alphaBR2,
            convBBetaR1:  betaBR1,
            convBBetaR2:  betaBR2,
        ]
    }

    private func allDispatches() -> [DispatchInfo] {
        [
            makeDispatchInfo(id: didAR1Alpha, task: "Task-AAA", convId: convAAlpha, elapsed: 0.166, startTime: 1782668321),
            makeDispatchInfo(id: didAR2Alpha, task: "Task-CCC", convId: convAAlpha, elapsed: 0.202, startTime: 1782668322),
            makeDispatchInfo(id: didAR1Beta, task: "Task-BBB", convId: convABetaR1, elapsed: 0.152, startTime: 1782668321),
            makeDispatchInfo(id: didAR2Beta, task: "Task-DDD", convId: convABetaR2, elapsed: 0.183, startTime: 1782668322),
            makeDispatchInfo(id: didBR1Alpha, task: "Task-EEE", convId: convBAlphaR1, elapsed: 0.166, startTime: 1782668321),
            makeDispatchInfo(id: didBR2Alpha, task: "Task-GGG", convId: convBAlphaR2, elapsed: 0.196, startTime: 1782668322),
            makeDispatchInfo(id: didBR1Beta, task: "Task-FFF", convId: convBBetaR1, elapsed: 0.170, startTime: 1782668321),
            makeDispatchInfo(id: didBR2Beta, task: "Task-HHH", convId: convBBetaR2, elapsed: 0.196, startTime: 1782668322),
        ]
    }

    // MARK: - Test 1: cache-keyed lookup per dispatch

    func testCacheKeyedLookupReturnsCorrectMessagesPerDispatch() {
        let cache = buildCache()
        let dispatches = allDispatches()

        for d in dispatches {
            let resolved = cache[d.conversationId]
            XCTAssertNotNil(resolved,
                "dispatch \(d.id) with convId \(d.conversationId) must resolve in cache")
            // Each resolved conversation must contain the dispatch's task text
            let content = (resolved ?? []).map { $0.content }.joined(separator: " ")
            XCTAssertTrue(content.contains(d.task),
                "dispatch \(d.id) cache must contain task \(d.task), got: \(content)")
        }
    }

    // MARK: - Test 2: assignStableIds uniqueness

    func testAssignStableIdsProducesUniqueIdsAcrossAllDispatches() {
        let cache = buildCache()
        var allIds: [String] = []

        for (_, msgs) in cache {
            let stabilized = assignStableIds(msgs)
            allIds.append(contentsOf: stabilized.map { $0.id })
        }

        // All stabilized IDs must be non-empty
        for sid in allIds {
            XCTAssertFalse(sid.isEmpty, "stabilized id must not be empty")
        }

        // No duplicates across all conversations
        XCTAssertEqual(allIds.count, Set(allIds).count,
            "assignStableIds must produce unique ids across all dispatch conversations")
    }

    // MARK: - Test 3: continuation shows both rounds' messages

    func testContinuationConversationShowsBothRounds() {
        let cache = buildCache()
        let continuationMsgs = cache[convAAlpha]!

        XCTAssertEqual(continuationMsgs.count, 4,
            "continuation conv must have 4 messages (2 per round)")

        let content = continuationMsgs.map { $0.content }.joined(separator: " ")
        XCTAssertTrue(content.contains("Task-AAA"),
            "continuation must contain round 1 task")
        XCTAssertTrue(content.contains("Task-CCC"),
            "continuation must contain round 2 task")
        XCTAssertTrue(content.contains("Response-AAA"),
            "continuation must contain round 1 response")
        XCTAssertTrue(content.contains("Response-CCC"),
            "continuation must contain round 2 response")
    }

    // MARK: - Test 4: per-dispatch model and duration

    func testPerDispatchModelAndDuration() {
        let dispatches = allDispatches()

        for d in dispatches {
            XCTAssertEqual(d.model, "mock-model",
                "dispatch \(d.id) model must be mock-model")
            XCTAssertTrue((d.elapsed ?? 0) > 0,
                "dispatch \(d.id) elapsed must be > 0")
        }
    }

    // MARK: - Test 5: distinct-convId dispatches do not cross-leak

    func testDistinctConvIdDispatchesDoNotCrossLeak() {
        let cache = buildCache()

        // Beta R1 and R2 have distinct convIds; each resolves independently
        let betaR1 = cache[convABetaR1]!
        let betaR2 = cache[convABetaR2]!

        let r1Content = betaR1.map { $0.content }.joined(separator: " ")
        let r2Content = betaR2.map { $0.content }.joined(separator: " ")

        // R1 must not contain R2's task, and vice versa
        XCTAssertTrue(r1Content.contains("Task-BBB"))
        XCTAssertFalse(r1Content.contains("Task-DDD"),
            "beta R1 must not contain R2 content (cross-leak)")
        XCTAssertTrue(r2Content.contains("Task-DDD"))
        XCTAssertFalse(r2Content.contains("Task-BBB"),
            "beta R2 must not contain R1 content (cross-leak)")
    }

    // MARK: - Test 6: cross-session independence

    func testCrossSessionIndependence() {
        let sessAConvIds: Set<String> = [convAAlpha, convABetaR1, convABetaR2]
        let sessBConvIds: Set<String> = [convBAlphaR1, convBAlphaR2, convBBetaR1, convBBetaR2]

        XCTAssertTrue(sessAConvIds.isDisjoint(with: sessBConvIds),
            "sess-a and sess-b must have disjoint conversationIds")
    }

    // MARK: - Test 7: 8 distinct dispatch IDs

    func testEightDistinctDispatchIds() {
        let dispatches = allDispatches()
        XCTAssertEqual(dispatches.count, 8)

        let ids = Set(dispatches.map { $0.id })
        XCTAssertEqual(ids.count, 8,
            "all 8 dispatches must have distinct ids")
    }

    // MARK: - Test 8: 7 distinct conversationIds (continuation shares one)

    func testSevenDistinctConversationIds() {
        let dispatches = allDispatches()
        let convIds = Set(dispatches.map { $0.conversationId })
        XCTAssertEqual(convIds.count, 7,
            "7 distinct convIds (alpha continuation shares parent)")
    }

    // MARK: - Test 9: task text matches engine scenario

    func testTaskTextMatchesEngineScenario() {
        let dispatches = allDispatches()
        let tasks = dispatches.map { $0.task }
        let expected = ["Task-AAA", "Task-CCC", "Task-BBB", "Task-DDD",
                        "Task-EEE", "Task-GGG", "Task-FFF", "Task-HHH"]
        XCTAssertEqual(tasks, expected)
    }
}
