// @vitest-environment jsdom
/**
 * FloatingPanel — previewFontSize CSS variable placement test.
 *
 * Verifies:
 *   - The content wrapper div carries --ion-conv-font-size: <n>px.
 *   - The header div does NOT carry --ion-conv-font-size (chrome stays fixed).
 *
 * Also verifies the openFloatingPanelCount store behavior (inc on mount,
 * dec on unmount).
 */
import React from 'react'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

;(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true

// ── Store mocks ────────────────────────────────────────────────────────────

let previewFontSize = 16
let openFloatingPanelCount = 0

vi.mock('../../preferences', () => ({
  usePreferencesStore: (sel: any) => sel({ previewFontSize }),
}))

const incMock = vi.fn(() => { openFloatingPanelCount++ })
const decMock = vi.fn(() => { openFloatingPanelCount = Math.max(0, openFloatingPanelCount - 1) })

vi.mock('../../stores/sessionStore', () => ({
  useSessionStore: (sel: any) => sel({
    openFloatingPanelCount,
    incOpenFloatingPanelCount: incMock,
    decOpenFloatingPanelCount: decMock,
  }),
}))

// useColors mock.
vi.mock('../../theme', () => ({
  useColors: () => ({
    containerBg: '#000',
    containerBorder: '#111',
    surfacePrimary: '#222',
    textTertiary: '#333',
    textSecondary: '#444',
    accent: '#00aaff',
  }),
}))

// PopoverLayer mock — render directly (no portal).
vi.mock('../PopoverLayer', () => ({
  usePopoverLayer: () => {
    return document.body
  },
}))

// X icon mock.
vi.mock('@phosphor-icons/react', () => ({ X: () => null }))

import { FloatingPanel } from '../FloatingPanel'

function Panel(props: { children: React.ReactNode; title?: string; onClose?: () => void }) {
  return React.createElement(FloatingPanel, {
    title: props.title ?? 'Test',
    onClose: props.onClose ?? (() => {}),
    children: props.children,
  })
}

describe('FloatingPanel — previewFontSize CSS variable', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>

  beforeEach(() => {
    previewFontSize = 16
    openFloatingPanelCount = 0
    incMock.mockClear()
    decMock.mockClear()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => { root.unmount() })
    document.body.removeChild(container)
  })

  it('content wrapper carries --ion-conv-font-size: 16px', async () => {
    await act(async () => {
      root.render(React.createElement(Panel, null, React.createElement('div', { 'data-testid': 'child' }, 'content')))
    })

    const allDivs = Array.from(document.body.querySelectorAll('div'))
    const contentWrapper = allDivs.find((div) => {
      return (div as HTMLElement).style.getPropertyValue('--ion-conv-font-size') === '16px'
    })
    expect(contentWrapper).toBeDefined()
  })

  it('header div does NOT carry --ion-conv-font-size (only content wrapper does)', async () => {
    await act(async () => {
      root.render(React.createElement(Panel, null, React.createElement('div', { 'data-testid': 'child-2' }, 'child content')))
    })

    const allDivs = Array.from(document.body.querySelectorAll('div'))
    const withVar = allDivs.filter((div) => {
      return (div as HTMLElement).style.getPropertyValue('--ion-conv-font-size') !== ''
    })
    // Exactly one div — the content wrapper — carries the variable.
    expect(withVar).toHaveLength(1)
    // That div is the content wrapper (contains the child).
    const child = withVar[0].querySelector('[data-testid="child-2"]')
    expect(child).not.toBeNull()
  })

  it('increments openFloatingPanelCount on mount', async () => {
    expect(openFloatingPanelCount).toBe(0)
    await act(async () => {
      root.render(React.createElement(Panel, null, React.createElement('div', null, 'c')))
    })
    expect(incMock).toHaveBeenCalledOnce()
    expect(openFloatingPanelCount).toBe(1)
  })

  it('decrements openFloatingPanelCount on unmount', async () => {
    await act(async () => {
      root.render(React.createElement(Panel, null, React.createElement('div', null, 'c')))
    })
    expect(openFloatingPanelCount).toBe(1)
    await act(async () => { root.unmount() })
    expect(decMock).toHaveBeenCalledOnce()
    expect(openFloatingPanelCount).toBe(0)
    // Re-create for afterEach cleanup.
    root = createRoot(container)
  })
})
