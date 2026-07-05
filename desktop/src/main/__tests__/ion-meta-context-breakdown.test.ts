/**
 * ion-meta context-breakdown consumer — source-level verification.
 *
 * Verifies that the in-repo ion-meta extension wires the `message_end`
 * hook to log context usage from the engine_context_breakdown event.
 * The hook is an observability consumer: it calls ctx.getContextUsage()
 * after each LLM turn so the breakdown surface has a live in-repo
 * consumer and the log emits structured context telemetry.
 *
 * These are source-scan tests — we read the ion-meta index.ts source
 * and assert that the hook registration and call site are present.
 * The same pattern is used by the WI-003 regression guard in
 * snapshot-wi-003-status-parity.test.ts.
 */

import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { join } from 'path'

const ION_META_SRC = readFileSync(
  join(__dirname, '../../../../engine/extensions/ion-meta/index.ts'),
  'utf-8',
)

describe('ion-meta context-breakdown consumer', () => {
  it('wires message_end hook', () => {
    expect(ION_META_SRC).toContain("ion.on('message_end'")
  })

  it('calls ctx.getContextUsage() in message_end handler', () => {
    expect(ION_META_SRC).toContain('ctx.getContextUsage()')
  })

  it('logs contextPercent and contextTokens from usage', () => {
    expect(ION_META_SRC).toContain('contextPercent: usage.percent')
    expect(ION_META_SRC).toContain('contextTokens: usage.tokens')
  })

  it('logs costUsd from usage', () => {
    expect(ION_META_SRC).toContain('costUsd: usage.cost')
  })

  it('handles getContextUsage failure non-fatally (debug log only)', () => {
    // Non-fatal path must use log.debug, not log.error, so normal
    // turns that produce no usage (e.g. first harness message) stay quiet.
    const msgEndBlock = ION_META_SRC.slice(
      ION_META_SRC.indexOf("ion.on('message_end'"),
    )
    expect(msgEndBlock).toContain('log.debug')
    expect(msgEndBlock).not.toContain("log.error('ion-meta: getContextUsage")
  })

  it('documents the breakdown surface purpose in a comment', () => {
    expect(ION_META_SRC).toContain('engine_context_breakdown')
  })
})
