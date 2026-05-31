// ion_list_extensions tool -- enumerate installed extensions.
//
// Walks `~/.ion/extensions/` and reports each subdirectory that looks
// like an extension (has an `index.ts`, `extension.ts`, or `index.js`
// entry point). For each, reports the entry-point path, manifest fields
// (when `extension.json` exists), and the count of agent markdown files.
//
// This is purely a discovery tool -- it does not parse code or check
// that the extension loads. Use `ion_inspect_extension` for a deeper
// scan of a specific extension.

import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'
import type { ToolDef } from '../../sdk/ion-sdk'

interface ListExtensionsParams {
  /** Root directory to scan. Defaults to `~/.ion/extensions/`. */
  dir?: string
}

interface ExtensionEntry {
  name: string
  path: string
  entry: string | null
  manifest: Record<string, unknown> | null
  agents: string[]
  isSDK?: boolean
}

const ENTRY_CANDIDATES = ['index.ts', 'extension.ts', 'index.js', 'main.ts', 'main.js'] as const

function findEntry(extDir: string): string | null {
  for (const c of ENTRY_CANDIDATES) {
    const p = join(extDir, c)
    if (existsSync(p)) return p
  }
  return null
}

function readManifest(extDir: string): Record<string, unknown> | null {
  const p = join(extDir, 'extension.json')
  if (!existsSync(p)) return null
  try {
    return JSON.parse(readFileSync(p, 'utf8'))
  } catch {
    return null
  }
}

function listAgents(extDir: string): string[] {
  const agentsDir = join(extDir, 'agents')
  if (!existsSync(agentsDir)) return []
  try {
    return readdirSync(agentsDir)
      .filter(f => f.endsWith('.md'))
      .map(f => f.replace(/\.md$/, ''))
      .sort()
  } catch {
    return []
  }
}

export function scanExtensions(root: string): { root: string; extensions: ExtensionEntry[] } {
  if (!existsSync(root)) {
    return { root, extensions: [] }
  }
  const entries: ExtensionEntry[] = []
  for (const child of readdirSync(root)) {
    const childPath = join(root, child)
    let s
    try { s = statSync(childPath) } catch { continue }
    if (!s.isDirectory()) continue
    if (child.startsWith('.')) continue

    // The sdk directory is special: it has no entry point of its own, it
    // is the bundled SDK consumed by other extensions.
    if (child === 'sdk') {
      entries.push({
        name: 'sdk',
        path: childPath,
        entry: null,
        manifest: null,
        agents: [],
        isSDK: true,
      })
      continue
    }

    entries.push({
      name: child,
      path: childPath,
      entry: findEntry(childPath),
      manifest: readManifest(childPath),
      agents: listAgents(childPath),
    })
  }
  entries.sort((a, b) => a.name.localeCompare(b.name))
  return { root, extensions: entries }
}

export const listExtensionsTool: ToolDef = {
  name: 'ion_list_extensions',
  description:
    'Enumerate extensions installed under `~/.ion/extensions/` (or a custom root). Reports entry-point path, manifest fields, and bundled agents.',
  parameters: {
    type: 'object',
    properties: {
      dir: {
        type: 'string',
        description: 'Absolute path to the extensions root directory. Defaults to `~/.ion/extensions/`.',
      },
    },
  },
  execute: async (params: ListExtensionsParams) => {
    const root = params.dir ?? join(homedir(), '.ion', 'extensions')
    return { content: JSON.stringify(scanExtensions(root), null, 2) }
  },
}
