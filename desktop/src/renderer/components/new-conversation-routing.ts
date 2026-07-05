import type { EngineProfile, NewConversationDefaultsPolicy } from '../../shared/types'

/**
 * new-conversation-routing — pure routing logic for the single "New
 * Conversation" entry point (conversation unification #256).
 *
 * Extracted into its own module so it can be unit-tested without any
 * React, DOM, Electron, or store dependencies.
 */

export type NewConversationAction =
  | { kind: 'plain' }
  | { kind: 'profile'; profileId: string }
  | { kind: 'show-picker' }
  | { kind: 'locked'; baseDirectory: string; profileId: string }

/**
 * Resolve the next action for "new conversation" given the current preference
 * state. Pure and side-effect-free.
 *
 * State machine (highest to lowest precedence):
 *   0. Enterprise-locked (highest): NewConversationDefaults.locked is true -> skip both
 *      pickers. Open directly with the mandated baseDirectory and profileId.
 *      Empty profileId means plain conversation.
 *   1. Zero engine profiles -> open plain conversation directly. No picker.
 *   2. defaultEngineProfileId is non-empty AND the profile still exists
 *      -> open with that profile directly. No picker.
 *   3. Otherwise -> show the extended picker (plain option + profiles).
 *
 * @param profiles         The current engineProfiles list.
 * @param defaultId        The current defaultEngineProfileId preference ('' = unset).
 * @param enterprisePolicy The enterprise NewConversationDefaults policy, or null if none.
 */
export function resolveNewConversationAction(
  profiles: EngineProfile[],
  defaultId: string,
  enterprisePolicy?: NewConversationDefaultsPolicy | null,
): NewConversationAction {
  // State 0 (highest precedence): enterprise-locked. Skip both pickers entirely.
  if (enterprisePolicy?.locked) {
    return {
      kind: 'locked',
      baseDirectory: enterprisePolicy.baseDirectory ?? '',
      profileId: enterprisePolicy.engineProfileId ?? '',
    }
  }

  // State 1: no profiles at all -> plain conversation, no picker.
  if (profiles.length === 0) return { kind: 'plain' }

  // State 2: a default is set and the profile still exists -> use it directly.
  if (defaultId) {
    const exists = profiles.some((p) => p.id === defaultId)
    if (exists) return { kind: 'profile', profileId: defaultId }
    // Default was deleted: fall through to picker.
  }

  // State 3: show the extended picker.
  return { kind: 'show-picker' }
}

/**
 * Execute the resolved routing action for a specific directory. Handles the
 * `locked`, `plain`, and `profile` branches by calling the appropriate store
 * action. Returns `'show-picker'` when the caller needs to open the
 * `NewConversationPicker` UI (the caller handles that).
 *
 * This eliminates duplication across TabStrip, useKeyboardShortcuts, context
 * menus, and the pill creation path. Every call site that creates a new
 * conversation tab MUST route through this helper (or through
 * `resolveNewConversationAction` directly) so the enterprise lock cannot be
 * bypassed.
 *
 * @param dir               Working directory to use (fallback from preferences
 *                          when empty/undefined).
 * @param action            The pre-resolved action from
 *                          `resolveNewConversationAction`.
 * @param createTabInDir    Store action: `sessionStore.createTabInDirectory`.
 * @param createConvTab     Store action: `sessionStore.createConversationTab`.
 * @param shouldUseWorktree Whether to open a worktree tab.
 * @returns 'show-picker' when the caller must present the picker UI,
 *          'done' when the tab was created.
 */
export function executeNewConversationAction(
  dir: string,
  action: NewConversationAction,
  createTabInDir: (dir: string, worktree?: boolean) => void,
  createConvTab: (dir: string, opts?: { profileId?: string }) => void,
  shouldUseWorktree: boolean = false,
): 'show-picker' | 'done' {
  switch (action.kind) {
    case 'locked': {
      // Enterprise-locked: always use the mandated dir (fall back to provided
      // dir if the mandated one is empty, matching desktop's new-tab behavior).
      const lockedDir = action.baseDirectory || dir
      if (action.profileId) {
        createConvTab(lockedDir, { profileId: action.profileId })
      } else {
        createTabInDir(lockedDir, shouldUseWorktree)
      }
      return 'done'
    }
    case 'plain':
      createTabInDir(dir, shouldUseWorktree)
      return 'done'
    case 'profile':
      createConvTab(dir, { profileId: action.profileId })
      return 'done'
    case 'show-picker':
      return 'show-picker'
  }
}

/**
 * newTabInDirectory — the single, lock-safe "new tab in this directory" entry
 * point shared by every per-tab context menu ("New tab in dir" on the tab
 * context menu, the group-pill context menu, and the group-picker dropdown's
 * per-tab menu).
 *
 * This collapses what used to be three duplicated inline handlers — two of
 * which called `createTabInDirectory` directly and so bypassed the enterprise
 * lock entirely. Routing every context-menu "new tab in dir" path through this
 * one function means the lock cannot be bypassed: there is exactly one path,
 * and it always resolves the action (including the highest-precedence `locked`
 * branch) before creating anything.
 *
 * It reads the routing inputs (profiles, default profile, enterprise policy)
 * from the preferences store and dispatches to the session store, so callers
 * only supply the target directory and worktree flag.
 *
 * @param dir   The tab's working directory to open the new tab in.
 * @param deps  The store accessors + worktree flag (injected so this is unit-
 *              testable without React/Electron).
 * @returns 'show-picker' when the resolver wants the picker UI (the caller is
 *          a context menu with no picker, so this is effectively 'no-op' there),
 *          'done' when a tab was created.
 */
export function newTabInDirectory(
  dir: string,
  deps: {
    profiles: EngineProfile[]
    defaultProfileId: string
    enterprisePolicy?: NewConversationDefaultsPolicy | null
    createTabInDir: (dir: string, worktree?: boolean) => void
    createConvTab: (dir: string, opts?: { profileId?: string }) => void
    shouldUseWorktree?: boolean
  },
): 'show-picker' | 'done' {
  const action = resolveNewConversationAction(
    deps.profiles,
    deps.defaultProfileId,
    deps.enterprisePolicy,
  )
  return executeNewConversationAction(
    dir,
    action,
    deps.createTabInDir,
    deps.createConvTab,
    deps.shouldUseWorktree ?? false,
  )
}
