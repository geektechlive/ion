/**
 * Desktop auto-updater.
 *
 * Checks GitHub Releases for a new version on launch and every 4 hours.
 * Only active in packaged builds (`app.isPackaged`).
 * Downloads updates in the background and notifies the renderer via IPC.
 * The user confirms before the app quits and installs.
 */
import { app, BrowserWindow, ipcMain } from "electron";
import { autoUpdater, type UpdateInfo } from "electron-updater";
import { IPC } from "../shared/types-ipc";
import { info, error as logError } from "./logger";

const CHECK_INTERVAL_MS = 4 * 60 * 60 * 1000; // 4 hours

const tag = "updater";

/** Logger adapter for electron-updater (expects info/warn/error/debug methods). */
const updaterLogger = {
  info: (msg: string) => info(tag, msg),
  warn: (msg: string) => info(tag, `WARN: ${msg}`),
  error: (msg: string) => logError(tag, msg),
  debug: (msg: string) => info(tag, `DEBUG: ${msg}`),
};

let intervalId: ReturnType<typeof setInterval> | undefined;

export function initAutoUpdater(): void {
  if (!app.isPackaged) {
    info(tag, "skipping — not packaged");
    return;
  }

  autoUpdater.autoDownload = true;
  autoUpdater.autoInstallOnAppQuit = true;
  autoUpdater.logger = updaterLogger;

  autoUpdater.on("update-available", (updateInfo: UpdateInfo) => {
    info(tag, `update available: ${updateInfo.version}`);
  });

  autoUpdater.on("update-downloaded", (updateInfo: UpdateInfo) => {
    info(tag, `update downloaded: ${updateInfo.version}`);
    notifyRenderer(IPC.UPDATE_DOWNLOADED, { version: updateInfo.version });
  });

  autoUpdater.on("error", (err: Error) => {
    logError(tag, `error: ${err.message}`);
  });

  // Renderer can request install
  ipcMain.on(IPC.INSTALL_UPDATE, () => {
    autoUpdater.quitAndInstall();
  });

  // First check shortly after launch
  setTimeout(() => autoUpdater.checkForUpdates().catch(() => {}), 10_000);

  // Periodic checks
  intervalId = setInterval(
    () => autoUpdater.checkForUpdates().catch(() => {}),
    CHECK_INTERVAL_MS,
  );
}

function notifyRenderer(
  channel: string,
  payload: Record<string, unknown>,
): void {
  for (const win of BrowserWindow.getAllWindows()) {
    win.webContents.send(channel, payload);
  }
}

export function stopAutoUpdater(): void {
  if (intervalId) {
    clearInterval(intervalId);
    intervalId = undefined;
  }
}
