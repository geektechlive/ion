import Foundation

struct BriefingItem: Codable, Identifiable, Sendable {
    let id: UUID
    let briefingId: String   // "morning_brief" | "midday_checkin"
    let title: String
    let text: String
    let receivedAt: Date
    var isRead: Bool = false
}
