import React, { useState, useRef, useEffect, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { ArrowsOutSimple, ArrowsInSimple, CaretDown, ListChecks, ShieldCheck } from '@phosphor-icons/react'
import { useColors } from '../theme'
import { usePopoverLayer } from './PopoverLayer'
import { useSessionStore } from '../stores/sessionStore'
import { AVAILABLE_MODELS, getModelDisplayLabel } from '../stores/model-labels'
import { useModelStore } from '../stores/model-store'
import { ModelPickerPopover } from './ModelPickerPopover'
import { usePreferencesStore } from '../preferences'
import type { StatusFields } from '../../shared/types'

interface Props {
  status: StatusFields | null
  isTall: boolean
  onToggleTall: () => void
  activeTabId: string
  engineModelOverride?: string
  /** Number of dispatched background agents currently in the `running`
   *  state for this engine instance. When `status.state === 'idle'`
   *  and this is > 0, the footer shows the yellow "awaiting children"
   *  pulse + label instead of the bare `[idle]` text. Passed by the
   *  parent `EngineView` from `agentStates.filter(...).length`. */
  agentRunningCount?: number
}

function renderContextBar(percent: number): string {
  const filled = Math.round(percent / 10)
  const empty = 10 - filled
  return '[' + '#'.repeat(filled) + '.'.repeat(empty) + '] ' + percent + '%'
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  return `${Math.round(n / 1000)}k`
}

export function EngineFooter({ status, isTall, onToggleTall, activeTabId, engineModelOverride, agentRunningCount = 0 }: Props) {
  const colors = useColors()
  const popoverLayer = usePopoverLayer()
  const setEngineModel = useSessionStore((s) => s.setEngineModel)
  const engineDefaultModel = usePreferencesStore((s) => s.engineDefaultModel)
  const permissionMode = useSessionStore((s) => {
    const pane = s.enginePanes.get(activeTabId)
    const instanceId = pane?.activeInstanceId
    if (instanceId) {
      return s.enginePermissionModes.get(`${activeTabId}:${instanceId}`) ?? 'auto'
    }
    return 'auto'
  })
  const setPermissionMode = useSessionStore((s) => s.setPermissionMode)

  const [hover, setHover] = useState(false)
  const barRef = useRef<HTMLSpanElement>(null)
  const [pos, setPos] = useState({ bottom: 0, left: 0 })
  const [showModeConfirm, setShowModeConfirm] = useState(false)

  // Model picker state
  const [modelOpen, setModelOpen] = useState(false)
  const modelTriggerRef = useRef<HTMLSpanElement>(null)
  const modelPopoverRef = useRef<HTMLDivElement>(null)
  const [modelPos, setModelPos] = useState({ bottom: 0, left: 0 })

  const preferredModel = usePreferencesStore((s) => s.preferredModel)
  const selectedModel = engineModelOverride || engineDefaultModel || preferredModel || AVAILABLE_MODELS[0].id
  const selectedLabel = getModelDisplayLabel(selectedModel)

  const isBusy = status?.state === 'running'

  const fetchModels = useModelStore((s) => s.fetchModels)
  const hasModels = useModelStore((s) => s.models.length > 0)
  const lastFetched = useModelStore((s) => s.lastFetched)

  useEffect(() => {
    if (!hasModels) fetchModels()
  }, [hasModels, fetchModels])

  useEffect(() => {
    if (modelOpen && Date.now() - lastFetched > 60_000) fetchModels()
  }, [modelOpen, lastFetched, fetchModels])

  const updateModelPos = useCallback(() => {
    if (!modelTriggerRef.current) return
    const rect = modelTriggerRef.current.getBoundingClientRect()
    setModelPos({ bottom: window.innerHeight - rect.top + 6, left: rect.left })
  }, [])

  useEffect(() => {
    if (!modelOpen) return
    const handler = (e: MouseEvent) => {
      const target = e.target as Node
      if (modelTriggerRef.current?.contains(target)) return
      if (modelPopoverRef.current?.contains(target)) return
      setModelOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [modelOpen])

  const handleModelToggle = () => {
    if (isBusy) return
    if (!modelOpen) updateModelPos()
    setModelOpen((o) => !o)
  }

  const handleBarEnter = () => {
    if (barRef.current) {
      const rect = barRef.current.getBoundingClientRect()
      setPos({ bottom: window.innerHeight - rect.top + 4, left: rect.left + rect.width / 2 })
    }
    setHover(true)
  }

  const pct = status?.contextPercent ?? 0
  const cw = status?.contextWindow ?? 0
  const tokens = cw > 0 ? pct * cw / 100 : 0
  const tooltip = cw > 0
    ? `${formatTokens(tokens)} / ${formatTokens(cw)} tokens`
    : `${pct}% context used`

  // Check if the engine's actual model differs from the user's selection
  const actualModel = status?.model
  const actualLabel = actualModel ? getModelDisplayLabel(actualModel) : null
  const modelDiffers = actualModel && actualLabel !== selectedLabel

  return (
    <div
      data-ion-ui
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        height: 24,
        borderTop: `1px solid ${colors.containerBorder}`,
        padding: '0 12px',
        fontSize: 11,
        flexShrink: 0,
        userSelect: 'none',
      }}
    >
      {/* Left: label, state, team, model picker */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 4, minWidth: 0, overflow: 'hidden' }}>
        {status ? (
          <>
            {status.extensionName && (
              <span style={{ color: colors.accent, fontWeight: 600 }}>{status.extensionName}</span>
            )}
            {/* State label & dot.
              *
              * Three visual states:
              *   - state === 'running'  → orange `statusRunning` pulse + `[running]`
              *   - state === 'idle' AND agentRunningCount > 0 →
              *       yellow `statusWaitingChildren` pulse +
              *       `[waiting for N background agent(s)]`
              *   - everything else (idle with no children, error, etc.) →
              *       no dot, `[{state}]` (existing behaviour)
              *
              * Foreground orange beats background yellow because the
              * orchestrator's own activity is the strongest signal —
              * matches the priority cascade in
              * `TabStripStatusDot.tsx` / `TabStripShared.getTabStatusColor`.
              * The pulse animation reuses `.animate-pulse-dot`, only
              * the background color differs between the two pulsing
              * branches.
              */}
            {(() => {
              const isRun = status.state === 'running'
              const isWaitingChildren = status.state === 'idle' && agentRunningCount > 0
              if (!isRun && !isWaitingChildren) {
                return <span style={{ color: colors.textTertiary }}>[{status.state}]</span>
              }
              const dotColor = isRun ? colors.statusRunning : colors.statusWaitingChildren
              const labelColor = isRun ? colors.statusRunning : colors.statusWaitingChildren
              const label = isRun
                ? 'running'
                : `waiting for ${agentRunningCount} background agent${agentRunningCount === 1 ? '' : 's'}`
              return (
                <span style={{ color: colors.textTertiary, display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                  <span
                    className="animate-pulse-dot"
                    style={{
                      width: 6,
                      height: 6,
                      borderRadius: '50%',
                      background: dotColor,
                      display: 'inline-block',
                      flexShrink: 0,
                    }}
                  />
                  <span style={{ color: labelColor }}>[{label}]</span>
                </span>
              )
            })()}
            {status.team && <>
              <span style={{ color: colors.textTertiary }}>|</span>
              <span style={{ color: colors.textSecondary }}>{status.team}</span>
            </>}
            <span style={{ color: colors.textTertiary }}>|</span>
            <span
              ref={modelTriggerRef}
              role="button"
              onClick={handleModelToggle}
              style={{
                cursor: isBusy ? 'not-allowed' : 'pointer',
                color: colors.textSecondary,
                display: 'flex',
                alignItems: 'center',
                gap: 2,
              }}
              title={isBusy ? 'Stop the task to change model' : 'Switch model'}
            >
              {selectedLabel}
              {modelDiffers && (
                <span style={{ color: colors.textTertiary, fontSize: 10 }}>({actualLabel})</span>
              )}
              <CaretDown size={9} style={{ opacity: 0.6 }} />
            </span>
            {status.backend !== 'api' && <>
              <span style={{ color: colors.textTertiary }}>|</span>
              <span style={{ color: '#e5a100', fontSize: 10, fontWeight: 500 }}>via CLI</span>
            </>}
            <span style={{ color: colors.textTertiary }}>|</span>
            <span
              role="button"
              onClick={() => setShowModeConfirm(true)}
              style={{
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                gap: 2,
                color: permissionMode === 'plan' ? '#2eb8a6' : colors.textTertiary,
              }}
              title="Permission mode — extensions control this; click to override"
            >
              {permissionMode === 'plan'
                ? <ListChecks size={11} weight="bold" />
                : <ShieldCheck size={11} weight="fill" />}
              {permissionMode === 'plan' ? 'Plan' : 'Auto'}
            </span>
          </>
        ) : (
          <span style={{ color: colors.textTertiary }}>--</span>
        )}
      </div>

      {/* Right: context bar, cost, toggle */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
        {status && (
          <>
            <span
              ref={barRef}
              style={{ fontFamily: 'monospace', fontSize: 10, color: colors.textTertiary, cursor: 'default' }}
              onMouseEnter={handleBarEnter}
              onMouseLeave={() => setHover(false)}
            >
              {renderContextBar(status.contextPercent)}
            </span>
            {popoverLayer && hover && createPortal(
              <div
                style={{
                  position: 'fixed',
                  bottom: pos.bottom,
                  left: pos.left,
                  transform: 'translateX(-50%)',
                  pointerEvents: 'none',
                  background: colors.popoverBg,
                  backdropFilter: 'blur(12px)',
                  WebkitBackdropFilter: 'blur(12px)',
                  border: `1px solid ${colors.popoverBorder}`,
                  borderRadius: 6,
                  padding: '3px 8px',
                  fontSize: 10,
                  color: colors.textSecondary,
                  whiteSpace: 'nowrap',
                  boxShadow: colors.popoverShadow,
                }}
              >
                {tooltip}
              </div>,
              popoverLayer,
            )}
            {status.totalCostUsd != null && status.totalCostUsd > 0 && (
              <span style={{ color: colors.textTertiary, fontSize: 10 }}>
                ${status.totalCostUsd.toFixed(2)}
              </span>
            )}
          </>
        )}
        <button
          data-ion-ui
          onClick={onToggleTall}
          title={isTall ? 'Collapse view' : 'Expand view'}
          style={{
            background: 'none',
            border: 'none',
            padding: 0,
            cursor: 'pointer',
            color: colors.textTertiary,
            display: 'flex',
            alignItems: 'center',
          }}
        >
          {isTall ? <ArrowsInSimple size={12} /> : <ArrowsOutSimple size={12} />}
        </button>
      </div>

      {/* Model picker popover */}
      {popoverLayer && modelOpen && createPortal(
        <ModelPickerPopover
          popoverRef={modelPopoverRef}
          selectedModelId={selectedModel}
          onSelect={(modelId) => { setEngineModel(activeTabId, modelId); setModelOpen(false) }}
          onClose={() => setModelOpen(false)}
          position={modelPos}
        />,
        popoverLayer,
      )}

      {/* Permission mode override confirmation */}
      {popoverLayer && showModeConfirm && createPortal(
        <div
          data-ion-ui
          style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(0,0,0,0.4)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 10000,
            pointerEvents: 'auto',
          }}
          onClick={() => setShowModeConfirm(false)}
        >
          <div
            data-ion-ui
            onClick={(e) => e.stopPropagation()}
            style={{
              background: colors.popoverBg,
              backdropFilter: 'blur(20px)',
              WebkitBackdropFilter: 'blur(20px)',
              border: `1px solid ${colors.popoverBorder}`,
              borderRadius: 12,
              padding: '16px 20px',
              maxWidth: 340,
              boxShadow: colors.popoverShadow,
            }}
          >
            <div style={{ fontSize: 13, fontWeight: 600, color: colors.textPrimary, marginBottom: 8 }}>
              Change Mode
            </div>
            <div style={{ fontSize: 12, color: colors.textSecondary, lineHeight: 1.5, marginBottom: 16 }}>
              The extension controls this tab&apos;s planning mode. Changing it manually may interfere with the extension&apos;s workflow.
            </div>
            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              <button
                data-ion-ui
                onClick={() => setShowModeConfirm(false)}
                style={{
                  background: 'none',
                  border: `1px solid ${colors.containerBorder}`,
                  borderRadius: 6,
                  padding: '5px 14px',
                  fontSize: 12,
                  color: colors.textSecondary,
                  cursor: 'pointer',
                }}
              >
                Cancel
              </button>
              <button
                data-ion-ui
                onClick={() => {
                  setPermissionMode(permissionMode === 'plan' ? 'auto' : 'plan', 'ui_dropdown')
                  setShowModeConfirm(false)
                }}
                style={{
                  background: colors.accent,
                  border: 'none',
                  borderRadius: 6,
                  padding: '5px 14px',
                  fontSize: 12,
                  color: '#fff',
                  fontWeight: 600,
                  cursor: 'pointer',
                }}
              >
                Switch to {permissionMode === 'plan' ? 'Auto' : 'Plan'}
              </button>
            </div>
          </div>
        </div>,
        popoverLayer,
      )}
    </div>
  )
}
