/**
 * Dispatched-children guard for the auto-move-to-done-group (#done-move-race).
 *
 * REGRESSION: An auto-mode orchestrator tab dispatches a background agent and
 * goes idle while that child is still running. The orchestrator's own status
 * transitions running→completed→idle, which previously triggered the done-group
 * move immediately. The child agent hadn't appeared in agentStates yet (the
 * engine_agent_state snapshot arrives up to ~2.6 s after the idle transition in
 * production), so the re-check only saw tab.status=idle and moved the tab.
 *
 * FIX: maybeScheduleDoneMove now folds pane.instances[*].agentStates for
 * status==='running' at BOTH the schedule point and inside the timer re-check.
 * The agent_state snapshot persists in the store from the moment it arrives, so
 * there is no additional race: if children are running, the store already has it.
 *
 * Each "must not move" test goes RED if the hasRunningAgents guard is removed.
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
// reducer commits (production uses a 1500ms delay; by then the tab status is the
// committed terminal value). Firing synchronously inside the reducer would see
// the pre-commit status — an artifact of zero-delay scheduling, not real behavior.
let scheduledMove: (() => void) | null = null

vi.mock('../session-store-helpers', () => ({
  nextMsgId: vi.fn(() => 'mock-msg-id'),
  playNotificationIfHidden: vi.fn(async () => {}),
  totalInputTokens: vi.fn(() => 0),
  scheduleDoneGroupMove: vi.fn((_tabId: string, _delay: number, cb: () => void) => {
    scheduledMove = cb
  }),
  cancelDoneGroupMove: vi.fn(() => false),
}))

vi.mock('../../preferences', () => ({
  usePreferencesStore: { getState: () => prefs },
}))

import { createEventSlice } from '../slices/event-slice'
import type { State } from '../session-store-types'
import { seedMainPane } from './helpers/conversation-test-helpers'
import type { AgentStateUpdate } from '../../../shared/types-engine'

function makeTab(overrides: Record<string, unknown> = {}) {
  return {
    id: 'tab1',
    title: 'Orchestrator',
    engineProfileId: 'cos',
    workingDirectory: '/tmp',
    hasChosenDirectory: true,
    pillIcon: null,
    groupId: 'group-inprogress',
    groupPinned: false,
    status: 'running' as const,
    customTitle: null,
    pillColor: null,
    permissionMode: 'plan' as const, // parent ghost field — sticky 'plan' from prior plan run
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
    currentActivity: 'Dispatching...',
    lastEventAt: 0,
    isCompacting: false,
    hasUnread: false,
    ...overrides,
  }
}

const runningChild: AgentStateUpdate = {
  name: 'dev-lead',
  status: 'running',
  metadata: {},
}

const doneChild: AgentStateUpdate = {
  name: 'dev-lead',
  status: 'done',
  metadata: {},
}

function buildHarness(opts: {
  instanceMode?: 'auto' | 'plan'
  agentStates?: AgentStateUpdate[]
  tabOverrides?: Record<string, unknown>
} = {}) {
  const moveTabToGroup = vi.fn()
  const conversationPanes = seedMainPane('tab1', {
    permissionMode: opts.instanceMode ?? 'auto',
    agentStates: opts.agentStates ?? [],
    sessionModel: 'mock-model',
  })
  const state: any = {
    activeTabId: 'tab1',
    isExpanded: true,
    tabs: [makeTab(opts.tabOverrides)],
    conversationPanes,
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

describe('event-slice-done-move — dispatched-children guard', () => {
  /**
   * (1) REGRESSION — schedule-point guard.
   *
   * Auto-mode orchestrator goes running→idle while a background agent is
   * still running (agentStates has status=running at the moment the transition
   * fires). The done-group move must NOT be scheduled.
   *
   * Goes RED if the hasRunningAgents check is removed from maybeScheduleDoneMove
   * before the scheduleDoneGroupMove call.
   */
  it('does NOT schedule a move when a dispatched child is running at the schedule point', () => {
    scheduledMove = null
    const { slice, moveTabToGroup } = buildHarness({ agentStates: [runningChild] })
    slice.handleStatusChange!('tab1', 'idle', 'running')
    expect(scheduledMove).toBeNull()
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })

  /**
   * (2) REVERSE-ORDERING — re-check guard.
   *
   * The orchestrator goes idle BEFORE the first agent_state snapshot arrives
   * (the normal production ordering). The done-move is scheduled because the
   * store has no running children yet. By the time the timer fires, an
   * agent_state snapshot with status=running has been written to the store.
   * The re-check must cancel the move.
   *
   * Goes RED if the hasRunningAgents check is removed from the timer callback.
   */
  it('cancels in the re-check when a running child appears before the timer fires', () => {
    scheduledMove = null
    // No children at schedule time — guard passes, timer is scheduled.
    const { state, slice, moveTabToGroup } = buildHarness({ agentStates: [] })
    slice.handleStatusChange!('tab1', 'idle', 'running')
    expect(scheduledMove).not.toBeNull()

    // Simulate: agent_state snapshot with a running child arrives before the timer.
    const pane = state.conversationPanes.get('tab1')!
    const updatedInstances = pane.instances.map((inst: any) => ({
      ...inst,
      agentStates: [runningChild],
    }))
    state.conversationPanes = new Map([['tab1', { ...pane, instances: updatedInstances }]])

    // Fire the timer — re-check must cancel.
    scheduledMove!()
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })

  /**
   * (3) CONTROL — clean completion with no running children executes the move.
   *
   * Auto-mode tab completes with no dispatched agents running (agentStates is
   * empty or all children are done). The move must execute.
   */
  it('executes the move when the tab completes with no running children', () => {
    scheduledMove = null
    const { slice, moveTabToGroup } = buildHarness({ agentStates: [] })
    slice.handleStatusChange!('tab1', 'idle', 'running')
    expect(scheduledMove).not.toBeNull()
    scheduledMove!()
    expect(moveTabToGroup).toHaveBeenCalledWith('tab1', 'group-done')
  })

  /**
   * (4) CONTROL variant — all children done (not running) does not block the move.
   */
  it('executes the move when dispatched children are all done', () => {
    scheduledMove = null
    const { slice, moveTabToGroup } = buildHarness({ agentStates: [doneChild] })
    slice.handleStatusChange!('tab1', 'idle', 'running')
    expect(scheduledMove).not.toBeNull()
    scheduledMove!()
    expect(moveTabToGroup).toHaveBeenCalledWith('tab1', 'group-done')
  })
})
