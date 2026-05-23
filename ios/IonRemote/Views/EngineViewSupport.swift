import SwiftUI

// MARK: - GroupedItem
//
// Identifiable wrapper used by EngineView to group consecutive tool messages
// into a single collapsible row and coalesce bootstrap harness messages.

enum GroupedItem: Identifiable {
    case single(EngineMessage)
    case toolGroup([EngineMessage])

    var id: String {
        switch self {
        case .single(let msg): return msg.id
        case .toolGroup(let msgs): return "tg-\(msgs.first?.id ?? "")"
        }
    }
}
