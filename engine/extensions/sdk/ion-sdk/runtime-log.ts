// Ion Extension SDK -- internal log emitter.
//
// Extracted so the agent-registration helper (runtime-agents.ts) can log
// without creating an import cycle with runtime.ts (which itself imports
// runtime-agents). The public `log` API re-exported from runtime.ts wraps
// this module's `emitLog` 1:1.
//
// All output goes through the engine's JSON-RPC `log` notification and
// lands in `~/.ion/engine.log` tagged with the extension name. Raw stdout
// writes would corrupt the JSON-RPC frame stream, so extension code MUST
// route through this helper rather than `console.log`.

export type LogLevel = 'debug' | 'info' | 'warn' | 'error'

/** Write a single JSON-RPC `log` notification to stdout. */
export function emitLog(level: LogLevel, message: string, fields?: Record<string, unknown>): void {
  process.stdout.write(
    JSON.stringify({
      jsonrpc: '2.0',
      method: 'log',
      params: { level, message, fields: fields ?? {} },
    }) + '\n',
  )
}

/**
 * Convenience wrapper used by internal SDK modules. The public-facing `log`
 * object exported from runtime.ts has the same shape; both share this
 * underlying emitter so the wire format stays in lock-step.
 */
export const log = {
  debug: (message: string, fields?: Record<string, unknown>) => emitLog('debug', message, fields),
  info: (message: string, fields?: Record<string, unknown>) => emitLog('info', message, fields),
  warn: (message: string, fields?: Record<string, unknown>) => emitLog('warn', message, fields),
  error: (message: string, fields?: Record<string, unknown>) => emitLog('error', message, fields),
}
