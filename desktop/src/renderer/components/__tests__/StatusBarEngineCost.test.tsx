// @vitest-environment jsdom
/**
 * StatusBarEngineCost — renders the running USD cost from the active engine
 * instance's StatusFields. This slot reads `useActiveEngineStatusFields()`;
 * before the statusFields root-cause fix that helper always returned null in
 * production, so the slot never rendered. These tests pin the consumer behavior
 * the fix newly enables by stubbing the helper to return populated StatusFields.
 *
 * Renders via react-dom/client + act into jsdom (matching StatusBarEngineState.test.tsx).
 */

import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, it, expect, vi } from 'vitest'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true

let mockStatus: any = null
vi.mock('../StatusBarEngineHelpers', () => ({
  useActiveEngineStatusFields: () => mockStatus,
}))
vi.mock('../../theme', () => ({
  useColors: () => ({ textTertiary: '#888888' }),
}))

import { StatusBarEngineCost } from '../StatusBarEngineCost'

function renderHTML(): string {
  const container = document.createElement('div')
  const root = createRoot(container)
  try {
    act(() => {
      root.render(<StatusBarEngineCost />)
    })
    return container.innerHTML
  } finally {
    act(() => {
      root.unmount()
    })
  }
}

describe('StatusBarEngineCost', () => {
  it('renders $X.XX when totalCostUsd > 0', () => {
    mockStatus = { state: 'idle', model: 'm', contextPercent: 0, contextWindow: 0, totalCostUsd: 1.23 }
    expect(renderHTML()).toContain('$1.23')
  })

  it('renders nothing when totalCostUsd is 0', () => {
    mockStatus = { state: 'idle', model: 'm', contextPercent: 0, contextWindow: 0, totalCostUsd: 0 }
    expect(renderHTML()).toBe('')
  })

  it('renders nothing when totalCostUsd is undefined', () => {
    mockStatus = { state: 'idle', model: 'm', contextPercent: 0, contextWindow: 0 }
    expect(renderHTML()).toBe('')
  })

  it('renders nothing when statusFields is null (plain tab / no data)', () => {
    mockStatus = null
    expect(renderHTML()).toBe('')
  })
})
