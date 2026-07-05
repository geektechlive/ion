import XCTest
@testable import IonRemote

/// Tests for DispatchBodyState — the pure decision logic behind the agent
/// dispatch popup body branch and the "Working…" spinner gating.
///
/// Regression for the "perpetual Working… with a ticking timer" bug: a selected
/// (pager) dispatch that is NOT running and has no transcript must render the
/// honest static "no transcript" state, never the live spinner, and the duration
/// must not tick against the live agent's clock.
final class DispatchBodyStateTests: XCTestCase {

    // MARK: - isRunning

    func test_isRunning_selectedDispatch_gatesOnDispatchStatus_notAgent() {
        // Selected dispatch not running, but the live agent IS running (a sibling
        // dispatch is live). The spinner must NOT show — it is gated on the
        // selected dispatch's own status.
        let result = DispatchBodyState.isRunning(
            hasActiveDispatch: true,
            dispatchStatus: "",
            agentStatus: "running"
        )
        XCTAssertFalse(result, "selected dispatch must not borrow the live agent's running state")
    }

    func test_isRunning_selectedDispatchRunning_showsSpinner() {
        let result = DispatchBodyState.isRunning(
            hasActiveDispatch: true,
            dispatchStatus: "running",
            agentStatus: "idle"
        )
        XCTAssertTrue(result)
    }

    func test_isRunning_singleDispatch_fallsBackToAgentStatus() {
        let result = DispatchBodyState.isRunning(
            hasActiveDispatch: false,
            dispatchStatus: nil,
            agentStatus: "running"
        )
        XCTAssertTrue(result, "single-dispatch path falls back to the agent's status")
    }

    // MARK: - branch

    func test_branch_selectedDispatch_emptyStatus_noTranscript_isNoTranscriptNotWorking() {
        // The exact bug scenario: dispatch #1 selected, empty conversationId (no
        // messages), empty status, agent running. Must resolve to .noTranscript,
        // never .working.
        let isRunning = DispatchBodyState.isRunning(
            hasActiveDispatch: true,
            dispatchStatus: "",
            agentStatus: "running"
        )
        let branch = DispatchBodyState.branch(
            hasMessages: false,
            isLoading: false,
            hasActiveDispatch: true,
            hasFullOutput: true,           // agent has global fullOutput; must NOT leak it
            isRunning: isRunning
        )
        XCTAssertEqual(branch, .noTranscript)
    }

    func test_branch_selectedDispatch_doesNotLeakAgentFullOutput() {
        // A selected dispatch with no transcript must not fall back to the agent's
        // global fullOutput (that would leak a sibling dispatch's content).
        let branch = DispatchBodyState.branch(
            hasMessages: false,
            isLoading: false,
            hasActiveDispatch: true,
            hasFullOutput: true,
            isRunning: false
        )
        XCTAssertEqual(branch, .noTranscript)
        XCTAssertNotEqual(branch, .fullOutput)
    }

    func test_branch_messagesWin() {
        let branch = DispatchBodyState.branch(
            hasMessages: true,
            isLoading: true,
            hasActiveDispatch: true,
            hasFullOutput: true,
            isRunning: true
        )
        XCTAssertEqual(branch, .messages)
    }

    func test_branch_loadingBeforeWorking() {
        let branch = DispatchBodyState.branch(
            hasMessages: false,
            isLoading: true,
            hasActiveDispatch: true,
            hasFullOutput: false,
            isRunning: true
        )
        XCTAssertEqual(branch, .loading)
    }

    func test_branch_selectedRunningDispatch_isWorking() {
        let branch = DispatchBodyState.branch(
            hasMessages: false,
            isLoading: false,
            hasActiveDispatch: true,
            hasFullOutput: false,
            isRunning: true
        )
        XCTAssertEqual(branch, .working)
    }

    func test_branch_singleDispatch_fullOutputFallback() {
        let branch = DispatchBodyState.branch(
            hasMessages: false,
            isLoading: false,
            hasActiveDispatch: false,
            hasFullOutput: true,
            isRunning: false
        )
        XCTAssertEqual(branch, .fullOutput)
    }

    func test_branch_singleDispatch_running_isWorking() {
        let branch = DispatchBodyState.branch(
            hasMessages: false,
            isLoading: false,
            hasActiveDispatch: false,
            hasFullOutput: false,
            isRunning: true
        )
        XCTAssertEqual(branch, .working)
    }

    func test_branch_singleDispatch_nothing_isEmpty() {
        let branch = DispatchBodyState.branch(
            hasMessages: false,
            isLoading: false,
            hasActiveDispatch: false,
            hasFullOutput: false,
            isRunning: false
        )
        XCTAssertEqual(branch, .empty)
    }

    // MARK: - elapsedSeconds (no ticking timer for a non-running selected dispatch)

    func test_elapsedSeconds_selectedDispatch_emptyStatus_noStartTime_returnsNil() {
        // The duration helper, fed the selected dispatch's own (empty) values,
        // must return nil — no ticking timer. This is what the view does after
        // the fix instead of borrowing the agent's startTime.
        let result = AgentDuration.elapsedSeconds(
            status: "",
            startTime: nil,
            elapsed: nil,
            now: Date()
        )
        XCTAssertNil(result, "a non-running dispatch with no startTime must show no ticking timer")
    }
}
