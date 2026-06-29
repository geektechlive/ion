import React, { useState, useEffect, useMemo } from 'react'
import { SpinnerGap } from '@phosphor-icons/react'
import { useColors } from '../theme'
import { usePreferencesStore } from '../preferences'
import { groupMessages, ToolGroup, AssistantMessage, MessageBubble, AgentTurnGroup, ThinkingBlock } from './conversation'
import { meta, formatDuration } from './agent-panel-helpers'
import { DispatchPager } from './DispatchPager'
import type { DispatchInfo } from './agent-panel-helpers'
import type { AgentStateUpdate } from '../../shared/types'
import type { Message } from '../../shared/types'

export function DurationDisplay({ startTime, elapsed, status }: { startTime?: number; elapsed?: number; status: string }) {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    if (status !== 'running' || !startTime) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [status, startTime])

  if (status === 'running' && startTime) {
    const secs = Math.floor((now / 1000) - startTime)
    return <span>{formatDuration(Math.max(0, secs))}</span>
  }
  if (elapsed != null) {
    return <span>{formatDuration(Math.round(elapsed))}</span>
  }
  return null
}

// ─── Structured expanded view for agent history ───

export interface ExpandedViewProps {
  agent: AgentStateUpdate
  colors: ReturnType<typeof useColors>
  loadedMessages?: Message[]
  loading?: boolean
  isFullscreen?: boolean
  dispatches: DispatchInfo[]
  selectedDispatch: number
  onSelectDispatch: (index: number) => void
  /**
   * When provided, the header (infoBar + pager) is handed to this slot
   * instead of rendered inline before the messages. The caller is responsible
   * for placing the returned node in a non-scrolling region. When absent
   * (inline-expand path in AgentPanel), behavior is unchanged — the header
   * renders inline exactly as before.
   */
  headerSlot?: (header: React.ReactNode) => React.ReactNode
}

export function AgentExpandedView({ agent, colors, loadedMessages, loading, isFullscreen, dispatches, selectedDispatch, onSelectDispatch, headerSlot }: ExpandedViewProps) {
  // In popup/fullscreen mode, use normal padding; inline mode indents past the agent label
  const leftPad = isFullscreen ? 12 : 148
  const unifiedTurnView = usePreferencesStore((s) => s.unifiedTurnView)
  const hasMultipleDispatches = dispatches.length > 1
  // When pager is active, show the selected dispatch's info instead of top-level
  const activeDispatch = hasMultipleDispatches ? dispatches[selectedDispatch] : undefined
  const agentModel = activeDispatch?.model || meta<string>(agent, 'model', '')
  // When a specific dispatch is selected, derive the duration STRICTLY from that
  // dispatch's own startTime/elapsed/status. Do NOT fall back to the live agent's
  // clock — a selected dispatch with no startTime and a non-running status must
  // show no ticking timer rather than borrowing the agent's running duration.
  // Only the single-dispatch (no activeDispatch) path uses the agent-level values.
  const startTime = activeDispatch
    ? activeDispatch.startTime
    : (agent.metadata?.startTime as number | undefined)
  const elapsed = activeDispatch
    ? activeDispatch.elapsed
    : (agent.metadata?.elapsed as number | undefined)
  const activeStatus = activeDispatch ? activeDispatch.status : agent.status
  // "Working..." is gated on the selected dispatch's own status when a specific
  // dispatch is selected; otherwise it falls back to the agent's status.
  const dispatchIsRunning = activeDispatch
    ? activeDispatch.status === 'running'
    : agent.status === 'running'
  const showInfoBar = !hasMultipleDispatches && (agentModel || startTime != null || elapsed != null)

  // Derive the message list and group it unconditionally, above every early
  // return below. This must run on every render regardless of loading state —
  // a conditional hook (useMemo gated behind the `loading` / empty-messages
  // branches) changes the hook count between renders and crashes the renderer
  // with a rules-of-hooks violation (React #185 on open, #310 on dispatch
  // switch). `groupMessages` is pure and returns [] for an empty input, so
  // computing it here is safe and cheap even when no branch consumes `grouped`.
  const messages = loadedMessages || meta(agent, 'messages', [] as any[])
  const msgs: Message[] = loadedMessages
    ? loadedMessages
    : (messages as any[]).map((m: any, i: number) => ({
        id: `${agent.name}-msg-${i}`,
        role: m.role as any,
        content: m.content,
        toolName: m.toolName,
        toolInput: '',
        toolStatus: 'completed' as const,
        timestamp: 0,
      }))
  const grouped = useMemo(() => groupMessages(msgs, { includeUser: true, unifiedTurnView }), [msgs, unifiedTurnView])

  const infoBar = showInfoBar ? (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 4,
        padding: `4px 12px 4px ${leftPad}px`,
        background: 'rgba(255,255,255,0.03)',
        fontSize: 10,
        color: colors.textTertiary,
        borderBottom: '1px solid rgba(255,255,255,0.04)',
      }}
    >
      {agentModel && <span>Model: {agentModel}</span>}
      {agentModel && (startTime != null || elapsed != null) && <span style={{ opacity: 0.4 }}>|</span>}
      {(startTime != null || elapsed != null) && (
        <span>
          Duration: <DurationDisplay startTime={startTime} elapsed={elapsed} status={activeStatus} />
        </span>
      )}
    </div>
  ) : null

  // Dispatch pager — shown when multiple dispatches exist
  const pager = hasMultipleDispatches ? (
    <DispatchPager dispatches={dispatches} selectedIndex={selectedDispatch} onSelect={onSelectDispatch} compact={isFullscreen} />
  ) : null

  // The header — infoBar (single-dispatch model/duration) and pager
  // (multi-dispatch tab strip). When headerSlot is provided, the parent places
  // this in a non-scrolling region and we render nothing here; when absent,
  // it renders inline.
  const header = <>{infoBar}{pager}</>

  // Emit header to slot (parent places it) or render inline (no slot).
  // In both cases the message body renders in the normal position below.
  const renderedHeader = headerSlot ? headerSlot(header) : header

  if (loading) {
    return (
      <div style={{ background: 'rgba(255,255,255,0.03)' }}>
        {renderedHeader}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            padding: `8px 12px 8px ${leftPad}px`,
            fontSize: 11,
            color: colors.textTertiary,
          }}
        >
          <SpinnerGap size={12} style={{ animation: 'spin 1s linear infinite' }} />
          Loading conversation...
        </div>
      </div>
    )
  }

  // Use loaded messages from engine, or fall back to metadata. `messages`,
  // `msgs`, and `grouped` are all derived unconditionally above.
  if (messages && messages.length > 0) {
    return (
      <div style={{ background: 'rgba(255,255,255,0.03)' }}>
        {renderedHeader}
        <div
          style={{
            maxHeight: isFullscreen ? undefined : 200,
            overflowY: 'auto',
            padding: `8px 12px 8px ${leftPad}px`,
          }}
        >
          {grouped.map((item, idx) => {
            if (item.kind === 'user') {
              return <MessageBubble key={item.message.id} message={item.message} skipMotion />
            }
            if (item.kind === 'assistant') {
              // Stable ID from mapConversationMessages (role-timestamp) or push
              // path (dispatch-text-{seq}) — no longer positional a-${idx}.
              return <AssistantMessage key={item.message.id} message={item.message} skipMotion />
            }
            if (item.kind === 'tool-group') {
              // Key off the first tool's stable toolId when present, falling
              // back to its message id. Never positional — insertion of a new
              // tool group must not shift keys of existing ones.
              const tgKey = item.messages[0]?.toolId
                ? `tg-${item.messages[0].toolId}`
                : `tg-${item.messages[0]?.id ?? idx}`
              return <ToolGroup key={tgKey} tools={item.messages} skipMotion />
            }
            if (item.kind === 'agent-turn') {
              // Key off the first stable id within the turn: prefer toolId of
              // the first tool entry, then fallback to its message id, then the
              // first assistant message id. Never positional.
              const firstToolId = item.tools[0]?.toolId
              const atKey = firstToolId
                ? `at-${firstToolId}`
                : `at-${item.tools[0]?.id ?? item.assistantMessages[0]?.id ?? idx}`
              return <AgentTurnGroup key={atKey} tools={item.tools} assistantMessages={item.assistantMessages} isActive={item.isActive} thinking={item.thinking} skipMotion />
            }
            if (item.kind === 'thinking') {
              return <ThinkingBlock key={item.message.id} message={item.message} skipMotion />
            }
            return null
          })}
        </div>
      </div>
    )
  }

  // Fallback to raw fullOutput
  const fullOutput = meta(agent, 'fullOutput', '')
  if (fullOutput) {
    return (
      <div style={{ background: 'rgba(255,255,255,0.03)' }}>
        {renderedHeader}
        <div
          style={{
            maxHeight: isFullscreen ? undefined : 120,
            overflowY: 'auto',
            fontFamily: 'monospace',
            fontSize: 11,
            color: colors.textSecondary,
            padding: `8px 12px 8px ${leftPad}px`,
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
          }}
        >
          {fullOutput}
        </div>
      </div>
    )
  }

  return (
    <div style={{ background: 'rgba(255,255,255,0.03)' }}>
      {renderedHeader}
      <div
        style={{
          padding: `8px 12px 8px ${leftPad}px`,
          fontSize: 11,
          color: colors.textTertiary,
        }}
      >
        {dispatchIsRunning
          ? 'Working...'
          : activeDispatch
            ? 'No transcript recorded for this dispatch'
            : 'No conversation data available'}
      </div>
    </div>
  )
}
