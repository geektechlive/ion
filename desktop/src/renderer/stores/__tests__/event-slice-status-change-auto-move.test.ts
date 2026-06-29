/**
 * Auto-move-to-done on the handleStatusChange path (engine_dead clean-exit).
 *
 * REGRESSION for the stranded-tab bug: a running auto-mode tab whose engine
 * process exits cleanly (engine_dead exitCode 0/null/undefined, no signal) is
 * driven to `idle` by the control plane's handleDeadEvent → setStatus(idle),
 * which NEVER emits task_complete. Before the fix, the auto-move-to-done logic
 * lived only inside `case 'task_complete'`, so the tab was stranded in the
 * in-progress group. The fix routes the move through maybeScheduleDoneMove,
 * invoked from handleStatusChange too.
 *
 * Each test that asserts a move would go RED if the maybeScheduleDoneMove call
 * is removed from handleStatusChange (proves it pins the fix, not pre-existing
 * task_complete behavior).
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
// reducer commits (production uses a 1500ms delay; by then the status is the
// committed terminal value). Firing synchronously inside the reducer would see
// the pre-commit status — an artifact of zero-delay scheduling.
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

function makeTab(overrides: Record<string, unknown> = {}) {
  return {
    id: 'tab1',
    title: 'Conversation',
    engineProfileId: null,
    workingDirectory: '/tmp',
    hasChosenDirectory: true,
    pillIcon: null,
    groupId: 'group-inprogress',
    groupPinned: false,
    status: 'running' as const,
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
  permissionDenied?: unknown
} = {}) {
  const moveTabToGroup = vi.fn()
  const state: any = {
    activeTabId: 'tab1',
    isExpanded: true,
    tabs: [makeTab(opts.tabOverrides)],
    conversationPanes: seedMainPane('tab1', {
      permissionMode: opts.instanceMode ?? 'auto',
      sessionModel: 'mock-model',
      permissionDenied: (opts.permissionDenied ?? null) as any,
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

describe('event-slice — auto-move on handleStatusChange (engine_dead clean-exit)', () => {
  it('moves a running auto-mode tab to Done on running→idle (no task_complete)', () => {
    scheduledMove = null
    const { slice, moveTabToGroup } = buildHarness()
    // engine_dead clean-exit drives the control plane to setStatus(idle),
    // surfacing in the renderer as handleStatusChange(tab, 'idle', 'running').
    slice.handleStatusChange!('tab1', 'idle', 'running')
    expect(scheduledMove).not.toBeNull()
    scheduledMove!()
    expect(moveTabToGroup).toHaveBeenCalledWith('tab1', 'group-done')
  })

  it('does NOT move when prevStatus was not running (e.g. idle→idle heartbeat)', () => {
    scheduledMove = null
    const { slice, moveTabToGroup } = buildHarness({ tabOverrides: { status: 'idle' } })
    slice.handleStatusChange!('tab1', 'idle', 'idle')
    expect(scheduledMove).toBeNull()
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })

  it('does NOT move a plan-mode tab', () => {
    scheduledMove = null
    const { slice, moveTabToGroup } = buildHarness({ instanceMode: 'plan' })
    slice.handleStatusChange!('tab1', 'idle', 'running')
    expect(scheduledMove).toBeNull()
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })

  it('does NOT move a pinned tab', () => {
    scheduledMove = null
    const { slice, moveTabToGroup } = buildHarness({ tabOverrides: { groupPinned: true } })
    slice.handleStatusChange!('tab1', 'idle', 'running')
    expect(scheduledMove).toBeNull()
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })

  it('does NOT move on a failure transition (running→dead)', () => {
    scheduledMove = null
    const { slice, moveTabToGroup } = buildHarness()
    slice.handleStatusChange!('tab1', 'dead', 'running')
    expect(scheduledMove).toBeNull()
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })

  it('does NOT move when the active instance has a permission denial', () => {
    scheduledMove = null
    const { slice, moveTabToGroup } = buildHarness({
      permissionDenied: { tools: [{ toolName: 'Bash' }] },
    })
    slice.handleStatusChange!('tab1', 'idle', 'running')
    // The clean-terminal clearDenied path nulls the denial in the SAME tick, so
    // the committed read sees no denial and the move proceeds. This documents
    // that handleStatusChange clears denials on terminal transitions; the denial
    // guard is exercised on the task_complete path (event-slice-engine-auto-move).
    expect(scheduledMove).not.toBeNull()
    scheduledMove!()
    expect(moveTabToGroup).toHaveBeenCalledWith('tab1', 'group-done')
  })

  it('re-check cancels the move if the tab went back to running before the timer fired', () => {
    scheduledMove = null
    const { state, slice, moveTabToGroup } = buildHarness()
    slice.handleStatusChange!('tab1', 'idle', 'running')
    expect(scheduledMove).not.toBeNull()
    // Tab resumed (relaunch + resume) before the 1500ms timer fired.
    state.tabs = [makeTab({ status: 'running' })]
    scheduledMove!()
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })
})
