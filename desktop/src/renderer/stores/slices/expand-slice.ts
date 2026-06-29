import type { StoreSet, StoreGet, State } from '../session-store-types'

export function createExpandSlice(set: StoreSet, get: StoreGet): Partial<State> {
  return {
    toggleExpanded: () => {
      const { activeTabId, isExpanded } = get()
      const willExpand = !isExpanded
      set((s) => ({
        isExpanded: willExpand,
        settingsOpen: false,
        tabs: willExpand
          ? s.tabs.map((t) => t.id === activeTabId ? { ...t, hasUnread: false } : t)
          : s.tabs,
      }))
    },

    toggleTallView: (tabId) => {
      set((s) => ({
        tallViewTabId: s.tallViewTabId === tabId ? null : tabId,
        ...(s.tallViewTabId !== tabId ? { terminalTallTabId: null } : {}),
      }))
    },

    openSettings: (tab?: string) => set({ settingsOpen: true, settingsInitialTab: tab ?? null }),
    closeSettings: () => set({ settingsOpen: false, settingsInitialTab: null }),

    incOpenFloatingPanelCount: () => set((s) => ({ openFloatingPanelCount: s.openFloatingPanelCount + 1 })),
    decOpenFloatingPanelCount: () => set((s) => ({ openFloatingPanelCount: Math.max(0, s.openFloatingPanelCount - 1) })),

    toggleGitPanel: () => {
      set((s) => ({ gitPanelOpen: !s.gitPanelOpen }))
    },

    closeGitPanel: () => {
      set({ gitPanelOpen: false })
    },
  }
}
