/**
 * SettingsDialog — shortcuts category registration test.
 *
 * Verifies:
 *   - The search index includes the 'shortcuts' categoryId for expected terms.
 *   - KeyboardShortcutsCategory is importable.
 */

import { describe, it, expect, vi } from 'vitest'

// Mock heavy UI dependencies to keep this a fast unit test.
vi.mock('@phosphor-icons/react', () => {
  const Icon = () => null
  return new Proxy({}, { get: () => Icon })
})

vi.mock('framer-motion', () => ({ motion: { div: (p: any) => p.children } }))
vi.mock('../../theme', () => ({ useColors: () => ({}) }))
vi.mock('../../preferences', () => ({ usePreferencesStore: (s: any) => s({ keyboardShortcuts: {}, setKeyboardShortcut: () => {}, resetKeyboardShortcut: () => {}, resetAllKeyboardShortcuts: () => {} }) }))
vi.mock('../../shortcuts/shortcut-catalog', async () => {
  const actual = await vi.importActual<any>('../../shortcuts/shortcut-catalog')
  return actual
})
vi.mock('../../shortcuts/chord', async () => {
  const actual = await vi.importActual<any>('../../shortcuts/chord')
  return actual
})
vi.mock('../ShortcutRow', () => ({ ShortcutRow: () => null }))
vi.mock('../SettingHeading', () => ({ SettingHeading: (p: any) => p.children }))

describe('SettingsDialog — shortcuts category search index', () => {
  it('search for "keyboard" matches the shortcuts category', async () => {
    const { searchSettings } = await import('../settings/settings-search-index')
    const results = searchSettings('keyboard')
    expect(results.has('shortcuts')).toBe(true)
  })

  it('search for "shortcut" matches the shortcuts category', async () => {
    const { searchSettings } = await import('../settings/settings-search-index')
    const results = searchSettings('shortcut')
    expect(results.has('shortcuts')).toBe(true)
  })

  it('search for "keybinding" matches the shortcuts category', async () => {
    const { searchSettings } = await import('../settings/settings-search-index')
    const results = searchSettings('keybinding')
    expect(results.has('shortcuts')).toBe(true)
  })

  it('search for "hotkey" matches the shortcuts category', async () => {
    const { searchSettings } = await import('../settings/settings-search-index')
    const results = searchSettings('hotkey')
    expect(results.has('shortcuts')).toBe(true)
  })

  it('KeyboardShortcutsCategory is importable without crashing', async () => {
    const { KeyboardShortcutsCategory } = await import('../settings/KeyboardShortcutsCategory')
    expect(typeof KeyboardShortcutsCategory).toBe('function')
  })
})
