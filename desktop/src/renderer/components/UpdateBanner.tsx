import React, { useEffect, useState } from 'react'

/**
 * Small toast-style banner shown when electron-updater has downloaded
 * a new version. Clicking "Restart" triggers quit-and-install via IPC.
 */
export function UpdateBanner(): React.ReactElement | null {
  const [version, setVersion] = useState<string | null>(null)

  useEffect(() => {
    return window.ion.onUpdateDownloaded((info) => {
      setVersion(info.version)
    })
  }, [])

  if (!version) return null

  return (
    <div className="fixed bottom-4 right-4 z-[9999] flex items-center gap-3 rounded-lg
      bg-blue-600 px-4 py-2 text-sm text-white shadow-lg">
      <span>Ion {version} is ready to install.</span>
      <button
        className="rounded bg-white/20 px-2 py-0.5 text-xs font-medium hover:bg-white/30"
        onClick={() => window.ion.installUpdate()}
      >
        Restart
      </button>
      <button
        className="text-white/60 hover:text-white"
        onClick={() => setVersion(null)}
      >
        ✕
      </button>
    </div>
  )
}
