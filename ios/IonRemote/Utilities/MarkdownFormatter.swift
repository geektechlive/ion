import SwiftUI

/// A parsed markdown block produced by `MarkdownFormatter.parse`.
enum MarkdownBlock: Identifiable {
    case heading(level: Int, text: AttributedString)
    case paragraph(text: AttributedString)
    case code(language: String?, text: String)
    case blockQuote(text: AttributedString)
    case listItem(ordinal: Int, ordered: Bool, text: AttributedString)
    case thematicBreak(id: Int)
    case image(url: URL)

    var id: String {
        switch self {
        case .heading(let l, let t):
            return "h\(l)-\(String(t.characters).hashValue)"
        case .paragraph(let t):
            return "p-\(String(t.characters).hashValue)"
        case .code(_, let t):
            return "c-\(t.hashValue)"
        case .blockQuote(let t):
            return "bq-\(String(t.characters).hashValue)"
        case .listItem(let o, let ord, let t):
            let k = "li\(ord ? "o" : "u")\(o)"
            return "\(k)-\(String(t.characters).hashValue)"
        case .thematicBreak(let id):
            return "hr-\(id)"
        case .image(let url):
            return "img-\(url.absoluteString.hashValue)"
        }
    }
}

/// Parses Markdown into `[MarkdownBlock]` for rich composite rendering, or
/// into a single `AttributedString` for compact inline previews.
///
/// Uses Apple's `AttributedString` with `.full` interpretation and walks the
/// `presentationIntent` runs to classify blocks. No third-party dependency.
@MainActor
enum MarkdownFormatter {

    // MARK: - Rich block API (full-screen viewer)

    static func parse(_ markdown: String) -> [MarkdownBlock] {
        guard let parsed = try? AttributedString(
            markdown: markdown,
            options: .init(
                allowsExtendedAttributes: true,
                interpretedSyntax: .full,
                failurePolicy: .returnPartiallyParsedIfPossible
            )
        ) else {
            return [.paragraph(text: fallback(markdown))]
        }

        var blocks: [MarkdownBlock] = []
        var accum = AttributedString()
        var currentKind: BlockKind?

        for run in parsed.runs {
            let segment = AttributedString(parsed[run.range])
            let kind = classify(run.presentationIntent)

            if kind != currentKind {
                flush(currentKind, text: &accum, into: &blocks)
                currentKind = kind
            }
            accum.append(segment)
        }
        flush(currentKind, text: &accum, into: &blocks)
        return blocks.flatMap { expandImages($0) }
    }

    // MARK: - Compact single-string API (card preview)

    static func format(_ markdown: String) -> AttributedString {
        let blocks = parse(markdown)
        var result = AttributedString()
        for (i, block) in blocks.enumerated() {
            if i > 0 { result.append(AttributedString("\n")) }
            switch block {
            case .heading(_, let t):
                var h = t; h.font = .headline
                result.append(h)
            case .paragraph(let t):
                result.append(t)
            case .code(_, let t):
                var a = AttributedString(t)
                a.font = .system(.caption, design: .monospaced)
                a.foregroundColor = .secondary
                result.append(a)
            case .blockQuote(let t):
                var q = AttributedString("▎ "); q.foregroundColor = .secondary
                result.append(q); result.append(t)
            case .listItem(let o, let ord, let t):
                result.append(AttributedString(ord ? "\(o). " : "• "))
                result.append(t)
            case .thematicBreak(_):
                var hr = AttributedString("───")
                hr.foregroundColor = .secondary
                result.append(hr)
            case .image:
                var img = AttributedString("🖼 [image]")
                img.foregroundColor = .secondary
                result.append(img)
            }
        }
        return result
    }

    // MARK: - Image extraction

    /// Splits a `.paragraph` block by scanning for bare image URLs on their
    /// own line. Non-image lines are re-joined into paragraph blocks; image
    /// lines become `.image` blocks. All other block types pass through unchanged.
    private static func expandImages(_ block: MarkdownBlock) -> [MarkdownBlock] {
        guard case .paragraph(let attr) = block else { return [block] }
        let raw = String(attr.characters)
        let lines = raw.components(separatedBy: "\n")

        var result: [MarkdownBlock] = []
        var pendingLines: [String] = []

        for line in lines {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if let url = bareImageURL(trimmed) {
                if !pendingLines.isEmpty {
                    let joined = pendingLines.joined(separator: "\n")
                    pendingLines = []
                    let rebuilt = (try? AttributedString(
                        markdown: joined,
                        options: .init(
                            allowsExtendedAttributes: true,
                            interpretedSyntax: .inlineOnlyPreservingWhitespace,
                            failurePolicy: .returnPartiallyParsedIfPossible
                        )
                    )) ?? AttributedString(joined)
                    result.append(.paragraph(text: rebuilt))
                }
                result.append(.image(url: url))
            } else {
                pendingLines.append(line)
            }
        }

        if !pendingLines.isEmpty {
            let joined = pendingLines.joined(separator: "\n")
            let rebuilt = (try? AttributedString(
                markdown: joined,
                options: .init(
                    allowsExtendedAttributes: true,
                    interpretedSyntax: .inlineOnlyPreservingWhitespace,
                    failurePolicy: .returnPartiallyParsedIfPossible
                )
            )) ?? AttributedString(joined)
            result.append(.paragraph(text: rebuilt))
        }

        return result.isEmpty ? [block] : result
    }

    /// Returns a `URL` if `line` is a bare image URL (no spaces, starts with
    /// http/https, ends with a recognised image extension or extension + query).
    private static func bareImageURL(_ line: String) -> URL? {
        guard !line.isEmpty,
              !line.contains(" "),
              line.hasPrefix("https://")
        else { return nil }

        let extensions = ["png", "jpg", "jpeg", "gif", "webp", "svg"]
        let lowered = line.lowercased()

        // Strip query string for extension check
        let pathPart = lowered.components(separatedBy: "?").first ?? lowered
        guard extensions.contains(where: { pathPart.hasSuffix(".\($0)") }),
              let url = URL(string: line)
        else { return nil }

        return url
    }

    // MARK: - Block classification

    /// Internal block kind used to group consecutive runs that belong to
    /// the same logical block. Equality is by kind + identity so that
    /// two distinct paragraphs are flushed separately.
    private enum BlockKind: Equatable {
        case heading(Int, Int)      // level, identity
        case paragraph(Int)         // identity
        case code(String?, Int)     // language, identity
        case blockQuote(Int)        // identity
        case listItem(Int, Bool, Int) // ordinal, ordered, identity
        case thematicBreak(Int)     // identity
        case unknown
    }

    private static func classify(
        _ intent: PresentationIntent?
    ) -> BlockKind {
        guard let intent else { return .unknown }

        for component in intent.components {
            switch component.kind {
            case .header(let level):
                return .heading(level, component.identity)
            case .codeBlock(let lang):
                return .code(lang, component.identity)
            case .blockQuote:
                return .blockQuote(component.identity)
            case .thematicBreak:
                return .thematicBreak(component.identity)
            case .listItem(let ordinal):
                let ordered = intent.components.contains {
                    $0.kind == .orderedList
                }
                return .listItem(ordinal, ordered, component.identity)
            default:
                continue
            }
        }
        // Fall back to paragraph, keyed by the outermost identity.
        let pid = intent.components.last?.identity ?? 0
        return .paragraph(pid)
    }

    // MARK: - Flushing

    private static func flush(
        _ kind: BlockKind?,
        text: inout AttributedString,
        into blocks: inout [MarkdownBlock]
    ) {
        guard let kind, !text.characters.isEmpty else {
            text = AttributedString()
            return
        }
        let captured = text
        text = AttributedString()

        switch kind {
        case .heading(let level, _):
            blocks.append(.heading(level: level, text: captured))
        case .paragraph:
            blocks.append(.paragraph(text: captured))
        case .code(let lang, _):
            blocks.append(.code(language: lang, text: plain(captured)))
        case .blockQuote:
            blocks.append(.blockQuote(text: captured))
        case .listItem(let ordinal, let ordered, _):
            blocks.append(.listItem(
                ordinal: ordinal, ordered: ordered, text: captured
            ))
        case .thematicBreak(let identity):
            blocks.append(.thematicBreak(id: identity))
        case .unknown:
            blocks.append(.paragraph(text: captured))
        }
    }

    // MARK: - Helpers

    private static func plain(_ attr: AttributedString) -> String {
        String(attr.characters)
    }

    private static func fallback(
        _ markdown: String
    ) -> AttributedString {
        if let inline = try? AttributedString(
            markdown: markdown,
            options: .init(
                interpretedSyntax: .inlineOnlyPreservingWhitespace
            )
        ) {
            return inline
        }
        return AttributedString(markdown)
    }
}
