/**
 * WI-004 desktop handler parity test (#259)
 *
 * Verifies that `handleLoadConversation` (the unified history handler) behaves
 * identically for plain tabs and extension-hosted tabs, and that the live
 * engine-state push fires based on runtime session status — not tab type.
 *
 * Coverage:
 *   1. Plain tab: messages returned, desktop_conversation_history sent, no
 *      live-state push when status is 'idle'.
 *   2. Extension-hosted tab: same behavior as plain tab for identical pane
 *      state (pagination, message shape, no live-state push when idle).
 *   3. Running session: live-state push fires for BOTH tab types when
 *      tabStatus is 'running'.
 *   4. Retired string: desktop_engine_conversation_history is never sent by
 *      handleLoadConversation.
 *   5. Pagination: both tab types respect the `before` cursor uniformly.
 */

import { vi, describe, it, expect, beforeEach } from 'vitest'

// ── Mocks ───────────────────────────────────────────────────────────────────

const sent: Array<{ deviceId: string | undefined; event: any }> = []
const sendToDeviceMock = vi.fn((deviceId: string, event: any) => { sent.push({ deviceId, event }) })
const sendMock = vi.fn((event: any) => { sent.push({ deviceId: undefined, event }) })

const executeJsMock = vi.fn()

vi.mock('../../../state', () => ({
  state: {
    get mainWindow() {
      return { webContents: { executeJavaScript: executeJsMock } }
    },
    get remoteTransport() {
      return { sendToDevice: sendToDeviceMock, send: sendMock }
    },
  },
  sessionPlane: {},
  engineBridge: {},
  activeAssistantMessages: new Map(),
  lastMessagePreview: new Map(),
  lastForwardedTabStatus: new Map(),
  extensionCommandRegistry: new Map(),
}))

vi.mock('../../../logger', () => ({ log: vi.fn() }))
vi.mock('../../../broadcast', () => ({ broadcast: vi.fn() }))
vi.mock('../../../terminal-manager-instance', () => ({ terminalManager: {} }))
vi.mock('../../../settings-store', () => ({ readSettings: vi.fn(() => ({})), readClaudeCompat: vi.fn(() => false) }))
vi.mock('../../snapshot', () => ({ getRemoteTabStates: vi.fn(async () => []) }))
vi.mock('./diagnostics', () => ({ autoPullDiagnosticLogs: vi.fn() }))
vi.mock('./tabs-sync', () => ({ broadcastSync: vi.fn(), sendSync: vi.fn() }))
vi.mock('../../../ipc-validation', () => ({ resolveDiscoveryWorkingDir: vi.fn() }))
vi.mock('./tabs-prompt', () => ({ handlePrompt: vi.fn(), handleCancel: vi.fn() }))

// ── Helpers ─────────────────────────────────────────────────────────────────

import { handleLoadConversation } from '../tabs'

/** Build an executeJavaScript mock result for the history IIFE. */
function makeHistoryResult(opts: {
  tabStatus?: string
  msgCount?: number
  toolName?: string
}) {
  const msgCount = opts.msgCount ?? 3
  const messages = Array.from({ length: msgCount }, (_, i) => ({
    id: `msg-${i}`,
    role: i % 2 === 0 ? 'user' : 'assistant',
    content: `message ${i}`,
    toolName: opts.toolName,
    toolInput: null,
    toolId: null,
    toolStatus: null,
    timestamp: 1000 + i,
    slashCommand: null,
    slashArgs: null,
    slashSource: null,
    attachments: [],
  }))
  return {
    messages,
    hasMore: false,
    cursor: undefined,
    total: msgCount,
    tabStatus: opts.tabStatus ?? 'idle',
  }
}

/** Build a live engine state snapshot result (for the second executeJavaScript call). */
function makeLiveStateResult() {
  return {
    instId: 'main',
    agents: [{ name: 'SubAgent', status: 'running' }],
    status: { contextPercent: 42 },
    working: 'Thinking...',
    modelOverride: null,
  }
}

// ── Tests ────────────────────────────────────────────────────────────────────

describe('handleLoadConversation — WI-004 unified handler', () => {
  beforeEach(() => {
    sent.length = 0
    sendToDeviceMock.mockClear()
    sendMock.mockClear()
    executeJsMock.mockReset()
  })

  // ── 1. Plain tab idle ────────────────────────────────────────────────────

  it('plain tab: sends desktop_conversation_history, no live-state push when idle', async () => {
    executeJsMock.mockResolvedValueOnce(makeHistoryResult({ tabStatus: 'idle' }))

    await handleLoadConversation({ type: 'desktop_load_conversation', tabId: 'tab-plain' }, 'device-1')

    const historyEvents = sent.filter(s => s.event.type === 'desktop_conversation_history')
    expect(historyEvents).toHaveLength(1)
    expect(historyEvents[0].event.tabId).toBe('tab-plain')
    expect(historyEvents[0].deviceId).toBe('device-1')

    // No live-state push when idle.
    const agentEvents = sent.filter(s => s.event.type === 'desktop_agent_state')
    expect(agentEvents).toHaveLength(0)
  })

  // ── 2. Extension-hosted tab idle — parity ────────────────────────────────

  it('extension-hosted tab: identical behavior to plain tab for idle state', async () => {
    executeJsMock.mockResolvedValueOnce(makeHistoryResult({ tabStatus: 'idle', msgCount: 3 }))

    await handleLoadConversation({ type: 'desktop_load_conversation', tabId: 'tab-ext' }, 'device-2')

    const historyEvents = sent.filter(s => s.event.type === 'desktop_conversation_history')
    expect(historyEvents).toHaveLength(1)
    expect(historyEvents[0].event.tabId).toBe('tab-ext')

    // No live-state push for idle extension-hosted tab either.
    expect(sent.filter(s => s.event.type === 'desktop_agent_state')).toHaveLength(0)
  })

  it('plain and extension-hosted tabs return the same event type and shape', async () => {
    // Plain tab
    executeJsMock.mockResolvedValueOnce(makeHistoryResult({ tabStatus: 'idle', msgCount: 2 }))
    await handleLoadConversation({ type: 'desktop_load_conversation', tabId: 'tab-plain-2' }, 'device-a')
    const plainEvent = sent.find(s => s.event.tabId === 'tab-plain-2')!

    sent.length = 0
    executeJsMock.mockReset()

    // Extension-hosted tab (same content)
    executeJsMock.mockResolvedValueOnce(makeHistoryResult({ tabStatus: 'idle', msgCount: 2 }))
    await handleLoadConversation({ type: 'desktop_load_conversation', tabId: 'tab-ext-2' }, 'device-b')
    const extEvent = sent.find(s => s.event.tabId === 'tab-ext-2')!

    // Same event type for both.
    expect(plainEvent.event.type).toBe('desktop_conversation_history')
    expect(extEvent.event.type).toBe('desktop_conversation_history')
    // Same field structure.
    expect(Object.keys(plainEvent.event).sort()).toEqual(Object.keys(extEvent.event).sort())
  })

  // ── 3. Running session: live-state push for BOTH tab types ───────────────

  it('running plain tab: live-state push fires', async () => {
    // First call: history IIFE returns tabStatus='running'
    executeJsMock.mockResolvedValueOnce(makeHistoryResult({ tabStatus: 'running' }))
    // Second call: live engine state snapshot
    executeJsMock.mockResolvedValueOnce(makeLiveStateResult())

    await handleLoadConversation({ type: 'desktop_load_conversation', tabId: 'tab-plain-run' }, 'device-3')

    const agentEvents = sent.filter(s => s.event.type === 'desktop_agent_state')
    expect(agentEvents).toHaveLength(1)
    expect(agentEvents[0].event.tabId).toBe('tab-plain-run')
    expect(agentEvents[0].deviceId).toBe('device-3')
  })

  it('running extension-hosted tab: live-state push fires (identical to plain)', async () => {
    executeJsMock.mockResolvedValueOnce(makeHistoryResult({ tabStatus: 'running' }))
    executeJsMock.mockResolvedValueOnce(makeLiveStateResult())

    await handleLoadConversation({ type: 'desktop_load_conversation', tabId: 'tab-ext-run' }, 'device-4')

    const agentEvents = sent.filter(s => s.event.type === 'desktop_agent_state')
    expect(agentEvents).toHaveLength(1)
    expect(agentEvents[0].event.tabId).toBe('tab-ext-run')
  })

  it('connecting status also triggers live-state push', async () => {
    executeJsMock.mockResolvedValueOnce(makeHistoryResult({ tabStatus: 'connecting' }))
    executeJsMock.mockResolvedValueOnce(makeLiveStateResult())

    await handleLoadConversation({ type: 'desktop_load_conversation', tabId: 'tab-connecting' }, 'device-5')

    expect(sent.filter(s => s.event.type === 'desktop_agent_state')).toHaveLength(1)
  })

  // ── 4. Retired string guard ──────────────────────────────────────────────

  it('never sends desktop_engine_conversation_history (retired string)', async () => {
    executeJsMock.mockResolvedValueOnce(makeHistoryResult({ tabStatus: 'idle' }))
    executeJsMock.mockResolvedValueOnce(makeHistoryResult({ tabStatus: 'running' }))
    executeJsMock.mockResolvedValueOnce(makeLiveStateResult())

    await handleLoadConversation({ type: 'desktop_load_conversation', tabId: 'tab-guard-1' }, 'device-6')
    await handleLoadConversation({ type: 'desktop_load_conversation', tabId: 'tab-guard-2' }, 'device-7')

    const retiredEvents = sent.filter(s => s.event.type === 'desktop_engine_conversation_history')
    expect(retiredEvents).toHaveLength(0)
  })

  // ── 5. Pagination: both tab types respect the before cursor ──────────────

  it('pagination: before cursor is passed uniformly regardless of tab type', async () => {
    // The handler passes `before` to the IIFE. We verify executeJavaScript is
    // called and the history returned uses the correct slice.
    const historyResult = makeHistoryResult({ tabStatus: 'idle', msgCount: 2 })
    executeJsMock.mockResolvedValueOnce(historyResult)

    await handleLoadConversation(
      { type: 'desktop_load_conversation', tabId: 'tab-page', before: 'msg-5' },
      'device-8'
    )

    // executeJavaScript was called (pagination IIFE ran).
    expect(executeJsMock).toHaveBeenCalledTimes(1)
    // The IIFE string must contain the before cursor value.
    const iifeArg = executeJsMock.mock.calls[0][0] as string
    expect(iifeArg).toContain('msg-5')

    // Response carries hasMore from the result.
    const histEvent = sent.find(s => s.event.type === 'desktop_conversation_history')!
    expect(histEvent.event.hasMore).toBe(false)
  })
})
