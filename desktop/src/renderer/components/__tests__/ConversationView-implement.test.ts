/**
 * runHandleImplement — the desktop Implement-button flow.
 *
 * Regression test for the plan-mode stale-parent defect: clicking Implement on
 * a PLAIN (non-engine) tab in plan mode must flip the AUTHORITATIVE permission
 * mode to 'auto'. For a plain tab the authoritative field is the parent
 * `tab.permissionMode` (effectivePermissionMode reads the parent for plain
 * tabs); the instance field is a ghost. The previous implementation wrote only
 * the instance, leaving the parent stuck on 'plan' so the very next submit()
 * re-asserted plan mode via the prompt_sync path — the implement run then
 * executed in plan mode.
 *
 * This test reuses the manual store harness (real tab-slice + send-slice) so
 * the production setPermissionMode action and effectivePermissionMode resolver
 * run for real. runHandleImplement reaches the store through the mocked
 * ../stores/sessionStore module below, which returns the harness store.
 *
 * Revert contract: restore the instance-only write in runHandleImplement and
 * the "parent reads 'auto'" assertion goes red — the parent stays 'plan'.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest'

// ── module-level mocks ────────────────────────────────────────────────────────

vi.mock('../../components/TerminalPanel', () => ({ destroyTerminalInstance: vi.fn() }))

// session-store-helpers constructs `new Audio()` at module load; stub the
// helpers the slices actually use so importing send-slice/tab-slice doesn't
// touch the DOM Audio API under jsdom-less test env.
vi.mock('../../stores/session-store-helpers', () => ({
  nextMsgId: vi.fn(() => `msg-${Math.random()}`),
  playNotificationIfHidden: vi.fn(async () => {}),
  cancelDoneGroupMove: vi.fn(() => false),
  scheduleDoneGroupMove: vi.fn(),
  makeLocalTab: vi.fn(),
  initialModelOverride: vi.fn(() => null),
}))

vi.mock('../../preferences', () => ({
  usePreferencesStore: {
    getState: vi.fn(() => ({
      autoGroupMovement: false,
      tabGroupMode: 'manual',
      planningGroupId: 'group-planning',
      inProgressGroupId: 'group-inprogress',
      doneGroupId: 'group-done',
      preferredModel: null,
      defaultPermissionMode: 'auto' as const,
      planModelSplitEnabled: false,
      planModeModel: null,
      implementModeModel: null,
      thinkingEnabled: false,
      engineProfiles: [],
      engineDefaultModel: null,
      tabGroups: [{ id: 'group-default', label: 'Default', isDefault: true, order: 0 }],
    })),
  },
}))

// The harness store is created per-test; this holder lets the sessionStore
// mock forward getState/setState to whichever harness the current test built.
const storeHolder: { current: any } = { current: null }
vi.mock('../../stores/sessionStore', () => ({
  useSessionStore: {
    getState: () => storeHolder.current,
    setState: (updater: any) => {
      const patch = typeof updater === 'function' ? updater(storeHolder.current) : updater
      Object.assign(storeHolder.current, patch)
    },
  },
}))

import { runHandleImplement } from '../ConversationView-implement'
import { createSendSlice } from '../../stores/slices/send-slice'
import { createTabSlice } from '../../stores/slices/tab-slice'
import { effectivePermissionMode } from '../../stores/conversation-instance'
import type { State } from '../../stores/session-store-types'
import type { TabState } from '../../../shared/types'
import type { ConversationInstance } from '../../../shared/types-engine'
import { seedMainPane } from '../../stores/__tests__/helpers/conversation-test-helpers'

// ── global window stub ────────────────────────────────────────────────────────

const mockPrompt = vi.fn(async () => {})
const mockSetPermissionMode = vi.fn()
const mockEngineSetPlanMode = vi.fn()
const mockSteer = vi.fn()
const mockReadPlan = vi.fn(async () => ({ content: '# plan body' }))
;(globalThis as any).window = {
  ion: {
    prompt: mockPrompt,
    setPermissionMode: mockSetPermissionMode,
    engineSetPlanMode: mockEngineSetPlanMode,
    steer: mockSteer,
    readPlan: mockReadPlan,
  },
  crypto: { randomUUID: () => 'uuid-1234' },
}

// ── test state builder ────────────────────────────────────────────────────────

function makeTab(overrides: Partial<TabState> = {}): TabState {
  return {
    id: 'tab-1',
    conversationId: null,
    historicalSessionIds: [],
    lastKnownSessionId: null,
    status: 'completed',
    activeRequestId: null,
    lastEventAt: null,
    hasUnread: false,
    currentActivity: '',
    attachments: [],
    title: 'New Tab',
    customTitle: null,
    lastResult: null,
    sessionTools: [],
    sessionMcpServers: [],
    sessionSkills: [],
    sessionVersion: null,
    queuedPrompts: [],
    workingDirectory: '/home/test',
    hasChosenDirectory: true,
    additionalDirs: [],
    bashResults: [],
    bashExecuting: false,
    bashExecId: null,
    pillColor: null,
    pillIcon: null,
    forkedFromSessionId: null,
    hasFileActivity: false,
    worktree: null,
    pendingWorktreeSetup: false,
    groupId: null,
    groupPinned: false,
    contextTokens: null,
    contextPercent: null,
    contextWindow: null,
    isCompacting: false,
    isTerminalOnly: false,
    engineProfileId: null,
    lastMessagePreview: null,
    ...overrides,
  }
}

function buildHarness(
  initialTab: TabState,
  instanceOverrides: Partial<ConversationInstance> = {},
) {
  const state: any = {
    tabs: [initialTab],
    activeTabId: initialTab.id,
    scrollToBottomCounter: 0,
    staticInfo: { homePath: '/home/test', projectPath: '/home/test', version: '1', email: null, subscriptionType: null },
    backend: 'api' as const,
    terminalPanes: new Map(),
    terminalOpenTabIds: new Set(),
    worktreeUncommittedMap: new Map(),
    engineWorkingMessages: new Map(),
    engineNotifications: new Map(),
    engineDialogs: new Map(),
    enginePinnedPrompt: new Map(),
    engineUsage: new Map(),
    conversationPanes: seedMainPane(initialTab.id, {
      permissionMode: 'auto',
      ...instanceOverrides,
    }),
    engineModelFallbacks: new Map(),
    fileExplorerOpenDirs: new Set(),
    fileEditorOpenDirs: new Set(),
  }

  const set = vi.fn((updater: any) => {
    const patch = typeof updater === 'function' ? updater(state) : updater
    Object.assign(state, patch)
  })
  const get = () => state as State

  const tabSlice = createTabSlice(set, get)
  const sendSlice = createSendSlice(set, get)
  Object.assign(state, tabSlice, sendSlice)
  state.moveTabToGroup = vi.fn()
  state.handleError = vi.fn()
  state.addEngineSystemMessage = vi.fn()
  state.setTabModel = vi.fn()

  storeHolder.current = state
  return { state, set }
}

// ── tests ─────────────────────────────────────────────────────────────────────

describe('runHandleImplement — plan-mode flip (plain tab)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPrompt.mockResolvedValue(undefined)
    mockReadPlan.mockResolvedValue({ content: '# plan body' })
  })

  it('clears the AUTHORITATIVE parent permission mode to auto and never re-asserts plan', async () => {
    // permissionMode now lives on the instance (WI-002) — pass as instance override
    const tab = makeTab()
    const { state } = buildHarness(tab, { permissionMode: 'plan', planFilePath: '/plans/test.md' })

    await runHandleImplement(
      {
        tabId: 'tab-1',
        clearPermissionDenied: () => {},
        submit: state.submit,
        tabPlanFilePath: '/plans/test.md',
        permissionDenied: null,
      },
      false,
    )

    // Authoritative mode is on the active instance; must be 'auto' after implement.
    const resolvedTab = state.tabs.find((t: TabState) => t.id === 'tab-1')!
    expect(effectivePermissionMode(resolvedTab, state.conversationPanes)).toBe('auto')
    // TabState no longer has permissionMode (WI-002) — the instance is the source.

    // The engine was told auto (plan-off). The downstream submit() prompt_sync
    // must NOT have re-asserted plan: every setPermissionMode call for this tab
    // is 'auto', never 'plan'.
    const tabCalls = mockSetPermissionMode.mock.calls.filter((c) => c[0] === 'tab-1')
    expect(tabCalls.length).toBeGreaterThan(0)
    for (const c of tabCalls) {
      expect(c[1]).toBe('auto')
    }
  })

  it('submits the implement prompt with implementationPhase', async () => {
    // permissionMode now lives on the instance (WI-002) — pass as instance override
    const tab = makeTab()
    const { state } = buildHarness(tab, { permissionMode: 'plan', planFilePath: '/plans/test.md' })

    await runHandleImplement(
      {
        tabId: 'tab-1',
        clearPermissionDenied: () => {},
        submit: state.submit,
        tabPlanFilePath: '/plans/test.md',
        permissionDenied: null,
      },
      false,
    )

    expect(mockPrompt).toHaveBeenCalledTimes(1)
    const args = mockPrompt.mock.calls[0] as unknown as any[]
    expect(args[2].implementationPhase).toBe(true)
  })
})
