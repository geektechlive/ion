import SwiftUI
import PhotosUI
import UniformTypeIdentifiers

struct EngineView: View {
    let tabId: String
    @Environment(SessionViewModel.self) private var viewModel
    @State private var promptText = ""
    @FocusState private var isInputFocused: Bool
    @State private var agentsPanelExpanded = true
    @State private var agentPanelFullscreen = false
    @State private var isNearBottom = true
    @State private var showFileExplorer = false
    @State private var showGitPane = false
    @State private var pendingAttachments: [PendingAttachment] = []
    @State private var showAttachMenu = false
    @State private var showFilePicker = false
    @State private var showPhotoPicker = false
    @State private var showDocumentPicker = false
    @State private var photosPickerItems: [PhotosPickerItem] = []

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

    private var workingDirectory: String {
        viewModel.tab(for: tabId)?.workingDirectory ?? ""
    }

    private var hasUploading: Bool {
        pendingAttachments.contains { $0.isUploading }
    }

    // MARK: - Extracted sub-views

    private var conversationScroll: some View {
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
    }

    private var agentSection: some View {
        VStack(spacing: 0) {
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
                let agentList = ScrollView {
                    VStack(spacing: 4) {
                        ForEach(visibleAgents) { agent in
                            AgentBarRow(agent: agent)
                        }
                    }
                    .padding(.horizontal, 8)
                    .padding(.vertical, 6)
                }

                if agentPanelFullscreen {
                    agentList
                } else {
                    agentList.frame(maxHeight: 132)
                }
            }
        }
    }

    /// Header: context bar, instance bar, working message, pinned prompt.
    private var headerSection: some View {
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

            // Engine instance bar
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
        }
    }

    /// Footer: status, divider, chips, input.
    private var footerSection: some View {
        VStack(spacing: 0) {
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

            // Attachment chips
            if !pendingAttachments.isEmpty {
                AttachmentChipsView(attachments: pendingAttachments) { id in
                    pendingAttachments.removeAll { $0.id == id }
                }
            }

            // Input bar
            engineInputBar
        }
    }

    /// Main content layout without modifiers.
    private var mainContent: some View {
        VStack(spacing: 0) {
            headerSection

            // Conversation messages area
            if !agentPanelFullscreen {
                conversationScroll
            } else {
                conversationScroll
                    .frame(height: 100)
            }

            // Active tool cards
            if !activeToolsList.isEmpty {
                VStack(spacing: 4) {
                    ForEach(activeToolsList) { tool in
                        ActiveToolRow(tabId: tabId, tool: tool)
                    }
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 6)
            }

            // Agent bars
            if !visibleAgents.isEmpty {
                agentSection
            }

            footerSection
        }
    }

    private var toolbarButtons: some View {
        HStack(spacing: 12) {
            Button { showFileExplorer = true } label: {
                Image(systemName: "folder")
                    .font(.subheadline)
            }
            Button { showGitPane = true } label: {
                Image(systemName: "arrow.triangle.branch")
                    .font(.subheadline)
            }
            Button { viewModel.addEngineInstance(tabId: tabId) } label: {
                Image(systemName: "plus.rectangle")
            }
        }
    }

    var body: some View {
        mainContent
        .navigationTitle(viewModel.tab(for: tabId)?.displayTitle ?? "Engine")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) { toolbarButtons }
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
        .sheet(item: Binding(
            get: { viewModel.engineDialogs[compoundKey] ?? nil },
            set: { _ in }
        )) { dialog in
            EngineDialogSheet(tabId: tabId, dialog: dialog)
        }
        .fullScreenCover(isPresented: $showGitPane) {
            GitPaneView(tabId: tabId)
                .environment(viewModel)
        }
        .fullScreenCover(isPresented: $showFileExplorer) {
            FileExplorerView(tabId: tabId)
                .environment(viewModel)
        }
        .sheet(isPresented: $showFilePicker) {
            FilePickerSheet(initialDirectory: workingDirectory) { path, name in
                addFileAttachment(path: path, name: name)
            }
            .environment(viewModel)
        }
        .onChange(of: viewModel.pendingUploadResults) { _, results in
            consumeUploadResults(results)
        }
        .onChange(of: photosPickerItems) { _, items in
            for item in items { handlePhotoSelection(item) }
            photosPickerItems = []
        }
        .photosPicker(
            isPresented: $showPhotoPicker,
            selection: $photosPickerItems,
            maxSelectionCount: 5,
            matching: .images
        )
        .fileImporter(
            isPresented: $showDocumentPicker,
            allowedContentTypes: [.item],
            allowsMultipleSelection: true
        ) { result in
            handleDocumentPickerResult(result)
        }
        .confirmationDialog("Attach", isPresented: $showAttachMenu) {
            Button("Photo Library") { showPhotoPicker = true }
            Button("Choose File") { showDocumentPicker = true }
            Button("Browse Desktop Files") { showFilePicker = true }
            Button("Cancel", role: .cancel) {}
        }
    }

    // MARK: - Engine input bar

    private var engineInputBar: some View {
        HStack(spacing: 8) {
            attachButton

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
            .disabled(cannotSend)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    private var attachButton: some View {
        Button {
            showAttachMenu = true
        } label: {
            Image(systemName: "paperclip")
                .font(.title3)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Actions

    private var cannotSend: Bool {
        let empty = promptText.trimmingCharacters(in: .whitespaces).isEmpty
        return (empty && pendingAttachments.isEmpty) || hasUploading
    }

    private func submitPrompt() {
        let trimmed = promptText.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty || !pendingAttachments.isEmpty else { return }
        guard !hasUploading else { return }
        Haptic.light()
        let attachments = pendingAttachments.map(\.commandAttachment)
        viewModel.submitEnginePrompt(
            tabId: tabId,
            text: promptText,
            attachments: attachments.isEmpty ? nil : attachments
        )
        isInputFocused = false
        promptText = ""
        pendingAttachments = []
    }

    private func contextBarColor(_ percent: Double) -> Color {
        if percent < 50 { return .green }
        if percent < 80 { return .orange }
        return .red
    }

    // MARK: - Attachments

    private func addFileAttachment(path: String, name: String) {
        let imageExts: Set = ["png", "jpg", "jpeg", "gif", "webp", "svg"]
        let ext = (name as NSString).pathExtension.lowercased()
        let type = imageExts.contains(ext) ? "image" : "file"
        pendingAttachments.append(PendingAttachment(
            id: UUID().uuidString, type: type, name: name, path: path, isUploading: false
        ))
    }

    private func handlePhotoSelection(_ item: PhotosPickerItem) {
        let placeholderId = UUID().uuidString
        let name = "photo-\(Int(Date().timeIntervalSince1970)).jpeg"
        pendingAttachments.append(PendingAttachment(
            id: placeholderId, type: "image", name: name, path: "", isUploading: true
        ))

        Task {
            guard let data = try? await item.loadTransferable(type: Data.self) else {
                await MainActor.run { pendingAttachments.removeAll { $0.id == placeholderId } }
                return
            }
            let compressed = compressImage(data: data, maxBytes: 1_000_000)
            let base64 = compressed.base64EncodedString()
            let dataUrl = "data:image/jpeg;base64,\(base64)"
            await MainActor.run {
                viewModel.uploadAttachment(dataUrl: dataUrl, name: name)
            }
        }
    }

    private func handleDocumentPickerResult(_ result: Result<[URL], Error>) {
        guard case .success(let urls) = result else { return }
        for url in urls {
            guard url.startAccessingSecurityScopedResource() else { continue }
            defer { url.stopAccessingSecurityScopedResource() }
            let name = url.lastPathComponent
            guard let data = try? Data(contentsOf: url) else { continue }

            let placeholderId = UUID().uuidString
            let imageExts: Set = ["png", "jpg", "jpeg", "gif", "webp", "svg"]
            let ext = url.pathExtension.lowercased()
            let type = imageExts.contains(ext) ? "image" : "file"

            pendingAttachments.append(PendingAttachment(
                id: placeholderId, type: type, name: name, path: "", isUploading: true
            ))
            if type == "image" {
                let compressed = compressImage(data: data, maxBytes: 1_000_000)
                let base64 = compressed.base64EncodedString()
                let mimeExt = (ext == "jpg" || ext == "jpeg") ? "jpeg" : ext
                let dataUrl = "data:image/\(mimeExt);base64,\(base64)"
                viewModel.uploadAttachment(dataUrl: dataUrl, name: name)
            } else {
                let base64 = data.base64EncodedString()
                let dataUrl = "data:application/octet-stream;base64,\(base64)"
                viewModel.uploadAttachment(dataUrl: dataUrl, name: name)
            }
        }
    }

    private func compressImage(data: Data, maxBytes: Int) -> Data {
        guard let uiImage = UIImage(data: data) else { return data }
        var quality: CGFloat = 0.8
        while quality > 0.1 {
            if let jpeg = uiImage.jpegData(compressionQuality: quality), jpeg.count <= maxBytes {
                return jpeg
            }
            quality -= 0.1
        }
        return uiImage.jpegData(compressionQuality: 0.1) ?? data
    }

    private func consumeUploadResults(_ results: [UploadAttachmentResult]) {
        guard !results.isEmpty else { return }
        for result in results {
            if let idx = pendingAttachments.firstIndex(where: { $0.isUploading && $0.name == result.name }) {
                if let error = result.error, !error.isEmpty {
                    pendingAttachments.remove(at: idx)
                } else {
                    pendingAttachments[idx] = PendingAttachment(
                        id: result.id, type: pendingAttachments[idx].type, name: result.name, path: result.path, isUploading: false
                    )
                }
            }
        }
        viewModel.pendingUploadResults = []
    }
}
