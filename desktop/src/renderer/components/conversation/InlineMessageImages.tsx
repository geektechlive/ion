import React, { useEffect, useState } from 'react'
import { useColors } from '../../theme'
import type { Attachment } from '../../../shared/types'

/** Module-level cache so the same image isn't re-read from disk per render. */
const dataUrlCache = new Map<string, string>()

function useImageDataUrl(path: string): string | null {
  const [dataUrl, setDataUrl] = useState<string | null>(() => dataUrlCache.get(path) ?? null)

  useEffect(() => {
    if (dataUrlCache.has(path)) {
      setDataUrl(dataUrlCache.get(path) ?? null)
      return
    }
    let cancelled = false
    window.ion.readImageDataUrl(path).then((res) => {
      if (cancelled) return
      if (res.dataUrl) {
        dataUrlCache.set(path, res.dataUrl)
        setDataUrl(res.dataUrl)
      }
    })
    return () => { cancelled = true }
  }, [path])

  return dataUrl
}

const ATTACHED_IMAGE_RE = /\[Attached image: ([^\]]+)\]/g

/**
 * Image attachment entries derived from a user message: the explicit
 * `attachments` array (when present) plus any `[Attached image: PATH]`
 * markers found in the message text. The marker pass survives a desktop
 * relaunch where the renderer's persisted state lost the attachments
 * array but the marker text in `content` was kept.
 */
export function deriveMessageImages(content: string, attachments?: Attachment[]): Array<{ key: string; path: string; name: string }> {
  const out: Array<{ key: string; path: string; name: string }> = []
  const seen = new Set<string>()

  for (const a of attachments || []) {
    if (a.type !== 'image') continue
    const path = (a as any).path as string | undefined
    if (!path || seen.has(path)) continue
    seen.add(path)
    out.push({ key: a.id, path, name: a.name })
  }

  for (const m of (content || '').matchAll(ATTACHED_IMAGE_RE)) {
    const path = m[1].trim()
    if (!path || seen.has(path)) continue
    seen.add(path)
    const name = path.split('/').pop() || path
    out.push({ key: `marker:${path}`, path, name })
  }

  return out
}

/**
 * Renders image attachments as inline thumbnails above the user-message bubble.
 * Non-image attachments are passed through to MessageAttachments below the bubble
 * (this component renders nothing for them).
 */
export function InlineMessageImages({ content, attachments }: { content: string; attachments?: Attachment[] }) {
  const images = deriveMessageImages(content, attachments)
  if (images.length === 0) return null

  return (
    <div className="flex flex-col gap-1 mb-1 items-end">
      {images.map((img) => (
        <InlineImage key={img.key} path={img.path} name={img.name} />
      ))}
    </div>
  )
}

function InlineImage({ path, name }: { path: string; name: string }) {
  const colors = useColors()
  const dataUrl = useImageDataUrl(path)

  if (!dataUrl) {
    return (
      <div
        className="text-[11px] px-2 py-1 rounded"
        style={{ background: colors.surfacePrimary, color: colors.textTertiary, border: `1px solid ${colors.surfaceSecondary}` }}
        title={path}
      >
        {name}
      </div>
    )
  }

  return (
    <button
      type="button"
      className="block rounded-lg overflow-hidden border cursor-pointer"
      style={{ borderColor: colors.toolBorder, background: colors.surfacePrimary, maxWidth: 280 }}
      onClick={() => window.ion.openExternal(`file://${path}`)}
      title={name}
    >
      <img
        src={dataUrl}
        alt={name}
        className="block w-full max-h-[260px] object-contain"
        loading="lazy"
      />
    </button>
  )
}
