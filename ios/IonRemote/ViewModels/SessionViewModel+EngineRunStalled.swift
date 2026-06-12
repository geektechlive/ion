import Foundation

// MARK: - Engine run-stalled handler
//
// Split out of SessionViewModel+EventHandlers.swift to keep that file
// under the 600-line Swift cap. The engine progress watchdog (see
// Go-side internal/backend/runloop_watchdog.go) emits engine_run_stalled
// as an advisory event immediately before cancelling a wedged run. The
// authoritative completion signal still arrives via the follow-up
// engine_task_complete + engine_dead/idle events, which the main
// EventHandlers file already routes through handleEngineTaskComplete /
// handleEngineDead. iOS observes only here; a dedicated UI surface
// (e.g. a watchdog icon distinct from a generic error toast) is a
// future enhancement and out of scope for the engine-side fix that
// introduced this code path.
//
// Diagnostic logging in DiagnosticLog+Events.swift records the stall
// duration and last activity so a postmortem reader can recover the
// stall window without cross-referencing the engine log.
extension SessionViewModel {
    func handleEngineRunStalled(
        tabId: String,
        instanceId: String?,
        stalledDuration: Double,
        lastActivity: String?
    ) {
        // Intentionally a no-op for now. The cancellation path produces
        // a follow-up engine_task_complete / engine_dead that the
        // existing handlers convert to a tab idle state. If/when iOS
        // gains a dedicated stall indicator, the new state mutation
        // lives here (key by tabId+instanceId so it doesn't bleed
        // across engine instances).
        _ = tabId
        _ = instanceId
        _ = stalledDuration
        _ = lastActivity
    }
}
