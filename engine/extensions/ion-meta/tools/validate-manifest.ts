// ion_validate_manifest tool -- schema-check extension.json.
//
// The engine accepts a very small surface: `name` (required string),
// `external` (optional string[] of native deps to leave external to the
// bundler), and `engineVersion` (optional version constraint). Unknown
// top-level keys cause the engine to reject the manifest at load time,
// so flagging them up front saves the author a debugging round-trip.

import type { ToolDef } from '../../sdk/ion-sdk'

interface ValidateManifestParams {
  /** Raw JSON text of the manifest. */
  content: string
}

interface ManifestReport {
  valid: boolean
  errors: string[]
  warnings: string[]
  parsed: Record<string, unknown> | null
}

const KNOWN_KEYS = new Set(['name', 'external', 'engineVersion'])

export function validateManifest(params: ValidateManifestParams): ManifestReport {
  const errors: string[] = []
  const warnings: string[] = []
  let parsed: Record<string, unknown> | null = null

  try {
    parsed = JSON.parse(params.content)
  } catch (err) {
    return {
      valid: false,
      errors: [`Invalid JSON: ${(err as Error).message}`],
      warnings,
      parsed: null,
    }
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return { valid: false, errors: ['Top-level must be an object'], warnings, parsed: null }
  }

  // Required: name.
  if (typeof parsed['name'] !== 'string' || !(parsed['name'] as string).trim()) {
    errors.push('`name` is required and must be a non-empty string')
  }

  // external: string[].
  if ('external' in parsed) {
    const ext = parsed['external']
    if (!Array.isArray(ext) || ext.some(v => typeof v !== 'string')) {
      errors.push('`external` must be an array of strings (native deps to skip during bundle)')
    } else if (ext.length === 0) {
      warnings.push('`external` is present but empty; you can omit it')
    }
  }

  // engineVersion: string semver-ish.
  if ('engineVersion' in parsed) {
    const ev = parsed['engineVersion']
    if (typeof ev !== 'string') {
      errors.push('`engineVersion` must be a string (a semver constraint or `*`)')
    }
  }

  // Unknown keys: hard error -- the engine rejects them too.
  for (const k of Object.keys(parsed)) {
    if (!KNOWN_KEYS.has(k)) {
      errors.push(`Unknown top-level key: ${JSON.stringify(k)}. Known keys: ${Array.from(KNOWN_KEYS).join(', ')}`)
    }
  }

  return { valid: errors.length === 0, errors, warnings, parsed }
}

export const validateManifestTool: ToolDef = {
  name: 'ion_validate_manifest',
  description:
    'Schema-check the JSON body of an `extension.json` manifest. Reports unknown keys (which the engine rejects), missing `name`, and ill-typed `external` / `engineVersion`.',
  parameters: {
    type: 'object',
    properties: {
      content: {
        type: 'string',
        description: 'Raw JSON content of `extension.json`.',
      },
    },
    required: ['content'],
  },
  execute: async (params: ValidateManifestParams) => ({
    content: JSON.stringify(validateManifest(params), null, 2),
  }),
}
