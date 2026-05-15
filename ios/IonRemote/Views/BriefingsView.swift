import SwiftUI

// MARK: - BriefingsView

struct BriefingsView: View {
    @Environment(BriefingsStore.self) private var store
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ZStack {
                JarvisTheme.background.ignoresSafeArea()
                Group {
                    if store.briefings.isEmpty {
                        ContentUnavailableView("No Briefings", systemImage: "newspaper")
                            .foregroundStyle(JarvisTheme.textSecondary)
                    } else {
                        List(store.briefings) { item in
                            BriefingRowView(item: item)
                                .listRowBackground(
                                    RoundedRectangle(cornerRadius: 10)
                                        .fill(JarvisTheme.surfaceElevated.opacity(0.65))
                                        .overlay(
                                            RoundedRectangle(cornerRadius: 10)
                                                .stroke(JarvisTheme.accent.opacity(0.12), lineWidth: 0.5)
                                        )
                                        .padding(.horizontal, 4)
                                )
                                .listRowSeparator(.hidden)
                        }
                        .scrollContentBackground(.hidden)
                        .background(Color.clear)
                    }
                }
            }
            .toolbar {
                ToolbarItem(placement: .principal) {
                    Text("B R I E F I N G S")
                        .font(.headline.weight(.black))
                        .kerning(3)
                        .foregroundStyle(JarvisTheme.accent)
                        .shadow(color: JarvisTheme.accent.opacity(0.9), radius: 4)
                        .shadow(color: JarvisTheme.accent.opacity(0.6), radius: 10)
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Done") { dismiss() }
                        .foregroundStyle(JarvisTheme.accent)
                }
            }
            .toolbarBackground(.ultraThinMaterial, for: .navigationBar)
            .toolbarColorScheme(.dark, for: .navigationBar)
        }
    }
}

// MARK: - BriefingRowView

private struct BriefingRowView: View {
    let item: BriefingItem

    @Environment(BriefingsStore.self) private var store
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                if !item.isRead {
                    Circle()
                        .fill(JarvisTheme.accent)
                        .frame(width: 7, height: 7)
                }
                Text(item.title)
                    .font(.headline)
                    .foregroundStyle(JarvisTheme.textPrimary)
                Spacer()
                Text(item.receivedAt.relativeFormatted)
                    .font(.caption2)
                    .foregroundStyle(JarvisTheme.textSecondary)
                Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                    .font(.caption2)
                    .foregroundStyle(JarvisTheme.textSecondary)
            }
            if isExpanded {
                Text(item.text)
                    .font(.system(.footnote, design: .monospaced))
                    .foregroundStyle(JarvisTheme.textPrimary)
                    .textSelection(.enabled)
            }
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
        .onTapGesture {
            withAnimation(.easeInOut(duration: 0.2)) {
                isExpanded.toggle()
            }
            if isExpanded {
                store.markRead(id: item.id)
            }
        }
    }
}

// MARK: - Date+Relative

private extension Date {
    var relativeFormatted: String {
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .abbreviated
        return formatter.localizedString(for: self, relativeTo: Date())
    }
}

// MARK: - Preview

#Preview {
    let store = BriefingsStore()
    store.receive(briefingId: "morning_brief", title: "Morning Brief", text: "Good morning. No urgent matters.")
    store.receive(briefingId: "midday_checkin", title: "Midday Check-in", text: "All systems nominal. Three items pending review.")
    return BriefingsView()
        .environment(store)
        .preferredColorScheme(.dark)
}
