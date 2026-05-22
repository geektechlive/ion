import SwiftUI
import Combine
import PhotosUI

struct InputBar: View {
    @Environment(SessionViewModel.self) private var viewModel
    let tabId: String

    @State private var promptText = ""
    @FocusState private var isFocused: Bool
    @State private var keyboardVisible = false
    @State private var pendingAttachments: [PendingAttachment] = []
    @State private var slashFilter: String? = nil

    private var tab: RemoteTabState? {
        viewModel.tab(for: tabId)
    }

    private var isRunning: Bool {
        tab?.status == .running
    }

    private var isConnected: Bool {
        viewModel.connectionState == .connected
    }

    private var isQueued: Bool {
        isRunning  // Will queue behind current run
    }

    private var workingDirectory: String {
        tab?.workingDirectory ?? ""
    }

    var body: some View {
        VStack(spacing: 0) {
            if keyboardVisible {
                KeyboardUtilityBar(
                    onDismiss: { isFocused = false },
                    promptText: $promptText
                )
                .transition(.move(edge: .bottom).combined(with: .opacity))
            }

            Divider()

            HStack(spacing: 8) {
                TextField("Message", text: $promptText, axis: .vertical)
                    .textFieldStyle(.roundedBorder)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .lineLimit(1...5)
                    .focused($isFocused)
                    .disabled(!isConnected)

                if isRunning {
                    Button {
                        viewModel.cancel(tabId: tabId)
                    } label: {
                        Image(systemName: "stop.circle.fill")
                            .font(.title2)
                            .foregroundStyle(.red)
                            .frame(minWidth: 44, minHeight: 44)
                            .contentShape(Rectangle())
                    }
                }

                Button {
                    guard !promptText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
                    viewModel.sendPrompt(tabId: tabId, text: promptText)
                    isFocused = false
                    promptText = ""
                } label: {
                    Image(systemName: "arrow.up.circle.fill")
                        .font(.title2)
                        .foregroundStyle(sendButtonColor)
                        .frame(minWidth: 44, minHeight: 44)
                        .contentShape(Rectangle())
                }
                .disabled(promptText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !isConnected)
            }
            .padding(.horizontal)
            .padding(.vertical, 8)

            // Queue indicator
            if isQueued && !promptText.isEmpty {
                Text("Message will be queued")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .padding(.bottom, 4)
            }
        }
        .background(.ultraThinMaterial)
        .animation(.easeInOut(duration: 0.15), value: keyboardVisible)
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in
            keyboardVisible = true
        }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in
            keyboardVisible = false
        }
        .onChange(of: viewModel.pendingInputByTab[tabId]) { _, newValue in
            if let text = newValue {
                promptText = text
                viewModel.pendingInputByTab.removeValue(forKey: tabId)
            }
        }
    }

    private var sendButtonColor: Color {
        if !isConnected {
            return .gray
        }
        return isQueued ? .orange : JarvisTheme.accent
    }

    // MARK: - Attachments

    private func addFileAttachment(path: String, name: String) {
        let imageExts: Set = ["png", "jpg", "jpeg", "gif", "webp", "svg"]
        let ext = (name as NSString).pathExtension.lowercased()
        let type = imageExts.contains(ext) ? "image" : "file"
        let att = PendingAttachment(id: UUID().uuidString, type: type, name: name, path: path, isUploading: false)
        pendingAttachments.append(att)
    }

    private func handlePhotoSelection(_ item: PhotosPickerItem) {
        let placeholderId = UUID().uuidString
        let correlationId = UUID().uuidString
        let name = "photo-\(Int(Date().timeIntervalSince1970 * 1000)).jpeg"
        pendingAttachments.append(PendingAttachment(id: placeholderId, type: "image", name: name, path: "", isUploading: true, correlationId: correlationId))

        Task {
            guard let data = try? await item.loadTransferable(type: Data.self) else {
                await MainActor.run { pendingAttachments.removeAll { $0.id == placeholderId } }
                return
            }
            // Compress to JPEG, target ~1MB max
            let compressed = compressImage(data: data, maxBytes: 1_000_000)
            AttachmentImageCache.shared.store(data: compressed, forKey: placeholderId)
            let base64 = compressed.base64EncodedString()
            let dataUrl = "data:image/jpeg;base64,\(base64)"
            await MainActor.run {
                viewModel.uploadAttachment(dataUrl: dataUrl, name: name, correlationId: correlationId)
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
            let correlationId = UUID().uuidString
            let imageExts: Set = ["png", "jpg", "jpeg", "gif", "webp", "svg"]
            let ext = url.pathExtension.lowercased()
            let type = imageExts.contains(ext) ? "image" : "file"

            if type == "image" {
                // Upload image via data URL
                pendingAttachments.append(PendingAttachment(id: placeholderId, type: "image", name: name, path: "", isUploading: true, correlationId: correlationId))
                let compressed = compressImage(data: data, maxBytes: 1_000_000)
                AttachmentImageCache.shared.store(data: compressed, forKey: placeholderId)
                let base64 = compressed.base64EncodedString()
                let mimeExt = (ext == "jpg" || ext == "jpeg") ? "jpeg" : ext
                let dataUrl = "data:image/\(mimeExt);base64,\(base64)"
                viewModel.uploadAttachment(dataUrl: dataUrl, name: name, correlationId: correlationId)
            } else {
                // Upload non-image file via data URL so desktop can save it
                pendingAttachments.append(PendingAttachment(id: placeholderId, type: "file", name: name, path: "", isUploading: true, correlationId: correlationId))
                let base64 = data.base64EncodedString()
                let dataUrl = "data:application/octet-stream;base64,\(base64)"
                viewModel.uploadAttachment(dataUrl: dataUrl, name: name, correlationId: correlationId)
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
        var consumedIds = Set<String>()
        for result in results {
            guard let cid = result.correlationId, !cid.isEmpty else { continue }
            if let idx = pendingAttachments.firstIndex(where: { $0.isUploading && $0.correlationId == cid }) {
                consumedIds.insert(cid)
                if let error = result.error, !error.isEmpty {
                    pendingAttachments.remove(at: idx)
                } else {
                    AttachmentImageCache.shared.rekey(from: pendingAttachments[idx].id, to: result.id)
                    // Also key by the desktop-side path so rehydrated messages
                    // (which only know the path from the marker text) can find
                    // the same image bytes for inline rendering.
                    if pendingAttachments[idx].type == "image", !result.path.isEmpty,
                       let bytes = AttachmentImageCache.shared.data(forKey: result.id) {
                        AttachmentImageCache.shared.store(data: bytes, forKey: result.path)
                    }
                    pendingAttachments[idx] = PendingAttachment(
                        id: result.id, type: pendingAttachments[idx].type, name: result.name, path: result.path, isUploading: false, correlationId: cid
                    )
                }
            }
        }
        if !consumedIds.isEmpty {
            viewModel.pendingUploadResults.removeAll { r in
                guard let cid = r.correlationId else { return false }
                return consumedIds.contains(cid)
            }
        }
    }

    // MARK: - Slash commands

    private func updateSlashFilter(_ text: String) {
        let pattern = #"^\/[a-zA-Z0-9_:\-]*$"#
        if text.range(of: pattern, options: .regularExpression) != nil {
            slashFilter = text
        } else {
            slashFilter = nil
        }
    }

    private func fetchCommandsIfNeeded() {
        let dir = workingDirectory
        guard !dir.isEmpty, viewModel.discoveredCommands[dir] == nil else { return }
        viewModel.discoverCommands(directory: dir)
    }
}
