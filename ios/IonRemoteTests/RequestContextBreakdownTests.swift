import XCTest
@testable import IonRemote

// MARK: - RequestContextBreakdownTests
//
// Tests for:
//   1. desktop_request_context_breakdown TypeKey decode (§10 wire).
//   2. RemoteCommand round-trip: encode → decode preserves tabId.
//   3. Session ID full-length: StatusDrawerView no longer truncates to 8 chars.
//
// Plan: minty-grinning-cocoa §§ 10, 11.
//
// Run with:
//   cd ios && xcodebuild test -project IonRemote.xcodeproj -scheme IonRemote \
//     -destination 'platform=iOS Simulator,name=iPhone 15' \
//     -only-testing IonRemoteTests/RequestContextBreakdownTests

final class RequestContextBreakdownTests: XCTestCase {

    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()

    private func jsonObject(from command: RemoteCommand) throws -> [String: Any] {
        let data = try encoder.encode(command)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    // MARK: - 1. TypeKey: requestContextBreakdown has correct raw value

    func test_requestContextBreakdown_typeKeyRawValue() {
        XCTAssertEqual(
            RemoteCommand.TypeKey.requestContextBreakdown.rawValue,
            "desktop_request_context_breakdown",
            "TypeKey raw value must match the wire string expected by the desktop"
        )
    }

    // MARK: - 2. Encode: requestContextBreakdown produces correct JSON

    func test_requestContextBreakdown_encodesToCorrectJSON() throws {
        let cmd = RemoteCommand.requestContextBreakdown(tabId: "tab-xyz")
        let json = try jsonObject(from: cmd)

        XCTAssertEqual(json["type"] as? String, "desktop_request_context_breakdown")
        XCTAssertEqual(json["tabId"] as? String, "tab-xyz")
    }

    // MARK: - 3. Decode: desktop_request_context_breakdown round-trips

    func test_requestContextBreakdown_decodeRoundTrip() throws {
        let original = RemoteCommand.requestContextBreakdown(tabId: "tab-round-trip")
        let data = try encoder.encode(original)
        let decoded = try decoder.decode(RemoteCommand.self, from: data)

        guard case .requestContextBreakdown(let tabId) = decoded else {
            XCTFail("Expected .requestContextBreakdown after round-trip, got: \(decoded)")
            return
        }
        XCTAssertEqual(tabId, "tab-round-trip")
    }

    // MARK: - 4. Session ID: StatusDrawerView does not truncate to 8 chars (§11)

    func test_sessionId_notTruncatedTo8Chars() throws {
        // Read the StatusDrawerView source and assert the .prefix(8) truncation
        // was removed in §11. The full ID is shown; CSS overflow (lineLimit +
        // truncationMode(.middle)) handles layout overflow.
        let fileURL = Bundle(for: type(of: self)).resourceURL?
            .deletingLastPathComponent()
            .deletingLastPathComponent()
        // The source file is not embedded in the test bundle; use a relative path
        // from the test file's location via __file.
        let sourceURL = URL(fileURLWithPath: #file)
            .deletingLastPathComponent()   // IonRemoteTests/
            .deletingLastPathComponent()   // ios/
            .appendingPathComponent("IonRemote/Views/StatusDrawerView.swift")

        let source = try String(contentsOf: sourceURL, encoding: .utf8)

        // §11: the old truncation expression must not appear.
        XCTAssertFalse(
            source.contains("id.prefix(8)"),
            "StatusDrawerView must not truncate session ID to 8 chars (§11 fix)"
        )
        // The full `id` must be passed to Text() directly.
        XCTAssertTrue(
            source.contains("Text(id)"),
            "StatusDrawerView must render Text(id) with full session ID"
        )
    }
}
