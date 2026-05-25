// ion-meta catalog -- SDK introspection.
//
// Parses the bundled SDK's `types.ts` at load time to derive ground truth
// for the hook catalog and IonContext method list. This replaces the
// hand-maintained constants in earlier versions of ion-meta, which drifted
// every time the SDK gained a hook.
//
// Why parse instead of import the types module directly? `HookPayloadMap`
// is a TypeScript interface; it has no runtime presence and esbuild strips
// the declaration during bundling. The SDK ships its source as part of the
// install (`~/.ion/extensions/sdk/ion-sdk/types.ts`), so we can read the
// file and regex for the field names. The same file is also the canonical
// source of truth for the engine's contract test, so any drift here means
// the SDK install is corrupt, not that ion-meta is stale.
//
// All exported parsers fall back to a hard-coded snapshot when the SDK file
// is missing (e.g. during integration tests that load ion-meta out of the
// repo without running `make engine` first). The snapshot is the contract
// at the time this module was written; if a hook is added, the SDK file
// will surface it. If a hook is *removed*, the snapshot still lists it --
// which is the safer drift direction (over-report, never under-report).

import { existsSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { log } from '../sdk/ion-sdk'

// ─── Hook catalog ─────────────────────────────────────────────────────────

/**
 * Categories mirror the comment headers in `HookPayloadMap`. Order is
 * stable so the persona's bullet list reads predictably.
 */
const HOOK_CATEGORY_ORDER: readonly string[] = [
  'lifecycle',
  'session',
  'pre-action',
  'content',
  'per-tool-call',
  'per-tool-result',
  'context-discovery',
  'permission',
  'file',
  'task',
  'elicitation',
  'context-injection',
  'capability',
  'extension-lifecycle',
  'plan-mode',
  'system-inject',
  'early-stop',
] as const

// Maps the canonical comment-line tags inside `HookPayloadMap` to category
// keys. The SDK author writes "// Lifecycle (13)" before the lifecycle
// hooks, "// Pre-action (2)" before pre-action hooks, etc.; we lowercase
// the first word(s) and use them as the discriminator.
const COMMENT_TO_CATEGORY: Record<string, string> = {
  lifecycle: 'lifecycle',
  session: 'session',
  'pre-action': 'pre-action',
  content: 'content',
  'per-tool': 'per-tool-call', // disambiguated below by suffix
  context: 'context-discovery',
  permission: 'permission',
  file: 'file',
  task: 'task',
  elicitation: 'elicitation',
  'context inject': 'context-injection',
  capability: 'capability',
  'extension lifecycle': 'extension-lifecycle',
  'plan mode': 'plan-mode',
  'system inject': 'system-inject',
  'early-stop': 'early-stop',
}

// Snapshot used as a fallback when the SDK source cannot be read. Mirrors
// the contract at write time. Any drift is detected by the runtime parse
// path first; this just keeps ion-meta useful when running in a partially
// installed environment.
const HOOK_CATALOG_SNAPSHOT: Record<string, string[]> = {
  lifecycle: [
    'session_start', 'session_end', 'before_prompt', 'turn_start', 'turn_end',
    'message_start', 'message_end', 'tool_start', 'tool_end', 'tool_call',
    'on_error', 'agent_start', 'agent_end',
  ],
  session: [
    'session_before_compact', 'session_compact', 'session_before_fork',
    'session_fork', 'session_before_switch',
  ],
  'pre-action': ['before_agent_start', 'before_provider_request'],
  content: [
    'context', 'message_update', 'tool_result', 'input', 'model_select',
    'user_bash', 'plan_mode_prompt',
  ],
  'per-tool-call': [
    'bash_tool_call', 'read_tool_call', 'write_tool_call', 'edit_tool_call',
    'grep_tool_call', 'glob_tool_call', 'agent_tool_call',
  ],
  'per-tool-result': [
    'bash_tool_result', 'read_tool_result', 'write_tool_result', 'edit_tool_result',
    'grep_tool_result', 'glob_tool_result', 'agent_tool_result',
  ],
  'context-discovery': ['context_discover', 'context_load', 'instruction_load'],
  permission: ['permission_request', 'permission_denied', 'permission_classify'],
  file: ['file_changed', 'workspace_file_changed'],
  task: ['task_created', 'task_completed'],
  elicitation: ['elicitation_request', 'elicitation_result'],
  'context-injection': ['context_inject'],
  capability: ['capability_discover', 'capability_match', 'capability_invoke'],
  'extension-lifecycle': [
    'extension_respawned', 'turn_aborted',
    'peer_extension_died', 'peer_extension_respawned',
  ],
  'plan-mode': ['before_plan_mode_enter', 'before_plan_mode_exit'],
  'system-inject': ['system_inject'],
  'early-stop': ['before_early_stop_decision', 'early_stop_continued'],
}

let hookCatalogCache: Record<string, string[]> | null = null

/**
 * Resolves the absolute path of the SDK's `types.ts` from this module's
 * vantage point. The SDK install layout is:
 *   ~/.ion/extensions/sdk/ion-sdk/{index,runtime,types}.ts
 *   ~/.ion/extensions/ion-meta/{index,catalog,...}.ts
 *
 * At runtime ion-meta is bundled into a single .mjs under .ion-build/, so
 * `__dirname` resolves into the build dir. We walk up to the extension
 * root, then sideways to `../sdk/ion-sdk/types.ts`.
 *
 * Returns the path even when the file does not exist; callers must check
 * via `existsSync` before reading.
 */
function resolveSdkTypesPath(): string {
  // From `<ext-root>/.ion-build/<hash>.mjs`, `dirname(import.meta.url)` is
  // the build dir. The ion-meta entry points to `<ext-root>/index.ts`,
  // which esbuild bundles into `<ext-root>/.ion-build/`. Walk up two
  // levels to reach the `extensions/` directory.
  const here = typeof __dirname === 'string' ? __dirname : process.cwd()
  // `here` could be `.ion-build/` (when bundled) or `<ext-root>/` (when run
  // directly via tsx). Walk up until we find a directory containing a
  // sibling `sdk/ion-sdk/types.ts`, capped at four levels to avoid infinite
  // ascent on a malformed install.
  let cur = here
  for (let i = 0; i < 4; i++) {
    const candidate = join(cur, '..', 'sdk', 'ion-sdk', 'types.ts')
    if (existsSync(candidate)) return candidate
    cur = dirname(cur)
  }
  // Fall back to the install layout's documented location.
  return join(here, '..', '..', 'sdk', 'ion-sdk', 'types.ts')
}

/**
 * Returns the hook catalog grouped by category. Generated on first call
 * by parsing the SDK's `types.ts`; subsequent calls return the cached
 * result.
 *
 * On parse failure (file missing, regex finds zero hooks), returns the
 * baked-in snapshot and logs a warning. ion-meta keeps working; the
 * audit tooling will flag the drift.
 */
export function getHookCatalog(): Record<string, string[]> {
  if (hookCatalogCache !== null) return hookCatalogCache

  const path = resolveSdkTypesPath()
  if (!existsSync(path)) {
    log.warn('ion-meta: SDK types.ts not found; falling back to hook snapshot', {
      tried: path,
    })
    hookCatalogCache = HOOK_CATALOG_SNAPSHOT
    return hookCatalogCache
  }

  try {
    hookCatalogCache = parseHookCatalog(readFileSync(path, 'utf8'))
    log.info('ion-meta: hook catalog parsed from SDK', {
      categories: Object.keys(hookCatalogCache).length,
      hooks: Object.values(hookCatalogCache).reduce((acc, v) => acc + v.length, 0),
    })
  } catch (err) {
    log.warn('ion-meta: failed to parse SDK types.ts; using snapshot', {
      err: (err as Error).message,
    })
    hookCatalogCache = HOOK_CATALOG_SNAPSHOT
  }
  return hookCatalogCache
}

/**
 * Parser: extracts the `HookPayloadMap` block from a `types.ts` source
 * file and walks its lines, attributing each hook field to whichever
 * category comment precedes it.
 *
 * Comment shape (canonical): `  // <CategoryWord> (N)` or
 * `  // <CategoryWord> (N) -- description`. Both are common in the SDK.
 *
 * Hook field shape: `  <hook_name>: <PayloadType>` with optional whitespace.
 *
 * Exported for unit tests; should not be called directly by extension code.
 */
export function parseHookCatalog(source: string): Record<string, string[]> {
  const block = extractInterfaceBlock(source, 'HookPayloadMap')
  if (!block) {
    throw new Error('HookPayloadMap interface not found in source')
  }

  const out: Record<string, string[]> = {}
  // Initialise all categories so the persona reads consistently even when
  // a category has zero hooks (it shouldn't, but defensive).
  for (const c of HOOK_CATEGORY_ORDER) out[c] = []

  let currentCategory = 'lifecycle' // SDK lists lifecycle first; safe default.

  // Match either `// Category (N)` style headers OR a hook field line.
  const commentRe = /^\s*\/\/\s*([A-Za-z][\w\s-]*?)(?:\s*\(\d+\))?(?:\s*--.*)?\s*$/
  const fieldRe = /^\s*([a-z_][a-z0-9_]*)\s*:/

  for (const line of block.split('\n')) {
    const cm = line.match(commentRe)
    if (cm) {
      const word = cm[1].toLowerCase().trim()
      const cat = classifyComment(word)
      if (cat) currentCategory = cat
      continue
    }
    const fm = line.match(fieldRe)
    if (fm) {
      const name = fm[1]
      // Skip TypeScript reserved-ish tokens that could match the field
      // regex (none expected in HookPayloadMap, but cheap guard).
      if (name === 'export' || name === 'interface') continue
      ;(out[currentCategory] ?? (out[currentCategory] = [])).push(name)
    }
  }

  // Drop empty categories so callers don't render headers with zero hooks.
  // (Categories never become empty when parsing succeeds against the
  // current SDK, but if the SDK reorganises we surface the new shape
  // cleanly.)
  for (const k of Object.keys(out)) {
    if (out[k].length === 0) delete out[k]
  }

  if (Object.values(out).reduce((acc, v) => acc + v.length, 0) === 0) {
    throw new Error('HookPayloadMap block parsed but zero hooks found')
  }
  return out
}

function classifyComment(word: string): string | null {
  // Direct match.
  if (COMMENT_TO_CATEGORY[word]) return COMMENT_TO_CATEGORY[word]
  // Per-tool variants. Comments read "Per-tool call" and "Per-tool result".
  if (word.startsWith('per-tool call')) return 'per-tool-call'
  if (word.startsWith('per-tool result')) return 'per-tool-result'
  // Suffix-tolerant match (e.g. "Context (3)" → "context-discovery";
  // "Context inject (1)" → "context-injection").
  for (const [k, v] of Object.entries(COMMENT_TO_CATEGORY)) {
    if (word.startsWith(k)) return v
  }
  return null
}

// ─── IonContext method introspection ──────────────────────────────────────

/**
 * Method signature extracted from `IonContext`. The signature string is
 * collapsed onto one line and stripped of doc comments; useful for the
 * `ion_list_sdk_methods` tool output.
 */
export interface SDKMethod {
  name: string
  signature: string
  doc: string
}

const SDK_METHOD_SNAPSHOT: SDKMethod[] = [
  { name: 'emit', signature: 'emit(event: EngineEvent): void', doc: 'Emit a typed engine event (engine_agent_state, engine_status, engine_working_message, engine_notify, engine_harness_message) or a custom-typed event.' },
  { name: 'sendMessage', signature: 'sendMessage(text: string): void', doc: 'Push a harness-authored message into the active conversation stream.' },
  { name: 'callTool', signature: 'callTool(name: string, input: Record<string, unknown>): Promise<{ content: string; isError?: boolean }>', doc: 'Dispatch a registered tool without an LLM round trip. Subject to session permission policy. Does NOT fire per-tool hooks or permission_request.' },
  { name: 'sendPrompt', signature: 'sendPrompt(text: string, opts?: SendPromptOpts): Promise<void>', doc: 'Queue a fresh prompt on the session. Recursion hazard: calling from before_prompt re-triggers before_prompt.' },
  { name: 'dispatchAgent', signature: 'dispatchAgent(opts: DispatchAgentOpts): Promise<DispatchAgentResult>', doc: 'Spawn a child agent session by name. Waits for the child to complete and returns its final result.' },
  { name: 'discoverAgents', signature: 'discoverAgents(opts?: DiscoverAgentsOpts): Promise<DiscoveredAgent[]>', doc: 'Enumerate agents the engine knows about: extension-bundled, runtime-registered, and discovered from the filesystem.' },
  { name: 'registerAgentSpec', signature: 'registerAgentSpec(spec: AgentSpec): Promise<void>', doc: 'Register an LLM-visible agent spec at runtime. Pairs with capability_match for self-hire flows.' },
  { name: 'deregisterAgentSpec', signature: 'deregisterAgentSpec(name: string): Promise<void>', doc: 'Remove a previously-registered agent spec.' },
  { name: 'elicit', signature: 'elicit(opts: ElicitOptions): Promise<ElicitResult>', doc: 'Raise an elicitation request. Engine fans out engine_elicitation_request to every client for Accept/Edit/Reject UI.' },
  { name: 'sandboxWrap', signature: 'sandboxWrap(command: string, profile?: SandboxProfile): Promise<SandboxWrapResult>', doc: 'Wrap a shell command with platform-appropriate sandbox restrictions (sandbox-exec on macOS, bwrap on Linux).' },
  { name: 'getContextUsage', signature: 'getContextUsage(): Promise<ContextUsage | null>', doc: 'Snapshot the active run\'s context-window usage. Use to make proactive decisions before reactive compaction fires (>80%).' },
  { name: 'searchHistory', signature: 'searchHistory(query: string, maxResults?: number): Promise<HistoryMatch[]>', doc: 'Search the persisted conversation log -- including pre-compaction messages -- for substring matches.' },
  { name: 'suppressTool', signature: 'suppressTool(name: string): Promise<void>', doc: 'Hide a registered tool (built-in or MCP-provided) from the LLM for the remainder of this session.' },
  { name: 'registerProcess', signature: 'registerProcess(name: string, pid: number, task: string): Promise<void>', doc: 'Track a long-running child process so the engine can surface its status in engine events.' },
  { name: 'deregisterProcess', signature: 'deregisterProcess(name: string): Promise<void>', doc: 'Untrack a process previously registered via registerProcess.' },
  { name: 'listProcesses', signature: 'listProcesses(): Promise<ProcessInfo[]>', doc: 'Enumerate every process the harness has registered on this session.' },
  { name: 'terminateProcess', signature: 'terminateProcess(name: string): Promise<void>', doc: 'Terminate a registered process. Engine sends SIGTERM and follows up with SIGKILL on stubborn children.' },
  { name: 'cleanStaleProcesses', signature: 'cleanStaleProcesses(): Promise<number>', doc: 'Reap registered processes whose PIDs no longer exist. Returns the count cleaned.' },
]

let sdkMethodsCache: SDKMethod[] | null = null

/**
 * Returns the IonContext method list with one-line signatures and prose
 * descriptions, sourced from `types.ts` at first call.
 *
 * Falls back to the snapshot above when parsing fails. Snapshot is
 * regression-tested against the live parse so drift surfaces in CI.
 */
export function getSDKMethods(): SDKMethod[] {
  if (sdkMethodsCache !== null) return sdkMethodsCache

  const path = resolveSdkTypesPath()
  if (!existsSync(path)) {
    log.warn('ion-meta: SDK types.ts not found; falling back to SDK method snapshot', {
      tried: path,
    })
    sdkMethodsCache = SDK_METHOD_SNAPSHOT
    return sdkMethodsCache
  }

  try {
    sdkMethodsCache = parseSDKMethods(readFileSync(path, 'utf8'))
    log.info('ion-meta: SDK methods parsed from types.ts', {
      count: sdkMethodsCache.length,
    })
  } catch (err) {
    log.warn('ion-meta: failed to parse IonContext methods; using snapshot', {
      err: (err as Error).message,
    })
    sdkMethodsCache = SDK_METHOD_SNAPSHOT
  }
  return sdkMethodsCache
}

/**
 * Extract the body of `export interface IonContext { ... }` and emit one
 * SDKMethod entry per method-shaped member (skipping data fields like
 * `sessionKey`, `cwd`, `model`, `config`).
 *
 * Doc strings come from the preceding `/** ... *\/` JSDoc block when
 * present; otherwise the doc field is empty.
 */
export function parseSDKMethods(source: string): SDKMethod[] {
  const block = extractInterfaceBlock(source, 'IonContext')
  if (!block) throw new Error('IonContext interface not found in source')

  const out: SDKMethod[] = []
  const lines = block.split('\n')

  let pendingDoc: string[] | null = null
  let inDoc = false

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const trimmed = line.trim()

    // JSDoc capture.
    if (trimmed.startsWith('/**')) {
      pendingDoc = []
      inDoc = true
      if (trimmed.endsWith('*/')) {
        const content = trimmed.slice(3, -2).trim()
        if (content) pendingDoc.push(content)
        inDoc = false
      }
      continue
    }
    if (inDoc) {
      if (trimmed.endsWith('*/')) {
        const content = trimmed.replace(/^\*\s?/, '').replace(/\*\/$/, '').trim()
        if (content) pendingDoc!.push(content)
        inDoc = false
        continue
      }
      const content = trimmed.replace(/^\*\s?/, '').trim()
      if (content) pendingDoc!.push(content)
      continue
    }

    // Method signature: `name(args): Return` or `name<T>(args): Return`.
    // We collapse multi-line signatures by reading forward until we see a
    // line ending in `)` (no nested parens to worry about in IonContext).
    const sigMatch = trimmed.match(/^([a-zA-Z][a-zA-Z0-9]*)\s*[<(]/)
    if (sigMatch) {
      const name = sigMatch[1]
      // Skip data-field-shaped entries (only methods qualify).
      if (
        name === 'sessionKey' || name === 'cwd' || name === 'model' ||
        name === 'config' || name === 'input'
      ) {
        pendingDoc = null
        continue
      }
      // Collapse the signature: read lines until paren balance is 0 and
      // the line ends with a colon-typed return + semicolon-or-newline.
      let sig = trimmed
      let parens = countParens(trimmed)
      while (parens !== 0 && i + 1 < lines.length) {
        i++
        const next = lines[i].trim()
        sig += ' ' + next
        parens += countParens(next)
      }
      // Normalise whitespace.
      sig = sig.replace(/\s+/g, ' ').trim().replace(/;$/, '')
      const docText = (pendingDoc ?? []).join(' ').replace(/\s+/g, ' ').trim()
      out.push({ name, signature: sig, doc: docText })
      pendingDoc = null
      continue
    }

    // Reset pending doc on any other significant line.
    if (trimmed && !trimmed.startsWith('//')) {
      pendingDoc = null
    }
  }

  if (out.length === 0) throw new Error('IonContext parsed but zero methods found')
  return out
}

function countParens(s: string): number {
  let n = 0
  for (const c of s) {
    if (c === '(') n++
    else if (c === ')') n--
  }
  return n
}

/**
 * Extract `export interface <Name> { ... }` body. Returns null when the
 * named interface is not present. Handles brace nesting (interface bodies
 * include nested object types).
 */
function extractInterfaceBlock(source: string, name: string): string | null {
  const re = new RegExp(`export\\s+interface\\s+${name}\\s*{`)
  const match = re.exec(source)
  if (!match) return null

  const start = match.index + match[0].length
  let depth = 1
  let i = start
  while (i < source.length && depth > 0) {
    const c = source[i]
    if (c === '{') depth++
    else if (c === '}') depth--
    i++
  }
  if (depth !== 0) return null
  return source.slice(start, i - 1)
}

// ─── Summary helpers used by the persona ──────────────────────────────────

/** Total hook count across all categories. */
export function totalHookCount(): number {
  return Object.values(getHookCatalog()).reduce((acc, v) => acc + v.length, 0)
}

/** Category order honoured by the persona builder. */
export function hookCategoryOrder(): readonly string[] {
  return HOOK_CATEGORY_ORDER
}
