import SwiftUI

struct ConversationView: View {
    @Environment(SessionViewModel.self) private var viewModel
    @Environment(\.dismiss) private var dismiss
    let tabId: String

    @State private var cachedRestoredCard: PermissionRequest?
    @State private var scrollTask: Task<Void, Never>?
    @State private var scrollProxy: ScrollViewProxy?
    @State private var showGitPane = false
    @State private var showFileExplorer = false
    @State private var isNearBottom = true
    @State private var forceScrollCounter = 0

    private var tab: RemoteTabState? {
        viewModel.tab(for: tabId)
    }

    private var isRunning: Bool {
        tab?.status == .running
    }

    private var conversationMessages: [Message] {
        viewModel.messages[tabId] ?? []
    }

    private var isLoading: Bool {
        viewModel.loadingConversation.contains(tabId)
    }

    private var loadFailed: Bool {
        viewModel.conversationLoadFailed.contains(tabId)
    }

    /// Text shown in the activity indicator while running.
    private var currentActivity: String {
        if let liveText = viewModel.liveText[tabId], !liveText.isEmpty {
            return "Thinking..."
        }
        if let activeTools = viewModel.activeTools[tabId], !activeTools.isEmpty,
           let first = activeTools.values.first {
            return "Running \(first.toolName)..."
        }
        return "Working..."
    }

    /// True when the agent is compacting context.
    private var isCompacting: Bool {
        tab?.permissionMode == .auto && (viewModel.liveText[tabId]?.contains("compacting") == true)
    }

    private var pendingPermission: PermissionRequest? {
        if let queue = tab?.permissionQueue {
            // ExitPlanMode cards should only show when the tab is no longer running
            // (matches desktop where the card appears after task_complete).
            for request in queue {
                if request.toolName == "ExitPlanMode" {
                    if isRunning { continue }
                    // Snapshot queue entries lack planContent -- prefer restored card from enriched messages
                    if request.toolInput?["planContent"]?.value as? String == nil,
                       let restored = cachedRestoredCard,
                       restored.toolInput?["planContent"]?.value as? String != nil {
                        return restored
                    }
                }
                return request
            }
        }
        // Synthesize a card from conversation history when the permission queue
        // is empty (e.g. reopening a previous conversation). Only when idle --
        // completed/failed/dead conversations should not resurface old cards.
        if tab?.status == .idle, !viewModel.dismissedLiveSpecialTabs.contains(tabId),
           let restored = cachedRestoredCard,
           !viewModel.dismissedRestoredCards.contains(restored.questionId) {
            return restored
        }
        return nil
    }

    /// Detect ExitPlanMode or AskUserQuestion as the last tool in history
    /// and synthesize a PermissionRequest so the card renders on reopen.
    /// Returns nil if a user message appears after the tool (the conversation
    /// has moved past the plan/question and the card is stale).
    private func computeRestoredSpecialCard() -> PermissionRequest? {
        guard let lastTool = conversationMessages.last(where: { $0.isTool }),
              lastTool.toolName == "ExitPlanMode" || lastTool.toolName == "AskUserQuestion"
        else { return nil }

        // Stale detection: walk backwards from the end of the conversation.
        // If a user message appears before we hit the tool, the conversation
        // continued past this plan/question — don't resurface the card.
        for message in conversationMessages.reversed() {
            if message.id == lastTool.id { break } // hit the tool first — genuine
            if message.role == .user { return nil } // user spoke after — stale
        }

        var toolInput: [String: AnyCodable]?
        if let inputStr = lastTool.toolInput, let data = inputStr.data(using: .utf8),
           let dict = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
            toolInput = dict.mapValues { AnyCodable($0) }
        }
        return PermissionRequest(
            questionId: "restored-\(lastTool.id)",
            toolName: lastTool.toolName ?? "",
            toolInput: toolInput,
            options: []
        )
    }

    var body: some View {
        VStack(spacing: 0) {
            ZStack(alignment: .bottom) {
                messageList
                if !isNearBottom {
                    Button {
                        isNearBottom = true
                        forceScrollCounter += 1
                    } label: {
                        Image(systemName: "chevron.down")
                            .font(.system(size: 14, weight: .semibold))
                            .foregroundStyle(.secondary)
                            .frame(width: 40, height: 40)
                            .background(.regularMaterial)
                            .clipShape(Circle())
                            .shadow(color: .black.opacity(0.15), radius: 4, y: 2)
                    }
                    .padding(.bottom, 12)
                    .transition(.opacity.combined(with: .scale))
                }
            }
            .animation(IonTheme.snappySpring, value: isNearBottom)

            // Activity indicator — pinned above status bar, always visible
            if isRunning {
                ActivityIndicatorView(
                    text: currentActivity,
                    dotColorOverride: isCompacting ? .blue : nil
                )
            }

            // Voice playback bar — always visible when speaking
            if viewModel.voiceService.isSpeaking {
                VoicePlaybackBar(
                    onSkip: { viewModel.voiceService.skip() },
                    onStopAll: { viewModel.voiceService.stop() },
                    hasPending: viewModel.voiceService.hasPending
                )
                .transition(.move(edge: .bottom).combined(with: .opacity))
            }

            if let request = pendingPermission {
                PermissionCardView(tabId: tabId, request: request)
                    .padding(.horizontal, 16)
                    .padding(.bottom, 8)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }

            ConversationStatusBar(
                modelOverride: tab?.modelOverride,
                preferredModel: viewModel.preferredModel,
                contextPercent: tab?.contextPercent,
                contextTokens: tab?.contextTokens,
                isRunning: isRunning,
                onSelectModel: { model in
                    viewModel.setTabModel(tabId: tabId, model: model)
                }
            )


            // Queued prompts indicator
            if let queued = tab?.queuedPrompts, !queued.isEmpty {
                HStack(spacing: 6) {
                    Image(systemName: "clock")
                        .font(.caption)
                    Text("\(queued.count) queued prompt\(queued.count == 1 ? "" : "s")")
                        .font(.caption)
                }
                .foregroundStyle(.secondary)
                .padding(.vertical, 4)
            }

            InputBar(tabId: tabId)
        }
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(JarvisTheme.background.opacity(0.95), for: .navigationBar)
        .toolbarColorScheme(.dark, for: .navigationBar)
        .toolbar {
            ToolbarItem(placement: .principal) {
                Text(tab?.displayTitle ?? "Tab")
                    .font(.headline.weight(.bold))
                    .foregroundStyle(JarvisTheme.accent)
                    .shadow(color: JarvisTheme.accent.opacity(0.8), radius: 4)
                    .shadow(color: JarvisTheme.accent.opacity(0.4), radius: 10)
            }
            ToolbarItem(placement: .topBarTrailing) {
                HStack(spacing: 12) {
                    Button {
                        showFileExplorer = true
                    } label: {
                        Image(systemName: "folder")
                            .font(.subheadline)
                    }

                    Button {
                        showGitPane = true
                    } label: {
                        Image(systemName: "arrow.triangle.branch")
                            .font(.subheadline)
                    }

                    Button {
                        guard let current = tab?.permissionMode else { return }
                        let newMode: PermissionMode = current == .plan ? .auto : .plan
                        viewModel.setPermissionMode(tabId: tabId, mode: newMode)
                    } label: {
                        HStack(spacing: 4) {
                            Image(systemName: tab?.permissionMode == .plan ? "doc.text" : "bolt.fill")
                                .font(.caption)
                            Text(tab?.permissionMode == .plan ? "Plan" : "Auto")
                                .font(.caption.weight(.medium))
                        }
                        .foregroundStyle(tab?.permissionMode == .plan ? Color(hex: 0x2EB8A6) : Color.secondary)
                    }
                }
            }
        }
        .onAppear {
            viewModel.loadConversation(tabId: tabId)
            cachedRestoredCard = computeRestoredSpecialCard()
        }
        .onDisappear {
            scrollTask?.cancel()
            viewModel.clearConversation(tabId: tabId)
        }
        .onChange(of: viewModel.messageCountByTab[tabId]) {
            cachedRestoredCard = computeRestoredSpecialCard()
            guard !viewModel.suppressScrollToBottom else {
                viewModel.suppressScrollToBottom = false
                return
            }
            // Defer scroll to let LazyVStack finish layout
            scrollTask?.cancel()
            scrollTask = Task { @MainActor in
                try? await Task.sleep(for: .milliseconds(50))
                guard !Task.isCancelled else { return }
                if let proxy = scrollProxy {
                    withAnimation {
                        proxy.scrollTo("bottom", anchor: .bottom)
                    }
                }
            }
        }
        .onChange(of: viewModel.connectionState) { _, newState in
            if newState == .disconnected {
                dismiss()
            }
        }
        .onChange(of: viewModel.tabIds) { _, newIds in
            // Tab was closed on the desktop -- auto-dismiss
            if !newIds.contains(tabId) {
                dismiss()
            }
        }
        .animation(.default, value: pendingPermission?.id)
        .fullScreenCover(isPresented: $showGitPane) {
            GitPaneView(tabId: tabId)
                .environment(viewModel)
        }
        .fullScreenCover(isPresented: $showFileExplorer) {
            FileExplorerView(tabId: tabId)
                .environment(viewModel)
        }
    }

    // MARK: - Message List

    private var messageList: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 4) {
                    // "Load more" button at the top
                    if viewModel.conversationHasMore[tabId] == true {
                        Button {
                            viewModel.loadMoreMessages(tabId: tabId)
                        } label: {
                            if isLoading {
                                ProgressView()
                            } else {
                                Text("Load earlier messages")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .padding(.vertical, 8)
                    }

                    // Loading indicator for initial load
                    if isLoading && conversationMessages.isEmpty {
                        ProgressView("Loading conversation...")
                            .padding(.top, 40)
                    } else if loadFailed && conversationMessages.isEmpty {
                        Button {
                            viewModel.loadConversation(tabId: tabId)
                        } label: {
                            VStack(spacing: 8) {
                                Image(systemName: "arrow.clockwise")
                                    .font(.title2)
                                Text("Couldn't load conversation.\nTap to retry.")
                                    .font(.subheadline)
                                    .multilineTextAlignment(.center)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .buttonStyle(.plain)
                        .padding(.top, 40)
                    }

                    // Messages
                    ForEach(conversationMessages) { message in
                        MessageBubble(
                            message: message,
                            isRunning: isRunning && message.id == conversationMessages.last?.id,
                            onRewind: message.role == .user ? { messageId in
                                viewModel.rewindConversation(tabId: tabId, messageId: messageId)
                            } : nil,
                            onFork: message.role == .user ? { messageId in
                                viewModel.forkFromMessage(tabId: tabId, messageId: messageId)
                            } : nil
                        )
                        .id(message.id)
                    }

                    // Fallback: legacy liveText when no structured messages
                    if conversationMessages.isEmpty, let text = viewModel.liveText[tabId], !text.isEmpty {
                        Text(text)
                            .font(.system(.body, design: .monospaced))
                            .textSelection(.enabled)
                            .padding(.horizontal)
                    }

                    Color.clear
                        .frame(height: 1)
                        .id("bottom")
                }
                .padding(.vertical)
            }
            .onAppear { scrollProxy = proxy }
            .padding(.vertical, 8)
        }
    }

    // MARK: - Conversation item dispatch

    @ViewBuilder
    private func conversationItemView(_ item: ConversationItem) -> some View {
        switch item {
        case .user(let message):
            MessageBubble(
                message: message,
                isRunning: false,
                onRewind: { messageId in
                    viewModel.rewindConversation(tabId: tabId, messageId: messageId)
                },
                onFork: { messageId in
                    viewModel.forkFromMessage(tabId: tabId, messageId: messageId)
                }
            )

        case .assistant(let message):
            let isLast = message.id == conversationMessages.last?.id
            let combined = consecutiveAssistantContent(
                for: message.id, in: conversationMessages
            )
            let voiceSvc = viewModel.voiceService
            MessageBubble(
                message: message,
                isRunning: isRunning && isLast,
                copyableContent: combined,
                isSpeaking: voiceSvc.speakingMessageId == message.id && voiceSvc.isSpeaking,
                hasPendingSpeech: voiceSvc.hasPending,
                onSkipSpeaking: { voiceSvc.skip() },
                onStopAllSpeaking: { voiceSvc.stop() }
            )

        case .system(let message):
            MessageBubble(message: message)

        case .toolGroup(let tools):
            ToolGroupView(tools: tools, isTabRunning: isRunning)

        case .compaction(let message):
            CompactionRowView(message: message)
        }
    }
}

