import { encrypt } from './crypto'
import { compressPayload } from './transport-compression'
import { log as _log } from '../logger'
import type { WireMessage } from './protocol'

function log(msg: string): void {
  _log('RemoteTransport', msg)
}

/**
 * Build a per-device wire frame: assign the next seq, compress, and encrypt
 * with the device secret (or fall back to plaintext when no 32-byte secret).
 * Returns null when encryption fails (the caller skips that device).
 *
 * Extracted from RemoteTransport so the per-device build is defined once and
 * shared by the broadcast path (_sendToAll) and the targeted path
 * (sendToDevice), keeping transport.ts within the file-size cap. `nextSeq`
 * is the transport's monotonic seq allocator (`() => ++this.seq`).
 */
export function buildDeviceFrame(
  deviceId: string,
  secret: Buffer,
  plaintext: string,
  nextSeq: () => number,
  push: boolean,
  pushTitle?: string,
  pushBody?: string,
): WireMessage | null {
  const wire = compressPayload(plaintext)
  const msg: WireMessage = { seq: nextSeq(), ts: Date.now(), deviceId } as WireMessage
  if (secret.length === 32) {
    try {
      const { nonce, ciphertext } = encrypt(wire, secret)
      ;(msg as any).nonce = nonce
      ;(msg as any).ciphertext = ciphertext
    } catch (err) {
      log(`encrypt failed for device ${deviceId}: ${(err as Error).message}`)
      return null
    }
  } else {
    ;(msg as any).payload = plaintext
  }
  ;(msg as any).push = push || undefined
  ;(msg as any).pushTitle = push ? pushTitle : undefined
  ;(msg as any).pushBody = push ? pushBody : undefined
  return msg
}
