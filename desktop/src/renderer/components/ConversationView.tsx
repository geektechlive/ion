import React, { useCallback, useEffect, useRef, useState, useMemo } from 'react'
import { AnimatePresence, motion } from 'framer-motion'
import { useSessionStore } from '../stores/sessionStore'
import { usePreferencesStore } from '../preferences'
import { useColors } from '../theme'
import { runHandleImplement } from './ConversationView-implement'
import { EngineDialog } from './EngineDialog'
import { EngineNotificationToasts } from './EngineNotificationToasts'
import { AgentPanel } from './AgentPanel'
import { PermissionDeniedCard } from './PermissionDeniedCard'
import { resolvePlanCardSuppression } from '../../shared/plan-card-gate'
import { useClearPermissionDenied } from '../hooks/useClearPermissionDenied'
import { ElicitationCardHost } from './ElicitationCardHost'
import { TodoListPanel } from './TodoListPanel'
import { ConversationSearch } from './ConversationSearch'
import { useConversationSearch } from '../hooks/useConversationSearch'
import { ArrowDown } from '@phosphor-icons/react'
import {
  groupMessages,
  ToolGroup, AssistantMessage, SystemMessage, HarnessMessage, MessageBubble,
  CopyButton, MessageActions, InterruptButton, CompactionRow, AgentTurnGroup, InterceptBanner, ThinkingBlock,
  QueuedMessage, EmptyState,
} from './conversation'

// Stable empty refs to avoid creating new array/object references on every render.
// Without these, `|| []` in selectors creates a new array each time, which Zustand
// treats as a change (Object.is), triggering cascading re-renders.
const EMPTY_ARRAY: any[] = []
const EMPTY_NOTIFICATIONS: any[] = []
const EMPTY_MESSAGES: any[] = []
const EMPTY_AGENTS: any[] = []

const INITIAL_RENDER_CAP = 100
const PAGE_SIZE = 100

// ─── Main Component ───
//
// The single, unified conversation view for EVERY tab — plain or
// extension-backed. There is no separate "engine view": this component (the
// former, richer EngineView) renders every feature from DATA, so engine-only
// chrome (agent panel, dialog, toasts, pinned prompt, working message) simply
// self-hides when its backing collection is empty. A plain conversation that
// dispatches background sub-agents shows the agent panel exactly like an
// extension-backed one. App.tsx mounts this for all non-terminal tabs.

interface ConversationViewProps {
  tabId: string
}

export function ConversationView({ tabId }: ConversationViewProps) {
  const colors = useColors()
  const pane = useSessionStore(s => s.conversationPanes.get(tabId))
  const activeInstanceId = pane?.activeInstanceId || ''
  const key = activeInstanceId ? tabId : ''
  const conversationFontSize = usePreferencesStore((s) => s.conversationFontSize)
  const queuedPrompts = useSessionStore(s => s.tabs.find(t => t.id === tabId)?.queuedPrompts ?? EMPTY_ARRAY)
  const editQueuedMessage = useSessionStore(s => s.editQueuedMessage)

  const pinnedPrompt = useSessionStore(s => {
    const p = s.conversationPanes.get(tabId)
    const k = p?.activeInstanceId ? tabId : ''
    return k ? (s.enginePinnedPrompt.get(k) || '') : ''
  })
  const notifications = useSessionStore(s => {
    const p = s.conversationPanes.get(tabId)
    const k = p?.activeInstanceId ? tabId : ''
    return k ? (s.engineNotifications.get(k) || EMPTY_NOTIFICATIONS) : EMPTY_NOTIFICATIONS
  })
  const messages = useSessionStore(s => {
    const p = s.conversationPanes.get(tabId)
    const inst = p?.activeInstanceId ? p.instances.find(i => i.id === p.activeInstanceId) : null
    return inst?.messages ?? EMPTY_MESSAGES
  })
  const agentStates = useSessionStore(s => {
    const p = s.conversationPanes.get(tabId)
    const inst = p?.activeInstanceId ? p.instances.find(i => i.id === p.activeInstanceId) : null
    return inst?.agentStates ?? EMPTY_AGENTS
  })
  const workingMessage = useSessionStore(s => {
    const p = s.conversationPanes.get(tabId)
    const k = p?.activeInstanceId ? tabId : ''
    return k ? (s.engineWorkingMessages.get(k) || '') : ''
  })
  const tabStatus = useSessionStore(s => s.tabs.find(t => t.id === tabId)?.status)
  // PermissionDenied is stored PER ENGINE INSTANCE on `instance.permissionDenied`.
  // Engine sub-tabs (instances) are independent sub-conversations, so storing
  // the denial on the parent tab would show the same card on every
  // sibling sub-tab. The card is scoped to whichever instance produced
  // it; switching to a sibling without a pending denial shows no card.
  //
  // Parent-tab pill bubbling: getWaitingState() in TabStripShared.ts
  // folds across instances for engine tabs. iOS receives the active
  // instance's denial via the snapshot path (see main/remote/snapshot.ts).
  const permissionDenied = useSessionStore(s => {
    const p = s.conversationPanes.get(tabId)
    const inst = p?.activeInstanceId ? p.instances.find(i => i.id === p.activeInstanceId) : null
    return inst?.permissionDenied ?? null
  })
  const tabPlanFilePath = useSessionStore(s => {
    const p = s.conversationPanes.get(tabId)
    const inst = p?.activeInstanceId ? p.instances.find(i => i.id === p.activeInstanceId) : null
    return inst?.planFilePath ?? null
  })
  const tabGroupPinned = useSessionStore(s => s.tabs.find(t => t.id === tabId)?.groupPinned)
  const tabConversationId = useSessionStore(s => s.tabs.find(t => t.id === tabId)?.conversationId)
  const staticInfo = useSessionStore(s => s.staticInfo)
  const submit = useSessionStore(s => s.submit)
  const interrupt = useSessionStore(s => s.interrupt)
  const isTall = useSessionStore(s => s.tallViewTabId === tabId)
  const toggleTallView = useSessionStore(s => s.toggleTallView)
  const unifiedTurnView = usePreferencesStore(s => s.unifiedTurnView)
  const engineModelOverride = useSessionStore(s => {
    const p = s.conversationPanes.get(tabId)
    const inst = p?.activeInstanceId ? p.instances.find(i => i.id === p.activeInstanceId) : null
    return inst?.modelOverride ?? undefined
  })
  const isRunning = tabStatus === 'running' || tabStatus === 'connecting'
  // Promote `.some(...)` to a count so we can render
  // "waiting for N background agent(s)" in the footer and Thinking
  // indicator. Computing both `runningChildCount` (number) and
  // `hasRunningChildren` (boolean) keeps existing call sites working
  // without scattering `.length > 0` checks. The boolean is still
  // used by the Interrupt-button visibility predicate further down,
  // which only cares about presence.
  const runningChildCount = agentStates.filter(a => a.status === 'running').length
  const hasRunningChildren = runningChildCount > 0
  // Defer the Plan Ready (ExitPlanMode) card while a background dispatch is
  // still running — see shared/plan-card-gate.ts for the full rationale. The
  // decision + its deferral log live in the helper to keep this component
  // under the size cap.
  const suppressPlanCard = resolvePlanCardSuppression({
    toolNames: permissionDenied?.tools.map((t) => t.toolName),
    hasRunningChildren,
    tabId,
    runningChildCount,
    log: console.log,
  })
  const [agentPanelFullscreen, setAgentPanelFullscreen] = useState(false)
  // Per-instance agent panel heights — persisted only for the tab's lifetime.
  // Key is the tabId (bare, since Phase 4b collapsed to a single 'main' instance).
  const [agentPanelHeights, setAgentPanelHeights] = useState<Map<string, number>>(new Map())
  const scrollRef = useRef<HTMLDivElement>(null)
  const isNearBottomRef = useRef(true)
  const [showScrollBtn, setShowScrollBtn] = useState(false)
  const [renderOffset, setRenderOffset] = useState(0)

  // Conversation search — scoped to scrollRef. A `scrollTrigger` string changes
  // whenever the scrollback grows so search re-indexes. Available for every tab.
  const searchTrigger = `${messages.length}:${messages[messages.length - 1]?.content?.length ?? 0}`
  const [searchState, searchActions] = useConversationSearch(scrollRef, searchTrigger)

  // Close search when switching tabs.
  useEffect(() => {
    window.dispatchEvent(new CustomEvent('ion:search-close'))
  }, [tabId])

  // Retry the last user message (failed-tab recovery) through the unified submit.
  const handleRetry = useCallback(() => {
    const lastUserMsg = [...messages].reverse().find((m) => m.role === 'user')
    if (lastUserMsg) submit(tabId, lastUserMsg.content)
  }, [messages, submit, tabId])

  // Reset pagination when switching engine instances
  useEffect(() => {
    setRenderOffset(0)
  }, [activeInstanceId])

  const handleScroll = useCallback(() => {
    if (!scrollRef.current) return
    const el = scrollRef.current
    const threshold = 80
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < threshold
    isNearBottomRef.current = nearBottom
    setShowScrollBtn(!nearBottom)
  }, [])

  // Include all messages (user messages shown inline, plus pinned prompt header)
  const totalCount = messages.length
  let startIndex = Math.max(0, totalCount - INITIAL_RENDER_CAP - renderOffset * PAGE_SIZE)
  const visibleMessages = startIndex > 0 ? messages.slice(startIndex) : messages
  const hasOlder = startIndex > 0
  const hiddenCount = totalCount - visibleMessages.length
  const grouped = useMemo(() => groupMessages(visibleMessages, { includeUser: true, unifiedTurnView }), [visibleMessages, unifiedTurnView])

  const hasContent = visibleMessages.some(m => m.role === 'assistant' && (m.content || '').length > 0)
  // Thinking indicator visibility — three modes:
  //   - foreground "Thinking…" (orange): orchestrator is running AND
  //     no assistant content has streamed yet AND no children are
  //     running. This is the original behaviour: show "Thinking…"
  //     between submit and first token while children haven't been
  //     dispatched.
  //   - background "Waiting for background agents…" (yellow):
  //     orchestrator is idle but at least one dispatched agent is
  //     still running. The label and dot color change so users
  //     understand the conversation is parked awaiting children, not
  //     stopped.
  //   - hidden otherwise.
  // The yellow branch wins when both could fire (orchestrator running
  // AND content) — but since we check `!isRunning` for the yellow
  // branch, that combination produces neither, which is correct: the
  // streaming-content pulse-dot in the message area is the right
  // signal then.
  const showThinkingForeground = isRunning && !hasContent && runningChildCount === 0
  const showWaitingChildren = !isRunning && hasRunningChildren
  const showThinking = showThinkingForeground || showWaitingChildren

  // Auto-scroll (only when user is near bottom)
  useEffect(() => {
    if (isNearBottomRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [messages.length, visibleMessages.length, agentStates.length, workingMessage, isRunning])

  // Auto-create first instance (skip during tab restoration to avoid
  // racing with the restore code that populates panes separately)
  const tabsReady = useSessionStore(s => s.tabsReady)
  useEffect(() => {
    if (!tabsReady) return
    const pane = useSessionStore.getState().conversationPanes.get(tabId)
    if (!pane || pane.instances.length === 0) {
      useSessionStore.getState().addEngineInstance(tabId)
    }
  }, [tabId, tabsReady])

  // Dismiss a single toast notification by id. Called by both the X
  // button and each toast's own auto-dismiss timer (see
  // EngineNotificationToasts.tsx for why timers are per-toast rather
  // than one shared head-removal timer).
  const dismissNotification = useCallback((id: string) => {
    useSessionStore.setState(state => {
      const p = state.conversationPanes.get(tabId)
      const k = p?.activeInstanceId ? tabId : ''
      if (!k) return {}
      const notifs = new Map(state.engineNotifications)
      const keyNotifs = notifs.get(k) || []
      if (keyNotifs.length === 0) return {}
      notifs.set(k, keyNotifs.filter(n => n.id !== id))
      return { engineNotifications: notifs }
    })
  }, [tabId])

  const handleAbort = useCallback(() => {
    // Unified data-conditioned interrupt: aborts the run, reaps the agent
    // subtree iff there are running children, cancels bash iff bash is
    // executing, and arms the 5s force-recover. One path for every tab type.
    interrupt(tabId)
  }, [interrupt, tabId])

  // ─── Permission-denied card handlers ───
  //
  // EngineView's variant of the ConversationView's
  // buildPermissionDeniedHandlers. The conversation hook calls
  // `sendMessage` (CLI path); the engine variant calls
  // `submitEnginePrompt` for the active engine instance so the answer
  // is delivered as a new prompt on the same key. Engine tabs do NOT
  // need `resetTabSession` — the engine manages its own session
  // lifecycle; we just disable plan mode and submit a new prompt.
  const clearPermissionDenied = useClearPermissionDenied(key, tabId, activeInstanceId)

  const handleAnswerDenial = useCallback((answer: string) => {
    console.log(`[ConversationView] handleAnswerDenial: tab=${tabId.slice(0, 8)} answerLen=${answer.length}`)
    clearPermissionDenied()
    submit(tabId, answer)
  }, [tabId, clearPermissionDenied, submit])

  const handleImplement = useCallback(async (clearContext: boolean = false) => {
    await runHandleImplement(
      { tabId, clearPermissionDenied, submit, tabPlanFilePath, permissionDenied },
      clearContext,
    )
  }, [tabId, clearPermissionDenied, submit, tabPlanFilePath, permissionDenied])

  const handleImplementAndUnpin = useCallback(async (clearContext: boolean = false) => {
    // Unpin first so the auto-move guard fires when handleImplement
    // switches the tab to auto mode.
    useSessionStore.getState().toggleTabGroupPin(tabId)
    console.log(`[EngineView] implement-and-unpin: tab=${tabId.slice(0, 8)} clearContext=${clearContext} — pin cleared`)
    await handleImplement(clearContext)
  }, [tabId, handleImplement])

  const handleLoadOlder = useCallback(() => {
    setRenderOffset((o) => o + 1)
  }, [])

  // No instances placeholder — all hooks MUST be declared above this point
  // to satisfy React's rules of hooks (constant hook count across renders).
  if (!pane || pane.instances.length === 0) {
    return (
      <div style={{
        display: 'flex', flexDirection: 'column', height: '100%',
        alignItems: 'center', justifyContent: 'center',
        color: colors.textTertiary, fontSize: 13,
      }}>
        Session not started
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', position: 'relative' }}>
      {/* Single-instance-per-tab model (conversation unification #256,
          phase 1). The EngineTabStrip (instance-switcher UI) is removed.
          Each engine tab now has exactly one conversation instance. */}

      {/* Pinned prompt header */}
      {pinnedPrompt && (
        <div
          style={{
            padding: '8px 12px',
            borderBottom: `1px solid ${colors.containerBorder}`,
            fontSize: 13,
            color: colors.textSecondary,
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
          }}
        >
          <span style={{ color: colors.accent, fontWeight: 600 }}>{' > '}</span>
          {pinnedPrompt}
        </div>
      )}

      {/* Scrollable conversation area */}
      <div style={{ flex: agentPanelFullscreen ? 0 : 1, maxHeight: agentPanelFullscreen ? 100 : undefined, position: 'relative', overflow: 'hidden' }}>
        {/* In-conversation search (every tab). */}
        <ConversationSearch
          state={searchState}
          actions={searchActions}
          hiddenCount={hiddenCount}
          onLoadAllOlder={() => setRenderOffset(Math.ceil(totalCount / INITIAL_RENDER_CAP))}
        />
        <div ref={scrollRef} onScroll={handleScroll} style={{ height: '100%', overflowY: 'auto', padding: '8px 12px', ['--ion-conv-font-size' as string]: `${conversationFontSize}px` } as React.CSSProperties}>
          {/* Empty state — fresh conversation with no messages yet. */}
          {messages.length === 0 && !isRunning && <EmptyState />}
          {/* Thinking indicator.
              *
              * Two visual modes, chosen by `showWaitingChildren`:
              *   - foreground (showThinkingForeground): orange
              *     `colors.accent` dot, label "Thinking…"
              *   - background (showWaitingChildren): yellow
              *     `colors.statusWaitingChildren` dot, label
              *     "Waiting for background agents…"
              * The yellow branch matches the footer state-label color
              * and the parent-tab/sub-tab pill dot so the visual
              * vocabulary is consistent across every surface. The dot
              * uses the same `.animate-pulse-dot` animation; only the
              * background color differs.
              */}
          <AnimatePresence>
            {showThinking && (
              <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={{ duration: 0.2 }}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 6,
                  padding: '8px 0',
                  fontSize: 12,
                  color: colors.textTertiary,
                }}
              >
                <span
                  className="animate-pulse-dot"
                  style={{
                    width: 6, height: 6, borderRadius: '50%',
                    background: showWaitingChildren ? colors.statusWaitingChildren : colors.accent, display: 'inline-block',
                  }}
                />
                <span>
                  {showWaitingChildren
                    ? `Waiting for background agent${runningChildCount === 1 ? '' : 's'}…`
                    : 'Thinking...'}
                </span>
              </motion.div>
            )}
          </AnimatePresence>

          {/* Load older messages (pagination) */}
          {hasOlder && (
            <div className="flex justify-center py-2">
              <button
                onClick={handleLoadOlder}
                className="text-[11px] px-3 py-1 rounded-full transition-colors"
                style={{ color: colors.textTertiary, border: `1px solid ${colors.toolBorder}` }}
              >
                Load {Math.min(PAGE_SIZE, hiddenCount)} older messages ({hiddenCount} hidden)
              </button>
            </div>
          )}

          {/* Grouped conversation messages */}
          {grouped.length > 0 && (
            <div style={{ paddingTop: 4 }}>
              {grouped.map((item, idx) => {
                switch (item.kind) {
                  case 'user':
                    return <MessageBubble key={item.message.id} message={item.message} skipMotion actions={<MessageActions message={item.message} variant="user" engineContext={{ tabId, instanceId: activeInstanceId }} />} />
                  case 'assistant':
                    return <AssistantMessage key={item.message.id} message={item.message} skipMotion />
                  case 'tool-group':
                    return <ToolGroup key={`tg-${idx}`} tools={item.messages} skipMotion />
                  case 'agent-turn':
                    return <AgentTurnGroup key={`at-${idx}`} tools={item.tools} assistantMessages={item.assistantMessages} isActive={item.isActive} thinking={item.thinking} skipMotion />
                  case 'thinking':
                    return <ThinkingBlock key={item.message.id} message={item.message} skipMotion />
                  case 'harness':
                    return <HarnessMessage key={item.message.id} message={item.message} skipMotion bootstrapCollapsedCount={item.bootstrapCollapsedCount} />
                  case 'intercept':
                    return <InterceptBanner key={item.message.id} message={item.message} skipMotion />
                  case 'system':
                    return <SystemMessage key={item.message.id} message={item.message} skipMotion />
                  case 'compaction':
                    return <CompactionRow key={item.message.id} message={item.message} skipMotion />
                  default:
                    return null
                }
              })}
            </div>
          )}

          {/* Queued prompts — typed-ahead while a turn runs (every tab). */}
          <AnimatePresence>
            {queuedPrompts.map((prompt: string, i: number) => (
              <QueuedMessage key={`queued-${i}`} content={prompt} onEdit={() => editQueuedMessage(tabId)} />
            ))}
          </AnimatePresence>

          {/* Working message */}
          {workingMessage && (
            <div style={{
              padding: '6px 0', fontSize: 12,
              color: colors.textTertiary, fontStyle: 'italic',
            }}>
              {workingMessage}
            </div>
          )}

          {/* Streaming indicator */}
          {isRunning && hasContent && (
            <div style={{ padding: '4px 0' }}>
              <span
                className="animate-pulse-dot"
                style={{
                  width: 5, height: 5, borderRadius: '50%',
                  background: colors.accent, display: 'inline-block',
                }}
              />
            </div>
          )}

          {/* Dead / failed state rows (every tab). Failed offers a retry that
              re-submits the last user message through the unified submit. */}
          {tabStatus === 'dead' && (
            <div style={{ padding: '6px 0', fontSize: 11, color: colors.statusError }}>
              Session ended unexpectedly
            </div>
          )}
          {tabStatus === 'failed' && (
            <div style={{ padding: '6px 0', display: 'flex', alignItems: 'center', gap: 8 }}>
              <span style={{ color: colors.statusError, fontSize: 11 }}>Failed</span>
              <button
                onClick={handleRetry}
                style={{ color: colors.accent, fontSize: 11, background: 'none', border: 'none', cursor: 'pointer', padding: 0 }}
              >
                Retry
              </button>
            </div>
          )}
        </div>
        {/* Scroll-to-bottom FAB */}
        {showScrollBtn && (
          <button
            onClick={() => {
              if (scrollRef.current) {
                scrollRef.current.scrollTop = scrollRef.current.scrollHeight
                isNearBottomRef.current = true
                setShowScrollBtn(false)
              }
            }}
            style={{
              position: 'absolute',
              bottom: 8,
              left: '50%',
              transform: 'translateX(-50%)',
              zIndex: 3,
              width: 28,
              height: 28,
              borderRadius: '50%',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              background: colors.popoverBg,
              border: `1px solid ${colors.containerBorder}`,
              color: colors.textSecondary,
              cursor: 'pointer',
              boxShadow: '0 2px 8px rgba(0,0,0,0.2)',
            }}
            title="Scroll to bottom"
          >
            <ArrowDown size={14} />
          </button>
        )}

        {/* Interrupt button — visible while the parent run is active OR
            while dispatched children are still running so the user can
            always reap a runaway dispatch even if the parent has died.

            The `messages.length > 0` guard was removed (Defect B): an
            iOS-started slash command on a fresh extension-hosted conversation
            can run BEFORE any message has rendered locally (the desktop renders
            the user bubble from its own optimistic insert or the iOS echo, which
            may land after the run flips to 'running'). Gating the interrupt
            button on a non-empty message list hid the abort affordance for
            exactly that case, so the user could not interrupt the run. Show it
            whenever a run (or a running child) is live, regardless of local
            message count. */}
        <AnimatePresence>
          {(isRunning || hasRunningChildren) && (
            <div style={{
              position: 'absolute',
              bottom: 4, right: 12,
              zIndex: 2,
            }}>
              <InterruptButton onInterrupt={handleAbort} />
            </div>
          )}
        </AnimatePresence>
      </div>

      {/* Permission-denied / AskUserQuestion card.
          Rendered between the scrollable conversation area and the agent
          panel so the question sits directly above the input where users
          expect it. Wired to the engine's submitEnginePrompt so the answer
          becomes a new prompt on the active engine instance.

          Hidden while the tab is running — after the user sends feedback,
          answers a question, or clicks Implement, the tab transitions to
          running/connecting and the card must stay hidden until the agent
          finishes the new turn. Without this, stale heartbeat ticks from
          the engine can re-populate instance.permissionDenied via
          handleEngineStatusEvent before prompt_dispatch clears the
          engine's lastPermissionDenials. */}
      <AnimatePresence>
        {permissionDenied && !isRunning && !suppressPlanCard && (
          <PermissionDeniedCard
            tools={permissionDenied.tools}
            tabId={tabId}
            sessionId={tabConversationId ?? null}
            projectPath={staticInfo?.projectPath || ''}
            messages={messages}
            tabPlanFilePath={tabPlanFilePath}
            tabGroupPinned={tabGroupPinned}
            supportsContextClear={false}
            onDismiss={clearPermissionDenied}
            onAnswer={handleAnswerDenial}
            onImplement={handleImplement}
            onImplementAndUnpin={handleImplementAndUnpin}
          />
        )}
      </AnimatePresence>

      {/* Extension elicitation (ctx.elicit) approval card — see
          ElicitationCardHost. The engine fans engine_elicitation_request to
          every client and parks the run on an indefinite human-wait until one
          answers. */}
      <ElicitationCardHost tabId={tabId} />

      {/* Agent bars */}
      <div style={{ flex: agentPanelFullscreen ? 1 : undefined, overflow: agentPanelFullscreen ? 'auto' : undefined, minHeight: 0 }}>
        <AgentPanel
          agents={agentStates}
          isFullscreen={agentPanelFullscreen}
          onToggleFullscreen={() => setAgentPanelFullscreen(!agentPanelFullscreen)}
          panelHeight={key ? agentPanelHeights.get(key) : undefined}
          onPanelHeightChange={(h) => {
            if (!key) return
            setAgentPanelHeights(prev => { const next = new Map(prev); next.set(key, h); return next })
          }}
        />
      </div>

      {/* Engine status bar removed — its controls have been absorbed
          into the single unified `StatusBar` mounted at the app root.
          See `App.tsx`. */}

      {/* Notification toasts — vertically stacked, individually
          dismissable. See EngineNotificationToasts.tsx. */}
      <EngineNotificationToasts notifications={notifications} onDismiss={dismissNotification} />

      {/* Task list — pinned below the conversation (every tab). */}
      <TodoListPanel messages={messages} isRunning={isRunning} />

      {/* Dialog overlay */}
      <EngineDialog tabId={tabId} />
    </div>
  )
}
