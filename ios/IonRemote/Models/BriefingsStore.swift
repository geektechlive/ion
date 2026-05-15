import Foundation
import Observation

@Observable
final class BriefingsStore {
    private(set) var briefings: [BriefingItem] = []

    var unreadCount: Int { briefings.filter { !$0.isRead }.count }

    private static let key = "jarvis.briefings.v1"
    private static let maxCount = 14

    init() { load() }

    func receive(briefingId: String, title: String, text: String) {
        let today = Calendar.current.startOfDay(for: Date())
        // One entry per briefingId per calendar day — deduplicates retries.
        if briefings.first(where: {
            $0.briefingId == briefingId && Calendar.current.startOfDay(for: $0.receivedAt) == today
        }) != nil { return }
        let item = BriefingItem(id: UUID(), briefingId: briefingId, title: title, text: text, receivedAt: Date())
        briefings.insert(item, at: 0)
        if briefings.count > Self.maxCount { briefings = Array(briefings.prefix(Self.maxCount)) }
        save()
    }

    func markRead(id: UUID) {
        guard let idx = briefings.firstIndex(where: { $0.id == id }) else { return }
        briefings[idx].isRead = true
        save()
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
