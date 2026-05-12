import SwiftUI

/// On-device diagnostic log viewer.
///
/// Shows timestamped connection lifecycle events in a scrollable list.
/// Use the Share button to AirDrop/copy the full log to a Mac for analysis.
struct DiagnosticLogView: View {
    @State private var entries: [DiagnosticLog.Entry] = []
    @State private var showShareSheet = false

    private let timeFmt: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "HH:mm:ss.SSS"
        return f
    }()

    var body: some View {
        List {
            if entries.isEmpty {
                Text("No diagnostic entries yet.\nConnect or switch desktops to generate logs.")
                    .foregroundStyle(.secondary)
                    .font(.caption)
            } else {
                ForEach(Array(entries.enumerated().reversed()), id: \.offset) { _, entry in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(timeFmt.string(from: entry.timestamp))
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                            .monospaced()
                        Text(entry.message)
                            .font(.caption)
                            .monospaced()
                    }
                    .listRowInsets(EdgeInsets(top: 4, leading: 12, bottom: 4, trailing: 12))
                }
            }
        }
        .listStyle(.plain)
        .navigationTitle("Diagnostic Log")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    ShareLink(item: DiagnosticLog.exportText()) {
                        Label("Share Log", systemImage: "square.and.arrow.up")
                    }
                    Button(role: .destructive) {
                        DiagnosticLog.clear()
                        entries = []
                    } label: {
                        Label("Clear Log", systemImage: "trash")
                    }
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
            }
        }
        .onAppear { entries = DiagnosticLog.entries() }
        .refreshable { entries = DiagnosticLog.entries() }
    }
}
