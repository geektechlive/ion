import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * Scroll-follow hook: auto-tails a scrollable container and exposes a
 * "scroll to bottom" button state. Extracted from ConversationView.tsx
 * so both the main view and nested transcript panels share one behavior.
 *
 * @param deps - Caller-supplied dependency array. When any dep changes
 *   and the user is near the bottom, the container scrolls to the end.
 */
export function useScrollFollow(deps: unknown[]) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const isNearBottomRef = useRef(true)
  const [showScrollBtn, setShowScrollBtn] = useState(false)

  const handleScroll = useCallback(() => {
    if (!scrollRef.current) return
    const el = scrollRef.current
    const threshold = 80
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < threshold
    isNearBottomRef.current = nearBottom
    setShowScrollBtn(!nearBottom)
  }, [])

  // Auto-tail: scroll to bottom whenever deps change and the user is
  // already near the bottom. isNearBottomRef starts true so the very first
  // populate scrolls automatically.
  useEffect(() => {
    if (isNearBottomRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  const scrollToBottom = useCallback(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
      isNearBottomRef.current = true
      setShowScrollBtn(false)
    }
  }, [])

  return { scrollRef, isNearBottomRef, showScrollBtn, handleScroll, scrollToBottom }
}
