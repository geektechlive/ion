import { app, BrowserWindow, dialog, globalShortcut, Menu, nativeImage, screen, session, Tray } from 'electron'
import { join } from 'path'
import { IPC } from '../shared/types'
import { log as _log, flushLogs } from './logger'
import { state, SPACES_DEBUG, sessionPlane, engineBridge } from './state'
import { broadcast } from './broadcast'
import { terminalManager } from './terminal-manager-instance'

function log(msg: string): void {
  _log('main', msg)
}

export function snapshotWindowState(reason: string): void {
  if (!SPACES_DEBUG) return
  if (!state.mainWindow || state.mainWindow.isDestroyed()) {
    log(`[spaces] ${reason} window=none`)
    return
  }

  const win = state.mainWindow
  const b = win.getBounds()
  const cursor = screen.getCursorScreenPoint()
  const display = screen.getDisplayNearestPoint(cursor)
  const visibleOnAll = win.isVisibleOnAllWorkspaces()
  const wcFocused = win.webContents.isFocused()

  log(
    `[spaces] ${reason} ` +
    `vis=${win.isVisible()} focused=${win.isFocused()} wcFocused=${wcFocused} ` +
    `alwaysOnTop=${win.isAlwaysOnTop()} allWs=${visibleOnAll} ` +
    `bounds=(${b.x},${b.y},${b.width}x${b.height}) ` +
    `cursor=(${cursor.x},${cursor.y}) display=${display.id} ` +
    `workArea=(${display.workArea.x},${display.workArea.y},${display.workArea.width}x${display.workArea.height})`
  )
}

export function scheduleToggleSnapshots(toggleId: number, phase: 'show' | 'hide'): void {
  if (!SPACES_DEBUG) return
  const probes = [0, 100, 400, 1200]
  for (const delay of probes) {
    setTimeout(() => {
      snapshotWindowState(`toggle#${toggleId} ${phase} +${delay}ms`)
    }, delay)
  }
}

function getContentSecurityPolicy(): string {
  const isDev = !!process.env.ELECTRON_RENDERER_URL
  if (isDev) {
    return [
      "default-src 'self'",
      "script-src 'self' 'unsafe-inline' 'unsafe-eval'",
      "style-src 'self' 'unsafe-inline'",
      "connect-src 'self' ws://localhost:*",
      "img-src 'self' data: blob:",
      "media-src 'self' data: blob:",
      "font-src 'self' data:",
      "object-src 'none'",
      "base-uri 'none'",
      "frame-src 'none'",
    ].join('; ')
  }
  return [
    "default-src 'self'",
    "script-src 'self'",
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: blob:",
    "media-src 'self' data: blob:",
    "font-src 'self' data:",
    "object-src 'none'",
    "base-uri 'none'",
    "frame-src 'none'",
  ].join('; ')
}

export function installContentSecurityPolicy(): void {
  session.defaultSession.webRequest.onHeadersReceived((details, callback) => {
    callback({
      responseHeaders: {
        ...details.responseHeaders,
        'Content-Security-Policy': [getContentSecurityPolicy()],
      },
    })
  })
}

export function createWindow(): void {
  const cursor = screen.getCursorScreenPoint()
  const display = screen.getDisplayNearestPoint(cursor)
  const { x: dx, y: dy, width: sw, height: sh } = display.workArea

  const mainWindow = new BrowserWindow({
    width: sw,
    height: sh,
    x: dx,
    y: dy,
    ...(process.platform === 'darwin' ? { type: 'panel' as const } : {}),
    frame: false,
    transparent: true,
    resizable: false,
    movable: true,
    alwaysOnTop: true,
    skipTaskbar: true,
    hasShadow: false,
    roundedCorners: true,
    backgroundColor: '#00000000',
    show: false,
    icon: join(__dirname, '../../resources/icon.icns'),
    webPreferences: {
      preload: join(__dirname, '../preload/index.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      webSecurity: true,
      allowRunningInsecureContent: false,
    },
  })

  state.mainWindow = mainWindow

  mainWindow.setVisibleOnAllWorkspaces(true, { visibleOnFullScreen: true })
  // Use 'modal-panel' rather than 'screen-saver' here. 'modal-panel' sits
  // above normal apps (browsers, VSCode — CGWindowLevel 0) so the overlay
  // covers them in tall mode, but it stays BELOW macOS TCC/permission dialogs
  // and the Dock (which live at kCGPopUpMenuWindowLevel and above). That lets
  // system permission prompts surface over the Ion overlay when they fire.
  //
  // DO NOT raise this back to 'screen-saver' to fix a perceived z-order bug.
  // 'screen-saver' is CGWindowLevel 2000, which sits above TCC dialogs
  // (~1000) and causes them to be hidden behind the overlay in tall mode —
  // the exact symptom this change was made to cure.
  mainWindow.setAlwaysOnTop(true, 'modal-panel')

  mainWindow.webContents.on('console-message', (_e, level, message) => {
    if (level >= 3) log(`[renderer:error] ${message}`)
    else if (level === 2) log(`[renderer:warn] ${message}`)
    else log(`[renderer] ${message}`)
  })
  mainWindow.webContents.on('render-process-gone', (_e, details) => {
    log(`[renderer:gone] reason=${details.reason} exitCode=${details.exitCode}`)
  })

  mainWindow.webContents.setWindowOpenHandler(() => ({ action: 'deny' }))
  mainWindow.webContents.on('will-navigate', (event) => event.preventDefault())

  mainWindow.once('ready-to-show', () => {
    state.mainWindow?.show()
    state.mainWindow?.setIgnoreMouseEvents(true, { forward: true })
    if (process.env.ELECTRON_RENDERER_URL) {
      state.mainWindow?.webContents.openDevTools({ mode: 'detach' })
    }
  })

  app.on('before-quit', (e) => {
    if (state.forceQuit) return
    e.preventDefault()
    const hasRunning = sessionPlane.hasRunningTabs()
    // 0 = Quit Desktop (engine keeps running)
    // 1 = Quit All (engine shuts down too)
    // 2 = Cancel
    const choice = dialog.showMessageBoxSync(state.mainWindow!, {
      type: 'question',
      buttons: ['Quit Desktop', 'Quit All', 'Cancel'],
      defaultId: 2,
      cancelId: 2,
      title: 'Quit Ion?',
      message: hasRunning
        ? 'Sessions are running in the engine.'
        : 'How would you like to quit?',
      detail: hasRunning
        ? 'Quit Desktop closes the window but keeps engine sessions running.\nQuit All stops the engine and all running sessions.\n\nTip: ⌥Space hides/shows the window without quitting.'
        : 'Quit Desktop closes the window but keeps the engine running.\nQuit All stops the engine too.\n\nTip: ⌥Space hides/shows the window without quitting.',
    })
    if (choice === 0 || choice === 1) {
      const shutdownEngine = choice === 1
      // Flush renderer tab state before exiting — the Zustand store debounces
      // persistTabs() at 100ms and app.exit(0) kills the renderer immediately,
      // so any pending state (conversationId, titles, etc.) would be lost.
      void (async () => {
        for (const win of BrowserWindow.getAllWindows()) {
          try {
            await win.webContents.executeJavaScript(
              'window.__ionForceFlushTabs && window.__ionForceFlushTabs()',
            )
          } catch {
            // Window may already be destroyed or renderer unresponsive.
          }
        }
        if (shutdownEngine) {
          log('Quit All: shutting down engine process')
          await engineBridge.shutdownAndWait().catch((err: Error) => {
            log(`Engine shutdown error (proceeding with quit): ${err.message}`)
          })
        }
        state.forceQuit = true
        terminalManager.destroyAll()
        sessionPlane.shutdown()
        globalShortcut.unregisterAll()
        if (state.tray) {
          state.tray.destroy()
          state.tray = null
        }
        flushLogs()
        app.exit(0)
      })()
    }
  })
  mainWindow.on('close', (e) => {
    if (!state.forceQuit) {
      e.preventDefault()
      state.mainWindow?.hide()
    }
  })
  mainWindow.on('closed', () => {
    state.mainWindow = null
  })

  if (process.env.ELECTRON_RENDERER_URL) {
    mainWindow.loadURL(process.env.ELECTRON_RENDERER_URL)
  } else {
    mainWindow.loadFile(join(__dirname, '../renderer/index.html'))
  }
}

export function createTray(): void {
  const trayIconPath = join(__dirname, '../../resources/trayTemplate.png')
  const trayIcon = nativeImage.createFromPath(trayIconPath)
  trayIcon.setTemplateImage(true)
  state.tray = new Tray(trayIcon)
  state.tray.setToolTip('Ion')
  state.tray.setContextMenu(
    Menu.buildFromTemplate([
      { label: 'Toggle Interface', accelerator: 'Alt+Space', click: () => toggleWindow('tray menu') },
      { type: 'separator' },
      { label: 'Settings...', click: () => {
        showWindow('tray settings')
        if (state.mainWindow && !state.mainWindow.isDestroyed()) {
          state.mainWindow.webContents.send(IPC.SHOW_SETTINGS)
        }
      } },
      { type: 'separator' },
      { label: 'Quit', click: () => { app.quit() } },
    ])
  )
}

export function ensureWindow(): void {
  if (!state.mainWindow || state.mainWindow.isDestroyed()) {
    createWindow()
  }
  if (!state.tray || state.tray.isDestroyed()) {
    createTray()
  }
}

export function showWindow(source = 'unknown'): void {
  ensureWindow()
  if (!state.mainWindow) return
  const toggleId = ++state.toggleSequence

  const cursor = screen.getCursorScreenPoint()
  const display = screen.getDisplayNearestPoint(cursor)
  const { x: dx, y: dy, width: sw, height: sh } = display.workArea
  state.mainWindow.setBounds({ x: dx, y: dy, width: sw, height: sh })

  state.mainWindow.setVisibleOnAllWorkspaces(true, { visibleOnFullScreen: true })

  if (SPACES_DEBUG) {
    log(`[spaces] showWindow#${toggleId} source=${source} move-to-display id=${display.id}`)
    snapshotWindowState(`showWindow#${toggleId} pre-show`)
  }
  state.mainWindow.show()
  state.mainWindow.webContents.focus()
  broadcast(IPC.WINDOW_SHOWN)
  if (SPACES_DEBUG) scheduleToggleSnapshots(toggleId, 'show')
}

export function toggleWindow(source = 'unknown'): void {
  if (!state.mainWindow || state.mainWindow.isDestroyed()) return
  const toggleId = ++state.toggleSequence
  if (SPACES_DEBUG) {
    log(`[spaces] toggle#${toggleId} source=${source} start`)
    snapshotWindowState(`toggle#${toggleId} pre`)
  }

  if (state.mainWindow.isVisible()) {
    state.mainWindow.hide()
    if (SPACES_DEBUG) scheduleToggleSnapshots(toggleId, 'hide')
  } else {
    showWindow(source)
  }
}
