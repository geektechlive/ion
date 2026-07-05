/**
 * Running-transition auto-group-move (planning / in-progress).
 *
 * SYMMETRY WITH THE DONE-MOVE: the done-move relocates a tab to the DONE group
 * on a running→idle transition (event-slice-status-change-auto-move.test.ts).
 * This is its missing counterpart: a tab that transitions INTO running — via
 * ANY path that reaches handleStatusChange, including session resume, engine
 * relaunch + re-activation, and reconnect — must relocate to its planning
 * (plan mode) or in-progress (auto mode) group, so a running tab is never
 * stranded in the DONE group.
 *
 * Before the fix the planning/in-progress move lived only inside the send
 * actions (applySendAutoGroupMove), so a tab driven back to running by a
 * non-send path never re-evaluated its group. The canonical bug: a tab that
 * completed in auto mode (auto-moved to DONE) and then resumed — it ran while
 * sitting in DONE.
 *
 * Each test that asserts a move would go RED if the maybeScheduleRunningMove
 * call is removed from handleStatusChange (proves it pins the fix, not
 * pre-existing send-path behavior).
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

const cancelDoneGroupMove = vi.fn(() => false)

vi.mock('../session-store-helpers', () => ({
  nextMsgId: vi.fn(() => 'mock-msg-id'),
  playNotificationIfHidden: vi.fn(async () => {}),
  totalInputTokens: vi.fn(() => 0),
  scheduleDoneGroupMove: vi.fn(),
  cancelDoneGroupMove: (...args: unknown[]) => cancelDoneGroupMove(...(args as [])),
}))

vi.mock('../../preferences', () => ({
  usePreferencesStore: { getState: () => prefs },
}))

import { createEventSlice } from '../slices/event-slice'
import type { State } from '../session-store-types'
import { seedMainPane } from './helpers/conversation-test-helpers'

function makeTab(overrides: Record<string, unknown> = {}) {
  return {
    id: 'tab1',
    title: 'Conversation',
    engineProfileId: null,
    workingDirectory: '/tmp',
    hasChosenDirectory: true,
    pillIcon: null,
    // Start the tab in the DONE group — the stranded-tab scenario.
    groupId: 'group-done',
    groupPinned: false,
    status: 'idle' as const,
    customTitle: null,
    pillColor: null,
    permissionMode: 'auto' as const,
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

function buildHarness(opts: {
  tabOverrides?: Record<string, unknown>
  instanceMode?: 'auto' | 'plan'
} = {}) {
  const moveTabToGroup = vi.fn()
  const state: any = {
    activeTabId: 'tab1',
    isExpanded: true,
    tabs: [makeTab(opts.tabOverrides)],
    conversationPanes: seedMainPane('tab1', {
      permissionMode: opts.instanceMode ?? 'auto',
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

describe('event-slice — running-transition auto-move (planning / in-progress)', () => {
  it('moves an auto-mode tab from DONE to in-progress on idle→running', () => {
    const { slice, moveTabToGroup } = buildHarness()
    // A resume/relaunch/reconnect surfaces as handleStatusChange(tab, 'running')
    // with a non-running prevStatus.
    slice.handleStatusChange!('tab1', 'running', 'idle')
    expect(moveTabToGroup).toHaveBeenCalledWith('tab1', 'group-inprogress')
  })

  it('moves a plan-mode tab from DONE to planning on idle→running', () => {
    const { slice, moveTabToGroup } = buildHarness({ instanceMode: 'plan' })
    slice.handleStatusChange!('tab1', 'running', 'idle')
    expect(moveTabToGroup).toHaveBeenCalledWith('tab1', 'group-planning')
  })

  it('does NOT move on a connecting transition (transient pre-run state)', () => {
    const { slice, moveTabToGroup } = buildHarness()
    slice.handleStatusChange!('tab1', 'connecting', 'idle')
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })

  it('does NOT move a pinned tab', () => {
    const { slice, moveTabToGroup } = buildHarness({ tabOverrides: { groupPinned: true } })
    slice.handleStatusChange!('tab1', 'running', 'idle')
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })

  it('does NOT move when autoGroupMovement is off', () => {
    const original = prefs.autoGroupMovement
    prefs.autoGroupMovement = false
    try {
      const { slice, moveTabToGroup } = buildHarness()
      slice.handleStatusChange!('tab1', 'running', 'idle')
      expect(moveTabToGroup).not.toHaveBeenCalled()
    } finally {
      prefs.autoGroupMovement = original
    }
  })

  it('does NOT move when the tab is already in the in-progress group (idempotent)', () => {
    const { slice, moveTabToGroup } = buildHarness({ tabOverrides: { groupId: 'group-inprogress' } })
    slice.handleStatusChange!('tab1', 'running', 'idle')
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })

  it('cancels a pending done-move on the running transition', () => {
    cancelDoneGroupMove.mockClear()
    const { slice } = buildHarness()
    slice.handleStatusChange!('tab1', 'running', 'idle')
    expect(cancelDoneGroupMove).toHaveBeenCalledWith('tab1')
  })
})
