/**
 * Regression test for the planFilePath carry-through bug in
 * handleLoadConversation (commit: fix(desktop): carry planFilePath through
 * handleLoadConversation).
 *
 * The history IIFE that reads messages out of the renderer store dropped
 * `planFilePath`, so plan-lifecycle divider system messages (Plan created /
 * Plan updated / Implementing plan) lost their clickable slug after a history
 * reload on iOS. This test pins two things:
 *
 *   1. The IIFE source string carries planFilePath through the store mapper.
 *   2. A plan-divider row's planFilePath survives to the wire response.
 */

import { vi, describe, it, expect, beforeEach } from 'vitest'

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

import { handleLoadConversation } from '../tabs'

describe('handleLoadConversation — planFilePath carry-through', () => {
  beforeEach(() => {
    sent.length = 0
    sendToDeviceMock.mockClear()
    sendMock.mockClear()
    executeJsMock.mockReset()
  })

  it('the store-read IIFE maps planFilePath through', async () => {
    executeJsMock.mockResolvedValueOnce({ messages: [], hasMore: false, total: 0, tabStatus: 'idle' })
    await handleLoadConversation({ type: 'desktop_load_conversation', tabId: 'tab-x' }, 'dev-1')
    const iife = executeJsMock.mock.calls[0][0] as string
    expect(iife).toContain('planFilePath: m.planFilePath')
  })

  it('carries planFilePath to the wire response for a plan-divider row', async () => {
    executeJsMock.mockResolvedValueOnce({
      messages: [
        { id: 'm0', role: 'user', content: 'go', timestamp: 1, attachments: [] },
        {
          id: 'm1', role: 'system', content: '── Plan created at 3:00 PM · plan ──',
          timestamp: 2, planFilePath: '/test/plan.md', attachments: [],
        },
      ],
      hasMore: false,
      total: 2,
      tabStatus: 'idle',
    })

    await handleLoadConversation({ type: 'desktop_load_conversation', tabId: 'tab-plan' }, 'dev-2')

    const hist = sent.find((s) => s.event.type === 'desktop_conversation_history')!
    const divider = hist.event.messages.find((m: any) => m.role === 'system')
    expect(divider.planFilePath).toBe('/test/plan.md')
  })
})
