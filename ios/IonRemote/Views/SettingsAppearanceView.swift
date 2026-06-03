import SwiftUI

struct SettingsAppearanceView: View {
    @Environment(SessionViewModel.self) private var viewModel
    @Environment(\.appTheme) private var theme

    var body: some View {
        List {
            Section {
                Picker("Theme", selection: Binding(
                    get: { theme.selectedThemeId },
                    set: { newValue in
                        theme.selectedThemeId = newValue
                        DiagnosticLog.log("[SettingsView] theme picker set to: \(newValue)")
                    }
                )) {
                    ForEach(ThemeRegistry.themes, id: \.id) { t in
                        Text(t.displayName).tag(t.id)
                    }
                }
                .onChange(of: theme.selectedThemeId) { oldVal, newVal in
                    DiagnosticLog.log("[SettingsView] theme picker changed: \(oldVal) -> \(newVal)")
                    DiagnosticLog.log("[SettingsView] theme.accent is now: \(theme.accent)")
                }
            } header: {
                Text("Appearance")
            } footer: {
                Text("Arc Reactor forces dark mode. Ion Default follows system settings.")
            }
        }
        .navigationTitle("Appearance")
        .navigationBarTitleDisplayMode(.inline)
    }
}
