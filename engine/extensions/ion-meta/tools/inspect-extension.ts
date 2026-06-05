// ion_inspect_extension tool -- parse an extension's source.
//
// Given an extension directory or its entry-point file, reads the source
// and reports every hook the extension registers, every tool it declares,
// every command it registers, and any manifest fields it ships with.
//
// The parser is a deliberately-shallow regex pass; it catches the
// canonical `ion.on(...)`, `ion.registerTool(...)`, `ion.registerCommand(...)`
// forms but does not understand dynamic registration (loops, conditional
// blocks). Authors that hide registrations behind a helper will need a
// proper AST tool -- for the common case this regex pass is sufficient
// and avoids pulling esbuild's analysis at runtime.

import { existsSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import type { ToolDef } from '../../sdk/ion-sdk'

interface InspectParams {
  /** Path to an extension directory or its entry-point file. */
  path: string
}

interface InspectReport {
  path: string
  entry: string | null
  manifest: Record<string, unknown> | null
  hooks: string[]
  tools: { name: string; description?: string }[]
  commands: { name: string; description?: string }[]
  imports: string[]
  externals: string[]
  bytes: number
  notes: string[]
}

const ENTRY_CANDIDATES = ['index.ts', 'extension.ts', 'index.js', 'main.ts', 'main.js'] as const

function resolveEntry(path: string): { entry: string | null; dir: string } {
  let stat
  try { stat = statSync(path) } catch {
    return { entry: null, dir: path }
  }
  if (stat.isFile()) {
    const dir = path.split('/').slice(0, -1).join('/') || '/'
    return { entry: path, dir }
  }
  // Directory: look for canonical entry-point names.
  for (const c of ENTRY_CANDIDATES) {
    const p = join(path, c)
    if (existsSync(p)) return { entry: p, dir: path }
  }
  return { entry: null, dir: path }
}

export function inspectExtension(params: InspectParams): InspectReport {
  const notes: string[] = []
  const { entry, dir } = resolveEntry(params.path)

  const manifest = (() => {
    const p = join(dir, 'extension.json')
    if (!existsSync(p)) return null
    try { return JSON.parse(readFileSync(p, 'utf8')) } catch { return null }
  })()

  if (!entry) {
    return {
      path: params.path,
      entry: null,
      manifest,
      hooks: [],
      tools: [],
      commands: [],
      imports: [],
      externals: [],
      bytes: 0,
      notes: ['No entry-point file found. Looked for: ' + ENTRY_CANDIDATES.join(', ')],
    }
  }

  const source = readFileSync(entry, 'utf8')

  // Hook registrations: `ion.on('hook_name', ...)`. We capture only the
  // first quoted string after `ion.on(`. Dynamic hook names elude this
  // pass; we note when we see a hint.
  const hooks: string[] = []
  const hookRe = /\bion\.on\(\s*['"`]([a-z_][a-z0-9_]*)['"`]/g
  for (let m; (m = hookRe.exec(source)); ) hooks.push(m[1])
  if (/\bion\.on\(\s*[a-zA-Z_]/.test(source.replace(hookRe, ''))) {
    notes.push('Found ion.on() calls with a non-literal hook name; the static scan may have missed some hooks.')
  }

  // Tool registrations: `ion.registerTool({ name: '<name>', description: '<desc>', ... })`.
  const tools: InspectReport['tools'] = []
  const toolRe = /\bion\.registerTool\(\s*\{([^}]*name\s*:\s*['"`]([a-zA-Z_][a-zA-Z0-9_]*)['"`][^}]*)\}/g
  for (let m; (m = toolRe.exec(source)); ) {
    const block = m[1]
    const name = m[2]
    const descMatch = block.match(/description\s*:\s*['"`]([^'"`]*)['"`]/)
    tools.push({ name, description: descMatch?.[1] })
  }
  // Pattern A above only matches when the registerTool object body has
  // no inner braces. Fall back to a multi-pass scan for nested-property
  // tool defs.
  if (tools.length === 0) {
    const fallbackRe = /ion\.registerTool\(\s*\{\s*name\s*:\s*['"`]([a-zA-Z_][a-zA-Z0-9_]*)['"`]/g
    for (let m; (m = fallbackRe.exec(source)); ) tools.push({ name: m[1] })
  }

  // Command registrations: `ion.registerCommand('<name>', { description: '<d>', ... })`.
  const commands: InspectReport['commands'] = []
  const cmdRe = /\bion\.registerCommand\(\s*['"`]([a-zA-Z_][a-zA-Z0-9_-]*)['"`]\s*,\s*\{([^}]*)\}/g
  for (let m; (m = cmdRe.exec(source)); ) {
    const name = m[1]
    const descMatch = m[2].match(/description\s*:\s*['"`]([^'"`]*)['"`]/)
    commands.push({ name, description: descMatch?.[1] })
  }
  if (commands.length === 0) {
    const fallbackRe = /\bion\.registerCommand\(\s*['"`]([a-zA-Z_][a-zA-Z0-9_-]*)['"`]/g
    for (let m; (m = fallbackRe.exec(source)); ) commands.push({ name: m[1] })
  }

  // Imports: every `from '<module>'` clause.
  const imports: string[] = []
  const importRe = /\bfrom\s+['"`]([^'"`]+)['"`]/g
  for (let m; (m = importRe.exec(source)); ) imports.push(m[1])

  // Externals from manifest (engine bundler leaves these alone at build).
  const externals: string[] = Array.isArray(manifest?.external) ? (manifest!.external as string[]) : []

  return {
    path: params.path,
    entry,
    manifest,
    hooks,
    tools,
    commands,
    imports,
    externals,
    bytes: source.length,
    notes,
  }
}

export const inspectExtensionTool: ToolDef = {
  name: 'ion_inspect_extension',
  description:
    'Parse an extension and report registered hooks, tools, commands, imports, and manifest fields. Pass a directory or the entry-point file path.',
  parameters: {
    type: 'object',
    properties: {
      path: {
        type: 'string',
        description: 'Absolute path to the extension directory or its entry-point file.',
      },
    },
    required: ['path'],
  },
  execute: async (params: InspectParams) => ({
    content: JSON.stringify(inspectExtension(params), null, 2),
  }),
}
