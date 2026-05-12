import Foundation
import Observation

// MARK: - LANClient

/// WebSocket client for direct LAN connection to an Ion desktop instance.
///
/// Connects to `ws://{host}:{port}` on the local network. No authentication
/// header is needed; the LAN connection is trusted after pairing, and E2E
/// encryption provides message-level auth.
@Observable
final class LANClient {

    // MARK: - Public state

    private(set) var isConnected = false

    // MARK: - Internals

    private var task: URLSessionWebSocketTask?
    private var session: URLSession?
    private var intentionallyClosed = false

    /// Monotonically increasing connection generation. Each `connect()` call
    /// bumps this so that stale `receiveLoop` callbacks from a previous
    /// URLSessionWebSocketTask can detect they belong to a superseded
    /// connection and avoid finishing the current continuation.
    private var connectionGen: UInt64 = 0

    /// Continuation for the current connection's message stream.
    /// Replaced on each `connect()` so old iterators terminate cleanly.
    private var messageContinuation: AsyncStream<Data>.Continuation?
    /// Message stream for the current connection. Recreated on each
    /// `connect()` call so stale `for await` loops from a previous
    /// connection end immediately.
    private(set) var messages: AsyncStream<Data>

    // MARK: - Init

    init() {
        // Placeholder stream -- replaced on first connect().
        var continuation: AsyncStream<Data>.Continuation!
        self.messages = AsyncStream { continuation = $0 }
        self.messageContinuation = continuation
    }

    deinit {
        messageContinuation?.finish()
        intentionallyClosed = true
        task?.cancel(with: .goingAway, reason: nil)
        session?.invalidateAndCancel()
    }

    // MARK: - Public API

    /// Connect to an Ion instance at the given host and port.
    func connect(host: String, port: UInt16) async {
        intentionallyClosed = false

        // Finish old stream so any `for await` on the previous connection ends.
        messageContinuation?.finish()

        task?.cancel(with: .goingAway, reason: nil)
        task = nil
        session?.invalidateAndCancel()
        session = nil

        // Bump generation BEFORE creating the new stream so any in-flight
        // callback from the old task sees a stale gen and bails out.
        connectionGen &+= 1
        let gen = connectionGen
        DiagnosticLog.log("LAN-WS: connect gen=\(gen) \(host):\(port)")

        // Create a fresh stream for this connection.
        var continuation: AsyncStream<Data>.Continuation!
        self.messages = AsyncStream { continuation = $0 }
        self.messageContinuation = continuation

        guard let url = URL(string: "ws://\(host):\(port)") else { return }

        let urlSession = URLSession(configuration: .default)
        self.session = urlSession
        let wsTask = urlSession.webSocketTask(with: url)
        self.task = wsTask

        wsTask.resume()
        receiveLoop(wsTask, gen: gen)
    }

    func disconnect() {
        DiagnosticLog.log("LAN-WS: disconnect gen=\(connectionGen)")
        intentionallyClosed = true
        messageContinuation?.finish()
        task?.cancel(with: .normalClosure, reason: nil)
        task = nil
        session?.invalidateAndCancel()
        session = nil
        isConnected = false
    }

    func send(data: Data) async throws {
        guard let task, task.state == .running else {
            throw LANClientError.notConnected
        }
        try await task.send(.data(data))
    }

    // MARK: - Receive loop

    private func receiveLoop(_ wsTask: URLSessionWebSocketTask, gen: UInt64) {
        wsTask.receive { [weak self] result in
            guard let self else { return }

            // If connect() was called again, this callback belongs to a
            // superseded connection — drop it silently. Without this guard
            // the old task's cancellation-failure fires handleDisconnect()
            // which finishes the NEW connection's continuation, killing the
            // listener stream ~100ms after auth.
            guard gen == self.connectionGen else {
                DiagnosticLog.log("LAN-WS: stale recv gen=\(gen) cur=\(self.connectionGen)")
                return
            }

            switch result {
            case .success(let message):
                if !self.isConnected {
                    self.isConnected = true
                    DiagnosticLog.log("LAN-WS: first msg, isConnected=true gen=\(gen)")
                }
                switch message {
                case .data(let data):
                    self.messageContinuation?.yield(data)
                case .string(let text):
                    if let data = text.data(using: .utf8) {
                        self.messageContinuation?.yield(data)
                    }
                @unknown default:
                    break
                }
                self.receiveLoop(wsTask, gen: gen)

            case .failure(let error):
                DiagnosticLog.log("LAN-WS: recv failure gen=\(gen) err=\(error.localizedDescription)")
                self.handleDisconnect(gen: gen)
            }
        }
    }

    private func handleDisconnect(gen: UInt64) {
        // Only act if this disconnect belongs to the current connection.
        // A stale receiveLoop from a previous connect() must not touch
        // the new connection's state or continuation.
        guard gen == connectionGen else {
            DiagnosticLog.log("LAN-WS: stale disconnect gen=\(gen) cur=\(connectionGen)")
            return
        }
        DiagnosticLog.log("LAN-WS: handleDisconnect gen=\(gen)")
        isConnected = false
        task = nil
        session?.invalidateAndCancel()
        session = nil
        // Finish the stream so any `for await` (auth, listener) exits.
        messageContinuation?.finish()
        // LAN client does not auto-reconnect. TransportManager handles
        // reconnection by monitoring Bonjour discovery state.
    }
}

// MARK: - Errors

enum LANClientError: Error, LocalizedError {
    case notConnected

    var errorDescription: String? {
        switch self {
        case .notConnected:
            return "LAN client is not connected"
        }
    }
}
