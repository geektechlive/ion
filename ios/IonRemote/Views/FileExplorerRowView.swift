import SwiftUI

/// A single row in the file explorer: directory or file entry with indentation.
struct FileExplorerRowView: View {
    @Environment(SessionViewModel.self) private var viewModel
    let entry: FsEntry
    let depth: Int
    let isExpanded: Bool
    let onTap: () -> Void

    var body: some View {
        if entry.isDirectory {
            directoryRow
        } else {
            fileRow
        }
    }

    // MARK: - Directory Row

    private var directoryRow: some View {
        Button(action: onTap) {
            HStack(spacing: 8) {
                Spacer()
                    .frame(width: CGFloat(depth) * 20)

                Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                    .font(.caption2.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .frame(width: 12)

                Image(systemName: isExpanded ? "folder.fill" : "folder")
                    .font(.subheadline)
                    .foregroundStyle(Color.accentColor)

                Text(entry.name)
                    .font(.subheadline)
                    .foregroundStyle(.primary)
                    .lineLimit(1)

                Spacer()
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    // MARK: - File Row

    private var fileRow: some View {
        NavigationLink {
            FileEditorView(filePath: entry.path, fileName: entry.name)
                .environment(viewModel)
        } label: {
            HStack(spacing: 8) {
                Spacer()
                    .frame(width: CGFloat(depth) * 20)

                // Placeholder for chevron alignment
                Color.clear
                    .frame(width: 12)

                Image(systemName: entry.iconName)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)

                Text(entry.name)
                    .font(.subheadline)
                    .foregroundStyle(.primary)
                    .lineLimit(1)

                Spacer()

                Text(entry.formattedSize)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}
