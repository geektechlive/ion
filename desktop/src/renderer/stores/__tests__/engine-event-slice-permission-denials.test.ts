/**
 * engine-event-slice — engine_status.permissionDenials → tab.permissionDenied
 *
 * The engine intercepts AskUserQuestion and ExitPlanMode and emits the
 * denial on the next engine_status event under `fields.permissionDenials`.
 * For engine-view tabs (compound `tabId:instanceId` key), the slice is the
 * only place that translates this signal into the renderer's
 * `tab.permissionDenied` shape — the CLI synthesis path in
 * engine-control-plane-events.ts:handleStatusEvent is bypassed for these
 * tabs because EngineControlPlane is keyed by bare tabId only.
 *
 * Contract pinned here:
 *   1. AskUserQuestion / ExitPlanMode denials populate tab.permissionDenied.
 *   2. Other denial tool names are ignored.
 *   3. Cost-only follow-up status (no denials) preserves the existing
 *      permissionDenied — it does NOT clobber.
 *   4. The parent tabId (key.split(':')[0]) is updated, even when the
 *      event arrives on a compound key.
 */

import { describe, it, expect, vi } from 'vitest'

vi.mock('../session-store-helpers', () => ({
  makeLocalTab: vi.fn(),
  nextMsgId: vi.fn(() => 'mock-msg-id'),
  playNotificationIfHidden: vi.fn(async () => {}),
}))

import { createEngineEventSlice } from '../slices/engine-event-slice'
import type { State } from '../session-store-types'

function buildHarness() {
  const state: any = {
    tabs: [{ id: 'tab1', isEngine: true, lastEventAt: 0, status: 'running', permissionDenied: null }],
    engineAgentStates: new Map(),
    engineStatusFields: new Map(),
    engineWorkingMessages: new Map(),
    engineNotifications: new Map(),
    engineDialogs: new Map(),
    enginePinnedPrompt: new Map(),
    engineUsage: new Map(),
    engineMessages: new Map(),
    engineDraftInputs: new Map(),
    engineModelOverrides: new Map(),
    engineConversationIds: new Map(),
    enginePanes: new Map(),
  }
  const set = (partial: any) => {
    const patch = typeof partial === 'function' ? partial(state) : partial
    Object.assign(state, patch)
  }
  const get = () => state as State
  const slice = createEngineEventSlice(set, get) as State
  return { state, slice }
}

describe('engine_status.permissionDenials → tab.permissionDenied', () => {
  it('sets tab.permissionDenied for AskUserQuestion denial on compound key', () => {
    const { state, slice } = buildHarness()
    const key = 'tab1:inst-a'

    slice.handleEngineEvent(key, {
      type: 'engine_status',
      fields: {
        label: 'engine',
        state: 'idle',
        model: 'sonnet',
        contextPercent: 12,
        contextWindow: 200000,
        permissionDenials: [
          {
            toolName: 'AskUserQuestion',
            toolUseId: 'tu-1',
            toolInput: { question: 'Pick one', options: ['A', 'B'] },
          },
        ],
      },
    } as any)

    const tab = state.tabs.find((t: any) => t.id === 'tab1')
    expect(tab.permissionDenied).not.toBeNull()
    expect(tab.permissionDenied.tools).toHaveLength(1)
    expect(tab.permissionDenied.tools[0].toolName).toBe('AskUserQuestion')
    expect(tab.permissionDenied.tools[0].toolUseId).toBe('tu-1')
    expect(tab.permissionDenied.tools[0].toolInput).toEqual({ question: 'Pick one', options: ['A', 'B'] })
  })

  it('sets tab.permissionDenied for ExitPlanMode denial', () => {
    const { state, slice } = buildHarness()
    const key = 'tab1:inst-a'

    slice.handleEngineEvent(key, {
      type: 'engine_status',
      fields: {
        label: 'engine',
        state: 'idle',
        model: 'sonnet',
        contextPercent: 12,
        contextWindow: 200000,
        permissionDenials: [
          {
            toolName: 'ExitPlanMode',
            toolUseId: 'tu-2',
            toolInput: { planFilePath: '/x/plan.md' },
          },
        ],
      },
    } as any)

    const tab = state.tabs.find((t: any) => t.id === 'tab1')
    expect(tab.permissionDenied?.tools[0].toolName).toBe('ExitPlanMode')
  })

  it('filters out non-interactive tool denials (Read, Bash, etc.)', () => {
    const { state, slice } = buildHarness()
    const key = 'tab1:inst-a'

    slice.handleEngineEvent(key, {
      type: 'engine_status',
      fields: {
        label: 'engine',
        state: 'idle',
        model: 'sonnet',
        contextPercent: 12,
        contextWindow: 200000,
        permissionDenials: [
          { toolName: 'Read', toolUseId: 'tu-3', toolInput: { file_path: '/x' } },
          { toolName: 'Bash', toolUseId: 'tu-4', toolInput: { command: 'ls' } },
        ],
      },
    } as any)

    const tab = state.tabs.find((t: any) => t.id === 'tab1')
    // No AskUserQuestion / ExitPlanMode denials → tab.permissionDenied untouched (null).
    expect(tab.permissionDenied).toBeNull()
  })

  it('preserves existing permissionDenied on follow-up cost-only status (no denials)', () => {
    const { state, slice } = buildHarness()
    const key = 'tab1:inst-a'

    // Tick 1: denial arrives.
    slice.handleEngineEvent(key, {
      type: 'engine_status',
      fields: {
        label: 'engine',
        state: 'idle',
        model: 'sonnet',
        contextPercent: 12,
        contextWindow: 200000,
        permissionDenials: [
          { toolName: 'AskUserQuestion', toolUseId: 'tu-1', toolInput: { question: 'q?' } },
        ],
      },
    } as any)

    // Tick 2: cost-only follow-up (engine emits this ~1ms after the denial tick).
    slice.handleEngineEvent(key, {
      type: 'engine_status',
      fields: {
        label: 'engine',
        state: 'idle',
        model: 'sonnet',
        contextPercent: 12,
        contextWindow: 200000,
        totalCostUsd: 0.001,
        // permissionDenials absent
      },
    } as any)

    const tab = state.tabs.find((t: any) => t.id === 'tab1')
    expect(tab.permissionDenied).not.toBeNull()
    expect(tab.permissionDenied.tools[0].toolName).toBe('AskUserQuestion')
  })

  it('derives parent tabId from compound key (key.split(":")[0])', () => {
    const { state, slice } = buildHarness()
    // Add a second tab to confirm we only touch the parent of the compound key.
    state.tabs.push({ id: 'tab2', isEngine: true, lastEventAt: 0, status: 'running', permissionDenied: null })

    slice.handleEngineEvent('tab1:inst-a', {
      type: 'engine_status',
      fields: {
        label: 'engine',
        state: 'idle',
        model: 'sonnet',
        contextPercent: 0,
        contextWindow: 200000,
        permissionDenials: [
          { toolName: 'AskUserQuestion', toolUseId: 'tu-1', toolInput: { question: 'q?' } },
        ],
      },
    } as any)

    const tab1 = state.tabs.find((t: any) => t.id === 'tab1')
    const tab2 = state.tabs.find((t: any) => t.id === 'tab2')
    expect(tab1.permissionDenied).not.toBeNull()
    expect(tab2.permissionDenied).toBeNull()
  })

  it('ignores engine_status on bare (non-compound) keys', () => {
    const { state, slice } = buildHarness()

    // Bare tabId — handler should return early via the !key.includes(':') guard.
    slice.handleEngineEvent('tab1', {
      type: 'engine_status',
      fields: {
        label: 'engine',
        state: 'idle',
        model: 'sonnet',
        contextPercent: 0,
        contextWindow: 200000,
        permissionDenials: [
          { toolName: 'AskUserQuestion', toolUseId: 'tu-1', toolInput: { question: 'q?' } },
        ],
      },
    } as any)

    const tab = state.tabs.find((t: any) => t.id === 'tab1')
    expect(tab.permissionDenied).toBeNull()
  })
})
