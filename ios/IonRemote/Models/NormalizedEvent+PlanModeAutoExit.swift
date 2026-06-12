import Foundation

// MARK: - Plan-mode auto-exit event (issue #187, ADR-007)
//
// Extracted from NormalizedEvent+Engine.swift to keep the umbrella
// engine-event file under the per-file size cap. This file owns the
// decoder + encoder for engine_plan_mode_auto_exit, the sibling event
// to engine_plan_proposal that fires when the engine deterministically
// synthesizes an ExitPlanMode call at end-of-turn (see ADR-007).

extension RemoteEvent {

    /// Decode the engine_plan_mode_auto_exit event from its wire shape.
    ///
    /// iOS does not act on this event today — the desktop is the
    /// authoritative consumer that renders the approval card — but
    /// decoding cleanly here keeps the wire protocol uniform across
    /// consumers and lets a future iOS surface (e.g. a "Plan surfaced
    /// automatically" hint above the approval card) read the
    /// telemetry-friendly payload (stopReason, reason, sessionId,
    /// runId) without contract changes.
    static func decodeEnginePlanModeAutoExit(
        container: KeyedDecodingContainer<CodingKeys>
    ) throws -> RemoteEvent {
        let tabId = try container.decode(String.self, forKey: .tabId)
        let instanceId = try container.decodeIfPresent(String.self, forKey: .instanceId)
        let stopReason = try container.decodeIfPresent(String.self, forKey: .planModeAutoExitStopReason) ?? ""
        let planFilePath = try container.decodeIfPresent(String.self, forKey: .planFilePath)
        let planSlug = try container.decodeIfPresent(String.self, forKey: .planSlug)
        let reason = try container.decodeIfPresent(String.self, forKey: .planModeAutoExitReason)
        let sessionId = try container.decodeIfPresent(String.self, forKey: .planModeAutoExitSessionId)
        let runId = try container.decodeIfPresent(String.self, forKey: .planModeAutoExitRunId)
        return .enginePlanModeAutoExit(
            tabId: tabId,
            instanceId: instanceId,
            stopReason: stopReason,
            planFilePath: planFilePath,
            planSlug: planSlug,
            reason: reason,
            sessionId: sessionId,
            runId: runId
        )
    }

    /// Encoder mirror of the decoder above. iOS never originates this
    /// event in practice (the engine emits it, iOS observes), but the
    /// encoder must round-trip cleanly so that re-encoded events in
    /// tests and diagnostic dumps don't lose fields.
    ///
    /// Instance method (not static) so the call site inside the
    /// `encodeEngine(into:)` switch in NormalizedEvent+Engine.swift can
    /// invoke it without rebuilding the value tuple — the variant's
    /// associated values are already destructured in the case binding.
    func encodeEnginePlanModeAutoExit(
        container: inout KeyedEncodingContainer<CodingKeys>,
        tabId: String,
        instanceId: String?,
        stopReason: String,
        planFilePath: String?,
        planSlug: String?,
        reason: String?,
        sessionId: String?,
        runId: String?
    ) throws {
        try container.encode(TypeKey.enginePlanModeAutoExit, forKey: .type)
        try container.encode(tabId, forKey: .tabId)
        try container.encodeIfPresent(instanceId, forKey: .instanceId)
        try container.encode(stopReason, forKey: .planModeAutoExitStopReason)
        try container.encodeIfPresent(planFilePath, forKey: .planFilePath)
        try container.encodeIfPresent(planSlug, forKey: .planSlug)
        try container.encodeIfPresent(reason, forKey: .planModeAutoExitReason)
        try container.encodeIfPresent(sessionId, forKey: .planModeAutoExitSessionId)
        try container.encodeIfPresent(runId, forKey: .planModeAutoExitRunId)
    }
}
