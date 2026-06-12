/**
 * event-wiring-disk-seed — cold-start resource store injection
 *
 * When the engine delivers an empty resource snapshot for a disk-backed kind
 * (e.g. briefing), `injectDiskResourcesIfEmpty` reads persisted JSON files
 * from ~/.ion/resources/global/ and injects them into the renderer store via
 * executeJavaScript. This corrects the cold-start gap where the extension
 * subprocess dies during HandleQuery.
 */

import { vi, describe, it, expect, beforeEach } from 'vitest'

// ─── fs / os mocks ───────────────────────────────────────────────────────

const {
  mockExistsSync,
  mockReaddirSync,
  mockReadFileSync,
  mockExecuteJavaScript,
  mockState,
} = vi.hoisted(() => {
  const mockExistsSync = vi.fn()
  const mockReaddirSync = vi.fn()
  const mockReadFileSync = vi.fn()
  const mockExecuteJavaScript = vi.fn().mockResolvedValue('injected:2')
  const mockState = {
    mainWindow: {
      webContents: { executeJavaScript: mockExecuteJavaScript },
    } as any,
  }
  return { mockExistsSync, mockReaddirSync, mockReadFileSync, mockExecuteJavaScript, mockState }
})

vi.mock('fs', () => ({
  existsSync: mockExistsSync,
  readdirSync: mockReaddirSync,
  readFileSync: mockReadFileSync,
  writeFileSync: vi.fn(),
  mkdirSync: vi.fn(),
}))

vi.mock('os', () => ({ homedir: () => '/test-home' }))
vi.mock('../state', () => ({ state: mockState }))
vi.mock('../logger', () => ({ log: vi.fn(), debug: vi.fn(), warn: vi.fn(), error: vi.fn() }))

import { injectDiskResourcesIfEmpty } from '../event-wiring-disk-seed'

// ─── Helpers ───────────────────────────────────────────────────────────────

function makeBriefingJson(id: string, extra: Record<string, unknown> = {}): string {
  return JSON.stringify({ id, kind: 'briefing', title: `Briefing ${id}`, read: false, ...extra })
}

/** Sets up the fs mocks so that globalDir exists and contains given files. */
function withDiskFiles(files: Record<string, string>): void {
  const filenames = Object.keys(files)
  mockExistsSync.mockReturnValue(true)
  mockReaddirSync.mockReturnValue(filenames)
  mockReadFileSync.mockImplementation((p: string) => {
    const key = filenames.find((f) => p.endsWith(f))
    if (key) return files[key]
    throw new Error(`ENOENT: ${p}`)
  })
}

// ─── Tests ─────────────────────────────────────────────────────────────────

describe('injectDiskResourcesIfEmpty', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockState.mainWindow = {
      webContents: { executeJavaScript: mockExecuteJavaScript },
    }
  })

  it('injects disk items into the renderer store when disk has matching items', async () => {
    withDiskFiles({
      'briefing-001.json': makeBriefingJson('briefing-001'),
      'briefing-002.json': makeBriefingJson('briefing-002'),
    })

    injectDiskResourcesIfEmpty('briefing', 'sub-1', 'tab1:inst1')
    await Promise.resolve()

    expect(mockExecuteJavaScript).toHaveBeenCalledTimes(1)
    const code: string = mockExecuteJavaScript.mock.calls[0][0]

    // Injected JS must contain the kind key and both item IDs
    expect(code).toContain('"briefing"')
    expect(code).toContain('briefing-001')
    expect(code).toContain('briefing-002')
    // Guard: does not overwrite a store that already has items
    expect(code).toContain('already-populated')
  })

  it('does not call executeJavaScript when globalDir does not exist', () => {
    mockExistsSync.mockReturnValue(false)

    injectDiskResourcesIfEmpty('briefing', 'sub-1', 'tab1:inst1')
    expect(mockExecuteJavaScript).not.toHaveBeenCalled()
  })

  it('does not inject for kinds that are not disk-backed', () => {
    withDiskFiles({ 'some-id.json': makeBriefingJson('some-id') })

    injectDiskResourcesIfEmpty('unknown-kind', 'sub-1', 'tab1:inst1')
    expect(mockExecuteJavaScript).not.toHaveBeenCalled()
  })

  it('skips files whose kind field does not match the requested kind', () => {
    withDiskFiles({
      'other.json': JSON.stringify({ id: 'other-001', kind: 'task', title: 'Not a briefing' }),
    })

    injectDiskResourcesIfEmpty('briefing', 'sub-1', 'tab1:inst1')
    // Nothing matched the 'briefing' kind — no injection
    expect(mockExecuteJavaScript).not.toHaveBeenCalled()
  })

  it('skips corrupt JSON files without throwing', async () => {
    withDiskFiles({
      'bad.json': 'not-valid-json',
      'good.json': makeBriefingJson('briefing-good'),
    })

    expect(() => injectDiskResourcesIfEmpty('briefing', 'sub-1', 'tab1:inst1')).not.toThrow()
    await Promise.resolve()

    expect(mockExecuteJavaScript).toHaveBeenCalledTimes(1)
    const code: string = mockExecuteJavaScript.mock.calls[0][0]
    expect(code).toContain('briefing-good')
  })

  it('does nothing when mainWindow is null', () => {
    withDiskFiles({ 'briefing-001.json': makeBriefingJson('briefing-001') })
    mockState.mainWindow = null

    expect(() => injectDiskResourcesIfEmpty('briefing', 'sub-1', 'tab1:inst1')).not.toThrow()
    expect(mockExecuteJavaScript).not.toHaveBeenCalled()
  })

  it('does not inject when disk has no matching .json files', () => {
    mockExistsSync.mockReturnValue(true)
    mockReaddirSync.mockReturnValue(['readme.txt', 'notes.md'])

    injectDiskResourcesIfEmpty('briefing', 'sub-1', 'tab1:inst1')
    expect(mockExecuteJavaScript).not.toHaveBeenCalled()
  })
})
