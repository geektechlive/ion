import SwiftUI

private let availableModels: [(id: String, label: String)] = [
    ("claude-opus-4-6", "Opus 4.6"),
    ("claude-sonnet-4-6", "Sonnet 4.6"),
    ("claude-haiku-4-5-20251001", "Haiku 4.5"),
]

struct StatusDrawerView: View {
    let tabId: String
    let compoundKey: String
    let fields: StatusFields?
    let agents: [AgentStateUpdate]
    let activeTools: [ActiveToolInfo]
    @Environment(SessionViewModel.self) private var viewModel

    private var resolvedModelId: String {
        if let override = viewModel.engineModelOverrides[compoundKey], !override.isEmpty {
            return override
        }
        return availableModels.first(where: { fields?.model.contains($0.id) == true })?.id
            ?? "claude-sonnet-4-6"
    }

    var body: some View {
        NavigationStack {
            List {
                Section("Model") {
                    Picker("Model", selection: Binding(
                        get: { resolvedModelId },
                        set: { viewModel.setEngineModel(tabId: tabId, model: $0) }
                    )) {
                        ForEach(availableModels, id: \.id) { m in
                            Text(m.label).tag(m.id)
                        }
                    }
                    .pickerStyle(.segmented)
                }

                if let fields {
                    Section("Context") {
                        HStack {
                            Text("Usage")
                                .foregroundStyle(JarvisTheme.textSecondary)
                            Spacer()
                            ProgressView(value: min(fields.contextPercent / 100.0, 1.0))
                                .frame(width: 80)
                                .tint(contextColor(fields.contextPercent))
                            Text("\(Int(fields.contextPercent))%")
                                .font(.caption.monospacedDigit())
                                .foregroundStyle(contextColor(fields.contextPercent))
                        }
                        if let cost = fields.totalCostUsd {
                            HStack {
                                Text("Cost")
                                    .foregroundStyle(JarvisTheme.textSecondary)
                                Spacer()
                                Text(String(format: "$%.4f", cost))
                                    .font(.caption.monospacedDigit())
                            }
                        }
                        HStack {
                            Text("State")
                                .foregroundStyle(JarvisTheme.textSecondary)
                            Spacer()
                            Text(fields.state)
                                .font(.caption)
                        }
                    }
                }

                if !activeTools.isEmpty {
                    Section("Active Tools") {
                        ForEach(activeTools) { tool in
                            ActiveToolRow(tabId: tabId, tool: tool)
                                .listRowInsets(EdgeInsets())
                                .listRowBackground(Color.clear)
                        }
                    }
                }

                if !agents.isEmpty {
                    Section("Staff") {
                        ForEach(agents) { agent in
                            AgentBarRow(agent: agent)
                                .listRowInsets(EdgeInsets())
                                .listRowBackground(Color.clear)
                        }
                    }
                }
            }
            .listStyle(.insetGrouped)
            .scrollContentBackground(.hidden)
            .background(JarvisTheme.background)
            .navigationTitle("Status")
            .navigationBarTitleDisplayMode(.inline)
        }
        .presentationDetents([.medium, .large])
        .presentationBackground(JarvisTheme.background)
    }

    private func contextColor(_ pct: Double) -> Color {
        if pct > 90 { return .red }
        if pct > 75 { return .orange }
        return JarvisTheme.textSecondary
    }
}
