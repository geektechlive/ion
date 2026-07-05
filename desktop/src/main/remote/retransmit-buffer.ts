import type { WireMessage } from './protocol'
import { warn as _warn } from '../logger'

function warn(msg: string): void {
  _warn('RetransmitBuffer', msg)
}

/**
 * Per-device retransmit ring buffer for the desktop↔iOS wire.
 *
 * The wire is fire-and-forget: a frame lost in transit (e.g. a LAN↔relay
 * transport switch mid-stream) is gone from the live stream, which freezes the
 * high-rate text/tool delta stream on iOS until the ~10s snapshot reconcile.
 * This buffer keeps the most recently sent encrypted frames per device, keyed
 * by their per-device `seq`, so that when iOS detects a forward seq gap and
 * sends `desktop_request_resend{ fromSeq, toSeq }`, the desktop can replay the
 * exact original frames — byte-identical, because they are the buffered
 * encrypted wire messages.
 *
 * Eviction: oldest-first, bounded by BOTH a message count and a byte budget
 * (whichever trips first), so a long-running stream cannot grow it unbounded.
 * A gap whose frames have been evicted is answered with
 * `desktop_resend_unavailable`, and iOS falls back to the snapshot reconcile.
 */
export class RetransmitBuffer {
  /** Per-device ordered map of seq -> stored frame. Insertion order = seq order
   *  (seq is monotonic per device), so the first key is the oldest. */
  private byDevice = new Map<string, Map<number, { msg: WireMessage; bytes: number }>>()
  private bytesByDevice = new Map<string, number>()

  constructor(
    private readonly maxMessages = 512,
    private readonly maxBytes = 2 * 1024 * 1024, // 2MB per device
  ) {}

  /** Record a frame that was just sent to `deviceId`. */
  record(deviceId: string, msg: WireMessage): void {
    let buf = this.byDevice.get(deviceId)
    if (!buf) {
      buf = new Map()
      this.byDevice.set(deviceId, buf)
      this.bytesByDevice.set(deviceId, 0)
    }
    // Approximate wire size from the encrypted payload (ciphertext) or the
    // plaintext payload fallback. Cheap and good enough for budgeting.
    const bytes = (msg.ciphertext?.length ?? msg.payload?.length ?? 0)
    buf.set(msg.seq, { msg, bytes })
    this.bytesByDevice.set(deviceId, (this.bytesByDevice.get(deviceId) ?? 0) + bytes)
    this.evict(deviceId, buf)
  }

  /** Evict oldest frames until within both bounds. */
  private evict(deviceId: string, buf: Map<number, { msg: WireMessage; bytes: number }>): void {
    let bytes = this.bytesByDevice.get(deviceId) ?? 0
    while (buf.size > this.maxMessages || bytes > this.maxBytes) {
      const oldestKey = buf.keys().next().value
      if (oldestKey === undefined) break
      const removed = buf.get(oldestKey)
      buf.delete(oldestKey)
      bytes -= removed?.bytes ?? 0
    }
    this.bytesByDevice.set(deviceId, Math.max(0, bytes))
  }

  /**
   * Return the buffered frames for `[fromSeq, toSeq]` (inclusive) in seq order.
   * `complete` is false when any seq in the range is no longer buffered (evicted
   * or never sent), so the caller can tell iOS to fall back to the reconcile.
   */
  range(deviceId: string, fromSeq: number, toSeq: number): { frames: WireMessage[]; complete: boolean } {
    const buf = this.byDevice.get(deviceId)
    if (!buf || fromSeq > toSeq) {
      if (fromSeq > toSeq) {
        warn(`range called with inverted bounds deviceId=${deviceId} fromSeq=${fromSeq} toSeq=${toSeq} — returning empty/incomplete`)
      }
      return { frames: [], complete: false }
    }
    const frames: WireMessage[] = []
    let complete = true
    for (let s = fromSeq; s <= toSeq; s++) {
      const entry = buf.get(s)
      if (entry) frames.push(entry.msg)
      else complete = false
    }
    return { frames, complete }
  }

  /** Drop all buffered frames for a device (on unpair / disconnect cleanup). */
  clearDevice(deviceId: string): void {
    this.byDevice.delete(deviceId)
    this.bytesByDevice.delete(deviceId)
  }
}

/**
 * Replay the buffered frames for `[fromSeq, toSeq]` to a device by handing each
 * to `deliver`. Returns whether the range was fully covered (every seq present
 * in the buffer). Extracted from RemoteTransport.resend to keep transport.ts
 * within the file-size cap; the transport supplies the per-frame delivery.
 */
export function replayRange(
  buffer: RetransmitBuffer,
  deviceId: string,
  fromSeq: number,
  toSeq: number,
  deliver: (frame: WireMessage) => void,
): boolean {
  const { frames, complete } = buffer.range(deviceId, fromSeq, toSeq)
  for (const frame of frames) deliver(frame)
  return complete
}
