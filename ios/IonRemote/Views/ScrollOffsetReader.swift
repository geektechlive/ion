import SwiftUI

// MARK: - UIKit Scroll Offset Reader

/// Finds the parent UIScrollView and observes contentOffset via KVO
/// to determine whether the user is near the bottom of the scroll view.
/// Uses a 100pt threshold — if the content below the viewport is less
/// than 100pt, `isNearBottom` is true.
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
                self?.updateNearBottom(sv)
            }
            sizeObservation = scrollView.observe(
                \.contentSize, options: [.new]
            ) { [weak self] sv, _ in
                self?.updateNearBottom(sv)
            }
        }

        private func updateNearBottom(_ sv: UIScrollView) {
            let distanceFromBottom =
                sv.contentSize.height - sv.contentOffset.y - sv.bounds.height
                + sv.adjustedContentInset.bottom
            let nearBottom = distanceFromBottom < 100

            // Only flip from near→far when the USER is actively scrolling.
            // ContentSize growth (new streaming content) pushes the bottom
            // further away, but the user hasn't scrolled — auto-scroll
            // should keep them pinned.  Without this guard, the KVO fires
            // before SwiftUI's .onChange, isNearBottom flips false, and the
            // debounced scroll-to-bottom never runs.
            let userIsScrolling =
                sv.isTracking || sv.isDragging || sv.isDecelerating
            if isNearBottom.wrappedValue && !nearBottom && !userIsScrolling {
                return  // content grew; don't flip the flag
            }

            if isNearBottom.wrappedValue != nearBottom {
                DispatchQueue.main.async {
                    self.isNearBottom.wrappedValue = nearBottom
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

/// Holds a weak reference to the UIScrollView discovered by
/// ScrollOffsetReader.  Currently used only for KVO attachment;
/// the ref is available for any future UIKit-level scroll needs.
final class ScrollHandle: ObservableObject {
    nonisolated(unsafe) weak var scrollView: UIScrollView?

    func attach(_ scrollView: UIScrollView) {
        self.scrollView = scrollView
    }
}
