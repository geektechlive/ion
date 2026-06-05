import SwiftUI
import UserNotifications
#if canImport(UIKit)
import UIKit

class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    weak var sessionViewModel: SessionViewModel?

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
