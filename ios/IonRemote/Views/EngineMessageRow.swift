import SwiftUI

// MARK: - EngineMessageRow

/// Renders a single engine conversation message based on role.
struct EngineMessageRow: View {
    let message: EngineMessage

    var body: some View {
        switch message.role {
        case "user":
            userMessage
        case "assistant":
            assistantMessage
        case "harness":
            harnessMessage
        case "tool":
            toolMessage
        default:
            systemMessage
        }
    }

    private var userMessage: some View {
        HStack {
            Spacer()
            HStack(spacing: 0) {
                RoundedRectangle(cornerRadius: 1.5)
                    .fill(IonTheme.accent)
                    .frame(width: 2.5)
                MarkdownContentView(
                    blocks: MarkdownBlockCache.shared.blocks(for: message.content)
                )
                .textSelection(.enabled)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
            }
            .background(
                ZStack {
                    Color(.tertiarySystemBackground)
                    IonTheme.userBubbleTint
                }
            )
            .clipShape(RoundedRectangle(cornerRadius: IonTheme.Radius.large))
        }
    }

    private var assistantMessage: some View {
        HStack {
            MarkdownContentView(
                blocks: MarkdownBlockCache.shared.blocks(for: message.content)
            )
            .textSelection(.enabled)
            Spacer(minLength: 0)
        }
    }

    private var harnessMessage: some View {
        HStack(spacing: 6) {
            Image(systemName: "gearshape.fill")
                .font(.caption2)
                .foregroundStyle(.orange.opacity(0.7))
            Text(message.content)
                .font(.caption)
                .foregroundStyle(.secondary)
                .italic()
            Spacer()
        }
        .padding(.vertical, 2)
    }

    private var toolMessage: some View {
        HStack(spacing: 6) {
            toolStatusIcon
            Text(chipLabel)
                .font(.caption.weight(.semibold))
                .foregroundStyle(IonTheme.accent)
            Spacer()
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(IonTheme.accent.opacity(0.12))
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }

    private var chipLabel: String {
        if let name = message.agentName, !name.isEmpty { return name }
        if message.toolName == "Agent" { return "Dispatching\u{2026}" }
        return message.toolName ?? "tool"
    }

    @ViewBuilder
    private var toolStatusIcon: some View {
        switch message.toolStatus {
        case "running":
            ProgressView()
                .scaleEffect(0.6)
        case "completed":
            Image(systemName: "checkmark.circle.fill")
                .font(.caption2)
                .foregroundStyle(.green)
        case "error":
            Image(systemName: "xmark.circle.fill")
                .font(.caption2)
                .foregroundStyle(.red)
        default:
            Image(systemName: "wrench")
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
    }

    private var systemMessage: some View {
        HStack {
            Spacer()
            Text(message.content)
                .font(.caption)
                .foregroundStyle(.tertiary)
            Spacer()
        }
    }
}
