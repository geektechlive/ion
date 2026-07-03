import SwiftUI

// MARK: - StatusDrawerBreakdown
//
// Context-breakdown building blocks for StatusDrawerView's Context Breakdown
// section (plan minty-grinning-cocoa §§ C3–C5, C8). Extracted from
// StatusDrawerView.swift to keep each file under the 600-line cap while
// preserving the redesign's grouping, proportion-graph, and cache-annotation
// parity with desktop StatusDrawer.tsx.

// MARK: - BreakdownKind (fixed ordering, labels, colors)

/// Fixed kind ordering + display metadata for the context breakdown, mirroring
/// desktop StatusDrawer.tsx (KIND_ORDER / KIND_LABEL / KIND_COLOR).
enum BreakdownKind {
    /// Fixed display order for kind buckets.
    static let order: [String] = ["system_prompt", "tools", "conversation", "file", "unaccounted"]

    /// Normalize an engine kind value to a fixed bucket key.
    static func key(_ kind: String) -> String {
        switch kind {
        case "system_prompt", "system-prompt": return "system_prompt"
        case "tools", "tool":                   return "tools"
        case "conversation", "message":         return "conversation"
        case "file":                            return "file"
        default:                                return "unaccounted"
        }
    }

    static func label(_ key: String) -> String {
        switch key {
        case "system_prompt": return "System Prompt"
        case "tools":         return "Tools"
        case "conversation":  return "Conversation"
        case "file":          return "Files"
        default:              return "Unaccounted"
        }
    }

    static func color(_ key: String) -> Color {
        switch key {
        case "system_prompt": return Color(breakdownHex: 0x7c6af7)
        case "tools":         return Color(breakdownHex: 0x3b82f6)
        case "conversation":  return Color(breakdownHex: 0x22c55e)
        case "file":          return Color(breakdownHex: 0xf59e0b)
        default:              return Color(breakdownHex: 0x6b7280)
        }
    }
}

// MARK: - BreakdownGrouping (pure grouping + graph-segment logic)

/// Pure grouping / ordering logic for the context breakdown, extracted so it can
/// be unit-tested without a SwiftUI view. Mirrors desktop groupCategories.
enum BreakdownGrouping {
    struct Group {
        let kind: String
        let categories: [ContextBreakdownCategory]
        var total: Int { categories.reduce(0) { $0 + $1.tokens } }
    }

    struct Segment {
        let kind: String
        let tokens: Int
        let pct: Double
    }

    /// Group categories by kind (fixed order), sorting descending by tokens
    /// within each bucket. Only present buckets are returned.
    static func group(_ categories: [ContextBreakdownCategory]) -> [Group] {
        var buckets: [String: [ContextBreakdownCategory]] = [:]
        for cat in categories {
            buckets[BreakdownKind.key(cat.kind), default: []].append(cat)
        }
        var result: [Group] = []
        for kind in BreakdownKind.order {
            guard let items = buckets[kind] else { continue }
            let sorted = items.sorted { $0.tokens > $1.tokens }
            result.append(Group(kind: kind, categories: sorted))
        }
        return result
    }

    /// Compute proportion-graph segments (bucket total / contextWindow) in fixed
    /// kind order. Only buckets with pct > 0 are included.
    static func graphSegments(groups: [Group], contextWindow: Int) -> [Segment] {
        guard contextWindow > 0 else { return [] }
        return groups.compactMap { group in
            let pct = Double(group.total) / Double(contextWindow) * 100
            guard pct > 0 else { return nil }
            return Segment(kind: group.kind, tokens: group.total, pct: pct)
        }
    }
}

// MARK: - ProportionGraphView (segmented bar + legend)

struct ProportionGraphView: View {
    let segments: [BreakdownGrouping.Segment]
    let contextWindow: Int
    @Environment(\.appTheme) private var theme

    private var usedPct: Double { segments.reduce(0) { $0 + $1.pct } }
    private var freePct: Double { max(0, 100 - usedPct) }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            // Segmented bar.
            GeometryReader { geo in
                HStack(spacing: 0) {
                    ForEach(segments, id: \.kind) { seg in
                        BreakdownKind.color(seg.kind)
                            .frame(width: geo.size.width * seg.pct / 100)
                    }
                    if freePct > 0 {
                        theme.textSecondary.opacity(0.15)
                            .frame(width: geo.size.width * freePct / 100)
                    }
                }
            }
            .frame(height: 8)
            .clipShape(RoundedRectangle(cornerRadius: 4))

            // Legend dots.
            FlowLegend(segments: segments, showFree: freePct > 0.5)
        }
    }
}

/// Wrapping legend row for the proportion graph.
private struct FlowLegend: View {
    let segments: [BreakdownGrouping.Segment]
    let showFree: Bool
    @Environment(\.appTheme) private var theme

    var body: some View {
        // A simple wrapping HStack via a lazy grid keeps layout robust.
        let columns = [GridItem(.adaptive(minimum: 70), spacing: 8, alignment: .leading)]
        LazyVGrid(columns: columns, alignment: .leading, spacing: 2) {
            ForEach(segments, id: \.kind) { seg in
                HStack(spacing: 3) {
                    RoundedRectangle(cornerRadius: 1)
                        .fill(BreakdownKind.color(seg.kind))
                        .frame(width: 6, height: 6)
                    Text(BreakdownKind.label(seg.kind))
                        .font(.system(size: 9))
                        .foregroundStyle(theme.textSecondary)
                }
            }
            if showFree {
                HStack(spacing: 3) {
                    RoundedRectangle(cornerRadius: 1)
                        .fill(theme.textSecondary.opacity(0.2))
                        .frame(width: 6, height: 6)
                    Text("Free")
                        .font(.system(size: 9))
                        .foregroundStyle(theme.textSecondary.opacity(0.7))
                }
            }
        }
    }
}

// MARK: - BreakdownCategoryRow

struct BreakdownCategoryRow: View {
    let cat: ContextBreakdownCategory
    let contextWindow: Int
    var indent: Bool = false
    @Environment(\.appTheme) private var theme

    private var pct: Int {
        contextWindow > 0 ? Int(round(Double(cat.tokens) / Double(contextWindow) * 100)) : 0
    }

    /// Display label: for file rows show the last two path components.
    private var label: String {
        if let path = cat.path, !path.isEmpty {
            let parts = path.split(separator: "/")
            return parts.suffix(2).joined(separator: "/")
        }
        return cat.name
    }

    var body: some View {
        HStack(spacing: 4) {
            if indent {
                Text("↳")
                    .font(.system(size: 9))
                    .foregroundStyle(theme.textSecondary.opacity(0.5))
            }
            Text(label)
                .font(.caption)
                .foregroundStyle(theme.textSecondary)
                .lineLimit(1)
                .truncationMode(.middle)
            Spacer()
            Text(cat.tokens.formatted())
                .font(.caption.monospacedDigit())
                .foregroundStyle(theme.textPrimary)
            Text("\(pct)%")
                .font(.caption2.monospacedDigit())
                .foregroundStyle(theme.textSecondary)
                .frame(minWidth: 28, alignment: .trailing)
            TierBadge(tier: cat.tier)
        }
        .padding(.leading, indent ? 12 : 0)
    }
}

// MARK: - TierBadge

private struct TierBadge: View {
    let tier: String
    @Environment(\.appTheme) private var theme

    private var label: String {
        switch tier {
        case "exact": return "exact"
        case "local": return "bpe"
        default: return "~"
        }
    }

    private var badgeColor: Color {
        switch tier {
        case "exact": return theme.accent.opacity(0.85)
        case "local": return theme.textSecondary.opacity(0.6)
        default: return theme.textSecondary.opacity(0.4)
        }
    }

    var body: some View {
        Text(label)
            .font(.system(size: 9, weight: .semibold, design: .monospaced))
            .foregroundStyle(.white)
            .padding(.horizontal, 4)
            .padding(.vertical, 1)
            .background(badgeColor)
            .clipShape(RoundedRectangle(cornerRadius: 3))
    }
}


// MARK: - Color(breakdownHex:)

private extension Color {
    /// Construct an opaque Color from a 0xRRGGBB integer literal. Named
    /// distinctly from the app-wide `Color(hex:)` to avoid overload ambiguity.
    init(breakdownHex hex: UInt32) {
        let r = Double((hex >> 16) & 0xFF) / 255.0
        let g = Double((hex >> 8) & 0xFF) / 255.0
        let b = Double(hex & 0xFF) / 255.0
        self.init(.sRGB, red: r, green: g, blue: b, opacity: 1)
    }
}
