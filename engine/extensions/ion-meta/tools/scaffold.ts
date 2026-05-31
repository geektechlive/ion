// ion_scaffold tool -- writes extension/agent/skill files to disk.
//
// Phase B step 3 of the ion-meta upgrade: the original `ion_scaffold`
// returned a description object the LLM had to render. This version
// actually writes the files when given a `targetDir`, and falls back to
// returning the template strings when `targetDir` is omitted (useful for
// preview / dry-run flows).

import { existsSync, mkdirSync, writeFileSync } from 'node:fs'
import { dirname, isAbsolute, join } from 'node:path'
import type { ToolDef } from '../../sdk/ion-sdk'

interface ScaffoldParams {
  /** Target directory absolute path. When omitted, files are returned as
   *  template strings instead of written. */
  targetDir?: string
  /** Name of the extension/agent/skill. Becomes the directory or filename. */
  name: string
  /** What to scaffold. */
  type: 'extension' | 'agent' | 'skill'
}

interface FileTemplate {
  path: string   // relative to the scaffold root
  body: string
}

/** Build the extension template set. Includes index.ts, extension.json,
 *  package.json, README.md, .gitignore, agents/orchestrator.md. */
function extensionTemplates(name: string): FileTemplate[] {
  return [
    {
      path: 'index.ts',
      body: [
        `// ${name} -- TODO: describe the extension's purpose.`,
        '',
        "import { createIon, log } from '../sdk/ion-sdk'",
        '',
        'const ion = createIon()',
        '',
        "ion.on('session_start', () => {",
        `  log.info('${name} loaded')`,
        '})',
        '',
        `ion.registerTool({`,
        `  name: '${name.replace(/-/g, '_')}_hello',`,
        `  description: 'Demo tool that returns a greeting.',`,
        `  parameters: { type: 'object', properties: {} },`,
        `  execute: async () => ({ content: 'hello from ${name}' }),`,
        `})`,
        '',
      ].join('\n'),
    },
    {
      path: 'extension.json',
      body: JSON.stringify(
        {
          name,
          engineVersion: '*',
        },
        null,
        2,
      ) + '\n',
    },
    {
      path: 'package.json',
      body: JSON.stringify(
        {
          name,
          version: '0.0.1',
          private: true,
          description: `Ion extension: ${name}`,
        },
        null,
        2,
      ) + '\n',
    },
    {
      path: 'README.md',
      body: [
        `# ${name}`,
        '',
        'TODO: describe the extension. Load via:',
        '',
        '```bash',
        `ion prompt --extension ~/.ion/extensions/${name}/index.ts "hello"`,
        '```',
        '',
      ].join('\n'),
    },
    {
      path: '.gitignore',
      body: ['.ion-build/', 'node_modules/', ''].join('\n'),
    },
    {
      path: 'agents/orchestrator.md',
      body: agentBody('orchestrator', `${name} orchestrator`),
    },
  ]
}

function agentTemplates(name: string): FileTemplate[] {
  return [{ path: `${name}.md`, body: agentBody(name, `Agent: ${name}`) }]
}

function skillTemplates(name: string): FileTemplate[] {
  return [
    {
      path: `${name}.md`,
      body: [
        '---',
        `name: ${name}`,
        'description: <one-line description of when to invoke this skill>',
        '---',
        '',
        '# Skill body',
        '',
        'TODO: write the skill prompt. Reference `docs/skills/` (if it exists) for skill conventions.',
        '',
      ].join('\n'),
    },
  ]
}

function agentBody(name: string, description: string): string {
  return [
    '---',
    `name: ${name}`,
    `description: ${description}`,
    'model: claude-sonnet-4-6',
    'tools: [Read, Write, Grep, Glob]',
    '---',
    '',
    `You are ${name}. TODO: replace this body with the agent's persona and routing instructions.`,
    '',
  ].join('\n')
}

function templatesFor(params: ScaffoldParams): FileTemplate[] {
  switch (params.type) {
    case 'extension': return extensionTemplates(params.name)
    case 'agent': return agentTemplates(params.name)
    case 'skill': return skillTemplates(params.name)
  }
}

interface WriteResult {
  written: string[]
  skipped: string[]
}

function writeAll(rootDir: string, templates: FileTemplate[]): WriteResult {
  const written: string[] = []
  const skipped: string[] = []
  for (const t of templates) {
    const dest = join(rootDir, t.path)
    if (existsSync(dest)) {
      skipped.push(dest)
      continue
    }
    mkdirSync(dirname(dest), { recursive: true })
    writeFileSync(dest, t.body, 'utf8')
    written.push(dest)
  }
  return { written, skipped }
}

export const scaffoldTool: ToolDef = {
  name: 'ion_scaffold',
  description:
    'Scaffold an Ion extension, agent, or skill. With `targetDir`, writes files to disk; without, returns the template strings for preview.',
  parameters: {
    type: 'object',
    properties: {
      name: {
        type: 'string',
        description: 'Name of the extension, agent, or skill. Used as directory/filename and embedded in the templates.',
      },
      type: {
        type: 'string',
        enum: ['extension', 'agent', 'skill'],
        description: 'What to scaffold.',
      },
      targetDir: {
        type: 'string',
        description:
          'Absolute path to the parent directory. For type=extension, files go under <targetDir>/<name>/. For agent/skill, files go directly under <targetDir>/. Omit for preview-only mode.',
      },
    },
    required: ['name', 'type'],
  },
  execute: async (params: ScaffoldParams) => {
    const templates = templatesFor(params)

    if (!params.targetDir) {
      // Preview mode: return the templates inline so the LLM can show
      // them to the user before they commit to a path.
      return {
        content: JSON.stringify(
          {
            mode: 'preview',
            type: params.type,
            name: params.name,
            files: templates.map(t => ({ path: t.path, bytes: t.body.length })),
            templates: Object.fromEntries(templates.map(t => [t.path, t.body])),
          },
          null,
          2,
        ),
      }
    }

    if (!isAbsolute(params.targetDir)) {
      return {
        content: `targetDir must be an absolute path, got: ${params.targetDir}`,
        isError: true,
      }
    }

    // For extension scaffolds, nest under the name; for agent/skill we
    // assume the caller already chose the parent directory.
    const root = params.type === 'extension'
      ? join(params.targetDir, params.name)
      : params.targetDir

    mkdirSync(root, { recursive: true })
    const result = writeAll(root, templates)

    return {
      content: JSON.stringify(
        {
          mode: 'wrote',
          type: params.type,
          name: params.name,
          root,
          written: result.written,
          skipped: result.skipped,
          note: result.skipped.length
            ? 'Some files already existed and were not overwritten. Delete them first or scaffold to a fresh directory.'
            : undefined,
        },
        null,
        2,
      ),
    }
  },
}
