#!/usr/bin/env npx tsx
/**
 * recover-tabs.ts
 *
 * One-shot script that recovers lost tabs from session-labels-api.json.
 *
 * For each labeled session that:
 *   1. Has a conversation file on disk (.tree.jsonl or .jsonl)
 *   2. Is NOT already referenced by an existing tab in tabs-api.json
 *
 * …it builds a PersistedTab and appends it to tabs-api.json.
 *
 * Usage:
 *   npx tsx desktop/scripts/recover-tabs.ts
 *   # or: npx ts-node desktop/scripts/recover-tabs.ts
 *
 * Idempotent — safe to run multiple times.
 */

import * as fs from 'fs'
import * as path from 'path'
import * as os from 'os'

// ─── Paths ───

const SETTINGS_DIR = path.join(os.homedir(), '.ion')
const CONVERSATIONS_DIR = path.join(SETTINGS_DIR, 'conversations')
const TABS_PATH = path.join(SETTINGS_DIR, 'tabs-api.json')
const LABELS_PATH = path.join(SETTINGS_DIR, 'session-labels-api.json')
const CHAINS_PATH = path.join(SETTINGS_DIR, 'session-chains-api.json')
const BACKUP_PATH = path.join(SETTINGS_DIR, 'tabs-api.json.pre-recovery')

const DEFAULT_WORKING_DIR = '/Users/Shared/source/personal/ion'

// ─── Types (minimal, matching types-persistence.ts) ───

interface PersistedTab {
  conversationId: string | null
  historicalSessionIds?: string[]
  title: string
  customTitle: string | null
  workingDirectory: string
  hasChosenDirectory: boolean
  additionalDirs: string[]
  permissionMode: 'auto' | 'plan'
  [key: string]: unknown
}

interface PersistedTabState {
  activeSessionId: string | null
  activeTabIndex?: number | null
  tabs: PersistedTab[]
  [key: string]: unknown
}

interface SessionChains {
  chains: Record<string, string[]>
  reverse: Record<string, string>
}

// ─── Helpers ───

function atomicWriteFileSync(filePath: string, data: string): void {
  const tmpPath = filePath + '.tmp'
  fs.writeFileSync(tmpPath, data, 'utf-8')
  fs.renameSync(tmpPath, filePath)
}

function conversationFilePath(sid: string): string | null {
  const treePath = path.join(CONVERSATIONS_DIR, `${sid}.tree.jsonl`)
  if (fs.existsSync(treePath)) return treePath
  const plainPath = path.join(CONVERSATIONS_DIR, `${sid}.jsonl`)
  if (fs.existsSync(plainPath)) return plainPath
  return null
}

function readWorkingDirectory(filePath: string): string | null {
  try {
    const fd = fs.openSync(filePath, 'r')
    const buf = Buffer.alloc(4096)
    const bytesRead = fs.readSync(fd, buf, 0, 4096, 0)
    fs.closeSync(fd)
    const firstLine = buf.subarray(0, bytesRead).toString('utf-8').split('\n')[0]
    const header = JSON.parse(firstLine)
    if (typeof header.workingDirectory === 'string') {
      return header.workingDirectory
    }
  } catch {
    // ignore parse errors
  }
  return null
}

/**
 * Collect every session ID already referenced by existing tabs so we can skip them.
 */
function collectExistingSessionIds(tabState: PersistedTabState): Set<string> {
  const ids = new Set<string>()
  for (const tab of tabState.tabs) {
    // CLI tab conversationId
    if (tab.conversationId) ids.add(tab.conversationId)

    // Historical session IDs on any tab
    if (Array.isArray(tab.historicalSessionIds)) {
      for (const sid of tab.historicalSessionIds) ids.add(sid)
    }

    // Engine tab per-instance session IDs
    const engineSessionIds = tab.engineSessionIds as Record<string, string> | undefined
    if (engineSessionIds && typeof engineSessionIds === 'object') {
      for (const sid of Object.values(engineSessionIds)) {
        if (sid) ids.add(sid)
      }
    }
  }
  return ids
}

/**
 * Given a labeled session ID and the chains data, compute:
 *   - conversationId: the tip of the chain (the most recent session)
 *   - historicalSessionIds: all sessions before the tip
 */
function resolveChain(
  sid: string,
  chains: SessionChains
): { conversationId: string; historicalSessionIds: string[] } {
  let fullChain: string[]

  if (sid in chains.reverse) {
    // This session is a continuation — find its root
    const root = chains.reverse[sid]
    fullChain = [root, ...(chains.chains[root] || [])]
  } else if (sid in chains.chains) {
    // This session IS a chain root
    fullChain = [sid, ...chains.chains[sid]]
  } else {
    // Standalone session
    return { conversationId: sid, historicalSessionIds: [] }
  }

  const tip = fullChain[fullChain.length - 1]
  const historical = fullChain.slice(0, -1)
  return { conversationId: tip, historicalSessionIds: historical }
}

// ─── Main ───

function main(): void {
  console.log('🔍 Ion Tab Recovery Script\n')

  // 1. Read inputs
  if (!fs.existsSync(LABELS_PATH)) {
    console.error(`❌ Labels file not found: ${LABELS_PATH}`)
    process.exit(1)
  }
  if (!fs.existsSync(CHAINS_PATH)) {
    console.error(`❌ Chains file not found: ${CHAINS_PATH}`)
    process.exit(1)
  }

  const labels: Record<string, string> = JSON.parse(fs.readFileSync(LABELS_PATH, 'utf-8'))
  const chainsData: SessionChains = JSON.parse(fs.readFileSync(CHAINS_PATH, 'utf-8'))

  let tabState: PersistedTabState
  if (fs.existsSync(TABS_PATH)) {
    tabState = JSON.parse(fs.readFileSync(TABS_PATH, 'utf-8'))
  } else {
    tabState = { activeSessionId: null, activeTabIndex: 0, tabs: [] }
  }

  console.log(`  📋 Labeled sessions: ${Object.keys(labels).length}`)
  console.log(`  📂 Existing tabs:    ${tabState.tabs.length}`)
  console.log(`  🔗 Chain roots:      ${Object.keys(chainsData.chains).length}`)
  console.log(`  🔗 Chain entries:    ${Object.keys(chainsData.reverse).length}`)
  console.log()

  // 2. Collect all session IDs already in tabs-api.json
  const existingIds = collectExistingSessionIds(tabState)
  console.log(`  🔒 Session IDs already in tabs: ${existingIds.size}`)

  // Also track conversationIds we're adding, to avoid duplicates from the same chain
  const addedConversationIds = new Set<string>()

  // 3. Process each labeled session
  const recovered: PersistedTab[] = []
  const skippedAlreadyInTabs: string[] = []
  const skippedNoFile: string[] = []
  const skippedDuplicateChain: string[] = []

  for (const [sid, title] of Object.entries(labels)) {
    // Resolve chain to get the tip (conversationId) and history
    const { conversationId, historicalSessionIds } = resolveChain(sid, chainsData)

    // Skip if the tip is already referenced by an existing tab
    if (existingIds.has(conversationId)) {
      skippedAlreadyInTabs.push(sid)
      continue
    }

    // Skip if any session in the chain is already in an existing tab
    const allInChain = [conversationId, ...historicalSessionIds]
    if (allInChain.some((id) => existingIds.has(id))) {
      skippedAlreadyInTabs.push(sid)
      continue
    }

    // Skip if we already added a tab for this chain tip (multiple labels can
    // point to different points in the same chain)
    if (addedConversationIds.has(conversationId)) {
      skippedDuplicateChain.push(sid)
      continue
    }

    // Find the conversation file for the tip
    const tipFile = conversationFilePath(conversationId)
    if (!tipFile) {
      // If tip has no file, try the labeled session itself (it might be mid-chain
      // with the file on an earlier session)
      const sidFile = conversationFilePath(sid)
      if (!sidFile) {
        skippedNoFile.push(sid)
        continue
      }
      // Use the labeled session directly as the conversationId
      // (the chain tip file is gone but this session's file exists)
      const workingDirectory = readWorkingDirectory(sidFile) || DEFAULT_WORKING_DIR

      recovered.push({
        conversationId: sid,
        historicalSessionIds: [],
        title,
        customTitle: title,
        workingDirectory,
        hasChosenDirectory: true,
        additionalDirs: [],
        permissionMode: 'plan',
      })
      addedConversationIds.add(sid)
      continue
    }

    // Read working directory from the conversation file
    const workingDirectory = readWorkingDirectory(tipFile) || DEFAULT_WORKING_DIR

    const tab: PersistedTab = {
      conversationId,
      historicalSessionIds: historicalSessionIds.length > 0 ? historicalSessionIds : undefined,
      title,
      customTitle: title,
      workingDirectory,
      hasChosenDirectory: true,
      additionalDirs: [],
      permissionMode: 'plan',
    }

    // Clean up undefined fields
    if (tab.historicalSessionIds === undefined) delete tab.historicalSessionIds

    recovered.push(tab)
    addedConversationIds.add(conversationId)
  }

  // 4. Report
  console.log('─── Summary ───\n')
  console.log(`  ✅ Tabs to recover:           ${recovered.length}`)
  console.log(`  ⏭️  Skipped (already in tabs): ${skippedAlreadyInTabs.length}`)
  console.log(`  ⏭️  Skipped (no conv file):    ${skippedNoFile.length}`)
  console.log(`  ⏭️  Skipped (duplicate chain): ${skippedDuplicateChain.length}`)
  console.log()

  if (skippedNoFile.length > 0) {
    console.log('  Sessions with no conversation file on disk:')
    for (const sid of skippedNoFile) {
      console.log(`    - ${sid}: ${labels[sid]}`)
    }
    console.log()
  }

  if (recovered.length === 0) {
    console.log('  Nothing to recover — all labeled sessions are already in tabs or have no files.')
    return
  }

  console.log('  Recovering:')
  for (const tab of recovered) {
    const histCount = tab.historicalSessionIds?.length ?? 0
    const histSuffix = histCount > 0 ? ` (${histCount} prior sessions in chain)` : ''
    console.log(`    + "${tab.title}" → ${tab.conversationId}${histSuffix}`)
  }
  console.log()

  // 5. Merge recovered tabs into tab state
  tabState.tabs = [...tabState.tabs, ...recovered]

  // 6. Write to staging file (not the live tabs file, which the running app
  //    would immediately overwrite with its in-memory state).
  const STAGING_PATH = path.join(SETTINGS_DIR, 'tabs-api.json.recovered')
  const output = JSON.stringify(tabState, null, 2) + '\n'
  atomicWriteFileSync(STAGING_PATH, output)

  console.log(`  ✅ Wrote ${tabState.tabs.length} tabs to: ${STAGING_PATH}`)
  console.log(`     (${tabState.tabs.length - recovered.length} existing + ${recovered.length} recovered)`)
  console.log()
  console.log('  To apply:')
  console.log('    1. Quit Ion Desktop')
  console.log(`    2. cp ${TABS_PATH} ${BACKUP_PATH}`)
  console.log(`    3. mv ${STAGING_PATH} ${TABS_PATH}`)
  console.log('    4. Restart Ion Desktop')
  console.log()
  console.log('  Or, if Ion Desktop is already stopped:')
  console.log(`    mv ${STAGING_PATH} ${TABS_PATH}`)
}

main()
