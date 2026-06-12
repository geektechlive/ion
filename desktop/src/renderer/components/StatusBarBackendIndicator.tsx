import React from 'react'
import { useSessionStore } from '../stores/sessionStore'
import { useActiveEngineStatusFields } from './StatusBarEngineHelpers'

/**
 * Backend indicator — renders the small "CLI" or "via CLI" badge in
 * the unified `StatusBar` left cluster when the active session is
 * routed through the CLI bridge proxy instead of speaking the API
 * directly.
 *
 * Two state sources, depending on tab type:
 *
 * - **Conversation tabs**: reads global `s.backend`. When the user
 *   has flipped the whole desktop into CLI bridge mode, this is
 *   `'cli'` and the badge shows as plain "CLI".
 *
 * - **Engine tabs**: reads `engineStatusFields[key].backend` from the
 *   per-instance `engine_status` snapshot. Each engine instance can
 *   choose its own backend, so the badge surfaces the active
 *   instance's signal as "via CLI" (matches the prior rendering in
 *   the former engine status bar). When the engine reports `'api'`
 *   or omits the field, no badge.
 *
 * Both signals can be true simultaneously (global CLI mode + engine
 * also on CLI). The engine-tab branch wins display in that case
 * because it's the more specific signal, but the engine branch is
 * only consulted when the active tab is an engine tab.
 */
export function BackendIndicator() {
  const backend = useSessionStore((s) => s.backend)
  const engineStatus = useActiveEngineStatusFields()

  // Engine tab: surface per-instance backend signal as "via CLI".
  if (engineStatus) {
    if (engineStatus.backend !== 'api' && engineStatus.backend != null) {
      return (
        <span style={{ color: '#e5a100', fontSize: 10, fontWeight: 500 }}>via CLI</span>
      )
    }
    // Engine tab but instance is on API — still fall through to the
    // global signal in case the desktop itself is in CLI bridge mode
    // (e.g. while debugging engine routing). Highly unusual; harmless.
  }

  // Conversation tab (or engine on API with global CLI mode):
  // global desktop backend.
  if (backend === 'cli') {
    return (
      <span style={{ color: '#e5a100', fontSize: 10, fontWeight: 500 }}>CLI</span>
    )
  }

  return null
}
