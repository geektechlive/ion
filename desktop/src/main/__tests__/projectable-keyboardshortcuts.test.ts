/**
 * projectable-settings — keyboardShortcuts allowlist entry tests.
 *
 * Verifies:
 *   - isProjectableKey('keyboardShortcuts') returns true.
 *   - validateSettingValue accepts a valid array (list type).
 *   - validateSettingValue rejects a non-array (number, string).
 *   - projectableKeysWithoutDefault() does NOT list keyboardShortcuts
 *     (the default {} is in SETTINGS_DEFAULTS, so the entry has a default).
 */

import { describe, it, expect, vi } from 'vitest'

vi.mock('electron', () => ({
  app: { get isPackaged() { return false } },
  safeStorage: {
    isEncryptionAvailable: () => false,
    encryptString: (s: string) => Buffer.from(s),
    decryptString: (b: Buffer) => b.toString(),
  },
}))

import {
  isProjectableKey,
  validateSettingValue,
  projectableKeysWithoutDefault,
} from '../projectable-settings'

describe('projectable-settings — keyboardShortcuts allowlist', () => {
  it('isProjectableKey("keyboardShortcuts") returns true', () => {
    expect(isProjectableKey('keyboardShortcuts')).toBe(true)
  })

  it('projectableKeysWithoutDefault does NOT include keyboardShortcuts', () => {
    const missingDefault = projectableKeysWithoutDefault()
    expect(missingDefault).not.toContain('keyboardShortcuts')
  })

  it('validateSettingValue accepts an empty array (list type)', () => {
    const error = validateSettingValue('keyboardShortcuts', [])
    expect(error).toBeNull()
  })

  it('validateSettingValue rejects a number (not an array)', () => {
    const error = validateSettingValue('keyboardShortcuts', 42)
    expect(error).toBeTruthy()
    expect(typeof error).toBe('string')
  })

  it('validateSettingValue rejects a plain string', () => {
    const error = validateSettingValue('keyboardShortcuts', 'not-an-array')
    expect(error).toBeTruthy()
    expect(typeof error).toBe('string')
  })

  it('validateSettingValue rejects a plain object (not an array)', () => {
    // Our settings store uses a Record, but the projectable validator expects
    // an array (list type). The enterprise projection path uses array form;
    // the renderer uses the Record form directly via preferences store.
    const error = validateSettingValue('keyboardShortcuts', { 'tab.next': 'Mod+]' })
    expect(error).toBeTruthy()
    expect(typeof error).toBe('string')
  })
})
