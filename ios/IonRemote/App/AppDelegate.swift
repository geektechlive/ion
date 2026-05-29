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

    // MARK: - UNUserNotificationCenterDelegate

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        Self.handleBriefingPayload(notification.request.content.userInfo)
        completionHandler([.banner, .list, .sound])
    }

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
    static let forceScrollToBottom = Notification.Name("forceScrollToBottom")
}
