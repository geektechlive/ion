import SwiftUI

// MARK: - NotificationsView

struct NotificationsView: View {
    @Environment(\.appTheme) private var theme
    @Environment(\.dismiss) private var dismiss
    let resourceStore: ResourceStore
    let viewModel: SessionViewModel

    @State private var selectedBriefing: ResourceItem? = nil

    private var briefings: [ResourceItem] {
        (resourceStore.items["briefing"] ?? [])
            .filter { $0.conversationId == nil || $0.conversationId?.isEmpty == true }
            .sorted { $0.createdAt > $1.createdAt }
    }

    var body: some View {
        NavigationStack {
            Group {
                if briefings.isEmpty {
                    emptyState
                } else {
                    briefingList
                }
            }
            .navigationTitle("Notifications")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Done") { dismiss() }
                        .fontWeight(.semibold)
                        .tint(theme.accent)
                }
            }
            .sheet(item: $selectedBriefing) { item in
                BriefingDetailView(item: item, resourceStore: resourceStore, viewModel: viewModel)
            }
        }
    }

    // MARK: - Empty state

    private var emptyState: some View {
        VStack(spacing: IonTheme.md) {
            Image(systemName: "bell.slash")
                .font(.system(size: 40))
                .foregroundStyle(.tertiary)
            Text("No Briefings Yet")
                .font(.title3.weight(.semibold))
                .foregroundStyle(.secondary)
            Text("Briefings from the ion-dev extension will appear here.")
                .font(.subheadline)
                .foregroundStyle(.tertiary)
                .multilineTextAlignment(.center)
                .padding(.horizontal, IonTheme.xl)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: - Briefing list

    private var briefingList: some View {
        List(briefings) { item in
            BriefingRow(item: item, resourceStore: resourceStore, viewModel: viewModel) { selectedBriefing = $0 }
                .listRowInsets(EdgeInsets(
                    top: IonTheme.sm,
                    leading: IonTheme.lg,
                    bottom: IonTheme.sm,
                    trailing: IonTheme.lg
                ))
        }
        .listStyle(.plain)
    }
}

// MARK: - BriefingRow

private struct BriefingRow: View {
    @Environment(\.appTheme) private var theme
    let item: ResourceItem
    let resourceStore: ResourceStore
    let viewModel: SessionViewModel
    let onSelect: (ResourceItem) -> Void

    private var isRead: Bool { resourceStore.readIds.contains(item.id) }
    private var title: String { item.title ?? item.metadata["agentName"] ?? "Briefing" }
    private var formattedTime: String {
        guard let date = ISO8601DateFormatter().date(from: item.createdAt) else { return "" }
        return date.formatted(date: .omitted, time: .shortened)
    }

    private var liveItem: ResourceItem {
        resourceStore.items[item.kind]?.first(where: { $0.id == item.id }) ?? item
    }

    var body: some View {
        Button {
            if !isRead {
                resourceStore.markRead(item.id)
                viewModel.send(.markResourceRead(kind: item.kind, resourceId: item.id))
            }
            onSelect(item)
        } label: {
            VStack(alignment: .leading, spacing: IonTheme.xs) {
                HStack(alignment: .firstTextBaseline, spacing: IonTheme.sm) {
                    Circle()
                        .fill(isRead ? Color.clear : theme.accent)
                        .frame(width: 7, height: 7)
                        .padding(.top, 3)
                    Text(title)
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(.primary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    Text(formattedTime)
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
                Text(liveItem.content.isEmpty ? "Tap to view…" : String(liveItem.content.prefix(120)))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                    .padding(.leading, IonTheme.md)
            }
        }
        .buttonStyle(.plain)
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            Button(role: .destructive) {
                resourceStore.deleteItem(kind: item.kind, resourceId: item.id)
                viewModel.send(.deleteResource(kind: item.kind, resourceId: item.id))
            } label: {
                Label("Delete", systemImage: "trash")
            }
        }
    }
}

// MARK: - BriefingDetailView

struct BriefingDetailView: View {
    @Environment(\.appTheme) private var theme
    @Environment(\.dismiss) private var dismiss
    let item: ResourceItem
    let resourceStore: ResourceStore
    let viewModel: SessionViewModel

    @State private var loadingContent = false
    @State private var contentFailed = false

    private var liveItem: ResourceItem {
        resourceStore.items[item.kind]?.first(where: { $0.id == item.id }) ?? item
    }
    private var title: String { item.title ?? item.metadata["agentName"] ?? "Briefing" }
    private var formattedTime: String {
        guard let date = ISO8601DateFormatter().date(from: item.createdAt) else { return "" }
        return date.formatted(date: .omitted, time: .shortened)
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                Group {
                    if !liveItem.content.isEmpty {
                        MarkdownContentView(blocks: MarkdownFormatter.parse(liveItem.content))
                            .padding(IonTheme.lg)
                            .onAppear { loadingContent = false }
                    } else if contentFailed {
                        HStack(spacing: IonTheme.sm) {
                            Image(systemName: "exclamationmark.circle")
                                .foregroundStyle(.secondary)
                            Text("Content unavailable")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            Spacer()
                            Button("Retry") {
                                contentFailed = false
                                loadingContent = true
                                viewModel.send(.requestResourceContent(kind: item.kind, resourceId: item.id))
                            }
                            .font(.caption)
                            .tint(theme.accent)
                        }
                        .padding(IonTheme.lg)
                    } else {
                        HStack(spacing: IonTheme.sm) {
                            ProgressView()
                                .scaleEffect(0.8)
                            Text("Loading…")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        .padding(IonTheme.lg)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Text(formattedTime)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Done") { dismiss() }
                        .fontWeight(.semibold)
                        .tint(theme.accent)
                }
            }
            .onAppear {
                if liveItem.content.isEmpty && !loadingContent && !contentFailed {
                    loadingContent = true
                    viewModel.send(.requestResourceContent(kind: item.kind, resourceId: item.id))
                }
            }
            .onChange(of: resourceStore.contentResponseIds) {
                guard loadingContent else { return }
                guard resourceStore.contentResponseIds.contains(item.id) else { return }
                if !liveItem.content.isEmpty {
                    loadingContent = false
                    contentFailed = false
                } else {
                    loadingContent = false
                    contentFailed = true
                }
            }
        }
    }
}

// MARK: - NotificationsBellButton

/// Bell button with unread badge for use in toolbars.
struct NotificationsBellButton: View {
    @Environment(\.appTheme) private var theme
    let resourceStore: ResourceStore
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack(alignment: .topTrailing) {
                Image(systemName: "bell")
                if resourceStore.unreadCount > 0 {
                    Text(resourceStore.unreadCount > 9 ? "9+" : "\(resourceStore.unreadCount)")
                        .font(.caption2.bold())
                        .foregroundStyle(.white)
                        .padding(3)
                        .background(Color.red)
                        .clipShape(Circle())
                        .offset(x: 6, y: -6)
                }
            }
        }
    }
}
