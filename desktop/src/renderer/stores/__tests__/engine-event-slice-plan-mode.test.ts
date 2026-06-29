/**
 * WI-001: engine_plan_mode_changed is handled exclusively by handleNormalizedEvent
 *
 * After the single-path collapse (WI-001), Path 1 (handleEngineEvent) is retired.
 * The engine_plan_mode_changed raw event reaches event-slice.ts via the normalized
 * stream (control plane forwards it as `event as any`). handleNormalizedEvent is the
 * single authoritative handler for ALL conversation types (plain and extension).
 *
 * What handleNormalizedEvent does on engine_plan_mode_changed:
 *   - Inserts the "Plan created" divider system message (deduped by planFilePath)
 *   - Updates instance.planFilePath when planModeEnabled=true
 *   - Sets instance.permissionMode = 'plan' (NOT parent tab.permissionMode)
 *
 * WI-001: all tabs use the instance path. The parent tab.permissionMode is never
 * written for plan_mode_changed (sticky-parent invariant).
 */

import { describe, it, expect, vi } from 'vitest'

vi.mock('../session-store-helpers', () => ({
  makeLocalTab: vi.fn(),
  nextMsgId: vi.fn(() => 'mock-msg-id'),
  playNotificationIfHidden: vi.fn(async () => {}),
  totalInputTokens: vi.fn(() => 0),
  scheduleDoneGroupMove: vi.fn(),
}))
vi.mock('../slices/event-slice-titling', () => ({ maybeGenerateTabTitle: vi.fn() }))
vi.mock('../../preferences', () => ({
  usePreferencesStore: { getState: vi.fn(() => ({ expandToolResults: false, aiGeneratedTitles: false })) },
}))
vi.mock('../slices/engine-event-slice-messages', () => ({
  handleCrossNormalizedEvent: vi.fn(() => false),
}))

import { createEventSlice } from '../slices/event-slice'
import { activeInstance } from '../conversation-instance'
import type { State } from '../session-store-types'

function makeInstance(id: string) {
  return {
    id, label: id, messages: [], messageCount: 0, modelOverride: null, sessionModel: null,
    permissionMode: 'auto', permissionDenied: null, permissionQueue: [], elicitationQueue: [],
    conversationIds: [], draftInput: '', agentStates: [],
    statusFields: null, planFilePath: null, thinkingEffort: 'off', sealed: false,
  }
}

function buildHarness() {
  const state: any = {
    tabs: [{
      id: 'tab1',
      engineProfileId: 'test-profile',
      status: 'running',
      lastEventAt: 0,
      permissionMode: undefined, // parent stays ghost (sticky-parent invariant)
      permissionDenied: null,
      contextTokens: 0,
      contextPercent: 0,
      hasUnread: false,
      queuedPrompts: [],
      historicalSessionIds: [],
    }],
    activeTabId: 'tab1',
    isExpanded: false,
    engineWorkingMessages: new Map(),
    engineNotifications: new Map(),
    engineDialogs: new Map(),
    enginePinnedPrompt: new Map(),
    engineUsage: new Map(),
    engineModelFallbacks: new Map(),
    conversationPanes: new Map([['tab1', { instances: [makeInstance('main')], activeInstanceId: 'main' }]]),
  }
  const set = (partial: any) => {
    const patch = typeof partial === 'function' ? partial(state) : partial
    Object.assign(state, patch)
  }
  const get = () => state as State
  const slice = createEventSlice(set, get) as State
  return { state, slice }
}

function getInstance(state: any) {
  return activeInstance(state.conversationPanes, 'tab1')
}

function getMessages(state: any) {
  return getInstance(state)?.messages ?? []
}

describe('engine_plan_mode_changed — handleNormalizedEvent (WI-001 single path)', () => {
  it('inserts a "Plan created" divider system message when planModeEnabled=true', () => {
    const { state, slice } = buildHarness()

    slice.handleNormalizedEvent('tab1', {
      type: 'engine_plan_mode_changed' as any,
      planModeEnabled: true,
      planFilePath: '/tmp/plan.md',
      planSlug: 'my-plan',
    } as any)

    const msgs = getMessages(state)
    expect(msgs.length).toBe(1)
    expect(msgs[0].role).toBe('system')
  })

  it('updates instance.planFilePath when planModeEnabled=true', () => {
    const { state, slice } = buildHarness()

    expect(getInstance(state)?.planFilePath).toBeNull()

    slice.handleNormalizedEvent('tab1', {
      type: 'engine_plan_mode_changed' as any,
      planModeEnabled: true,
      planFilePath: '/workspace/plan.md',
      planSlug: 'my-plan',
    } as any)

    expect(getInstance(state)?.planFilePath).toBe('/workspace/plan.md')
  })

  it('WI-001: sets instance.permissionMode = plan (NOT parent tab)', () => {
    const { state, slice } = buildHarness()
    const parentPermModeBefore = state.tabs[0].permissionMode

    slice.handleNormalizedEvent('tab1', {
      type: 'engine_plan_mode_changed' as any,
      planModeEnabled: true,
      planFilePath: '/workspace/plan.md',
    } as any)

    // Instance gets 'plan'.
    expect(getInstance(state)?.permissionMode).toBe('plan')
    // Parent tab.permissionMode is NOT written.
    expect(state.tabs[0].permissionMode).toBe(parentPermModeBefore)
  })

  it('does NOT update planFilePath when planFilePath is absent', () => {
    const { state, slice } = buildHarness()

    slice.handleNormalizedEvent('tab1', {
      type: 'engine_plan_mode_changed' as any,
      planModeEnabled: true,
      // no planFilePath
    } as any)

    expect(getInstance(state)?.planFilePath).toBeNull()
  })

  it('does NOT insert divider on planModeEnabled=false (proposal path)', () => {
    const { state, slice } = buildHarness()

    slice.handleNormalizedEvent('tab1', {
      type: 'engine_plan_mode_changed' as any,
      planModeEnabled: false,
      planFilePath: '/tmp/plan.md',
    } as any)

    expect(getMessages(state).length).toBe(0)
    expect(getInstance(state)?.planFilePath).toBeNull()
  })

  it('deduplicates divider on session resume (same planFilePath already present)', () => {
    const { state, slice } = buildHarness()

    slice.handleNormalizedEvent('tab1', {
      type: 'engine_plan_mode_changed' as any,
      planModeEnabled: true,
      planFilePath: '/tmp/plan.md',
      planSlug: 'my-plan',
    } as any)
    expect(getMessages(state).length).toBe(1)

    // Second emission (e.g. session resume re-emits plan_mode_changed)
    slice.handleNormalizedEvent('tab1', {
      type: 'engine_plan_mode_changed' as any,
      planModeEnabled: true,
      planFilePath: '/tmp/plan.md',
      planSlug: 'my-plan',
    } as any)

    // No duplicate divider.
    expect(getMessages(state).length).toBe(1)
  })

  it('repeated events with different planFilePaths each insert one divider (no accumulation)', () => {
    const { state, slice } = buildHarness()

    slice.handleNormalizedEvent('tab1', {
      type: 'engine_plan_mode_changed' as any,
      planModeEnabled: true,
      planFilePath: '/tmp/plan-1.md',
      planSlug: 'first',
    } as any)
    slice.handleNormalizedEvent('tab1', {
      type: 'engine_plan_mode_changed' as any,
      planModeEnabled: true,
      planFilePath: '/tmp/plan-2.md',
      planSlug: 'second',
    } as any)

    // Two different plans get two dividers.
    expect(getMessages(state).length).toBe(2)
    // planFilePath reflects the most recent event.
    expect(getInstance(state)?.planFilePath).toBe('/tmp/plan-2.md')
  })
})
