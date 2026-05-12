import Foundation
import os

/// Thread-safe in-memory ring buffer for connection diagnostics.
///
/// Captures key lifecycle events (connect, auth, Bonjour, transport state
/// changes) so they can be inspected on-device or shared via AirDrop
/// without needing a USB cable or Xcode console.
///
/// Usage: `DiagnosticLog.log("connect: device=\(name)")`
final class DiagnosticLog: Sendable {

    static let shared = DiagnosticLog()

    /// Maximum entries before oldest are dropped.
    private static let maxEntries = 500

    private let lock = OSAllocatedUnfairLock(initialState: [Entry]())
    private let logger = Logger(subsystem: "com.sprague.ion.mobile", category: "diag")

    struct Entry: Sendable {
        let timestamp: Date
        let message: String
    }

    // MARK: - Public API

    /// Append a diagnostic message. Also echoes to os_log.
    static func log(_ message: String) {
        shared.append(message)
    }

    /// Return all current entries (oldest first).
    static func entries() -> [Entry] {
        shared.lock.withLock { $0 }
    }

    /// Clear all entries.
    static func clear() {
        shared.lock.withLock { $0.removeAll() }
    }

    /// Format all entries as a shareable plain-text string.
    static func exportText() -> String {
        let fmt = DateFormatter()
        fmt.dateFormat = "HH:mm:ss.SSS"
        return entries().map { "\(fmt.string(from: $0.timestamp)) \($0.message)" }
            .joined(separator: "\n")
    }

    // MARK: - Internal

    private func append(_ message: String) {
        logger.info("\(message, privacy: .public)")
        lock.withLock { state in
            state.append(Entry(timestamp: Date(), message: message))
            if state.count > Self.maxEntries {
                state.removeFirst(state.count - Self.maxEntries)
            }
        }
    }
}
