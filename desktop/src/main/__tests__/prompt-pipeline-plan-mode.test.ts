/**
 * Tests for the plan→auto permission-mode guard in the prompt pipeline.
 *
 * Guards the behaviour of `isFirstPromptForTab` (slash-classify.ts): the
 * pipeline must auto-switch from plan→auto ONLY on the very first prompt of
 * a fresh tab, and must preserve plan mode in all other scenarios:
 *
 *   - conversation already active (promptCount > 0)
 *   - same-boot mid-session (engine-side TabEntry.conversationId populated)
 *   - resumed session (runOptions.sessionId carries the prior conversationId)
 *
 * The resumed-session case is the most subtle: after an app restart the
 * engine-side TabEntry.conversationId is null (the engine hasn't started
 * yet), but the renderer sends runOptions.sessionId with the saved
 * conversationId. Without checking runOptions.sessionId the guard would
 * see promptCount=0 and conversationId=null and incorrectly allow the switch.
 *
 * Uses the same vi.hoisted() + vi.mock('../state') pattern as
 * prompt-pipeline.test.ts. Split into a companion file to keep both files
 * under the 600-line cap.
 */

import { vi, describe, it, expect, beforeEach } from 'vitest'

// ───────────────────────────────────────────────────────────────────────────
// Mocks — same pattern as prompt-pipeline.test.ts.
// ───────────────────────────────────────────────────────────────────────────

const mocks = vi.hoisted(() => {
  const bridgeListeners = new Map<string, Array<(key: string, event: any) => void>>()
  const sendCommandMock = (globalThis as any).vi?.fn?.() ?? function () {}
  const sendPromptMock = (globalThis as any).vi?.fn?.()?.mockResolvedValue?.({ ok: true }) ?? function () { return Promise.resolve({ ok: true }) }
  const submitPromptMock = (globalThis as any).vi?.fn?.()?.mockResolvedValue?.(undefined) ?? function () { return Promise.resolve() }
  const setPermissionModeMock = (globalThis as any).vi?.fn?.() ?? function () {}
  const remoteSendMock = (globalThis as any).vi?.fn?.() ?? function () {}
  const executeJsMock = (globalThis as any).vi?.fn?.()?.mockResolvedValue?.(null) ?? function () { return Promise.resolve(null) }
  const broadcastMock = (globalThis as any).vi?.fn?.() ?? function () {}
  const expandSlashMock = (globalThis as any).vi?.fn?.() ?? function () {}
  const clearConversationFileMock = (globalThis as any).vi?.fn?.()?.mockResolvedValue?.(undefined) ?? function () { return Promise.resolve() }
  const getTabStatusMock = (globalThis as any).vi?.fn?.()?.mockReturnValue?.({ promptCount: 0, conversationId: null }) ?? function () { return { promptCount: 0, conversationId: null } }
  return {
    bridgeListeners,
    sendCommandMock,
    sendPromptMock,
    submitPromptMock,
    setPermissionModeMock,
    remoteSendMock,
    executeJsMock,
    broadcastMock,
    expandSlashMock,
    clearConversationFileMock,
    getTabStatusMock,
  }
})

mocks.sendCommandMock = vi.fn()
mocks.sendPromptMock = vi.fn().mockResolvedValue({ ok: true })
mocks.submitPromptMock = vi.fn().mockResolvedValue(undefined)
mocks.setPermissionModeMock = vi.fn()
mocks.remoteSendMock = vi.fn()
mocks.executeJsMock = vi.fn().mockResolvedValue(null)
mocks.broadcastMock = vi.fn()
mocks.expandSlashMock = vi.fn().mockResolvedValue({ expanded: false })
mocks.clearConversationFileMock = vi.fn().mockResolvedValue(undefined)
mocks.getTabStatusMock = vi.fn().mockReturnValue({ promptCount: 0, conversationId: null })

function emitBridgeEvent(key: string, event: any): void {
  const arr = mocks.bridgeListeners.get('event') ?? []
  for (const fn of arr) fn(key, event)
}

vi.mock('../state', () => {
  const mockEngineBridge = {
    sendCommand: (...args: any[]) => mocks.sendCommandMock(...args),
    sendPrompt: (...args: any[]) => mocks.sendPromptMock(...args),
    clearConversationFile: (...args: any[]) => mocks.clearConversationFileMock(...args),
    on: (name: string, fn: (key: string, event: any) => void) => {
      const arr = mocks.bridgeListeners.get(name) ?? []
      arr.push(fn)
      mocks.bridgeListeners.set(name, arr)
    },
  }
  return {
    state: {
      mainWindow: { webContents: { executeJavaScript: (...args: any[]) => mocks.executeJsMock(...args) } },
      remoteTransport: { send: (...args: any[]) => mocks.remoteSendMock(...args) },
    },
    sessionPlane: {
      submitPrompt: (...args: any[]) => mocks.submitPromptMock(...args),
      setPermissionMode: (...args: any[]) => mocks.setPermissionModeMock(...args),
      getTabStatus: (...args: any[]) => mocks.getTabStatusMock(...args),
    },
    engineBridge: mockEngineBridge,
    extensionCommandRegistry: new Map(),
  }
})

vi.mock('../broadcast', () => ({
  broadcast: (...args: any[]) => mocks.broadcastMock(...args),
}))

vi.mock('../logger', () => ({
  log: vi.fn(),
  debug: vi.fn(),
  warn: vi.fn(),
  error: vi.fn(),
}))

vi.mock('../cli-compat/slash-expand', () => ({
  expandSlashCommand: (...args: any[]) => mocks.expandSlashMock(...args),
}))

vi.mock('../settings-store', () => ({
  readSettings: () => ({ enableClaudeCompat: true }),
  SETTINGS_DEFAULTS: { enableClaudeCompat: true },
}))

vi.mock('../remote/attachment-encoder', () => ({
  encodeImageAttachments: (text: string, _atts: any[]) => ({ encoded: [], rewrittenText: text }),
}))

import { processIncomingPrompt } from '../prompt-pipeline'
import { _resetAwaitersForTests } from '../command-await'

// ───────────────────────────────────────────────────────────────────────────
// Shared setup — engine reports unknown_command so the pipeline falls
// through to .md expansion on every test.
// ───────────────────────────────────────────────────────────────────────────

beforeEach(() => {
  mocks.sendCommandMock.mockReset()
  mocks.sendPromptMock.mockReset().mockResolvedValue({ ok: true })
  mocks.submitPromptMock.mockReset().mockResolvedValue(undefined)
  mocks.setPermissionModeMock.mockReset()
  mocks.remoteSendMock.mockReset()
  mocks.executeJsMock.mockReset().mockResolvedValue(null)
  mocks.broadcastMock.mockReset()
  mocks.expandSlashMock.mockReset().mockResolvedValue({ expanded: true, systemPrompt: 'sys', userPrompt: 'expanded' })
  mocks.clearConversationFileMock.mockReset().mockResolvedValue(undefined)
  mocks.getTabStatusMock.mockReset().mockReturnValue({ promptCount: 0, conversationId: null })
  mocks.bridgeListeners.clear()
  _resetAwaitersForTests()
  // Default: engine returns unknown_command so .md expansion is reached.
  mocks.sendCommandMock.mockImplementation((key: string, command: string) => {
    setTimeout(() => emitBridgeEvent(key, { type: 'engine_command_result', command, commandError: 'unknown_command', message: `unknown command: ${command}` }), 0)
  })
})

// ───────────────────────────────────────────────────────────────────────────
// Tests
// ───────────────────────────────────────────────────────────────────────────

describe('plan→auto guard: isFirstPromptForTab', () => {
  it('does NOT auto-switch when conversation is already active (promptCount > 0)', async () => {
    // The engine-side TabEntry.promptCount is > 0: at least one prompt has
    // already been submitted in this app boot, so the conversation is live.
    // The guard must preserve plan mode regardless of the template expanding.
    mocks.getTabStatusMock.mockReturnValue({ promptCount: 1, conversationId: 'conv-123' })
    await processIncomingPrompt({
      tabId: 'tab-1',
      text: '/ion--review 138',
      reqId: 'req-guard-a',
      source: 'desktop',
      isEngineTab: false,
      runOptions: { prompt: '/ion--review 138' } as any,
    })
    expect(mocks.setPermissionModeMock).not.toHaveBeenCalled()
  })

  it('does NOT auto-switch when engine-side conversationId is set (same-boot mid-session)', async () => {
    // After engine_status fires the TabEntry.conversationId is populated.
    // promptCount may still be 0 if the session was started by something
    // other than submitPrompt (e.g. engine warmup). Guard must block.
    mocks.getTabStatusMock.mockReturnValue({ promptCount: 0, conversationId: 'live-conv' })
    await processIncomingPrompt({
      tabId: 'tab-1',
      text: '/ion--review 138',
      reqId: 'req-guard-b',
      source: 'desktop',
      isEngineTab: false,
      runOptions: { prompt: '/ion--review 138' } as any,
    })
    expect(mocks.setPermissionModeMock).not.toHaveBeenCalled()
  })

  it('does NOT auto-switch when tab is a resumed session (runOptions.sessionId set)', async () => {
    // Primary guard for the app-restart scenario: after a restart the
    // engine-side TabEntry is a fresh makeEmptyTab (conversationId=null,
    // promptCount=0), but the renderer sends runOptions.sessionId with the
    // prior conversation id so the engine can resume from disk.
    //
    // Without checking runOptions.sessionId the guard would see
    // promptCount=0 + conversationId=null and incorrectly allow the switch,
    // silently pulling a plan-mode tab into auto mode before the first prompt.
    mocks.getTabStatusMock.mockReturnValue({ promptCount: 0, conversationId: null })
    await processIncomingPrompt({
      tabId: 'tab-1',
      text: '/ion--review 138',
      reqId: 'req-guard-c',
      source: 'desktop',
      isEngineTab: false,
      runOptions: { prompt: '/ion--review 138', sessionId: 'restored-conv-id' } as any,
    })
    expect(mocks.setPermissionModeMock).not.toHaveBeenCalled()
  })
})
