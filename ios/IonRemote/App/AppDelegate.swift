import UIKit
import UserNotifications

/// Handles push notification registration and delivery.
///
/// This is intentionally minimal — push notifications are an
/// enhancement, not a requirement. All app functionality works
/// via WebSocket regardless of push status.
final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {

    /// Shared reference set by IonRemoteApp so we can forward the device token.
    weak var sessionViewModel: SessionViewModel?

    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        requestNotificationPermission()
        return true
    }

    // MARK: - Registration

    func application(
        _ application: UIApplication,
        didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
    ) {
        let token = deviceToken.map { String(format: "%02x", $0) }.joined()
        print("[push] registered with token: \(token.prefix(8))...")
        sessionViewModel?.apnsToken = token
    }

    func application(
        _ application: UIApplication,
        didFailToRegisterForRemoteNotificationsWithError error: Error
    ) {
        // Graceful degradation: push is optional.
        print("[push] registration failed: \(error.localizedDescription)")
    }

    /// Called when iOS wakes the app in the background for a content-available:1 push.
    /// Ensures briefing payloads land in BriefingsStore even when the user opens
    /// the app directly without tapping the notification.
    func application(
        _ application: UIApplication,
        didReceiveRemoteNotification userInfo: [AnyHashable: Any],
        fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void
    ) {
        Self.handleBriefingPayload(userInfo)
        completionHandler(.newData)
    }

    // MARK: - Foreground delivery

    /// Process briefings silently when the app is in the foreground; suppress banner.
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        Self.handleBriefingPayload(notification.request.content.userInfo)
        completionHandler([])
    }

    /// Handle notification taps (app was in background or closed).
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let userInfo = response.notification.request.content.userInfo
        Self.handleBriefingPayload(userInfo, openSheet: true)
        if let tabId = userInfo["tabId"] as? String {
            sessionViewModel?.navigateToTab(tabId)
        }
        completionHandler()
    }

    // MARK: - Briefings

    private static func handleBriefingPayload(_ userInfo: [AnyHashable: Any], openSheet: Bool = false) {
        guard let briefingId = userInfo["briefingId"] as? String,
              let briefingText = userInfo["briefingText"] as? String else { return }
        let title = userInfo["briefingTitle"] as? String ?? "Morning Brief"
        let payload: [String: Any] = [
            "briefingId": briefingId,
            "briefingTitle": title,
            "briefingText": briefingText,
            "openSheet": openSheet,
        ]
        NotificationCenter.default.post(name: .briefingFromPush, object: nil, userInfo: payload)
    }

    // MARK: - Private

    private func requestNotificationPermission() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .badge, .sound]) { granted, error in
            if let error {
                print("[push] authorization error: \(error.localizedDescription)")
                return
            }
            guard granted else {
                print("[push] authorization denied by user")
                return
            }
            DispatchQueue.main.async {
                UIApplication.shared.registerForRemoteNotifications()
            }
        }
    }
}

extension Notification.Name {
    static let briefingFromPush = Notification.Name("briefingFromPush")
}
