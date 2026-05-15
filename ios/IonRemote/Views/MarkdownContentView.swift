import SwiftUI

/// Renders an array of `MarkdownBlock` values with GitHub-inspired styling.
/// Each block becomes its own SwiftUI view, enabling backgrounds on code blocks,
/// dividers under headers, accent bars on blockquotes, and proper list indentation.
private struct ImageItem: Identifiable, Sendable {
    let url: URL
    var id: String { url.absoluteString }
}

struct MarkdownContentView: View {
    let blocks: [MarkdownBlock]
    @State private var fullscreenImage: ImageItem?

    var body: some View {
        LazyVStack(alignment: .leading, spacing: 14) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                blockView(block)
            }
        }
        .sheet(item: $fullscreenImage) { item in
            FullscreenImageView(url: item.url)
        }
    }

    // MARK: - Block dispatch

    @ViewBuilder
    private func blockView(_ block: MarkdownBlock) -> some View {
        switch block {
        case .heading(let level, let text):
            headingView(level: level, text: text)
        case .paragraph(let text):
            paragraphView(text: text)
        case .code(let language, let code):
            codeBlockView(language: language, code: code)
        case .blockQuote(let text):
            blockQuoteView(text: text)
        case .listItem(let ordinal, let ordered, let text):
            listItemView(ordinal: ordinal, ordered: ordered, text: text)
        case .thematicBreak(_):
            thematicBreakView
        case .image(let url):
            imageView(url: url)
        }
    }

    // MARK: - Heading

    private func headingView(
        level: Int,
        text: AttributedString
    ) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(text)
                .font(headingFont(level))
                .fixedSize(horizontal: false, vertical: true)

            if level <= 2 {
                Divider()
            }
        }
    }

    private func headingFont(_ level: Int) -> Font {
        switch level {
        case 1: .title.bold()
        case 2: .title2.bold()
        case 3: .title3.bold()
        default: .headline.bold()
        }
    }

    // MARK: - Paragraph

    private func paragraphView(text: AttributedString) -> some View {
        Text(text)
            .fixedSize(horizontal: false, vertical: true)
    }

    // MARK: - Code block

    private func codeBlockView(
        language: String?,
        code: String
    ) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            if let lang = language, !lang.isEmpty {
                Text(lang)
                    .font(.caption2.monospaced())
                    .foregroundStyle(.tertiary)
                    .padding(.horizontal, 12)
                    .padding(.top, 10)
                    .padding(.bottom, 4)
            }

            ScrollView(.horizontal, showsIndicators: false) {
                Text(code)
                    .font(.system(.callout, design: .monospaced))
                    .textSelection(.enabled)
                    .padding(.horizontal, 12)
                    .padding(.vertical, language != nil ? 6 : 10)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.tertiarySystemFill))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    // MARK: - Block quote

    private func blockQuoteView(text: AttributedString) -> some View {
        HStack(alignment: .top, spacing: 0) {
            RoundedRectangle(cornerRadius: 1.5)
                .fill(Color(hex: 0x4ECDC4).opacity(0.6))
                .frame(width: 3)

            Text(text)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.leading, 10)
        }
        .padding(.vertical, 4)
    }

    // MARK: - List item

    private func listItemView(
        ordinal: Int,
        ordered: Bool,
        text: AttributedString
    ) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 0) {
            Text(ordered ? "\(ordinal)." : "•")
                .monospacedDigit()
                .frame(width: 24, alignment: .trailing)
                .foregroundStyle(.secondary)

            Text(text)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.leading, 6)
        }
    }

    // MARK: - Thematic break

    private var thematicBreakView: some View {
        Divider()
            .padding(.vertical, 4)
    }

    // MARK: - Image

    private func imageView(url: URL) -> some View {
        AsyncImage(url: url) { phase in
            switch phase {
            case .empty:
                ZStack {
                    RoundedRectangle(cornerRadius: 8)
                        .fill(Color(.tertiarySystemFill))
                        .frame(maxWidth: .infinity)
                        .frame(height: 120)
                    ProgressView()
                }
            case .success(let image):
                image
                    .resizable()
                    .scaledToFit()
                    .frame(maxWidth: .infinity)
                    .frame(maxHeight: 300)
                    .clipShape(RoundedRectangle(cornerRadius: 8))
                    .onTapGesture { fullscreenImage = ImageItem(url: url) }
            case .failure:
                ZStack {
                    RoundedRectangle(cornerRadius: 8)
                        .fill(Color(.tertiarySystemFill))
                        .frame(maxWidth: .infinity)
                        .frame(height: 80)
                    Label("Image unavailable", systemImage: "photo.slash")
                        .foregroundStyle(.secondary)
                        .font(.caption)
                }
            @unknown default:
                EmptyView()
            }
        }
    }
}

private struct FullscreenImageView: View {
    let url: URL
    @Environment(\.dismiss) private var dismiss
    @State private var scale: CGFloat = 1.0
    @GestureState private var magnifyBy: CGFloat = 1.0

    var body: some View {
        NavigationStack {
            AsyncImage(url: url) { phase in
                switch phase {
                case .success(let image):
                    image
                        .resizable()
                        .scaledToFit()
                        .scaleEffect(scale * magnifyBy)
                        .gesture(
                            MagnificationGesture()
                                .updating($magnifyBy) { value, state, _ in state = value }
                                .onEnded { value in
                                    scale = max(1.0, min(scale * value, 5.0))
                                }
                        )
                        .onTapGesture(count: 2) { scale = 1.0 }
                case .empty:
                    ProgressView()
                case .failure:
                    Label("Image unavailable", systemImage: "photo.slash")
                        .foregroundStyle(.secondary)
                @unknown default:
                    EmptyView()
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.black)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Done") { dismiss() }
                        .foregroundStyle(.white)
                }
            }
            .toolbarBackground(Color.black, for: .navigationBar)
            .toolbarColorScheme(.dark, for: .navigationBar)
        }
    }
}
