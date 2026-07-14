import { readFileSync, statSync } from 'fs'
import { basename, extname } from 'path'
import { nativeImage } from 'electron'
import { log as _log, warn as _warn } from '../logger'
import { expandHome } from '../utils/expandHome'
import { stageAttachment } from '../engine-bridge-fs'
import type { ImageAttachmentPayload } from '../../shared/types'

const TAG = 'attachments'
function log(msg: string): void { _log(TAG, msg) }
function warn(msg: string): void { _warn(TAG, msg) }

// Original-file size cap. Anything larger is rejected before decode so a
// stray multi-hundred-MB photo never explodes memory on the way to resize.
const RAW_MAX_BYTES = 25 * 1024 * 1024

// Anthropic auto-downscales anything wider than 1568px on the long edge,
// so resampling client-side at this size is the largest you'd ever want
// to send. Everything past it is paid token waste.
const MAX_DIM = 1568

// Target encoded size after recompression. iOS already compresses to
// ~1 MB before upload; matching that here keeps the wire payload small,
// stays well under Anthropic's 5 MB per-image input cap, and stays under
// the engine's NDJSON line cap with room to spare.
const TARGET_BYTES = 1_000_000

// Engine-side cap for a single inlined document (mirrors the engine's
// maxInlineAttachmentBytes). PDFs are sent verbatim -- never recompressed --
// so anything over this is refused client-side with an honest fallback.
const PDF_MAX_BYTES = 24 * 1024 * 1024

// Cumulative raw-bytes budget across all attachments in one prompt. Base64
// inflates by ~4/3, so 36MB raw ~= 48MB encoded -- comfortably under the
// engine's 64MB NDJSON line cap including envelope. Only inline (image/PDF
// content-block) bytes count against this -- staged files ride a separate
// out-of-band stage_attachment call, not the prompt payload.
const PROMPT_TOTAL_MAX_BYTES = 36 * 1024 * 1024

// Mirrors the engine's stage_attachment decoded-payload cap
// (engine/internal/protocol/protocol.go). A file over this size cannot be
// staged either -- client-side pre-check avoids reading + base64-encoding a
// file the engine will reject outright.
const ENGINE_STAGE_MAX_BYTES = 36 * 1024 * 1024

const MIME_BY_EXT: Record<string, string> = {
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.png': 'image/png',
  '.webp': 'image/webp',
  '.gif': 'image/gif',
}

// Above this size a text-representable file is staged to the engine host
// (like any other binary) instead of being decoded and inlined into the
// prompt text. Keeps a stray multi-MB log from blowing up the prompt --
// the engine's own attachment-marker Read path handles the large case.
const INLINE_TEXT_MAX = 256 * 1024

// Extensions treated as UTF-8 decodable text worth inlining directly into
// the prompt (below INLINE_TEXT_MAX) rather than staging to the engine
// host: plain text/data formats plus common source-code extensions.
const TEXT_EXTENSIONS = new Set([
  '.txt', '.md', '.markdown', '.csv', '.tsv', '.yaml', '.yml', '.log', '.json',
  '.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs', '.py', '.go', '.rs', '.java',
  '.c', '.h', '.cpp', '.hpp', '.cc', '.cs', '.rb', '.php', '.swift', '.kt',
  '.sh', '.bash', '.zsh', '.sql', '.html', '.htm', '.css', '.scss', '.xml',
  '.toml', '.ini', '.cfg', '.conf', '.env', '.gitignore', '.dockerfile',
])

// Best-effort MIME for the engine's advisory (logged-only) MimeType field
// on stage_attachment. Falls back to a generic octet-stream for anything
// not covered by MIME_BY_EXT or the text set below.
const MIME_BY_TEXT_EXT: Record<string, string> = {
  '.json': 'application/json',
  '.yaml': 'text/yaml',
  '.yml': 'text/yaml',
  '.csv': 'text/csv',
  '.html': 'text/html',
  '.htm': 'text/html',
  '.css': 'text/css',
  '.xml': 'application/xml',
}

function guessMimeType(ext: string, isText: boolean): string {
  if (MIME_BY_EXT[ext]) return MIME_BY_EXT[ext]
  if (isText) return MIME_BY_TEXT_EXT[ext] ?? 'text/plain'
  return 'application/octet-stream'
}

/** Subset of the inbound remote attachment shape we read from. */
export interface RawAttachment {
  type: string // "image" | "file" | ...
  name: string
  path: string
}

export interface EncodeOptions {
  /**
   * Whether the engine runs on a different host. Controls the fallback for
   * attachments we cannot inline: locally the original marker survives (the
   * engine can still read the path from disk -- #789); remotely the marker is
   * rewritten to an honest "unavailable" note, because a client-local path is
   * meaningless on the engine host.
   */
  isRemote: boolean
  /**
   * Engine session key (the same value sent as `ClientCommand.Key` on
   * `send_prompt` for this conversation -- the tab id). Required when
   * `isRemote` is true and any non-image/PDF or over-threshold attachment is
   * present, since `stage_attachment` scopes its scratch directory to this
   * key. Ignored when `isRemote` is false (the local engine reads the
   * client-local path directly and never stages).
   */
  key?: string
}

export interface EncodeResult {
  encoded: ImageAttachmentPayload[]
  rewrittenText: string
}

/**
 * Decode → optionally downscale → JPEG-encode at decreasing quality until
 * the output fits TARGET_BYTES. Returns the encoded JPEG bytes, or null if
 * the source can't be decoded as an image.
 *
 * GIFs get sent as-is (no decode loop): nativeImage drops animation, and
 * static frames are usually small. PNG with transparency also passes
 * through unchanged at <= TARGET_BYTES, otherwise it's flattened to JPEG.
 */
function compressImage(buf: Buffer, mediaType: string): { bytes: Buffer; mediaType: string } | null {
  if (mediaType === 'image/gif') {
    return { bytes: buf, mediaType }
  }
  let img = nativeImage.createFromBuffer(buf)
  if (img.isEmpty()) return null

  const size = img.getSize()
  const longEdge = Math.max(size.width, size.height)
  if (longEdge > MAX_DIM) {
    const scale = MAX_DIM / longEdge
    img = img.resize({
      width: Math.round(size.width * scale),
      height: Math.round(size.height * scale),
      quality: 'best',
    })
  }

  // PNG passthrough when small enough — keeps transparency intact.
  if (mediaType === 'image/png' && longEdge <= MAX_DIM && buf.length <= TARGET_BYTES) {
    return { bytes: buf, mediaType: 'image/png' }
  }

  for (const q of [85, 75, 65, 55, 45, 35]) {
    const encoded = img.toJPEG(q)
    if (encoded.length <= TARGET_BYTES) {
      return { bytes: encoded, mediaType: 'image/jpeg' }
    }
  }
  // Last-resort: lowest quality we tried, even if it still exceeds the
  // target. Better to overshoot a little than to drop the image entirely.
  return { bytes: img.toJPEG(35), mediaType: 'image/jpeg' }
}

/** Replace the first occurrence of `marker` in `text` with `replacement`. */
function replaceMarker(text: string, marker: string, replacement: string): string {
  const idx = text.indexOf(marker)
  if (idx < 0) return text
  return text.slice(0, idx) + replacement + text.slice(idx + marker.length)
}

/**
 * Read each attachment from disk, base64-encode it, and produce both:
 *   - an array of {mediaType,data,path} payloads to ride alongside the user
 *     prompt as native multimodal content (image blocks for images, document
 *     blocks for PDFs -- the engine keys on mediaType), and
 *   - a rewritten prompt text.
 *
 * Marker rewriting is the contract that keeps remote engines fast and honest:
 *   - successfully encoded (inlined) attachments get `[Attachment: <name>
 *     (content attached)]` -- a form that matches NEITHER the harness
 *     resolver's MARKER_RE nor the engine's attachmentMarkerRe, so no
 *     component ever polls or Reads a client-local path for content that
 *     already rode the wire (previously every remote image/PDF burned a
 *     ~15s resolver timeout);
 *   - failures fall back per EncodeOptions.isRemote (see above).
 *
 * Non-image, non-PDF `file` attachments (remote only -- local engines read
 * the client-local path directly, see #789):
 *   - text-representable files (by extension) at or under INLINE_TEXT_MAX are
 *     read and their UTF-8 contents are inlined directly into the prompt
 *     text, replacing the marker -- no engine round-trip needed;
 *   - everything else (binaries, and text files over the inline threshold)
 *     is staged to the engine host via `stageAttachment` and the marker is
 *     rewritten to `[Attached file: <engine-host-path>]` -- deliberately the
 *     `file` kind regardless of original type, since that is the only kind
 *     the engine's attachment-marker handling (`cli_attachments.go`) gives
 *     special (PDF document-block) treatment to; for every other extension
 *     it is left as literal text, which the model reads via Read/Bash on
 *     the engine host now that the bytes actually live there;
 *   - images and PDFs over their inline caps (RAW_MAX_BYTES /
 *     PDF_MAX_BYTES) stage the same way remotely, instead of being dropped;
 *     locally they keep the pre-existing behavior (unrelated client-side
 *     decode/memory safety caps, not a reachability problem);
 *   - staging failures fall back to the same honest "unavailable" note used
 *     for image/PDF failures -- never a thrown error, never a silently
 *     dropped prompt.
 */
export async function encodeAttachments(
  text: string,
  attachments: RawAttachment[] | undefined,
  opts: EncodeOptions,
): Promise<EncodeResult> {
  if (!attachments || attachments.length === 0) {
    return { encoded: [], rewrittenText: text }
  }

  const encoded: ImageAttachmentPayload[] = []
  let rewritten = text
  let totalBytes = 0

  for (const a of attachments) {
    const name = basename(a.path)
    const ext = extname(a.path).toLowerCase()
    const isPdf = a.type === 'file' && ext === '.pdf'
    const isOtherFile = a.type === 'file' && !isPdf

    // Local engines already read the client-local path directly (#789);
    // only remote engines need staging/inlining for non-image, non-PDF
    // files. Leave the marker untouched here, exactly as before.
    if (isOtherFile && !opts.isRemote) continue

    const marker = `[Attached ${a.type}: ${a.path}]`
    const kindNoun = a.type === 'image' ? 'image' : 'file'
    const fail = (reason: string, note: string): void => {
      warn(`encode skipped: ${reason} path=${a.path}`)
      if (opts.isRemote || a.type === 'image') {
        // Remote: the path cannot be read on the engine host, so an honest
        // note beats a dead marker. Images also always get the note (their
        // markers were never Read-fallback material).
        rewritten = replaceMarker(rewritten, marker, `[${kindNoun} unavailable: ${name}${note}]`)
      }
      // Local non-image: keep the original marker -- the engine reads the
      // path from disk (#789) or the model falls back to the Read tool.
    }

    let srcPath: string
    let size: number
    try {
      srcPath = expandHome(a.path)
      size = statSync(srcPath).size
    } catch (err) {
      fail(`stat failed: ${(err as Error).message}`, '')
      continue
    }

    // Shared stage-to-engine-host path for anything that cannot be inlined:
    // non-image/PDF files (binary, or text over INLINE_TEXT_MAX), and
    // images/PDFs over their inline caps. Rewrites the marker to
    // `[Attached file: <engine-host-path>]` -- deliberately the `file` kind
    // regardless of the original attachment type, since that is the only
    // kind the engine's attachmentMarkerRe gives special (PDF
    // document-block) treatment to; for every other extension the engine
    // leaves a `file` marker as literal text, which is exactly what we want
    // here -- the model sees a real, Read-able absolute path on its own
    // host instead of a client-local path that doesn't exist there. Local
    // engines never reach this path -- non-image/PDF files return above,
    // and over-cap images/PDFs locally still fail (unchanged, pre-existing
    // client-side safety caps unrelated to remote reachability).
    const stageFile = async (mimeType: string): Promise<boolean> => {
      if (!opts.key) {
        fail(`stage skipped: missing engine session key`, '')
        return false
      }
      if (size > ENGINE_STAGE_MAX_BYTES) {
        fail(`too large to stage: ${(size / (1024 * 1024)).toFixed(1)}MB > ${ENGINE_STAGE_MAX_BYTES / (1024 * 1024)}MB`, ` -- too large to send (${(size / (1024 * 1024)).toFixed(0)}MB)`)
        return false
      }
      let buf: Buffer
      try {
        buf = readFileSync(srcPath)
      } catch (err) {
        fail(`read failed: ${(err as Error).message}`, '')
        return false
      }
      const staged = await stageAttachment(opts.key, name, mimeType, buf.toString('base64'))
      if (!staged.ok || !staged.path) {
        fail(`stage failed: ${staged.error ?? 'unknown error'}`, '')
        return false
      }
      rewritten = replaceMarker(rewritten, marker, `[Attached file: ${staged.path}]`)
      log(`staged ${kindNoun}: ${name} raw=${buf.length} enginePath=${staged.path}`)
      return true
    }

    if (isPdf) {
      if (size > PDF_MAX_BYTES) {
        if (opts.isRemote) {
          await stageFile('application/pdf')
        } else {
          // Local: pre-existing behavior, unchanged -- keep the original
          // marker so the engine's disk-Read fallback (#789) handles it.
          fail(`pdf too large: ${(size / (1024 * 1024)).toFixed(1)}MB`, ` -- too large to send (${(size / (1024 * 1024)).toFixed(0)}MB)`)
        }
        continue
      }
      if (totalBytes + size > PROMPT_TOTAL_MAX_BYTES) {
        fail(`prompt attachment budget exceeded`, ' -- attachment budget for this message exceeded')
        continue
      }
      let buf: Buffer
      try {
        buf = readFileSync(srcPath)
      } catch (err) {
        fail(`read failed: ${(err as Error).message}`, '')
        continue
      }
      totalBytes += buf.length
      encoded.push({ mediaType: 'application/pdf', data: buf.toString('base64'), path: a.path })
      rewritten = replaceMarker(rewritten, marker, `[Attachment: ${name} (content attached)]`)
      log(`encoded pdf: ${name} raw=${buf.length}`)
      continue
    }

    if (isOtherFile) {
      // opts.isRemote is guaranteed true here (local returned above).
      const isText = TEXT_EXTENSIONS.has(ext)

      if (isText && size <= INLINE_TEXT_MAX) {
        let buf: Buffer
        try {
          buf = readFileSync(srcPath)
        } catch (err) {
          fail(`read failed: ${(err as Error).message}`, '')
          continue
        }
        const contents = buf.toString('utf8')
        rewritten = replaceMarker(rewritten, marker, `Contents of ${name}:\n\n${contents}`)
        log(`inlined text file: ${name} bytes=${buf.length}`)
        continue
      }

      // Binary, or text over the inline threshold: stage to the engine host
      // and let the model Read it from there.
      await stageFile(guessMimeType(ext, isText))
      continue
    }

    // Images: existing compress pipeline, unchanged, except over-cap images
    // now stage remotely instead of being dropped -- the engine host gets
    // the bytes even though it cannot turn a bare `file` marker into a
    // native image content block (no such engine-side path-based image
    // ingestion exists), so the model reaches it via Read/Bash instead.
    const mediaType = MIME_BY_EXT[ext]
    if (!mediaType) {
      fail(`unsupported image extension: ${ext || '(none)'}`, '')
      continue
    }
    if (size > RAW_MAX_BYTES) {
      if (opts.isRemote) {
        await stageFile(mediaType)
      } else {
        // Local: pre-existing behavior, unchanged -- images always fail
        // with an honest note rather than staging (client-side decode
        // safety cap, unrelated to remote reachability).
        fail(`image too large to load: ${(size / (1024 * 1024)).toFixed(1)}MB > ${RAW_MAX_BYTES / (1024 * 1024)}MB`, '')
      }
      continue
    }
    let buf: Buffer
    try {
      buf = readFileSync(srcPath)
    } catch (err) {
      fail(`read failed: ${(err as Error).message}`, '')
      continue
    }
    const compressed = compressImage(buf, mediaType)
    if (!compressed) {
      fail(`decode failed: not a valid image`, '')
      continue
    }
    totalBytes += compressed.bytes.length
    encoded.push({
      mediaType: compressed.mediaType,
      data: compressed.bytes.toString('base64'),
      path: a.path,
    })
    rewritten = replaceMarker(rewritten, marker, `[Attachment: ${name} (content attached)]`)
    log(`encoded image: ${name} raw=${buf.length} sent=${compressed.bytes.length} mime=${compressed.mediaType}`)
  }

  return { encoded, rewrittenText: rewritten }
}
