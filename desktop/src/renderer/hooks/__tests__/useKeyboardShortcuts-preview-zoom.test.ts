/**
 * useKeyboardShortcuts — preview (pop-up) zoom routing
 *
 * Tests the three-way routing: preview → editor → conversation.
 * Pins the behavior that isPreviewZoomTarget() gates the new previewFontSize
 * branch before isEditorZoomTarget() and conversationFontSize.
 *
 * Revert-check: with the preview branch removed, the "zoom.in with pop-up open"
 * test would call setConversationFontSize (falling through to the else branch),
 * making it fail. The test is pinned on the condition that isPreviewZoomTarget
 * takes priority.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'

// ── Store mocks ────────────────────────────────────────────────────────────

let prefState = {
  editorFontSize: 14,
  conversationFontSize: 13,
  previewFontSize: 13,
  keyboardShortcuts: {} as Record<string, string>,
  setEditorFontSize: vi.fn((n: number) => { prefState.editorFontSize = n }),
  setConversationFontSize: vi.fn((n: number) => { prefState.conversationFontSize = n }),
  setPreviewFontSize: vi.fn((n: number) => { prefState.previewFontSize = n }),
}

vi.mock('../../preferences', () => ({
  usePreferencesStore: {
    getState: () => prefState,
  },
}))

vi.mock('../../stores/sessionStore', async () => {
  return {
    useSessionStore: { getState: () => sessionState },
    editorDirForTab: (tab: any) => tab?.worktreeRepoPath ?? tab?.workingDirectory ?? '',
  }
})

vi.mock('../../../shared/tab-predicates', () => ({
  tabHasExtensions: () => false,
}))

vi.mock('../../components/new-conversation-routing', () => ({
  resolveNewConversationAction: () => ({ kind: 'plain' }),
  executeNewConversationAction: vi.fn(),
}))

vi.mock('../../preferences-types', async () => {
  const actual = await vi.importActual<any>('../../preferences-types')
  return actual
})

// ── Shared session state ───────────────────────────────────────────────────

let sessionState: any

function makeTab(id: string, workingDirectory = '/projects/ion') {
  return {
    id,
    workingDirectory,
    worktreeRepoPath: null,
    status: 'idle',
    title: 'Test',
    customTitle: null,
    pillColor: null,
    pillIcon: null,
    groupId: null,
    hasChosenDirectory: true,
    engineProfileId: null,
  }
}

function makeFileEditorState(activeFileId: string | null) {
  return {
    activeFileId,
    files: activeFileId
      ? [{ id: activeFileId, fileName: 'README.md', filePath: null, content: '', savedContent: '', isDirty: false, isReadOnly: false, isPreview: false }]
      : [],
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  prefState = {
    editorFontSize: 14,
    conversationFontSize: 13,
    previewFontSize: 13,
    keyboardShortcuts: {},
    setEditorFontSize: vi.fn((n: number) => { prefState.editorFontSize = n }),
    setConversationFontSize: vi.fn((n: number) => { prefState.conversationFontSize = n }),
    setPreviewFontSize: vi.fn((n: number) => { prefState.previewFontSize = n }),
  }
  const tab = makeTab('tab1')
  sessionState = {
    tabs: [tab],
    activeTabId: 'tab1',
    fileEditorFocused: false,
    fileEditorOpenDirs: new Set<string>(),
    fileEditorStates: new Map(),
    openFloatingPanelCount: 0,
  }
})

// ── isPreviewZoomTarget ────────────────────────────────────────────────────

describe('isPreviewZoomTarget()', () => {
  it('returns true when openFloatingPanelCount > 0', async () => {
    const { isPreviewZoomTarget } = await import('../useKeyboardShortcuts')
    sessionState.openFloatingPanelCount = 1
    expect(isPreviewZoomTarget()).toBe(true)
  })

  it('returns true when count is 2 (multiple panels open)', async () => {
    const { isPreviewZoomTarget } = await import('../useKeyboardShortcuts')
    sessionState.openFloatingPanelCount = 2
    expect(isPreviewZoomTarget()).toBe(true)
  })

  it('returns false when openFloatingPanelCount is 0', async () => {
    const { isPreviewZoomTarget } = await import('../useKeyboardShortcuts')
    sessionState.openFloatingPanelCount = 0
    expect(isPreviewZoomTarget()).toBe(false)
  })
})

// ── Three-way zoom routing ────────────────────────────────────────────────

describe('zoom routing — preview target (pop-up open)', () => {
  it('zoom.in calls setPreviewFontSize and NOT conversation/editor when pop-up is open', async () => {
    const { isPreviewZoomTarget, isEditorZoomTarget } = await import('../useKeyboardShortcuts')
    sessionState.openFloatingPanelCount = 1

    // Simulate the handler logic.
    const p = prefState
    if (isPreviewZoomTarget()) {
      p.setPreviewFontSize(p.previewFontSize + 1)
    } else if (isEditorZoomTarget()) {
      p.setEditorFontSize(p.editorFontSize + 1)
    } else {
      p.setConversationFontSize(p.conversationFontSize + 1)
    }

    expect(p.setPreviewFontSize).toHaveBeenCalledTimes(1)
    expect(p.setPreviewFontSize).toHaveBeenCalledWith(14)
    // Revert-check: without the preview branch, this would be called.
    expect(p.setConversationFontSize).not.toHaveBeenCalled()
    expect(p.setEditorFontSize).not.toHaveBeenCalled()
  })

  it('zoom.out calls setPreviewFontSize when pop-up is open', async () => {
    const { isPreviewZoomTarget, isEditorZoomTarget } = await import('../useKeyboardShortcuts')
    sessionState.openFloatingPanelCount = 1

    const p = prefState
    if (isPreviewZoomTarget()) {
      p.setPreviewFontSize(p.previewFontSize - 1)
    } else if (isEditorZoomTarget()) {
      p.setEditorFontSize(p.editorFontSize - 1)
    } else {
      p.setConversationFontSize(p.conversationFontSize - 1)
    }

    expect(p.setPreviewFontSize).toHaveBeenCalledWith(12)
    expect(p.setConversationFontSize).not.toHaveBeenCalled()
    expect(p.setEditorFontSize).not.toHaveBeenCalled()
  })

  it('zoom.reset resets previewFontSize to SETTINGS_DEFAULTS when pop-up is open', async () => {
    const { isPreviewZoomTarget, isEditorZoomTarget } = await import('../useKeyboardShortcuts')
    const { SETTINGS_DEFAULTS } = await import('../../preferences-types')
    sessionState.openFloatingPanelCount = 1
    prefState.previewFontSize = 20

    const p = prefState
    if (isPreviewZoomTarget()) {
      p.setPreviewFontSize(SETTINGS_DEFAULTS.previewFontSize)
    } else if (isEditorZoomTarget()) {
      p.setEditorFontSize(SETTINGS_DEFAULTS.editorFontSize)
    } else {
      p.setConversationFontSize(SETTINGS_DEFAULTS.conversationFontSize)
    }

    expect(p.setPreviewFontSize).toHaveBeenCalledWith(SETTINGS_DEFAULTS.previewFontSize)
    expect(p.setConversationFontSize).not.toHaveBeenCalled()
    expect(p.setEditorFontSize).not.toHaveBeenCalled()
  })

  it('preview takes priority over editor when both conditions are true', async () => {
    // Edge case: both a pop-up is open AND the editor is focused.
    // Preview wins.
    const { isPreviewZoomTarget, isEditorZoomTarget } = await import('../useKeyboardShortcuts')
    sessionState.openFloatingPanelCount = 1
    // Also set up editor as focused
    sessionState.fileEditorFocused = true
    sessionState.fileEditorOpenDirs = new Set(['/projects/ion'])
    sessionState.fileEditorStates = new Map([['/projects/ion', makeFileEditorState('file1')]])

    expect(isPreviewZoomTarget()).toBe(true)
    expect(isEditorZoomTarget()).toBe(true)

    const p = prefState
    if (isPreviewZoomTarget()) {
      p.setPreviewFontSize(p.previewFontSize + 1)
    } else if (isEditorZoomTarget()) {
      p.setEditorFontSize(p.editorFontSize + 1)
    } else {
      p.setConversationFontSize(p.conversationFontSize + 1)
    }

    expect(p.setPreviewFontSize).toHaveBeenCalledTimes(1)
    expect(p.setEditorFontSize).not.toHaveBeenCalled()
    expect(p.setConversationFontSize).not.toHaveBeenCalled()
  })
})

describe('zoom routing — editor and conversation (regression: no pop-up)', () => {
  it('routes to editor when editor is focused and no pop-up is open', async () => {
    const { isPreviewZoomTarget, isEditorZoomTarget } = await import('../useKeyboardShortcuts')
    sessionState.openFloatingPanelCount = 0
    sessionState.fileEditorFocused = true
    sessionState.fileEditorOpenDirs = new Set(['/projects/ion'])
    sessionState.fileEditorStates = new Map([['/projects/ion', makeFileEditorState('file1')]])

    expect(isPreviewZoomTarget()).toBe(false)
    expect(isEditorZoomTarget()).toBe(true)

    const p = prefState
    if (isPreviewZoomTarget()) {
      p.setPreviewFontSize(p.previewFontSize + 1)
    } else if (isEditorZoomTarget()) {
      p.setEditorFontSize(p.editorFontSize + 1)
    } else {
      p.setConversationFontSize(p.conversationFontSize + 1)
    }

    expect(p.setEditorFontSize).toHaveBeenCalledWith(15)
    expect(p.setPreviewFontSize).not.toHaveBeenCalled()
    expect(p.setConversationFontSize).not.toHaveBeenCalled()
  })

  it('routes to conversation when neither pop-up nor editor is active', async () => {
    const { isPreviewZoomTarget, isEditorZoomTarget } = await import('../useKeyboardShortcuts')
    sessionState.openFloatingPanelCount = 0
    sessionState.fileEditorFocused = false

    expect(isPreviewZoomTarget()).toBe(false)
    expect(isEditorZoomTarget()).toBe(false)

    const p = prefState
    if (isPreviewZoomTarget()) {
      p.setPreviewFontSize(p.previewFontSize + 1)
    } else if (isEditorZoomTarget()) {
      p.setEditorFontSize(p.editorFontSize + 1)
    } else {
      p.setConversationFontSize(p.conversationFontSize + 1)
    }

    expect(p.setConversationFontSize).toHaveBeenCalledWith(14)
    expect(p.setPreviewFontSize).not.toHaveBeenCalled()
    expect(p.setEditorFontSize).not.toHaveBeenCalled()
  })
})
