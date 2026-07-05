import XCTest
@testable import IonRemote

/// Regression: the plan approval card must render its body even when the
/// snapshot did not enrich a `planContentPreview` and the card carries the full
/// body inline in `planContent`.
///
/// Background: the plan-preview migration switched the card to read ONLY
/// `planContentPreview`. But two card-source paths deliver the body inline as
/// `planContent` and never carry `planContentPreview`:
///   1. The desktop snapshot promotes a permission denial whose toolInput was
///      backfilled from conversation history; its preview enrichment is skipped
///      when the plan file is unreadable on disk.
///   2. `computeRestoredSpecialCard` synthesizes the card from a persisted
///      ExitPlanMode message — `planContentPreview` is a snapshot-only key and
///      is structurally absent.
/// Reading only `planContentPreview` produced a blank card. The fix prefers the
/// preview and falls back to the inline `planContent`.
///
/// These tests pin `PlanApprovalCardView.resolveDisplayContent(toolInput:)` —
/// the pure precedence resolver the view delegates to. Reverting the fallback
/// (returning only the preview) makes case 2 go red.
final class PlanApprovalCardContentTests: XCTestCase {

    // MARK: - Case 1: preview present → preview wins (unchanged behavior)

    func testPreviewPresent_returnsPreview() {
        let toolInput: [String: AnyCodable] = [
            "planContentPreview": AnyCodable("PREVIEW BODY"),
            "planContent": AnyCodable("FULL INLINE BODY"),
            "planFilePath": AnyCodable("/tmp/plan.md"),
        ]
        XCTAssertEqual(
            PlanApprovalCardView.resolveDisplayContent(toolInput: toolInput),
            "PREVIEW BODY"
        )
    }

    // MARK: - Case 2: no preview, inline planContent present → inline body (the regression)

    func testNoPreview_inlinePlanContent_returnsInline() {
        // The exact shape the logs show: inputKeys=["planContent","planFilePath"],
        // NO planContentPreview. Pre-fix this resolved to nil → blank card.
        let toolInput: [String: AnyCodable] = [
            "planContent": AnyCodable("FULL INLINE BODY"),
            "planFilePath": AnyCodable("/Users/josh/.ion/plans/hazel-gathering-cave.md"),
        ]
        XCTAssertEqual(
            PlanApprovalCardView.resolveDisplayContent(toolInput: toolInput),
            "FULL INLINE BODY"
        )
    }

    func testEmptyPreview_inlinePlanContent_returnsInline() {
        // An empty-string preview must not win over a real inline body.
        let toolInput: [String: AnyCodable] = [
            "planContentPreview": AnyCodable(""),
            "planContent": AnyCodable("FULL INLINE BODY"),
        ]
        XCTAssertEqual(
            PlanApprovalCardView.resolveDisplayContent(toolInput: toolInput),
            "FULL INLINE BODY"
        )
    }

    // MARK: - Case 3: neither present → nil (card shows its no-content affordance, no crash)

    func testNeitherPresent_returnsNil() {
        let toolInput: [String: AnyCodable] = [
            "planFilePath": AnyCodable("/tmp/plan.md"),
        ]
        XCTAssertNil(PlanApprovalCardView.resolveDisplayContent(toolInput: toolInput))
    }

    func testNilToolInput_returnsNil() {
        XCTAssertNil(PlanApprovalCardView.resolveDisplayContent(toolInput: nil))
    }

    func testEmptyInlineAndNoPreview_returnsNil() {
        let toolInput: [String: AnyCodable] = [
            "planContent": AnyCodable(""),
        ]
        XCTAssertNil(PlanApprovalCardView.resolveDisplayContent(toolInput: toolInput))
    }
}
