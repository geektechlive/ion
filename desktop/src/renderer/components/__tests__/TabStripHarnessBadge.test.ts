/**
 * Tests for the per-tab harness badge helpers in TabStripShared.ts:
 *
 *   - `abbreviateProfileName`: contract-pins the four-rule abbreviation
 *     algorithm so a future refactor can't silently break the initials/
 *     truncation/fallback logic.
 *
 *   - `tabHasExtensions`: contract-pins the one-line presence predicate
 *     that gates badge visibility. Per ADR-009 this derives from the
 *     stored `engineProfileId` field and is final (a non-null, non-empty
 *     `engineProfileId` means extensions are active); the test pins the
 *     predicate so a future refactor can't silently break the gate.
 *
 * Both are pure functions with no React, no DOM, and no Electron
 * dependency — the node test environment is sufficient.
 *
 * Badge render coverage: the decision "show harness badge / label" is
 * fully captured by `tabHasExtensions` (visibility gate) and
 * `abbreviateProfileName` (what text to show). The inline badge element
 * in TabStripTabPill.tsx renders `harnessBadgeLabel` directly, so
 * covering these two functions is equivalent to covering badge content
 * from the unit-test surface. End-to-end visual rendering is verified
 * by manual inspection and future E2E tests; that is outside the
 * vitest/node scope.
 */

import { describe, it, expect } from 'vitest'

// Stub the imports that TabStripShared pulls in at module load time.
// These transitively reach Electron, React, and localStorage — none of
// which exist in the node test environment.
import { vi } from 'vitest'

vi.mock('@phosphor-icons/react', () => ({
  Diamond: () => null, Square: () => null, StarFour: () => null,
  Triangle: () => null, Heart: () => null, Hexagon: () => null,
  Lightning: () => null, Terminal: () => null,
  DeviceMobile: () => null, Monitor: () => null, Gear: () => null,
}))

vi.mock('../../stores/sessionStore', () => ({
  useSessionStore: { getState: () => ({ conversationPanes: new Map() }) },
}))

vi.mock('../../preferences', () => ({
  usePreferencesStore: { getState: () => ({ uiZoom: 1, gitOpsMode: 'standard' }) },
}))

import { abbreviateProfileName, tabHasExtensions } from '../TabStripShared'

// ─── abbreviateProfileName ────────────────────────────────────────────────────

describe('abbreviateProfileName', () => {
  it('returns EXT for null', () => {
    expect(abbreviateProfileName(null)).toBe('EXT')
  })

  it('returns EXT for undefined', () => {
    expect(abbreviateProfileName(undefined)).toBe('EXT')
  })

  it('returns EXT for empty string', () => {
    expect(abbreviateProfileName('')).toBe('EXT')
  })

  it('returns EXT for whitespace-only string', () => {
    expect(abbreviateProfileName('   ')).toBe('EXT')
  })

  it('returns the name as-is when it is already ≤ 8 chars', () => {
    expect(abbreviateProfileName('COS')).toBe('COS')
    expect(abbreviateProfileName('ion')).toBe('ion')
    expect(abbreviateProfileName('ABCD')).toBe('ABCD')
    expect(abbreviateProfileName('Orion')).toBe('Orion')
    expect(abbreviateProfileName('ion-dev')).toBe('ion-dev')
    expect(abbreviateProfileName('ABCDEFGH')).toBe('ABCDEFGH')
  })

  it('returns initials from multi-word names where combined length > 8 (Ion Dev Engine → IDE)', () => {
    // "Ion Dev" is 7 chars — passes through at cap 8 (rule 3).
    // Need a longer multi-word name to trigger the initials path.
    expect(abbreviateProfileName('Ion Dev Engine')).toBe('IDE')
  })

  it('returns initials from three-word name (My Long Name → MLN)', () => {
    expect(abbreviateProfileName('My Long Name')).toBe('MLN')
  })

  it('caps initials at 8 chars (nine-word name)', () => {
    // First letters of nine words → 9 chars, capped to 8
    // Alpha Beta Gamma Delta Epsilon Zeta Eta Theta Iota → ABGDEZETI capped to ABGDEZET
    expect(abbreviateProfileName('Alpha Beta Gamma Delta Epsilon Zeta Eta Theta Iota')).toBe('ABGDEZET')
  })

  it('uppercases initials from a long multi-word name', () => {
    expect(abbreviateProfileName('ion dev engine')).toBe('IDE')
  })

  it('falls back to first-8-chars-uppercased for a single long word', () => {
    // 'Enterprise' → first 8 chars = 'Enterpri' → uppercased = 'ENTERPRI'
    expect(abbreviateProfileName('Enterprise')).toBe('ENTERPRI')
  })

  it('passes through a 5-char single word unchanged (Orion harness name)', () => {
    expect(abbreviateProfileName('Orion')).toBe('Orion')
  })

  it('passes through a 7-char single word unchanged (ion-dev — was ION-D at cap 5, now full word at cap 8)', () => {
    // "ion-dev" is 7 chars, within the 8-char passthrough window
    expect(abbreviateProfileName('ion-dev')).toBe('ion-dev')
  })

  it('passes through Ion Dev unchanged (7 chars, within 8-char cap)', () => {
    // At cap 5 this would have truncated or produced initials; at cap 8 it passes through
    expect(abbreviateProfileName('Ion Dev')).toBe('Ion Dev')
  })

  it('handles a name of exactly 4 chars without modification', () => {
    expect(abbreviateProfileName('Code')).toBe('Code')
  })

  it('handles a name of exactly 8 chars without modification', () => {
    expect(abbreviateProfileName('ABCDEFGH')).toBe('ABCDEFGH')
  })

  it('handles extra whitespace between words gracefully', () => {
    // "Ion  Dev" is 8 chars including the double space — trims to "Ion  Dev" which is > 8? No.
    // "Ion  Dev" trimmed = "Ion  Dev" (8 chars). Passthrough at ≤ 8.
    // But "My  Long  Name" trimmed is 14 chars → initials path
    expect(abbreviateProfileName('My  Long  Name')).toBe('MLN')
  })
})

// ─── tabHasExtensions ─────────────────────────────────────────────────────────

describe('tabHasExtensions', () => {
  it('returns true when the tab has a non-null engineProfileId', () => {
    const tab = { engineProfileId: 'cos' } as any
    expect(tabHasExtensions(tab)).toBe(true)
  })

  it('returns false when engineProfileId is null (plain tab)', () => {
    const tab = { engineProfileId: null } as any
    expect(tabHasExtensions(tab)).toBe(false)
  })

  it('returns false when engineProfileId is empty string', () => {
    const tab = { engineProfileId: '' } as any
    expect(tabHasExtensions(tab)).toBe(false)
  })
})

// ─── Badge visibility contract ────────────────────────────────────────────────

describe('harness badge visibility contract', () => {
  // The badge render site in TabStripTabPill.tsx does:
  //   const harnessBadgeLabel = tabHasExtensions(tab)
  //     ? abbreviateProfileName(profileName) : null
  // Badge renders when harnessBadgeLabel !== null.

  it('badge shown: extensions present, profile name resolves to initials for long multi-word name', () => {
    const tab = { engineProfileId: 'p1' } as any
    const profileName = 'Ion Dev Engine'
    const label = tabHasExtensions(tab) ? abbreviateProfileName(profileName) : null
    expect(label).toBe('IDE')
  })

  it('badge shown: extensions present, short profile name passes through unchanged', () => {
    const tab = { engineProfileId: 'p1' } as any
    const profileName = 'Ion Dev'  // 7 chars, within 8-char cap — passes through as-is
    const label = tabHasExtensions(tab) ? abbreviateProfileName(profileName) : null
    expect(label).toBe('Ion Dev')
  })

  it('badge shown: extensions present, profile not found (null name) -> EXT fallback', () => {
    const tab = { engineProfileId: 'unknown' } as any
    const profileName = null // profile lookup returned undefined
    const label = tabHasExtensions(tab) ? abbreviateProfileName(profileName) : null
    expect(label).toBe('EXT')
  })

  it('badge hidden: plain tab (no extensions)', () => {
    const tab = { engineProfileId: null } as any
    const profileName = null
    const label = tabHasExtensions(tab) ? abbreviateProfileName(profileName) : null
    expect(label).toBeNull()
  })

  it('badge hidden: plain tab (empty profileId)', () => {
    const tab = { engineProfileId: '' } as any
    const label = tabHasExtensions(tab) ? abbreviateProfileName('Some Profile') : null
    expect(label).toBeNull()
  })
})
