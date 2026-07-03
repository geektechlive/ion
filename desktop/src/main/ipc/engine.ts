import { ipcMain } from 'electron'
import { IPC } from '../../shared/types'
import { buildClearDividerRemoteEvent } from '../../shared/clear-divider'
import { log as _log } from '../logger'
import { isValidProjectPath } from '../ipc-validation'
import { engineBridge, sessionPlane, state } from '../state'
import { broadcastEngineHistory } from '../remote/handlers/engine-history'

function log(msg: string): void {
  _log('main', msg)
}

/**
 * Validate a renderer/iOS-supplied planFilePath before forwarding it to the
 * engine. planFilePath is an absolute instruction-file path; an invalid value
 * degrades to "no restore" (undefined) rather than aborting the plan-mode
 * toggle — enabling plan mode without a restore file is still the correct
 * outcome. Returns the validated path, or undefined when absent or malformed.
 */
export function sanitizePlanFilePath(planFilePath: string | undefined, channel: string): string | undefined {
  if (!planFilePath) return undefined
  if (!isValidProjectPath(planFilePath)) {
    log(`IPC ${channel}: rejecting malformed planFilePath (degrading to no-restore)`)
    return undefined
  }
  return planFilePath
}

export function registerEngineIpc(): void {
  ipcMain.handle(IPC.ENGINE_START, async (_event, { key, config }: { key: string; config: import('../../shared/types').EngineConfig }) => {
    log(`IPC ENGINE_START: key=${key} ext=${config.extensions?.join(',')} sessionId=${config.sessionId ?? 'none'}`)
    // Seed the control-plane TabEntry with the resolved conversationId BEFORE the
    // engine session starts. This IPC starts the session via engineBridge
    // directly (bypassing EngineControlPlane.ensureSession, which is the only
    // other start site that seeds conversationId). Without this seed, an
    // extension-hosted restored tab has no tracked id when the engine emits its
    // first idle status, so the engine_status first-bind branch adopts whatever
    // id the engine reports — including an empty pre-minted id on a restore that
    // supplied none. Seeding here arms the divergence guard. Idempotent: a no-op
    // when the tab already tracks an id.
    if (config.sessionId) {
      sessionPlane.seedConversationId(key, config.sessionId)
    }
    return engineBridge.startSession(key, config)
  })

  ipcMain.handle(IPC.ENGINE_ABORT, (_event, { key }: { key: string }) => {
    log(`IPC ENGINE_ABORT: key=${key}`)
    engineBridge.sendAbort(key)
  })

  ipcMain.handle(
    IPC.ENGINE_ABORT_AGENT,
    (_event, { key, agentName, subtree }: { key: string; agentName: string; subtree?: boolean }) => {
      log(`IPC ENGINE_ABORT_AGENT: key=${key} agent=${agentName} subtree=${subtree ?? false}`)
      engineBridge.sendAbortAgent(key, agentName, subtree ?? false)
    },
  )

  ipcMain.handle(IPC.ENGINE_DIALOG_RESPONSE, (_event, { key, dialogId, value }: { key: string; dialogId: string; value: any }) => {
    log(`IPC ENGINE_DIALOG_RESPONSE: key=${key} dialog=${dialogId}`)
    engineBridge.sendDialogResponse(key, dialogId, value)
  })

  ipcMain.handle(IPC.ENGINE_COMMAND, (_event, { key, command, args }: { key: string; command: string; args: string }) => {
    log(`IPC ENGINE_COMMAND: key=${key} cmd=/${command}`)
    engineBridge.sendCommand(key, command, args)
    // Mirror /clear divider to iOS so the remote client sees the checkpoint
    // immediately, without waiting for a conversation reload. The renderer
    // has already inserted the divider into its local message store via
    // addSystemMessage / addEngineSystemMessage; here we relay it to iOS.
    // The envelope kind (engine_harness_message vs. message_added) is keyed
    // by the engine session key shape — see buildClearDividerRemoteEvent.
    if (command === 'clear' && state.remoteTransport) {
      state.remoteTransport.send(buildClearDividerRemoteEvent(key, new Date()))
    }
  })

  ipcMain.handle(IPC.ENGINE_STOP, (_event, { key }: { key: string }) => {
    log(`IPC ENGINE_STOP: key=${key}`)
    engineBridge.stopSession(key)
  })

  ipcMain.handle(IPC.ENGINE_GET_CONTEXT_BREAKDOWN, (_event, { key }: { key: string }) => {
    log(`IPC ENGINE_GET_CONTEXT_BREAKDOWN: key=${key}`)
    // Fire-and-forget. The engine emits engine_context_breakdown on its event
    // bus; the existing event-wiring handler translates it to context_breakdown
    // and broadcasts to the renderer. The IPC reply is empty — the caller
    // observes the result through the engine event stream.
    engineBridge._send({ cmd: 'get_context_breakdown', key })
  })

  ipcMain.handle(IPC.ENGINE_REMAP_SESSION, (_event, { oldKey, newKey }: { oldKey: string; newKey: string }) => {
    log(`IPC ENGINE_REMAP_SESSION: ${oldKey} -> ${newKey}`)
    engineBridge.remapSession(oldKey, newKey)
  })

  ipcMain.handle(IPC.ENGINE_BROADCAST_HISTORY, async (_event, { tabId, instanceId }: { tabId: string; instanceId: string | null }) => {
    log(`IPC ENGINE_BROADCAST_HISTORY: tabId=${tabId} instanceId=${instanceId || 'null'}`)
    await broadcastEngineHistory(tabId, instanceId)
  })

  ipcMain.on(IPC.SET_PERMISSION_MODE, (_event, payload: { tabId: string; mode: string; source?: string; planFilePath?: string }) => {
    const { tabId, mode, source, planFilePath } = payload
    if (mode !== 'auto' && mode !== 'plan') {
      log(`IPC SET_PERMISSION_MODE: invalid mode "${mode}" — ignoring`)
      return
    }
    const safePlanFilePath = sanitizePlanFilePath(planFilePath, 'SET_PERMISSION_MODE')
    log(`IPC SET_PERMISSION_MODE: tab=${tabId} mode=${mode} source=${source ?? 'unknown'} planFilePath=${safePlanFilePath ?? '<none>'}`)
    sessionPlane.setPermissionMode(tabId, mode, source, safePlanFilePath)
  })

  ipcMain.on('ion:engine-set-plan-mode', (_event, key: string, enabled: boolean, planFilePath?: string) => {
    const safePlanFilePath = sanitizePlanFilePath(planFilePath, 'engine-set-plan-mode')
    log(`IPC engine-set-plan-mode: key=${key} enabled=${enabled} planFilePath=${safePlanFilePath ?? '<none>'}`)
    // planFilePath restores plan-file continuity when enabling plan mode on a
    // session that lost its in-memory path (e.g. after restart / rebind). The
    // engine re-adopts it if it exists on disk; ignored on disable. Forwarded
    // as the 6th sendSetPlanMode arg (bash allowlist stays undefined here —
    // the extension-instance plan toggle does not project the allowlist).
    engineBridge.sendSetPlanMode(key, enabled, undefined, 'prompt_sync', undefined, safePlanFilePath)
  })
}
