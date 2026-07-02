import { useCallback } from 'react'

/** Default agent-panel rows-container height in pixels. */
export const DEFAULT_PANEL_HEIGHT = 200
/** Minimum height the user can drag the panel down to. */
export const MIN_PANEL_HEIGHT = 80
/** Maximum panel height as a fraction of the window height. */
export const MAX_PANEL_PCT = 0.8

/**
 * Drag-to-resize handler for the agent panel's rows container.
 *
 * Returns an onMouseDown handler that, while the button is held, tracks the
 * pointer and reports a clamped new height through onPanelHeightChange. Dragging
 * UP (negative deltaY) increases the height; the height is clamped between
 * MIN_PANEL_HEIGHT and MAX_PANEL_PCT of the window height. The cursor and
 * user-select are pinned for the duration of the drag and restored on release.
 *
 * Extracted from AgentPanel.tsx so that file stays under the 600-line cap; the
 * resize mechanics are self-contained and have no other dependency on the
 * panel's render state.
 */
export function useAgentPanelResize(
  panelHeight: number | undefined,
  onPanelHeightChange: ((height: number) => void) | undefined,
) {
  return useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    const startY = e.clientY
    const startHeight = panelHeight ?? DEFAULT_PANEL_HEIGHT
    const maxHeight = window.innerHeight * MAX_PANEL_PCT

    const onMouseMove = (ev: MouseEvent) => {
      // Dragging up (negative deltaY) should increase panel height
      const deltaY = startY - ev.clientY
      const newHeight = Math.max(MIN_PANEL_HEIGHT, Math.min(maxHeight, startHeight + deltaY))
      onPanelHeightChange?.(Math.round(newHeight))
    }

    const onMouseUp = () => {
      window.removeEventListener('mousemove', onMouseMove)
      window.removeEventListener('mouseup', onMouseUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }

    document.body.style.cursor = 'ns-resize'
    document.body.style.userSelect = 'none'
    window.addEventListener('mousemove', onMouseMove)
    window.addEventListener('mouseup', onMouseUp)
  }, [panelHeight, onPanelHeightChange])
}
