/**
 * engine-event-slice-helpers — shared instance-write primitives
 *
 * Extracted from engine-event-slice.ts to keep that file under the
 * 600-line TypeScript cap. Contains:
 *
 *   - The renderer-side extension command registry (module-level Map +
 *     getRendererExtensionCommands export). Autocomplete reads this
 *     synchronously on keystroke; no reactive subscription needed.
 *
 *   - withInstanceMessages / withInstanceAgentStates — pure Map
 *     transformers that return a new enginePanes Map with the specified
 *     ConversationInstance field updated. Returning the original Map
 *     unchanged on a miss avoids spurious re-renders on the hot event
 *     path.
 */

import type { EnginePaneState, AgentStateUpdate } from '../../../shared/types-engine'
import type { Message } from '../../../shared/types-session'

/**
 * Per-tab cache of extension-registered command names, populated by
 * engine_command_registry snapshots emitted from the Go engine. Used by
 * the slash-autocomplete UI in InputBar so extension commands appear in
 * the menu alongside filesystem `.md` discoveries. Keyed by engine session
 * key (tabId or `${tabId}:${instanceId}`) — autocomplete reads under the
 * active tab/instance combination.
 *
 * Snapshot semantics: every event REPLACES the prior set. An empty
 * `commands: []` is the authoritative "no extension commands" signal and
 * clears the entry. Mirrors the main-process cache in
 * `desktop/src/main/state.ts:extensionCommandRegistry`.
 */
export const extensionCommandsByKey = new Map<string, Array<{ name: string; description?: string }>>()

/** Get a snapshot of the current extension commands for an engine session key.
 *  Used by autocomplete; returns an empty array when no commands are cached. */
export function getRendererExtensionCommands(key: string): Array<{ name: string; description?: string }> {
  return extensionCommandsByKey.get(key) ?? []
}

// ─── Instance-write helpers ───────────────────────────────────────────────────
//
// Each helper returns a new enginePanes Map with the specified ConversationInstance
// field updated on the instance identified by `key` (`${tabId}:${instanceId}`).
// Returns the original Map unchanged when the pane or instance is not found,
// so callers can do `if (updated !== original) returnPatch.enginePanes = updated`
// without allocating on the hot path.

export function withInstanceMessages(
  enginePanes: Map<string, EnginePaneState>,
  key: string,
  messages: Message[],
): Map<string, EnginePaneState> {
  const [tabId, instanceId] = key.split(':')
  const pane = enginePanes.get(tabId)
  if (!pane) return enginePanes
  const idx = pane.instances.findIndex((i) => i.id === instanceId)
  if (idx === -1) return enginePanes
  const updated = new Map(enginePanes)
  const instances = pane.instances.slice()
  instances[idx] = { ...instances[idx], messages }
  updated.set(tabId, { ...pane, instances })
  return updated
}

export function withInstanceAgentStates(
  enginePanes: Map<string, EnginePaneState>,
  key: string,
  agentStates: AgentStateUpdate[],
): Map<string, EnginePaneState> {
  const [tabId, instanceId] = key.split(':')
  const pane = enginePanes.get(tabId)
  if (!pane) return enginePanes
  const idx = pane.instances.findIndex((i) => i.id === instanceId)
  if (idx === -1) return enginePanes
  const updated = new Map(enginePanes)
  const instances = pane.instances.slice()
  instances[idx] = { ...instances[idx], agentStates }
  updated.set(tabId, { ...pane, instances })
  return updated
}
