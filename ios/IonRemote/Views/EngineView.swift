import SwiftUI

struct EngineView: View {
    let tabId: String
    @Environment(SessionViewModel.self) var viewModel
    @State private var promptText = ""
    @FocusState private var isInputFocused: Bool
    @State private var showStatusDrawer = false
    @State private var showTranscript = false
    @State var pendingAttachments: [PendingAttachment] = []
    private var instances: [EngineInstanceInfo] {
        viewModel.engineInstances[tabId] ?? []
    }

    private var activeInstanceId: String {
        viewModel.activeEngineInstance[tabId] ?? instances.first?.id ?? ""
    }

    private var compoundKey: String {
        viewModel.engineCompoundKey(tabId: tabId)
    }

    private var visibleAgents: [AgentStateUpdate] {
        (viewModel.engineAgentStates[compoundKey] ?? []).filter(\.isVisible)
    }

    private var activeToolsList: [ActiveToolInfo] {
        (viewModel.activeTools[compoundKey] ?? [:]).values.sorted { $0.startTime < $1.startTime }
    }

    private var engineMsgs: [EngineMessage] {
        viewModel.engineMessages[compoundKey] ?? []
    }

    var body: some View {
        let tab = viewModel.tab(for: tabId)
        let isRunning = tab?.status == .running

        return VStack(spacing: 0) {
            if instances.count > 1 {
                EngineInstanceBar(
                    tabId: tabId,
                    instances: instances,
                    activeInstanceId: activeInstanceId
                )
            }

            ThinkingScanLine(isActive: isRunning)

            if let working = viewModel.engineWorkingMessages[compoundKey], !working.isEmpty {
                HStack {
                    ProgressView().scaleEffect(0.7)
                    Text(working).lineLimit(1).truncationMode(.tail)
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                .modifier(EngineBannerStyle(verticalPadding: 6))
            }

            if let prompt = viewModel.enginePinnedPrompt[compoundKey], !prompt.isEmpty {
                HStack {
                    Text("> ").foregroundStyle(JarvisTheme.accent).fontWeight(.semibold)
                    Text(prompt).lineLimit(1).truncationMode(.tail)
                }
                .font(.caption.monospaced())
                .modifier(EngineBannerStyle(verticalPadding: 8))
            }

            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 8) {
                        ForEach(engineMsgs) { msg in
                            EngineMessageRow(message: msg)
                                .id(msg.id)
                        }
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                }
                .onChange(of: engineMsgs.count) {
                    if let last = engineMsgs.last {
                        withAnimation(.easeOut(duration: 0.2)) {
                            proxy.scrollTo(last.id, anchor: .bottom)
                        }
                    }
                }
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(spacing: 0) {
                if let fields = viewModel.engineStatusFields[compoundKey] {
                    HStack(spacing: 8) {
                        Circle()
                            .fill(isRunning ? JarvisTheme.accent : JarvisTheme.statusIdle)
                            .frame(width: 6, height: 6)
                        Text(fields.state)
                            .foregroundStyle(JarvisTheme.textSecondary)
                        Spacer()
                        let model = viewModel.engineModelOverrides[compoundKey] ?? fields.model
                        Text(shortModelName(model))
                            .foregroundStyle(JarvisTheme.textSecondary)
                    }
                    .font(.caption2)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 4)
                }
                Divider()
                let isPromptEmpty = promptText.trimmingCharacters(in: .whitespaces).isEmpty
                HStack(spacing: 8) {
                    TextField("Send a prompt...", text: $promptText)
                        .textFieldStyle(.plain)
                        .focused($isInputFocused)
                        .onSubmit { submitPrompt() }
                    Button { submitPrompt() } label: {
                        Image(systemName: "arrow.up.circle.fill")
                            .font(.title2)
                            .foregroundStyle(isPromptEmpty ? .gray : JarvisTheme.accent)
                    }
                    .disabled(isPromptEmpty)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(.ultraThinMaterial)
            }
        }
        .background(
            ZStack {
                JarvisTheme.background
                ArcReactorBackground()
                    .opacity(0.35)
            }
            .ignoresSafeArea()
        )
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(JarvisTheme.background.opacity(0.95), for: .navigationBar)
        .toolbarColorScheme(.dark, for: .navigationBar)
        .toolbar {
            ToolbarItem(placement: .principal) {
                Text(tab?.displayTitle ?? "Engine")
                    .font(.headline.weight(.bold))
                    .foregroundStyle(JarvisTheme.accent)
                    .shadow(color: JarvisTheme.accent.opacity(0.8), radius: 4)
                    .shadow(color: JarvisTheme.accent.opacity(0.4), radius: 10)
            }
            ToolbarItemGroup(placement: .navigationBarTrailing) {
                Button {
                    showTranscript = true
                } label: {
                    Image(systemName: "quote.bubble")
                        .foregroundStyle(JarvisTheme.accent)
                }
                Button {
                    showStatusDrawer = true
                } label: {
                    Image(systemName: "info.circle")
                        .foregroundStyle(JarvisTheme.accent)
                }
            }
        }
        .sheet(isPresented: $showStatusDrawer) {
            StatusDrawerView(
                tabId: tabId,
                compoundKey: compoundKey,
                fields: viewModel.engineStatusFields[compoundKey],
                agents: visibleAgents,
                activeTools: activeToolsList
            )
        }
        .sheet(isPresented: $showTranscript) {
            TranscriptFlyout(messages: engineMsgs)
        }
        .sheet(item: Binding(
            get: { viewModel.engineDialogs[compoundKey] ?? nil },
            set: { _ in viewModel.engineDialogs[compoundKey] = nil }
        )) { dialog in
            EngineDialogSheet(tabId: tabId, dialog: dialog)
        }
        .onAppear {
            viewModel.loadEngineConversation(tabId: tabId)
        }
        .task(id: compoundKey) {
            try? await Task.sleep(for: .seconds(2))
            if !Task.isCancelled && engineMsgs.isEmpty {
                viewModel.loadEngineConversation(tabId: tabId)
            }
        }
    }

    private func submitPrompt() {
        let trimmed = promptText.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return }
        viewModel.submitEnginePrompt(tabId: tabId, text: promptText)
        promptText = ""
    }

    private func shortModelName(_ id: String) -> String {
        if id.contains("opus") { return "Opus" }
        if id.contains("sonnet") { return "Sonnet" }
        if id.contains("haiku") { return "Haiku" }
        return id
    }
}

private struct EngineBannerStyle: ViewModifier {
    let verticalPadding: CGFloat

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, 12)
            .padding(.vertical, verticalPadding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.ultraThinMaterial)
    }
}

// MARK: - ThinkingScanLine

struct ThinkingScanLine: View {
    let isActive: Bool
    @State private var offset: CGFloat = -0.4

    var body: some View {
        GeometryReader { geo in
            Rectangle()
                .fill(LinearGradient(
                    colors: [.clear, JarvisTheme.accent.opacity(0.7), .clear],
                    startPoint: .leading, endPoint: .trailing
                ))
                .frame(width: geo.size.width * 0.4, height: 1)
                .offset(x: offset * geo.size.width)
                .onChange(of: isActive) { _, active in
                    if active { animateScan(width: geo.size.width) }
                    else { offset = -0.4 }
                }
                .onAppear {
                    if isActive { animateScan(width: geo.size.width) }
                }
        }
        .frame(height: 1)
        .opacity(isActive ? 1 : 0)
        .animation(.easeInOut(duration: 0.3), value: isActive)
    }

    private func animateScan(width: CGFloat) {
        offset = -0.4
        withAnimation(.linear(duration: 1.8).repeatForever(autoreverses: false)) {
            offset = 1.0
        }
    }
}
