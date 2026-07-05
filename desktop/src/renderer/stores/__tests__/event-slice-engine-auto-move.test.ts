/**
 * Engine-tab auto-group-movement (#256 regression).
 *
 * The session-key collapse re-activated the normalized event path (event-slice)
 * for engine/extension tabs. The done-move guard read the PARENT
 * `tab.permissionMode`, but engine tabs keep permissionMode on the active
 * instance (the parent is a stale default or sticky 'plan'). Result: an engine
 * tab that had ever entered plan mode never auto-moved to Done on completion.
 *
 * These tests construct the ENGINE-tab variant (engineProfileId set, mode on the
 * instance) — the variant the existing tab-group-pin / send-slice tests never
 * cover (they build CLI tabs, engineProfileId:null). Each fails on the unfixed
 * code (done-move reading updated.permissionMode):
 *   - instance=auto + parent sticky 'plan' → MUST move to Done (failed before).
 *   - instance=plan → MUST NOT move.
 *
 * Reverting event-slice's done-move to `updated.permissionMode === 'auto'` makes
 * the first case go red.
 */

import { describe, it, expect, vi } from 'vitest'

const prefs = {
  expandToolResults: false,
  aiGeneratedTitles: false,
  autoGroupMovement: true,
  tabGroupMode: 'manual',
  doneGroupId: 'group-done',
  inProgressGroupId: 'group-inprogress',
  planningGroupId: 'group-planning',
}

// Capture the scheduled done-move callback so the test can fire it AFTER the
// reducer commits (production uses a 1500ms delay; by then the tab status is
// 'completed'). Firing it synchronously inside the reducer would see the
// pre-commit 'running' status and the re-check would cancel — an artifact of
// zero-delay scheduling, not the behavior under test.
let scheduledMove: (() => void) | null = null

vi.mock('../session-store-helpers', () => ({
  nextMsgId: vi.fn(() => 'mock-msg-id'),
  playNotificationIfHidden: vi.fn(async () => {}),
  totalInputTokens: vi.fn(() => 0),
  scheduleDoneGroupMove: vi.fn((_tabId: string, _delay: number, cb: () => void) => { scheduledMove = cb }),
  cancelDoneGroupMove: vi.fn(() => false),
}))

vi.mock('../../preferences', () => ({
  usePreferencesStore: { getState: () => prefs },
}))

import { createEventSlice } from '../slices/event-slice'
import type { State } from '../session-store-types'
import { seedMainPane } from './helpers/conversation-test-helpers'

function makeEngineTab(overrides: Record<string, unknown> = {}) {
  return {
    id: 'tab1',
    title: 'Engine',
    engineProfileId: 'cos', // ENGINE tab — mode lives on the instance
    workingDirectory: '/tmp',
    hasChosenDirectory: true,
    pillIcon: null,
    groupId: 'group-inprogress',
    groupPinned: false,
    status: 'running' as const,
    customTitle: null,
    pillColor: null,
    permissionMode: 'plan' as const, // parent ghost field — sticky 'plan'
    queuedPrompts: [],
    historicalSessionIds: [],
    conversationId: 'conv-1',
    lastKnownSessionId: 'conv-1',
    lastResult: null,
    sessionTools: [],
    sessionMcpServers: [],
    sessionSkills: [],
    sessionVersion: '',
    activeRequestId: 'req-1',
    currentActivity: 'Writing...',
    lastEventAt: 0,
    isCompacting: false,
    hasUnread: false,
    ...overrides,
  }
}

function buildHarness(instanceMode: 'auto' | 'plan') {
  const moveTabToGroup = vi.fn()
  const state: any = {
    activeTabId: 'tab1',
    isExpanded: true,
    tabs: [makeEngineTab()],
    conversationPanes: seedMainPane('tab1', {
      permissionMode: instanceMode, // AUTHORITATIVE for engine tabs
      sessionModel: 'mock-model',
    }),
    backend: 'api',
    moveTabToGroup,
    submit: vi.fn(),
  }
  const set = (partial: any) => {
    const patch = typeof partial === 'function' ? partial(state) : partial
    Object.assign(state, patch)
  }
  const get = () => state as State
  const slice = createEventSlice(set, get) as State
  return { state, slice, moveTabToGroup }
}

describe('event-slice — engine-tab auto-move on completion (#256)', () => {
  it('moves an auto-mode engine tab to Done even when the parent is sticky-plan', () => {
    // instance=auto (authoritative), parent.permissionMode='plan' (sticky ghost).
    scheduledMove = null
    const { slice, moveTabToGroup } = buildHarness('auto')
    slice.handleNormalizedEvent!('tab1', { type: 'task_complete' } as any)
    // The done-move was scheduled (guard passed); fire it now that status committed.
    expect(scheduledMove).not.toBeNull()
    scheduledMove!()
    expect(moveTabToGroup).toHaveBeenCalledWith('tab1', 'group-done')
  })

  it('does NOT move a plan-mode engine tab to Done', () => {
    // instance=plan → the conversation is awaiting plan approval, not done.
    scheduledMove = null
    const { slice, moveTabToGroup } = buildHarness('plan')
    slice.handleNormalizedEvent!('tab1', { type: 'task_complete' } as any)
    // Guard fails for plan mode → nothing scheduled, nothing moved.
    expect(scheduledMove).toBeNull()
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })
})
