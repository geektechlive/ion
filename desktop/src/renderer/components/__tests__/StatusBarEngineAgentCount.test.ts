/**
 * HR-1 regression: `useActiveEngineAgentRunningCount` is TAB-TYPE-AGNOSTIC.
 *
 * The "waiting for N background agent(s)" pulse must fire for a PLAIN
 * conversation that dispatched sub-agents, not just for extension-hosted
 * tabs — the Agent tool dispatches sub-agents regardless of harness, and the
 * close guard (tab-slice.ts) now blocks closing any tab with running children.
 * Pre-fix the selector returned 0 for `!tabHasExtensions(tab)`, so a plain tab
 * with running sub-agents showed no pulse.
 *
 * The selector reads `useSessionStore(selectorFn)`. We stub the store so the
 * call form invokes our selector against a fixed snapshot (pure fold, no
 * React, no DOM) — mirroring TabStripShared-running-children.test.ts.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest'

const state: { tabs: any[]; activeTabId: string | null; conversationPanes: Map<string, any> } = {
  tabs: [],
  activeTabId: null,
  conversationPanes: new Map(),
}

// useActiveEngineAgentRunningCount calls `useSessionStore(selector)`, so the
// store mock must invoke the selector with the current state (the hook form),
// and also expose getState() for any sibling that uses it.
vi.mock('../../stores/sessionStore', () => ({
  useSessionStore: Object.assign(
    (selector: (s: typeof state) => unknown) => selector(state),
    { getState: () => state },
  ),
}))

vi.mock('zustand/shallow', () => ({
  useShallow: (fn: unknown) => fn,
}))

import { useActiveEngineAgentRunningCount } from '../StatusBarEngineHelpers'

function reset() {
  state.tabs = []
  state.activeTabId = null
  state.conversationPanes = new Map()
}

function setActiveTab(tab: { id: string; engineProfileId: string | null }) {
  state.tabs = [tab]
  state.activeTabId = tab.id
}

function setPaneAgents(tabId: string, statuses: string[]) {
  state.conversationPanes.set(tabId, {
    instances: [{ id: 'main', label: 'main', statusFields: null, agentStates: statuses.map((status, i) => ({ name: `agent-${i}`, status })) }],
    activeInstanceId: 'main',
  })
}

describe('useActiveEngineAgentRunningCount — tab-type-agnostic (HR-1)', () => {
  beforeEach(reset)

  it('counts running children on a PLAIN tab (engineProfileId: null)', () => {
    setActiveTab({ id: 'tab1', engineProfileId: null })
    setPaneAgents('tab1', ['running', 'done', 'running'])
    expect(useActiveEngineAgentRunningCount()).toBe(2)
  })

  it('counts running children on an EXTENSION tab (parity)', () => {
    setActiveTab({ id: 'tab1', engineProfileId: 'test-profile' })
    setPaneAgents('tab1', ['running'])
    expect(useActiveEngineAgentRunningCount()).toBe(1)
  })

  it('returns 0 when a PLAIN tab has no running children', () => {
    setActiveTab({ id: 'tab1', engineProfileId: null })
    setPaneAgents('tab1', ['done', 'idle', 'cancelled'])
    expect(useActiveEngineAgentRunningCount()).toBe(0)
  })

  it('returns 0 when there is no active instance', () => {
    setActiveTab({ id: 'tab1', engineProfileId: null })
    expect(useActiveEngineAgentRunningCount()).toBe(0)
  })
})
