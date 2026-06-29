import Foundation

/// Pure render-gate decision for the Plan Ready (ExitPlanMode) approval card.
///
/// An orchestrator can call ExitPlanMode and have its run go idle while a
/// background dispatch it spawned is still running. The plan at that instant is
/// provisional: when the dispatch reports back the orchestrator resumes,
/// revises the plan, and re-presents it. Showing the Plan Ready card during
/// that window misleads the user into thinking the plan is final when the turn
/// is effectively still active.
///
/// This mirrors the desktop helper (`desktop/src/shared/plan-card-gate.ts`):
/// suppress ONLY the ExitPlanMode card and ONLY while a background dispatch is
/// running. An AskUserQuestion card is a direct question to the user and is
/// never invalidated by a dispatch; a generic permission request is a live
/// blocking request. Both still render. The suppression is a deferral, not a
/// loss — the underlying permission entry is untouched and the card returns
/// once the dispatch ends.
enum PlanCardGate {
    /// Returns `true` when a card with the given `toolName` should be shown,
    /// given how many background dispatch agents are currently running.
    ///
    /// The ExitPlanMode card is hidden while `runningAgentCount > 0`. Every
    /// other card (AskUserQuestion, generic permission) is unaffected.
    static func shouldShowCard(toolName: String, runningAgentCount: Int) -> Bool {
        if toolName == "ExitPlanMode" && runningAgentCount > 0 {
            return false
        }
        return true
    }
}
