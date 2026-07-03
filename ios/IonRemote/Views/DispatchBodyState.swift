import Foundation

/// Pure decision logic for what the agent dispatch body should render and how
/// the duration ticker should behave. Extracted from AgentExpandedContent so the
/// behavior is testable without instantiating a SwiftUI view (the view's
/// computed properties are private and not directly observable).
///
/// The load-bearing contract pinned here:
///   - A selected (pager) dispatch that is NOT running and has no transcript
///     renders the honest static "no transcript" state — never the live spinner.
///   - The "Working…" spinner is gated strictly on the selected dispatch's own
///     status; it never borrows the live agent's running state.
///   - The duration ticker is computed strictly from the selected dispatch's own
///     status/startTime/elapsed; a non-running dispatch with no startTime shows
///     no ticking timer rather than the agent's clock.
enum DispatchBodyState {

    /// The branch the body should render, in priority order.
    enum Branch: Equatable {
        /// Transcript messages are available — render them.
        case messages
        /// A load is in flight — render the loading spinner.
        case loading
        /// No dispatch selected; fall back to the agent's fullOutput.
        case fullOutput
        /// Live work is happening — render the "Working…" spinner.
        case working
        /// A specific dispatch is selected but it is terminal with no transcript.
        case noTranscript
        /// Nothing to show.
        case empty
    }

    /// Whether the "Working…" spinner should show for the current selection.
    /// `hasActiveDispatch` is true when a specific dispatch is selected via the
    /// pager (multi-dispatch). When true, the result is gated strictly on the
    /// dispatch's own status; otherwise it falls back to the agent's status.
    static func isRunning(
        hasActiveDispatch: Bool,
        dispatchStatus: String?,
        agentStatus: String
    ) -> Bool {
        if hasActiveDispatch {
            return dispatchStatus == "running"
        }
        return agentStatus == "running"
    }

    /// Resolve which body branch to render.
    ///
    /// - Parameters:
    ///   - hasMessages: transcript messages exist and are non-empty.
    ///   - isLoading: a conversation load is in flight.
    ///   - hasActiveDispatch: a specific dispatch is selected (pager mode).
    ///   - hasFullOutput: the agent has a non-empty fullOutput fallback.
    ///   - isRunning: the spinner-gating result from `isRunning(...)`.
    static func branch(
        hasMessages: Bool,
        isLoading: Bool,
        hasActiveDispatch: Bool,
        hasFullOutput: Bool,
        isRunning: Bool
    ) -> Branch {
        if hasMessages { return .messages }
        if isLoading { return .loading }
        // fullOutput fallback only applies in the single-dispatch case (no
        // specific dispatch selected) so a selected dispatch never leaks the
        // agent's global output.
        if !hasActiveDispatch, hasFullOutput { return .fullOutput }
        if isRunning { return .working }
        if hasActiveDispatch {
            // Selected dispatch, not running, no transcript — honest static state.
            return .noTranscript
        }
        return .empty
    }
}
