/**
 * tab-slice — closeTab action-layer guard
 *
 * Pins the "no force-close" contract: an engine tab whose orchestrator
 * is running OR whose dispatched background agents are still
 * executing CANNOT be closed via the action layer. The user must
 * stop the tab first (Interrupt + wait for children) before close is
 * allowed.
 *
 * The guard is engine-tab-only — CLI tabs have no dispatched-agent
 * concept and continue to honor their existing close semantics
 * (Cmd+W → confirm → close even while `status === 'running'`).
 *
 * Mirrors the UI-layer suppression in TabStripTabPill.tsx (X button
 * hidden, middle-click no-op when closeBlocked). Together they
 * enforce the same rule at every entry point — keyboard shortcuts,
 * group-pill close, programmatic calls, future entry points.
 *
 * See plan: ~/.ion/plans/blue-studying-hill.md (deliverable 5,
 * "Hard-block tab close while running or awaiting children").
 */

import { describe, it, expect, beforeEach, vi } from 'vitest'

vi.mock('../../components/TerminalPanel', () => ({
  destroyTerminalInstance: vi.fn(),
}))

vi.mock('../session-store-helpers', () => ({
  makeLocalTab: vi.fn(),
  nextMsgId: vi.fn(() => 'msg-x'),
  playNotificationIfHidden: vi.fn(async () => {}),
  cancelDoneGroupMove: vi.fn(() => false),
  scheduleDoneGroupMove: vi.fn(),
  isBlankConversationTab: vi.fn(() => false),
}))

vi.mock('../../preferences', () => ({
  usePreferencesStore: {
    getState: vi.fn(() => ({
      tabGroups: [],
      tabGroupMode: 'off',
      stashedManualTabAssignments: {},
      setStashedManualTabAssignments: vi.fn(),
    })),
  },
}))

import { createTabSlice } from '../slices/tab-slice'
import type { State } from '../session-store-types'

const closeTabRpc = vi.fn().mockResolvedValue(undefined)

;(globalThis as any).window = {
  ion: {
    closeTab: closeTabRpc,
    gitWorktreeRemove: vi.fn().mockResolvedValue(undefined),
    terminalDestroy: vi.fn().mockResolvedValue(undefined),
  },
}

function makeEngineTab(id: string) {
  return {
    id,
    title: 'Engine Tab',
    customTitle: null,
    isEngine: true,
    engineProfileId: null,
    workingDirectory: '/tmp',
    hasChosenDirectory: true,
    status: 'idle',
    activeRequestId: null,
    lastEventAt: null,
    hasUnread: false,
    currentActivity: '',
    permissionQueue: [],
    permissionDenied: null,
    attachments: [],
    draftInput: '',
    messages: [],
    queuedPrompts: [],
    pillColor: null,
    pillIcon: null,
    forkedFromSessionId: null,
    hasFileActivity: false,
    worktree: null,
    pendingWorktreeSetup: false,
    groupId: null,
    groupPinned: false,
    bashExecuting: false,
    bashExecId: null,
    historicalSessionIds: [],
    lastKnownSessionId: null,
    additionalDirs: [],
    permissionMode: 'auto' as const,
    planFilePath: null,
    bashResults: [],
    contextTokens: null,
    contextPercent: null,
    isCompacting: false,
    isTerminalOnly: false,
    sessionModel: null,
    modelOverride: null,
    sessionTools: [],
    sessionMcpServers: [],
    sessionSkills: [],
    sessionVersion: null,
    conversationId: null,
    lastResult: null,
    lastMessagePreview: null,
  }
}

function makeCliTab(id: string) {
  return { ...makeEngineTab(id), isEngine: false, engineProfileId: undefined }
}

interface Harness {
  state: any
  slice: Partial<State>
  warnSpy: ReturnType<typeof vi.spyOn>
}

function buildHarness(tabs: any[]): Harness {
  const state: any = {
    tabs,
    enginePanes: new Map(),
    engineMessages: new Map(),
    engineAgentStates: new Map(),
    engineStatusFields: new Map(),
    engineWorkingMessages: new Map(),
    engineNotifications: new Map(),
    engineDialogs: new Map(),
    enginePinnedPrompt: new Map(),
    engineUsage: new Map(),
    engineDraftInputs: new Map(),
    engineModelOverrides: new Map(),
    engineConversationIds: new Map(),
    enginePermissionDenied: new Map(),
    enginePermissionModes: new Map(),
    terminalPanes: new Map(),
    terminalOpenTabIds: new Set(),
    fileExplorerOpenDirs: new Set(),
    fileEditorOpenDirs: new Set(),
    stashedManualTabAssignments: new Map(),
  }
  const set = (patch: any) => {
    if (typeof patch === 'function') Object.assign(state, patch(state))
    else Object.assign(state, patch)
  }
  const get = () => state
  const slice = createTabSlice(set, get) as Partial<State>
  // The action-layer guard emits via console.warn — capture so tests
  // can assert the refusal was logged.
  const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
  return { state, slice, warnSpy }
}

beforeEach(() => {
  closeTabRpc.mockClear()
})

describe('closeTab action-layer guard', () => {
  it('allows close when the engine tab is truly idle', () => {
    const tab = makeEngineTab('tab1')
    const h = buildHarness([tab])
    h.state.enginePanes.set('tab1', { instances: [{ id: 'inst1', label: 'inst1' }], activeInstanceId: 'inst1' })
    h.state.engineStatusFields.set('tab1:inst1', { state: 'idle' })

    h.slice.closeTab!('tab1')

    expect(closeTabRpc).toHaveBeenCalledWith('tab1')
    expect(h.warnSpy).not.toHaveBeenCalled()
    h.warnSpy.mockRestore()
  })

  it('refuses close when the orchestrator is running', () => {
    const tab = makeEngineTab('tab1')
    const h = buildHarness([tab])
    h.state.enginePanes.set('tab1', { instances: [{ id: 'inst1', label: 'inst1' }], activeInstanceId: 'inst1' })
    h.state.engineStatusFields.set('tab1:inst1', { state: 'running' })

    h.slice.closeTab!('tab1')

    expect(closeTabRpc).not.toHaveBeenCalled()
    expect(h.warnSpy).toHaveBeenCalledWith(expect.stringContaining('refused engine tab close'))
    expect(h.warnSpy).toHaveBeenCalledWith(expect.stringContaining('orchestratorRunning=true'))
    h.warnSpy.mockRestore()
  })

  it('refuses close when the orchestrator is connecting', () => {
    const tab = makeEngineTab('tab1')
    const h = buildHarness([tab])
    h.state.enginePanes.set('tab1', { instances: [{ id: 'inst1', label: 'inst1' }], activeInstanceId: 'inst1' })
    h.state.engineStatusFields.set('tab1:inst1', { state: 'connecting' })

    h.slice.closeTab!('tab1')

    expect(closeTabRpc).not.toHaveBeenCalled()
    expect(h.warnSpy).toHaveBeenCalled()
    h.warnSpy.mockRestore()
  })

  it('refuses close when the orchestrator is idle but background children are running', () => {
    const tab = makeEngineTab('tab1')
    const h = buildHarness([tab])
    h.state.enginePanes.set('tab1', { instances: [{ id: 'inst1', label: 'inst1' }], activeInstanceId: 'inst1' })
    h.state.engineStatusFields.set('tab1:inst1', { state: 'idle' })
    h.state.engineAgentStates.set('tab1:inst1', [
      { name: 'agent-a', status: 'running' },
      { name: 'agent-b', status: 'done' },
    ])

    h.slice.closeTab!('tab1')

    expect(closeTabRpc).not.toHaveBeenCalled()
    expect(h.warnSpy).toHaveBeenCalledWith(expect.stringContaining('refused engine tab close'))
    expect(h.warnSpy).toHaveBeenCalledWith(expect.stringContaining('inst1:1'))
    h.warnSpy.mockRestore()
  })

  it('refuses close when a sibling instance has running children', () => {
    const tab = makeEngineTab('tab1')
    const h = buildHarness([tab])
    h.state.enginePanes.set('tab1', {
      instances: [{ id: 'inst1', label: 'inst1' }, { id: 'inst2', label: 'inst2' }],
      activeInstanceId: 'inst1',
    })
    h.state.engineStatusFields.set('tab1:inst1', { state: 'idle' })
    h.state.engineStatusFields.set('tab1:inst2', { state: 'idle' })
    h.state.engineAgentStates.set('tab1:inst1', [])
    h.state.engineAgentStates.set('tab1:inst2', [{ name: 'agent-a', status: 'running' }])

    h.slice.closeTab!('tab1')

    expect(closeTabRpc).not.toHaveBeenCalled()
    expect(h.warnSpy).toHaveBeenCalled()
    h.warnSpy.mockRestore()
  })

  it('does NOT gate CLI tabs — the existing Cmd+W → confirm flow is preserved', () => {
    const tab = makeCliTab('tab1')
    tab.status = 'running'
    const h = buildHarness([tab])

    h.slice.closeTab!('tab1')

    expect(closeTabRpc).toHaveBeenCalledWith('tab1')
    expect(h.warnSpy).not.toHaveBeenCalled()
    h.warnSpy.mockRestore()
  })

  it('allows close once all children flip to terminal status', () => {
    const tab = makeEngineTab('tab1')
    const h = buildHarness([tab])
    h.state.enginePanes.set('tab1', { instances: [{ id: 'inst1', label: 'inst1' }], activeInstanceId: 'inst1' })
    h.state.engineStatusFields.set('tab1:inst1', { state: 'idle' })
    h.state.engineAgentStates.set('tab1:inst1', [
      { name: 'agent-a', status: 'done' },
      { name: 'agent-b', status: 'cancelled' },
      { name: 'agent-c', status: 'error' },
    ])

    h.slice.closeTab!('tab1')

    expect(closeTabRpc).toHaveBeenCalledWith('tab1')
    expect(h.warnSpy).not.toHaveBeenCalled()
    h.warnSpy.mockRestore()
  })
})
