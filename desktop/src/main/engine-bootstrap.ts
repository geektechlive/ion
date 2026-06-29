/**
 * First-launch engine bootstrap.
 *
 * Ensures the Ion Engine launchd daemon is installed and current every time
 * the desktop starts. This single module serves both install routes (source
 * build and DMG package) so they cannot drift.
 *
 * Steps (idempotent):
 *   1. Write/refresh ~/Library/LaunchAgents/com.ion.engine.plist from the
 *      bundled template, substituting $HOME with the real home directory.
 *   2. Copy the bundled engine binary to ~/.ion/bin/ion if missing or
 *      version-mismatched (compare `ion version` output).
 *   3. Run `ion install-assets` to install SDK/ion-meta/canonical docs.
 *   4. `launchctl bootstrap` + `kickstart` the agent.
 *
 * All steps are idempotent. A no-op on Linux/Windows (daemon is macOS-only).
 */

import { execFileSync, execSync } from 'child_process'
import { existsSync, readFileSync, writeFileSync, mkdirSync, copyFileSync, chmodSync } from 'fs'
import { homedir } from 'os'
import { join } from 'path'
import { log as _log } from './logger'

function log(msg: string): void {
  _log('bootstrap', msg)
}

const PLIST_LABEL = 'com.ion.engine'
const PLIST_FILENAME = 'com.ion.engine.plist'

/**
 * Locate the plist template. Checked in order:
 *   1. Packaged .app: Contents/Resources/engine/com.ion.engine.plist
 *   2. Dev monorepo: <repo>/packaging/launchd/com.ion.engine.plist
 */
function findPlistTemplate(): string | null {
  const candidates = [
    process.resourcesPath ? join(process.resourcesPath, 'engine', PLIST_FILENAME) : null,
    join(__dirname, '..', '..', '..', 'packaging', 'launchd', PLIST_FILENAME),
    join(__dirname, '..', '..', '..', '..', 'packaging', 'launchd', PLIST_FILENAME),
  ]
  for (const c of candidates) {
    if (c && existsSync(c)) return c
  }
  return null
}

/**
 * Locate the bundled engine binary. Checked in order:
 *   1. Packaged .app: Contents/Resources/engine/ion
 *   2. Dev monorepo: <repo>/engine/bin/ion
 *   3. Globally installed: ~/.ion/bin/ion (already at destination)
 */
function findBundledBinary(): string | null {
  const candidates = [
    process.resourcesPath ? join(process.resourcesPath, 'engine', 'ion') : null,
    join(__dirname, '..', '..', '..', 'engine', 'bin', 'ion'),
    join(__dirname, '..', '..', '..', '..', 'engine', 'bin', 'ion'),
  ]
  for (const c of candidates) {
    if (c && existsSync(c)) return c
  }
  return null
}

/** Read the output of `ion version` for a given binary path. Returns null on failure. */
function getVersion(binaryPath: string): string | null {
  try {
    return execFileSync(binaryPath, ['version'], { encoding: 'utf-8', timeout: 5000 }).trim()
  } catch (err) {
    log(`getVersion: 'ion version' failed for ${binaryPath}: ${err instanceof Error ? err.message : String(err)}`)
    return null
  }
}

/**
 * Ensure the Ion Engine launchd daemon is installed and current.
 * Called once at desktop startup, before the bridge connects.
 *
 * Exported for testing. In production, call from app-lifecycle.ts.
 */
export async function ensureEngineDaemon(): Promise<void> {
  if (process.platform !== 'darwin') {
    log('Not macOS, skipping launchd daemon bootstrap')
    return
  }

  const home = homedir()
  const uid = process.getuid?.() ?? 501

  // Track whether the plist or the binary actually changed this launch. The
  // force-restart (kickstart -k) is only justified when one of them did — the
  // engine is a persistent launchd daemon that outlives the desktop, so an
  // unconditional -k on every relaunch would force-kill a healthy daemon and
  // any in-flight work for no reason. See the Step-4 gate below.
  let plistChanged = false
  let binaryUpdated = false

  // ── Step 1: Write/refresh plist ────────────────────────────────────────────

  const templatePath = findPlistTemplate()
  if (!templatePath) {
    log('WARNING: plist template not found, skipping plist install')
  } else {
    const template = readFileSync(templatePath, 'utf-8')
    const rendered = template.replace(/\$HOME/g, home)

    const launchAgentsDir = join(home, 'Library', 'LaunchAgents')
    mkdirSync(launchAgentsDir, { recursive: true })
    const plistDest = join(launchAgentsDir, PLIST_FILENAME)

    // Only write if content changed (avoids unnecessary launchd reload)
    let needsWrite = true
    if (existsSync(plistDest)) {
      const existing = readFileSync(plistDest, 'utf-8')
      if (existing === rendered) {
        log('Plist unchanged, skipping write')
        needsWrite = false
      }
    }

    if (needsWrite) {
      writeFileSync(plistDest, rendered, { mode: 0o644 })
      plistChanged = true
      log(`Plist written to ${plistDest}`)
    }
  }

  // ── Step 2: Copy engine binary if missing or version-mismatched ────────────

  const ionBinDir = join(home, '.ion', 'bin')
  const destBinary = join(ionBinDir, 'ion')
  const srcBinary = findBundledBinary()

  if (!srcBinary) {
    log('WARNING: bundled engine binary not found, skipping binary install')
  } else if (srcBinary === destBinary) {
    // Source IS the destination (globally installed binary). Nothing to copy.
    log('Engine binary is already at daemon path, skipping copy')
  } else {
    const srcVersion = getVersion(srcBinary)
    const destVersion = existsSync(destBinary) ? getVersion(destBinary) : null

    if (destVersion && destVersion === srcVersion) {
      log(`Engine binary version match (${destVersion}), skipping copy`)
    } else {
      log(
        `Engine binary ${destVersion ? `version mismatch (${destVersion} -> ${srcVersion})` : 'missing'}` +
        `, copying from ${srcBinary}`,
      )
      mkdirSync(ionBinDir, { recursive: true })
      copyFileSync(srcBinary, destBinary)
      chmodSync(destBinary, 0o755)
      binaryUpdated = true
      log(`Engine binary installed to ${destBinary}`)
    }
  }

  // ── Step 3: Run install-assets ─────────────────────────────────────────────

  if (existsSync(destBinary)) {
    try {
      const output = execFileSync(destBinary, ['install-assets'], {
        encoding: 'utf-8',
        timeout: 30000,
      })
      log(`install-assets: ${output.trim().split('\n').pop() || 'done'}`)
    } catch (err: any) {
      log(`WARNING: install-assets failed (non-fatal): ${err.message}`)
    }
  }

  // ── Step 4: Bootstrap + kickstart the LaunchAgent ──────────────────────────

  const plistDest = join(home, 'Library', 'LaunchAgents', PLIST_FILENAME)
  if (!existsSync(plistDest)) {
    log('WARNING: plist not installed, cannot bootstrap daemon')
    return
  }

  // Bootstrap loads the plist into the launchd namespace. It fails with
  // exit code 5 (or "service already loaded") if already loaded, which is
  // expected on subsequent launches.
  try {
    execSync(`launchctl bootstrap gui/${uid} ${plistDest}`, { timeout: 5000 })
    log('launchctl bootstrap succeeded')
  } catch (err: any) {
    // Exit 5 = "service already loaded" on macOS. Not an error.
    const msg = err.message || ''
    if (msg.includes('already loaded') || msg.includes('service already loaded') || err.status === 5) {
      log('LaunchAgent already loaded (expected on subsequent launches)')
    } else {
      log(`launchctl bootstrap note: ${msg}`)
    }
  }

  // Kickstart ensures the daemon is running. The -k flag force-restarts a
  // running daemon (kill + respawn); plain kickstart starts it only if it is
  // not already running and is a no-op otherwise.
  //
  // Gate the force-restart on an actual change. The engine daemon is
  // persistent and outlives the desktop: a relaunch where neither the binary
  // nor the plist changed must NOT kill a healthy daemon (and its in-flight
  // work). Only force-restart when we installed a new binary or rewrote the
  // plist — that is when the running daemon is genuinely stale. Otherwise use
  // a non-destructive kickstart, which together with RunAtLoad + KeepAlive
  // guarantees the daemon is up (covering the case where a prior graceful quit
  // booted it out) without disturbing a running one.
  const forceRestart = binaryUpdated || plistChanged
  const kickstartCmd = forceRestart
    ? `launchctl kickstart -k gui/${uid}/${PLIST_LABEL}`
    : `launchctl kickstart gui/${uid}/${PLIST_LABEL}`
  try {
    execSync(kickstartCmd, { timeout: 5000 })
    if (forceRestart) {
      log(`launchctl kickstart -k succeeded (force-restart: binaryUpdated=${binaryUpdated} plistChanged=${plistChanged})`)
    } else {
      log('launchctl kickstart succeeded (no change — daemon left running if already up)')
    }
  } catch (err: any) {
    log(`WARNING: launchctl kickstart failed (forceRestart=${forceRestart}): ${err.message}`)
  }
}

// Exported for testing
export { findPlistTemplate, findBundledBinary, getVersion, PLIST_LABEL, PLIST_FILENAME }
