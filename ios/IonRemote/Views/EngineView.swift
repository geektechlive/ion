import SwiftUI

struct EngineView: View {
    let tabId: String
    @Environment(SessionViewModel.self) private var viewModel
    @State private var promptText = ""
    @FocusState private var isInputFocused: Bool
    @State private var agentsPanelExpanded = true
    @State private var agentPanelFullscreen = false
    @State private var isNearBottom = true

    private var instances: [EngineInstanceInfo] {
        viewModel.engineInstances[tabId] ?? []
    }

    private var activeInstanceId: String {
        viewModel.activeEngineInstance[tabId] ?? instances.first?.id ?? ""
    }

    private var compoundKey: String {
        viewModel.engineCompoundKey(tabId: tabId)
    }

    // Feature 2b: sorted agents
    private var visibleAgents: [AgentStateUpdate] {
        (viewModel.engineAgentStates[compoundKey] ?? [])
            .filter(\.isVisible)
            .sorted { a, b in
                let statusOrder: [String: Int] = ["running": 0, "done": 1, "error": 1, "cancelled": 1, "idle": 2]
                let visOrder: [String: Int] = ["always": 0, "sticky": 1, "ephemeral": 2]
                let sa = statusOrder[a.status] ?? 2
                let sb = statusOrder[b.status] ?? 2
                if sa != sb { return sa < sb }
                let va = visOrder[a.visibility] ?? 9
                let vb = visOrder[b.visibility] ?? 9
                if va != vb { return va < vb }
                return a.displayName.localizedCompare(b.displayName) == .orderedAscending
            }
    }

    private var activeToolsList: [ActiveToolInfo] {
        (viewModel.activeTools[compoundKey] ?? [:]).values.sorted { $0.startTime < $1.startTime }
    }

    private var engineMsgs: [EngineMessage] {
        viewModel.engineMessages[compoundKey] ?? []
    }

    // Feature 1: tool grouping
    private enum GroupedItem: Identifiable {
        case single(EngineMessage)
        case toolGroup([EngineMessage])

        var id: String {
            switch self {
            case .single(let msg): return msg.id
            case .toolGroup(let msgs): return "tg-\(msgs.first?.id ?? "")"
            }
        }
    }

    private var groupedMessages: [GroupedItem] {
        var result: [GroupedItem] = []
        var toolBuf: [EngineMessage] = []
        for msg in engineMsgs {
            if msg.role == "tool" {
                toolBuf.append(msg)
            } else {
                if !toolBuf.isEmpty {
                    result.append(.toolGroup(toolBuf))
                    toolBuf = []
                }
                result.append(.single(msg))
            }
        }
        if !toolBuf.isEmpty {
            result.append(.toolGroup(toolBuf))
        }
        return result
    }

    var body: some View {
        VStack(spacing: 0) {
            // Context usage bar
            if let fields = viewModel.engineStatusFields[compoundKey] {
                GeometryReader { geo in
                    Rectangle()
                        .fill(contextBarColor(fields.contextPercent))
                        .frame(width: geo.size.width * min(CGFloat(fields.contextPercent) / 100, 1))
                }
                .frame(height: 3)
                .background(Color(.tertiarySystemFill))
            }

            // Engine instance bar (shown when multiple instances exist)
            if instances.count > 1 {
                EngineInstanceBar(
                    tabId: tabId,
                    instances: instances,
                    activeInstanceId: activeInstanceId
                )
            }

            // Working message header
            if let working = viewModel.engineWorkingMessages[compoundKey], !working.isEmpty {
                HStack {
                    ProgressView()
                        .scaleEffect(0.7)
                    Text(working)
                        .lineLimit(1)
                        .truncationMode(.tail)
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
                .background(Capsule().fill(Color(.tertiarySystemFill)))
                .padding(.horizontal, 12)
                .padding(.vertical, 4)
                .frame(maxWidth: .infinity, alignment: .leading)
            }

            // Pinned prompt header
            if let prompt = viewModel.enginePinnedPrompt[compoundKey], !prompt.isEmpty {
                HStack {
                    Text("> ")
                        .foregroundStyle(.orange)
                        .fontWeight(.semibold)
                    Text(prompt)
                        .lineLimit(1)
                        .truncationMode(.tail)
                }
                .font(IonTheme.codeFont(size: 12))
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color(.secondarySystemFill).opacity(0.7))
            }

            // Conversation messages area
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 8) {
                        ForEach(groupedMessages) { item in
                            switch item {
                            case .single(let msg):
                                EngineMessageRow(message: msg)
                                    .id(msg.id)
                            case .toolGroup(let tools):
                                EngineToolGroupRow(tools: tools)
                                    .id("tg-\(tools.first?.id ?? "")")
                            }
                        }

                        // Feature 2: bottom anchor for scroll tracking
                        Color.clear
                            .frame(height: 1)
                            .id("bottom-anchor")
                            .onAppear { isNearBottom = true }
                            .onDisappear { isNearBottom = false }
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                }
                .onChange(of: engineMsgs.count) {
                    if isNearBottom, let last = engineMsgs.last {
                        withAnimation(.easeOut(duration: 0.2)) {
                            proxy.scrollTo(last.id, anchor: .bottom)
                        }
                    }
                }
                // Feature 2: scroll-to-bottom button
                .overlay(alignment: .bottom) {
                    if !isNearBottom {
                        Button {
                            withAnimation {
                                proxy.scrollTo("bottom-anchor", anchor: .bottom)
                            }
                        } label: {
                            Image(systemName: "chevron.down.circle.fill")
                                .font(.title2)
                                .foregroundStyle(.orange)
                                .background(Circle().fill(.ultraThinMaterial))
                        }
                        .padding(.bottom, 8)
                        .transition(.opacity)
                    }
                }
            }
            // Feature 3: layout priority for messages vs agents
            .frame(maxHeight: agentPanelFullscreen ? 100 : .infinity)
            .layoutPriority(agentPanelFullscreen ? 0 : 1)

            // Active tool cards (above agent bars)
            if !activeToolsList.isEmpty {
                VStack(spacing: 4) {
                    ForEach(activeToolsList) { tool in
                        ActiveToolRow(tabId: tabId, tool: tool)
                    }
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 6)
            }

            // Agent bars with collapsible header
            if !visibleAgents.isEmpty {
                VStack(spacing: 0) {
                    // Collapsible header with fullscreen toggle
                    HStack(spacing: 4) {
                        Button {
                            withAnimation(IonTheme.snappySpring) {
                                agentsPanelExpanded.toggle()
                            }
                        } label: {
                            HStack(spacing: 4) {
                                Image(systemName: agentsPanelExpanded ? "chevron.down" : "chevron.right")
                                    .font(.caption2)
                                Text("Agents")
                                    .font(.caption.weight(.semibold))
                                Text("(\(visibleAgents.count))")
                                    .font(.caption2)
                                    .foregroundStyle(.tertiary)
                            }
                            .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.plain)

                        Spacer()

                        // Feature 3: fullscreen toggle
                        Button {
                            withAnimation(IonTheme.snappySpring) {
                                agentPanelFullscreen.toggle()
                            }
                        } label: {
                            Image(systemName: agentPanelFullscreen
                                  ? "arrow.down.right.and.arrow.up.left"
                                  : "arrow.up.left.and.arrow.down.right")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.plain)
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 4)

                    if agentsPanelExpanded {
                        ScrollView {
                            VStack(spacing: 4) {
                                ForEach(visibleAgents) { agent in
                                    AgentBarRow(agent: agent)
                                }
                            }
                            .padding(.horizontal, 8)
                            .padding(.vertical, 6)
                        }
                        .frame(maxHeight: agentPanelFullscreen ? .infinity : 132)
                    }
                }
                .layoutPriority(agentPanelFullscreen ? 1 : 0)
            }

            Divider()

            // Status footer
            if let fields = viewModel.engineStatusFields[compoundKey] {
                EngineFooterView(
                    fields: fields,
                    onSelectModel: { model in
                        viewModel.setEngineModel(tabId: tabId, model: model)
                    },
                    selectedModel: viewModel.engineModelOverrides[compoundKey] ?? ""
                )
            }

            Divider()

            // Input bar
            HStack(spacing: 8) {
                TextField("Send a prompt...", text: $promptText)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .background(Color(.tertiarySystemBackground))
                    .clipShape(RoundedRectangle(cornerRadius: IonTheme.Radius.medium))
                    .overlay(RoundedRectangle(cornerRadius: IonTheme.Radius.medium).stroke(Color(.separator), lineWidth: 1))
                    .focused($isInputFocused)
                    .onSubmit { submitPrompt() }

                Button {
                    submitPrompt()
                } label: {
                    Image(systemName: "arrow.up.circle.fill")
                        .font(.title)
                        .foregroundStyle(.orange)
                }
                .disabled(promptText.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .navigationTitle(viewModel.tab(for: tabId)?.displayTitle ?? "Engine")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    viewModel.addEngineInstance(tabId: tabId)
                } label: {
                    Image(systemName: "plus.rectangle")
                }
            }
        }
        .onAppear {
            viewModel.loadEngineConversation(tabId: tabId)
        }
        .task(id: compoundKey) {
            // Retry loading if no messages arrived within 2 seconds
            // (handles relay latency / dropped responses)
            try? await Task.sleep(for: .seconds(2))
            if !Task.isCancelled && engineMsgs.isEmpty {
                viewModel.loadEngineConversation(tabId: tabId)
            }
        }
        // Dialog sheet
        .sheet(item: Binding(
            get: { viewModel.engineDialogs[compoundKey] ?? nil },
            set: { _ in }
        )) { dialog in
            EngineDialogSheet(tabId: tabId, dialog: dialog)
        }
    }

    private func submitPrompt() {
        let trimmed = promptText.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return }
        Haptic.light()
        viewModel.submitEnginePrompt(tabId: tabId, text: promptText)
        promptText = ""
    }

    private func contextBarColor(_ percent: Double) -> Color {
        if percent < 50 { return .green }
        if percent < 80 { return .orange }
        return .red
    }
}

// MARK: - EngineMessageRow

/// Renders a single engine conversation message based on role.
struct EngineMessageRow: View {
    let message: EngineMessage

    var body: some View {
        switch message.role {
        case "user":
            userMessage
        case "assistant":
            assistantMessage
        case "harness":
            harnessMessage
        case "tool":
            toolMessage
        default:
            systemMessage
        }
    }

    // Feature 1: markdown user message with bubble styling
    private var userMessage: some View {
        HStack {
            Spacer()
            HStack(spacing: 0) {
                RoundedRectangle(cornerRadius: 1.5)
                    .fill(IonTheme.accent)
                    .frame(width: 2.5)
                MarkdownContentView(
                    blocks: MarkdownBlockCache.shared.blocks(for: message.content)
                )
                .textSelection(.enabled)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
            }
            .background(
                ZStack {
                    Color(.tertiarySystemBackground)
                    IonTheme.userBubbleTint
                }
            )
            .clipShape(RoundedRectangle(cornerRadius: IonTheme.Radius.large))
        }
    }

    // Feature 1: markdown assistant message
    private var assistantMessage: some View {
        HStack {
            MarkdownContentView(
                blocks: MarkdownBlockCache.shared.blocks(for: message.content)
            )
            .textSelection(.enabled)
            Spacer(minLength: 0)
        }
    }

    private var harnessMessage: some View {
        HStack(spacing: 6) {
            Image(systemName: "gearshape.fill")
                .font(.caption2)
                .foregroundStyle(.orange.opacity(0.7))
            Text(message.content)
                .font(.caption)
                .foregroundStyle(.secondary)
                .italic()
            Spacer()
        }
        .padding(.vertical, 2)
    }

    private var toolMessage: some View {
        HStack(spacing: 6) {
            toolStatusIcon
            Text(message.toolName ?? "tool")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
            Spacer()
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(Color(.secondarySystemBackground))
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }

    @ViewBuilder
    private var toolStatusIcon: some View {
        switch message.toolStatus {
        case "running":
            ProgressView()
                .scaleEffect(0.6)
        case "completed":
            Image(systemName: "checkmark.circle.fill")
                .font(.caption2)
                .foregroundStyle(.green)
        case "error":
            Image(systemName: "xmark.circle.fill")
                .font(.caption2)
                .foregroundStyle(.red)
        default:
            Image(systemName: "wrench")
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
    }

    private var systemMessage: some View {
        HStack {
            Spacer()
            Text(message.content)
                .font(.caption)
                .foregroundStyle(.tertiary)
            Spacer()
        }
    }
}

// MARK: - ActiveToolRow

/// Displays an in-progress tool call with elapsed time and an abort button
/// when the tool appears stalled (> 30s or marked stalled by the engine).
struct ActiveToolRow: View {
    let tabId: String
    let tool: ActiveToolInfo
    @Environment(SessionViewModel.self) private var viewModel
    @State private var now = Date()
    @State private var showAbortConfirm = false

    private var elapsed: TimeInterval {
        now.timeIntervalSince(tool.startTime)
    }

    private var isLikelyStalled: Bool {
        tool.isStalled || elapsed > 30
    }

    var body: some View {
        HStack(spacing: 8) {
            // Tool name capsule
            Text(tool.toolName)
                .font(.caption.weight(.semibold))
                .foregroundStyle(.white)
                .padding(.horizontal, 8)
                .padding(.vertical, 3)
                .background(isLikelyStalled ? Color.red.opacity(0.85) : Color.orange.opacity(0.85))
                .clipShape(Capsule())

            // Elapsed time
            Text(formatElapsed(elapsed))
                .font(.caption2.monospacedDigit())
                .foregroundStyle(isLikelyStalled ? .red : .secondary)

            if isLikelyStalled {
                Text("may be stuck")
                    .font(.caption2)
                    .foregroundStyle(.red.opacity(0.8))
            }

            Spacer()

            // Status indicator or abort button
            if isLikelyStalled {
                Button {
                    showAbortConfirm = true
                } label: {
                    Text("Abort")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.white)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 4)
                        .background(Color.red)
                        .clipShape(Capsule())
                }
            } else {
                // Pulsing activity dot
                Circle()
                    .fill(.orange)
                    .frame(width: 6, height: 6)
                    .opacity(pulseOpacity)
                    .animation(.easeInOut(duration: 1).repeatForever(autoreverses: true), value: now)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .background(
            (isLikelyStalled ? Color.red : Color.orange)
                .opacity(0.08)
        )
        .clipShape(RoundedRectangle(cornerRadius: 6))
        .onReceive(Timer.publish(every: 1, on: .main, in: .common).autoconnect()) { time in
            now = time
        }
        .alert("Abort Run?", isPresented: $showAbortConfirm) {
            Button("Cancel", role: .cancel) {}
            Button("Abort", role: .destructive) {
                viewModel.abortEngine(tabId: tabId)
            }
        } message: {
            Text("\(tool.toolName) has been running for \(Int(elapsed))s. This may be waiting for a macOS permission dialog. Aborting will stop the entire run.")
        }
    }

    private var pulseOpacity: Double {
        // Alternate between 0.3 and 1.0 based on time
        let phase = now.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: 2)
        return phase < 1 ? 0.3 : 1.0
    }

    private func formatElapsed(_ interval: TimeInterval) -> String {
        let seconds = Int(interval)
        if seconds < 60 {
            return "\(seconds)s"
        }
        let minutes = seconds / 60
        let secs = seconds % 60
        return "\(minutes)m \(secs)s"
    }
}


