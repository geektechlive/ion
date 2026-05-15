import SwiftUI

/// Jarvis signature color palette — arc reactor cyan.
enum JarvisTheme {
    static let accent = Color(red: 0x33 / 255, green: 0xC3 / 255, blue: 0xF7 / 255)
    static let accentSubtle = accent.opacity(0.12)
    static let accentGlow = accent.opacity(0.18)
    static let statusError = Color(red: 0xC4 / 255, green: 0x70 / 255, blue: 0x60 / 255)
    static let statusQuestion = Color(red: 0x4A / 255, green: 0x9E / 255, blue: 0xF5 / 255)
    static let statusIdle = Color(red: 0x8A / 255, green: 0x8A / 255, blue: 0x80 / 255)

    static let background = Color(red: 4 / 255, green: 14 / 255, blue: 28 / 255)
    static let surfaceElevated = Color(red: 8 / 255, green: 24 / 255, blue: 44 / 255)
    static let userBubbleBg = Color(red: 10 / 255, green: 36 / 255, blue: 60 / 255)
    private static let textBase = Color(red: 190 / 255, green: 235 / 255, blue: 255 / 255)
    static let textPrimary = textBase.opacity(0.92)
    static let textSecondary = textBase.opacity(0.55)
    static let textTertiary = textBase.opacity(0.33)
}

// MARK: - Color hex init (shared)

extension Color {
    init(hex: UInt, opacity: Double = 1.0) {
        self.init(
            .sRGB,
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255,
            opacity: opacity
        )
    }
}

#Preview {
    VStack(spacing: 12) {
        RoundedRectangle(cornerRadius: 8)
            .fill(JarvisTheme.accent)
            .frame(height: 44)
            .overlay(Text("accent").foregroundStyle(.black))
        RoundedRectangle(cornerRadius: 8)
            .fill(JarvisTheme.accentSubtle)
            .frame(height: 44)
            .overlay(Text("accentSubtle").foregroundStyle(.primary))
        RoundedRectangle(cornerRadius: 8)
            .fill(JarvisTheme.accentGlow)
            .frame(height: 44)
            .overlay(Text("accentGlow").foregroundStyle(.primary))
    }
    .padding()
    .preferredColorScheme(.dark)
}
