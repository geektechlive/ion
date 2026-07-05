import XCTest
@testable import IonRemote

/// WI-002 / #259 — thinkingEffort storage-fork collapse.
///
/// Before this change, `setThinkingEffort` branched on `hasEngineExtension`:
///   - extension tab  → wrote `tabs[idx].conversationInstances[iIdx].thinkingEffort`
///   - plain tab      → wrote `tabs[idx].thinkingEffort`
///
/// After the collapse, every tab — plain or extension-hosted — writes through
/// the single ConversationInstanceInfo that post-#256 every tab owns. There is
/// no tab-type branch. The instance is the authoritative home.
///
/// These tests pin two contracts:
///   1. Guard: the fork is gone from source — `setThinkingEffort` no longer
///      contains a `hasEngineExtension` branch.
///   2. Parity: a plain tab and an extension-hosted tab both land the write
///      on `conversationInstances[tabId]?.first?.thinkingEffort`, and both
///      clear to nil when effort == "off".
final class ThinkingEffortStorageForkTests: XCTestCase {

    // MARK: - Source Guard

    /// Asserts the fork is gone at the source level. If `setThinkingEffort`
    /// ever re-introduces a `hasEngineExtension` branch, this test fails.
    func testSetThinkingEffortHasNoTabTypeBranch() throws {
        let url = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("IonRemote/ViewModels/SessionViewModel+Commands.swift")
        let src = try String(contentsOf: url, encoding: .utf8)

        // Locate the setThinkingEffort function body (from its declaration to
        // the closing brace of the next top-level function or end of file).
        guard let startRange = src.range(of: "func setThinkingEffort(") else {
            XCTFail("setThinkingEffort not found in SessionViewModel+Commands.swift")
            return
        }
        // Grab the 30 lines after the declaration to capture the full body
        // without relying on precise brace matching.
        let afterDecl = String(src[startRange.lowerBound...])
        let lines = afterDecl.components(separatedBy: "\n").prefix(30).joined(separator: "\n")

        XCTAssertFalse(lines.contains("hasEngineExtension"),
            "setThinkingEffort must not branch on hasEngineExtension (WI-002 / #259). " +
            "Every tab writes thinkingEffort to its ConversationInstanceInfo.")
        XCTAssertFalse(lines.contains("tabs[idx].thinkingEffort"),
            "setThinkingEffort must not write to the top-level tab field. " +
            "The instance is the single authoritative home post-#256.")
        XCTAssertFalse(lines.contains("conversationInstances["),
            "setThinkingEffort must not index into tabs[idx].conversationInstances directly. " +
            "It should use mutateEngineInstance so all callers go through the unified accessor.")
    }

    // MARK: - Parity: plain tab

    @MainActor
    func testPlainTabSetThinkingEffortLandsOnInstance() {
        let vm = SessionViewModel()
        let tabId = "plain-tab-1"
        vm.tabs = [makePlainTab(id: tabId)]

        vm.setThinkingEffort(tabId: tabId, effort: "high")

        let inst = vm.engineInstance(tabId: tabId, instanceId: nil)
        XCTAssertEqual(inst?.thinkingEffort, "high",
            "Plain tab: thinkingEffort must land on the ConversationInstanceInfo")
    }

    @MainActor
    func testPlainTabSetThinkingEffortOffClearsToNil() {
        let vm = SessionViewModel()
        let tabId = "plain-tab-2"
        vm.tabs = [makePlainTab(id: tabId)]

        vm.setThinkingEffort(tabId: tabId, effort: "medium")
        vm.setThinkingEffort(tabId: tabId, effort: "off")

        let inst = vm.engineInstance(tabId: tabId, instanceId: nil)
        XCTAssertNil(inst?.thinkingEffort,
            "Plain tab: effort 'off' must clear thinkingEffort to nil on the instance")
    }

    // MARK: - Parity: extension-hosted tab

    @MainActor
    func testExtensionTabSetThinkingEffortLandsOnInstance() {
        let vm = SessionViewModel()
        let tabId = "ext-tab-1"
        vm.tabs = [makeExtensionTab(id: tabId)]

        vm.setThinkingEffort(tabId: tabId, effort: "low")

        let inst = vm.engineInstance(tabId: tabId, instanceId: nil)
        XCTAssertEqual(inst?.thinkingEffort, "low",
            "Extension-hosted tab: thinkingEffort must land on the ConversationInstanceInfo")
    }

    @MainActor
    func testExtensionTabSetThinkingEffortOffClearsToNil() {
        let vm = SessionViewModel()
        let tabId = "ext-tab-2"
        vm.tabs = [makeExtensionTab(id: tabId)]

        vm.setThinkingEffort(tabId: tabId, effort: "high")
        vm.setThinkingEffort(tabId: tabId, effort: "off")

        let inst = vm.engineInstance(tabId: tabId, instanceId: nil)
        XCTAssertNil(inst?.thinkingEffort,
            "Extension-hosted tab: effort 'off' must clear thinkingEffort to nil on the instance")
    }

    // MARK: - Parity: identical behavior

    /// Plain and extension-hosted tabs produce the same instance-level outcome
    /// for the same effort value. The only difference between the two tab kinds
    /// is `hasEngineExtension`; the storage path is identical.
    @MainActor
    func testPlainAndExtensionTabsBehaveIdentically() {
        let vm = SessionViewModel()
        let plainId = "plain-parity"
        let extId = "ext-parity"
        vm.tabs = [makePlainTab(id: plainId), makeExtensionTab(id: extId)]

        vm.setThinkingEffort(tabId: plainId, effort: "medium")
        vm.setThinkingEffort(tabId: extId, effort: "medium")

        let plainInst = vm.engineInstance(tabId: plainId, instanceId: nil)
        let extInst = vm.engineInstance(tabId: extId, instanceId: nil)
        XCTAssertEqual(plainInst?.thinkingEffort, extInst?.thinkingEffort,
            "Plain and extension-hosted tabs must produce the same thinkingEffort " +
            "result — the storage path is now identical for both.")
    }

    // MARK: - Helpers

    private func makePlainTab(id: String) -> RemoteTabState {
        var t = RemoteTabState(
            id: id, title: "Plain", status: .idle,
            workingDirectory: "/tmp", permissionMode: .auto,
            permissionQueue: []
        )
        t.hasEngineExtension = false
        return t
    }

    private func makeExtensionTab(id: String) -> RemoteTabState {
        var t = RemoteTabState(
            id: id, title: "Engine", status: .idle,
            workingDirectory: "/tmp", permissionMode: .auto,
            permissionQueue: []
        )
        t.hasEngineExtension = true
        // Provide a seeded instance so the pre-existing activeEngineInstance
        // lookup can resolve the active id. The instance path now uses
        // mutateEngineInstance which calls ensureMainInstance, so this seed is
        // not strictly required — but it mirrors the real snapshot path and
        // avoids relying on ensureMainInstance side-effects in tests.
        t.conversationInstances = [ConversationInstanceInfo(
            id: ConversationInstanceInfo.mainInstanceId, label: "main"
        )]
        return t
    }
}
