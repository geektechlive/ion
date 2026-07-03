/**
 * Conversation tail fingerprint — the cross-platform staleness signal for the
 * iOS main-conversation heal.
 *
 * The iOS client builds its main conversation from live wire deltas
 * (desktop_text_delta / desktop_tool_start / desktop_tool_end) plus a one-time
 * history load. When deltas are lost (e.g. a LAN↔relay transport switch or a
 * seq gap mid-stream), iOS silently freezes — a tool stays "running", the
 * assistant text stops mid-sentence — while the desktop streams to completion.
 *
 * This fingerprint lets iOS DETECT that drift cheaply. The desktop computes it
 * over the active conversation's message tail and sends it in the snapshot;
 * iOS computes the SAME fingerprint over its local tail and compares. When in
 * sync (even mid-stream) the two are byte-identical, so there is no
 * false-positive reload; when iOS missed a delta they diverge and iOS re-fetches
 * the authoritative history.
 *
 * CRITICAL: the Swift implementation (conversationTailFingerprint in
 * SessionViewModel+Snapshot.swift) and the inline-JS copy in snapshot.ts's
 * executeJavaScript projection MUST produce byte-identical output for the same
 * input. The pinning rules:
 *
 *   - Window: the last TAIL_WINDOW messages, in order.
 *   - Per message token:
 *       tool rows:        "<id>:t<statusToken>"   (status only — see below)
 *       non-tool rows:    "<id>:<utf8ByteLen>"
 *     statusToken ∈ { r, c, e, - } for running / completed / error / none.
 *   - Tokens joined with ",". No total-message-count suffix.
 *   - Content length is UTF-8 BYTE length (Swift `content.utf8.count`,
 *     JS `new TextEncoder().encode(content).length`) — never UTF-16 .length,
 *     which would diverge on any non-ASCII content and cause a reload loop.
 *   - Tool rows are fingerprinted by STATUS ONLY (no content length). The
 *     history page truncates tool content >2KB (tabs.ts) while the snapshot
 *     sees the full content, so including a tool's content length would make
 *     a big tool result permanently diverge after a reload (reload loop). The
 *     tool's status flip (running→completed) is the signal we need, and it is
 *     truncation-immune.
 *
 * Any change here must be mirrored in BOTH the inline JS (snapshot.ts) and the
 * Swift (SessionViewModel+Snapshot.swift), and the parity is pinned by tests:
 * desktop conversation-fingerprint.test.ts and iOS ConversationStalenessReconcileTests.
 */

/** Number of trailing messages the fingerprint spans. Smaller than the history
 *  PAGE_SIZE so pagination never causes divergence: both sides fingerprint the
 *  same final-N window regardless of how much older history each holds. Large
 *  enough to span a stuck tool plus its surrounding turn. */
export const FINGERPRINT_TAIL_WINDOW = 10

/** Minimal message shape the fingerprint needs. */
export interface FingerprintMessage {
  id: string
  role: string
  content: string
  toolStatus?: string
}

/** UTF-8 byte length, matching Swift's `content.utf8.count`. */
function utf8ByteLength(s: string): number {
  return new TextEncoder().encode(s).length
}

/** Map a tool status to its single-char token. */
function statusToken(toolStatus: string | undefined): string {
  switch (toolStatus) {
    case 'running': return 'r'
    case 'completed': return 'c'
    case 'error': return 'e'
    default: return '-'
  }
}

/**
 * Build the tail fingerprint for a conversation's message list.
 *
 * The fingerprint is the joined tail tokens ONLY — it deliberately does NOT
 * include a total message count. iOS holds a paginated PAGE of the conversation
 * (its local count is the page size), while the desktop holds the FULL list, so
 * any total-count term would diverge on every conversation longer than one page
 * and reload-loop the iOS heal. The tail tokens alone are complete: a message
 * can only enter the conversation by being appended at the end (deltas append),
 * so a dropped new message shifts a tail token; a message only falls outside the
 * tail after 10+ newer messages arrive, which themselves shift tail tokens. The
 * tail is both pagination-safe and sufficient.
 */
export function conversationTailFingerprint(messages: FingerprintMessage[]): string {
  const tail = messages.slice(Math.max(0, messages.length - FINGERPRINT_TAIL_WINDOW))
  const tokens = tail.map((m) => {
    if (m.role === 'tool') {
      return `${m.id}:t${statusToken(m.toolStatus)}`
    }
    return `${m.id}:${utf8ByteLength(m.content || '')}`
  })
  return tokens.join(',')
}
