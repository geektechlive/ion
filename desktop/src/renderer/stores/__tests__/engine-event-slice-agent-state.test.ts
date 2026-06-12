/**
 * engine-event-slice — `engine_agent_state` snapshot contract
 *
 * The engine emits `engine_agent_state` as a COMPLETE SNAPSHOT of every
 * agent it considers live. Consumers replace local state with the
 * payload — they do not merge, retain, or invent rules about which
 * entries to preserve. See docs/architecture/agent-state.md.
 *
 * These tests pin that contract on the desktop renderer:
 *
 *   - Non-empty payload → instance.agentStates is replaced with payload.
 *   - Empty payload → instance.agentStates is replaced with empty array.
 *   - The slice never invents entries that aren't in the engine's payload.
 */

import { describe, it, expect, vi } from 'vitest'

vi.mock('../session-store-helpers', () => ({
  makeLocalTab: vi.fn(),
  nextMsgId: vi.fn(() => 'mock-msg-id'),
  playNotificationIfHidden: vi.fn(async () => {}),
}))

import { createEngineEventSlice } from '../slices/engine-event-slice'
import type { State } from '../session-store-types'
import type { AgentStateUpdate } from '../../../shared/types-engine'

function makeInstance(id: string, extra: Partial<any> = {}) {
  return {
    id,
    label: id,
    messages: [],
    modelOverride: null,
    permissionMode: 'auto',
    permissionDenied: null,
    conversationIds: [],
    draftInput: '',
    agentStates: [] as AgentStateUpdate[],
    statusFields: null,
    planFilePath: null,
    ...extra,
  }
}

function buildHarness() {
  const state: any = {
    tabs: [{ id: 'tab1', isEngine: true, lastEventAt: 0 }],
    engineWorkingMessages: new Map(),
    engineNotifications: new Map(),
    engineDialogs: new Map(),
    enginePinnedPrompt: new Map(),
    engineUsage: new Map(),
    engineModelFallbacks: new Map(),
    enginePanes: new Map([['tab1', { instances: [makeInstance('inst1')], activeInstanceId: 'inst1' }]]),
  }
  const set = (partial: any) => {
    const patch = typeof partial === 'function' ? partial(state) : partial
    Object.assign(state, patch)
  }
  const get = () => state as State
  const slice = createEngineEventSlice(set, get) as State
  return { state, slice }
}

/** Read agentStates from the instance in enginePanes. */
function getAgentStates(state: any, tabId: string, instanceId: string): AgentStateUpdate[] | undefined {
  const pane = state.enginePanes.get(tabId)
  const inst = pane?.instances.find((i: any) => i.id === instanceId)
  return inst?.agentStates
}

describe('engine_agent_state snapshot contract', () => {
  it('replaces instance.agentStates with non-empty payload', () => {
    const { state, slice } = buildHarness()
    const key = 'tab1:inst1'

    slice.handleEngineEvent(key, {
      type: 'engine_agent_state',
      agents: [
        { name: 'a', status: 'running' },
        { name: 'b', status: 'done' },
      ],
    } as any)

    const stored = getAgentStates(state, 'tab1', 'inst1')
    expect(stored).toBeDefined()
    expect(stored).toHaveLength(2)
    expect(stored![0].name).toBe('a')
    expect(stored![1].status).toBe('done')
  })

  it('replaces instance.agentStates with empty array — no historical preservation', () => {
    const { state, slice } = buildHarness()
    const key = 'tab1:inst1'

    // Seed: pretend a sticky/done agent was previously stored on the instance.
    const pane = state.enginePanes.get('tab1')
    pane.instances[0] = {
      ...pane.instances[0],
      agentStates: [{ name: 'kept', status: 'done', metadata: { conversationId: 'conv-xyz', visibility: 'sticky' } }],
    }

    slice.handleEngineEvent(key, {
      type: 'engine_agent_state',
      agents: [],
    } as any)

    const stored = getAgentStates(state, 'tab1', 'inst1')
    expect(stored).toBeDefined()
    expect(stored).toHaveLength(0)
  })

  it('does not invent entries beyond the payload', () => {
    const { state, slice } = buildHarness()
    const key = 'tab1:inst1'

    const pane = state.enginePanes.get('tab1')
    pane.instances[0] = {
      ...pane.instances[0],
      agentStates: [
        { name: 'old-1', status: 'done' },
        { name: 'old-2', status: 'done' },
      ],
    }

    slice.handleEngineEvent(key, {
      type: 'engine_agent_state',
      agents: [{ name: 'new', status: 'running' }],
    } as any)

    const stored = getAgentStates(state, 'tab1', 'inst1')
    expect(stored).toHaveLength(1)
    expect(stored![0].name).toBe('new')
  })

  it('handles missing agents field as empty payload', () => {
    const { state, slice } = buildHarness()
    const key = 'tab1:inst1'

    const pane = state.enginePanes.get('tab1')
    pane.instances[0] = {
      ...pane.instances[0],
      agentStates: [{ name: 'old', status: 'done' }],
    }

    slice.handleEngineEvent(key, { type: 'engine_agent_state' } as any)

    const stored = getAgentStates(state, 'tab1', 'inst1')
    expect(stored).toHaveLength(0)
  })

  it('ignores events when key has no instance component', () => {
    const { state, slice } = buildHarness()
    const beforePanes = state.enginePanes

    slice.handleEngineEvent('tab1', {
      type: 'engine_agent_state',
      agents: [{ name: 'x', status: 'running' }],
    } as any)

    // No colon in key → handler returns early, enginePanes unchanged.
    expect(state.enginePanes).toBe(beforePanes)
  })

  it('writes a new enginePanes Map (no in-place mutation)', () => {
    const { state, slice } = buildHarness()
    const key = 'tab1:inst1'
    const before = state.enginePanes

    slice.handleEngineEvent(key, {
      type: 'engine_agent_state',
      agents: [{ name: 'a', status: 'running' }],
    } as any)

    // The slice should construct a new Map so React detects the change.
    expect(state.enginePanes).not.toBe(before)
  })
})
