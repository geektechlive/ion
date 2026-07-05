/**
 * Engine-tab send-side auto-group-movement (#256 regression).
 *
 * Before this fix, only the CLI send paths (sendMessage / submitRemotePrompt)
 * moved a tab to planning/in-progress on send; submitEnginePrompt (the engine-tab
 * path) did no group movement at all, so engine tabs never moved on send. The fix
 * extracts the shared applySendAutoGroupMove action — which reads the AUTHORITATIVE
 * per-tab mode (effectivePermissionMode: instance for ALL tab types) — and calls it
 * from every send path including submitEnginePrompt.
 *
 * These tests pin applySendAutoGroupMove for BOTH the engine-tab variant AND the
 * plain-tab variant after WI-002 (permissionMode unified onto the instance).
 * The "CLI uses parent mode" case is gone — both tab types now use the instance.
 */

import { describe, it, expect, vi } from 'vitest'

const prefs = {
  autoGroupMovement: true,
  tabGroupMode: 'manual',
  planningGroupId: 'group-planning',
  inProgressGroupId: 'group-inprogress',
}

vi.mock('../../preferences', () => ({
  usePreferencesStore: { getState: () => prefs },
}))

vi.mock('../session-store-helpers', () => ({
  nextMsgId: vi.fn(() => 'mock-msg-id'),
  playNotificationIfHidden: vi.fn(async () => {}),
  cancelDoneGroupMove: vi.fn(() => false),
}))

import { createSendSlice } from '../slices/send-slice'
import type { State } from '../session-store-types'
import { seedMainPane } from './helpers/conversation-test-helpers'

function buildHarness(opts: {
  engineProfileId: string | null
  instanceMode: 'auto' | 'plan'
  parentMode?: 'auto' | 'plan'
  groupId?: string | null
  groupPinned?: boolean
}) {
  const moveTabToGroup = vi.fn()
  const state: any = {
    activeTabId: 'tab1',
    tabs: [{
      id: 'tab1',
      engineProfileId: opts.engineProfileId,
      // permissionMode is no longer a tab-level field (WI-002) — it lives on the
      // conversation instance. The parentMode option is kept for backward-compat
      // with the "meta-guard" test below but is NOT on the real TabState shape.
      groupId: opts.groupId ?? null,
      groupPinned: opts.groupPinned ?? false,
      status: 'idle',
    }],
    conversationPanes: seedMainPane('tab1', { permissionMode: opts.instanceMode }),
    moveTabToGroup,
  }
  const set = (partial: any) => {
    const patch = typeof partial === 'function' ? partial(state) : partial
    Object.assign(state, patch)
  }
  const get = () => state as State
  const slice = createSendSlice(set, get) as State
  return { slice, moveTabToGroup }
}

describe('applySendAutoGroupMove — engine-tab variant (#256)', () => {
  it('moves an auto-mode engine tab to in-progress (reads the instance mode)', () => {
    // parent ghost says plan; instance (authoritative) says auto → in-progress.
    const { slice, moveTabToGroup } = buildHarness({
      engineProfileId: 'cos', instanceMode: 'auto', parentMode: 'plan', groupId: 'group-planning',
    })
    slice.applySendAutoGroupMove!('tab1')
    expect(moveTabToGroup).toHaveBeenCalledWith('tab1', 'group-inprogress')
  })

  it('moves a plan-mode engine tab to planning (reads the instance mode)', () => {
    const { slice, moveTabToGroup } = buildHarness({
      engineProfileId: 'cos', instanceMode: 'plan', parentMode: 'auto', groupId: 'group-inprogress',
    })
    slice.applySendAutoGroupMove!('tab1')
    expect(moveTabToGroup).toHaveBeenCalledWith('tab1', 'group-planning')
  })

  it('does not move a pinned engine tab', () => {
    const { slice, moveTabToGroup } = buildHarness({
      engineProfileId: 'cos', instanceMode: 'auto', groupId: 'group-planning', groupPinned: true,
    })
    slice.applySendAutoGroupMove!('tab1')
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })

  it('plain tab reads the INSTANCE mode (not a parent ghost field) (WI-002)', () => {
    // After WI-002: effectivePermissionMode uses the instance for ALL tab types.
    // Instance says 'auto' → moves to in-progress (not planning).
    // Pre-WI-002 behavior was "parent 'plan' → planning" for CLI tabs — that is gone.
    const { slice, moveTabToGroup } = buildHarness({
      engineProfileId: null, instanceMode: 'auto', parentMode: 'plan', groupId: 'group-inprogress',
    })
    slice.applySendAutoGroupMove!('tab1')
    // Instance says 'auto' AND already in in-progress → no move needed.
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })

  it('plain tab in plan mode (instance) moves to planning', () => {
    const { slice, moveTabToGroup } = buildHarness({
      engineProfileId: null, instanceMode: 'plan', groupId: 'group-inprogress',
    })
    slice.applySendAutoGroupMove!('tab1')
    expect(moveTabToGroup).toHaveBeenCalledWith('tab1', 'group-planning')
  })

  it('no-op when already in the target group', () => {
    const { slice, moveTabToGroup } = buildHarness({
      engineProfileId: 'cos', instanceMode: 'auto', groupId: 'group-inprogress',
    })
    slice.applySendAutoGroupMove!('tab1')
    expect(moveTabToGroup).not.toHaveBeenCalled()
  })
})
