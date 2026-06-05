import Foundation

/// A slash command discovered from filesystem command/skill directories.
/// Mirrors the `DiscoveredCommand` interface in `types-events.ts`.
struct DiscoveredSlashCommand: Codable, Sendable, Identifiable {
    var id: String { name }
    let name: String
    let description: String
    let scope: String       // "user" | "project"
    let source: String      // "command" | "skill"
    /// Which directory family this command was discovered from.
    /// `"ion"` for ~/.ion/commands and {project}/.ion/commands.
    /// `"claude"` for ~/.claude/commands, {project}/.claude/commands,
    /// and ~/.claude/skills.
    ///
    /// The desktop server filters out `"claude"` entries before sending
    /// when the user has Claude Code Compatibility disabled, so iOS does
    /// not have to act on this field — it just decodes it. Marked
    /// optional to keep decoding resilient against older desktop builds
    /// that pre-date the field (the field was added alongside the
    /// claude-compat filter fix).
    let origin: String?     // "ion" | "claude"
}
