import XCTest
@testable import IonRemote

/// Defect A (iOS): a live user echo for a fresh, not-yet-loaded conversation
/// must render the user bubble.
///
/// Before the fix, `handleMessageAdded` short-circuited with
/// `guard conversationLoaded.contains(tabId) else { return }`, so an
/// iOS-started slash command on a fresh extension-hosted conversation — where
/// no history had loaded yet — dropped the desktop_message_added user echo and
/// NO user bubble appeared. The desktop forwards a user echo as a
/// desktop_message_added from its own remote-prompt path; for user/assistant
/// roles iOS marks the conversation loaded and inserts, then reconciles by id
/// so a later history reload heals without duplication.
@MainActor
final class MessageAddedFreshConversationTests: XCTestCase {

    private func makeMessage(id: String, role: MessageRole, content: String) -> Message {
        Message(id: id, role: role, content: content, timestamp: 1_700_000_000_000)
    }

    func testUserEchoRendersOnNotYetLoadedConversation() {
        let vm = SessionViewModel()
        // Conversation has NOT been loaded — the fresh-from-iOS slash case.
        XCTAssertFalse(vm.conversationLoaded.contains("fresh"))

        vm.handleMessageAdded(
            tabId: "fresh",
            message: makeMessage(id: "entry-1", role: .user, content: "/align the docs"),
        )

        // The user bubble renders despite the conversation not being loaded,
        // and the conversation is now marked loaded so subsequent live events
        // and the eventual history reload reconcile against it.
        XCTAssertTrue(vm.conversationLoaded.contains("fresh"))
        let msgs = vm.conversationMessages("fresh")
        XCTAssertEqual(msgs.count, 1)
        XCTAssertEqual(msgs.first?.id, "entry-1")
        XCTAssertEqual(msgs.first?.content, "/align the docs")
        XCTAssertEqual(msgs.first?.role, .user)
    }

    func testSecondEchoReconcilesByIdWithoutDuplicating() {
        let vm = SessionViewModel()

        vm.handleMessageAdded(
            tabId: "fresh",
            message: makeMessage(id: "entry-1", role: .user, content: "/align the docs"),
        )
        // A second event for the same entryId (e.g. the canonical version after
        // a history reload, or a re-broadcast) must replace by id, not append.
        vm.handleMessageAdded(
            tabId: "fresh",
            message: makeMessage(id: "entry-1", role: .user, content: "/align the docs"),
        )

        let msgs = vm.conversationMessages("fresh")
        XCTAssertEqual(msgs.count, 1, "same entryId must reconcile, not duplicate")
        XCTAssertEqual(msgs.first?.id, "entry-1")
    }

    func testNonUserRoleStillGuardedOnNotYetLoaded() {
        let vm = SessionViewModel()
        // A tool message on a not-yet-loaded conversation keeps the original
        // guard — only user/assistant echoes bypass it.
        vm.handleMessageAdded(
            tabId: "fresh",
            message: makeMessage(id: "t1", role: .tool, content: "tool output"),
        )
        XCTAssertFalse(vm.conversationLoaded.contains("fresh"))
        XCTAssertEqual(vm.conversationMessages("fresh").count, 0)
    }
}
