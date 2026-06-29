import SwiftUI

/// Full-screen view for agent details. Presented via `.fullScreenCover`
/// when the `agentPanelFullScreenPopup` setting is enabled.
/// Reads live agent state and conversation data from the view model
/// so streaming updates appear in real time while the popup is open.
struct AgentDetailFullScreenView: View {
    let agentName: String
    let compoundKey: String
    @Environment(SessionViewModel.self) private var viewModel
    @Environment(\.dismiss) private var dismiss
    @Environment(\.appTheme) private var theme

    /// Live agent from the view model's agent state array.
    private var agent: AgentStateUpdate? {
        // Post-#256: compoundKey is bare tabId. Tolerate legacy "tabId:instanceId"
        // form via parseEngineSessionKey so old in-flight state doesn't crash.
        let tabId = SessionViewModel.parseEngineSessionKey(compoundKey)
        return (viewModel.engineInstance(tabId: tabId, instanceId: nil)?.agentStates ?? [])
            .first { $0.name == agentName }
    }

    /// Stable identity for tracking dispatch changes — the set of
    /// conversationIds across all dispatches plus the agent status.
    /// When this changes, we know new work has arrived and should
    /// re-fetch conversation data.
    private var dispatchSignature: String {
        guard let agent else { return "" }
        let ids = agent.dispatches.map(\.conversationId).joined(separator: ",")
        return "\(ids)|\(agent.status)|\(agent.dispatches.count)"
    }

    /// The most recent dispatch's conversationId, or empty when none is known
    /// yet. The poller refreshes this conversation while it is still running.
    private var latestDispatchConvId: String {
        agent?.dispatches.last?.conversationId ?? ""
    }

    /// Whether the latest dispatch (or the agent itself, when the structured
    /// dispatch entry has no status yet) is still running. The live poller only
    /// fires while running; once terminal, the .onChange(dispatchSignature)
    /// refresh delivers the final transcript and polling stops.
    private var latestDispatchRunning: Bool {
        guard let agent else { return false }
        if let last = agent.dispatches.last, !last.status.isEmpty {
            return last.status == "running"
        }
        return agent.status == "running"
    }

    /// Slow reconcile cadence. The live transcript is carried in real time by
    /// the engine_dispatch_activity push path (folded in the view model); this
    /// timer is the CORRECTNESS BACKSTOP that re-fetches the file-backed
    /// snapshot so any gap from a dropped delta or a LAN↔relay transport switch
    /// self-heals. Slow on purpose — push does the streaming. Mirrors the
    /// desktop AgentPanel reconcile interval.
    private let reconcileInterval: TimeInterval = 12

    var body: some View {
        NavigationStack {
            Group {
                if let agent {
                    // pinHeader: true — AgentExpandedContent manages its own
                    // internal ScrollView for the transcript and keeps the header
                    // (model tag + duration + dispatch picker) pinned above it.
                    // The outer NavigationStack/Group provides the chrome; no
                    // additional ScrollView wrapper is needed.
                    AgentExpandedContent(
                        agent: agent,
                        messages: viewModel.agentConversationMessages[agent.name],
                        convMessageCache: viewModel.agentConversationMessages,
                        isLoadingMessages: viewModel.agentConversationLoading.contains(agent.name)
                            || agent.dispatches.contains { viewModel.agentConversationLoading.contains($0.conversationId) },
                        onLoadDispatch: { convId in
                            viewModel.loadAgentDispatchConversation(agent: agent, conversationId: convId)
                        },
                        onPreloadDispatches: { excludingConvId in
                            viewModel.preloadAgentDispatches(agent: agent, excluding: excludingConvId)
                        },
                        pinHeader: true
                    )
                } else {
                    ContentUnavailableView(
                        "Agent Not Found",
                        systemImage: "person.slash",
                        description: Text("This agent is no longer active.")
                    )
                }
            }
            .background(theme.background)
            .navigationTitle(agent?.displayName ?? "Agent")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Done") {
                        dismiss()
                    }
                    .fontWeight(.semibold)
                }
            }
            // Re-fetch conversation data when the agent's dispatches change
            // (new dispatch added, agent completed, etc.) so a new dispatch's
            // first snapshot loads promptly.
            .onChange(of: dispatchSignature) {
                guard let agent else { return }
                refreshConversation(for: agent)
            }
            // Slow reconcile backstop: while the latest dispatch is still
            // running, re-fetch the file-backed snapshot on the reconcile
            // cadence so any gap the push stream missed self-heals. The push
            // path (engine_dispatch_activity) carries the real-time transcript;
            // this only corrects drift.
            .onReceive(Timer.publish(every: reconcileInterval, on: .main, in: .common).autoconnect()) { _ in
                guard let agent, latestDispatchRunning, !latestDispatchConvId.isEmpty else { return }
                viewModel.refreshAgentDispatchConversation(agent: agent, conversationId: latestDispatchConvId)
            }
            // Final reconcile when the running dispatch transitions to terminal,
            // so the popup converges on the complete persisted transcript
            // regardless of whether the last few push deltas landed.
            .onChange(of: latestDispatchRunning) { wasRunning, isRunning in
                guard wasRunning, !isRunning, let agent, !latestDispatchConvId.isEmpty else { return }
                viewModel.refreshAgentDispatchConversation(agent: agent, conversationId: latestDispatchConvId)
            }
        }
    }

    /// Invalidates cached conversation data for the agent's dispatches
    /// and re-fetches so the popup shows fresh content.
    private func refreshConversation(for agent: AgentStateUpdate) {
        if let lastDispatch = agent.dispatches.last,
           !lastDispatch.conversationId.isEmpty {
            viewModel.refreshAgentDispatchConversation(
                agent: agent,
                conversationId: lastDispatch.conversationId
            )
        } else if !agent.conversationIds.isEmpty {
            viewModel.refreshAgentConversation(agent: agent)
        }
    }
}
