import SwiftUI

struct SettingsModelsView: View {
    @Environment(SessionViewModel.self) private var viewModel
    @Environment(\.appTheme) private var theme

    var body: some View {
        let models = viewModel.availableModels
        List {
            Section("Models") {
                Picker("Conversation", selection: Binding<String>(
                    get: { viewModel.preferredModel },
                    set: { newValue in viewModel.setPreferredModelDefault(newValue) }
                )) {
                    ForEach(models) { model in
                        Text(model.label).tag(model.id)
                    }
                }
                Picker("Engine", selection: Binding<String>(
                    get: { viewModel.engineDefaultModel },
                    set: { newValue in viewModel.setEngineDefaultModelDefault(newValue) }
                )) {
                    Text("Same as Conversation").tag("")
                    ForEach(models) { model in
                        Text(model.label).tag(model.id)
                    }
                }
            }
        }
        .navigationTitle("Models")
        .navigationBarTitleDisplayMode(.inline)
    }
}
