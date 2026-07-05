import XCTest
@testable import IonRemote

/// Phase 4 of the #256 iOS unification: the single per-tab draft store.
///
/// Post-#256 plain and engine tabs share ONE bare-tabId-keyed draft store
/// (`draftInputByTab`) persisted under one UserDefaults key. The engine
/// accessors are thin shims over the tab-draft functions. A one-time hydrate
/// migration folds the legacy `engineDraftInputByKey` UserDefaults payload
/// (including compound "tabId:instanceId" keys) into the unified store.
@MainActor
final class UnifiedDraftStoreTests: XCTestCase {

    override func tearDown() {
        // Keep the suite hermetic — these tests poke real UserDefaults keys.
        UserDefaults.standard.removeObject(forKey: SessionViewModel.draftInputByTabKey)
        UserDefaults.standard.removeObject(forKey: SessionViewModel.legacyEngineDraftInputByKeyKey)
        super.tearDown()
    }

    func testEngineAndPlainDraftsPersistThroughOneKey() {
        UserDefaults.standard.removeObject(forKey: SessionViewModel.draftInputByTabKey)
        let vm = SessionViewModel()
        vm.setTabDraft("plain", "plain draft")
        vm.setEngineDraft(tabId: "engine", instanceId: "main", "engine draft")

        // Both land in the single UserDefaults dictionary.
        let stored = UserDefaults.standard.dictionary(forKey: SessionViewModel.draftInputByTabKey) as? [String: String]
        XCTAssertEqual(stored?["plain"], "plain draft")
        XCTAssertEqual(stored?["engine"], "engine draft")
    }

    func testLegacyEngineDraftsMigrateOnHydrate() {
        // Seed a legacy payload (bare + compound keys) then hydrate.
        UserDefaults.standard.removeObject(forKey: SessionViewModel.draftInputByTabKey)
        UserDefaults.standard.set(
            ["tab-a": "bare legacy", "tab-b:main": "compound legacy", "tab-c:inst-xyz": "custom inst legacy"],
            forKey: SessionViewModel.legacyEngineDraftInputByKeyKey
        )

        let vm = SessionViewModel()
        vm.hydrateDrafts()

        XCTAssertEqual(vm.tabDraft("tab-a"), "bare legacy")
        XCTAssertEqual(vm.tabDraft("tab-b"), "compound legacy", "Compound key must fold to bare tabId")
        XCTAssertEqual(vm.tabDraft("tab-c"), "custom inst legacy")

        // Migration runs once: the legacy key is removed afterwards.
        XCTAssertNil(UserDefaults.standard.dictionary(forKey: SessionViewModel.legacyEngineDraftInputByKeyKey),
            "Legacy engine-draft key must be cleared after the one-time migration")
    }

    func testHydrateDoesNotClobberExistingUnifiedDraft() {
        UserDefaults.standard.set(["tab-x": "unified value"], forKey: SessionViewModel.draftInputByTabKey)
        UserDefaults.standard.set(["tab-x:main": "legacy value"], forKey: SessionViewModel.legacyEngineDraftInputByKeyKey)

        let vm = SessionViewModel()
        vm.hydrateDrafts()

        XCTAssertEqual(vm.tabDraft("tab-x"), "unified value",
            "An existing unified draft must win over a legacy compound entry for the same tab")
    }
}
