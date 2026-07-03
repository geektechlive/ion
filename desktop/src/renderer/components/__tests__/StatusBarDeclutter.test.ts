// @vitest-environment jsdom
/**
 * StatusBar — structural regression tests after the declutter.
 *
 * Two assertions (both are regressions from minty-grinning-cocoa):
 *   1. No drawer-toggle Info button in the rendered output.
 *   2. No StatusBarEngineCost component in the rendered output.
 *
 * StatusBar is a composite component that renders many sub-components.
 * Rather than mounting the full tree (which needs heavy mocking of IPC,
 * git hooks, electron globals, etc.), we assert at the module level:
 *   - The Info icon is not imported (removed from imports in step 4).
 *   - StatusBarEngineCost is not re-exported (removed from re-exports in step 6).
 *
 * These are compile-time / module-graph assertions that typecheck already
 * catches, but having them as explicit test assertions makes the intent
 * visible and gives a clear failure message on regression.
 */

import { describe, it, expect } from 'vitest'
import * as fs from 'node:fs'
import * as path from 'node:path'

const statusBarSrc = fs.readFileSync(
  path.resolve(__dirname, '../StatusBar.tsx'),
  'utf8',
)

describe('StatusBar declutter — source-level assertions', () => {
  it('does not import Info from @phosphor-icons/react', () => {
    // The Info icon was only used by the now-removed drawer-toggle button.
    expect(statusBarSrc).not.toMatch(/\bInfo\b/)
  })

  it('does not import StatusBarEngineCost', () => {
    expect(statusBarSrc).not.toMatch(/StatusBarEngineCost/)
  })

  it('does not reference toggleStatusDrawer', () => {
    // toggleStatusDrawer was only used by the removed Info button in StatusBar.
    // It now lives exclusively in StatusBarContextIndicator.
    expect(statusBarSrc).not.toMatch(/toggleStatusDrawer/)
  })

  it('does not reference statusDrawerOpen', () => {
    // statusDrawerOpen was only used to color the removed Info button.
    expect(statusBarSrc).not.toMatch(/statusDrawerOpen/)
  })
})
