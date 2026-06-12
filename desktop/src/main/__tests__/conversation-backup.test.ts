import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'fs'
import { tmpdir } from 'os'
import { join } from 'path'
import { runExport } from '../conversation-backup/export'
import { previewRestore, runRestore } from '../conversation-backup/restore'
import { validateManifest, buildManifest } from '../conversation-backup/manifest'

let root: string
let conversationsDir: string
let ionHome: string

beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), 'ion-backup-test-'))
  ionHome = join(root, '.ion')
  conversationsDir = join(ionHome, 'conversations')
  // Create the home + conversations dir structure
  require('fs').mkdirSync(conversationsDir, { recursive: true })
})

afterEach(() => {
  try { rmSync(root, { recursive: true, force: true }) } catch {}
})

function writeConversationFiles(id: string, content: { llm: string; tree: string; memory?: string }) {
  writeFileSync(join(conversationsDir, id + '.llm.jsonl'), content.llm)
  writeFileSync(join(conversationsDir, id + '.tree.jsonl'), content.tree)
  if (content.memory) writeFileSync(join(conversationsDir, id + '.memory.md'), content.memory)
}

function writeMetadataFile(name: string, content: any) {
  writeFileSync(join(ionHome, name), JSON.stringify(content, null, 2))
}

describe('export / restore round-trip', () => {
  it('exports all conversations and restores them byte-for-byte (skip policy on empty dest)', async () => {
    // Arrange: two conversations on disk.
    writeConversationFiles('conv-A', {
      llm: '{"meta":true,"id":"conv-A","version":2}\n{"role":"user","content":"hi"}\n',
      tree: '{"meta":true,"id":"conv-A","version":2}\n{"id":"e1","type":"message"}\n',
      memory: '# Session memory for conv-A\n',
    })
    writeConversationFiles('conv-B', {
      llm: '{"meta":true,"id":"conv-B","version":2}\n',
      tree: '{"meta":true,"id":"conv-B","version":2}\n',
    })
    writeMetadataFile('tabs-api.json', { tabs: [{ conversationId: 'conv-A' }, { conversationId: 'conv-B' }] })
    writeMetadataFile('session-chains-api.json', { chains: { 'conv-A': [] }, reverse: {} })
    writeMetadataFile('session-labels-api.json', { 'conv-A': 'Important' })

    const zipPath = join(root, 'backup.zip')
    const exportResult = await runExport({
      scope: 'all',
      destinationPath: zipPath,
      sources: {
        conversationsDir,
        tabsFiles: [join(ionHome, 'tabs-api.json'), join(ionHome, 'tabs-cli.json')],
        chainsFiles: [join(ionHome, 'session-chains-api.json'), join(ionHome, 'session-chains-cli.json')],
        labelsFiles: [join(ionHome, 'session-labels-api.json'), join(ionHome, 'session-labels-cli.json')],
      },
      ionVersion: '1.0.0-test',
      backendSnapshot: 'api',
    })
    expect(exportResult.ok).toBe(true)
    expect(exportResult.conversationCount).toBe(2)
    expect(existsSync(zipPath)).toBe(true)

    // Wipe the conversations directory.
    rmSync(conversationsDir, { recursive: true, force: true })
    require('fs').mkdirSync(conversationsDir, { recursive: true })

    // Act: restore.
    const restoreResult = await runRestore({
      zipPath,
      conflictPolicy: 'skip',
      restoreTabs: false,
      sources: { conversationsDir, ionHomeDir: ionHome },
    })

    // Assert: both conversations recovered with original content.
    expect(restoreResult.ok).toBe(true)
    expect(restoreResult.restored).toBe(5) // 2 llm + 2 tree + 1 memory
    expect(readFileSync(join(conversationsDir, 'conv-A.llm.jsonl'), 'utf-8')).toBe(
      '{"meta":true,"id":"conv-A","version":2}\n{"role":"user","content":"hi"}\n',
    )
    expect(readFileSync(join(conversationsDir, 'conv-B.llm.jsonl'), 'utf-8')).toBe(
      '{"meta":true,"id":"conv-B","version":2}\n',
    )
    expect(readFileSync(join(conversationsDir, 'conv-A.memory.md'), 'utf-8')).toBe(
      '# Session memory for conv-A\n',
    )
  })

  it('skip policy preserves local files when they already exist', async () => {
    writeConversationFiles('conv-A', {
      llm: '{"original":"content"}\n',
      tree: '{"original":"tree"}\n',
    })

    const zipPath = join(root, 'backup.zip')
    await runExport({
      scope: 'all',
      destinationPath: zipPath,
      sources: {
        conversationsDir,
        tabsFiles: [],
        chainsFiles: [],
        labelsFiles: [],
      },
      ionVersion: '1.0.0-test',
      backendSnapshot: 'api',
    })

    // Replace local files with different content.
    writeConversationFiles('conv-A', {
      llm: '{"newer":"content"}\n',
      tree: '{"newer":"tree"}\n',
    })

    const result = await runRestore({
      zipPath,
      conflictPolicy: 'skip',
      restoreTabs: false,
      sources: { conversationsDir, ionHomeDir: ionHome },
    })

    expect(result.ok).toBe(true)
    expect(result.skipped).toBe(2)
    expect(result.restored).toBe(0)
    expect(result.overwritten).toBe(0)
    // Local content preserved.
    expect(readFileSync(join(conversationsDir, 'conv-A.llm.jsonl'), 'utf-8')).toBe('{"newer":"content"}\n')
  })

  it('overwrite policy replaces local files', async () => {
    writeConversationFiles('conv-A', { llm: '{"original":true}\n', tree: '{"tree-original":true}\n' })

    const zipPath = join(root, 'backup.zip')
    await runExport({
      scope: 'all',
      destinationPath: zipPath,
      sources: { conversationsDir, tabsFiles: [], chainsFiles: [], labelsFiles: [] },
      ionVersion: '1.0.0-test',
      backendSnapshot: 'api',
    })

    writeConversationFiles('conv-A', { llm: '{"local-changed":true}\n', tree: '{"local-tree-changed":true}\n' })

    const result = await runRestore({
      zipPath,
      conflictPolicy: 'overwrite',
      restoreTabs: false,
      sources: { conversationsDir, ionHomeDir: ionHome },
    })

    expect(result.ok).toBe(true)
    expect(result.overwritten).toBe(2)
    expect(readFileSync(join(conversationsDir, 'conv-A.llm.jsonl'), 'utf-8')).toBe('{"original":true}\n')
  })

  it('rename policy writes files under fresh IDs and leaves local intact', async () => {
    writeConversationFiles('conv-A', { llm: '{"backup-llm":true}\n', tree: '{"backup-tree":true}\n' })

    const zipPath = join(root, 'backup.zip')
    await runExport({
      scope: 'all',
      destinationPath: zipPath,
      sources: { conversationsDir, tabsFiles: [], chainsFiles: [], labelsFiles: [] },
      ionVersion: '1.0.0-test',
      backendSnapshot: 'api',
    })

    writeConversationFiles('conv-A', { llm: '{"local-llm":true}\n', tree: '{"local-tree":true}\n' })

    const result = await runRestore({
      zipPath,
      conflictPolicy: 'rename',
      restoreTabs: false,
      sources: { conversationsDir, ionHomeDir: ionHome },
    })

    expect(result.ok).toBe(true)
    expect(result.renamed).toBe(2)
    // Local files untouched.
    expect(readFileSync(join(conversationsDir, 'conv-A.llm.jsonl'), 'utf-8')).toBe('{"local-llm":true}\n')

    // A fresh-ID file should now exist alongside conv-A with the backup
    // content. The rename uses {millis}-{12hex} so we can find it by
    // looking for any new .llm.jsonl that isn't conv-A.
    const files: string[] = require('fs').readdirSync(conversationsDir)
    const newLlm = files.find((f: string) => f.endsWith('.llm.jsonl') && f !== 'conv-A.llm.jsonl')
    expect(newLlm).toBeDefined()
    if (newLlm) {
      expect(readFileSync(join(conversationsDir, newLlm), 'utf-8')).toBe('{"backup-llm":true}\n')
      // Filename pattern: <13-digit millis>-<12 hex chars>.llm.jsonl
      expect(newLlm).toMatch(/^\d{13}-[0-9a-f]{12}\.llm\.jsonl$/)
    }
  })

  it('preview reads the manifest without extracting', async () => {
    writeConversationFiles('conv-A', { llm: '{}\n', tree: '{}\n' })
    writeMetadataFile('tabs-api.json', { tabs: [{ conversationId: 'conv-A' }] })

    const zipPath = join(root, 'backup.zip')
    await runExport({
      scope: 'currently-open',
      destinationPath: zipPath,
      sources: {
        conversationsDir,
        tabsFiles: [join(ionHome, 'tabs-api.json')],
        chainsFiles: [],
        labelsFiles: [],
      },
      ionVersion: '1.2.3-test',
      backendSnapshot: 'api',
    })

    const preview = await previewRestore(zipPath)
    expect(preview.ok).toBe(true)
    expect(preview.manifest?.ionVersion).toBe('1.2.3-test')
    expect(preview.manifest?.scope).toBe('currently-open')
    expect(preview.manifest?.backendSnapshot).toBe('api')
  })

  it('restoreTabs=false leaves the local tabs file alone', async () => {
    writeConversationFiles('conv-A', { llm: '{}\n', tree: '{}\n' })
    writeMetadataFile('tabs-api.json', { tabs: [{ conversationId: 'conv-A', title: 'in-backup' }] })

    const zipPath = join(root, 'backup.zip')
    await runExport({
      scope: 'all',
      destinationPath: zipPath,
      sources: {
        conversationsDir,
        tabsFiles: [join(ionHome, 'tabs-api.json')],
        chainsFiles: [],
        labelsFiles: [],
      },
      ionVersion: '1.0.0',
      backendSnapshot: 'api',
    })

    // Change the local tabs file before restoring.
    writeMetadataFile('tabs-api.json', { tabs: [{ conversationId: 'conv-Z', title: 'local-only' }] })

    await runRestore({
      zipPath,
      conflictPolicy: 'skip',
      restoreTabs: false,
      sources: { conversationsDir, ionHomeDir: ionHome },
    })

    const localTabs = JSON.parse(readFileSync(join(ionHome, 'tabs-api.json'), 'utf-8'))
    // Tabs file was not touched.
    expect(localTabs.tabs).toEqual([{ conversationId: 'conv-Z', title: 'local-only' }])
  })

  it('restoreTabs=true merges backup tabs into local without overwriting existing tab IDs', async () => {
    writeConversationFiles('conv-A', { llm: '{}\n', tree: '{}\n' })
    writeMetadataFile('tabs-api.json', {
      tabs: [
        { conversationId: 'conv-A', title: 'in-backup-A' },
        { conversationId: 'conv-only-in-backup', title: 'unique-to-backup' },
      ],
    })

    const zipPath = join(root, 'backup.zip')
    await runExport({
      scope: 'all',
      destinationPath: zipPath,
      sources: {
        conversationsDir,
        tabsFiles: [join(ionHome, 'tabs-api.json')],
        chainsFiles: [],
        labelsFiles: [],
      },
      ionVersion: '1.0.0',
      backendSnapshot: 'api',
    })

    writeMetadataFile('tabs-api.json', {
      tabs: [{ conversationId: 'conv-A', title: 'updated-locally-after-export' }],
    })

    await runRestore({
      zipPath,
      conflictPolicy: 'skip',
      restoreTabs: true,
      sources: { conversationsDir, ionHomeDir: ionHome },
    })

    const merged = JSON.parse(readFileSync(join(ionHome, 'tabs-api.json'), 'utf-8'))
    // Local conv-A is preserved (not overwritten by backup version).
    const convA = merged.tabs.find((t: any) => t.conversationId === 'conv-A')
    expect(convA.title).toBe('updated-locally-after-export')
    // conv-only-in-backup is appended.
    const backupOnly = merged.tabs.find((t: any) => t.conversationId === 'conv-only-in-backup')
    expect(backupOnly).toBeDefined()
    expect(backupOnly.title).toBe('unique-to-backup')
  })
})

describe('manifest', () => {
  it('builds a manifest with the expected shape', () => {
    const m = buildManifest({
      scope: 'all',
      conversationCount: 42,
      backendSnapshot: 'api',
      ionVersion: '1.0.0',
      hostname: 'test-host',
    })
    expect(m.version).toBe(1)
    expect(m.scope).toBe('all')
    expect(m.conversationCount).toBe(42)
    expect(m.backendSnapshot).toBe('api')
    expect(m.ionVersion).toBe('1.0.0')
    expect(m.hostname).toBe('test-host')
    expect(m.createdBy).toBe('ion-desktop')
    expect(typeof m.createdAt).toBe('string')
  })

  it('rejects manifest with unsupported version', () => {
    const result = validateManifest({
      version: 9999,
      createdAt: '2026-06-08T00:00:00Z',
      createdBy: 'ion-desktop',
      ionVersion: '1.0',
      scope: 'all',
      conversationCount: 1,
      backendSnapshot: 'api',
      hostname: 'host',
    })
    expect(typeof result).toBe('string')
    if (typeof result === 'string') {
      expect(result).toContain('unsupported manifest.version')
    }
  })

  it('rejects manifest with missing required fields', () => {
    expect(validateManifest({})).toBeTypeOf('string')
    expect(validateManifest(null)).toBeTypeOf('string')
    expect(validateManifest('not an object')).toBeTypeOf('string')
  })

  it('accepts a fully-valid manifest and echoes its fields back', () => {
    const result = validateManifest({
      version: 1,
      createdAt: '2026-06-08T00:00:00Z',
      createdBy: 'ion-desktop',
      ionVersion: '1.0.0',
      scope: 'all',
      conversationCount: 7,
      backendSnapshot: 'cli',
      hostname: 'mac-mini',
    })
    expect(typeof result).toBe('object')
    if (typeof result === 'object') {
      expect(result.version).toBe(1)
      expect(result.scope).toBe('all')
      expect(result.hostname).toBe('mac-mini')
    }
  })
})
