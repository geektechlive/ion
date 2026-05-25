// ion_read_doc tool -- read a canonical doc bundled with ion-meta.
//
// At install time the engine's install.command copies the repo's
// `docs/extensions/`, `docs/hooks/`, `docs/agents/`, and
// `docs/architecture/adr/` into `~/.ion/extensions/ion-meta/docs/canonical/`.
// This tool reads from that bundled tree, behind an allow-list that
// rejects path traversal and access outside the four namespaces.
//
// When the canonical tree is missing (e.g. ion-meta running out of the
// repo before `make engine`), the tool falls back to reading from the
// repo's `<repo>/docs/` resolved relative to the extension's bundle
// path. This keeps the tool useful for in-repo testing without forcing
// developers to install before running.

import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, isAbsolute, join, normalize, relative } from 'node:path'
import type { ToolDef } from '../../sdk/ion-sdk'

interface ReadDocParams {
  /** Relative path inside the canonical doc tree (e.g. `extensions/anatomy.md`). */
  path?: string
  /** When true, returns the tree of available docs instead of reading a file. */
  list?: boolean
}

const NAMESPACES = ['extensions', 'hooks', 'agents', 'architecture'] as const

function findDocRoots(): string[] {
  const roots: string[] = []
  const here = typeof __dirname === 'string' ? __dirname : process.cwd()

  // Primary: install layout. ext-root/docs/canonical/{ns}/...
  // From bundled .ion-build/ → ext-root is `..`. From source layout →
  // ext-root is `here` (when running via tsx on the .ts files directly).
  const candidates = [
    join(here, '..', 'docs', 'canonical'),
    join(here, '..', '..', 'docs', 'canonical'),
    join(here, 'docs', 'canonical'),
  ]
  for (const c of candidates) {
    if (existsSync(c)) roots.push(c)
  }

  // Repo fallback: walk up looking for a docs/ dir that contains both
  // `extensions/anatomy.md` and `hooks/reference.md`. Capped at six
  // levels.
  let cur = here
  for (let i = 0; i < 6; i++) {
    const repoDocs = join(cur, 'docs')
    if (
      existsSync(join(repoDocs, 'extensions', 'anatomy.md')) &&
      existsSync(join(repoDocs, 'hooks', 'reference.md'))
    ) {
      roots.push(repoDocs)
    }
    cur = dirname(cur)
  }

  // De-dupe while preserving order.
  return Array.from(new Set(roots))
}

function listAll(root: string): string[] {
  const out: string[] = []
  const walk = (dir: string, prefix: string) => {
    let entries
    try { entries = readdirSync(dir) } catch { return }
    for (const e of entries) {
      const full = join(dir, e)
      let s
      try { s = statSync(full) } catch { continue }
      if (s.isDirectory()) walk(full, join(prefix, e))
      else if (e.endsWith('.md')) out.push(join(prefix, e))
    }
  }
  for (const ns of NAMESPACES) {
    const nsDir = join(root, ns)
    if (existsSync(nsDir)) walk(nsDir, ns)
  }
  return out.sort()
}

export function readDoc(params: ReadDocParams): { content: string; isError?: boolean } {
  const roots = findDocRoots()
  if (roots.length === 0) {
    return {
      content: 'No canonical docs found. Run `make engine` from the repo root, or load ion-meta from a fresh checkout where docs/ is present.',
      isError: true,
    }
  }

  if (params.list || !params.path) {
    const all = listAll(roots[0])
    return {
      content: JSON.stringify({ root: roots[0], docs: all }, null, 2),
    }
  }

  // Sanitise: reject absolute paths and `..` traversal.
  const requested = normalize(params.path)
  if (isAbsolute(requested) || requested.split('/').includes('..')) {
    return { content: `path must be a relative path under one of: ${NAMESPACES.join(', ')}`, isError: true }
  }

  // Enforce namespace allow-list.
  const firstSegment = requested.split('/')[0]
  if (!NAMESPACES.includes(firstSegment as typeof NAMESPACES[number])) {
    return {
      content: `path must start with one of: ${NAMESPACES.join(', ')}/`,
      isError: true,
    }
  }

  // Try each root in order.
  for (const root of roots) {
    const abs = join(root, requested)
    // Belt-and-braces: ensure resolved path stays within root after symlink-free normalisation.
    const rel = relative(root, abs)
    if (rel.startsWith('..') || isAbsolute(rel)) continue
    if (!existsSync(abs)) continue
    try {
      const body = readFileSync(abs, 'utf8')
      return {
        content: JSON.stringify(
          { path: requested, root, bytes: body.length, body },
          null,
          2,
        ),
      }
    } catch (err) {
      return { content: `read failed: ${(err as Error).message}`, isError: true }
    }
  }

  return { content: `doc not found: ${requested}`, isError: true }
}

export const readDocTool: ToolDef = {
  name: 'ion_read_doc',
  description:
    'Read a canonical Ion doc bundled with the extension. Pass `path` like `extensions/anatomy.md`, `hooks/reference.md`, `agents/definition-format.md`, or `architecture/adr/001-engine-vs-harness.md`. Pass `list: true` for the directory listing.',
  parameters: {
    type: 'object',
    properties: {
      path: {
        type: 'string',
        description: 'Relative path under one of: extensions/, hooks/, agents/, architecture/.',
      },
      list: {
        type: 'boolean',
        description: 'When true, return the list of available doc files instead of reading one.',
      },
    },
  },
  execute: async (params: ReadDocParams) => readDoc(params),
}
