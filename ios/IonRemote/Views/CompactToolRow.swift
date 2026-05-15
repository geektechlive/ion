import SwiftUI

struct CompactToolRow: View {
    let message: EngineMessage
    @Binding var isExpanded: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                withAnimation(.easeInOut(duration: 0.2)) {
                    isExpanded.toggle()
                }
            } label: {
                HStack(spacing: 6) {
                    if let name = message.agentName {
                        Text("[\(name)]")
                            .font(.caption2.weight(.semibold))
                            .foregroundStyle(JarvisTheme.accent.opacity(0.8))
                    }
                    toolStatusIcon
                    // Hide "Agent" when the agent name badge already identifies who is running.
                    let displayName = message.toolName ?? "tool"
                    let hideName = message.agentName != nil && displayName.lowercased() == "agent"
                    if !hideName {
                        Text(displayName)
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(
                                message.toolStatus == "running"
                                    ? JarvisTheme.accent
                                    : JarvisTheme.textSecondary
                            )
                    }
                    Spacer()
                    if !message.content.isEmpty {
                        Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                            .font(.caption2)
                            .foregroundStyle(JarvisTheme.textSecondary.opacity(0.6))
                    }
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(JarvisTheme.surfaceElevated)
                .clipShape(RoundedRectangle(cornerRadius: 6))
            }
            .buttonStyle(.plain)

            if isExpanded && !message.content.isEmpty {
                Text(message.content)
                    .font(.caption2.monospaced())
                    .foregroundStyle(JarvisTheme.textSecondary)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 6)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(JarvisTheme.surfaceElevated.opacity(0.5))
                    .clipShape(RoundedRectangle(cornerRadius: 6))
                    .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
    }

    @ViewBuilder
    private var toolStatusIcon: some View {
        switch message.toolStatus {
        case "running":
            ProgressView()
                .scaleEffect(0.6)
                .tint(JarvisTheme.accent)
        case "completed":
            statusImage("checkmark.circle.fill", color: .green)
        case "error":
            statusImage("xmark.circle.fill", color: .red)
        default:
            statusImage("wrench", color: JarvisTheme.textSecondary)
        }
    }

    private func statusImage(_ name: String, color: Color) -> some View {
        Image(systemName: name)
            .font(.caption2)
            .foregroundStyle(color)
    }
}
