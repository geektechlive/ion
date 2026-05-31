// ion_validate_agent tool -- validate an agent .md file.
//
// Phase B step 4 of the ion-meta upgrade: original implementation only
// checked `name` and `description`. This one parses the YAML frontmatter,
// validates field shapes (tools must be a YAML array, model must be a
// known model id pattern, parent must reference an existing peer when
// `peers` is supplied), and verifies a non-empty body.

import { existsSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import type { ToolDef } from '../../sdk/ion-sdk'

interface ValidateParams {
  /** Full markdown content of the agent file. */
  content: string
  /** Optional absolute path to the agent .md. When supplied, the validator
   *  walks the sibling directory and uses it as the peer set for the
   *  `parent` reference check. */
  filePath?: string
  /** Explicit peer override (filenames stems). Used by tests and by the
   *  inspect-extension tool to feed in a pre-discovered peer set. */
  peers?: string[]
}

interface ValidationReport {
  valid: boolean
  errors: string[]
  warnings: string[]
  fields: Record<string, string | string[]>
  body: { lines: number; chars: number }
}

const KNOWN_MODEL_PREFIXES = ['claude-', 'gpt-', 'o1-', 'gemini-', 'llama-', 'qwen-']

export function validateAgent(params: ValidateParams): ValidationReport {
  const errors: string[] = []
  const warnings: string[] = []
  const fields: Record<string, string | string[]> = {}

  const fmMatch = params.content.match(/^---\s*\n([\s\S]*?)\n---\s*(?:\n|$)/)
  if (!fmMatch) {
    return {
      valid: false,
      errors: ['No frontmatter found (expected `---` fence, frontmatter body, then `---` fence on its own line)'],
      warnings: [],
      fields: {},
      body: { lines: 0, chars: 0 },
    }
  }

  const fmBody = fmMatch[1]
  const afterFm = params.content.slice(fmMatch[0].length)

  // Parse the YAML frontmatter line-by-line. We don't depend on a full
  // YAML parser to keep the bundle small; the grammar agents use is
  // shallow (key: value or key: [a, b, c]).
  for (const rawLine of fmBody.split('\n')) {
    const line = rawLine.replace(/\r$/, '')
    if (!line.trim() || line.trim().startsWith('#')) continue
    const colonIdx = line.indexOf(':')
    if (colonIdx === -1) {
      warnings.push(`Unparseable frontmatter line (no colon): ${JSON.stringify(line)}`)
      continue
    }
    const key = line.slice(0, colonIdx).trim()
    const val = line.slice(colonIdx + 1).trim()

    if (key === 'tools') {
      // Accept `[Read, Write]` or `[Read,Write]` -- a YAML inline array.
      if (!val.startsWith('[') || !val.endsWith(']')) {
        errors.push(`tools must be a YAML inline array (e.g. \`tools: [Read, Write]\`); got ${JSON.stringify(val)}`)
        fields['tools'] = val
      } else {
        const inner = val.slice(1, -1).trim()
        const arr = inner === '' ? [] : inner.split(',').map(s => s.trim()).filter(Boolean)
        fields['tools'] = arr
      }
    } else {
      fields[key] = val
    }
  }

  if (!fields['name']) errors.push('Missing required field: name')
  if (!fields['description']) errors.push('Missing required field: description')
  if (!fields['model']) warnings.push('No model specified (will inherit session default)')

  // Model sanity check.
  if (fields['model']) {
    const model = String(fields['model'])
    if (!KNOWN_MODEL_PREFIXES.some(p => model.startsWith(p))) {
      warnings.push(`Model id has an unfamiliar prefix: ${JSON.stringify(model)}. Known prefixes: ${KNOWN_MODEL_PREFIXES.join(', ')}`)
    }
  }

  if (!fields['tools']) warnings.push('No tools specified (agent will have no tool access)')

  // Parent reference: when peers are available, ensure `parent` points
  // to an existing peer. We treat omission of `parent` as "this is a
  // root agent" (silent), and the orchestrator/root must be in the peer
  // set when peers are supplied.
  const peers = derivePeers(params)
  if (peers !== null) {
    if (fields['parent']) {
      const parent = String(fields['parent'])
      if (!peers.includes(parent)) {
        errors.push(`parent: ${JSON.stringify(parent)} does not match any sibling agent. Known peers: ${peers.join(', ')}`)
      }
    }
  }

  // Body: agents need a system prompt. Empty body == agent has nothing
  // to say; warn rather than error so the validator can be used during
  // scaffolding (the freshly-written file has a TODO body).
  const bodyLines = afterFm.split('\n').length
  const bodyChars = afterFm.trim().length
  if (bodyChars === 0) {
    warnings.push('Agent body is empty. The agent will have no system prompt.')
  } else if (afterFm.trim().startsWith('TODO')) {
    warnings.push('Agent body starts with TODO — looks like an unfilled scaffold.')
  }

  return {
    valid: errors.length === 0,
    errors,
    warnings,
    fields,
    body: { lines: bodyLines, chars: bodyChars },
  }
}

function derivePeers(params: ValidateParams): string[] | null {
  if (params.peers) return params.peers
  if (!params.filePath) return null
  const dir = dirname(params.filePath)
  if (!existsSync(dir)) return null
  try {
    return readdirSync(dir)
      .filter(f => f.endsWith('.md'))
      .map(f => f.replace(/\.md$/, ''))
  } catch {
    return null
  }
}

export const validateAgentTool: ToolDef = {
  name: 'ion_validate_agent',
  description:
    'Validate an agent .md file. Checks frontmatter shape, required fields (name, description), tools array shape, model id prefix, and (when filePath is supplied) parent-reference integrity against sibling agents.',
  parameters: {
    type: 'object',
    properties: {
      content: { type: 'string', description: 'Full markdown content of the agent file.' },
      filePath: {
        type: 'string',
        description: 'Absolute path of the agent .md, used to enumerate sibling agents for parent-reference validation. Optional.',
      },
      peers: {
        type: 'array',
        items: { type: 'string' },
        description: 'Explicit peer set override (filename stems). When supplied, replaces the sibling-directory walk.',
      },
    },
    required: ['content'],
  },
  execute: async (params: ValidateParams) => ({
    content: JSON.stringify(validateAgent(params), null, 2),
  }),
}
