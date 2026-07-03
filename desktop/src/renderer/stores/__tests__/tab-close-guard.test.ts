/**
 * Unit tests for the pure close-guard predicate extracted from tab-slice.ts.
 *
 * evaluateCloseGuard is TAB-TYPE-AGNOSTIC by construction: it reads only
 * per-instance statusFields.state + agentStates and has no notion of tab type.
 * These tests pin the fold arithmetic at the pure-function seam; the closeTab
 * action wiring (which uses this) is covered by tab-slice-close-guard.test.ts.
 */

import { describe, it, expect } from 'vitest'
import { evaluateCloseGuard, formatCloseGuardRefusal } from '../slices/tab-close-guard'

describe('evaluateCloseGuard', () => {
  it('does not block when pane is null/undefined (nothing to protect)', () => {
    expect(evaluateCloseGuard(null).blocked).toBe(false)
    expect(evaluateCloseGuard(undefined).blocked).toBe(false)
  })

  it('does not block when there are no instances', () => {
    expect(evaluateCloseGuard({ instances: [] }).blocked).toBe(false)
  })

  it('does not block a quiescent instance (idle, no running agents)', () => {
    const r = evaluateCloseGuard({ instances: [{ id: 'main', statusFields: { state: 'idle' }, agentStates: [] }] })
    expect(r.blocked).toBe(false)
    expect(r.orchestratorRunning).toBe(false)
  })

  it('blocks when the orchestrator is running / connecting / starting', () => {
    for (const state of ['running', 'connecting', 'starting']) {
      const r = evaluateCloseGuard({ instances: [{ id: 'main', statusFields: { state }, agentStates: [] }] })
      expect(r.blocked, state).toBe(true)
      expect(r.orchestratorRunning, state).toBe(true)
    }
  })

  it('blocks when a background child agent is running (orchestrator idle)', () => {
    const r = evaluateCloseGuard({
      instances: [{ id: 'main', statusFields: { state: 'idle' }, agentStates: [{ status: 'done' }, { status: 'running' }] }],
    })
    expect(r.blocked).toBe(true)
    expect(r.orchestratorRunning).toBe(false)
    expect(r.childCounts).toEqual([{ id: 'main', count: 1 }])
  })

  it('blocks when a sibling instance has a running child even if the active one is idle', () => {
    const r = evaluateCloseGuard({
      instances: [
        { id: 'inst1', statusFields: { state: 'idle' }, agentStates: [] },
        { id: 'inst2', statusFields: { state: 'idle' }, agentStates: [{ status: 'running' }] },
      ],
    })
    expect(r.blocked).toBe(true)
  })

  it('does not block when every agent is terminal', () => {
    const r = evaluateCloseGuard({
      instances: [{ id: 'main', statusFields: { state: 'idle' }, agentStates: [{ status: 'done' }, { status: 'error' }, { status: 'cancelled' }] }],
    })
    expect(r.blocked).toBe(false)
  })
})

describe('formatCloseGuardRefusal', () => {
  it('includes the tab id, orchestrator flag, and per-instance counts', () => {
    const r = evaluateCloseGuard({ instances: [{ id: 'main-abc', statusFields: { state: 'idle' }, agentStates: [{ status: 'running' }] }] })
    const msg = formatCloseGuardRefusal('tab1-deadbeef', r)
    expect(msg).toContain('refused tab close')
    expect(msg).toContain('orchestratorRunning=false')
    expect(msg).toContain('main-a:1')
  })
})
