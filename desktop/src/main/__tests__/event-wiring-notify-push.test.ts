/**
 * event-wiring — engine_notification push-contract forwarding
 *
 * Regression test for the dropped-push bug: ctx.notify() emits an
 * engine_notification EngineEvent with `push: true, pushTitle, pushBody`
 * (engine/internal/types/engine_event.go, mirrored in
 * shared/types-engine-event.ts) — the engine's BroadcastNotification sets
 * these expecting the relay to fire APNs when the mobile peer is absent
 * (relay/relay.go only pushes when the forwarded frame's `push` flag is
 * set). The generic engine-event forwarder in `wireEngineBridgeEvents`
 * (event-wiring.ts) previously called `state.remoteTransport.send(envelope)`
 * unconditionally for every engine_* event, defaulting `push` to false and
 * silently dropping that contract — reminders, briefings, and critical
 * findings only ever reached the phone when the app was open and connected.
 *
 * The fix inspects `event.push === true` for engine_notification and calls
 * `send(envelope, true, { title, body })` so the relay's offline-peer APNs
 * path fires. All other event types (and engine_notification with
 * push:false/undefined) continue through the unconditional `send(envelope)`
 * path with push=false.
 *
 * Harness mirrors event-wiring-generic-wire-type.test.ts (same vi.hoisted
 * mock block, same captured `engineBridge.'event'` handler, same
 * `sentOfType` helper).
 */

import { vi, describe, it, expect, beforeEach } from 'vitest'

vi.mock('electron', () => ({ app: { getPath: vi.fn() }, ipcMain: { on: vi.fn(), handle: vi.fn() } }))

const {
  mockSend,
  mockState,
  mockPermDenialSet,
  mockLastStatusMap,
  capturedHandler,
  mockShouldStream,
} = vi.hoisted(() => {
  const mockSend = vi.fn()
  const mockState = {
    remoteTransport: { send: mockSend } as any,
    mainWindow: null,
  }
  const mockPermDenialSet = new Set<string>()
  const mockLastStatusMap = new Map<string, string>()
  const capturedHandler = { fn: null as ((key: string, event: any) => void) | null }
  const mockShouldStream = vi.fn(() => true)
  return {
    mockSend,
    mockState,
    mockPermDenialSet,
    mockLastStatusMap,
    capturedHandler,
    mockShouldStream,
  }
})

vi.mock('../state', () => ({
  state: mockState,
  sessionPlane: { on: vi.fn(), emit: vi.fn(), notifyConversationCleared: vi.fn() },
  engineBridge: {
    on: vi.fn((event: string, handler: any) => {
      if (event === 'event') capturedHandler.fn = handler
    }),
    sendReconcileState: vi.fn(),
  },
  activeAssistantMessages: new Map(),
  lastMessagePreview: new Map(),
  extensionCommandRegistry: new Map(),
  forwardedEnginePermissionDenials: mockPermDenialSet,
  lastForwardedTabStatus: mockLastStatusMap,
}))

vi.mock('../broadcast', () => ({ broadcast: vi.fn() }))
vi.mock('../settings-store', () => ({
  currentBackend: 'test',
  shouldStreamThinkingToRemote: mockShouldStream,
}))
vi.mock('../logger', () => ({ log: vi.fn(), debug: vi.fn(), warn: vi.fn(), error: vi.fn() }))
vi.mock('../../shared/clear-divider', () => ({ formatClearDivider: vi.fn(() => '[clear]') }))

import { wireEngineBridgeEvents } from '../event-wiring'

function emit(key: string, event: any): void {
  capturedHandler.fn!(key, event)
}

/** All forwarded wire messages whose type matches `wireType`. */
function sentOfType(wireType: string) {
  return mockSend.mock.calls.filter((c) => c[0]?.type === wireType)
}

// Compound key (`tabId:instanceId`) — the forwarder splits it for the
// tabId/instanceId ride-along.
const KEY = 'tab1:inst1'

describe('wireEngineBridgeEvents — engine_notification push contract', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    capturedHandler.fn = null
    mockState.remoteTransport = { send: mockSend } as any
    mockPermDenialSet.clear()
    mockLastStatusMap.clear()
    mockShouldStream.mockReturnValue(true)
    wireEngineBridgeEvents()
  })

  it('forwards engine_notification with push:true as push=true with title/body pushMeta', () => {
    emit(KEY, {
      type: 'engine_notification',
      push: true,
      pushTitle: 'Reminder',
      pushBody: 'Call the vet at 3pm',
      notifyKind: 'reminder',
      notifyTitle: 'Reminder',
      notifyBody: 'Call the vet at 3pm',
    })

    const sent = sentOfType('desktop_notification')
    expect(sent).toHaveLength(1)
    const [envelope, push, pushMeta] = sent[0]
    expect(envelope.tabId).toBe('tab1')
    expect(envelope.instanceId).toBe('inst1')
    expect(push).toBe(true)
    expect(pushMeta).toEqual({ title: 'Reminder', body: 'Call the vet at 3pm' })
  })

  it('falls back to notifyTitle/notifyBody when pushTitle/pushBody are empty', () => {
    emit(KEY, {
      type: 'engine_notification',
      push: true,
      pushTitle: '',
      pushBody: '',
      notifyKind: 'briefing',
      notifyTitle: 'New briefing ready',
      notifyBody: 'Your daily summary is available.',
    })

    const sent = sentOfType('desktop_notification')
    expect(sent).toHaveLength(1)
    const [, push, pushMeta] = sent[0]
    expect(push).toBe(true)
    expect(pushMeta).toEqual({ title: 'New briefing ready', body: 'Your daily summary is available.' })
  })

  it('forwards engine_notification with push:false as push=false with no pushMeta', () => {
    emit(KEY, {
      type: 'engine_notification',
      push: false,
      notifyKind: 'briefing',
      notifyTitle: 'New briefing ready',
      notifyBody: 'Your daily summary is available.',
    })

    const sent = sentOfType('desktop_notification')
    expect(sent).toHaveLength(1)
    const [, push, pushMeta] = sent[0]
    expect(push).toBeUndefined()
    expect(pushMeta).toBeUndefined()
  })

  it('forwards engine_notification with push undefined as push=false', () => {
    emit(KEY, {
      type: 'engine_notification',
      notifyKind: 'task_complete',
      notifyTitle: 'Task finished',
      notifyBody: 'The analysis run completed.',
    })

    const sent = sentOfType('desktop_notification')
    expect(sent).toHaveLength(1)
    const [, push, pushMeta] = sent[0]
    expect(push).toBeUndefined()
    expect(pushMeta).toBeUndefined()
  })

  it('leaves unrelated event types at push=false', () => {
    emit(KEY, { type: 'engine_status', fields: { state: 'running' } })

    const sent = sentOfType('desktop_status')
    expect(sent).toHaveLength(1)
    const [, push, pushMeta] = sent[0]
    expect(push).toBeUndefined()
    expect(pushMeta).toBeUndefined()
  })
})
