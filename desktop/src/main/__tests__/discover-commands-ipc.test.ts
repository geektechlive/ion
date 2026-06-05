/**
 * Regression test for the IPC.DISCOVER_COMMANDS handler in
 * `ipc/sessions-list.ts`.
 *
 * Bug: when `enableClaudeCompat` was disabled in desktop settings, the
 * handler short-circuited to `[]` for the entire result set, hiding
 * `~/.ion/commands/` and `{project}/.ion/commands/` entries. Only the
 * renderer's built-in `/clear` remained in the autocomplete list.
 *
 * Fix: always call `discoverCommands`, then filter out
 * `origin === 'claude'` entries when the setting is off. Ion-native
 * commands are always available; only `.claude/*` paths are gated by
 * the setting (matching the expansion-time gate in
 * `slash-classify.ts`).
 *
 * Sibling regression: the iOS `discover_commands` remote handler in
 * `remote/handlers/tabs.ts` had no gate at all, so iOS would show
 * `.claude/*` entries even when the desktop setting was off. The same
 * filter is applied there for desktop↔iOS parity.
 */

import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest'
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'fs'
import { tmpdir } from 'os'
import { join } from 'path'

const handlers = new Map<string, (...args: any[]) => any>()

vi.mock('electron', () => ({
  ipcMain: {
    handle: (channel: string, handler: (...args: any[]) => any) => {
      handlers.set(channel, handler)
    },
    on: (channel: string, handler: (...args: any[]) => any) => {
      handlers.set(channel, handler)
    },
  },
}))

// Mutable settings shim so each test can flip enableClaudeCompat.
const settingsMock = vi.hoisted(() => ({ enableClaudeCompat: true as boolean }))

vi.mock('../settings-store', () => ({
  readSettings: () => ({ enableClaudeCompat: settingsMock.enableClaudeCompat }),
  SETTINGS_DEFAULTS: { enableClaudeCompat: true },
  currentBackend: 'cli',
  loadSessionLabels: () => ({}),
}))

vi.mock('../logger', () => ({
  log: vi.fn(),
}))

// Mock os.homedir so the discovery function reads from our temp dir
// instead of the real user home. command-discovery.ts imports `homedir`
// from 'os' at module scope, so this mock has to be installed before
// the registerSessionsListIpc import lower in the file.
const homeMock = vi.hoisted(() => ({ home: '' }))
vi.mock('os', async () => {
  const actual = await vi.importActual<typeof import('os')>('os')
  return {
    ...actual,
    homedir: () => homeMock.home,
  }
})

// state / session-meta dependencies are not exercised by DISCOVER_COMMANDS
// but must mock cleanly so registerSessionsListIpc imports without error.
vi.mock('../state', () => ({
  sessionPlane: {
    loadSessionHistory: vi.fn(),
    getConversation: vi.fn(),
  },
}))
vi.mock('../session-meta', () => ({
  cleanCliTags: (s: string) => s,
  collapseSessionChains: (s: any) => s,
  decodeProjectPath: () => null,
  extractBashEntries: () => ({ bashEntries: [] }),
  extractTag: () => null,
  loadClaudeSessionMessages: () => [],
  loadEngineConversationMessages: () => [],
  parseSessionMeta: () => null,
}))

import { registerSessionsListIpc } from '../ipc/sessions-list'

let tempRoot: string
let projectPath: string

beforeEach(() => {
  handlers.clear()
  settingsMock.enableClaudeCompat = true

  // Build a fake home + project on disk.
  tempRoot = mkdtempSync(join(tmpdir(), 'ion-discover-test-'))
  const fakeHome = join(tempRoot, 'home')
  projectPath = join(tempRoot, 'project')

  // ~/.ion/commands/foo.md   (ion user)
  mkdirSync(join(fakeHome, '.ion', 'commands'), { recursive: true })
  writeFileSync(join(fakeHome, '.ion', 'commands', 'foo.md'), 'foo: ion-user command\n')

  // ~/.claude/commands/bar.md   (claude user)
  mkdirSync(join(fakeHome, '.claude', 'commands'), { recursive: true })
  writeFileSync(join(fakeHome, '.claude', 'commands', 'bar.md'), 'bar: claude-user command\n')

  // {project}/.ion/commands/baz.md   (ion project)
  mkdirSync(join(projectPath, '.ion', 'commands'), { recursive: true })
  writeFileSync(join(projectPath, '.ion', 'commands', 'baz.md'), 'baz: ion-project command\n')

  // {project}/.claude/commands/qux.md   (claude project)
  mkdirSync(join(projectPath, '.claude', 'commands'), { recursive: true })
  writeFileSync(join(projectPath, '.claude', 'commands', 'qux.md'), 'qux: claude-project command\n')

  // ~/.claude/skills/myskill/SKILL.md   (claude user skill)
  mkdirSync(join(fakeHome, '.claude', 'skills', 'myskill'), { recursive: true })
  writeFileSync(
    join(fakeHome, '.claude', 'skills', 'myskill', 'SKILL.md'),
    '---\ndescription: A test skill\n---\nbody\n',
  )

  homeMock.home = fakeHome
  registerSessionsListIpc()
})

afterEach(() => {
  rmSync(tempRoot, { recursive: true, force: true })
})

describe('IPC.DISCOVER_COMMANDS handler', () => {
  it('returns the full ion + claude union when enableClaudeCompat is true', async () => {
    settingsMock.enableClaudeCompat = true
    const handler = handlers.get('ion:discover-commands')
    expect(handler).toBeDefined()

    const result: Array<{ name: string; origin: string }> = await handler!(null, projectPath)
    const names = result.map((c) => c.name).sort()

    expect(names).toEqual(['bar', 'baz', 'foo', 'myskill', 'qux'])
  })

  it('returns only ion-native entries when enableClaudeCompat is false (regression: ion-native commands must not disappear from the autocomplete list)', async () => {
    settingsMock.enableClaudeCompat = false
    const handler = handlers.get('ion:discover-commands')
    expect(handler).toBeDefined()

    const result: Array<{ name: string; origin: string }> = await handler!(null, projectPath)
    const names = result.map((c) => c.name).sort()

    // foo (~/.ion/commands) and baz ({project}/.ion/commands) are ion-native
    // and must always survive. bar (~/.claude/commands),
    // qux ({project}/.claude/commands), and myskill (~/.claude/skills) are
    // all gated by the setting.
    expect(names).toEqual(['baz', 'foo'])

    // Every surviving entry must carry origin='ion' so future filters can
    // re-derive the directory family without re-scanning paths.
    expect(result.every((c) => c.origin === 'ion')).toBe(true)
  })

  it('stamps every returned entry with an origin field (contract: DiscoveredCommand.origin is required)', async () => {
    settingsMock.enableClaudeCompat = true
    const handler = handlers.get('ion:discover-commands')
    const result: Array<{ name: string; origin: string }> = await handler!(null, projectPath)

    for (const c of result) {
      expect(c.origin).toMatch(/^(ion|claude)$/)
    }

    const byName = new Map(result.map((c) => [c.name, c.origin]))
    expect(byName.get('foo')).toBe('ion')          // ~/.ion/commands
    expect(byName.get('baz')).toBe('ion')          // {project}/.ion/commands
    expect(byName.get('bar')).toBe('claude')       // ~/.claude/commands
    expect(byName.get('qux')).toBe('claude')       // {project}/.claude/commands
    expect(byName.get('myskill')).toBe('claude')   // ~/.claude/skills
  })

  it('rejects invalid project paths without scanning the filesystem', async () => {
    settingsMock.enableClaudeCompat = true
    const handler = handlers.get('ion:discover-commands')
    // Validator requires absolute paths; a relative path should bounce.
    const result = await handler!(null, 'relative/path')
    expect(result).toEqual([])
  })
})
