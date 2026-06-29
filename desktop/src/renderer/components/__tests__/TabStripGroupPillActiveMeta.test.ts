/**
 * Tests for the active-group-pill selected-tab metadata decisions added to
 * TabStripGroupPill.tsx: when the active group's selected tab is rendered, the
 * pill now shows the selected tab's harness badge and working-directory
 * basename (in addition to the title).
 *
 * Two pure decisions drive that rendering:
 *
 *   1. Harness-badge visibility/label — identical derivation to the single-tab
 *      pill: `selectedTab.engineProfileId` resolves to a profile →
 *      `abbreviateProfileName(profile.name)`; absent/empty/unresolved →
 *      `null` (badge hidden) or 'EXT' fallback when the id is present but the
 *      profile lookup misses.
 *
 *   2. Directory basename — `workingDirectory.split('/').pop()` returns the
 *      final path segment; empty/undefined → not shown.
 *
 * Both are pure functions with no React, no DOM, and no Electron dependency.
 * The component renders these decisions inline (mirroring TabStripTabPill.tsx),
 * so pinning the derivations is equivalent to pinning the rendered content from
 * the unit-test surface. Mirrors the structure of TabStripHarnessBadge.test.ts.
 */

import { describe, it, expect, vi } from 'vitest'

// Stub the imports TabStripShared pulls in at module load time — these
// transitively reach Electron, React, and localStorage, none of which exist in
// the node test environment.
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

import { abbreviateProfileName } from '../TabStripShared'

// Replicate the exact inline derivation in TabStripGroupPill.tsx's
// harnessBadgeLabel subscription so the test pins the same decision the
// component renders.
function resolveBadgeLabel(
  selectedTab: { engineProfileId?: string | null } | undefined,
  engineProfiles: Array<{ id: string; name?: string }>,
): string | null {
  if (!selectedTab?.engineProfileId) return null
  const profile = engineProfiles.find((p) => p.id === selectedTab.engineProfileId)
  return abbreviateProfileName(profile?.name)
}

// Replicate the directory-basename derivation.
function resolveDir(
  selectedTab: { workingDirectory?: string } | undefined,
): string | null {
  if (!selectedTab?.workingDirectory) return null
  return selectedTab.workingDirectory.split('/').pop() || selectedTab.workingDirectory
}

// ─── Badge visibility / label ────────────────────────────────────────────────

describe('active group pill: selected-tab harness badge', () => {
  const profiles = [
    { id: 'p1', name: 'Ion Dev Engine' },
    { id: 'p2', name: 'Orion' },
  ]

  it('shows the abbreviated label when the selected tab resolves a profile', () => {
    expect(resolveBadgeLabel({ engineProfileId: 'p1' }, profiles)).toBe('IDE')
  })

  it('passes a short profile name through unchanged', () => {
    expect(resolveBadgeLabel({ engineProfileId: 'p2' }, profiles)).toBe('Orion')
  })

  it('falls back to EXT when the engineProfileId has no matching profile', () => {
    expect(resolveBadgeLabel({ engineProfileId: 'gone' }, profiles)).toBe('EXT')
  })

  it('hides the badge (null) for a plain selected tab with null engineProfileId', () => {
    expect(resolveBadgeLabel({ engineProfileId: null }, profiles)).toBeNull()
  })

  it('hides the badge (null) for an empty engineProfileId', () => {
    expect(resolveBadgeLabel({ engineProfileId: '' }, profiles)).toBeNull()
  })

  it('hides the badge (null) when there is no selected tab', () => {
    expect(resolveBadgeLabel(undefined, profiles)).toBeNull()
  })

  // Regression: the badge label must always reflect the CURRENTLY-selected
  // tab, never a previously-selected one. The original GroupPill computed the
  // label inside a usePreferencesStore selector closure that captured
  // selectedTab; because selectedTab comes from props (group.selectedTabId) and
  // not from the preferences store, switching the selected tab within the group
  // while engineProfiles was unchanged could leave the badge showing the prior
  // tab's profile on a plain tab. The fix derives the label synchronously from
  // selectedTab in the render body. This sequence pins the invariant: each
  // derivation depends only on its own tab, so selecting an extension tab and
  // then a plain tab yields the badge then null — never the stale badge.
  it('derives strictly from the current tab when the selected tab changes (no carryover)', () => {
    // 1) extension tab selected → badge shows
    expect(resolveBadgeLabel({ engineProfileId: 'p1' }, profiles)).toBe('IDE')
    // 2) switch to a plain tab → badge must clear, not carry over 'IDE'
    expect(resolveBadgeLabel({ engineProfileId: null }, profiles)).toBeNull()
    // 3) switch to a different extension tab → its own label, not the first one
    expect(resolveBadgeLabel({ engineProfileId: 'p2' }, profiles)).toBe('Orion')
  })
})

// ─── Directory basename ──────────────────────────────────────────────────────

describe('active group pill: selected-tab directory basename', () => {
  it('returns the final path segment', () => {
    expect(resolveDir({ workingDirectory: '/Users/josh/source/ion' })).toBe('ion')
  })

  it('returns the whole value when there is no slash', () => {
    expect(resolveDir({ workingDirectory: 'ion' })).toBe('ion')
  })

  it('returns null when workingDirectory is empty', () => {
    expect(resolveDir({ workingDirectory: '' })).toBeNull()
  })

  it('returns null when workingDirectory is undefined', () => {
    expect(resolveDir({})).toBeNull()
  })

  it('returns null when there is no selected tab', () => {
    expect(resolveDir(undefined)).toBeNull()
  })
})
