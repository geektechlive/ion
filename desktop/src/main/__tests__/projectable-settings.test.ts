// Tests for the projectable-settings allowlist and validation helpers.
// Two layers:
//
//   1. Structural integrity. Every entry on the allowlist must point at a
//      real key on either SETTINGS_DEFAULTS (main-process) or the
//      renderer-side SETTINGS_DEFAULTS map. Without this, a future
//      settings rename could silently break iOS projection because the
//      handler would still emit the (now-orphan) key but the desktop
//      would never write it.
//
//   2. Validation. The allowlist must reject unknown keys and wrong-type
//      values without raising — the handler's contract is "silent log +
//      no write" on bad input.
//
// The projection itself (reading settings.json + filling in defaults) is
// also tested so the on-disk omitempty contract is honored.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

import {
  PROJECTABLE_SETTINGS,
  PROJECTABLE_GROUP_ORDER,
  PROJECTABLE_GROUP_LABELS,
  isProjectableKey,
  validateSettingValue,
  projectableKeysWithoutDefault,
  projectCurrentSettings,
  projectableSchema,
  projectableGroups,
} from '../projectable-settings'
import * as settingsStore from '../settings-store'

describe('projectable-settings allowlist', () => {
  it('every entry has a non-empty key, label, and description', () => {
    for (const entry of PROJECTABLE_SETTINGS) {
      expect(entry.key, `entry ${entry.key}: key`).toBeTruthy()
      expect(entry.label, `entry ${entry.key}: label`).toBeTruthy()
      expect(entry.description, `entry ${entry.key}: description`).toBeTruthy()
    }
  })

  it('every entry declares a recognized type', () => {
    const valid = new Set(['boolean', 'string', 'number'])
    for (const entry of PROJECTABLE_SETTINGS) {
      expect(valid.has(entry.type), `entry ${entry.key}: type=${entry.type}`).toBe(true)
    }
  })

  it('every entry has a defaultValue matching its declared type', () => {
    for (const entry of PROJECTABLE_SETTINGS) {
      expect(typeof entry.defaultValue, `entry ${entry.key}: defaultValue type`).toBe(entry.type)
    }
  })

  it('keys are unique across the allowlist', () => {
    const seen = new Set<string>()
    for (const entry of PROJECTABLE_SETTINGS) {
      expect(seen.has(entry.key), `duplicate key: ${entry.key}`).toBe(false)
      seen.add(entry.key)
    }
  })

  it('every key has a corresponding entry in some SETTINGS_DEFAULTS map', () => {
    // projectableKeysWithoutDefault returns the list of keys that point
    // at neither the main-process SETTINGS_DEFAULTS nor the renderer
    // one. A non-empty list means an entry has been added to the
    // allowlist without a matching defaults source — the iOS UI would
    // render the row but the desktop's writeSettings call would create
    // a phantom key that no consumer reads.
    const orphans = projectableKeysWithoutDefault()
    expect(orphans, `keys with no defaults: ${orphans.join(', ')}`).toEqual([])
  })

  it('every entry declares a group that exists in PROJECTABLE_GROUP_ORDER', () => {
    const validGroups = new Set(PROJECTABLE_GROUP_ORDER)
    for (const entry of PROJECTABLE_SETTINGS) {
      expect(validGroups.has(entry.group as any), `entry ${entry.key} group=${entry.group}`).toBe(true)
    }
  })

  it('every group in PROJECTABLE_GROUP_ORDER has at least one entry', () => {
    // Empty sections render as a header with no rows, which looks
    // broken on iOS. Catching empty groups here forces a deliberate
    // group removal rather than leaving a dead section name behind
    // after the last entry is moved out.
    const groupsWithEntries = new Set(PROJECTABLE_SETTINGS.map((s) => s.group))
    for (const group of PROJECTABLE_GROUP_ORDER) {
      expect(groupsWithEntries.has(group as any), `group ${group} has no entries`).toBe(true)
    }
  })

  it('every group in PROJECTABLE_GROUP_ORDER has a label', () => {
    for (const group of PROJECTABLE_GROUP_ORDER) {
      expect(PROJECTABLE_GROUP_LABELS[group], `group ${group} label`).toBeTruthy()
    }
  })
})

describe('projectableSchema / projectableGroups', () => {
  it('schema mirrors the allowlist in order and field shape', () => {
    const schema = projectableSchema()
    expect(schema.length).toBe(PROJECTABLE_SETTINGS.length)
    for (let i = 0; i < schema.length; i++) {
      expect(schema[i].key).toBe(PROJECTABLE_SETTINGS[i].key)
      expect(schema[i].type).toBe(PROJECTABLE_SETTINGS[i].type)
      expect(schema[i].group).toBe(PROJECTABLE_SETTINGS[i].group)
      expect(schema[i].label).toBe(PROJECTABLE_SETTINGS[i].label)
      expect(schema[i].description).toBe(PROJECTABLE_SETTINGS[i].description)
      expect(schema[i].defaultValue).toBe(PROJECTABLE_SETTINGS[i].defaultValue)
    }
  })

  it('groups returns the ordered list of { id, label } descriptors', () => {
    const groups = projectableGroups()
    expect(groups.length).toBe(PROJECTABLE_GROUP_ORDER.length)
    for (let i = 0; i < groups.length; i++) {
      expect(groups[i].id).toBe(PROJECTABLE_GROUP_ORDER[i])
      expect(groups[i].label).toBe(PROJECTABLE_GROUP_LABELS[PROJECTABLE_GROUP_ORDER[i]])
    }
  })
})

describe('isProjectableKey', () => {
  it('returns true for every allowlisted key', () => {
    for (const entry of PROJECTABLE_SETTINGS) {
      expect(isProjectableKey(entry.key)).toBe(true)
    }
  })

  it('returns false for an unknown key', () => {
    expect(isProjectableKey('not_a_real_setting')).toBe(false)
  })

  it('returns false for a Settings field that is intentionally NOT projected', () => {
    // `defaultBaseDirectory` lives in the renderer SETTINGS_DEFAULTS but
    // is intentionally excluded from the allowlist (it's a local-fs
    // path that has no meaning on iOS). If a future change projected
    // it, this test would fail and force a deliberate review.
    expect(isProjectableKey('defaultBaseDirectory')).toBe(false)
    expect(isProjectableKey('relayApiKey')).toBe(false)
    expect(isProjectableKey('terminalFontFamily')).toBe(false)
  })
})

describe('validateSettingValue', () => {
  it('accepts a boolean for a boolean key', () => {
    expect(validateSettingValue('enableEarlyStopContinuation', true)).toBeNull()
    expect(validateSettingValue('enableEarlyStopContinuation', false)).toBeNull()
  })

  it('rejects a non-boolean for a boolean key', () => {
    expect(validateSettingValue('enableEarlyStopContinuation', 'true')).not.toBeNull()
    expect(validateSettingValue('enableEarlyStopContinuation', 1)).not.toBeNull()
    expect(validateSettingValue('enableEarlyStopContinuation', null)).not.toBeNull()
    expect(validateSettingValue('enableEarlyStopContinuation', undefined)).not.toBeNull()
  })

  it('rejects an unknown key regardless of value', () => {
    expect(validateSettingValue('not_a_real_setting', true)).not.toBeNull()
    expect(validateSettingValue('not_a_real_setting', 'value')).not.toBeNull()
  })

  it('rejects NaN even when a number is expected (NaN === number type but is not a valid setting value)', () => {
    // The current allowlist has no number entries but the validator must
    // still guard NaN preemptively so future number projections inherit
    // the right behavior. Skip if there is no number entry today.
    const numericEntry = PROJECTABLE_SETTINGS.find((s) => s.type === 'number')
    if (!numericEntry) return
    expect(validateSettingValue(numericEntry.key, NaN)).not.toBeNull()
  })
})

describe('projectCurrentSettings', () => {
  let readSettingsSpy: any

  beforeEach(() => {
    readSettingsSpy = vi.spyOn(settingsStore, 'readSettings')
  })

  afterEach(() => {
    readSettingsSpy.mockRestore()
  })

  it('returns every projectable key, falling back to defaults when settings.json omits one', () => {
    readSettingsSpy.mockReturnValue({})
    const out = projectCurrentSettings()
    for (const entry of PROJECTABLE_SETTINGS) {
      expect(out, `key ${entry.key} present`).toHaveProperty(entry.key)
      expect(out[entry.key], `key ${entry.key} default`).toBe(entry.defaultValue)
    }
  })

  it('returns the persisted value when settings.json carries one', () => {
    // Flip a representative boolean to its non-default and verify the
    // projection picks it up. Use enableEarlyStopContinuation which
    // defaults to true; persist false; expect false.
    readSettingsSpy.mockReturnValue({ enableEarlyStopContinuation: false })
    const out = projectCurrentSettings()
    expect(out.enableEarlyStopContinuation).toBe(false)
  })

  it('does not include non-projectable keys even if settings.json carries them', () => {
    // settings.json typically carries dozens of keys; the projection
    // must filter to only the allowlist. A non-projectable key
    // (relayApiKey, a path, a font) must not leak.
    readSettingsSpy.mockReturnValue({
      enableEarlyStopContinuation: true,
      relayApiKey: 'secret-key',
      defaultBaseDirectory: '/Users/me/work',
      terminalFontFamily: 'Custom Font',
    })
    const out = projectCurrentSettings()
    expect(out).not.toHaveProperty('relayApiKey')
    expect(out).not.toHaveProperty('defaultBaseDirectory')
    expect(out).not.toHaveProperty('terminalFontFamily')
  })
})
