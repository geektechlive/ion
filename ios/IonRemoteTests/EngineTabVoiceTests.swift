import XCTest
@testable import IonRemote

/// Regression coverage for engine-tab voice readback (Jarvis-specific TTS).
///
/// Engine conversations emit `tab_status:idle` at turn end but do NOT emit a
/// `task_complete` event, so `handleTaskComplete` never runs for them.
/// `handleTabStatus(.idle)` is therefore the only voice-readback trigger for
/// engine tabs. A merge conflict resolution (bcd9447) once dropped this
/// `speak()` call site and replaced it with a comment claiming voice fired in
/// `handleTaskComplete` — silently killing turn-end voice. These tests pin the
/// trigger so the regression cannot recur: remove the `speak()` call in
/// `handleTabStatus` and `testIdleSpeaksLastAssistantMessageOnce` goes red.
@MainActor
final class EngineTabVoiceTests: XCTestCase {

    /// Records `speak()` invocations without touching the network. Subclassing
    /// (rather than a protocol) keeps `SessionViewModel.voiceService` a concrete
    /// type so SwiftUI views retain their `@Observable` observation.
    final class VoiceSpy: VoiceService {
        private(set) var spokenTexts: [String] = []
        private(set) var spokenTabIds: [String?] = []

        override func speak(text: String, messageId: String? = nil, tabId: String? = nil) {
            spokenTexts.append(text)
            spokenTabIds.append(tabId)
        }
    }

    /// Build a `RemoteTabState` from wire JSON (mirrors the lifecycle tests),
    /// avoiding the verbose memberwise initializer.
    private func makeTab(id: String, status: String) throws -> RemoteTabState {
        let json = """
        {"id":"\(id)","title":"T","customTitle":null,"status":"\(status)","workingDirectory":"/tmp","permissionMode":"auto","permissionQueue":[],"lastMessage":null,"contextTokens":null}
        """.data(using: .utf8)!
        return try JSONDecoder().decode(RemoteTabState.self, from: json)
    }

    private func wireEngineTab(
        _ vm: SessionViewModel,
        tabId: String,
        messages: [Message],
        status: String = "running"
    ) throws {
        vm.tabs = [try makeTab(id: tabId, status: status)]
        vm.conversationInstances[tabId] = [
            ConversationInstanceInfo(id: "i1", label: "main", messages: messages),
        ]
        vm.activeEngineInstance[tabId] = "i1"
    }

    private func assistant(_ content: String, id: String = "m1") -> Message {
        Message(id: id, role: .assistant, content: content, timestamp: 0)
    }

    // MARK: - Tests

    func testIdleSpeaksLastAssistantMessageOnce() throws {
        let vm = SessionViewModel()
        let spy = VoiceSpy()
        vm.voiceService = spy

        try wireEngineTab(vm, tabId: "t1", messages: [assistant("The build passed and I deployed it.")])

        vm.handleTabStatus(tabId: "t1", status: .idle)

        XCTAssertEqual(spy.spokenTexts, ["The build passed and I deployed it."])
        XCTAssertEqual(spy.spokenTabIds, ["t1"])
    }

    func testRepeatedIdleDoesNotRespeak() throws {
        let vm = SessionViewModel()
        let spy = VoiceSpy()
        vm.voiceService = spy

        try wireEngineTab(vm, tabId: "t1", messages: [assistant("Deployed.")])

        vm.handleTabStatus(tabId: "t1", status: .idle)
        // A reconnect / upstream re-delivery sends idle again with no new message.
        vm.handleTabStatus(tabId: "t1", status: .idle)

        XCTAssertEqual(spy.spokenTexts.count, 1, "repeated idle must not re-speak the same response")
    }

    func testRunningStatusDoesNotSpeak() throws {
        let vm = SessionViewModel()
        let spy = VoiceSpy()
        vm.voiceService = spy

        try wireEngineTab(vm, tabId: "t1", messages: [assistant("In progress.")], status: "running")

        vm.handleTabStatus(tabId: "t1", status: .running)

        XCTAssertTrue(spy.spokenTexts.isEmpty, "a running turn must not trigger voice readback")
    }

    func testIdleWithNoAssistantMessageDoesNotSpeak() throws {
        let vm = SessionViewModel()
        let spy = VoiceSpy()
        vm.voiceService = spy

        let userOnly = Message(id: "u1", role: .user, content: "do the thing", timestamp: 0)
        try wireEngineTab(vm, tabId: "t1", messages: [userOnly])

        vm.handleTabStatus(tabId: "t1", status: .idle)

        XCTAssertTrue(spy.spokenTexts.isEmpty, "no assistant response means nothing to speak")
    }
}
