// ion-meta agent-state emission.
//
// Centralises every `engine_agent_state` snapshot ion-meta sends. The
// engine treats `engine_agent_state` as a complete-snapshot contract:
// consumers replace their local view with the payload, never merge. See
// `docs/architecture/agent-state.md`. As a consequence, every snapshot
// emission must list ALL agents ion-meta wants visible — partial updates
// are silently destructive.
//
// We build the snapshot from the static specialist roster plus a runtime
// `active` map keyed on specialist name. The map records the timestamp
// `agent_start` saw, so `agent_end` can compute elapsed time before
// flipping the status back to idle (or `done`, when the parent run
// completed).

import type { IonContext } from '../sdk/ion-sdk'

/**
 * Stable specialist roster surfaced in the desktop Agents panel from
 * `session_start` through `session_end`. Each specialist appears as a
 * row; status flips to `running` while the specialist is dispatched and
 * back to `idle` when done.
 *
 * Order is the order shown in the panel. Mode-shaped agents come first
 * (tutor / improver / builder) because they reflect the *active intent*
 * — the user typed "build me an X" or "audit this harness" and the
 * relevant row flipping to running is the visible feedback that the
 * orchestrator routed correctly. Knowledge specialists follow as the
 * deep-dive helpers.
 *
 * The orchestrator is intentionally NOT in this list. The orchestrator
 * IS the conversation -- it is the persona injected via `before_prompt`
 * from `agents/orchestrator.md`, and the user is talking to it
 * directly. Listing it as a panel row would suggest a dispatchable
 * sub-agent that runs and finishes, which is the wrong mental model
 * (matches chief-of-staff's convention: the root is the conversation,
 * the panel lists dispatch targets only). `agents/orchestrator.md`
 * remains on disk because `persona.ts` reads its body to compose the
 * system prompt; the engine's agent-discovery walk also enumerates it,
 * but no caller dispatches to it.
 */
export const SPECIALISTS: readonly { name: string; displayName: string; type: string; color: string }[] = [
  // Mode-shaped agents (the orchestrator routes to these based on intent).
  { name: 'ion-tutor',           displayName: 'Tutor',              type: 'specialist', color: '#2e8b57' },
  { name: 'extension-improver',  displayName: 'Extension Improver', type: 'specialist', color: '#b45a28' },
  { name: 'extension-builder',   displayName: 'Extension Builder',  type: 'specialist', color: '#6432b4' },
  // Knowledge-shaped specialists (one per topical surface; used when a
  // conversation goes deep on one area).
  { name: 'extension-architect', displayName: 'Extension Architect', type: 'specialist', color: '#1e7896' },
  { name: 'agent-designer',      displayName: 'Agent Designer',     type: 'specialist', color: '#c83264' },
  { name: 'skill-author',        displayName: 'Skill Author',       type: 'specialist', color: '#8c6e14' },
  { name: 'hook-specialist',     displayName: 'Hook Specialist',    type: 'specialist', color: '#3c5ab4' },
  { name: 'testing-guide',       displayName: 'Testing Guide',      type: 'specialist', color: '#1e966e' },
  { name: 'orchestration-designer', displayName: 'Orchestration Designer', type: 'specialist', color: '#964b1e' },
] as const

interface ActiveEntry {
  startTime: number
  lastWork?: string
}

/**
 * Per-session active-specialist tracker. Keyed on sessionKey so concurrent
 * ion-meta sessions in the same extension subprocess do not stomp each
 * other's panels.
 */
const activeBySession = new Map<string, Map<string, ActiveEntry>>()

function getActive(sessionKey: string): Map<string, ActiveEntry> {
  let m = activeBySession.get(sessionKey)
  if (!m) {
    m = new Map<string, ActiveEntry>()
    activeBySession.set(sessionKey, m)
  }
  return m
}

/**
 * Emit the initial snapshot at `session_start`: every specialist marked
 * idle. Always emitted as a full snapshot (the only kind there is).
 *
 * Note: nothing is marked `running` here. The orchestrator (the
 * conversation itself) is not a panel row; the specialists do not run
 * until the LLM dispatches them via the Agent tool.
 */
export function emitInitialSnapshot(ctx: IonContext): void {
  const active = getActive(ctx.sessionKey)
  active.clear()
  ctx.emit({
    type: 'engine_agent_state',
    agents: buildAgentList(ctx.sessionKey),
  })
}

/**
 * Update the snapshot when a specialist begins running. Called from the
 * `agent_start` hook. We add the specialist to the active map and re-emit
 * the full agent list with the running status.
 *
 * Unknown agent names (i.e. not in our specialist roster) are ignored:
 * the engine fires `agent_start` for Agent-tool sub-agents the LLM
 * dispatches, which may include agents from other extensions. ion-meta's
 * panel only tracks ion-meta specialists.
 */
export function emitAgentRunning(ctx: IonContext, name: string, lastWork?: string): void {
  if (!isSpecialist(name)) return
  const active = getActive(ctx.sessionKey)
  active.set(name, { startTime: Date.now(), lastWork })
  ctx.emit({
    type: 'engine_agent_state',
    agents: buildAgentList(ctx.sessionKey),
  })
}

/**
 * Update the snapshot when a specialist finishes. Called from the
 * `agent_end` hook. Removes the specialist from the active map and
 * re-emits the full list.
 *
 * `isSpecialist` rejects names not in our roster (e.g. orchestrator,
 * which is the root persona and not a panel row, plus any Agent-tool
 * dispatch outside the ion-meta family).
 */
export function emitAgentDone(ctx: IonContext, name: string, lastWork?: string): void {
  if (!isSpecialist(name)) return
  const active = getActive(ctx.sessionKey)
  const entry = active.get(name)
  active.delete(name)
  // Re-emit; the specialist now appears as idle in the snapshot. If the
  // caller passed `lastWork`, persist it for the idle row by stashing on a
  // sidecar map indexed under a sentinel key.
  if (entry) {
    lastWorkBySession.set(`${ctx.sessionKey}:${name}`, lastWork ?? entry.lastWork ?? '')
  }
  ctx.emit({
    type: 'engine_agent_state',
    agents: buildAgentList(ctx.sessionKey),
  })
}

/**
 * Emit the terminal snapshot at `session_end`: wipe the panel by emitting
 * `agents: []`. See docs/architecture/agent-state.md §"Session reset".
 *
 * We also free the per-session maps so memory does not leak across many
 * short-lived sessions in one subprocess.
 */
export function emitTerminalSnapshot(ctx: IonContext): void {
  activeBySession.delete(ctx.sessionKey)
  for (const k of Array.from(lastWorkBySession.keys())) {
    if (k.startsWith(`${ctx.sessionKey}:`)) lastWorkBySession.delete(k)
  }
  ctx.emit({ type: 'engine_agent_state', agents: [] })
}

const lastWorkBySession = new Map<string, string>()

function isSpecialist(name: string): boolean {
  return SPECIALISTS.some(s => s.name === name)
}

interface PanelAgent {
  name: string
  status: 'idle' | 'running' | 'done' | 'error'
  metadata: {
    displayName: string
    visibility: string
    invited: boolean
    type: string
    color: string
    startTime?: number
    elapsed?: number
    lastWork?: string
  }
}

function buildAgentList(sessionKey: string): PanelAgent[] {
  const active = getActive(sessionKey)
  return SPECIALISTS.map(s => {
    const entry = active.get(s.name)
    const lastWork = lastWorkBySession.get(`${sessionKey}:${s.name}`) || undefined
    if (entry) {
      return {
        name: s.name,
        status: 'running' as const,
        metadata: {
          displayName: s.displayName,
          // All panel rows are specialists (the orchestrator is the
          // conversation, not a panel row), so visibility is uniformly
          // 'sticky' -- visible when running, faded when idle.
          visibility: 'sticky',
          invited: true,
          type: s.type,
          color: s.color,
          startTime: entry.startTime,
          lastWork: entry.lastWork,
        },
      }
    }
    return {
      name: s.name,
      status: 'idle' as const,
      metadata: {
        displayName: s.displayName,
        visibility: 'sticky',
        invited: true,
        type: s.type,
        color: s.color,
        lastWork,
      },
    }
  })
}
