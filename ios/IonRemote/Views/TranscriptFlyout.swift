import SwiftUI

struct TranscriptFlyout: View {
    let messages: [EngineMessage]

    private var assistantMessages: [EngineMessage] {
        Array(messages.filter { $0.role == "assistant" }.suffix(10))
    }

    var body: some View {
        VStack(spacing: 0) {
            Capsule()
                .frame(width: 36, height: 4)
                .foregroundStyle(JarvisTheme.textSecondary.opacity(0.4))
                .padding(.top, 8)
            Text("Transcript")
                .font(.caption)
                .foregroundStyle(JarvisTheme.textSecondary)
                .padding(.vertical, 8)
            Divider()
                .background(JarvisTheme.textSecondary.opacity(0.2))
            if assistantMessages.isEmpty {
                Spacer()
                Text("No messages yet.")
                    .font(.body)
                    .foregroundStyle(JarvisTheme.textSecondary)
                Spacer()
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 12) {
                        ForEach(assistantMessages) { msg in
                            Text(msg.content)
                                .font(.body)
                                .foregroundStyle(JarvisTheme.textPrimary)
                                .textSelection(.enabled)
                        }
                    }
                    .padding()
                }
            }
        }
        .background(JarvisTheme.background.ignoresSafeArea())
        .presentationDetents([.medium, .large])
        .presentationBackground(JarvisTheme.background)
    }
}
