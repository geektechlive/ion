import XCTest
import CryptoKit
@testable import IonRemote

/// Tests for the wire gap-recovery retransmit path: the new wire types round
/// trip, and a forward seq gap records the missing range (capped) for resend.
final class TransportResendTests: XCTestCase {

    private func makeManager() -> TransportManager {
        TransportManager(
            relayURL: URL(string: "wss://relay.example.com")!,
            apiKey: "k",
            channelId: "chan",
            sharedKey: SymmetricKey(size: .bits256)
        )
    }

    // MARK: - Codec round-trips

    func testRequestResendCommandRoundTrips() throws {
        let cmd = RemoteCommand.requestResend(fromSeq: 7, toSeq: 12)
        let data = try JSONEncoder().encode(cmd)
        // Wire shape carries the desktop_ type and the seq range.
        let obj = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(obj["type"] as? String, "desktop_request_resend")
        XCTAssertEqual(obj["fromSeq"] as? UInt64, 7)
        XCTAssertEqual(obj["toSeq"] as? UInt64, 12)
        let decoded = try JSONDecoder().decode(RemoteCommand.self, from: data)
        guard case .requestResend(let from, let to) = decoded else { return XCTFail("wrong case") }
        XCTAssertEqual(from, 7)
        XCTAssertEqual(to, 12)
    }

    func testResendUnavailableEventDecodes() throws {
        let json = #"{"type":"desktop_resend_unavailable","fromSeq":42}"#.data(using: .utf8)!
        let event = try JSONDecoder().decode(RemoteEvent.self, from: json)
        guard case .resendUnavailable(let fromSeq) = event else { return XCTFail("wrong case") }
        XCTAssertEqual(fromSeq, 42)
    }

    // MARK: - Gap recording

    @MainActor
    func testForwardGapRecordsMissingRange() {
        let m = makeManager()
        m.requestResendForGap(fromSeq: 5, toSeq: 8)
        // The missing seqs [5,8] are tracked so replayed frames below
        // lastReceivedSeq are accepted rather than deduped away.
        XCTAssertEqual(m.pendingResendSeqs, [5, 6, 7, 8])
    }

    @MainActor
    func testHugeGapIsCappedToBoundedRange() {
        let m = makeManager()
        // A massive gap must not balloon the pending set — it is capped (256),
        // beyond which the snapshot reconcile heals.
        m.requestResendForGap(fromSeq: 1, toSeq: 100_000)
        XCTAssertEqual(m.pendingResendSeqs.count, 256)
        XCTAssertTrue(m.pendingResendSeqs.contains(1))
        XCTAssertTrue(m.pendingResendSeqs.contains(256))
        XCTAssertFalse(m.pendingResendSeqs.contains(257))
    }

    @MainActor
    func testResendUnavailableClearsPendingViaTransportPath() throws {
        let sharedKey = SymmetricKey(size: .bits256)
        let m = TransportManager(
            relayURL: URL(string: "wss://relay.example.com")!,
            apiKey: "k",
            channelId: "chan",
            sharedKey: sharedKey
        )
        // Seed a pending range exactly as the live gap-detection path does.
        m.requestResendForGap(fromSeq: 5, toSeq: 8)
        XCTAssertEqual(m.pendingResendSeqs, [5, 6, 7, 8], "precondition: gap recorded")

        // Build a real desktop_resend_unavailable event payload and encrypt it
        // the same way live frames arrive — so the test exercises the full
        // handleIncomingData path including decrypt, JSON decode, and the
        // pendingResendSeqs.removeAll() at TransportManager+Receive.swift:226-229.
        let json = #"{"type":"desktop_resend_unavailable","fromSeq":5}"#
        let plaintext = Data(json.utf8)
        let (nonce, ciphertext) = try E2ECrypto.encrypt(plaintext: plaintext, key: sharedKey)
        let wire = WireMessage(
            seq: 99,
            ts: nil,
            payload: nil,
            nonce: nonce.base64EncodedString(),
            ciphertext: ciphertext.base64EncodedString()
        )
        let wireData = try JSONEncoder().encode(wire)

        m.handleIncomingData(wireData, isRelay: false)

        // The handler at lines 226-229 must have run — pending set is cleared.
        XCTAssertTrue(m.pendingResendSeqs.isEmpty,
            "handleIncomingData must clear pendingResendSeqs on resendUnavailable")
    }
}
