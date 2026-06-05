// ion_list_hooks tool -- enriched hook reference.
//
// Phase B step 5 of the ion-meta upgrade: original listed only names. This
// version reads the bundled `docs/canonical/hooks/reference.md` (installed
// alongside the extension) and joins each hook with its payload type,
// return shape, and one-line use case extracted from the doc.
//
// When the canonical docs are missing (e.g. ion-meta loaded from a fresh
// checkout before `make engine`), falls back to name + category only,
// which mirrors the original behavior.

import { existsSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import type { ToolDef } from '../../sdk/ion-sdk'
import { getHookCatalog } from '../catalog'

interface ListHooksParams {
  /** Filter by category. Returns the full catalog when omitted. */
  category?: string
  /** Filter by exact hook name. Mutually exclusive with `category`; takes precedence. */
  name?: string
}

interface HookEntry {
  name: string
  category: string
  payload?: string
  returns?: string
  useCase?: string
}

/**
 * Resolve the bundled canonical hook reference at runtime. Layout:
 *   ~/.ion/extensions/ion-meta/docs/canonical/hooks/reference.md
 * Falls back through the `__dirname` ladder so tests running out of the
 * repo find `engine/extensions/ion-meta/docs/canonical/...` too.
 */
function findHookReference(): string | null {
  const candidates: string[] = []
  const here = typeof __dirname === 'string' ? __dirname : process.cwd()
  // From bundled .ion-build/, walk up to ext root and into docs/canonical.
  candidates.push(join(here, '..', 'docs', 'canonical', 'hooks', 'reference.md'))
  candidates.push(join(here, '..', '..', 'docs', 'canonical', 'hooks', 'reference.md'))
  candidates.push(join(here, 'docs', 'canonical', 'hooks', 'reference.md'))
  // Repo layout fallback: engine/extensions/ion-meta is two levels under engine/, canonical docs live at <repo>/docs.
  let cur = here
  for (let i = 0; i < 5; i++) {
    candidates.push(join(cur, 'docs', 'hooks', 'reference.md'))
    cur = dirname(cur)
  }
  for (const c of candidates) {
    if (existsSync(c)) return c
  }
  return null
}

interface ParsedHookDoc {
  payload?: string
  returns?: string
  useCase?: string
}

/**
 * Parse `docs/hooks/reference.md` for per-hook metadata. The reference
 * file structures each hook as `## <hook_name>` followed by labelled
 * lines or bullet sections (`Payload: ...`, `Returns: ...`, etc.). We
 * pull the first paragraph after the header as the use case.
 *
 * Cached on first call -- the doc never changes within one process.
 */
const parsedDocCache = new Map<string, ParsedHookDoc>()
let docParsed = false

function parseHookDoc(docPath: string): void {
  if (docParsed) return
  docParsed = true
  let source: string
  try {
    source = readFileSync(docPath, 'utf8')
  } catch {
    return
  }
  const blocks = source.split(/\n(?=##\s+)/)
  for (const block of blocks) {
    const headerMatch = block.match(/^##\s+(`?)([a-z_][a-z0-9_]*)\1/)
    if (!headerMatch) continue
    const name = headerMatch[2]
    const body = block.slice(block.indexOf('\n') + 1)
    const entry: ParsedHookDoc = {}
    // Look for `**Payload:**` / `Payload:` / `- Payload:` lines.
    const payload = body.match(/(?:\*\*Payload\*\*|Payload):\s*([^\n]+)/)
    if (payload) entry.payload = payload[1].trim()
    const returns = body.match(/(?:\*\*Returns?\*\*|Returns?):\s*([^\n]+)/)
    if (returns) entry.returns = returns[1].trim()
    // Use case: first non-empty, non-table, non-list line that is not a
    // label line.
    for (const line of body.split('\n').map(l => l.trim())) {
      if (!line) continue
      if (line.startsWith('|') || line.startsWith('-') || line.startsWith('*')) continue
      if (/^(\*\*)?(payload|returns?|fires when|when fired)/i.test(line)) continue
      if (line.startsWith('```')) break
      entry.useCase = line
      break
    }
    parsedDocCache.set(name, entry)
  }
}

function categoryOf(name: string): string {
  const catalog = getHookCatalog()
  for (const [cat, names] of Object.entries(catalog)) {
    if (names.includes(name)) return cat
  }
  return 'unknown'
}

function entryFor(name: string): HookEntry {
  const docPath = findHookReference()
  if (docPath) parseHookDoc(docPath)
  const meta = parsedDocCache.get(name) ?? {}
  return {
    name,
    category: categoryOf(name),
    payload: meta.payload,
    returns: meta.returns,
    useCase: meta.useCase,
  }
}

export const listHooksTool: ToolDef = {
  name: 'ion_list_hooks',
  description:
    'List Ion Engine hooks. Returns all hooks grouped by category by default; pass `category` to filter to one category, or `name` to look up one hook with payload/return/use-case metadata.',
  parameters: {
    type: 'object',
    properties: {
      category: {
        type: 'string',
        description:
          'Optional category filter. Valid values: lifecycle, session, pre-action, content, per-tool-call, per-tool-result, context-discovery, permission, file, task, elicitation, context-injection, capability, extension-lifecycle, plan-mode, system-inject, early-stop.',
      },
      name: {
        type: 'string',
        description: 'Optional exact hook name. Returns the enriched entry for that single hook.',
      },
    },
  },
  execute: async (params: ListHooksParams) => {
    const catalog = getHookCatalog()

    if (params.name) {
      const all = Object.values(catalog).flat()
      if (!all.includes(params.name)) {
        return {
          content: JSON.stringify(
            { error: `Unknown hook: ${params.name}`, known: all },
            null,
            2,
          ),
          isError: true,
        }
      }
      return { content: JSON.stringify(entryFor(params.name), null, 2) }
    }

    if (params.category) {
      const key = params.category.toLowerCase()
      if (!catalog[key]) {
        return {
          content: JSON.stringify(
            { error: `Unknown category: ${params.category}`, valid: Object.keys(catalog) },
            null,
            2,
          ),
          isError: true,
        }
      }
      return {
        content: JSON.stringify(
          {
            category: key,
            hooks: catalog[key].map(entryFor),
          },
          null,
          2,
        ),
      }
    }

    const total = Object.values(catalog).reduce((acc, v) => acc + v.length, 0)
    return {
      content: JSON.stringify(
        {
          total,
          categories: Object.fromEntries(
            Object.entries(catalog).map(([cat, names]) => [cat, names.map(entryFor)]),
          ),
        },
        null,
        2,
      ),
    }
  },
}
