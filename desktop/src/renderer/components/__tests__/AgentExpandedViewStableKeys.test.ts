// @vitest-environment jsdom
//
// Regression tests for stable React keys in AgentExpandedView's render loop.
//
// Before the fix, assistant rows used `a-${idx}`, tool-group rows used
// `tg-${idx}`, and agent-turn rows used `at-${idx}` — all positional.
// Inserting a message at the front (or any position before the last item)
// shifted every subsequent key, causing React to tear down and remount those
// DOM nodes. That remount also scrolled the panel back to the top.
//
// After the fix, keys derive from stable message IDs (role-timestamp for
// non-tools, tool-${toolId} for tools). Appending a message at the tail
// changes ONLY the new element's key; existing keys are untouched.
//
// How we verify without rendering: `groupMessages` is the same pure function
// the component calls. We apply the same key expressions the component render
// loop uses, collect them, and assert stability across two grouped arrays
// that differ only in the tail. If any test fails on the pre-fix code it is
// because the old `a-${idx}` / `tg-${idx}` expression indexed into the
// positional slot — inserting at the front shifts every slot.
import { describe, it, expect } from 'vitest'
import { groupMessages } from '../conversation'
import { mapConversationMessages } from '../agent-conversation-mapper'
import type { RawSessionMessage } from '../agent-conversation-mapper'

// Helper: derive the React key string the component assigns to each
// GroupedItem — mirrors the render loop in AgentExpandedView exactly.
function deriveKey(item: ReturnType<typeof groupMessages>[number], idx: number): string {
  if (item.kind === 'user') return item.message.id
  if (item.kind === 'thinking') return item.message.id
  if (item.kind === 'assistant') return item.message.id
  if (item.kind === 'tool-group') {
    const first = item.messages[0]
    return first?.toolId ? `tg-${first.toolId}` : `tg-${first?.id ?? idx}`
  }
  if (item.kind === 'agent-turn') {
    const firstToolId = item.tools[0]?.toolId
    return firstToolId
      ? `at-${firstToolId}`
      : `at-${item.tools[0]?.id ?? item.assistantMessages[0]?.id ?? idx}`
  }
  // harness / system / compaction / intercept — not rendered by AgentExpandedView
  // but groupMessages may produce them; return a sentinel so the test can skip.
  return `skip-${idx}`
}

const RAW_USER: RawSessionMessage = { role: 'user', content: 'task', timestamp: 1000 }
const RAW_ASSISTANT: RawSessionMessage = { role: 'assistant', content: 'working', timestamp: 2000 }
const RAW_TOOL: RawSessionMessage = {
  role: 'tool', content: '', toolName: 'Read', toolId: 'toolu_STABLE', timestamp: 3000,
}
const RAW_ASSISTANT2: RawSessionMessage = { role: 'assistant', content: 'done', timestamp: 4000 }

describe('AgentExpandedView render keys — stable after the fix', () => {
  it('assistant key uses message.id, not positional a-${idx}', () => {
    const msgs = mapConversationMessages([RAW_USER, RAW_ASSISTANT])
    const grouped = groupMessages(msgs, { includeUser: true })
    const assistantItem = grouped.find((g) => g.kind === 'assistant')!
    const key = deriveKey(assistantItem, 99)

    // New stable key: matches the id from mapConversationMessages
    expect(key).toBe('assistant-2000')
    // Must NOT be the old positional form
    expect(key).not.toMatch(/^a-\d+$/)
  })

  it('tool-group key uses tg-${toolId}, not positional tg-${idx}', () => {
    const msgs = mapConversationMessages([RAW_USER, RAW_TOOL])
    const grouped = groupMessages(msgs, { includeUser: true })
    const tgItem = grouped.find((g) => g.kind === 'tool-group')!
    const key = deriveKey(tgItem, 99)

    expect(key).toBe('tg-toolu_STABLE')
    expect(key).not.toMatch(/^tg-\d+$/)
  })

  it('appending a new assistant message does not shift the tool-group key', () => {
    // Base: user + tool
    const baseMsgs = mapConversationMessages([RAW_USER, RAW_TOOL])
    const baseGrouped = groupMessages(baseMsgs, { includeUser: true })

    // Extended: user + tool + assistant appended at the tail
    const extMsgs = mapConversationMessages([RAW_USER, RAW_TOOL, RAW_ASSISTANT2])
    const extGrouped = groupMessages(extMsgs, { includeUser: true })

    const baseTgKey = deriveKey(baseGrouped.find((g) => g.kind === 'tool-group')!, 0)
    const extTgKey = deriveKey(extGrouped.find((g) => g.kind === 'tool-group')!, 1)

    // The tool-group key must be identical even though its positional index
    // may differ between the two grouped arrays.
    expect(extTgKey).toBe(baseTgKey)
  })

  it('prepending a new user message does not shift the assistant key', () => {
    // Base: just the assistant turn
    const baseMsgs = mapConversationMessages([RAW_ASSISTANT])
    const baseGrouped = groupMessages(baseMsgs, { includeUser: true })

    // Extended: user prepended before assistant (simulates a fetch returning
    // the full conversation when previously only push data was shown)
    const extMsgs = mapConversationMessages([RAW_USER, RAW_ASSISTANT])
    const extGrouped = groupMessages(extMsgs, { includeUser: true })

    const baseAstKey = deriveKey(baseGrouped.find((g) => g.kind === 'assistant')!, 0)
    const extAstKey = deriveKey(extGrouped.find((g) => g.kind === 'assistant')!, 1)

    // Key is derived from message.id, which is role-timestamp — same regardless
    // of position in the grouped array.
    expect(extAstKey).toBe(baseAstKey)
    expect(extAstKey).toBe('assistant-2000')
  })

  it('two calls with the same input produce identical keys for every item', () => {
    const raw: RawSessionMessage[] = [RAW_USER, RAW_TOOL, RAW_ASSISTANT2]
    const msgs1 = mapConversationMessages(raw)
    const msgs2 = mapConversationMessages(raw)
    const g1 = groupMessages(msgs1, { includeUser: true })
    const g2 = groupMessages(msgs2, { includeUser: true })

    const keys1 = g1.map((item, i) => deriveKey(item, i))
    const keys2 = g2.map((item, i) => deriveKey(item, i))

    expect(keys1).toEqual(keys2)
  })
})
