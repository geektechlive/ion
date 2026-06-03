import SwiftUI

/// Top-level settings screen. Shows category rows that push into
/// dedicated detail views, matching the iOS Settings.app pattern.
/// All section logic now lives in the per-category files:
/// - SettingsDesktopsView (connection, desktop settings, paired devices)
/// - SettingsAppearanceView (theme)
/// - SettingsInterfaceView (new tab, tab list, agent panel, tab groups)
/// - SettingsModelsView (conversation/engine model pickers)
/// - SettingsVoiceView (voice toggle, API key, mode, prompt)
/// - SettingsDiagnosticsView (transport info, diagnostic log, about)
struct SettingsView: View {
    @Environment(SessionViewModel.self) private var viewModel
    @Environment(\.appTheme) private var theme
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                categoryLink(
                    "Desktops & Connection",
                    icon: "desktopcomputer",
                    color: .blue,
                    detail: viewModel.activeDevice?.displayName
                ) {
                    SettingsDesktopsView()
                }

                categoryLink(
                    "Appearance",
                    icon: "paintbrush",
                    color: .purple,
                    detail: currentThemeName
                ) {
                    SettingsAppearanceView()
                }

                categoryLink(
                    "Interface",
                    icon: "square.grid.2x2",
                    color: .indigo
                ) {
                    SettingsInterfaceView()
                }

                categoryLink(
                    "Models",
                    icon: "cpu",
                    color: .orange,
                    detail: currentModelLabel
                ) {
                    SettingsModelsView()
                }

                categoryLink(
                    "Voice",
                    icon: "waveform",
                    color: .green,
                    detail: viewModel.voiceService.isEnabled ? "On" : "Off"
                ) {
                    SettingsVoiceView()
                }

                categoryLink(
                    "Diagnostics & About",
                    icon: "stethoscope",
                    color: .gray
                ) {
                    SettingsDiagnosticsView()
                }
            }
            .navigationTitle("Settings")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }

    // MARK: - Helpers

    private var currentThemeName: String {
        ThemeRegistry.themes.first { $0.id == theme.selectedThemeId }?.displayName ?? "Default"
    }

    private var currentModelLabel: String {
        viewModel.availableModels.first { $0.id == viewModel.preferredModel }?.label ?? viewModel.preferredModel
    }

    /// Reusable category row with icon, label, optional detail, and chevron.
    private func categoryLink<Destination: View>(
        _ title: String,
        icon: String,
        color: Color,
        detail: String? = nil,
        @ViewBuilder destination: () -> Destination
    ) -> some View {
        NavigationLink(destination: destination) {
            HStack(spacing: 12) {
                Image(systemName: icon)
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(.white)
                    .frame(width: 28, height: 28)
                    .background(color, in: RoundedRectangle(cornerRadius: 6))

                Text(title)

                Spacer()

                if let detail {
                    Text(detail)
                        .foregroundStyle(.secondary)
                        .font(.subheadline)
                        .lineLimit(1)
                }
            }
        }
    }
}
