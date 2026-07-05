// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { useScrollFollow } from '../useScrollFollow'

// Minimal hook runner without @testing-library/react. Works by rendering a
// tiny function component that captures the hook result into a mutable ref.
import React, { useState } from 'react'
import { act } from 'react'
import { createRoot } from 'react-dom/client'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true

function renderScrollHook(initialDeps: unknown[]) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  let result: ReturnType<typeof useScrollFollow>
  let setDeps: (d: unknown[]) => void

  function Harness() {
    const [deps, _setDeps] = useState(initialDeps)
    setDeps = _setDeps
    result = useScrollFollow(deps)
    return null
  }

  act(() => { root.render(React.createElement(Harness)) })

  return {
    get current() { return result! },
    update(newDeps: unknown[]) {
      act(() => { setDeps!(newDeps) })
    },
    unmount() {
      act(() => { root.unmount() })
      document.body.removeChild(container)
    },
  }
}

describe('useScrollFollow', () => {
  it('starts with showScrollBtn=false', () => {
    const hook = renderScrollHook([0])
    expect(hook.current.showScrollBtn).toBe(false)
    hook.unmount()
  })

  it('sets showScrollBtn when user scrolls away from bottom', () => {
    const hook = renderScrollHook([0])

    const div = document.createElement('div')
    Object.defineProperty(div, 'scrollHeight', { value: 1000, configurable: true })
    Object.defineProperty(div, 'scrollTop', { value: 0, writable: true, configurable: true })
    Object.defineProperty(div, 'clientHeight', { value: 400, configurable: true })

    ;(hook.current.scrollRef as any).current = div

    act(() => { hook.current.handleScroll() })

    // 1000 - 0 - 400 = 600 > 80 threshold
    expect(hook.current.showScrollBtn).toBe(true)
    hook.unmount()
  })

  it('hides showScrollBtn when near bottom (within 80px)', () => {
    const hook = renderScrollHook([0])

    const div = document.createElement('div')
    Object.defineProperty(div, 'scrollHeight', { value: 1000, configurable: true })
    Object.defineProperty(div, 'scrollTop', { value: 550, writable: true, configurable: true })
    Object.defineProperty(div, 'clientHeight', { value: 400, configurable: true })

    ;(hook.current.scrollRef as any).current = div

    act(() => { hook.current.handleScroll() })

    // 1000 - 550 - 400 = 50 < 80
    expect(hook.current.showScrollBtn).toBe(false)
    hook.unmount()
  })

  it('scrollToBottom scrolls and hides button', () => {
    const hook = renderScrollHook([0])

    const div = document.createElement('div')
    Object.defineProperty(div, 'scrollHeight', { value: 1000, configurable: true })
    Object.defineProperty(div, 'scrollTop', { value: 0, writable: true, configurable: true })
    Object.defineProperty(div, 'clientHeight', { value: 400, configurable: true })

    ;(hook.current.scrollRef as any).current = div

    // Scroll away first
    act(() => { hook.current.handleScroll() })
    expect(hook.current.showScrollBtn).toBe(true)

    // Then scrollToBottom
    act(() => { hook.current.scrollToBottom() })

    expect(div.scrollTop).toBe(1000)
    expect(hook.current.showScrollBtn).toBe(false)
    hook.unmount()
  })

  it('auto-tails when deps change and user is near bottom', () => {
    const hook = renderScrollHook([0])

    const div = document.createElement('div')
    Object.defineProperty(div, 'scrollHeight', { value: 500, configurable: true })
    Object.defineProperty(div, 'scrollTop', { value: 0, writable: true, configurable: true })
    Object.defineProperty(div, 'clientHeight', { value: 400, configurable: true })

    ;(hook.current.scrollRef as any).current = div

    // isNearBottomRef defaults to true, so auto-tail fires on dep change
    hook.update([1])

    expect(div.scrollTop).toBe(500)
    hook.unmount()
  })
})
