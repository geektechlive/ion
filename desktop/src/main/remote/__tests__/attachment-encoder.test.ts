import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mkdtempSync, writeFileSync, rmSync, mkdirSync } from 'fs'
import { tmpdir } from 'os'
import { join } from 'path'

// electron is not loadable from a node-environment vitest run. Mock
// nativeImage so the encoder's compress step works against a controllable
// stub: the stub returns the input bytes verbatim as a JPEG, keeping the
// test focused on encoder behavior (path rewriting, mime sniffing, error
// handling) rather than real image codec output.
vi.mock('electron', () => {
  const makeImage = (buf: Buffer, w = 100, h = 100): any => ({
    isEmpty: () => buf.length === 0,
    getSize: () => ({ width: w, height: h }),
    resize: (_opts: any) => makeImage(buf, _opts.width ?? w, _opts.height ?? h),
    toJPEG: (_q: number) => buf,
  })
  return {
    nativeImage: {
      createFromBuffer: (buf: Buffer) => makeImage(buf),
    },
  }
})

// The engine bridge (and everything it transitively imports -- state,
// electron IPC) is mocked wholesale so attachment-encoder's `stageAttachment`
// import resolves to a controllable stub instead of touching Electron.
const stageAttachmentMock = vi.fn()
vi.mock('../../engine-bridge-fs', () => ({
  stageAttachment: (...args: unknown[]) => stageAttachmentMock(...args),
}))

import { encodeAttachments, type RawAttachment } from '../attachment-encoder'

let workDir: string

beforeEach(() => {
  workDir = mkdtempSync(join(tmpdir(), 'attachenc-'))
  stageAttachmentMock.mockReset()
})

afterEach(() => {
  rmSync(workDir, { recursive: true, force: true })
})

const writeBytes = (name: string, bytes: Buffer): string => {
  const p = join(workDir, name)
  writeFileSync(p, bytes)
  return p
}

const att = (path: string, type = 'image', name?: string): RawAttachment => ({
  type,
  path,
  name: name ?? path.split('/').pop() ?? path,
})

const local = { isRemote: false }
const remote = { isRemote: true, key: 'tab-123' }

/** Write a sparse file whose stat size is `mb` megabytes. */
const writeSparse = (name: string, mb: number): string => {
  const p = join(workDir, name)
  const fd = require('fs').openSync(p, 'w')
  require('fs').writeSync(fd, Buffer.from([0]), 0, 1, mb * 1024 * 1024)
  require('fs').closeSync(fd)
  return p
}

describe('encodeAttachments — images', () => {
  it('returns empty result when no attachments are supplied', async () => {
    const r = await encodeAttachments('hi', undefined, local)
    expect(r.encoded).toEqual([])
    expect(r.rewrittenText).toBe('hi')
  })

  it('encodes a real jpeg and rewrites its marker to the content-attached form', async () => {
    const path = writeBytes('photo.jpg', Buffer.from([0xff, 0xd8, 0xff, 0xe0]))
    const text = `[Attached image: ${path}]\n\nwhat is this`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(path)], local)
    expect(rewrittenText).toBe('[Attachment: photo.jpg (content attached)]\n\nwhat is this')
    expect(encoded).toHaveLength(1)
    expect(encoded[0].mediaType).toBe('image/jpeg')
    expect(encoded[0].data).toBe(Buffer.from([0xff, 0xd8, 0xff, 0xe0]).toString('base64'))
    expect(encoded[0].path).toBe(path)
  })

  it('the rewritten marker matches neither harness MARKER_RE nor engine attachmentMarkerRe', async () => {
    const path = writeBytes('photo.jpg', Buffer.from([0xff, 0xd8]))
    const { rewrittenText } = await encodeAttachments(`[Attached image: ${path}]`, [att(path)], remote)
    // Same grammar as harness-ts attachmentResolver MARKER_RE and the engine's
    // attachmentMarkerRe: a rewritten marker must never re-match either.
    const markerRe = /\[Attached (file|image|plan): ([^\]]+)\]/g
    expect(rewrittenText.match(markerRe)).toBeNull()
  })

  it('rewrites the marker for a missing file and omits it from encoded', async () => {
    const path = join(workDir, 'gone.png')
    const text = `[Attached image: ${path}]\n\nplease describe`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(path)], local)
    expect(encoded).toEqual([])
    expect(rewrittenText).toBe('[image unavailable: gone.png]\n\nplease describe')
  })

  it('rewrites the marker for an unsupported extension', async () => {
    const path = writeBytes('thing.bmp', Buffer.from([1, 2, 3]))
    const text = `[Attached image: ${path}]`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(path)], local)
    expect(encoded).toEqual([])
    expect(rewrittenText).toBe('[image unavailable: thing.bmp]')
  })

  it('rejects images larger than the raw cap by rewriting the marker', async () => {
    const big = writeSparse('huge.jpg', 26)
    const text = `[Attached image: ${big}]`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(big)], local)
    expect(encoded).toEqual([])
    expect(rewrittenText).toBe('[image unavailable: huge.jpg]')
  })

  it('passes png through unchanged when small, sends webp recompressed as jpeg', async () => {
    const png = writeBytes('a.png', Buffer.from([1, 2]))
    const webp = writeBytes('b.webp', Buffer.from([3, 4]))
    const text = `[Attached image: ${png}]\n[Attached image: ${webp}]`
    const { encoded } = await encodeAttachments(text, [att(png), att(webp)], local)
    expect(encoded).toHaveLength(2)
    expect(encoded[0].mediaType).toBe('image/png')
    expect(encoded[1].mediaType).toBe('image/jpeg')
  })

  it('does not pollute the directory when given empty input', async () => {
    mkdirSync(join(workDir, 'subdir'))
    const r = await encodeAttachments('', [], local)
    expect(r.encoded).toEqual([])
    expect(r.rewrittenText).toBe('')
  })
})

describe('encodeAttachments — PDFs', () => {
  it('encodes a pdf verbatim (no recompression) and rewrites its marker', async () => {
    const bytes = Buffer.from('%PDF-1.4 test content')
    const path = writeBytes('report.pdf', bytes)
    const text = `[Attached file: ${path}]\n\nsummarize`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(path, 'file')], remote)
    expect(encoded).toHaveLength(1)
    expect(encoded[0].mediaType).toBe('application/pdf')
    expect(encoded[0].data).toBe(bytes.toString('base64'))
    expect(encoded[0].path).toBe(path)
    expect(rewrittenText).toBe('[Attachment: report.pdf (content attached)]\n\nsummarize')
  })

  it('over-cap pdf: keeps the original marker locally (Read/disk fallback)', async () => {
    const big = writeSparse('big.pdf', 25)
    const text = `[Attached file: ${big}]`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(big, 'file')], local)
    expect(encoded).toEqual([])
    expect(rewrittenText).toBe(text)
  })

  it('over-cap pdf (but under the engine stage cap): stages remotely instead of dropping', async () => {
    stageAttachmentMock.mockResolvedValue({ ok: true, path: '/home/engine/.ion/attachments/tab-123/uuid-big.pdf' })
    const big = writeSparse('big.pdf', 25)
    const text = `[Attached file: ${big}]`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(big, 'file')], remote)
    expect(encoded).toEqual([])
    expect(stageAttachmentMock).toHaveBeenCalledWith('tab-123', 'big.pdf', 'application/pdf', expect.any(String))
    expect(rewrittenText).toBe('[Attached file: /home/engine/.ion/attachments/tab-123/uuid-big.pdf]')
  })

  it('over the engine stage cap too: rewrites to an honest note remotely without staging', async () => {
    const huge = writeSparse('huge.pdf', 40)
    const text = `[Attached file: ${huge}]`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(huge, 'file')], remote)
    expect(encoded).toEqual([])
    expect(stageAttachmentMock).not.toHaveBeenCalled()
    expect(rewrittenText).toContain('[file unavailable: huge.pdf -- too large to send (40MB)]')
  })

  it('enforces the cumulative prompt budget across multiple pdfs', async () => {
    const a = writeSparse('a.pdf', 20)
    const b = writeSparse('b.pdf', 20)
    const text = `[Attached file: ${a}]\n[Attached file: ${b}]`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(a, 'file'), att(b, 'file')], remote)
    expect(encoded).toHaveLength(1)
    expect(encoded[0].path).toBe(a)
    expect(rewrittenText).toContain('[Attachment: a.pdf (content attached)]')
    expect(rewrittenText).toContain('[file unavailable: b.pdf -- attachment budget for this message exceeded]')
  })

  it('missing pdf: keeps marker locally, rewrites remotely', async () => {
    const gone = join(workDir, 'gone.pdf')
    const text = `[Attached file: ${gone}]`
    expect((await encodeAttachments(text, [att(gone, 'file')], local)).rewrittenText).toBe(text)
    expect((await encodeAttachments(text, [att(gone, 'file')], remote)).rewrittenText).toBe('[file unavailable: gone.pdf]')
  })

  it('handles pdf + image together', async () => {
    const pdf = writeBytes('doc.pdf', Buffer.from('%PDF-1.4 x'))
    const img = writeBytes('pic.jpg', Buffer.from([0xff, 0xd8]))
    const text = `[Attached file: ${pdf}]\n[Attached image: ${img}]\ncompare`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(pdf, 'file'), att(img)], remote)
    expect(encoded).toHaveLength(2)
    expect(encoded.map((e) => e.mediaType)).toEqual(['application/pdf', 'image/jpeg'])
    expect(rewrittenText).toContain('[Attachment: doc.pdf (content attached)]')
    expect(rewrittenText).toContain('[Attachment: pic.jpg (content attached)]')
  })
})

describe('encodeAttachments — non-image/PDF files (local, unchanged)', () => {
  it('leaves non-pdf file attachments and plan markers untouched locally', async () => {
    const txt = writeBytes('notes.txt', Buffer.from('hello'))
    const text = `[Attached file: ${txt}]\n[Attached plan: /tmp/plan.md]\ngo`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(txt, 'file')], local)
    expect(encoded).toEqual([])
    expect(rewrittenText).toBe(text)
    expect(stageAttachmentMock).not.toHaveBeenCalled()
  })
})

describe('encodeAttachments — remote text-representable files (inlined)', () => {
  it('(a) inlines a small remote .txt file directly, without staging', async () => {
    const txt = writeBytes('notes.txt', Buffer.from('hello world'))
    const text = `[Attached file: ${txt}]\n\nsummarize`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(txt, 'file')], remote)
    expect(encoded).toEqual([])
    expect(rewrittenText).toBe('Contents of notes.txt:\n\nhello world\n\nsummarize')
    expect(stageAttachmentMock).not.toHaveBeenCalled()
  })

  it('(c) stages a remote text file over INLINE_TEXT_MAX instead of inlining it', async () => {
    stageAttachmentMock.mockResolvedValue({ ok: true, path: '/home/engine/.ion/attachments/tab-123/uuid-big.log' })
    const big = writeSparse('big.log', 1) // 1MB > 256KB threshold
    const text = `[Attached file: ${big}]`
    const { rewrittenText } = await encodeAttachments(text, [att(big, 'file')], remote)
    expect(stageAttachmentMock).toHaveBeenCalledTimes(1)
    expect(rewrittenText).toBe('[Attached file: /home/engine/.ion/attachments/tab-123/uuid-big.log]')
  })
})

describe('encodeAttachments — over-cap images (remote stages, local unchanged)', () => {
  it('remote image over RAW_MAX_BYTES stages instead of dropping', async () => {
    stageAttachmentMock.mockResolvedValue({ ok: true, path: '/home/engine/.ion/attachments/tab-123/uuid-huge.jpg' })
    const big = writeSparse('huge.jpg', 26)
    const text = `[Attached image: ${big}]`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(big)], remote)
    expect(encoded).toEqual([])
    expect(stageAttachmentMock).toHaveBeenCalledWith('tab-123', 'huge.jpg', 'image/jpeg', expect.any(String))
    expect(rewrittenText).toBe('[Attached file: /home/engine/.ion/attachments/tab-123/uuid-huge.jpg]')
  })

  it('local image over RAW_MAX_BYTES still fails with an honest note (unchanged, no staging)', async () => {
    const big = writeSparse('huge.jpg', 26)
    const text = `[Attached image: ${big}]`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(big)], local)
    expect(encoded).toEqual([])
    expect(stageAttachmentMock).not.toHaveBeenCalled()
    expect(rewrittenText).toBe('[image unavailable: huge.jpg]')
  })
})

describe('encodeAttachments — remote binary files (staged)', () => {
  it('(b) stages a remote .docx via stageAttachment and rewrites the marker to the engine path', async () => {
    stageAttachmentMock.mockResolvedValue({ ok: true, path: '/home/engine/.ion/attachments/tab-123/uuid-report.docx' })
    const docx = writeBytes('report.docx', Buffer.from([0x50, 0x4b, 0x03, 0x04]))
    const text = `[Attached file: ${docx}]\n\nreview this`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(docx, 'file')], remote)

    expect(stageAttachmentMock).toHaveBeenCalledTimes(1)
    expect(stageAttachmentMock).toHaveBeenCalledWith(
      'tab-123',
      'report.docx',
      'application/octet-stream',
      Buffer.from([0x50, 0x4b, 0x03, 0x04]).toString('base64'),
    )
    expect(encoded).toEqual([])
    expect(rewrittenText).toBe('[Attached file: /home/engine/.ion/attachments/tab-123/uuid-report.docx]\n\nreview this')
  })

  it('(e) falls back to an honest unavailable note when stageAttachment fails, never throws', async () => {
    stageAttachmentMock.mockResolvedValue({ ok: false, error: 'payload too large' })
    const docx = writeBytes('report.docx', Buffer.from([0x50, 0x4b, 0x03, 0x04]))
    const text = `[Attached file: ${docx}]`
    await expect(encodeAttachments(text, [att(docx, 'file')], remote)).resolves.not.toThrow()
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(docx, 'file')], remote)
    expect(encoded).toEqual([])
    expect(rewrittenText).toBe('[file unavailable: report.docx]')
  })
})

describe('encodeAttachments — (d) image/PDF within caps unchanged, (f) local entirely unchanged', () => {
  it('(d) image within compression caps still rides as an inline content block, no staging', async () => {
    const png = writeBytes('a.png', Buffer.from([1, 2]))
    const text = `[Attached image: ${png}]`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(png)], remote)
    expect(encoded).toHaveLength(1)
    expect(encoded[0].mediaType).toBe('image/png')
    expect(rewrittenText).toBe('[Attachment: a.png (content attached)]')
    expect(stageAttachmentMock).not.toHaveBeenCalled()
  })

  it('(d) pdf within cap still rides as an inline content block, no staging', async () => {
    const pdf = writeBytes('doc.pdf', Buffer.from('%PDF-1.4 x'))
    const text = `[Attached file: ${pdf}]`
    const { encoded } = await encodeAttachments(text, [att(pdf, 'file')], remote)
    expect(encoded).toHaveLength(1)
    expect(encoded[0].mediaType).toBe('application/pdf')
    expect(stageAttachmentMock).not.toHaveBeenCalled()
  })

  it('(f) local engine: non-image/PDF files never inline, never stage, marker untouched', async () => {
    const docx = writeBytes('report.docx', Buffer.from([0x50, 0x4b, 0x03, 0x04]))
    const txt = writeBytes('notes.txt', Buffer.from('hello'))
    const text = `[Attached file: ${docx}]\n[Attached file: ${txt}]`
    const { encoded, rewrittenText } = await encodeAttachments(text, [att(docx, 'file'), att(txt, 'file')], local)
    expect(encoded).toEqual([])
    expect(rewrittenText).toBe(text)
    expect(stageAttachmentMock).not.toHaveBeenCalled()
  })
})
