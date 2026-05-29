import SwiftUI

/// Compact overlay showing actively running staff agents above the input bar.
/// Replaces the ActivityIndicatorView slot in ConversationView when staff agents
/// are dispatched. Shows up to three agents with bouncing-dot indicators for
/// running agents and a static dot for invited agents. Dismisses after a 700ms
/// checkmark window once all agents stop running.
struct StaffActivityView: View {
    let agents: [AgentStateUpdate]

    @State private var dismissTask: Task<Void, Never>?
    @State private var isVisible = true
    @State private var showingDone = false

    private var visibleAgents: [AgentStateUpdate] {
        let running = agents.filter { $0.status == "running" }
        let others = agents.filter { $0.status != "running" }
        return Array((running + others).prefix(3))
    }

    private var overflowCount: Int { max(0, agents.count - 3) }
    private var anyRunning: Bool { agents.contains { $0.status == "running" } }

    private var leadingColor: Color {
        if let first = agents.first(where: { $0.status == "running" }) {
            return agentColor(first)
        }
        return agents.first.map { agentColor($0) } ?? JarvisTheme.accent
    }

    var body: some View {
        Group {
            if isVisible {
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(visibleAgents) { agent in
                        agentRow(agent)
                            .transition(.opacity.combined(with: .scale(0.95, anchor: .leading)))
                    }
                    if overflowCount > 0 {
                        Text("+\(overflowCount) more")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                            .padding(.leading, 32)
                    }
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 8)
                .background(
                    RoundedRectangle(cornerRadius: 12)
                        .fill(JarvisTheme.surfaceElevated)
                        .overlay(
                            RoundedRectangle(cornerRadius: 12)
                                .stroke(Color.white.opacity(0.07), lineWidth: 1)
                        )
                        .overlay(
                            RoundedRectangle(cornerRadius: 12)
                                .stroke(leadingColor.opacity(anyRunning ? 0.18 : 0), lineWidth: 1)
                        )
                )
                .padding(.horizontal, 16)
                .animation(IonTheme.snappySpring, value: visibleAgents.map(\.name))
                .transition(.move(edge: .bottom).combined(with: .opacity))
            }
        }
        .onChange(of: anyRunning) { _, running in
            if !running {
                showingDone = true
                dismissTask?.cancel()
                dismissTask = Task {
                    try? await Task.sleep(for: .milliseconds(700))
                    guard !Task.isCancelled else { return }
                    await MainActor.run {
                        withAnimation(IonTheme.snappySpring) { isVisible = false }
                    }
                }
            } else {
                dismissTask?.cancel()
                dismissTask = nil
                showingDone = false
                isVisible = true
            }
        }
    }

    @ViewBuilder
    private func agentRow(_ agent: AgentStateUpdate) -> some View {
        let color = agentColor(agent)
        HStack(spacing: 8) {
            // Dot cluster — fixed 24pt width
            Group {
                if agent.status == "running" && !showingDone {
                    HStack(spacing: 3) {
                        BouncingDot(delay: 0.0, size: 5, color: color)
                        BouncingDot(delay: 0.15, size: 5, color: color)
                        BouncingDot(delay: 0.30, size: 5, color: color)
                    }
                    .shadow(color: color.opacity(0.5), radius: 4)
                } else if agent.status == "running" && showingDone {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.caption2)
                        .foregroundStyle(color)
                } else {
                    Circle()
                        .fill(color.opacity(0.5))
                        .frame(width: 6, height: 6)
                }
            }
            .frame(width: 24, alignment: .leading)

            // Name
            Text(agent.displayName)
                .font(.caption.weight(.semibold))
                .foregroundStyle(.primary)
                .lineLimit(1)
                .truncationMode(.tail)
                .frame(maxWidth: 120, alignment: .leading)

            // Task / invited label
            Group {
                if agent.status == "running", let task = agent.task, !task.isEmpty {
                    Text(task)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                } else if agent.status != "running" {
                    Text("invited")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .trailing)
        }
    }

    private func agentColor(_ agent: AgentStateUpdate) -> Color {
        if let hex = agent.color, !hex.isEmpty { return Color(hex: hex) }
        switch agent.type {
        case "chief": return .orange
        case "staff": return JarvisTheme.accent
        default: return Color(hex: "#8A8A80")
        }
    }
}

#Preview("Staff Activity") {
    let json = """
    [
      {"name":"news_desk","status":"running","metadata":{"displayName":"News Desk","type":"staff","visibility":"ephemeral","invited":false,"task":"Fetching RSS feeds","color":"#E8854A"}},
      {"name":"research","status":"running","metadata":{"displayName":"Research Analyst","type":"staff","visibility":"ephemeral","invited":false,"task":"Searching papers","color":"#4ECDC4"}},
      {"name":"cfo","status":"invited","metadata":{"displayName":"CFO Watch","type":"staff","visibility":"ephemeral","invited":true,"color":"#A8E6CF"}}
    ]
    """.data(using: .utf8)!
    let agents = (try? JSONDecoder().decode([AgentStateUpdate].self, from: json)) ?? []
    return ZStack {
        Color.black.ignoresSafeArea()
        StaffActivityView(agents: agents)
            .padding()
    }
}
