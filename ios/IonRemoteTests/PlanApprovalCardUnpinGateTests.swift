import XCTest
@testable import IonRemote

/// Pins `PlanApprovalCardView.resolveShowUnpinOption(groupPinned:hasEngineExtension:)`
/// — the pure gate that decides whether the "Implement and Unpin" split-row
/// button renders on the Plan Ready card. The gate mirrors the desktop exactly:
/// the unpin option is revealed whenever the tab is pinned to its group
/// (`groupPinned == true`), regardless of whether the conversation is
/// extension-hosted.
///
/// Regression guard: the prior iOS gate carried an orphaned
/// `&& hasEngineExtension != true` predicate (a pre-unification artifact left
/// behind by PR #256 / commit 2ade1824) that hid the unpin button for
/// extension-hosted, group-pinned tabs while desktop still showed it. The
/// `testExtensionHostedPinned_showsUnpinOption` case below fails against that
/// old gate (hasEngineExtension=true forced the whole expression to false) and
/// passes only against the corrected `groupPinned`-only gate.
final class PlanApprovalCardUnpinGateTests: XCTestCase {

    // The corrected-behavior pin: an extension-hosted, group-pinned tab MUST
    // present the unpin option. This case would return false under the old
    // `groupPinned == true && hasEngineExtension != true` gate.
    func testExtensionHostedPinned_showsUnpinOption() {
        XCTAssertTrue(
            PlanApprovalCardView.resolveShowUnpinOption(
                groupPinned: true,
                hasEngineExtension: true
            ),
            "Extension-hosted, group-pinned tab must show the unpin option (desktop parity)."
        )
    }

    func testPlainPinned_showsUnpinOption() {
        XCTAssertTrue(
            PlanApprovalCardView.resolveShowUnpinOption(
                groupPinned: true,
                hasEngineExtension: false
            )
        )
    }

    func testExtensionHostedNotPinned_hidesUnpinOption() {
        XCTAssertFalse(
            PlanApprovalCardView.resolveShowUnpinOption(
                groupPinned: false,
                hasEngineExtension: true
            )
        )
    }

    func testPlainNotPinned_hidesUnpinOption() {
        XCTAssertFalse(
            PlanApprovalCardView.resolveShowUnpinOption(
                groupPinned: false,
                hasEngineExtension: false
            )
        )
    }

    func testNilGroupPinned_hidesUnpinOption() {
        // An absent groupPinned flag must not reveal the option.
        XCTAssertFalse(
            PlanApprovalCardView.resolveShowUnpinOption(
                groupPinned: nil,
                hasEngineExtension: true
            )
        )
        XCTAssertFalse(
            PlanApprovalCardView.resolveShowUnpinOption(
                groupPinned: nil,
                hasEngineExtension: nil
            )
        )
    }
}
