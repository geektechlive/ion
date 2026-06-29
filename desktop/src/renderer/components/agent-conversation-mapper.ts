/**
 * Stable per-message ID mapping for dispatched-agent conversation history.
 *
 * The engine's `get_conversation` response (SessionMessage[]) carries a
 * `toolId` field on tool rows (the Anthropic tool-use ID, e.g. `toolu_01…`)
 * but has no top-level ID field on user/assistant rows. This module derives
 * stable, deterministic IDs so React's reconciler can preserve DOM nodes
 * across refetches rather than tearing them down and recreating them — which
 * previously reset the scroll position on every 12-second slow-reconcile poll.
 *
 * ID strategy:
 *  - Tool messages: `tool-${m.toolId}` — the Anthropic tool-use ID is
 *    persisted in the JSONL entry and never changes across refetches.
 *  - User / assistant messages: `${m.role}-${m.timestamp}`. The engine stores
 *    the entry timestamp on disk (int64 ms), so it is stable across refetches.
 *    Collision suffix `#n` (n >= 1) is appended when the same (role, timestamp)
 *    pair appears more than once in the same response (e.g. two assistant turns
 *    in the same millisecond).
 *
 * The helper is a pure function so it can be unit-tested without any React or
 * IPC infrastructure.
 */

import type { Message } from '../../shared/types'

/** Shape of a raw message returned by window.ion.getConversation. */
export interface RawSessionMessage {
  role: string
  content: string
  toolName?: string
  toolId?: string
  toolInput?: string
  timestamp?: number
}

/**
 * Map raw engine SessionMessage[] into typed Message[] with stable IDs.
 *
 * Called by AgentPanel.refetchConversation after every getConversation fetch.
 * Deterministic: calling with the same input always produces the same IDs.
 */
export function mapConversationMessages(rawMessages: RawSessionMessage[]): Message[] {
  // Track how many times each (role, timestamp) base key has been seen so far
  // in this response, to generate collision-avoidance suffixes.
  const seenCounts = new Map<string, number>()

  return rawMessages.map((m) => {
    let id: string

    if (m.toolId) {
      // Tool rows: use the persisted Anthropic tool-use ID directly.
      id = `tool-${m.toolId}`
    } else {
      // User / assistant rows: base key is role + timestamp.
      const ts = m.timestamp ?? 0
      const base = `${m.role}-${ts}`
      const count = seenCounts.get(base) ?? 0
      seenCounts.set(base, count + 1)
      // First occurrence uses the plain base key; subsequent occurrences append
      // a 1-based suffix so every ID in the array is unique.
      id = count === 0 ? base : `${base}#${count}`
    }

    return {
      id,
      role: m.role as Message['role'],
      content: m.content,
      toolName: m.toolName || '',
      toolId: m.toolId || '',
      toolInput: m.toolInput || '',
      toolStatus: 'completed' as const,
      timestamp: m.timestamp ?? 0,
    }
  })
}
