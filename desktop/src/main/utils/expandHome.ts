import { homedir } from 'node:os'
import { join } from 'node:path'

/**
 * Expand a leading `~` to the current user's home directory.
 *
 * `~/foo` → `<home>/foo`
 * `~`     → `<home>`
 *
 * Absolute paths and relative paths without a leading `~` are returned
 * unchanged.
 */
export function expandHome(p: string): string {
  if (p === '~' || p.startsWith('~/')) {
    return join(homedir(), p.slice(1))
  }
  return p
}
