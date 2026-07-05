/**
 * Behavioral tests for the harness badge in DropdownTabRow.
 *
 * The badge label is derived inline in DropdownTabRow via:
 *
 *   const harnessBadgeLabel = usePreferencesStore((s) => {
 *     if (!tabHasExtensions(tab)) return null
 *     const profile = s.engineProfiles.find((p) => p.id === tab.engineProfileId)
 *     return abbreviateProfileName(profile?.name)
 *   })
 *
 * That derivation is pure — given a tab and a profiles array it always
 * produces the same result. These tests pin the contract directly against
 * the two helpers the component calls so a future refactor of the
 * component body can't silently break badge visibility or label content.
 *
 * Component-level mount tests are out of scope for the node environment
 * (framer-motion + Reorder require a DOM); the pure-function approach
 * matches the project's established pattern (see TabStripHarnessBadge.test.ts,
 * DefaultProfileSetting.test.ts).
 */

import { describe, it, expect } from 'vitest'
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
  usePreferencesStore: { getState: () => ({ uiZoom: 1, gitOpsMode: 'standard', engineProfiles: [] }) },
}))

import { tabHasExtensions, abbreviateProfileName } from '../TabStripShared'
import type { EngineProfile } from '../../../shared/types'

// ─── Helpers ─────────────────────────────────────────────────────────────────

/** Reproduce the exact derivation DropdownTabRow uses for harnessBadgeLabel. */
function deriveBadgeLabel(
  tab: { engineProfileId: string | null | undefined },
  engineProfiles: EngineProfile[],
): string | null {
  if (!tabHasExtensions(tab as any)) return null
  const profile = engineProfiles.find((p) => p.id === tab.engineProfileId)
  return abbreviateProfileName(profile?.name)
}

function makeProfile(id: string, name: string): EngineProfile {
  return { id, name, extensions: [`ext/${id}.js`] }
}

// ─── Badge visibility ─────────────────────────────────────────────────────────

describe('DropdownTabRow harness badge — visibility', () => {
  it('returns a label (not null) for an engine tab with a matching profile', () => {
    const tab = { engineProfileId: 'ion-dev-id' }
    const profiles = [makeProfile('ion-dev-id', 'ion-dev')]
    expect(deriveBadgeLabel(tab, profiles)).not.toBeNull()
  })

  it('returns null for a plain tab (engineProfileId is null)', () => {
    const tab = { engineProfileId: null }
    const profiles = [makeProfile('ion-dev-id', 'ion-dev')]
    expect(deriveBadgeLabel(tab, profiles)).toBeNull()
  })

  it('returns null for a plain tab (engineProfileId is empty string)', () => {
    const tab = { engineProfileId: '' }
    const profiles = [makeProfile('ion-dev-id', 'ion-dev')]
    expect(deriveBadgeLabel(tab, profiles)).toBeNull()
  })

  it('returns EXT fallback (not null) when engine tab profile id is unknown', () => {
    // Profile was deleted or not yet loaded — badge still renders with EXT
    const tab = { engineProfileId: 'deleted-profile' }
    const profiles: EngineProfile[] = []
    expect(deriveBadgeLabel(tab, profiles)).toBe('EXT')
  })
})

// ─── Badge label content ──────────────────────────────────────────────────────

describe('DropdownTabRow harness badge — label content', () => {
  it('renders the profile name when it is ≤ 8 chars (ion-dev passes through)', () => {
    const tab = { engineProfileId: 'p1' }
    const profiles = [makeProfile('p1', 'ion-dev')]
    expect(deriveBadgeLabel(tab, profiles)).toBe('ion-dev')
  })

  it('renders initials for a multi-word profile name longer than 8 chars', () => {
    const tab = { engineProfileId: 'p2' }
    const profiles = [makeProfile('p2', 'Ion Dev Engine')]
    expect(deriveBadgeLabel(tab, profiles)).toBe('IDE')
  })

  it('renders first-8-chars-uppercased for a single long word profile name', () => {
    const tab = { engineProfileId: 'p3' }
    const profiles = [makeProfile('p3', 'Enterprise')]
    expect(deriveBadgeLabel(tab, profiles)).toBe('ENTERPRI')
  })

  it('renders EXT when the profile is not found in the list', () => {
    const tab = { engineProfileId: 'ghost' }
    const profiles = [makeProfile('other', 'Other')]
    expect(deriveBadgeLabel(tab, profiles)).toBe('EXT')
  })

  it('renders EXT when the engine profiles list is empty', () => {
    const tab = { engineProfileId: 'any-id' }
    expect(deriveBadgeLabel(tab, [])).toBe('EXT')
  })

  it('derives the label from the matching profile, not a different one', () => {
    const tab = { engineProfileId: 'cos' }
    const profiles = [
      makeProfile('ion', 'Ion Dev'),
      makeProfile('cos', 'COS'),
      makeProfile('ent', 'Enterprise'),
    ]
    expect(deriveBadgeLabel(tab, profiles)).toBe('COS')
  })
})
