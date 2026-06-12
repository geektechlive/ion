import React from 'react'
import { useColors } from '../theme'
import { useActiveEngineStatusFields } from './StatusBarEngineHelpers'

/**
 * Engine cost slot — renders the running USD cost for the active
 * engine instance in the unified `StatusBar` right cluster. Engine
 * tabs only.
 *
 * Only renders when `totalCostUsd` is defined and > 0. Two decimal
 * places, dollar-sign prefix. Sources from
 * `engineStatusFields[key].totalCostUsd`.
 *
 * Extracted from the former engine status bar (the bottom-of-engine-view
 * slab that was dissolved into the unified `StatusBar`).
 */
export function StatusBarEngineCost() {
  const colors = useColors()
  const status = useActiveEngineStatusFields()

  if (!status) return null
  if (status.totalCostUsd == null || status.totalCostUsd <= 0) return null

  return (
    <span style={{ color: colors.textTertiary, fontSize: 10 }}>
      ${status.totalCostUsd.toFixed(2)}
    </span>
  )
}
