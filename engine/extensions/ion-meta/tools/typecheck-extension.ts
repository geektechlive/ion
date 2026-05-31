// ion_typecheck_extension tool -- run esbuild against an extension.
//
// Runs `esbuild --bundle --analyze --metafile=- --outfile=/dev/null` (or
// the Windows equivalent) over the extension's entry point. Bundles dry
// to discard output but surface the same parse/type/import errors the
// engine would hit at load time.
//
// Limitations:
//   - Pure type errors (TS-level, not parse) are NOT caught by esbuild;
//     it transpiles but does not typecheck. For full TS coverage the
//     author still needs `tsc --noEmit` separately.
//   - esbuild must be on PATH. We check upfront and degrade gracefully.

import { execFile } from 'node:child_process'
import { existsSync, mkdtempSync, statSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { promisify } from 'node:util'
import type { ToolDef } from '../../sdk/ion-sdk'

const execFileP = promisify(execFile)

interface TypecheckParams {
  /** Path to the extension directory or entry-point file. */
  path: string
}

interface TypecheckReport {
  path: string
  entry: string | null
  esbuildAvailable: boolean
  ok: boolean
  errors: string[]
  warnings: string[]
  durationMs: number
}

const ENTRY_CANDIDATES = ['index.ts', 'extension.ts', 'index.js', 'main.ts', 'main.js'] as const

function resolveEntry(path: string): string | null {
  let s
  try { s = statSync(path) } catch { return null }
  if (s.isFile()) return path
  for (const c of ENTRY_CANDIDATES) {
    const p = join(path, c)
    if (existsSync(p)) return p
  }
  return null
}

async function checkEsbuild(): Promise<boolean> {
  try {
    await execFileP('esbuild', ['--version'], { timeout: 3000 })
    return true
  } catch {
    return false
  }
}

export async function typecheckExtension(params: TypecheckParams): Promise<TypecheckReport> {
  const started = Date.now()
  const entry = resolveEntry(params.path)
  const report: TypecheckReport = {
    path: params.path,
    entry,
    esbuildAvailable: false,
    ok: false,
    errors: [],
    warnings: [],
    durationMs: 0,
  }
  if (!entry) {
    report.errors.push('No entry-point file found. Looked for: ' + ENTRY_CANDIDATES.join(', '))
    report.durationMs = Date.now() - started
    return report
  }

  report.esbuildAvailable = await checkEsbuild()
  if (!report.esbuildAvailable) {
    report.errors.push('esbuild not on PATH. Install with `npm i -g esbuild`.')
    report.durationMs = Date.now() - started
    return report
  }

  // Bundle to a discarded outfile under a tempdir. We pin platform=node
  // and format=esm to mirror what the engine does at load time.
  const outDir = mkdtempSync(join(tmpdir(), 'ion-typecheck-'))
  const outFile = join(outDir, 'bundle.mjs')

  // Resolve `../sdk/ion-sdk` -- the SDK lives at the sibling extensions/sdk/
  // directory. Find it the same way the runtime does: walk up two levels
  // from the entry's directory looking for `sdk/ion-sdk/index.ts`.
  const sdkDir = findSdkDir(dirname(entry))
  const aliasArgs: string[] = []
  if (sdkDir) {
    aliasArgs.push(`--alias:../sdk/ion-sdk=${join(sdkDir, 'index.ts')}`)
  }

  try {
    const { stderr } = await execFileP(
      'esbuild',
      [
        entry,
        '--bundle',
        '--platform=node',
        '--target=node20',
        '--format=esm',
        `--outfile=${outFile}`,
        '--log-level=warning',
        ...aliasArgs,
      ],
      { timeout: 30_000, maxBuffer: 1024 * 1024 * 10 },
    )
    report.ok = true
    if (stderr) {
      report.warnings = stderr.split('\n').filter(Boolean).slice(0, 10)
    }
  } catch (err) {
    const e = err as { stderr?: string; message?: string }
    const text = e.stderr || e.message || String(err)
    report.errors = text.split('\n').filter(Boolean).slice(0, 20)
  }

  report.durationMs = Date.now() - started
  return report
}

function findSdkDir(start: string): string | null {
  let cur = start
  for (let i = 0; i < 5; i++) {
    const candidate = join(cur, '..', 'sdk', 'ion-sdk')
    if (existsSync(join(candidate, 'index.ts'))) return candidate
    cur = dirname(cur)
  }
  return null
}

export const typecheckExtensionTool: ToolDef = {
  name: 'ion_typecheck_extension',
  description:
    'Run esbuild against an extension to surface parse and import errors before loading it. Returns the first ~20 error lines. Requires esbuild on PATH.',
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
  execute: async (params: TypecheckParams) => ({
    content: JSON.stringify(await typecheckExtension(params), null, 2),
  }),
}
