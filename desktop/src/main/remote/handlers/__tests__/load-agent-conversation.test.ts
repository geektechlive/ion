/**
 * Handler test for handleLoadAgentConversation.
 *
 * Verifies straight pass-through: all messages from the engine
 * conversation are forwarded to iOS without slicing.
 */
import { vi, describe, it, expect, beforeEach } from 'vitest'

// ── Mocks ───────────────────────────────────────────────────────────────────

const sent: Array<{ deviceId: string; event: any }> = []
const sendToDeviceMock = vi.fn((deviceId: string, event: any) => { sent.push({ deviceId, event }) })

const getConversationMock = vi.fn()

vi.mock('../../../logger', () => ({ log: vi.fn() }))
vi.mock('../../../state', () => ({
  state: {
    get mainWindow() {
      return { webContents: { executeJavaScript: executeJsMock } }
    },
    get remoteTransport() {
      return { sendToDevice: sendToDeviceMock }
    },
  },
  engineBridge: {
    getConversation: (...args: any[]) => getConversationMock(...args),
  },
}))

const executeJsMock = vi.fn()

// ── Import under test ───────────────────────────────────────────────────────

import { handleLoadAgentConversation } from '../engine'

// ── Tests ───────────────────────────────────────────────────────────────────

describe('handleLoadAgentConversation', () => {
  beforeEach(() => {
    sent.length = 0
    sendToDeviceMock.mockClear()
    executeJsMock.mockReset()
    getConversationMock.mockReset()
  })

  it('passes all messages through without slicing', async () => {
    getConversationMock.mockResolvedValue({
      messages: [
        { id: 'm1', role: 'assistant', content: 'first', timestamp: 100_000 },
        { id: 'm2', role: 'assistant', content: 'second', timestamp: 200_000 },
        { id: 'm3', role: 'assistant', content: 'third', timestamp: 300_000 },
      ],
    })

    executeJsMock.mockResolvedValue({ name: 'test-agent' })

    await handleLoadAgentConversation(
      { type: 'desktop_load_agent_conversation', conversationIds: ['conv-1'] },
      'device-1'
    )

    expect(sent).toHaveLength(1)
    const event = sent[0].event
    expect(event.type).toBe('desktop_agent_conversation_history')
    expect(event.agentName).toBe('test-agent')

    const msgIds = event.messages.map((m: any) => m.id)
    expect(msgIds).toEqual(['m1', 'm2', 'm3'])
  })

  it('returns all messages when no dispatches found', async () => {
    getConversationMock.mockResolvedValue({
      messages: [
        { id: 'm1', role: 'assistant', content: 'text', timestamp: 100_000 },
      ],
    })

    executeJsMock.mockResolvedValue({ name: 'legacy-agent' })

    await handleLoadAgentConversation(
      { type: 'desktop_load_agent_conversation', conversationIds: ['conv-legacy'] },
      'device-1'
    )

    expect(sent[0].event.messages).toHaveLength(1)
  })

  it('echoes conversationId for single-dispatch requests', async () => {
    getConversationMock.mockResolvedValue({
      messages: [
        { id: 'm1', role: 'assistant', content: 'ok', timestamp: 100_000 },
      ],
    })

    executeJsMock.mockResolvedValue({ name: 'agent-a' })

    await handleLoadAgentConversation(
      { type: 'desktop_load_agent_conversation', conversationIds: ['conv-abc'] },
      'device-1'
    )

    expect(sent[0].event.conversationId).toBe('conv-abc')
  })
})
