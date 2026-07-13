import type { EngineBridge } from './engine-bridge'

/**
 * Resolve once the bridge reconnects, or after `timeoutMs` elapses.
 * Used by engine-bridge-fs.ts `probeWorkingDir` to wait out the reconnect
 * window before retrying a probe that failed with `transport: true` — the
 * M2 engine restarts on every deploy, closing the socket mid-session, and a
 * retry after reconnect distinguishes "the engine host is temporarily
 * unreachable" from "the directory genuinely doesn't exist".
 *
 * Extracted from engine-bridge.ts to stay under the file-size cap. See
 * engine-bridge-lifecycle.ts for the same `bridge: EngineBridge`-first
 * extraction convention.
 */
export function whenConnected(bridge: EngineBridge, timeoutMs: number): Promise<boolean> {
  if (bridge.connected) return Promise.resolve(true)
  return new Promise((resolve) => {
    const onReconnect = (): void => {
      clearTimeout(timer)
      resolve(true)
    }
    const timer = setTimeout(() => {
      bridge.removeListener('reconnected', onReconnect)
      resolve(false)
    }, timeoutMs)
    bridge.once('reconnected', onReconnect)
  })
}
