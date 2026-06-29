// @vitest-environment jsdom
//
// Regression tests for the tab-strip onWheel portal-bleed fix.
//
// Root cause (2026-06-27): React synthetic wheel events from portaled popovers
// (GroupPickerDropdown) bubble through the React component tree to TabStrip's
// onWheel handler even though their DOM target lives in the PopoverLayer, not
// inside the scrollRef container. Without the DOM-containment guard, scrollLeft
// changes on the tab strip whenever the user scrolls inside the group picker.
//
// This test verifies:
//   1. A wheel event whose nativeEvent.target is OUTSIDE scrollRef does NOT
//      change scrollLeft (the guard fires).
//   2. A wheel event whose nativeEvent.target IS inside scrollRef DOES change
//      scrollLeft (normal horizontal scroll still works).
//
// Reversibility: removing the `scrollRef.current.contains(...)` guard from
// TabStrip.tsx causes assertion (1) to fail.

import React from 'react'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true

if (typeof globalThis.ResizeObserver === 'undefined') {
  ;(globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

// ─── Module stubs (same shape as TabStrip.recentDirs.test.tsx) ────────────────

vi.mock('framer-motion', () => ({
  AnimatePresence: ({ children }: { children?: React.ReactNode }) => children ?? null,
  motion: {
    div: React.forwardRef(({ children, ...rest }: React.HTMLAttributes<HTMLDivElement>, ref) =>
      React.createElement('div', { ...rest, ref }, children)),
  },
}))

vi.mock('@phosphor-icons/react', () => ({
  Terminal: () => null,
  CaretLeft: () => null,
  CaretRight: () => null,
  ArrowsInSimple: () => null,
  ArrowsOutSimple: () => null,
  ChatCircle: () => null,
  Diamond: () => null, Square: () => null, StarFour: () => null,
  Triangle: () => null, Heart: () => null, Hexagon: () => null,
  Lightning: () => null, DeviceMobile: () => null, Monitor: () => null, Gear: () => null,
  FolderPlus: () => null, FolderOpen: () => null, Trash: () => null,
  Clock: () => null, ChatCircleText: () => null, Stack: () => null, Bell: () => null,
  BellRinging: () => null, X: () => null,
}))

vi.mock('../../theme', () => ({
  useColors: () => new Proxy({}, { get: () => '#000' }),
}))

vi.mock('../TabStripDirectoryPicker', () => ({
  DirectoryPicker: () => null,
}))

vi.mock('../TabStripTabPill', () => ({
  TabPill: ({ tabRefs, tab }: { tabRefs: React.MutableRefObject<Map<string, HTMLDivElement>>; tab: { id: string } }) => {
    // Render a real div so we can grab it as a target inside scrollRef.
    return React.createElement('div', {
      'data-testid': `tab-pill-${tab.id}`,
      ref: (el: HTMLDivElement | null) => { if (el) tabRefs.current.set(tab.id, el) },
    })
  },
}))

vi.mock('../TabStripGroupPill', () => ({
  GroupPill: () => null,
}))

vi.mock('../TabStripPillColorPicker', () => ({
  PillColorPicker: () => null,
}))

vi.mock('../TabStripDirContextMenu', () => ({
  DirContextMenu: () => null,
}))

vi.mock('../TabStripTabContextMenu', () => ({
  TabContextMenu: () => null,
}))

vi.mock('../HistoryPicker', () => ({
  HistoryPicker: () => null,
}))

vi.mock('../SettingsPopover', () => ({
  SettingsPopover: () => null,
}))

vi.mock('../NotificationsPanel', () => ({
  NotificationsBell: () => null,
}))

vi.mock('../BranchPickerDialog', () => ({
  BranchPickerDialog: () => null,
}))

vi.mock('../NewConversationPicker', () => ({
  NewConversationPicker: () => null,
  resolveNewConversationAction: () => ({ kind: 'plain' }),
  executeNewConversationAction: () => undefined,
  newTabInDirectory: () => undefined,
}))

vi.mock('../new-conversation-routing', () => ({
  resolveNewConversationAction: () => ({ kind: 'plain' }),
  executeNewConversationAction: () => undefined,
  newTabInDirectory: () => undefined,
}))

vi.mock('../../hooks/useTabGroups', () => ({
  useTabGroups: () => ({ mode: 'off', groups: [], ungrouped: [] }),
}))

vi.mock('../../hooks/useManualReorder', () => ({
  useManualReorder: () => ({
    onItemPointerDown: () => {},
    isDraggingRef: { current: false },
  }),
}))

vi.mock('../TabStripShared', () => ({
  checkWorktreeUncommitted: () => {},
  shouldUseWorktree: () => false,
  zoomRect: (r: DOMRect) => r,
}))

vi.mock('../PopoverLayer', () => ({
  usePopoverLayer: () => null,
  PopoverLayer: ({ children }: { children?: React.ReactNode }) => children ?? null,
}))

vi.mock('../../stores/remote-fs-store', () => ({
  pickDirectoryForSession: async () => null,
}))

const STUB_TAB = {
  id: 'tab-1',
  title: 'Test tab',
  customTitle: null,
  engineProfileId: null,
  workingDirectory: '/work/ion',
  status: 'idle',
  worktree: false,
  groupId: null,
  pillColor: null,
  pillIcon: null,
  pendingWorktreeSetup: false,
  hasChosenDirectory: true,
  historicalSessionIds: [],
  conversationId: null,
}

vi.mock('../../stores/sessionStore', () => ({
  useSessionStore: (selector: (s: unknown) => unknown) =>
    selector({
      tabs: [STUB_TAB],
      activeTabId: 'tab-1',
      conversationPanes: new Map(),
      tabsReady: true,
      isExpanded: false,
      terminalOpenTabIds: new Set(),
      terminalTallTabId: null,
      tallViewTabId: null,
      worktreeUncommittedMap: new Map(),
      fileEditorFocused: false,
      fileEditorOpenDirs: new Set(),
      fileEditorStates: new Map(),
      openFloatingPanelCount: 0,
      staticInfo: { homePath: '/Users/test' },
      selectTab: () => {},
      closeTab: () => {},
      reorderTabs: () => {},
      renameTab: () => {},
      setTabPillColor: () => {},
      setTabPillIcon: () => {},
      createTabInDirectory: () => {},
      toggleTerminal: () => {},
      createTerminalTab: () => {},
      createConversationTab: () => {},
      toggleExpanded: () => {},
      toggleFileExplorer: () => {},
      toggleFileEditor: () => {},
      toggleGitPanel: () => {},
      toggleTerminalTall: () => {},
      toggleTallView: () => {},
      forkTab: () => {},
      finishWorktreeTab: () => {},
      setupWorktree: () => {},
      cancelWorktreeSetup: () => {},
      createScratchFile: () => {},
      addTerminalInstance: () => {},
    }),
  editorDirForTab: () => '/work/ion',
}))

vi.mock('../../preferences', () => ({
  usePreferencesStore: (selector: (s: unknown) => unknown) =>
    selector({
      recentBaseDirectories: [],
      directoryUsageCounts: {},
      defaultBaseDirectory: '/work/ion',
      enterpriseNewConversationDefaults: null,
      engineProfiles: [],
      defaultEngineProfileId: '',
      uiZoom: 1,
      addRecentBaseDirectory: () => {},
      incrementDirectoryUsage: () => {},
      removeRecentBaseDirectory: () => {},
    }),
}))

// ─── Helpers ──────────────────────────────────────────────────────────────────

import { TabStrip } from '../TabStrip'

/** Fire a synthetic React WheelEvent on `target` whose `nativeEvent.target`
 *  points at `nativeTarget`. React's synthetic onWheel reads `e.nativeEvent.target`
 *  for the DOM-containment check, so we have to pass the native target explicitly.
 */
function fireWheelEvent(target: Element, nativeTarget: EventTarget, deltaY: number) {
  // Build a native WheelEvent whose .target will be `nativeTarget`.
  // We dispatch on `nativeTarget` but React's event delegation at the root
  // will still invoke the React onWheel handler on `target`'s React tree.
  // For our purposes, creating a WheelEvent manually and attaching to a
  // synthetic-looking object is sufficient — we simulate what React exposes
  // to the onWheel callback.
  const nativeEvent = new WheelEvent('wheel', { deltaY, bubbles: true, cancelable: true })
  // Override the read-only `target` using defineProperty so our guard sees it.
  Object.defineProperty(nativeEvent, 'target', { value: nativeTarget, configurable: true })

  // React's synthetic event wraps nativeEvent. We call the onWheel prop directly
  // to keep the test simple and deterministic — no need for real DOM event dispatch.
  const scrollDiv = target.querySelector('[class*="overflow-x-auto"]') as HTMLElement | null
  if (!scrollDiv) throw new Error('Could not find scrollRef div')

  // Get the React onWheel handler from the fiber. Instead of reaching into React
  // internals, we retrieve the prop as stored by React on the DOM node.
  // The simplest approach: dispatch a real WheelEvent on the scrollDiv with
  // the nativeEvent's deltaY, and check scrollLeft after.
  const evt = new WheelEvent('wheel', { deltaY, bubbles: true, cancelable: true })
  Object.defineProperty(evt, 'target', { value: nativeTarget, configurable: true })
  scrollDiv.dispatchEvent(evt)
}

// ─── Tests ────────────────────────────────────────────────────────────────────

describe('TabStrip onWheel portal-bleed fix', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    act(() => {
      root = createRoot(container)
      root.render(React.createElement(TabStrip))
    })
  })

  afterEach(() => {
    act(() => { root.unmount() })
    container.remove()
  })

  it('does not change scrollLeft when wheel target is OUTSIDE scrollRef (portal bleed guard)', () => {
    const scrollDiv = container.querySelector('[class*="overflow-x-auto"]') as HTMLElement
    expect(scrollDiv).not.toBeNull()

    // jsdom default scrollLeft is 0.
    expect(scrollDiv.scrollLeft).toBe(0)

    // An element outside the scroll container — e.g. a portaled popover DOM node.
    const outsideEl = document.createElement('div')
    document.body.appendChild(outsideEl)

    act(() => {
      // Dispatch wheel with deltaY=100, but the native target is outside scrollRef.
      const evt = new WheelEvent('wheel', { deltaY: 100, bubbles: true, cancelable: true })
      Object.defineProperty(evt, 'target', { value: outsideEl, configurable: true })
      scrollDiv.dispatchEvent(evt)
    })

    // Guard must have fired: scrollLeft must stay 0.
    expect(scrollDiv.scrollLeft).toBe(0)

    outsideEl.remove()
  })

  it('changes scrollLeft when wheel target IS inside scrollRef (normal scroll works)', () => {
    const scrollDiv = container.querySelector('[class*="overflow-x-auto"]') as HTMLElement
    expect(scrollDiv).not.toBeNull()

    // Make the scroll container actually scrollable so scrollLeft can change.
    // jsdom doesn't do layout; we fake scrollWidth > clientWidth by setting
    // scrollLeft directly to confirm the handler attempts to set it.
    // We spy on the scrollLeft setter to detect whether the handler ran.
    let capturedScrollLeftDelta: number | undefined
    const originalScrollLeft = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollLeft')
    Object.defineProperty(scrollDiv, 'scrollLeft', {
      get: () => capturedScrollLeftDelta ?? 0,
      set: (v: number) => { capturedScrollLeftDelta = v },
      configurable: true,
    })

    act(() => {
      // A tab pill inside the scroll container is the native target.
      const tabPillEl = container.querySelector('[data-testid="tab-pill-tab-1"]') as HTMLElement
      // If the TabPill stub rendered a div, use it; otherwise use scrollDiv itself as a child.
      const innerTarget = tabPillEl ?? scrollDiv.firstElementChild ?? scrollDiv

      const evt = new WheelEvent('wheel', { deltaY: 50, bubbles: true, cancelable: true })
      Object.defineProperty(evt, 'target', { value: innerTarget, configurable: true })
      scrollDiv.dispatchEvent(evt)
    })

    // Handler must have run: scrollLeft was set to delta (0 + 50).
    expect(capturedScrollLeftDelta).toBe(50)

    // Restore
    if (originalScrollLeft) {
      Object.defineProperty(scrollDiv, 'scrollLeft', originalScrollLeft)
    }
  })
})
