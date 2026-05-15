import SwiftUI
import UserNotifications
#if canImport(UIKit)
import UIKit

// MARK: - AppDelegate

class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        return true
    }

    func application(
        _ application: UIApplication,
        didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
    ) {
        let hex = deviceToken.map { String(format: "%02.2hhx", $0) }.joined()
        let previous = UserDefaults.standard.string(forKey: "apnsDeviceToken")
        UserDefaults.standard.set(hex, forKey: "apnsDeviceToken")
        print("[Ion] APNs device token registered: \(hex)")
        if previous != hex {
            NotificationCenter.default.post(name: .apnsTokenRefreshed, object: nil)
        }
    }

    func application(
        _ application: UIApplication,
        didFailToRegisterForRemoteNotificationsWithError error: Error
    ) {
        print("[Ion] APNs registration failed: \(error)")
    }

    // MARK: - UNUserNotificationCenterDelegate

    /// Foreground delivery: surface the briefing payload to the app and keep
    /// the system alert visible so the user still sees the banner.
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        Self.handleBriefingPayload(notification.request.content.userInfo)
        completionHandler([.banner, .list, .sound])
    }

    /// User tapped the notification: extract the briefing, hand it to the app,
    /// and signal the UI to open the Briefings sheet.
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        Self.handleBriefingPayload(response.notification.request.content.userInfo, openSheet: true)
        completionHandler()
    }

    private static func handleBriefingPayload(_ userInfo: [AnyHashable: Any], openSheet: Bool = false) {
        guard let briefingId = userInfo["briefingId"] as? String,
              let briefingText = userInfo["briefingText"] as? String else { return }
        let title = (userInfo["title"] as? String)
            ?? (((userInfo["aps"] as? [String: Any])?["alert"] as? [String: Any])?["title"] as? String)
            ?? "Briefing"
        let payload: [String: Any] = [
            "briefingId": briefingId,
            "title": title,
            "briefingText": briefingText,
            "openSheet": openSheet,
        ]
        DispatchQueue.main.async {
            NotificationCenter.default.post(name: .briefingFromPush, object: nil, userInfo: payload)
        }
    }
}
#endif

extension Notification.Name {
    static let apnsTokenRefreshed = Notification.Name("apnsTokenRefreshed")
    static let briefingFromPush = Notification.Name("briefingFromPush")
}

@main
struct IonRemoteApp: App {
    #if canImport(UIKit)
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate: AppDelegate
    #endif
    @State private var viewModel = SessionViewModel()
    @Environment(\.scenePhase) private var scenePhase
    @State private var didGoToBackground = false

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environment(viewModel)
                .environment(viewModel.briefingsStore)
                .preferredColorScheme(.dark)
                .onAppear {
                    UNUserNotificationCenter.current().getNotificationSettings { settings in
                        let already = settings.authorizationStatus == .authorized
                            || settings.authorizationStatus == .provisional
                        if already {
                            DispatchQueue.main.async {
                                #if canImport(UIKit)
                                UIApplication.shared.registerForRemoteNotifications()
                                #endif
                            }
                        }
                    }
                    UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { granted, _ in
                        if granted {
                            DispatchQueue.main.async {
                                #if canImport(UIKit)
                                UIApplication.shared.registerForRemoteNotifications()
                                #endif
                            }
                        }
                    }
                }
                .onChange(of: scenePhase) { _, newPhase in
                    viewModel.scenePhase = newPhase
                }
                .onChange(of: scenePhase) { _, newPhase in
                    switch newPhase {
                    case .active:
                        guard !viewModel.pairedDevices.isEmpty else { break }
                        if didGoToBackground {
                            didGoToBackground = false
                            // Returning from a true app switch (went through .background).
                            // disconnect() already fired; only reconnect if the background
                            // retry loop hasn't already started a new attempt.
                            if viewModel.connectionState == .disconnected {
                                viewModel.reconnect()
                            }
                        } else {
                            // Returning from screen lock (.inactive only, no .background).
                            // Reconnect on any non-connected state to recover silent relay drops.
                            // Guard didGoToBackground to prevent firing on cold launch
                            // (.inactive → .active with didGoToBackground == false).
                            if didGoToBackground && viewModel.connectionState != .connected {
                                viewModel.reconnect()
                            }
                        }
                    case .background:
                        didGoToBackground = true
                        viewModel.disconnect()
                    default:
                        break
                    }
                }
        }
    }
}

struct ContentView: View {
    @Environment(SessionViewModel.self) private var viewModel

    var body: some View {
        Group {
            if viewModel.pairedDevices.isEmpty || viewModel.connectionState == .authFailed {
                PairingView()
            } else if viewModel.connectionState == .disconnected || viewModel.connectionState == .connecting || viewModel.connectionState == .reconnecting {
                disconnectedView
            } else {
                TabListView()
            }
        }
        .onChange(of: viewModel.connectionState) { _, newState in
            if newState == .authFailed {
                viewModel.resetAll()
            }
        }
    }

    private var disconnectedView: some View {
        ZStack {
            JarvisTheme.background.ignoresSafeArea()
            ArcReactorBackground()
                .ignoresSafeArea()
                .opacity(0.6)
            VStack(spacing: 16) {
                Spacer()
                ConnectingSpinner()
                Text(viewModel.connectionState.label)
                    .font(.headline)
                    .foregroundStyle(JarvisTheme.textPrimary)
                    .shadow(color: JarvisTheme.accent.opacity(0.5), radius: 4)
                Text("Waiting for Jarvis...")
                    .font(.subheadline)
                    .foregroundStyle(JarvisTheme.textSecondary)
                Button("Retry") {
                    viewModel.reconnect()
                }
                .buttonStyle(.borderedProminent)
                .tint(JarvisTheme.accent)
                .padding(.top, 8)
                Spacer()
                Button("Unpair and Start Over", role: .destructive) {
                    viewModel.resetAll()
                }
                .font(.footnote)
                .padding(.bottom, 32)
            }
        }
        .task(id: viewModel.connectionState) {
            guard !viewModel.pairedDevices.isEmpty else { return }
            switch viewModel.connectionState {
            case .disconnected:
                // Retry every 5 seconds while fully disconnected.
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(5))
                    guard !Task.isCancelled,
                          viewModel.connectionState == .disconnected else { break }
                    viewModel.reconnect()
                }
            case .connecting, .reconnecting:
                // Break out of a stuck handshake after 15 seconds and keep retrying.
                // Loop because reconnect() transitions .connecting→.disconnected→.connecting
                // synchronously; SwiftUI may batch the change so .task(id:) never restarts.
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(15))
                    guard !Task.isCancelled,
                          viewModel.connectionState == .connecting
                              || viewModel.connectionState == .reconnecting else { return }
                    viewModel.reconnect()
                }
            default:
                break
            }
        }
    }
}

private struct ConnectingSpinner: View {
    @State private var rotation: Double = 0
    @State private var trimEnd: Double = 0.7

    var body: some View {
        ZStack {
            Circle()
                .stroke(JarvisTheme.accent.opacity(0.15), lineWidth: 2)
                .frame(width: 64, height: 64)
            Circle()
                .trim(from: 0, to: trimEnd)
                .stroke(
                    JarvisTheme.accent,
                    style: StrokeStyle(lineWidth: 2, lineCap: .round)
                )
                .frame(width: 64, height: 64)
                .rotationEffect(.degrees(rotation - 90))
                .shadow(color: JarvisTheme.accent.opacity(0.6), radius: 4)
        }
        .onAppear {
            withAnimation(.linear(duration: 1.2).repeatForever(autoreverses: false)) {
                rotation = 360
            }
            withAnimation(.easeInOut(duration: 1.8).repeatForever(autoreverses: true)) {
                trimEnd = 0.15
            }
        }
    }
}

#Preview("Connecting") {
    ContentView()
        .environment(SessionViewModel())
        .preferredColorScheme(.dark)
}
