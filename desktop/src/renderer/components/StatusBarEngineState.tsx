import React from 'react'
import { useColors } from '../theme'
import { useActiveEngineStatusFields, useActiveEngineAgentRunningCount } from './StatusBarEngineHelpers'

/**
 * Engine state slot — renders the orchestrator state dot + label in
 * the unified `StatusBar` left cluster. Engine tabs only.
 *
 * Three visual states (priority order):
 *   - state === 'running'  → orange `statusRunning` pulse + `[running]`
 *   - state === 'idle' AND agentRunningCount > 0 →
 *       yellow `statusWaitingChildren` pulse +
 *       `[waiting for N background agent(s)]`
 *   - everything else (idle with no children, error, etc.) →
 *       no dot, `[{state}]` (existing behaviour)
 *
 * Foreground orange beats background yellow because the orchestrator's
 * own activity is the strongest signal — matches the priority cascade
 * in `TabStripStatusDot.tsx` / `TabStripShared.getTabStatusColor`. The
 * pulse animation reuses `.animate-pulse-dot`, only the background
 * color differs between the two pulsing branches.
 *
 * Extracted verbatim from the former engine status bar (the
 * bottom-of-engine-view slab that was dissolved into the unified
 * `StatusBar`).
 */
export function StatusBarEngineState() {
  const colors = useColors()
  const status = useActiveEngineStatusFields()
  const agentRunningCount = useActiveEngineAgentRunningCount()

  if (!status) return null

  const isRun = status.state === 'running'
  const isWaitingChildren = status.state === 'idle' && agentRunningCount > 0

  if (!isRun && !isWaitingChildren) {
    return <span style={{ color: colors.textTertiary, fontSize: 10 }}>[{status.state}]</span>
  }

  const dotColor = isRun ? colors.statusRunning : colors.statusWaitingChildren
  const labelColor = isRun ? colors.statusRunning : colors.statusWaitingChildren
  const label = isRun
    ? 'running'
    : `waiting for ${agentRunningCount} background agent${agentRunningCount === 1 ? '' : 's'}`

  return (
    <span style={{ color: colors.textTertiary, display: 'inline-flex', alignItems: 'center', gap: 4, fontSize: 10 }}>
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
}
