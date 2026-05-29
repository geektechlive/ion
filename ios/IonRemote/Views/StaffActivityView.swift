import SwiftUI

/// Compact overlay showing actively running staff agents above the input bar.
/// Data source: `viewModel.engineAgentStates` filtered to running/invited agents.
/// UI/UX refinement is deferred — structure and data wiring are the primary goals here.
struct StaffActivityView: View {
    let agents: [AgentStateUpdate]

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            ForEach(agents) { agent in
                HStack(spacing: 6) {
                    Circle()
                        .fill(Color(hex: agent.color ?? "#E8854A"))
                        .frame(width: 8, height: 8)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(agent.displayName)
                            .font(.caption)
                            .fontWeight(.medium)
                            .foregroundColor(.primary)
                        if let task = agent.task, !task.isEmpty {
                            Text(task)
                                .font(.caption2)
                                .foregroundColor(.secondary)
                                .lineLimit(1)
                        }
                    }
                }
                .transition(.move(edge: .leading).combined(with: .opacity))
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 10))
        .animation(.spring(response: 0.3), value: agents.map(\.name))
    }
}

#Preview {
    // AgentStateUpdate uses a custom Codable init — construct via JSON for previews.
    let json = """
    [
        {"name":"admin","status":"running","metadata":{"displayName":"Admin","type":"staff","visibility":"ephemeral","invited":false,"task":"Checking calendar","color":"#4ECDC4"}},
        {"name":"cfo","status":"running","metadata":{"displayName":"CFO","type":"staff","visibility":"ephemeral","invited":false}}
    ]
    """.data(using: .utf8)!
    let agents = (try? JSONDecoder().decode([AgentStateUpdate].self, from: json)) ?? []
    return StaffActivityView(agents: agents)
        .padding()
}
