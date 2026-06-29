import XCTest
@testable import IonRemote

/// Regression test for the main-conversation freeze on dropped live deltas
/// (e.g. a LAN↔relay transport switch mid-stream): the snapshot staleness
/// reconcile (v2 — tail fingerprint) must re-fetch authoritative history when
/// iOS's local tail fingerprint diverges from the desktop's, and must NOT
/// thrash when in sync.
///
/// Reverting `maybeReconcileStaleConversation` (or breaking fingerprint parity)
/// turns the heal/parity assertions red.
@MainActor
final class ConversationStalenessReconcileTests: XCTestCase {

    private func makeTab(id: String, convFingerprint: String?, status: TabStatus = .idle) -> RemoteTabState {
        var tab = RemoteTabState(
            id: id,
            title: id,
            customTitle: nil,
            status: status,
            workingDirectory: "/tmp",
            permissionMode: .auto,
            thinkingEffort: nil,
            permissionQueue: [],
            hasEngineExtension: false
        )
        tab.convFingerprint = convFingerprint
        return tab
    }

    private func msg(id: String, role: MessageRole, content: String, toolStatus: ToolStatus? = nil) -> Message {
        Message(id: id, role: role, content: content, toolStatus: toolStatus, timestamp: 1)
    }

    // MARK: - Cross-platform parity anchor

    /// The golden string MUST match the desktop's
    /// conversation-fingerprint.test.ts "produces the pinned golden string"
    /// case byte-for-byte. If either side's algorithm changes, one of the two
    /// tests fails — that is the parity guard.
    func testFingerprintGoldenStringMatchesDesktop() {
        let vm = SessionViewModel()
        let msgs = [
            msg(id: "u1", role: .user, content: "hello"),         // 5 bytes
            msg(id: "a1", role: .assistant, content: "hi there"), // 8 bytes
            msg(id: "t1", role: .tool, content: "whatever", toolStatus: .running),
        ]
        XCTAssertEqual(vm.conversationTailFingerprint(msgs), "u1:5,a1:8,t1:tr")
    }

    /// Pagination-safety regression for the reload-flash bug: iOS holds a
    /// paginated PAGE while the desktop holds the FULL list. When both share the
    /// same final tail, the fingerprints must be EQUAL (no total-count term), or
    /// the heal reloads on every snapshot.
    func testFingerprintPaginationSafe() {
        let vm = SessionViewModel()
        var sharedTail: [Message] = []
        for i in 0..<10 { sharedTail.append(msg(id: "tail-\(i)", role: .assistant, content: "t \(i)")) }
        var page = (0..<40).map { msg(id: "page-\($0)", role: .assistant, content: "p \($0)") }
        page.append(contentsOf: sharedTail)
        var full = (0..<490).map { msg(id: "old-\($0)", role: .assistant, content: "old \($0)") }
        full.append(contentsOf: sharedTail)
        XCTAssertEqual(vm.conversationTailFingerprint(page), vm.conversationTailFingerprint(full))
    }

    func testFingerprintUsesUTF8ByteLength() {
        let vm = SessionViewModel()
        // "é" is 1 UTF-16 unit but 2 UTF-8 bytes; "ab" is 2 bytes. Equal byte
        // length → equal fingerprint, proving byte (not UTF-16) length.
        let ascii = [msg(id: "a", role: .assistant, content: "ab")]
        let accent = [msg(id: "a", role: .assistant, content: "é")]
        XCTAssertEqual(
            vm.conversationTailFingerprint(ascii),
            vm.conversationTailFingerprint(accent)
        )
        let emoji = [msg(id: "a", role: .assistant, content: "😀")] // 4 bytes
        XCTAssertNotEqual(
            vm.conversationTailFingerprint(ascii),
            vm.conversationTailFingerprint(emoji)
        )
    }

    func testFingerprintToolStatusOnlyImmuneToTruncation() {
        let vm = SessionViewModel()
        // Desktop sees full tool content; history page truncates >2KB. Tool rows
        // are fingerprinted by status only, so the two must match (no reload loop).
        let full = [msg(id: "t1", role: .tool, content: String(repeating: "x", count: 5000), toolStatus: .completed)]
        let truncated = [msg(id: "t1", role: .tool, content: String(repeating: "x", count: 2048) + "\n... [truncated]", toolStatus: .completed)]
        XCTAssertEqual(
            vm.conversationTailFingerprint(full),
            vm.conversationTailFingerprint(truncated)
        )
    }

    // MARK: - Heal behavior

    /// Desktop fingerprint diverges from local (a dropped tool_end: desktop says
    /// completed, local still running) → heal fires (loadConversation clears
    /// messages, drops the loaded mark, marks the tab loading).
    func testHealsWhenFingerprintDiverges() {
        let vm = SessionViewModel()
        let tab = "tab-stale"
        // Local: tool still "running" (its tool_end delta was dropped).
        vm.handleConversationHistory(tabId: tab, newMessages: [msg(id: "t1", role: .tool, content: "out", toolStatus: .running)], hasMore: false, cursor: nil)
        XCTAssertTrue(vm.conversationLoaded.contains(tab))

        // Desktop snapshot fingerprint reflects the completed tool.
        let desktopFp = vm.conversationTailFingerprint([msg(id: "t1", role: .tool, content: "out", toolStatus: .completed)])
        vm.maybeReconcileStaleConversation(tab: makeTab(id: tab, convFingerprint: desktopFp))

        XCTAssertTrue(vm.loadingConversation.contains(tab), "diverged fingerprint must re-fetch history")
        XCTAssertFalse(vm.conversationLoaded.contains(tab), "re-fetch clears the loaded mark until history returns")
        XCTAssertEqual(vm.conversationMessages(tab).count, 0, "re-fetch clears the stale transcript")
    }

    /// Fingerprints match → no heal, no thrash (the in-sync streaming case).
    func testNoHealWhenFingerprintMatches() {
        let vm = SessionViewModel()
        let tab = "tab-sync"
        let msgs = [msg(id: "a1", role: .assistant, content: "hello")]
        vm.handleConversationHistory(tabId: tab, newMessages: msgs, hasMore: false, cursor: nil)
        let inSyncFp = vm.conversationTailFingerprint(msgs)

        vm.maybeReconcileStaleConversation(tab: makeTab(id: tab, convFingerprint: inSyncFp))

        XCTAssertFalse(vm.loadingConversation.contains(tab), "in-sync tab must not re-fetch")
        XCTAssertTrue(vm.conversationLoaded.contains(tab))
        XCTAssertEqual(vm.conversationMessages(tab).count, 1, "in-sync transcript is preserved")
    }

    /// The per-tab debounce prevents a second heal within the window.
    func testDebouncePreventsImmediateSecondHeal() {
        let vm = SessionViewModel()
        let tab = "tab-debounce"
        vm.handleConversationHistory(tabId: tab, newMessages: [msg(id: "a1", role: .assistant, content: "x")], hasMore: false, cursor: nil)
        let divergedFp = vm.conversationTailFingerprint([msg(id: "a1", role: .assistant, content: "x GREW LONGER")])

        vm.maybeReconcileStaleConversation(tab: makeTab(id: tab, convFingerprint: divergedFp))
        XCTAssertTrue(vm.loadingConversation.contains(tab))

        // History lands (clears loading) so the in-flight guard doesn't mask the debounce.
        vm.handleConversationHistory(tabId: tab, newMessages: [msg(id: "a1", role: .assistant, content: "x")], hasMore: false, cursor: nil)
        XCTAssertFalse(vm.loadingConversation.contains(tab))

        // Immediately-following snapshot, still diverged: debounce suppresses it.
        vm.maybeReconcileStaleConversation(tab: makeTab(id: tab, convFingerprint: divergedFp))
        XCTAssertFalse(vm.loadingConversation.contains(tab), "debounce must suppress the immediate second heal")
    }

    /// While a load is already in flight, no duplicate heal is issued.
    func testNoHealWhileLoadInFlight() {
        let vm = SessionViewModel()
        let tab = "tab-inflight"
        vm.handleConversationHistory(tabId: tab, newMessages: [msg(id: "a1", role: .assistant, content: "x")], hasMore: false, cursor: nil)
        vm.loadingConversation.insert(tab)
        vm.maybeReconcileStaleConversation(tab: makeTab(id: tab, convFingerprint: "different|n=99"))
        XCTAssertEqual(vm.conversationMessages(tab).count, 1, "must not pile a second re-fetch onto an in-flight load")
    }

    /// Empty/nil desktop fingerprint (cold-start tab) → nothing to compare, no heal.
    func testNoHealWithoutDesktopFingerprint() {
        let vm = SessionViewModel()
        let tab = "tab-cold"
        vm.handleConversationHistory(tabId: tab, newMessages: [msg(id: "a1", role: .assistant, content: "x")], hasMore: false, cursor: nil)
        vm.maybeReconcileStaleConversation(tab: makeTab(id: tab, convFingerprint: ""))
        XCTAssertFalse(vm.loadingConversation.contains(tab))
        vm.maybeReconcileStaleConversation(tab: makeTab(id: tab, convFingerprint: nil))
        XCTAssertFalse(vm.loadingConversation.contains(tab))
    }

    // MARK: - Streaming gate (new)

    /// While a tab is actively streaming (.running), reconcile must be suppressed
    /// even when the fingerprint diverges. Firing loadConversation mid-stream
    /// would wipe live messages and cause a 1-2s blank flicker on every snapshot.
    ///
    /// Regression anchor: reverting the `tab.status != .running` guard in
    /// maybeReconcileStaleConversation turns this test red.
    func testNoHealWhileTabIsRunning() {
        let vm = SessionViewModel()
        let tab = "tab-running"
        // Load a message so there is a local fingerprint to compare.
        vm.handleConversationHistory(tabId: tab, newMessages: [msg(id: "a1", role: .assistant, content: "partial")], hasMore: false, cursor: nil)
        XCTAssertTrue(vm.conversationLoaded.contains(tab))

        // Desktop fingerprint is ahead (final assistant message arrived on desktop
        // but iOS has only the partial delta).
        let desktopFp = vm.conversationTailFingerprint([msg(id: "a1", role: .assistant, content: "partial complete")])

        // Tab is still .running — reconcile must be suppressed.
        vm.maybeReconcileStaleConversation(tab: makeTab(id: tab, convFingerprint: desktopFp, status: .running))

        XCTAssertFalse(vm.loadingConversation.contains(tab), "reconcile must not fire while tab.status == .running")
        XCTAssertTrue(vm.conversationLoaded.contains(tab), "loaded mark must be preserved")
        XCTAssertEqual(vm.conversationMessages(tab).count, 1, "live messages must not be wiped")

        // .connecting is the other streaming state — also suppressed.
        vm.maybeReconcileStaleConversation(tab: makeTab(id: tab, convFingerprint: desktopFp, status: .connecting))
        XCTAssertFalse(vm.loadingConversation.contains(tab), "reconcile must not fire while tab.status == .connecting")
    }

    /// One-shot post-run heal: handleTabStatus(.idle) after .running must fire
    /// exactly one reconcile when the fingerprint diverges, and a second .idle
    /// call (already idle) must not fire a second load.
    func testPostRunHealFiresOnceOnRunningToIdle() {
        let vm = SessionViewModel()
        let tabId = "tab-post-run"

        // Set up a loaded tab with a stale local message.
        vm.handleConversationHistory(tabId: tabId, newMessages: [msg(id: "a1", role: .assistant, content: "partial")], hasMore: false, cursor: nil)
        XCTAssertTrue(vm.conversationLoaded.contains(tabId))

        // Register the tab in vm.tabs as .running so handleTabStatus can find it.
        var runningTab = RemoteTabState(
            id: tabId,
            title: tabId,
            customTitle: nil,
            status: .running,
            workingDirectory: "/tmp",
            permissionMode: .auto,
            thinkingEffort: nil,
            permissionQueue: [],
            hasEngineExtension: false
        )
        // Desktop fingerprint diverges (final message differs from local partial).
        runningTab.convFingerprint = vm.conversationTailFingerprint([msg(id: "a1", role: .assistant, content: "partial complete")])
        vm.tabs.append(runningTab)

        // Transition .running → .idle: one-shot heal should fire.
        vm.handleTabStatus(tabId: tabId, status: .idle)

        XCTAssertTrue(vm.loadingConversation.contains(tabId), "post-run heal must re-fetch history when fingerprint diverges")

        // Simulate history landing so we can test the second call.
        vm.handleConversationHistory(tabId: tabId, newMessages: [msg(id: "a1", role: .assistant, content: "partial")], hasMore: false, cursor: nil)
        XCTAssertFalse(vm.loadingConversation.contains(tabId))

        // Second .idle call (idle → idle — previousStatus captured before update
        // is now .idle, not .running): no second heal within the debounce window.
        vm.handleTabStatus(tabId: tabId, status: .idle)
        XCTAssertFalse(vm.loadingConversation.contains(tabId), "second idle transition must not fire a second load")
    }
}
