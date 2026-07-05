// @vitest-environment jsdom
/**
 * StatusBarBackendIndicator — renders "via CLI" when the active engine instance's
 * StatusFields.backend is 'cli', else falls back to the global desktop backend
 * ("CLI"). The engine-instance branch reads `useActiveEngineStatusFields()`;
 * before the statusFields root-cause fix that helper always returned null in
 * production, so the "via CLI" per-instance badge never rendered (only the
 * global `backend === 'cli'` path worked). These tests pin the per-instance
 * branch the fix newly enables by stubbing the helper.
 *
 * Renders via react-dom/client + act into jsdom (matching StatusBarEngineState.test.tsx).
 */

import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, it, expect, vi } from 'vitest'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true

let mockStatus: any = null
let mockBackend: string = 'api'

vi.mock('../StatusBarEngineHelpers', () => ({
  useActiveEngineStatusFields: () => mockStatus,
}))
vi.mock('../../stores/sessionStore', () => ({
  useSessionStore: (selector: (s: { backend: string }) => unknown) => selector({ backend: mockBackend }),
}))

import { BackendIndicator } from '../StatusBarBackendIndicator'

function renderHTML(): string {
  const container = document.createElement('div')
  const root = createRoot(container)
  try {
    act(() => {
      root.render(<BackendIndicator />)
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

describe('BackendIndicator', () => {
  it('renders "via CLI" when engine instance backend is cli', () => {
    mockStatus = fields({ backend: 'cli' })
    mockBackend = 'api'
    expect(renderHTML()).toContain('via CLI')
  })

  it('renders nothing when engine instance backend is api and global is api', () => {
    mockStatus = fields({ backend: 'api' })
    mockBackend = 'api'
    expect(renderHTML()).toBe('')
  })

  it('falls back to global "CLI" when no engine status and global backend is cli', () => {
    mockStatus = null
    mockBackend = 'cli'
    expect(renderHTML()).toContain('CLI')
  })

  it('renders nothing when no engine status and global backend is api', () => {
    mockStatus = null
    mockBackend = 'api'
    expect(renderHTML()).toBe('')
  })
})
