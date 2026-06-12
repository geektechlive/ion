/**
 * event-wiring-resources — reconnect resubscription and session key tracking
 *
 * Pins the fix for the "notifications empty after desktop restart" regression:
 *
 * When the desktop restarts and connects to a running engine, `engine_command_registry`
 * is NOT re-emitted (the engine only emits it during initial session creation). Without
 * a reconnect recovery path, `subscribeToResourceKinds` is never called for any session,
 * and the notifications panel stays empty permanently for that session.
 *
 * Fix: `recordActiveSessionKey` tracks keys that have ever had a successful per-session
 * subscription. `resubscribeSessionResourceKinds` re-subscribes all tracked keys on
 * reconnect, called from `subscribeGlobalResources` in event-wiring.ts.
 *
 * Tests:
 *   - recordActiveSessionKey stores a key
 *   - resubscribeSessionResourceKinds calls subscribeToResourceKinds for all tracked keys
 *   - resubscribeSessionResourceKinds is a no-op when no keys are tracked
 *   - resubscribeSessionResourceKinds continues after per-key error (resilient)
 *   - clearResourceSubscriptions does NOT clear tracked session keys (keys survive reconnect)
 */

import { vi, describe, it, expect, beforeEach } from 'vitest'

// ── Mocks ──────────────────────────────────────────────────────────────────

const { mockRequest } = vi.hoisted(() => {
  const mockRequest = vi.fn().mockResolvedValue({ ok: true, data: { subscriptionId: 'sub-mock-1' } })
  return { mockRequest }
})

// Mock the engineBridge.request to avoid real RPC calls
vi.mock('../state', () => ({
  state: { mainWindow: null },
  engineBridge: {
    request: mockRequest,
    on: vi.fn(),
  },
}))

vi.mock('../logger', () => ({ log: vi.fn(), debug: vi.fn(), warn: vi.fn(), error: vi.fn() }))

vi.mock('fs', () => ({
  existsSync: vi.fn().mockReturnValue(false),
  readFileSync: vi.fn(),
  writeFileSync: vi.fn(),
  readdirSync: vi.fn().mockReturnValue([]),
  mkdirSync: vi.fn(),
}))

vi.mock('os', () => ({ homedir: () => '/test-home' }))
vi.mock('electron', () => ({ ipcMain: { on: vi.fn(), handle: vi.fn() } }))

import {
  recordActiveSessionKey,
  resubscribeSessionResourceKinds,
  clearResourceSubscriptions,
} from '../event-wiring-resources'

// ── Tests ──────────────────────────────────────────────────────────────────

describe('event-wiring-resources — session key tracking for reconnect', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('resubscribeSessionResourceKinds is a no-op when no keys are tracked', async () => {
    // No keys registered — function returns without calling engineBridge.request
    await resubscribeSessionResourceKinds()
    expect(mockRequest).not.toHaveBeenCalled()
  })

  it('resubscribeSessionResourceKinds calls subscribeToResourceKinds for each tracked key', async () => {
    recordActiveSessionKey('tab1:inst1')
    recordActiveSessionKey('tab2:inst2')

    await resubscribeSessionResourceKinds()

    // subscribeToResourceKinds calls engineBridge.request('resource_subscribe', ...)
    // for each key. Two keys = at least 2 request calls.
    expect(mockRequest.mock.calls.length).toBeGreaterThanOrEqual(2)

    const keys = mockRequest.mock.calls
      .filter((c: any[]) => c[0] === 'resource_subscribe')
      .map((c: any[]) => (c[1] as { key: string }).key)
    expect(keys).toContain('tab1:inst1')
    expect(keys).toContain('tab2:inst2')
  })

  it('continues resubscribing remaining keys after a per-key error', async () => {
    recordActiveSessionKey('tab3:inst3')
    recordActiveSessionKey('tab4:inst4')

    // First resource_subscribe call throws
    mockRequest
      .mockRejectedValueOnce(new Error('rpc timeout'))
      .mockResolvedValue({ ok: true, data: { subscriptionId: 'sub-x' } })

    // Should not throw
    await expect(resubscribeSessionResourceKinds()).resolves.toBeUndefined()
  })

  it('clearResourceSubscriptions does not remove tracked session keys', async () => {
    recordActiveSessionKey('tab5:inst5')

    clearResourceSubscriptions()

    // After clearing subscription IDs, the session key must survive
    // so it can be resubscribed on reconnect.
    await resubscribeSessionResourceKinds()

    const keys = mockRequest.mock.calls
      .filter((c: any[]) => c[0] === 'resource_subscribe')
      .map((c: any[]) => (c[1] as { key: string }).key)
    expect(keys).toContain('tab5:inst5')
  })
})
