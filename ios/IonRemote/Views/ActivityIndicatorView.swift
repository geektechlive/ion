import SwiftUI

/// Bouncing-dots activity indicator with a text label.
/// Mirrors the desktop's ConversationView activity row (three staggered
/// bouncing dots + currentActivity text like "Thinking…" or "Running Bash…").
struct ActivityIndicatorView: View {
    let text: String
    var dotColorOverride: Color? = nil

    private let dotSize: CGFloat = 5 // matches desktop 4×4px dots (bumped to 5 for mobile)
    private let defaultDotColor = Color(hex: 0xE8854A) // matches desktop statusRunning

    private var dotColor: Color { dotColorOverride ?? defaultDotColor }

    var body: some View {
        HStack(spacing: 6) {
            HStack(spacing: 3) {
                BouncingDot(delay: 0.0, size: dotSize, color: dotColor)
                BouncingDot(delay: 0.15, size: dotSize, color: dotColor)
                BouncingDot(delay: 0.30, size: dotSize, color: dotColor)
            }

            Text(text)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(Capsule().fill(Color(.tertiarySystemFill)))
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 16)
    }
}

