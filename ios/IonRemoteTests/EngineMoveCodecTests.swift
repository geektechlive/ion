import XCTest
@testable import IonRemote

/// Codec tests for the engine_instance_moved event.
///
/// The `engine_move_instance` *command* was removed in #256 (single-instance
/// collapse) — iOS no longer emits it. The `engine_instance_moved` *event* is
/// still decoded (the no-op handler in SessionViewModel+EventHandlers.swift
/// logs and ignores it for backward compatibility with legacy desktops), so
/// its codec stays pinned here. Mirrors the style of
/// NormalizedEventStreamTests.swift — pure encode/decode, no network or
/// MainActor required.
final class EngineMoveCodecTests: XCTestCase {
    private let decoder = JSONDecoder()
    private let encoder = JSONEncoder()

    // MARK: - RemoteEvent decode

    func testDecodeEngineInstanceMovedEvent() throws {
        let json = """
        {
            "type": "desktop_instance_moved",
            "sourceTabId": "tab-a",
            "instanceId": "inst-1",
            "targetTabId": "tab-b"
        }
        """.data(using: .utf8)!

        let event = try decoder.decode(RemoteEvent.self, from: json)

        if case .engineInstanceMoved(let sourceTabId, let instanceId, let targetTabId) = event {
            XCTAssertEqual(sourceTabId, "tab-a")
            XCTAssertEqual(instanceId, "inst-1")
            XCTAssertEqual(targetTabId, "tab-b")
        } else {
            XCTFail("Expected engineInstanceMoved, got \(event)")
        }
    }

    // MARK: - RemoteEvent encode

    func testEncodeEngineInstanceMovedEvent() throws {
        let event = RemoteEvent.engineInstanceMoved(
            sourceTabId: "tab-x",
            instanceId: "inst-9",
            targetTabId: "tab-y"
        )
        let data = try encoder.encode(event)
        let json = try JSONSerialization.jsonObject(with: data) as! [String: Any]

        XCTAssertEqual(json["type"] as? String, "desktop_instance_moved")
        XCTAssertEqual(json["sourceTabId"] as? String, "tab-x")
        XCTAssertEqual(json["instanceId"] as? String, "inst-9")
        XCTAssertEqual(json["targetTabId"] as? String, "tab-y")
    }

    // MARK: - RemoteEvent round-trip

    func testRoundTripEngineInstanceMovedEvent() throws {
        let original = RemoteEvent.engineInstanceMoved(
            sourceTabId: "round-src",
            instanceId: "round-inst",
            targetTabId: "round-dst"
        )
        let data = try encoder.encode(original)
        let decoded = try decoder.decode(RemoteEvent.self, from: data)

        if case .engineInstanceMoved(let sourceTabId, let instanceId, let targetTabId) = decoded {
            XCTAssertEqual(sourceTabId, "round-src")
            XCTAssertEqual(instanceId, "round-inst")
            XCTAssertEqual(targetTabId, "round-dst")
        } else {
            XCTFail("Round-trip engineInstanceMoved failed, got \(decoded)")
        }
    }
}
