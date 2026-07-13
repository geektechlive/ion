/**
 * engine-bridge-fs Tests
 *
 * Covers probeWorkingDir's three-way branch: 'ok' / 'missing' / 'unreachable'.
 * This is the regression coverage for the transport-vs-genuine-missing
 * conflation bug — see engine-control-plane.test.ts "remote directory
 * validation" for the consumer-side assertion on the resulting user message.
 */

import { vi, describe, it, expect, beforeEach } from 'vitest'

const mockBridge = vi.hoisted(() => ({
  request: vi.fn(),
  whenConnected: vi.fn(),
}))

// engine-bridge-fs reads the bridge singleton lazily through ./state (see
// the module's own comment on the import-cycle safety of that pattern).
// Mocking ./state directly avoids pulling in the full Electron app/tray/
// pairing module graph that the real singleton module imports.
vi.mock('../state', () => ({
  engineBridge: mockBridge,
}))

vi.mock('../logger', () => ({
  log: vi.fn(),
  debug: vi.fn(),
  warn: vi.fn(),
  error: vi.fn(),
}))

vi.mock('../engine-bridge', () => ({
  IS_REMOTE: true,
}))

import { probeWorkingDir } from '../engine-bridge-fs'

describe('probeWorkingDir', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('returns "ok" when the engine confirms the directory exists', async () => {
    mockBridge.request.mockResolvedValue({ ok: true, data: { path: '/tmp', entries: [], truncated: false, parent: '/' } })

    const status = await probeWorkingDir('/tmp')

    expect(status).toBe('ok')
    expect(mockBridge.whenConnected).not.toHaveBeenCalled()
  })

  it('returns "missing" when the engine replies ok:false with no transport flag', async () => {
    mockBridge.request.mockResolvedValue({ ok: false, error: 'no such file or directory' })

    const status = await probeWorkingDir('/nope')

    expect(status).toBe('missing')
    expect(mockBridge.whenConnected).not.toHaveBeenCalled()
  })

  it('returns "unreachable" after retrying once when every probe is transport-flagged', async () => {
    mockBridge.request.mockResolvedValue({ ok: false, error: 'Request timed out', transport: true })
    mockBridge.whenConnected.mockResolvedValue(false)

    const status = await probeWorkingDir('/tmp')

    expect(status).toBe('unreachable')
    expect(mockBridge.whenConnected).toHaveBeenCalledOnce()
    expect(mockBridge.request).toHaveBeenCalledTimes(2)
  })

  it('retries after reconnect and returns "ok" if the retry succeeds', async () => {
    mockBridge.request
      .mockResolvedValueOnce({ ok: false, error: 'Request timed out', transport: true })
      .mockResolvedValueOnce({ ok: true, data: { path: '/tmp', entries: [], truncated: false, parent: '/' } })
    mockBridge.whenConnected.mockResolvedValue(true)

    const status = await probeWorkingDir('/tmp')

    expect(status).toBe('ok')
    expect(mockBridge.whenConnected).toHaveBeenCalledOnce()
  })

  it('returns "unreachable" when the underlying request() rejects', async () => {
    mockBridge.request.mockRejectedValue(new Error('Remote engine is not reachable'))
    mockBridge.whenConnected.mockResolvedValue(false)

    const status = await probeWorkingDir('/tmp')

    expect(status).toBe('unreachable')
  })
})
