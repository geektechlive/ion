import XCTest
@testable import IonRemote

/// Phase 3 of the #256 iOS unification: the single history-load-state machine,
/// and the regression for the compound-vs-bare snapshot key bug.
///
/// Before this phase the snapshot pre-load guard composed a *compound*
/// `"\(tabId):\(activeInstanceId)"` key while the engineConversationHistory
/// handler inserted a *bare* `tabId` into a separate `engineConversationLoaded`
/// set. When `activeEngineInstance[tabId]` was non-nil the two keys never
/// matched, so the snapshot re-requested history on every ~5s tick (flicker).
///
/// WI-004 / #259: `engineConversationHistory` is retired. History for every
/// tab — plain or extension-hosted — arrives via `desktop_conversation_history`
/// (TypeKey.conversationHistory → NormalizedEvent.conversationHistory →
/// handleConversationHistory). These tests are re-pointed at that live path,
/// preserving the behavioral contracts that were pinned before:
///   1. Unified history call marks the bare tabId loaded.
///   2. The mark holds even when activeEngineInstance[tabId] is set.
///   3. Plain and extension-hosted tabs share the same set.
///   4. History reload is idempotent for both tab types.
@MainActor
final class ConversationLoadStateTests: XCTestCase {

    private func makeMessage(id: String) -> Message {
        Message(id: id, role: .assistant, content: "c", timestamp: 1)
    }

    func testEngineHistoryMarksBareTabIdLoaded() {
        // Re-pointed from engineConversationHistory to handleConversationHistory
        // (the live desktop_conversation_history handler). Same contract:
        // bare tabId is inserted into conversationLoaded.
        let vm = SessionViewModel()
        vm.handleConversationHistory(tabId: "tab-e", newMessages: [makeMessage(id: "m1")], hasMore: false, cursor: nil)
        XCTAssertTrue(vm.conversationLoaded.contains("tab-e"))
        XCTAssertEqual(vm.conversationMessages("tab-e").count, 1)
    }

    func testLoadedMarkHoldsWhenActiveInstanceIdIsSet() {
        // The compound-key bug surfaced precisely when activeEngineInstance was
        // non-nil. Set it, load history (handler inserts bare tabId), then
        // assert the unified set contains the bare tabId — i.e. the snapshot
        // guard `conversationLoaded.contains(tab.id)` would skip re-request.
        let vm = SessionViewModel()
        vm.activeEngineInstance["tab-e"] = "main"
        vm.handleConversationHistory(tabId: "tab-e", newMessages: [makeMessage(id: "m1")], hasMore: false, cursor: nil)

        // This is the exact predicate SessionViewModel+Snapshot evaluates.
        XCTAssertTrue(vm.conversationLoaded.contains("tab-e"),
            "Bare tabId must be marked loaded regardless of activeEngineInstance — the snapshot must not re-request")
    }

    func testPlainHistoryMarksSameSet() {
        let vm = SessionViewModel()
        vm.handleConversationHistory(tabId: "tab-p", newMessages: [makeMessage(id: "m1")], hasMore: false, cursor: nil)
        XCTAssertTrue(vm.conversationLoaded.contains("tab-p"))
    }

    func testHistoryReloadIsIdempotentForBothTabTypes() {
        let vm = SessionViewModel()
        // Extension-hosted tab: two identical loads — replace, not duplicate
        vm.handleConversationHistory(tabId: "tab-e", newMessages: [makeMessage(id: "a"), makeMessage(id: "b")], hasMore: false, cursor: nil)
        vm.handleConversationHistory(tabId: "tab-e", newMessages: [makeMessage(id: "a"), makeMessage(id: "b")], hasMore: false, cursor: nil)
        XCTAssertEqual(vm.conversationMessages("tab-e").count, 2, "Reload replaces, does not duplicate")
        // Plain tab
        vm.handleConversationHistory(tabId: "tab-p", newMessages: [makeMessage(id: "x")], hasMore: false, cursor: nil)
        vm.handleConversationHistory(tabId: "tab-p", newMessages: [makeMessage(id: "x")], hasMore: false, cursor: nil)
        XCTAssertEqual(vm.conversationMessages("tab-p").count, 1)
    }

    // MARK: - WI-004 retirement guard

    /// Asserts that no production source file still calls .engineConversationHistory
    /// or references the TypeKey. If this test turns red, a dangling reference
    /// was re-introduced after WI-004 retired the event.
    func testEngineConversationHistoryFullyRetired() throws {
        let iosRoot = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent() // IonRemoteTests
            .deletingLastPathComponent() // ios
        let fm = FileManager.default
        guard let enumerator = fm.enumerator(
            at: iosRoot,
            includingPropertiesForKeys: [.isRegularFileKey],
            options: [.skipsHiddenFiles, .skipsPackageDescendants]
        ) else {
            XCTFail("Could not enumerate ios/ directory")
            return
        }
        var violations: [String] = []
        for case let fileURL as URL in enumerator {
            guard fileURL.pathExtension == "swift" else { continue }
            // Exclude this test file itself (it contains the string in comments)
            // and the tombstone comments in NormalizedEvent.swift.
            let filename = fileURL.lastPathComponent
            if filename == "ConversationLoadStateTests.swift" { continue }
            let src: String
            do {
                src = try String(contentsOf: fileURL, encoding: .utf8)
            } catch { continue }
            // Only flag lines that are executable references (not pure comments).
            // A simple heuristic: the pattern appears on a non-comment line.
            let lines = src.components(separatedBy: "\n")
            for (i, line) in lines.enumerated() {
                let trimmed = line.trimmingCharacters(in: .whitespaces)
                // Skip comment-only lines (tombstone docs are allowed)
                if trimmed.hasPrefix("//") || trimmed.hasPrefix("*") || trimmed.hasPrefix("///") { continue }
                if trimmed.contains("engineConversationHistory") {
                    violations.append("\(fileURL.lastPathComponent):\(i + 1): \(trimmed.prefix(100))")
                }
            }
        }
        XCTAssertTrue(violations.isEmpty,
            "engineConversationHistory found in production code after WI-004 retirement:\n" +
            violations.joined(separator: "\n"))
    }
}
