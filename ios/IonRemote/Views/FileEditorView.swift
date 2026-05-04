import SwiftUI

/// Full-screen file editor with monospace TextEditor, save button, and dirty state tracking.
/// Pushed onto the NavigationStack from FileExplorerView when a file is tapped.
struct FileEditorView: View {
    @Environment(SessionViewModel.self) private var viewModel
    @Environment(\.dismiss) private var dismiss

    let filePath: String
    let fileName: String

    @State private var editedContent: String = ""
    @State private var originalContent: String = ""
    @State private var isLoaded = false
    @State private var isReadOnly = false
    @State private var showUnsavedAlert = false
    @State private var saveMessage: String?

    private var isDirty: Bool {
        isLoaded && editedContent != originalContent
    }

    private var isLoading: Bool {
        viewModel.fileContentLoading.contains(filePath)
    }

    private var fileError: String? {
        viewModel.fileContent[filePath]?.error
    }

    var body: some View {
        contentView
            .navigationTitle(fileName)
            .navigationBarTitleDisplayMode(.inline)
            .navigationBarBackButtonHidden(isDirty)
            .toolbar { toolbarContent }
            .task { viewModel.requestFsReadFile(filePath: filePath) }
            .onChange(of: viewModel.fileContent[filePath]?.content) { _, newContent in
                handleContentLoaded(newContent)
            }
            .onChange(of: viewModel.fileWriteResult) { _, result in
                handleWriteResult(result)
            }
            .alert("Unsaved Changes", isPresented: $showUnsavedAlert) {
                Button("Discard", role: .destructive) { dismiss() }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("You have unsaved changes. Discard them?")
            }
    }

    // MARK: - Content

    @ViewBuilder
    private var contentView: some View {
        if isLoading && !isLoaded {
            ProgressView("Loading file…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if let error = fileError {
            errorView(error)
        } else if isLoaded {
            editorView
        } else {
            Color.clear
        }
    }

    // MARK: - Toolbar

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        if isDirty {
            ToolbarItem(placement: .topBarLeading) {
                Button("Cancel") { showUnsavedAlert = true }
            }
        }
        ToolbarItem(placement: .topBarTrailing) {
            HStack(spacing: 12) {
                Button {
                    isReadOnly.toggle()
                } label: {
                    Image(systemName: isReadOnly ? "lock.fill" : "lock.open")
                        .font(.subheadline)
                        .foregroundStyle(isReadOnly ? .orange : .secondary)
                }

                Button {
                    save()
                } label: {
                    Text("Save")
                        .fontWeight(.semibold)
                        .foregroundStyle(isDirty ? Color(hex: 0x2EB8A6) : .secondary)
                }
                .disabled(!isDirty)
            }
        }
    }

    // MARK: - Editor View

    private var editorView: some View {
        ZStack(alignment: .topTrailing) {
            TextEditor(text: isReadOnly ? .constant(editedContent) : $editedContent)
                .font(.system(.body, design: .monospaced))
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .scrollContentBackground(.hidden)
                .background(Color(.systemBackground))
                .disabled(isReadOnly)

            if let msg = saveMessage {
                saveMessageBadge(msg)
            }
        }
    }

    private func saveMessageBadge(_ msg: String) -> some View {
        Text(msg)
            .font(.caption.weight(.medium))
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(msg == "Saved" ? Color.green.opacity(0.15) : Color.red.opacity(0.15))
            .foregroundStyle(msg == "Saved" ? .green : .red)
            .clipShape(Capsule())
            .padding(8)
    }

    // MARK: - Error View

    private func errorView(_ message: String) -> some View {
        VStack(spacing: 12) {
            Image(systemName: "exclamationmark.triangle")
                .font(.largeTitle)
                .foregroundStyle(.secondary)
            Text(message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }

    // MARK: - Actions

    private func save() {
        guard isDirty else { return }
        saveMessage = nil
        viewModel.requestFsWriteFile(filePath: filePath, content: editedContent)
    }

    private func handleContentLoaded(_ newContent: String?) {
        if let content = newContent, !isLoaded {
            originalContent = content
            editedContent = content
            isLoaded = true
        }
    }

    private func handleWriteResult(_ result: FsWriteResultResponse?) {
        guard let result, result.filePath == filePath else { return }
        if result.ok {
            originalContent = editedContent
            saveMessage = "Saved"
            Task { @MainActor in
                try? await Task.sleep(for: .seconds(2))
                if saveMessage == "Saved" { saveMessage = nil }
            }
        } else {
            saveMessage = result.error ?? "Save failed"
        }
    }
}
