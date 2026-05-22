import SwiftUI

/// Full-screen view for reading a plan's markdown content.
/// Presented via `.fullScreenCover` from `PlanApprovalCardView`.
/// Uses the composite `MarkdownContentView` for GitHub-style block rendering.
struct PlanFullScreenView: View {
    let content: String
    var onImplement: (() -> Void)? = nil
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                MarkdownContentView(
                    blocks: MarkdownFormatter.parse(content)
                )
                .textSelection(.enabled)
                .padding()
            }
            .navigationTitle("Plan")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button("Done") {
                        dismiss()
                    }
                }
                if onImplement != nil {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button("Implement") {
                            onImplement?()
                        }
                        .fontWeight(.semibold)
                        .tint(.green)
                    }
                }
            }
        }
    }
}
