// @vitest-environment jsdom
/**
 * useEdgeResize — 8-direction edge/corner resize hook.
 *
 * Two layers of coverage:
 *
 *   1. computeResizeGeometry() — the pure geometry solver. Tested directly
 *      (no React) for each direction, the min-size floor pin, and the viewport
 *      clamp. This is the load-bearing math.
 *
 *   2. useEdgeResize() — the React hook. Rendered via a minimal local
 *      renderHook helper (createRoot + act, the repo convention — see
 *      useScrollFollow.test.ts). A drag is simulated by dispatching a
 *      mousedown on a zone's onMouseDown, then window mousemove/mouseup, and
 *      we assert onResize/onResizeEnd fire with the correct geometry.
 */
import { describe, it, expect, vi } from 'vitest'
import React from 'react'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import {
  computeResizeGeometry,
  useEdgeResize,
  type ResizeGeometry,
  type UseEdgeResizeParams,
  type UseEdgeResizeResult,
} from '../useEdgeResize'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true

// A generous viewport for the pure-math tests where we don't want the clamp
// to interfere unless we're explicitly testing it.
const VW = 2000
const VH = 2000

const START: ResizeGeometry = { x: 100, y: 100, w: 400, h: 300 }

describe('computeResizeGeometry — direction math', () => {
  it('e drag: +50 dx → w increases by 50, x unchanged', () => {
    const g = computeResizeGeometry(START, 'e', 50, 0, 280, 180, VW, VH)
    expect(g.w).toBe(450)
    expect(g.x).toBe(100)
    expect(g.h).toBe(300)
    expect(g.y).toBe(100)
  })

  it('w drag: +50 dx → w decreases by 50, x increases by 50', () => {
    const g = computeResizeGeometry(START, 'w', 50, 0, 280, 180, VW, VH)
    expect(g.w).toBe(350)
    expect(g.x).toBe(150)
  })

  it('s drag: +50 dy → h increases by 50, y unchanged', () => {
    const g = computeResizeGeometry(START, 's', 0, 50, 280, 180, VW, VH)
    expect(g.h).toBe(350)
    expect(g.y).toBe(100)
  })

  it('n drag: +50 dy → h decreases by 50, y increases by 50', () => {
    const g = computeResizeGeometry(START, 'n', 0, 50, 280, 180, VW, VH)
    expect(g.h).toBe(250)
    expect(g.y).toBe(150)
  })

  it('se drag: +50 dx +50 dy → w and h both increase', () => {
    const g = computeResizeGeometry(START, 'se', 50, 50, 280, 180, VW, VH)
    expect(g.w).toBe(450)
    expect(g.h).toBe(350)
    expect(g.x).toBe(100)
    expect(g.y).toBe(100)
  })

  it('nw drag: +50 dx +50 dy → w decreases, x increases, h decreases, y increases', () => {
    const g = computeResizeGeometry(START, 'nw', 50, 50, 280, 180, VW, VH)
    expect(g.w).toBe(350)
    expect(g.x).toBe(150)
    expect(g.h).toBe(250)
    expect(g.y).toBe(150)
  })
})

describe('computeResizeGeometry — clamps', () => {
  it('min-clamp: w drag past minWidth pins x at startX + startW - minWidth', () => {
    // Drag the left edge inward far enough that width would fall below minWidth.
    // startW=400, minWidth=280. A +200 dx would give w=200 (< 280) → clamp to 280.
    // x must not move past startX + startW - minWidth = 100 + 400 - 280 = 220.
    const g = computeResizeGeometry(START, 'w', 200, 0, 280, 180, VW, VH)
    expect(g.w).toBe(280)
    expect(g.x).toBe(220)
    // Dragging even further must not move x past the pin.
    const g2 = computeResizeGeometry(START, 'w', 400, 0, 280, 180, VW, VH)
    expect(g2.w).toBe(280)
    expect(g2.x).toBe(220)
  })

  it('min-clamp: n drag past minHeight pins y at startY + startH - minHeight', () => {
    // startH=300, minHeight=180. +200 dy → h=100 (<180) clamp to 180.
    // y pin = 100 + 300 - 180 = 220.
    const g = computeResizeGeometry(START, 'n', 0, 200, 280, 180, VW, VH)
    expect(g.h).toBe(180)
    expect(g.y).toBe(220)
  })

  it('viewport clamp: e drag past innerWidth clamps w and keeps x valid', () => {
    // Small viewport so the width exceeds it. startX=100, startW=400.
    // A huge +5000 dx would give w=5400; clamp to vw=800 → but x=100 means
    // x + w must stay <= vw, so x is clamped to vw - w = 800 - 800 = 0.
    const smallVW = 800
    const g = computeResizeGeometry(START, 'e', 5000, 0, 280, 180, smallVW, VH)
    expect(g.w).toBe(smallVW)
    expect(g.x).toBeGreaterThanOrEqual(0)
    expect(g.x + g.w).toBeLessThanOrEqual(smallVW)
  })
})

// ── Hook harness ────────────────────────────────────────────────────────────

function renderEdgeResize(params: UseEdgeResizeParams) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  let result: UseEdgeResizeResult

  function Harness() {
    result = useEdgeResize(params)
    return null
  }

  act(() => {
    root.render(React.createElement(Harness))
  })

  return {
    get current() {
      return result!
    },
    unmount() {
      act(() => root.unmount())
      document.body.removeChild(container)
    },
  }
}

describe('useEdgeResize — hook drag lifecycle', () => {
  it('fires onResize during drag and onResizeEnd on mouseup', () => {
    const onResize = vi.fn()
    const onResizeEnd = vi.fn()
    const geo: ResizeGeometry = { x: 100, y: 100, w: 400, h: 300 }

    // jsdom's window.innerWidth/innerHeight default to 1024x768.
    const hook = renderEdgeResize({
      minWidth: 280,
      minHeight: 180,
      onResize,
      onResizeEnd,
      getGeometry: () => geo,
    })

    // Start an `e` drag at clientX=500.
    const props = hook.current.getZoneProps('e')
    act(() => {
      props.onMouseDown({
        button: 0,
        clientX: 500,
        clientY: 200,
        preventDefault: () => {},
        stopPropagation: () => {},
      } as unknown as React.MouseEvent)
    })

    // Move +50 in x.
    act(() => {
      window.dispatchEvent(new MouseEvent('mousemove', { clientX: 550, clientY: 200 }))
    })
    expect(onResize).toHaveBeenCalled()
    const last = onResize.mock.calls[onResize.mock.calls.length - 1][0] as ResizeGeometry
    expect(last.w).toBe(450)
    expect(last.x).toBe(100)

    // Release.
    act(() => {
      window.dispatchEvent(new MouseEvent('mouseup', { clientX: 550, clientY: 200 }))
    })
    expect(onResizeEnd).toHaveBeenCalledTimes(1)
    const endGeo = onResizeEnd.mock.calls[0][0] as ResizeGeometry
    expect(endGeo.w).toBe(450)

    hook.unmount()
  })

  it('ignores non-left-button mousedown', () => {
    const onResize = vi.fn()
    const hook = renderEdgeResize({
      minWidth: 280,
      minHeight: 180,
      onResize,
      onResizeEnd: vi.fn(),
      getGeometry: () => ({ x: 0, y: 0, w: 400, h: 300 }),
    })
    const props = hook.current.getZoneProps('e')
    act(() => {
      props.onMouseDown({
        button: 2,
        clientX: 500,
        clientY: 200,
        preventDefault: () => {},
        stopPropagation: () => {},
      } as unknown as React.MouseEvent)
    })
    act(() => {
      window.dispatchEvent(new MouseEvent('mousemove', { clientX: 550, clientY: 200 }))
    })
    expect(onResize).not.toHaveBeenCalled()
    hook.unmount()
  })

  it('renderZones returns exactly 8 zone elements', () => {
    const hook = renderEdgeResize({
      minWidth: 280,
      minHeight: 180,
      onResize: vi.fn(),
      onResizeEnd: vi.fn(),
      getGeometry: () => ({ x: 0, y: 0, w: 400, h: 300 }),
    })
    const zones = hook.current.renderZones()
    expect(zones).toHaveLength(8)
    hook.unmount()
  })

  it('removes window listeners on unmount', () => {
    const onResize = vi.fn()
    const hook = renderEdgeResize({
      minWidth: 280,
      minHeight: 180,
      onResize,
      onResizeEnd: vi.fn(),
      getGeometry: () => ({ x: 0, y: 0, w: 400, h: 300 }),
    })
    const props = hook.current.getZoneProps('e')
    act(() => {
      props.onMouseDown({
        button: 0,
        clientX: 500,
        clientY: 200,
        preventDefault: () => {},
        stopPropagation: () => {},
      } as unknown as React.MouseEvent)
    })
    hook.unmount()
    // After unmount, no listener should fire.
    window.dispatchEvent(new MouseEvent('mousemove', { clientX: 999, clientY: 200 }))
    // Only calls (if any) predate unmount; a post-unmount move must not add one.
    const callsBefore = onResize.mock.calls.length
    window.dispatchEvent(new MouseEvent('mousemove', { clientX: 1010, clientY: 200 }))
    expect(onResize.mock.calls.length).toBe(callsBefore)
  })
})
