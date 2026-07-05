/**
 * event-wiring-remote — engine-stream duplicate-envelope suppression
 *
 * Regression for the iOS Remote incoming-duplication bug. Post-#256 every
 * conversation is engine-backed with a BARE session key, so the EngineControlPlane
 * matches the key and re-emits engine events onto the sessionPlane as normalized
 * `text_chunk` / `tool_call` / `tool_call_update` / `tool_result` events. The
 * generic engine forwarder (wireEngineBridgeEvents in event-wiring.ts) ALSO
 * forwards those same engine events to iOS as the structured `desktop_text_delta`
 * / `desktop_tool_start` / `desktop_tool_end` wire events. iOS appends a row from
 * the structured path, so the sessionPlane forwarder must NOT also mirror them as
 * `desktop_message_added` / `desktop_message_updated` — doing so appended a SECOND
 * assistant row / tool row on iOS (the live-only duplication that healed on a
 * history reload).
 *
 * These tests pin that the streaming branches emit NO message envelopes, while
 * the non-duplicated branches (task_complete permission-denial forwarding,
 * compaction system message — neither of which the generic forwarder produces in
 * an iOS-decodable shape) still fire. They go RED if the duplicate
 * desktop_message_added / desktop_message_updated sends are reintroduced.
 */

import { vi, describe, it, expect, beforeEach } from 'vitest'
import { EventEmitter } from 'events'

vi.mock('electron', () => ({ app: { getPath: vi.fn() }, ipcMain: { on: vi.fn(), handle: vi.fn() } }))

const { mockSend, mockState, sessionPlaneEmitter } = vi.hoisted(() => {
  const mockSend = vi.fn()
  const mockState = { remoteTransport: { send: mockSend } as any, mainWindow: null }
  const sessionPlaneEmitter = new (require('events').EventEmitter)()
  return { mockSend, mockState, sessionPlaneEmitter }
})

vi.mock('../state', () => ({
  state: mockState,
  sessionPlane: sessionPlaneEmitter,
  activeAssistantMessages: new Map(),
  lastMessagePreview: new Map<string, string>(),
}))

// normalizedToRemote returns null so the top-of-listener send is suppressed in
// this test — we are exercising ONLY the switch branches' message envelopes.
vi.mock('../remote/protocol', () => ({ normalizedToRemote: vi.fn(() => null) }))
vi.mock('../../shared/clear-divider', () => ({ formatClearDivider: vi.fn(() => '[clear]') }))
vi.mock('../logger', () => ({ log: vi.fn(), debug: vi.fn(), warn: vi.fn(), error: vi.fn() }))

import { wireRemoteSessionPlaneForwarding } from '../event-wiring-remote'

function sentOfType(type: string) {
  return mockSend.mock.calls.filter((c) => (c[0] as any)?.type === type)
}

describe('wireRemoteSessionPlaneForwarding — no duplicate message envelopes for engine stream', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ;(sessionPlaneEmitter as EventEmitter).removeAllListeners()
    mockState.remoteTransport = { send: mockSend } as any
    wireRemoteSessionPlaneForwarding()
  })

  it('does NOT send desktop_message_added for a text_chunk (structured path owns it)', () => {
    sessionPlaneEmitter.emit('event', 'tab1', { type: 'text_chunk', text: 'hello' })
    expect(sentOfType('desktop_message_added')).toHaveLength(0)
    expect(sentOfType('desktop_message_updated')).toHaveLength(0)
  })

  it('does NOT send a second envelope when a text_chunk extends an in-flight assistant message', () => {
    sessionPlaneEmitter.emit('event', 'tab1', { type: 'text_chunk', text: 'hello' })
    sessionPlaneEmitter.emit('event', 'tab1', { type: 'text_chunk', text: ' world' })
    expect(sentOfType('desktop_message_added')).toHaveLength(0)
    expect(sentOfType('desktop_message_updated')).toHaveLength(0)
  })

  it('does NOT send desktop_message_added(tool) for a tool_call (desktop_tool_start owns it)', () => {
    sessionPlaneEmitter.emit('event', 'tab1', { type: 'tool_call', toolName: 'Bash', toolId: 'toolu_1', index: 0 })
    expect(sentOfType('desktop_message_added')).toHaveLength(0)
  })

  it('does NOT send desktop_message_updated for a tool_call_update', () => {
    sessionPlaneEmitter.emit('event', 'tab1', { type: 'tool_call_update', toolId: 'toolu_1', partialInput: '{"a":1}' })
    expect(sentOfType('desktop_message_updated')).toHaveLength(0)
  })

  it('does NOT send desktop_message_updated for a tool_result (desktop_tool_end owns it)', () => {
    sessionPlaneEmitter.emit('event', 'tab1', { type: 'tool_result', toolId: 'toolu_1', content: 'ok', isError: false })
    expect(sentOfType('desktop_message_updated')).toHaveLength(0)
  })

  it('STILL sends the compaction system message (generic forwarder does not produce an iOS-decodable one)', () => {
    sessionPlaneEmitter.emit('event', 'tab1', {
      type: 'compacting',
      active: false,
      messagesBefore: 40,
      messagesAfter: 5,
      summary: 'did stuff',
      strategy: 'summarize',
    })
    const added = sentOfType('desktop_message_added')
    expect(added).toHaveLength(1)
    expect(added[0][0].message.role).toBe('system')
    expect(added[0][0].message.content).toContain('Compaction')
  })
})
