import { useEffect } from 'react'
import { motion } from 'framer-motion'
import { createPortal } from 'react-dom'
import { usePopoverLayer } from './PopoverLayer'
import { useColors } from '../theme'

const TRANSITION = { duration: 0.26, ease: [0.4, 0, 0.1, 1] as const }

/**
 * Modal confirmation for closing an engine instance (sub-tab) inside an
 * engine view's status bar. Structurally mirrors CloseTabConfirmDialog —
 * portalled into the PopoverLayer so the buttons land in the viewport
 * center, far from the small X icon the user just clicked. Per
 * docs/plans/quiet-greeting-deer.md, an inline Yes/No next to the X is
 * a footgun: a double-tap or stray click can confirm the destructive
 * action before the user sees it. The modal forces the user to move
 * their pointer to a different region of the screen.
 *
 * When `isLastInstance` is true the dialog surfaces a stronger warning
 * because removing the last engine instance also tears down the parent
 * engine tab (see engine-slice.ts → removeEngineInstance, "if
 * remaining.length === 0 → closeTab(tabId)").
 */
export function EngineInstanceCloseConfirmDialog({
  instanceLabel,
  tabTitle,
  isLastInstance,
  onConfirm,
  onCancel,
}: {
  instanceLabel: string
  tabTitle: string
  isLastInstance: boolean
  onConfirm: () => void
  onCancel: () => void
}) {
  const colors = useColors()
  const popoverLayer = usePopoverLayer()

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel()
      if (e.key === 'Enter') onConfirm()
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [onCancel, onConfirm])

  if (!popoverLayer) return null

  return createPortal(
    <motion.div
      data-ion-ui
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.15 }}
      onClick={onCancel}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0, 0, 0, 0.4)',
        pointerEvents: 'auto',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <motion.div
        data-ion-ui
        initial={{ opacity: 0, scale: 0.96 }}
        animate={{ opacity: 1, scale: 1 }}
        exit={{ opacity: 0, scale: 0.96 }}
        transition={TRANSITION}
        onClick={(e) => e.stopPropagation()}
        className="glass-surface"
        style={{
          width: 340,
          borderRadius: 16,
          padding: 20,
          display: 'flex',
          flexDirection: 'column',
          gap: 12,
        }}
      >
        <div style={{ fontSize: 13, fontWeight: 600, color: colors.textPrimary }}>
          Close engine instance?
        </div>
        <div style={{ fontSize: 11, color: colors.textSecondary, lineHeight: 1.5 }}>
          <div style={{ fontWeight: 500 }}>{instanceLabel}</div>
          <div style={{ color: colors.textTertiary, marginTop: 2 }}>in {tabTitle}</div>
        </div>
        {isLastInstance && (
          <div
            style={{
              fontSize: 11,
              lineHeight: 1.5,
              color: colors.textPrimary,
              background: `${colors.accent}18`,
              border: `1px solid ${colors.accent}40`,
              borderRadius: 8,
              padding: '8px 10px',
            }}
          >
            This is the last instance — closing it will close the entire engine tab.
          </div>
        )}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 4 }}>
          <button
            onClick={onCancel}
            className="px-3 py-1 rounded-lg text-[11px]"
            style={{
              color: colors.textSecondary,
              background: colors.surfacePrimary,
              border: `1px solid ${colors.containerBorder}`,
              cursor: 'pointer',
            }}
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            className="px-3 py-1 rounded-lg text-[11px]"
            style={{
              color: '#fff',
              background: colors.accent,
              border: 'none',
              cursor: 'pointer',
            }}
          >
            Close
          </button>
        </div>
      </motion.div>
    </motion.div>,
    popoverLayer,
  )
}
