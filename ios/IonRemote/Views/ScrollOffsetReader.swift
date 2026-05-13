import SwiftUI

// MARK: - UIKit Scroll Offset Reader

/// Finds the parent UIScrollView and observes contentOffset via KVO
/// to determine whether the user is near the bottom of the scroll view.
/// Uses a 100pt threshold — if the content below the viewport is less
/// than 100pt, `isNearBottom` is true.
///
/// Also exposes `scrollToBottom()` via a shared holder, allowing callers
/// to bypass SwiftUI's `scrollTo` (which relies on LazyVStack estimated
/// sizing and can overshoot on initial load).
struct ScrollOffsetReader: UIViewRepresentable {
    @Binding var isNearBottom: Bool
    var scrollHandle: ScrollHandle?

    func makeUIView(context: Context) -> UIView {
        let view = UIView(frame: .zero)
        view.isHidden = true
        view.isUserInteractionEnabled = false
        DispatchQueue.main.async {
            guard let scrollView = findScrollView(in: view) else { return }
            context.coordinator.observe(scrollView: scrollView)
        }
        return view
    }

    func updateUIView(_ uiView: UIView, context: Context) {}

    func makeCoordinator() -> Coordinator {
        Coordinator(isNearBottom: $isNearBottom, scrollHandle: scrollHandle)
    }

    private func findScrollView(in view: UIView) -> UIScrollView? {
        var current: UIView? = view
        while let parent = current?.superview {
            if let scrollView = parent as? UIScrollView {
                return scrollView
            }
            current = parent
        }
        return nil
    }

    final class Coordinator: NSObject {
        private var isNearBottom: Binding<Bool>
        private var offsetObservation: NSKeyValueObservation?
        private var sizeObservation: NSKeyValueObservation?
        private weak var scrollHandle: ScrollHandle?

        init(isNearBottom: Binding<Bool>, scrollHandle: ScrollHandle?) {
            self.isNearBottom = isNearBottom
            self.scrollHandle = scrollHandle
        }

        func observe(scrollView: UIScrollView) {
            scrollHandle?.attach(scrollView)
            offsetObservation = scrollView.observe(
                \.contentOffset, options: [.new]
            ) { [weak self] sv, _ in
                let distanceFromBottom =
                    sv.contentSize.height - sv.contentOffset.y - sv.bounds.height
                    + sv.adjustedContentInset.bottom
                let nearBottom = distanceFromBottom < 100
                if self?.isNearBottom.wrappedValue != nearBottom {
                    DispatchQueue.main.async {
                        self?.isNearBottom.wrappedValue = nearBottom
                    }
                }
            }
            // Also observe contentSize so the handle can re-scroll
            // when LazyVStack corrects its estimated size.
            sizeObservation = scrollView.observe(
                \.contentSize, options: [.new]
            ) { [weak self] _, _ in
                DispatchQueue.main.async {
                    self?.scrollHandle?.contentSizeDidChange()
                }
            }
        }

        deinit {
            offsetObservation = nil
            sizeObservation = nil
        }
    }
}

// MARK: - ScrollHandle

/// Holds a weak reference to the UIScrollView so callers can perform
/// UIKit-level `setContentOffset` scrolls that bypass LazyVStack
/// estimated sizing issues.
///
/// Also supports `pinToBottom()` — a mode where every contentSize
/// change re-scrolls to the bottom for a short window after load.
/// This handles LazyVStack progressively correcting its estimates.
final class ScrollHandle: ObservableObject {
    /// Set from the KVO coordinator on the main queue.
    nonisolated(unsafe) weak var scrollView: UIScrollView?
    /// When non-nil, contentSize changes trigger re-scroll until this fires.
    private var pinTask: Task<Void, Never>?

    func attach(_ scrollView: UIScrollView) {
        self.scrollView = scrollView
    }

    /// Scrolls to the true bottom using UIKit's contentSize.
    @MainActor
    func scrollToBottom(animated: Bool = false) {
        guard let sv = scrollView else { return }
        let bottomOffset = sv.contentSize.height - sv.bounds.height
            + sv.adjustedContentInset.bottom
        guard bottomOffset > 0 else { return }
        sv.setContentOffset(CGPoint(x: 0, y: bottomOffset), animated: animated)
    }

    /// Pin to bottom for `duration` seconds. Every contentSize change
    /// during this window re-scrolls to bottom, compensating for
    /// LazyVStack progressively correcting its estimated content size.
    @MainActor
    func pinToBottom(for duration: Duration = .seconds(1)) {
        pinTask?.cancel()
        pinTask = Task { @MainActor in
            try? await Task.sleep(for: duration)
            // pin expires naturally
        }
        scrollToBottom()
    }

    /// Called by the KVO observer when contentSize changes.
    @MainActor
    func contentSizeDidChange() {
        guard let task = pinTask, !task.isCancelled else { return }
        scrollToBottom()
    }
}
