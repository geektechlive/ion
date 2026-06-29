/**
 * DefaultProfileSetting — dropdown logic unit tests.
 *
 * Covers the two pure helpers extracted from GeneralCategory.tsx that
 * drive the "Default engine profile for new conversations" dropdown:
 *
 *   deriveProfileOptions(profiles)
 *     - Always produces a leading "Plain conversation" option (value='').
 *     - One additional option per profile, value=id, label=name.
 *     - Empty profile list -> only the plain option.
 *
 *   resolveSelectedProfileOption(storedId, profiles)
 *     - '' stored -> '' selected (plain).
 *     - Stored id matches a known profile -> that id selected.
 *     - Stored id refers to a deleted profile -> falls back to ''.
 *
 * These tests target pure functions and run in the node environment
 * without React machinery, matching the project's existing test pattern.
 */

import { describe, it, expect } from 'vitest'
import {
  deriveProfileOptions,
  resolveSelectedProfileOption,
} from '../settings/default-profile-options'
import type { EngineProfile } from '../../../shared/types'

// ─── Helpers ─────────────────────────────────────────────────────────────────

function makeProfile(id: string, name: string): EngineProfile {
  return { id, name, extensions: [`ext/${id}.js`] }
}

const PLAIN_OPTION = { value: '', label: 'Plain conversation (no extensions)' }

// ─── deriveProfileOptions ─────────────────────────────────────────────────────

describe('deriveProfileOptions', () => {
  it('returns only the plain option when profiles is empty', () => {
    const opts = deriveProfileOptions([])
    expect(opts).toHaveLength(1)
    expect(opts[0]).toEqual(PLAIN_OPTION)
  })

  it('plain option is always first', () => {
    const profiles = [makeProfile('abc', 'Cos'), makeProfile('def', 'Dev')]
    const opts = deriveProfileOptions(profiles)
    expect(opts[0]).toEqual(PLAIN_OPTION)
  })

  it('renders one option per profile after the plain entry', () => {
    const profiles = [makeProfile('abc', 'Cos'), makeProfile('def', 'Dev')]
    const opts = deriveProfileOptions(profiles)
    expect(opts).toHaveLength(3)
    expect(opts[1]).toEqual({ value: 'abc', label: 'Cos' })
    expect(opts[2]).toEqual({ value: 'def', label: 'Dev' })
  })

  it('uses profile.id as option value', () => {
    const opts = deriveProfileOptions([makeProfile('my-id', 'My Profile')])
    expect(opts[1].value).toBe('my-id')
  })

  it('uses profile.name as option label', () => {
    const opts = deriveProfileOptions([makeProfile('x', 'Extended Research')])
    expect(opts[1].label).toBe('Extended Research')
  })

  it('preserves profile order', () => {
    const profiles = [
      makeProfile('z', 'Zebra'),
      makeProfile('a', 'Alpha'),
      makeProfile('m', 'Middle'),
    ]
    const opts = deriveProfileOptions(profiles)
    expect(opts.map((o) => o.value)).toEqual(['', 'z', 'a', 'm'])
  })
})

// ─── resolveSelectedProfileOption ────────────────────────────────────────────

describe('resolveSelectedProfileOption', () => {
  it('returns empty string when storedId is empty', () => {
    const profiles = [makeProfile('abc', 'Cos')]
    expect(resolveSelectedProfileOption('', profiles)).toBe('')
  })

  it('returns empty string when storedId is empty and profiles list is empty', () => {
    expect(resolveSelectedProfileOption('', [])).toBe('')
  })

  it('returns the storedId when it matches a known profile', () => {
    const profiles = [makeProfile('abc', 'Cos'), makeProfile('def', 'Dev')]
    expect(resolveSelectedProfileOption('abc', profiles)).toBe('abc')
    expect(resolveSelectedProfileOption('def', profiles)).toBe('def')
  })

  it('falls back to empty string when storedId no longer matches any profile', () => {
    const profiles = [makeProfile('abc', 'Cos')]
    expect(resolveSelectedProfileOption('deleted-id', profiles)).toBe('')
  })

  it('falls back to empty string when profiles list is empty and storedId is set', () => {
    expect(resolveSelectedProfileOption('some-id', [])).toBe('')
  })

  it('is an exact match — partial id does not match', () => {
    const profiles = [makeProfile('abc-full', 'Cos')]
    expect(resolveSelectedProfileOption('abc', profiles)).toBe('')
  })
})
