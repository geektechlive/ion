/**
 * Tests for the StatusBarContextIndicator's local percent recomputation,
 * plus the B3/B4 idle+reload fallback paths.
 *
 * The component's job is to render a percent + tooltip showing how much
 * of the conversation's context window is used.
 *
 * B3 fix: when contextTokens is null (idle/reload), the component falls back
 * to inst.statusFields.contextPercent / contextWindow instead of showing 0%
 * (the structurally-dead tab.contextPercent / tab.contextWindow dead-field path).
 *
 * B4 fix: contextWindow is now persisted in session-store-persistence.ts so
 * the denominator survives reload without requiring a new engine run.
 *
 * We test the pure math and selector logic in isolation. React rendering is
 * intentionally not tested — the formula is the contract.
 */

import { describe, it, expect } from 'vitest'

// Mirrors the formula in StatusBarContextIndicator.tsx. Kept in lockstep
// with that component; any change there must update both.
function resolvePercentAndTokens(input: {
  contextTokens: number | null
  contextPercent: number | null
  engineContextWindow: number | null
  pickerWindow: number
}): { pct: number | null; tokens: number } {
  const windowSize = input.engineContextWindow ?? input.pickerWindow
  const pct = input.contextTokens != null
    ? Math.min(100, Math.round((input.contextTokens / windowSize) * 100))
    : input.contextPercent
  if (pct === null) return { pct: null, tokens: 0 }
  const tokens = input.contextTokens ?? (pct * windowSize / 100)
  return { pct, tokens }
}

// Mirrors the StatusBarContextIndicator selector logic for idle/reload.
// When contextTokens (liveTokens) is null, contextPercent should come from
// statusFields.contextPercent — not from the structurally-dead tab.contextPercent.
function resolveIdlePercentFromStatusFields(input: {
  liveTokens: number | null
  sfPercent: number | null
  sfWindow: number | null
  liveWindow: number | null
  pickerWindow: number
}): { pct: number | null; effectiveWindow: number | null } {
  const contextPercent = input.liveTokens !== null ? null : input.sfPercent
  const engineContextWindow = input.liveWindow ?? input.sfWindow
  const windowSize = engineContextWindow ?? input.pickerWindow
  const pct = input.liveTokens != null
    ? Math.min(100, Math.round((input.liveTokens / windowSize) * 100))
    : contextPercent
  return { pct, effectiveWindow: engineContextWindow }
}

describe('StatusBarContextIndicator math', () => {
  it('uses engine window over picker window (regression for sturdy-wishing-tide bug)', () => {
    const out = resolvePercentAndTokens({
      contextTokens: 497742,
      contextPercent: 50,
      engineContextWindow: 1_000_000,
      pickerWindow: 200_000,
    })
    expect(out.pct).toBe(50)
    expect(out.tokens).toBe(497742)
  })

  it('falls back to picker window when engine has not yet reported (cold-start tab)', () => {
    const out = resolvePercentAndTokens({
      contextTokens: 50_000,
      contextPercent: null,
      engineContextWindow: null,
      pickerWindow: 200_000,
    })
    expect(out.pct).toBe(25)
    expect(out.tokens).toBe(50_000)
  })

  it('prefers engine percent when contextTokens is null', () => {
    const out = resolvePercentAndTokens({
      contextTokens: null,
      contextPercent: 42,
      engineContextWindow: 200_000,
      pickerWindow: 200_000,
    })
    expect(out.pct).toBe(42)
  })

  it('returns null when both engine percent and contextTokens are null', () => {
    const out = resolvePercentAndTokens({
      contextTokens: null,
      contextPercent: null,
      engineContextWindow: null,
      pickerWindow: 200_000,
    })
    expect(out.pct).toBeNull()
  })

  it('caps the displayed percent at 100 even on transient mismatch', () => {
    const out = resolvePercentAndTokens({
      contextTokens: 500_000,
      contextPercent: null,
      engineContextWindow: 200_000,
      pickerWindow: 1_000_000,
    })
    expect(out.pct).toBe(100)
  })

  it('picker change between turns does NOT change the displayed percent', () => {
    const before = resolvePercentAndTokens({
      contextTokens: 497742,
      contextPercent: 50,
      engineContextWindow: 1_000_000,
      pickerWindow: 1_000_000,
    })
    const after = resolvePercentAndTokens({
      contextTokens: 497742,
      contextPercent: 50,
      engineContextWindow: 1_000_000,
      pickerWindow: 200_000,
    })
    expect(after.pct).toBe(before.pct)
    expect(after.tokens).toBe(before.tokens)
  })
})

describe('B3 — idle indicator shows persisted context% via statusFields fallback', () => {
  it('shows statusFields contextPercent when idle (contextTokens null) — fails on dead-tab-field path', () => {
    // The old code read tab.contextPercent which is never written at runtime.
    // The fix reads inst.statusFields.contextPercent when contextTokens is null.
    const out = resolveIdlePercentFromStatusFields({
      liveTokens: null,         // idle: no live token count
      sfPercent: 63,            // engine heartbeat reported 63% on last idle
      sfWindow: 200_000,
      liveWindow: null,
      pickerWindow: 200_000,
    })
    // Must show 63%, not 0% (dead-tab-field path would return null/0)
    expect(out.pct).toBe(63)
    expect(out.effectiveWindow).toBe(200_000)
  })

  it('during live run: ignores statusFields and uses liveTokens', () => {
    // When a run is in progress (liveTokens set), the selector zeroes out
    // contextPercent (liveTokens != null path), then computes pct from liveTokens.
    // liveTokens=100k / window=200k = 50%. The stale sfPercent (63) must NOT appear.
    const out = resolveIdlePercentFromStatusFields({
      liveTokens: 100_000,
      sfPercent: 63,            // stale status from previous turn
      sfWindow: 200_000,
      liveWindow: 200_000,
      pickerWindow: 200_000,
    })
    // 100k / 200k = 50%, not the stale 63% from statusFields
    expect(out.pct).toBe(50)
    expect(out.pct).not.toBe(63) // proves statusFields.contextPercent was not used
  })
})

describe('B4 — reload shows persisted contextWindow denominator', () => {
  it('uses persisted contextWindow (sfWindow) when liveWindow is null after reload', () => {
    // After reload: liveTokens and liveWindow are null (no run yet this session).
    // sfWindow comes from statusFields which is seeded by the engine on resume.
    // Before B4, contextWindow was not persisted so sfWindow was also null,
    // forcing fallback to the picker model window — potentially wrong model.
    const out = resolveIdlePercentFromStatusFields({
      liveTokens: null,
      sfPercent: 45,
      sfWindow: 1_000_000,      // Opus was the running model; persisted
      liveWindow: null,         // no live breakdown yet
      pickerWindow: 200_000,    // user has Sonnet selected in picker
    })
    expect(out.pct).toBe(45)
    // Effective window must be the persisted Opus window, not the picker window
    expect(out.effectiveWindow).toBe(1_000_000)
  })

  it('without persisted window: falls back to picker (old behavior, now avoidable with B4)', () => {
    // Regression guard: without B4 the effectiveWindow would be the picker window.
    // This test documents the fallback is still correct when no persisted window exists.
    const out = resolveIdlePercentFromStatusFields({
      liveTokens: null,
      sfPercent: null,
      sfWindow: null,
      liveWindow: null,
      pickerWindow: 200_000,
    })
    expect(out.pct).toBeNull()  // no percent at all → indicator hides
    expect(out.effectiveWindow).toBeNull()
  })
})

