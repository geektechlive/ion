/**
 * iOS parity (Bug #1): the resolved permission mode must reach iOS via the
 * snapshot projection.
 *
 * Bug #1 fix establishes `permissionMode='plan'` on the active conversation
 * instance — engine-side via the routing-binding fix (so the entry event is no
 * longer dropped) and, defense-in-depth, renderer-side from the
 * engine_plan_proposal arm. The snapshot poller resolves the active instance's
 * permissionMode (snapshot.ts) and projectRendererTab projects it onto the
 * RemoteTabState the iOS app reads. This pins that 'plan' is carried through to
 * iOS (so the iOS pill shows Plan and group placement matches the desktop),
 * and that 'auto' / absent map to 'auto'.
 */

import { describe, it, expect } from 'vitest'
import { projectRendererTab } from '../snapshot-project'

const BASE = { lastMessage: null, permissionQueue: [] }

describe('Bug #1 iOS parity: projectRendererTab projects resolved permissionMode', () => {
  it("projects 'plan' to iOS when the resolved instance mode is plan", () => {
    const result = projectRendererTab(
      { id: 't1', title: 'T', status: 'completed', engineProfileId: null, permissionMode: 'plan' },
      BASE,
    )
    expect(result.permissionMode).toBe('plan')
  })

  it("projects 'auto' when the resolved instance mode is auto", () => {
    const result = projectRendererTab(
      { id: 't1', title: 'T', status: 'idle', engineProfileId: null, permissionMode: 'auto' },
      BASE,
    )
    expect(result.permissionMode).toBe('auto')
  })

  it("defaults to 'auto' when permissionMode is absent", () => {
    const result = projectRendererTab(
      { id: 't1', title: 'T', status: 'idle', engineProfileId: null },
      BASE,
    )
    expect(result.permissionMode).toBe('auto')
  })
})
