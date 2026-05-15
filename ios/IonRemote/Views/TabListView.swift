import SwiftUI

struct TabListView: View {
    @Environment(SessionViewModel.self) private var viewModel
    @Environment(BriefingsStore.self) private var briefingsStore
    @State private var showSettings = false
    @State private var showBriefings = false
    @State private var navigationPath = NavigationPath()
    @State private var flickerOpacity: Double = 1.0

    private let agentHarnessDir = "/Users/cfavero/AgentHarness"

    var body: some View {
        ZStack {
            JarvisTheme.background.ignoresSafeArea()
            ArcReactorBackground()
                .ignoresSafeArea()
                .opacity(0.9)
            NavigationStack(path: $navigationPath) {
                List {
                    ForEach(viewModel.displayGroups, id: \.id) { group in
                        Section {
                            ForEach(group.tabs) { tab in
                                NavigationLink(value: tab.id) {
                                    JarvisTabRowView(tab: tab, showDirectory: viewModel.tabGroupMode == "manual")
                                }
                                .listRowBackground(
                                    RoundedRectangle(cornerRadius: 10)
                                        .fill(JarvisTheme.surfaceElevated.opacity(0.65))
                                        .overlay(
                                            RoundedRectangle(cornerRadius: 10)
                                                .stroke(JarvisTheme.accent.opacity(0.12), lineWidth: 0.5)
                                        )
                                        .padding(.horizontal, 4)
                                )
                                .contextMenu {
                                    if viewModel.tabGroupMode == "manual" {
                                        let targets = viewModel.tabGroups.filter { $0.id != tab.groupId }
                                        if !targets.isEmpty {
                                            Menu {
                                                ForEach(targets) { target in
                                                    Button(target.label) {
                                                        viewModel.moveTabToGroup(tabId: tab.id, groupId: target.id)
                                                    }
                                                }
                                            } label: {
                                                Label("Move to Group", systemImage: "arrow.right.arrow.left")
                                            }
                                        }
                                    }
                                }
                            }
                            .onDelete { offsets in
                                let ids = offsets.map { group.tabs[$0].id }
                                for id in ids {
                                    viewModel.closeTab(id)
                                }
                            }
                        } header: {
                            HStack {
                                Label(group.label, systemImage: group.icon)
                                    .font(.subheadline.weight(.semibold))
                                    .foregroundStyle(JarvisTheme.textSecondary)
                                Spacer()
                            }
                        }
                    }
                }
                .scrollContentBackground(.hidden)
                .background(Color.clear)
                .navigationDestination(for: String.self) { tabId in
                    if viewModel.tab(for: tabId)?.isEngine == true {
                        EngineView(tabId: tabId)
                    } else if viewModel.tab(for: tabId)?.isTerminalOnly == true {
                        RemoteTerminalView(tabId: tabId)
                    } else {
                        ConversationView(tabId: tabId)
                    }
                }
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) {
                        HStack(spacing: 12) {
                            Button {
                                showSettings = true
                            } label: {
                                Image(systemName: "gearshape")
                            }
                            Button {
                                showBriefings = true
                            } label: {
                                Image(systemName: "newspaper.fill")
                                    .overlay(alignment: .topTrailing) {
                                        if briefingsStore.unreadCount > 0 {
                                            Text("\(min(briefingsStore.unreadCount, 9))")
                                                .font(.system(size: 9, weight: .bold))
                                                .foregroundStyle(.black)
                                                .padding(3)
                                                .background(Color.cyan, in: Circle())
                                                .offset(x: 6, y: -6)
                                        }
                                    }
                            }
                            .tint(briefingsStore.unreadCount > 0 ? .cyan : .secondary)
                            ConnectionQualityView(compact: true)
                        }
                    }
                    ToolbarItem(placement: .principal) {
                        Text("J A R V I S")
                            .font(.headline.weight(.black))
                            .kerning(4)
                            .foregroundStyle(JarvisTheme.accent)
                            .shadow(color: JarvisTheme.accent.opacity(0.9), radius: 4)
                            .shadow(color: JarvisTheme.accent.opacity(0.6), radius: 10)
                            .shadow(color: JarvisTheme.accent.opacity(0.3), radius: 20)
                            .opacity(flickerOpacity)
                    }
                    ToolbarItem(placement: .topBarTrailing) {
                        Menu {
                            let profiles = viewModel.engineProfiles
                            if profiles.isEmpty {
                                Button {
                                    viewModel.createEngineTab(workingDirectory: agentHarnessDir)
                                } label: {
                                    Label("Jarvis", systemImage: "bolt.fill")
                                }
                            } else {
                                ForEach(profiles) { profile in
                                    Button {
                                        viewModel.createEngineTab(
                                            workingDirectory: agentHarnessDir,
                                            profileId: profile.id
                                        )
                                    } label: {
                                        Label(profile.name, systemImage: "bolt.fill")
                                    }
                                }
                            }
                            Divider()
                            Button {
                                viewModel.createTerminalTab()
                            } label: {
                                Label("Terminal", systemImage: "terminal")
                            }
                        } label: {
                            Image(systemName: "bolt.fill")
                        }
                        .tint(.cyan)
                    }
                }
                .refreshable {
                    viewModel.sync()
                }
                .onChange(of: viewModel.connectionState) { _, newState in
                    if newState == .disconnected {
                        navigationPath = NavigationPath()
                    } else if newState == .connected {
                        if let engineTab = viewModel.tabs.first(where: { $0.isEngine == true }) {
                            navigationPath.append(engineTab.id)
                        }
                    }
                }
                .onChange(of: viewModel.pendingNavigationTabId) { _, tabId in
                    if let tabId {
                        navigationPath.append(tabId)
                        viewModel.pendingNavigationTabId = nil
                    }
                }
                .onReceive(NotificationCenter.default.publisher(for: .briefingFromPush)) { note in
                    guard let info = note.userInfo,
                          let briefingId = info["briefingId"] as? String,
                          let title = info["title"] as? String,
                          let text = info["briefingText"] as? String else { return }
                    briefingsStore.receive(briefingId: briefingId, title: title, text: text)
                    if info["openSheet"] as? Bool == true {
                        showBriefings = true
                    }
                }
                .sheet(isPresented: $showSettings) {
                    SettingsView()
                }
                .sheet(isPresented: $showBriefings) {
                    BriefingsView()
                }
                .toolbarBackground(JarvisTheme.background.opacity(0.95), for: .navigationBar)
                .toolbarColorScheme(.dark, for: .navigationBar)
                .overlay {
                    if viewModel.tabs.isEmpty {
                        ContentUnavailableView(
                            "No Sessions",
                            systemImage: "bolt.fill",
                            description: Text("Tap ⚡ to start a Jarvis session or pull to refresh.")
                        )
                        .background(Color.clear)
                    }
                }
                .overlay(alignment: .top) {
                    if viewModel.voiceService.isSpeaking {
                        VoicePlaybackBar(
                            onSkip: { viewModel.voiceService.skip() },
                            onStopAll: { viewModel.voiceService.stop() },
                            hasPending: viewModel.voiceService.hasPending
                        )
                        .transition(.move(edge: .top).combined(with: .opacity))
                        .animation(IonTheme.snappySpring, value: viewModel.voiceService.isSpeaking)
                    }
                }
            }
            .task {
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(Double.random(in: 3.0...9.0)))
                    guard !Task.isCancelled else { break }
                    // Quick flicker sequence
                    withAnimation(.easeInOut(duration: 0.05)) { flickerOpacity = 0.55 }
                    try? await Task.sleep(for: .milliseconds(60))
                    withAnimation(.easeInOut(duration: 0.05)) { flickerOpacity = 1.0 }
                    try? await Task.sleep(for: .milliseconds(90))
                    withAnimation(.easeInOut(duration: 0.04)) { flickerOpacity = 0.75 }
                    try? await Task.sleep(for: .milliseconds(50))
                    withAnimation(.easeInOut(duration: 0.1)) { flickerOpacity = 1.0 }
                }
            }
        }
    }

}

// MARK: - TabRowView

private struct JarvisTabRowView: View {
    let tab: RemoteTabState
    var showDirectory: Bool = false

    @State private var pulseOpacity: Double = 1.0

    var body: some View {
        HStack(spacing: 12) {
            Circle()
                .fill(statusInfo.color)
                .frame(width: 10, height: 10)
                .opacity(statusInfo.pulse ? pulseOpacity : 1.0)
                .onChange(of: statusInfo.pulse) { _, shouldPulse in
                    if shouldPulse {
                        withAnimation(.easeInOut(duration: 1.5).repeatForever(autoreverses: true)) {
                            pulseOpacity = 0.3
                        }
                    } else {
                        withAnimation(.default) {
                            pulseOpacity = 1.0
                        }
                    }
                }
                .onAppear {
                    if statusInfo.pulse {
                        withAnimation(.easeInOut(duration: 1.5).repeatForever(autoreverses: true)) {
                            pulseOpacity = 0.3
                        }
                    }
                }

            if tab.isEngine == true {
                Image(systemName: "bolt.fill")
                    .font(.caption)
                    .foregroundStyle(JarvisTheme.textSecondary)
            } else if tab.isTerminalOnly == true {
                Image(systemName: "terminal")
                    .font(.caption)
                    .foregroundStyle(JarvisTheme.textSecondary)
            }

            VStack(alignment: .leading, spacing: 2) {
                Text(tab.displayTitle)
                    .font(.headline)
                    .foregroundStyle(JarvisTheme.textPrimary)

                if showDirectory {
                    Text(directoryLabel)
                        .font(.caption2)
                        .foregroundStyle(JarvisTheme.textSecondary)
                        .lineLimit(1)
                }

                if let message = tab.lastMessage {
                    Text(message)
                        .font(.caption2)
                        .foregroundStyle(JarvisTheme.textSecondary.opacity(0.6))
                        .lineLimit(1)
                }
            }

            Spacer()
        }
        .padding(.vertical, 4)
    }

    private var directoryLabel: String {
        let path = tab.workingDirectory
        let base = (path as NSString).lastPathComponent
        if base.isEmpty || path == "/" || path == "~" { return "Home" }
        return base
    }

    /// Status color and pulse state matching desktop TabStrip priority order.
    private var statusInfo: (color: Color, pulse: Bool) {
        // 1. Dead/Failed -> red (no pulse)
        if tab.status == .dead || tab.status == .failed {
            return (JarvisTheme.statusError, false)
        }

        // 2. Check permission queue for special tool states
        let hasGenericPermission = tab.permissionQueue.contains {
            $0.toolName != "ExitPlanMode" && $0.toolName != "AskUserQuestion"
        }
        let hasPlanReady = tab.permissionQueue.contains { $0.toolName == "ExitPlanMode" }
        let hasQuestion = tab.permissionQueue.contains { $0.toolName == "AskUserQuestion" }

        // 3. Generic permission -> orange (needs attention)
        if hasGenericPermission {
            return (Color(hex: 0xE8854A), false)
        }
        // 4. Running/Connecting -> Jarvis cyan + pulse
        if tab.status == .running || tab.status == .connecting {
            return (JarvisTheme.accent, true)
        }
        // 5. Plan ready -> green (idle or completed)
        if hasPlanReady && (tab.status == .idle || tab.status == .completed) {
            return (.green, false)
        }
        // 6. Question pending -> blue (idle or completed)
        if hasQuestion && (tab.status == .idle || tab.status == .completed) {
            return (JarvisTheme.statusQuestion, false)
        }
        // 7. Default -> gray
        return (JarvisTheme.statusIdle, false)
    }

}
