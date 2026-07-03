import XCTest
@testable import IonRemote

/// Pins the Plan Ready (ExitPlanMode) card render gate (desktop parity).
///
/// An orchestrator can exit plan mode while a background dispatch it spawned is
/// still running. The Plan Ready card must be deferred during that window — the
/// orchestrator will resume and revise the plan when the dispatch reports back.
/// AskUserQuestion and generic permission cards are never suppressed. The pure
/// `PlanCardGate.shouldShowCard(toolName:runningAgentCount:)` helper makes that
/// logic testable without instantiating the SwiftUI view, mirroring the desktop
/// `shouldSuppressPlanCardForDispatch` helper.
final class PlanCardGateTests: XCTestCase {

    func testExitPlanModeHiddenWhileDispatchRunning() {
        // The reported bug: orchestrator exits plan mode while dev-lead is still
        // dispatching. The card must be deferred.
        XCTAssertFalse(PlanCardGate.shouldShowCard(toolName: "ExitPlanMode", runningAgentCount: 1))
        XCTAssertFalse(PlanCardGate.shouldShowCard(toolName: "ExitPlanMode", runningAgentCount: 3))
    }

    func testExitPlanModeShownWhenNoDispatchRunning() {
        // Normal Plan Ready case — the card renders.
        XCTAssertTrue(PlanCardGate.shouldShowCard(toolName: "ExitPlanMode", runningAgentCount: 0))
    }

    func testAskUserQuestionAlwaysShown() {
        // A direct question to the user is not invalidated by a background dispatch.
        XCTAssertTrue(PlanCardGate.shouldShowCard(toolName: "AskUserQuestion", runningAgentCount: 2))
        XCTAssertTrue(PlanCardGate.shouldShowCard(toolName: "AskUserQuestion", runningAgentCount: 0))
    }

    func testGenericPermissionAlwaysShown() {
        // A live blocking tool request must still render.
        XCTAssertTrue(PlanCardGate.shouldShowCard(toolName: "Bash", runningAgentCount: 2))
    }
}
