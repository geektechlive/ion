import Foundation
import Observation

@Observable
final class BriefingsStore {
    private(set) var briefings: [BriefingItem] = []

    var unreadCount: Int { briefings.filter { !$0.isRead }.count }

    private static let key = "jarvis.briefings.v1"
    private static let maxCount = 14
    private static let maxAgeDays = 5

    init() { load(); purgeExpired() }

    func receive(briefingId: String, title: String, text: String) {
        purgeExpired()
        let today = Calendar.current.startOfDay(for: Date())
        // One entry per briefingId per calendar day — deduplicates retries.
        let truncatedMarker = "…[truncated — open Briefings for full text]"
        if let idx = briefings.firstIndex(where: {
            $0.briefingId == briefingId && Calendar.current.startOfDay(for: $0.receivedAt) == today
        }) {
            if briefings[idx].text.hasSuffix(truncatedMarker) {
                briefings[idx].text = text
                briefings[idx].isRead = false
                save()
            }
            return
        }
        let item = BriefingItem(id: UUID(), briefingId: briefingId, title: title, text: text, receivedAt: Date())
        briefings.insert(item, at: 0)
        if briefings.count > Self.maxCount { briefings = Array(briefings.prefix(Self.maxCount)) }
        save()
    }

    func delete(id: UUID) {
        briefings.removeAll { $0.id == id }
        save()
    }

    func markRead(id: UUID) {
        guard let idx = briefings.firstIndex(where: { $0.id == id }) else { return }
        briefings[idx].isRead = true
        save()
    }

    private func purgeExpired() {
        let cutoff = Calendar.current.date(byAdding: .day, value: -Self.maxAgeDays, to: Date()) ?? .distantPast
        let before = briefings.count
        briefings.removeAll { $0.receivedAt < cutoff }
        if briefings.count != before { save() }
    }

    private func load() {
        guard let data = UserDefaults.standard.data(forKey: Self.key),
              let decoded = try? JSONDecoder().decode([BriefingItem].self, from: data)
        else { return }
        briefings = decoded
    }

    private func save() {
        guard let data = try? JSONEncoder().encode(briefings) else { return }
        UserDefaults.standard.set(data, forKey: Self.key)
    }
}
