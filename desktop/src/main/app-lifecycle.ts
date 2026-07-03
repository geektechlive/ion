import { app, BrowserWindow, globalShortcut, Menu, screen } from 'electron'
import { rmSync, writeFileSync } from 'fs'
import { join } from 'path'
import { log as _log, flushLogs } from './logger'
import { state, SPACES_DEBUG, sessionPlane, engineBridge, fileWatchers, bashProcesses } from './state'
import { terminalManager } from './terminal-manager-instance'
import { stopTabSnapshotPolling } from './remote/snapshot-polling'
import { createTray, createWindow, installContentSecurityPolicy, snapshotWindowState, showWindow, toggleWindow } from './window-manager'
import { requestPermissions } from './permissions-preflight'
import { cleanOrphanedWorktrees } from './git-runner'
import { focusState } from './git/focus-state'
import { startConversationCleanup } from './conversation-cleanup'
import { tabsFileForBackend, sessionChainsFileForBackend, sessionLabelsFileForBackend } from './settings-store'
import { ensureEngineDaemon } from './engine-bootstrap'

function log(msg: string): void {
  _log('main', msg)
}

/**
 * Force the renderer to flush any pending debounced tab persistence.
 * The Zustand store debounces persistTabs() at 100ms — if we call
 * app.exit(0) before the timer fires, the latest tab state (including
 * conversationId, titles, etc.) is lost. This mirrors the pattern used
 * by SWITCH_BACKEND in ipc/settings.ts.
 */
async function flushRendererTabs(): Promise<void> {
  for (const win of BrowserWindow.getAllWindows()) {
    try {
      await win.webContents.executeJavaScript(
        'window.__ionForceFlushTabs && window.__ionForceFlushTabs()',
      )
    } catch {
      // Window may already be destroyed or renderer unresponsive — safe to skip.
    }
  }
}

export function setupAppLifecycle(): void {
  app.whenReady().then(async () => {
    if (process.platform === 'darwin' && app.dock) {
      app.dock.hide()
    }

    await requestPermissions()

    // Ensure the engine daemon is installed, current, and running before
    // creating the window. The bootstrap is idempotent: writes/refreshes the
    // LaunchAgent plist, copies the binary if version-mismatched, runs
    // install-assets, and kickstarts the daemon. On non-macOS this is a no-op.
    await ensureEngineDaemon()

    // Connect to the engine daemon. The bridge retries with backoff if the
    // daemon is still starting after a fresh kickstart.
    try {
      await engineBridge.connect()
    } catch (err: any) {
      log(`Engine connect failed (will retry on first IPC): ${err.message}`)
    }

    installContentSecurityPolicy()

    cleanOrphanedWorktrees().catch((err: Error) => log(`Worktree cleanup failed: ${err.message}`))

    createWindow()
    snapshotWindowState('after createWindow')

    const pidDir = app.getPath('userData')
    const pidPath = join(pidDir, 'ion.pid')
    writeFileSync(pidPath, String(process.pid))
    log(`PID file written: ${pidPath} (${process.pid})`)

    Menu.setApplicationMenu(Menu.buildFromTemplate([
      {
        label: app.name,
        submenu: [
          { role: 'about' },
          { type: 'separator' },
          { role: 'hide' },
          { role: 'hideOthers' },
          { role: 'unhide' },
          { type: 'separator' },
          { role: 'quit' },
        ],
      },
      {
        label: 'Edit',
        submenu: [
          { role: 'undo' },
          { role: 'redo' },
          { type: 'separator' },
          { role: 'cut' },
          { role: 'copy' },
          { role: 'paste' },
          { role: 'selectAll' },
        ],
      },
    ]))

    app.on('browser-window-focus', () => focusState.setFocused(true))
    app.on('browser-window-blur', () => {
      focusState.setFocused(BrowserWindow.getAllWindows().some((w) => w.isFocused()))
    })

    if (SPACES_DEBUG) {
      state.mainWindow?.on('show', () => snapshotWindowState('event window show'))
      state.mainWindow?.on('hide', () => snapshotWindowState('event window hide'))
      state.mainWindow?.on('focus', () => snapshotWindowState('event window focus'))
      state.mainWindow?.on('blur', () => snapshotWindowState('event window blur'))
      state.mainWindow?.webContents.on('focus', () => snapshotWindowState('event webContents focus'))
      state.mainWindow?.webContents.on('blur', () => snapshotWindowState('event webContents blur'))

      app.on('browser-window-focus', () => snapshotWindowState('event app browser-window-focus'))
      app.on('browser-window-blur', () => snapshotWindowState('event app browser-window-blur'))

      screen.on('display-added', (_e, display) => {
        log(`[spaces] event display-added id=${display.id}`)
        snapshotWindowState('event display-added')
      })
      screen.on('display-removed', (_e, display) => {
        log(`[spaces] event display-removed id=${display.id}`)
        snapshotWindowState('event display-removed')
      })
      screen.on('display-metrics-changed', (_e, display, changedMetrics) => {
        log(`[spaces] event display-metrics-changed id=${display.id} changed=${changedMetrics.join(',')}`)
        snapshotWindowState('event display-metrics-changed')
      })
    }

    const registered = globalShortcut.register('Alt+Space', () => toggleWindow('shortcut Alt+Space'))
    if (!registered) {
      log('Alt+Space shortcut registration failed — macOS input sources may claim it')
    }
    globalShortcut.register('CommandOrControl+Shift+K', () => toggleWindow('shortcut Cmd/Ctrl+Shift+K'))

    createTray()

    // Background conversation cleanup (dry-run by default).
    //
    // We pass explicit per-backend file paths instead of deriving them
    // inside a closure. The previous version did `require('./settings-store')`
    // lazily inside the callback and silently returned `[]` on any error,
    // which on June 7 caused the desktop to send `excludeIds=[]` to the
    // engine. With DRY_RUN=true that was harmless; with DRY_RUN=false it
    // would have deleted ~51 tab-referenced conversations. See
    // docs/plans/grassy-chirping-crest.md Layer 2 for the full analysis.
    //
    // Both backends are passed in regardless of which is currently active —
    // an inactive backend's tabs are still valid resumable conversations
    // and must not be deleted just because the user toggled the backend.
    startConversationCleanup({
      tabsFiles: [tabsFileForBackend('api'), tabsFileForBackend('cli')],
      chainsFiles: [sessionChainsFileForBackend('api'), sessionChainsFileForBackend('cli')],
      labelsFiles: [sessionLabelsFileForBackend('api'), sessionLabelsFileForBackend('cli')],
    })

    app.on('activate', () => showWindow('app activate'))
  })

  app.on('will-quit', () => {
    globalShortcut.unregisterAll()
    sessionPlane.shutdown()
    for (const [, entry] of fileWatchers) {
      if (entry.debounceTimer) clearTimeout(entry.debounceTimer)
      entry.watcher.close()
    }
    fileWatchers.clear()
    if (state.tray) {
      state.tray.destroy()
      state.tray = null
    }
    stopTabSnapshotPolling()
    if (state.remoteTransport) {
      state.remoteTransport.stop()
      state.remoteTransport = null
    }
    try { rmSync(join(app.getPath('userData'), 'ion.pid')) } catch {}
    flushLogs()
  })

  process.on('SIGUSR1', () => {
    log('SIGUSR1 received, draining active work before quit')
    const timeout = setTimeout(async () => {
      log('Drain timeout (5min), force quitting')
      await flushRendererTabs()
      state.forceQuit = true
      terminalManager.destroyAll()
      // Bootout the daemon so launchd does not restart it after we exit.
      await engineBridge.shutdownAndWait().catch((e) => { log(`engine daemon bootout failed on quit (non-fatal): ${e instanceof Error ? e.message : String(e)}`) })
      sessionPlane.shutdown()
      globalShortcut.unregisterAll()
      if (state.tray) { state.tray.destroy(); state.tray = null }
      try { rmSync(join(app.getPath('userData'), 'ion.pid')) } catch {}
      flushLogs()
      app.exit(0)
    }, 5 * 60 * 1000)

    sessionPlane.drain(() => bashProcesses.size > 0).then(async () => {
      clearTimeout(timeout)
      log('All agents finished, quitting')
      await flushRendererTabs()
      state.forceQuit = true
      terminalManager.destroyAll()
      // Bootout the daemon so launchd does not restart it after we exit.
      await engineBridge.shutdownAndWait().catch((e) => { log(`engine daemon bootout failed on quit (non-fatal): ${e instanceof Error ? e.message : String(e)}`) })
      sessionPlane.shutdown()
      globalShortcut.unregisterAll()
      if (state.tray) { state.tray.destroy(); state.tray = null }
      try { rmSync(join(app.getPath('userData'), 'ion.pid')) } catch {}
      flushLogs()
      app.exit(0)
    })
  })

  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') {
      app.quit()
    }
  })
}
