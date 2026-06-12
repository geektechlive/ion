/**
 * engine-event-slice — resource subsystem event routing
 *
 * Pins the contract that engine_resource_snapshot and engine_resource_delta
 * are handled for BOTH global (key="") and session-scoped (key="tab:inst")
 * events. The earlier bug: both types were in handleMessageEvents, which is
 * only reached after the `if (!key.includes(':')) return` guard. Global
 * resources arrived with key="" and were silently dropped.
 *
 * Also pins the notification-panel visibility contract (regression for the
 * d306b769 / global-notifications-empty bug):
 *   - The store holds all items flat, keyed by kind.
 *   - NotificationsPanel renders only items without conversationId.
 *   - A snapshot containing mixed global + conversation items must preserve
 *     the global items so the panel is not empty after delivery.
 *
 * Tests:
 *   - engine_resource_snapshot with key="" updates the store (global path)
 *   - engine_resource_snapshot with key="tab:inst" updates the store (session path)
 *   - engine_resource_delta create with key="" updates the store (global path)
 *   - engine_resource_delta create with key="tab:inst" updates the store (session path)
 *   - engine_resource_delta update mutates the correct item
 *   - engine_resource_delta delete removes the correct item
 *   - engine_resource_delta mark_read adds item id to readResourceIds
 *   - applyResourceSnapshot merges read=true items into readResourceIds
 *   - engine_notification with key="" is handled (does not throw, returns)
 *   - engine_notification with key="tab:inst" is handled (does not throw, returns)
 *   - mixed snapshot: global items (no conversationId) survive the panel filter
 */

import { describe, it, expect, vi } from 'vitest'

vi.mock('../session-store-helpers', () => ({
  makeLocalTab: vi.fn(),
  nextMsgId: vi.fn(() => 'mock-msg-id'),
  playNotificationIfHidden: vi.fn(async () => {}),
}))

import { handleCrossEngineEvent } from '../slices/engine-event-slice-messages'
import type { ResourceItem } from '../../../shared/types-engine'

// ── Minimal state shape required by the resource handlers ──────────────────

function makeResourceState() {
  const state: any = {
    resources: {} as Record<string, ResourceItem[]>,
    resourceSubscriptions: {} as Record<string, string>,
    readResourceIds: new Set<string>(),
  }
  const set = (partial: any) => {
    const patch = typeof partial === 'function' ? partial(state) : partial
    Object.assign(state, patch)
  }
  return { state, set }
}

function makeItem(overrides: Partial<ResourceItem> = {}): ResourceItem {
  return {
    id: 'item-1',
    kind: 'briefing',
    content: 'Hello world',
    createdAt: '2026-01-01T00:00:00.000Z',
    ...overrides,
  }
}

// ── Snapshot tests ─────────────────────────────────────────────────────────

describe('engine_resource_snapshot', () => {
  it('populates the store when key is "" (global scope)', () => {
    const { state, set } = makeResourceState()
    const item = makeItem({ id: 'g-1', kind: 'briefing' })

    const handled = handleCrossEngineEvent(set, () => state, '', {
      type: 'engine_resource_snapshot',
      resourceKind: 'briefing',
      resourceSubId: 'sub-global',
      resourceItems: [item],
    })

    expect(handled).toBe(true)
    expect(state.resources['briefing']).toHaveLength(1)
    expect(state.resources['briefing'][0].id).toBe('g-1')
    expect(state.resourceSubscriptions['briefing']).toBe('sub-global')
  })

  it('populates the store when key is "tab1:inst1" (session scope)', () => {
    const { state, set } = makeResourceState()
    const item = makeItem({ id: 's-1', kind: 'briefing', conversationId: 'conv-abc' })

    const handled = handleCrossEngineEvent(set, () => state, 'tab1:inst1', {
      type: 'engine_resource_snapshot',
      resourceKind: 'briefing',
      resourceSubId: 'sub-session',
      resourceItems: [item],
    })

    expect(handled).toBe(true)
    expect(state.resources['briefing']).toHaveLength(1)
    expect(state.resources['briefing'][0].conversationId).toBe('conv-abc')
    expect(state.resourceSubscriptions['briefing']).toBe('sub-session')
  })

  it('replaces the entire collection when incoming count equals or exceeds existing', () => {
    const { state, set } = makeResourceState()
    // Prime with two items.
    state.resources['briefing'] = [makeItem({ id: 'old-1' }), makeItem({ id: 'old-2' })]

    // Snapshot with same count: replaces (full snapshot semantics).
    handleCrossEngineEvent(set, () => state, '', {
      type: 'engine_resource_snapshot',
      resourceKind: 'briefing',
      resourceSubId: 'sub-replace',
      resourceItems: [makeItem({ id: 'new-1' }), makeItem({ id: 'new-2' })],
    })

    expect(state.resources['briefing']).toHaveLength(2)
    expect(state.resources['briefing'].map((i: ResourceItem) => i.id)).toEqual(['new-1', 'new-2'])
  })

  it('merges when partial snapshot has fewer items than existing (protects disk-seeded items)', () => {
    const { state, set } = makeResourceState()
    // Prime with three items (simulating disk cold-load).
    state.resources['briefing'] = [
      makeItem({ id: 'old-1' }),
      makeItem({ id: 'old-2' }),
      makeItem({ id: 'old-3' }),
    ]

    // Partial snapshot (e.g. extension respawn with fresh in-memory state, only 1 item known).
    handleCrossEngineEvent(set, () => state, '', {
      type: 'engine_resource_snapshot',
      resourceKind: 'briefing',
      resourceSubId: 'sub-partial',
      resourceItems: [makeItem({ id: 'old-2', content: 'updated content' })],
    })

    // All 3 items survive. old-2 is updated (incoming wins on conflict).
    expect(state.resources['briefing']).toHaveLength(3)
    const ids = state.resources['briefing'].map((i: ResourceItem) => i.id)
    expect(ids).toContain('old-1')
    expect(ids).toContain('old-2')
    expect(ids).toContain('old-3')
    const updated = state.resources['briefing'].find((i: ResourceItem) => i.id === 'old-2')
    expect(updated?.content).toBe('updated content')
  })

  it('merges read=true items from snapshot into readResourceIds', () => {
    const { state, set } = makeResourceState()
    const readItem = makeItem({ id: 'already-read', read: true })
    const unreadItem = makeItem({ id: 'unread' })

    handleCrossEngineEvent(set, () => state, '', {
      type: 'engine_resource_snapshot',
      resourceKind: 'briefing',
      resourceSubId: 'sub-read-merge',
      resourceItems: [readItem, unreadItem],
    })

    expect(state.readResourceIds.has('already-read')).toBe(true)
    expect(state.readResourceIds.has('unread')).toBe(false)
  })

  it('handles empty resourceItems without throwing', () => {
    const { state, set } = makeResourceState()
    expect(() => {
      handleCrossEngineEvent(set, () => state, '', {
        type: 'engine_resource_snapshot',
        resourceKind: 'briefing',
        resourceSubId: 'sub-empty',
        resourceItems: [],
      })
    }).not.toThrow()
    expect(state.resources['briefing']).toHaveLength(0)
  })
})

// ── Delta tests ────────────────────────────────────────────────────────────

describe('engine_resource_delta', () => {
  it('create delta with key="" adds item to the store (global path)', () => {
    const { state, set } = makeResourceState()

    const handled = handleCrossEngineEvent(set, () => state, '', {
      type: 'engine_resource_delta',
      resourceKind: 'briefing',
      resourceSubId: 'sub-delta-global',
      resourceDelta: {
        op: 'create',
        item: makeItem({ id: 'created-global' }),
      },
    })

    expect(handled).toBe(true)
    expect(state.resources['briefing']).toHaveLength(1)
    expect(state.resources['briefing'][0].id).toBe('created-global')
  })

  it('create delta with key="tab1:inst1" adds item to the store (session path)', () => {
    const { state, set } = makeResourceState()

    const handled = handleCrossEngineEvent(set, () => state, 'tab1:inst1', {
      type: 'engine_resource_delta',
      resourceKind: 'briefing',
      resourceSubId: 'sub-delta-session',
      resourceDelta: {
        op: 'create',
        item: makeItem({ id: 'created-session', conversationId: 'conv-xyz' }),
      },
    })

    expect(handled).toBe(true)
    expect(state.resources['briefing']).toHaveLength(1)
    expect(state.resources['briefing'][0].conversationId).toBe('conv-xyz')
  })

  it('update delta mutates the matching item in-place', () => {
    const { state, set } = makeResourceState()
    state.resources['briefing'] = [
      makeItem({ id: 'item-to-update', title: 'Old Title' }),
      makeItem({ id: 'untouched' }),
    ]

    handleCrossEngineEvent(set, () => state, '', {
      type: 'engine_resource_delta',
      resourceKind: 'briefing',
      resourceSubId: 'sub-update',
      resourceDelta: {
        op: 'update',
        item: makeItem({ id: 'item-to-update', title: 'New Title' }),
      },
    })

    const updated = state.resources['briefing'].find((i: ResourceItem) => i.id === 'item-to-update')
    expect(updated?.title).toBe('New Title')
    const untouched = state.resources['briefing'].find((i: ResourceItem) => i.id === 'untouched')
    expect(untouched?.id).toBe('untouched')
  })

  it('delete delta removes the matching item', () => {
    const { state, set } = makeResourceState()
    state.resources['briefing'] = [
      makeItem({ id: 'to-delete' }),
      makeItem({ id: 'to-keep' }),
    ]

    handleCrossEngineEvent(set, () => state, '', {
      type: 'engine_resource_delta',
      resourceKind: 'briefing',
      resourceSubId: 'sub-delete',
      resourceDelta: {
        op: 'delete',
        item: makeItem({ id: 'to-delete' }),
      },
    })

    expect(state.resources['briefing']).toHaveLength(1)
    expect(state.resources['briefing'][0].id).toBe('to-keep')
  })

  it('mark_read delta sets read=true on the item and adds to readResourceIds', () => {
    const { state, set } = makeResourceState()
    state.resources['briefing'] = [makeItem({ id: 'mark-me' })]

    handleCrossEngineEvent(set, () => state, '', {
      type: 'engine_resource_delta',
      resourceKind: 'briefing',
      resourceSubId: 'sub-mark-read',
      resourceDelta: {
        op: 'mark_read',
        item: makeItem({ id: 'mark-me' }),
      },
    })

    const item = state.resources['briefing'][0]
    expect(item.read).toBe(true)
    expect(state.readResourceIds.has('mark-me')).toBe(true)
  })

  it('does nothing when resourceDelta is absent', () => {
    const { state, set } = makeResourceState()

    const handled = handleCrossEngineEvent(set, () => state, '', {
      type: 'engine_resource_delta',
      resourceKind: 'briefing',
      resourceSubId: 'sub-no-delta',
      // no resourceDelta field
    })

    // Still handled (returns true) but store is unchanged.
    expect(handled).toBe(true)
    expect(state.resources['briefing']).toBeUndefined()
  })
})

// ── Notification panel filter regression ──────────────────────────────────
//
// Pins the contract broken in d306b769 (global notifications empty bug).
//
// NotificationsPanel renders items filtered by !item.conversationId.
// A per-session snapshot may contain both global items (no conversationId)
// and conversation-scoped items (with conversationId). After the snapshot,
// the panel must show at least the global items.

describe('notification panel visibility — global items survive mixed snapshot', () => {
  it('global items (no conversationId) pass the panel filter after a mixed snapshot', () => {
    const { state, set } = makeResourceState()

    const globalItem = makeItem({ id: 'global-1', kind: 'briefing' }) // no conversationId
    const convItem = makeItem({ id: 'conv-1', kind: 'briefing', conversationId: 'conv-abc' })

    handleCrossEngineEvent(set, () => state, 'tab1:inst1', {
      type: 'engine_resource_snapshot',
      resourceKind: 'briefing',
      resourceSubId: 'sub-mixed',
      resourceItems: [globalItem, convItem],
    })

    // Store has both items.
    expect(state.resources['briefing']).toHaveLength(2)

    // Simulate the NotificationsPanel filter: only show items without conversationId.
    const panelItems = (state.resources['briefing'] as ResourceItem[]).filter(
      (item) => !item.conversationId,
    )
    expect(panelItems).toHaveLength(1)
    expect(panelItems[0].id).toBe('global-1')
  })

  it('an empty snapshot does NOT wipe existing items (disk-seed guard)', () => {
    // Multiple sessions fire engine_resource_snapshot on connect. If the
    // extension's HandleQuery fails (subprocess died) or the subscription
    // races with extension init, the snapshot arrives with 0 items. The
    // disk-seed injects persisted items into the store, but subsequent
    // empty snapshots from other sessions would wipe them without the
    // guard in applyResourceSnapshot.
    const { state, set } = makeResourceState()

    // Prime with real data from disk-seed injection.
    state.resources['briefing'] = [
      makeItem({ id: 'real-1' }),
      makeItem({ id: 'real-2' }),
    ]

    // SubscribeDirect delivers empty snapshot (Items: nil → []).
    handleCrossEngineEvent(set, () => state, '', {
      type: 'engine_resource_snapshot',
      resourceKind: 'briefing',
      resourceSubId: 'sub-direct-empty',
      resourceItems: [],
    })

    // Empty snapshot must NOT wipe the existing items.
    expect(state.resources['briefing']).toHaveLength(2)
    expect(state.resources['briefing'][0].id).toBe('real-1')
  })
})

describe('engine_notification', () => {
  it('is handled and returns true when key is "" (global)', () => {
    const { state, set } = makeResourceState()
    expect(() => {
      const handled = handleCrossEngineEvent(set, () => state, '', {
        type: 'engine_notification',
        push: true,
        notifyKind: 'briefing',
        notifyTitle: 'New briefing ready',
        notifyBody: 'Your daily summary is available.',
      })
      expect(handled).toBe(true)
    }).not.toThrow()
  })

  it('is handled and returns true when key is "tab1:inst1" (session)', () => {
    const { state, set } = makeResourceState()
    expect(() => {
      const handled = handleCrossEngineEvent(set, () => state, 'tab1:inst1', {
        type: 'engine_notification',
        push: false,
        notifyKind: 'briefing',
        notifyTitle: 'Session alert',
        notifyBody: 'Something happened in this session.',
      })
      expect(handled).toBe(true)
    }).not.toThrow()
  })
})
