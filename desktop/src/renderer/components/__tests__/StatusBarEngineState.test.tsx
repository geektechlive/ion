// @vitest-environment jsdom
/**
 * Regression: the engine-state status-bar slot ("[running]" /
 * "[waiting for N agent(s)]") must render from the signals that are
 * actually populated in the renderer — `tab.status` for the orchestrator's own
 * run-state and `useActiveEngineAgentRunningCount()` for the dispatched
 * agent count — NOT from `inst.statusFields`, which the renderer
 * never populates.
 *
 * Pre-fix, `StatusBarEngineState` did `const status = useActiveEngineStatusFields()`
 * then `if (!status) return null`. Because `inst.statusFields` is always null in
 * the renderer, the slot rendered nothing on EVERY tab — the yellow
 * "waiting for N agent(s)" text never appeared. The idle+running-agent
 * case below is the regression assertion: it is red on the pre-fix code (slot
 * returns null) and green after the fix.
 *
 * The store is stubbed so the component's narrow `useSessionStore(useShallow(...))`
 * selector folds a fixed snapshot, and `useActiveEngineAgentRunningCount` (which
 * also calls `useSessionStore(selector)`) reads the same snapshot. `useColors`
 * and `zustand/shallow` are stubbed so the test is a pure render with no theme
 * or store wiring. Renders via react-dom/client + act into jsdom (matching
 * ToolGroup.test.tsx), asserting on rendered text.
 */

import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, it, expect, beforeEach, vi } from 'vitest'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const state: { tabs: any[]; activeTabId: string | null; conversationPanes: Map<string, any> } = {
  tabs: [],
  activeTabId: null,
  conversationPanes: new Map(),
}

// Both the component's `tab.status` selector and `useActiveEngineAgentRunningCount`
// call `useSessionStore(selector)`, so the mock invokes the selector with the
// fixed snapshot (the hook form) and also exposes getState().
vi.mock('../../stores/sessionStore', () => ({
  useSessionStore: Object.assign(
    (selector: (s: typeof state) => unknown) => selector(state),
    { getState: () => state },
  ),
}))

// useShallow is identity here — the selector is invoked directly against state.
vi.mock('zustand/shallow', () => ({
  useShallow: (fn: unknown) => fn,
}))

// useColors yields a distinct color per token so we can assert which branch
// (running vs. waiting-children) drove the dot color.
vi.mock('../../theme', () => ({
  useColors: () => ({
    statusRunning: '#d97757',
    statusWaitingChildren: '#f59e0b',
    textTertiary: '#888888',
  }),
}))

import { StatusBarEngineState } from '../StatusBarEngineState'

function reset() {
  state.tabs = []
  state.activeTabId = null
  state.conversationPanes = new Map()
}

function setActiveTab(tab: { id: string; engineProfileId: string | null; status: string }) {
  state.tabs = [tab]
  state.activeTabId = tab.id
}

function setPaneAgents(tabId: string, statuses: string[]) {
  state.conversationPanes.set(tabId, {
    instances: [
      {
        id: 'main',
        label: 'main',
        statusFields: null,
        agentStates: statuses.map((status, i) => ({ name: `agent-${i}`, status })),
      },
    ],
    activeInstanceId: 'main',
  })
}

function renderHTML(): string {
  const container = document.createElement('div')
  const root = createRoot(container)
  try {
    act(() => {
      root.render(<StatusBarEngineState />)
    })
    return container.innerHTML
  } finally {
    act(() => {
      root.unmount()
    })
  }
}

describe('StatusBarEngineState — derives from tab.status + agentRunningCount', () => {
  beforeEach(reset)

  it('PLAIN tab, idle orchestrator, 1 running agent → "[waiting for 1 agent]" (REGRESSION)', () => {
    setActiveTab({ id: 'tab1', engineProfileId: null, status: 'idle' })
    setPaneAgents('tab1', ['running', 'done'])
    expect(renderHTML()).toContain('[waiting for 1 agent]')
  })

  it('PLAIN tab, idle orchestrator, 2 running agents → pluralized "agents"', () => {
    setActiveTab({ id: 'tab1', engineProfileId: null, status: 'idle' })
    setPaneAgents('tab1', ['running', 'running'])
    expect(renderHTML()).toContain('[waiting for 2 agents]')
  })

  it('EXTENSION tab, idle orchestrator, 1 running agent → renders waiting text (parity)', () => {
    setActiveTab({ id: 'tab1', engineProfileId: 'test-profile', status: 'idle' })
    setPaneAgents('tab1', ['running'])
    expect(renderHTML()).toContain('[waiting for 1 agent]')
  })

  it('running orchestrator + running agents → "[running]", no waiting text (priority cascade)', () => {
    setActiveTab({ id: 'tab1', engineProfileId: null, status: 'running' })
    setPaneAgents('tab1', ['running'])
    const html = renderHTML()
    expect(html).toContain('[running]')
    expect(html).not.toContain('waiting for')
  })

  it('connecting orchestrator → "[running]" (connecting treated as foreground)', () => {
    setActiveTab({ id: 'tab1', engineProfileId: null, status: 'connecting' })
    setPaneAgents('tab1', [])
    expect(renderHTML()).toContain('[running]')
  })

  it('idle orchestrator, 0 running agents → renders nothing', () => {
    setActiveTab({ id: 'tab1', engineProfileId: null, status: 'idle' })
    setPaneAgents('tab1', ['done', 'cancelled'])
    expect(renderHTML()).toBe('')
  })
})
