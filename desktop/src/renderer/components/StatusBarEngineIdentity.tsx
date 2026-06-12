import React from 'react'
import { useColors } from '../theme'
import { useActiveEngineStatusFields } from './StatusBarEngineHelpers'

/**
 * Engine identity slot — renders the active engine's extension name
 * (accent-colored, bold) and team (secondary text) in the unified
 * `StatusBar` left cluster. Engine tabs only.
 *
 * Either field may be absent. We render exactly what the engine emits
 * via `engine_status.extensionName` / `engine_status.team` — no
 * synthesis, no fallback strings. If both are absent we render
 * nothing.
 *
 * Extracted from the former engine status bar (the bottom-of-engine-view
 * slab that was dissolved into the unified `StatusBar`).
 */
export function StatusBarEngineIdentity() {
  const colors = useColors()
  const status = useActiveEngineStatusFields()

  if (!status) return null
  if (!status.extensionName && !status.team) return null

  return (
    <>
      {status.extensionName && (
        <span style={{ color: colors.accent, fontWeight: 600, fontSize: 10 }}>{status.extensionName}</span>
      )}
      {status.team && (
        <span style={{ color: colors.textSecondary, fontSize: 10 }}>{status.team}</span>
      )}
    </>
  )
}
