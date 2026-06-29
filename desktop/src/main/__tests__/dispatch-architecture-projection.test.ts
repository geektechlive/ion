/**
 * Dispatch architecture projection test.
 *
 * Feeds the 8-dispatch/2-session fixture (derived from the engine
 * integration test) through the desktop's getDispatches() helper and
 * validates the projection shape that iOS receives via the snapshot.
 *
 * Proves: distinct convIds, distinct dispatch ids, correct model/elapsed
 * per dispatch, continuation shares parent convId, fresh dispatches
 * do not.
 */
import { describe, it, expect } from 'vitest'
import { getDispatches } from '../../renderer/components/agent-panel-helpers'
import {
  sessAAgentStates,
  sessBAgentStates,
  sessAAlphaDispatches,
  sessABetaDispatches,
  sessBAlphaDispatches,
  sessBBetaDispatches,
  CONV_IDS,
  DISPATCH_IDS,
} from './fixtures/dispatch-architecture.fixture'

describe('dispatch architecture projection (8-dispatch scenario)', () => {
  // ── getDispatches extraction ──

  it('extracts 2 dispatches per agent from sess-a', () => {
    const alphaD = getDispatches(sessAAgentStates[0])
    const betaD = getDispatches(sessAAgentStates[1])
    expect(alphaD).toHaveLength(2)
    expect(betaD).toHaveLength(2)
  })

  it('extracts 2 dispatches per agent from sess-b', () => {
    const alphaD = getDispatches(sessBAgentStates[0])
    const betaD = getDispatches(sessBAgentStates[1])
    expect(alphaD).toHaveLength(2)
    expect(betaD).toHaveLength(2)
  })

  // ── Distinct dispatch IDs across all 8 ──

  it('all 8 dispatches have distinct ids', () => {
    const all = [
      ...sessAAlphaDispatches,
      ...sessABetaDispatches,
      ...sessBAlphaDispatches,
      ...sessBBetaDispatches,
    ]
    expect(all).toHaveLength(8)
    const ids = all.map(d => d.id)
    expect(new Set(ids).size).toBe(8)
  })

  // ── Distinct convIds (7 distinct: continuation shares one) ──

  it('produces 7 distinct conversationIds (continuation reuses parent)', () => {
    const all = [
      ...sessAAlphaDispatches,
      ...sessABetaDispatches,
      ...sessBAlphaDispatches,
      ...sessBBetaDispatches,
    ]
    const convIds = all.map(d => d.conversationId)
    // alpha R1 and R2 in sess-a share the same convId
    expect(new Set(convIds).size).toBe(7)
  })

  // ── Continuation: alpha R1 and R2 share convId ──

  it('sess-a alpha R1 and R2 share the same conversationId (continuation)', () => {
    expect(sessAAlphaDispatches[0].conversationId).toBe(CONV_IDS.A_ALPHA)
    expect(sessAAlphaDispatches[1].conversationId).toBe(CONV_IDS.A_ALPHA)
    expect(sessAAlphaDispatches[0].conversationId)
      .toBe(sessAAlphaDispatches[1].conversationId)
  })

  // ── Fresh: beta R1 and R2 have different convIds ──

  it('sess-a beta R1 and R2 have distinct conversationIds (both fresh)', () => {
    expect(sessABetaDispatches[0].conversationId).toBe(CONV_IDS.A_R1_BETA)
    expect(sessABetaDispatches[1].conversationId).toBe(CONV_IDS.A_R2_BETA)
    expect(sessABetaDispatches[0].conversationId)
      .not.toBe(sessABetaDispatches[1].conversationId)
  })

  // ── Cross-session independence ──

  it('no conversationId appears in both sessions', () => {
    const aConvs = new Set([
      ...sessAAlphaDispatches.map(d => d.conversationId),
      ...sessABetaDispatches.map(d => d.conversationId),
    ])
    const bConvs = new Set([
      ...sessBAlphaDispatches.map(d => d.conversationId),
      ...sessBBetaDispatches.map(d => d.conversationId),
    ])
    for (const c of aConvs) {
      expect(bConvs.has(c)).toBe(false)
    }
  })

  // ── Per-dispatch model ──

  it('every dispatch carries model mock-model', () => {
    const all = [
      ...sessAAlphaDispatches,
      ...sessABetaDispatches,
      ...sessBAlphaDispatches,
      ...sessBBetaDispatches,
    ]
    for (const d of all) {
      expect(d.model).toBe('mock-model')
    }
  })

  // ── Per-dispatch elapsed ──

  it('every dispatch has positive elapsed', () => {
    const all = [
      ...sessAAlphaDispatches,
      ...sessABetaDispatches,
      ...sessBAlphaDispatches,
      ...sessBBetaDispatches,
    ]
    for (const d of all) {
      expect(d.elapsed).toBeGreaterThan(0)
    }
  })

  // ── Dispatch ID values match engine fixture ──

  it('dispatch ids match engine fixture constants', () => {
    expect(sessAAlphaDispatches[0].id).toBe(DISPATCH_IDS.A_R1_ALPHA)
    expect(sessAAlphaDispatches[1].id).toBe(DISPATCH_IDS.A_R2_ALPHA)
    expect(sessABetaDispatches[0].id).toBe(DISPATCH_IDS.A_R1_BETA)
    expect(sessABetaDispatches[1].id).toBe(DISPATCH_IDS.A_R2_BETA)
    expect(sessBAlphaDispatches[0].id).toBe(DISPATCH_IDS.B_R1_ALPHA)
    expect(sessBAlphaDispatches[1].id).toBe(DISPATCH_IDS.B_R2_ALPHA)
    expect(sessBBetaDispatches[0].id).toBe(DISPATCH_IDS.B_R1_BETA)
    expect(sessBBetaDispatches[1].id).toBe(DISPATCH_IDS.B_R2_BETA)
  })

  // ── Task text matches engine fixture ──

  it('task text matches the engine scenario', () => {
    expect(sessAAlphaDispatches[0].task).toBe('Task-AAA')
    expect(sessAAlphaDispatches[1].task).toBe('Task-CCC')
    expect(sessABetaDispatches[0].task).toBe('Task-BBB')
    expect(sessABetaDispatches[1].task).toBe('Task-DDD')
    expect(sessBAlphaDispatches[0].task).toBe('Task-EEE')
    expect(sessBAlphaDispatches[1].task).toBe('Task-GGG')
    expect(sessBBetaDispatches[0].task).toBe('Task-FFF')
    expect(sessBBetaDispatches[1].task).toBe('Task-HHH')
  })

  // ── getDispatches round-trip: extracted dispatches match source ──

  it('getDispatches round-trips all dispatch fields from agent metadata', () => {
    const extracted = getDispatches(sessAAgentStates[0])
    expect(extracted).toHaveLength(2)
    expect(extracted[0].id).toBe(DISPATCH_IDS.A_R1_ALPHA)
    expect(extracted[0].task).toBe('Task-AAA')
    expect(extracted[0].model).toBe('mock-model')
    expect(extracted[0].conversationId).toBe(CONV_IDS.A_ALPHA)
    expect(extracted[0].status).toBe('done')
    expect(extracted[1].id).toBe(DISPATCH_IDS.A_R2_ALPHA)
    expect(extracted[1].task).toBe('Task-CCC')
    expect(extracted[1].conversationId).toBe(CONV_IDS.A_ALPHA)
  })

  // ── Projection shape: what iOS would receive in the snapshot ──

  it('each agent carries correct dispatch count in metadata', () => {
    for (const agent of [...sessAAgentStates, ...sessBAgentStates]) {
      const dispatches = agent.metadata?.dispatches as unknown[]
      expect(dispatches).toBeDefined()
      expect(dispatches).toHaveLength(2)
    }
  })

  it('continuation dispatch resolves to same convId as parent via getDispatches', () => {
    const dispatches = getDispatches(sessAAgentStates[0])
    // Both R1 (Task-AAA) and R2 (Task-CCC) of alpha share a convId.
    // A message cache keyed by convId would return the SAME conversation
    // for both pager tabs, containing both rounds' content.
    const convIds = dispatches.map(d => d.conversationId)
    expect(convIds[0]).toBe(convIds[1])
  })
})
