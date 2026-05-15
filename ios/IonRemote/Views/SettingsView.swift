import SwiftUI

struct SettingsView: View {
    @Environment(SessionViewModel.self) private var viewModel
    @Environment(\.dismiss) private var dismiss
    @State private var elevenLabsKey: String = ""
    @State private var keySaved = false
    @State private var voiceTestInProgress = false
    @State private var voiceTestResult: VoiceService.TestResult?
    @State private var showVoiceTestAlert = false
    @State private var voicePromptText: String = ""


    var body: some View {
        NavigationStack {
            List {
                connectionSection
                voiceSection
                newTabSection
                tabGroupsSection
                pairedDevicesSection
                aboutSection
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

    private var statusLabel: String {
        guard viewModel.connectionState == .connected else {
            return viewModel.connectionState.label
        }
        switch viewModel.transportState {
        case .lanPreferred: return "Connected (LAN)"
        case .relayOnly: return "Connected (Relay)"
        case .disconnected: return "Connected"
        }
    }

    // MARK: - Sections

    private var voiceSection: some View {
        Section {
            Toggle(isOn: Binding(
                get: { viewModel.voiceService.isEnabled },
                set: {
                    viewModel.voiceService.isEnabled = $0
                    viewModel.sendVoiceConfig()
                }
            )) {
                Label("Voice Responses", systemImage: "waveform")
            }
            SecureField("ElevenLabs API Key", text: $elevenLabsKey)
                .textContentType(.password)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
            Button {
                if elevenLabsKey.trimmingCharacters(in: .whitespaces).isEmpty {
                    KeychainHelper.delete("com.geektechlive.ionremote.elevenlabs")
                } else {
                    KeychainHelper.set(elevenLabsKey, service: "com.geektechlive.ionremote.elevenlabs")
                }
                withAnimation { keySaved = true }
                Haptic.success()
                Task {
                    try? await Task.sleep(nanoseconds: 2_000_000_000)
                    withAnimation { keySaved = false }
                }
            } label: {
                HStack {
                    Text(keySaved ? "Key Saved ✓" : "Save Key")
                    if keySaved {
                        Spacer()
                    }
                }
                .foregroundStyle(keySaved ? .green : JarvisTheme.accent)
            }
            Button {
                voiceTestInProgress = true
                Task {
                    let result = await viewModel.voiceService.testVoice()
                    voiceTestInProgress = false
                    voiceTestResult = result
                    showVoiceTestAlert = true
                    if result.isSuccess { Haptic.success() } else { Haptic.error() }
                }
            } label: {
                HStack {
                    Text("Test Voice")
                    if voiceTestInProgress {
                        Spacer()
                        ProgressView()
                    }
                }
            }
            .disabled(voiceTestInProgress)
            Picker(selection: Binding(
                get: { viewModel.voiceService.voiceMode },
                set: {
                    viewModel.voiceService.voiceMode = $0
                    viewModel.sendVoiceConfig()
                }
            )) {
                Text("Client-Only").tag(VoiceService.VoiceMode.clientOnly)
                Text("Desktop-Assisted").tag(VoiceService.VoiceMode.desktopAssisted)
            } label: {
                Label("Processing", systemImage: "cpu")
            }
            if viewModel.voiceService.voiceMode == .desktopAssisted {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Voice System Prompt")
                        .font(.subheadline.weight(.medium))
                    TextEditor(text: $voicePromptText)
                        .font(.caption)
                        .frame(minHeight: 120, maxHeight: 200)
                        .scrollContentBackground(.hidden)
                        .background(Color(.systemGray6))
                        .clipShape(RoundedRectangle(cornerRadius: 8))
                    HStack {
                        Button("Save Prompt") {
                            viewModel.voiceService.voiceSystemPrompt = voicePromptText
                            viewModel.sendVoiceConfig()
                            Haptic.success()
                        }
                        .font(.subheadline)
                        Spacer()
                        Button("Reset to Default") {
                            voicePromptText = VoiceService.defaultVoicePrompt
                            viewModel.voiceService.voiceSystemPrompt = voicePromptText
                            viewModel.sendVoiceConfig()
                        }
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    }
                }
            }
        } header: {
            Text("Voice")
        } footer: {
            if !viewModel.voiceService.isEnabled {
                Text("Voice is off.")
            } else if viewModel.voiceService.voiceMode == .desktopAssisted {
                Text("Desktop shapes LLM output for voice before iOS speaks it.")
            } else {
                Text("Jarvis will read responses aloud.")
            }
        }
        .onAppear {
            elevenLabsKey = KeychainHelper.get("com.geektechlive.ionremote.elevenlabs") ?? ""
            voicePromptText = viewModel.voiceService.voiceSystemPrompt
        }
        .alert(
            voiceTestResult?.isSuccess == true ? "Voice Test Passed" : "Voice Test Failed",
            isPresented: $showVoiceTestAlert
        ) {
            Button("OK", role: .cancel) { }
        } message: {
            Text(voiceTestResult?.message ?? "")
        }
    }

    @ViewBuilder
    private var connectionSection: some View {
        Section("Connection") {
            HStack {
                Text("Status")
                Spacer()
                HStack(spacing: 6) {
                    Circle()
                        .fill(viewModel.connectionState.color)
                        .frame(width: 8, height: 8)
                    Text(statusLabel)
                        .foregroundStyle(.secondary)
                }
            }

            HStack {
                Text("Relay URL")
                Spacer()
                Text(viewModel.relayURL.isEmpty ? "Not configured" : viewModel.relayURL)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
        }
    }

    private var newTabSection: some View {
        Section("New Tab") {
            Picker("Default Directory", selection: Binding<String?>(
                get: { viewModel.defaultBaseDirectory },
                set: { viewModel.defaultBaseDirectory = $0 }
            )) {
                Text("None (desktop default)").tag(nil as String?)
                ForEach(viewModel.recentDirectories, id: \.self) { dir in
                    Text((dir as NSString).lastPathComponent).tag(dir as String?)
                }
            }
        }
    }

    private var tabGroupsSection: some View {
        Section {
            Picker("Grouping", selection: Binding<String>(
                get: { viewModel.tabGroupMode == "manual" ? "manual" : "auto" },
                set: { newValue in viewModel.setTabGroupMode(newValue) }
            )) {
                Text("Auto (by directory)").tag("auto")
                Text("Manual (custom groups)").tag("manual")
            }

            if viewModel.tabGroupMode == "manual" {
                let sorted = viewModel.tabGroups.sorted { $0.order < $1.order }
                ForEach(sorted) { group in
                    HStack {
                        Text(group.label)
                        Spacer()
                        if group.isDefault {
                            Image(systemName: "star.fill")
                                .font(.caption)
                                .foregroundStyle(.yellow)
                        }
                    }
                }
            }
        } header: {
            Text("Tab Groups")
        } footer: {
            if viewModel.tabGroupMode == "manual" {
                Text("Groups are managed on the desktop app. Create or rearrange groups from the desktop settings.")
            }
        }
    }

    private var pairedDevicesSection: some View {
        Section("Paired Device") {
            if viewModel.pairedDevices.isEmpty {
                Text("No paired devices")
                    .foregroundStyle(.secondary)
            } else {
                ForEach(viewModel.pairedDevices) { device in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(device.name)
                            .font(.headline)
                        Text("Paired \(device.pairedAt.formatted(date: .abbreviated, time: .shortened))")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        if let lastSeen = device.lastSeen {
                            Text("Last seen \(lastSeen.formatted(.relative(presentation: .named)))")
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                    }
                }
                .onDelete { offsets in
                    let devices = offsets.map { viewModel.pairedDevices[$0] }
                    for device in devices {
                        viewModel.unpairDevice(device)
                    }
                }
            }
        }
    }

    private var aboutSection: some View {
        Section("About") {
            HStack {
                Text("Version")
                Spacer()
                Text(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "1.0")
                    .foregroundStyle(.secondary)
            }

            Button(role: .destructive) {
                dismiss()
                viewModel.resetAll()
            } label: {
                Text("Unpair Device")
            }
        }
    }
}
