import XCTest
@testable import IonRemote

/// Regression tests for the post-#256 iOS streaming "stalls after first turn" fix.
///
/// Root cause: handleEngineTextDelta created a new assistant row whenever the
/// last message was sealed (role == .assistant && sealed == true). Combined with
/// the desktop-side message_end/text_delta FIFO race (fixed in event-wiring.ts),
/// late-arriving text deltas that landed after the seal event created spurious
/// extra assistant rows. Each turn compounded the damage until the snapshot
/// reconcile replaced the iOS list.
///
/// Fix (Fix 2): handleEngineTextDelta now appends to the existing assistant row
/// regardless of its sealed state. A genuinely new turn is identified by a
/// NON-assistant trailing message (e.g. a tool row). The sealed flag is cleared
/// so subsequent deltas for the same run keep appending without re-entering
/// this branch.
///
/// These tests go RED on the pre-fix code (which created a second bubble for
/// text arriving after a seal) and GREEN on the fixed code.
@MainActor
final class EngineTextDeltaOrderingTests: XCTestCase {

    // MARK: - Helpers

    private func makeAssistant(content: String, sealed: Bool = false) -> Message {
        var m = Message(id: UUID().uuidString, role: .assistant, content: content,
                        timestamp: 1_700_000_000_000)
        m.sealed = sealed
        return m
    }

    private func makeTool(toolId: String) -> Message {
        Message(id: toolId, role: .tool, content: "", toolName: "Bash",
                toolId: toolId, toolStatus: .completed, timestamp: 1_700_000_000_001)
    }

    // MARK: - Fix 2: sealed assistant is NOT a new-turn boundary

    func testDeltaAfterSealAppendsToExistingRowNotNewBubble() {
        // Arrange: one sealed assistant message (simulates message_end arrived
        // before the final text batch — the pre-fix race condition).
        let vm = SessionViewModel()
        vm.mutateEngineInstance(tabId: "tab1", instanceId: nil) { inst in
            inst.messages = [self.makeAssistant(content: "partial", sealed: true)]
        }

        // Act: a late text delta arrives after the seal.
        vm.handleEngineTextDelta(tabId: "tab1", instanceId: nil, text: " tail")

        // Assert: still ONE assistant message, content extended (no second bubble).
        let msgs = vm.conversationMessages("tab1")
        XCTAssertEqual(msgs.count, 1, "A late delta must append to the existing row, not create a second bubble")
        XCTAssertEqual(msgs[0].content, "partial tail")
        XCTAssertEqual(msgs[0].role, .assistant)
    }

    func testDeltaAfterSealUnsealsTheRow() {
        // The fix unseals the row so subsequent deltas for the same run keep
        // appending without re-entering the "sealed" branch.
        let vm = SessionViewModel()
        vm.mutateEngineInstance(tabId: "tab1", instanceId: nil) { inst in
            inst.messages = [self.makeAssistant(content: "text", sealed: true)]
        }

        vm.handleEngineTextDelta(tabId: "tab1", instanceId: nil, text: " more")
        vm.handleEngineTextDelta(tabId: "tab1", instanceId: nil, text: " content")

        let msgs = vm.conversationMessages("tab1")
        XCTAssertEqual(msgs.count, 1)
        XCTAssertEqual(msgs[0].content, "text more content")
        XCTAssertFalse(msgs[0].sealed, "Row must be unsealed after delta so the next delta appends too")
    }

    func testDeltaAfterToolRowOpensNewAssistantRow() {
        // A tool row as the last message IS the genuine new-turn signal.
        // The fix must still open a fresh assistant row in this case.
        let vm = SessionViewModel()
        vm.mutateEngineInstance(tabId: "tab1", instanceId: nil) { inst in
            inst.messages = [
                self.makeAssistant(content: "turn 1 text", sealed: true),
                self.makeTool(toolId: "toolu_1"),
            ]
        }

        vm.handleEngineTextDelta(tabId: "tab1", instanceId: nil, text: "turn 2 text")

        let msgs = vm.conversationMessages("tab1")
        XCTAssertEqual(msgs.count, 3, "After a tool row a new assistant row must open")
        XCTAssertEqual(msgs[2].role, .assistant)
        XCTAssertEqual(msgs[2].content, "turn 2 text")
    }

    func testMultipleDeltasExtendSingleRowWhenNoPriorMessage() {
        // Baseline: empty instance — first delta creates the assistant row,
        // subsequent deltas extend it.
        let vm = SessionViewModel()

        vm.handleEngineTextDelta(tabId: "tab1", instanceId: nil, text: "Hello")
        vm.handleEngineTextDelta(tabId: "tab1", instanceId: nil, text: " world")

        let msgs = vm.conversationMessages("tab1")
        XCTAssertEqual(msgs.count, 1)
        XCTAssertEqual(msgs[0].content, "Hello world")
    }

    func testHandleEngineMessageEndSealsThenDeltaStillAppends() {
        // Full integration: text → message_end (seal) → late text delta.
        // The late delta must append, not create a second bubble.
        let vm = SessionViewModel()

        vm.handleEngineTextDelta(tabId: "tab1", instanceId: nil, text: "First part")
        vm.handleEngineMessageEnd(tabId: "tab1", instanceId: nil, inputTokens: 10, contextPercent: 0.1)

        // Verify the seal was applied.
        let afterSeal = vm.conversationMessages("tab1")
        XCTAssertEqual(afterSeal.count, 1)
        XCTAssertTrue(afterSeal[0].sealed, "message_end must seal the assistant row")

        // Simulate the late delta (FIFO race: timer fired after message_end).
        vm.handleEngineTextDelta(tabId: "tab1", instanceId: nil, text: " (late tail)")

        let final = vm.conversationMessages("tab1")
        XCTAssertEqual(final.count, 1, "Late delta must not create a second bubble")
        XCTAssertEqual(final[0].content, "First part (late tail)")
    }
}
