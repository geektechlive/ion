/**
 * engine-event-slice — `engine_harness_message` dedup convention
 *
 * The engine carries an opaque `metadata` map on `engine_harness_message`
 * events. The harness sets `metadata.dedupKey` when it wants the renderer
 * to suppress repeated emissions of the "same logical message" within an
 * engine-instance scrollback. The engine treats `metadata` as opaque
 * pass-through; this slice is where the dedup convention is enforced.
 *
 * Concrete motivating case: ion-meta emits a welcome on every
 * `session_start`. The filesystem-based freshness check in
 * fresh-session.ts can't suppress the welcome across an app restart with
 * no intervening user turn (the engine has nothing to persist for a
 * zero-turn conversation, so the freshness signal stays "fresh"). The
 * renderer is the safety net.
 *
 * These tests pin three behaviors:
 *
 *   1. Two events with identical `metadata.dedupKey` produce exactly one
 *      message in scrollback (second emission is dropped).
 *   2. Two events with different `dedupKey` both push (different logical
 *      messages, no dedup expected).
 *   3. Two events with no `metadata` (bare harness messages) both push —
 *      omitting the field is the opt-out signal.
 */

import { describe, it, expect, vi } from 'vitest'

vi.mock('../session-store-helpers', () => ({
  makeLocalTab: vi.fn(),
  nextMsgId: (() => {
    let n = 0
    return vi.fn(() => `mock-msg-${++n}`)
  })(),
  playNotificationIfHidden: vi.fn(async () => {}),
}))

import { createEngineEventSlice } from '../slices/engine-event-slice'
import type { State } from '../session-store-types'
import type { Message } from '../../../shared/types-session'

function buildHarness() {
  const state: any = {
    tabs: [{ id: 'tab1', isEngine: true, lastEventAt: 0 }],
    engineAgentStates: new Map(),
    engineStatusFields: new Map(),
    engineWorkingMessages: new Map(),
    engineNotifications: new Map(),
    engineDialogs: new Map(),
    enginePinnedPrompt: new Map(),
    engineUsage: new Map(),
    engineMessages: new Map<string, Message[]>(),
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

describe('engine_harness_message dedupKey convention', () => {
  it('drops the second emission when dedupKey matches a prior harness message', () => {
    const { state, slice } = buildHarness()
    const key = 'tab1:inst1'

    slice.handleEngineEvent(key, {
      type: 'engine_harness_message',
      message: 'Welcome to Ion Meta',
      source: 'ion-meta',
      metadata: { dedupKey: 'ion-meta:welcome' },
    } as any)

    slice.handleEngineEvent(key, {
      type: 'engine_harness_message',
      message: 'Welcome to Ion Meta',
      source: 'ion-meta',
      metadata: { dedupKey: 'ion-meta:welcome' },
    } as any)

    const msgs = state.engineMessages.get(key) ?? []
    expect(msgs).toHaveLength(1)
    expect(msgs[0].role).toBe('harness')
    expect(msgs[0].content).toBe('Welcome to Ion Meta')
    expect(msgs[0].dedupKey).toBe('ion-meta:welcome')
  })

  it('pushes both emissions when dedupKey values differ', () => {
    const { state, slice } = buildHarness()
    const key = 'tab1:inst1'

    slice.handleEngineEvent(key, {
      type: 'engine_harness_message',
      message: 'first',
      metadata: { dedupKey: 'ext:msg-a' },
    } as any)

    slice.handleEngineEvent(key, {
      type: 'engine_harness_message',
      message: 'second',
      metadata: { dedupKey: 'ext:msg-b' },
    } as any)

    const msgs = state.engineMessages.get(key) ?? []
    expect(msgs).toHaveLength(2)
    expect(msgs[0].dedupKey).toBe('ext:msg-a')
    expect(msgs[1].dedupKey).toBe('ext:msg-b')
  })

  it('pushes both emissions when metadata/dedupKey is absent (opt-out)', () => {
    const { state, slice } = buildHarness()
    const key = 'tab1:inst1'

    // Case A: no metadata field at all.
    slice.handleEngineEvent(key, {
      type: 'engine_harness_message',
      message: 'bare 1',
    } as any)

    // Case B: metadata present but no dedupKey inside it.
    slice.handleEngineEvent(key, {
      type: 'engine_harness_message',
      message: 'bare 2',
      metadata: { someOtherHint: 'foo' },
    } as any)

    const msgs = state.engineMessages.get(key) ?? []
    expect(msgs).toHaveLength(2)
    // Neither message persists a dedupKey since none was supplied.
    expect(msgs[0].dedupKey).toBeUndefined()
    expect(msgs[1].dedupKey).toBeUndefined()
  })
})
