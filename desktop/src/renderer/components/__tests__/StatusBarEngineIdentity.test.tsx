// @vitest-environment jsdom
/**
 * StatusBarEngineIdentity — renders the active engine's extension name + team
 * from StatusFields. Reads `useActiveEngineStatusFields()`; before the
 * statusFields root-cause fix that helper always returned null in production, so
 * the slot never rendered. These tests pin the consumer behavior the fix newly
 * enables by stubbing the helper to return populated StatusFields.
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
  useColors: () => ({ accent: '#33c3f7', textSecondary: '#bbbbbb' }),
}))

import { StatusBarEngineIdentity } from '../StatusBarEngineIdentity'

function renderHTML(): string {
  const container = document.createElement('div')
  const root = createRoot(container)
  try {
    act(() => {
      root.render(<StatusBarEngineIdentity />)
    })
    return container.innerHTML
  } finally {
    act(() => {
      root.unmount()
    })
  }
}

function fields(overrides: any = {}): any {
  return { state: 'idle', model: 'm', contextPercent: 0, contextWindow: 0, ...overrides }
}

describe('StatusBarEngineIdentity', () => {
  it('renders extensionName and team when both present', () => {
    mockStatus = fields({ extensionName: 'Chief of Staff', team: 'Platform' })
    const html = renderHTML()
    expect(html).toContain('Chief of Staff')
    expect(html).toContain('Platform')
  })

  it('renders extensionName only when team absent', () => {
    mockStatus = fields({ extensionName: 'Chief of Staff' })
    const html = renderHTML()
    expect(html).toContain('Chief of Staff')
  })

  it('renders nothing when both extensionName and team are absent', () => {
    mockStatus = fields()
    expect(renderHTML()).toBe('')
  })

  it('renders nothing when statusFields is null (plain tab / no data)', () => {
    mockStatus = null
    expect(renderHTML()).toBe('')
  })
})
