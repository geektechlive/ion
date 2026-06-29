/**
 * NewConversationPicker — routing logic unit tests.
 *
 * Pins the four-state smart-picker behaviour for the single "New
 * Conversation" button (conversation unification #256):
 *
 *   State 0 (locked): enterprise NewConversationDefaults.locked=true -> 'locked',
 *            bypassing both pickers regardless of profiles/default.
 *   State 1: zero profiles configured -> 'plain' (no picker).
 *   State 2: defaultEngineProfileId set and the profile exists -> 'profile'
 *            (direct open, no picker).
 *   State 3: profiles exist, no default (or default deleted) -> 'show-picker'.
 *
 * These tests target the pure `resolveNewConversationAction` function, which
 * contains all the branching logic and is easily verified without React
 * machinery.
 */

import { describe, it, expect, vi } from 'vitest'
import { resolveNewConversationAction, executeNewConversationAction, newTabInDirectory } from '../new-conversation-routing'
import type { EngineProfile, NewConversationDefaultsPolicy } from '../../../shared/types'

// ─── Helpers ─────────────────────────────────────────────────────────────────

function makeProfile(id: string, name: string): EngineProfile {
  return { id, name, extensions: [`ext/${id}`] }
}

function makePolicy(overrides: Partial<NewConversationDefaultsPolicy> = {}): NewConversationDefaultsPolicy {
  return {
    baseDirectory: '/enterprise/dir',
    engineProfileId: 'orion-profile',
    locked: true,
    ...overrides,
  }
}

// ─── State 0: enterprise locked ──────────────────────────────────────────────

describe('resolveNewConversationAction — State 0: enterprise locked', () => {
  it('returns locked when policy is locked, regardless of profiles', () => {
    const profiles = [makeProfile('p1', 'A'), makeProfile('p2', 'B')]
    const policy = makePolicy()
    const result = resolveNewConversationAction(profiles, 'p1', policy)
    expect(result).toEqual({ kind: 'locked', baseDirectory: '/enterprise/dir', profileId: 'orion-profile' })
  })

  it('returns locked when policy is locked and profiles list is empty', () => {
    const policy = makePolicy({ baseDirectory: '/org/home', engineProfileId: '' })
    const result = resolveNewConversationAction([], '', policy)
    expect(result).toEqual({ kind: 'locked', baseDirectory: '/org/home', profileId: '' })
  })

  it('locked with empty profileId means plain conversation (no extension)', () => {
    const policy = makePolicy({ engineProfileId: '' })
    const result = resolveNewConversationAction([makeProfile('p1', 'A')], 'p1', policy)
    expect(result.kind).toBe('locked')
    expect((result as any).profileId).toBe('')
  })

  it('returns locked with mandated dir even when defaultEngineProfileId is set', () => {
    const profiles = [makeProfile('user-default', 'User Default')]
    const policy = makePolicy({ engineProfileId: 'enterprise-profile' })
    const result = resolveNewConversationAction(profiles, 'user-default', policy)
    expect(result).toEqual({ kind: 'locked', baseDirectory: '/enterprise/dir', profileId: 'enterprise-profile' })
  })

  it('does NOT lock when policy exists but locked=false', () => {
    const profiles = [makeProfile('p1', 'A')]
    const policy = makePolicy({ locked: false })
    const result = resolveNewConversationAction(profiles, '', policy)
    // locked=false -> falls through to state 3 (show-picker)
    expect(result).toEqual({ kind: 'show-picker' })
  })

  it('does NOT lock when enterprisePolicy is null', () => {
    const profiles = [makeProfile('p1', 'A')]
    const result = resolveNewConversationAction(profiles, '', null)
    expect(result).toEqual({ kind: 'show-picker' })
  })

  it('does NOT lock when enterprisePolicy is undefined (omitted)', () => {
    const profiles = [makeProfile('p1', 'A')]
    const result = resolveNewConversationAction(profiles, '')
    expect(result).toEqual({ kind: 'show-picker' })
  })
})

// ─── State 1: zero profiles ───────────────────────────────────────────────────

describe('resolveNewConversationAction — State 1: no profiles', () => {
  it('returns plain when the profiles list is empty, regardless of defaultId', () => {
    expect(resolveNewConversationAction([], '')).toEqual({ kind: 'plain' })
    expect(resolveNewConversationAction([], 'some-id')).toEqual({ kind: 'plain' })
  })

  it('returns plain for an empty profiles array with any defaultId value', () => {
    const cases = ['', 'profile-x', 'orion', '  ']
    for (const id of cases) {
      const result = resolveNewConversationAction([], id)
      expect(result.kind).toBe('plain')
    }
  })
})

// ─── State 2: default profile set ────────────────────────────────────────────

describe('resolveNewConversationAction — State 2: default profile set', () => {
  it('returns the default profile directly when it exists in the list', () => {
    const profiles = [makeProfile('p1', 'Orion'), makeProfile('p2', 'Dev')]
    const result = resolveNewConversationAction(profiles, 'p1')
    expect(result).toEqual({ kind: 'profile', profileId: 'p1' })
  })

  it('uses the second profile as default when the first is not the default', () => {
    const profiles = [makeProfile('p1', 'A'), makeProfile('p2', 'B')]
    const result = resolveNewConversationAction(profiles, 'p2')
    expect(result).toEqual({ kind: 'profile', profileId: 'p2' })
  })

  it('works when there is exactly one profile and it is the default', () => {
    const profiles = [makeProfile('solo', 'Solo')]
    const result = resolveNewConversationAction(profiles, 'solo')
    expect(result).toEqual({ kind: 'profile', profileId: 'solo' })
  })

  it('falls through to show-picker when defaultId is set but profile was deleted', () => {
    const profiles = [makeProfile('p2', 'B'), makeProfile('p3', 'C')]
    // 'p1' was deleted from the list
    const result = resolveNewConversationAction(profiles, 'p1')
    expect(result).toEqual({ kind: 'show-picker' })
  })
})

// ─── State 3: show picker ─────────────────────────────────────────────────────

describe('resolveNewConversationAction — State 3: show extended picker', () => {
  it('shows picker when profiles exist but defaultId is empty', () => {
    const profiles = [makeProfile('p1', 'A'), makeProfile('p2', 'B')]
    const result = resolveNewConversationAction(profiles, '')
    expect(result).toEqual({ kind: 'show-picker' })
  })

  it('shows picker when there is exactly one profile but no default is set', () => {
    const profiles = [makeProfile('solo', 'Solo')]
    const result = resolveNewConversationAction(profiles, '')
    expect(result).toEqual({ kind: 'show-picker' })
  })

  it('shows picker when there are many profiles and no default', () => {
    const profiles = [
      makeProfile('p1', 'A'),
      makeProfile('p2', 'B'),
      makeProfile('p3', 'C'),
      makeProfile('p4', 'D'),
    ]
    const result = resolveNewConversationAction(profiles, '')
    expect(result).toEqual({ kind: 'show-picker' })
  })
})

// ─── Edge cases ───────────────────────────────────────────────────────────────

describe('resolveNewConversationAction — edge cases', () => {
  it('treats a whitespace-only defaultId as no default (falls to picker)', () => {
    const profiles = [makeProfile('p1', 'A')]
    const result = resolveNewConversationAction(profiles, '   ')
    // '   ' is truthy so we check if any profile has id === '   ' -> false -> picker
    expect(result).toEqual({ kind: 'show-picker' })
  })

  it('is deterministic: same inputs always produce the same output', () => {
    const profiles = [makeProfile('x', 'X'), makeProfile('y', 'Y')]
    const r1 = resolveNewConversationAction(profiles, 'x')
    const r2 = resolveNewConversationAction(profiles, 'x')
    expect(r1).toEqual(r2)
  })

  it('does not mutate the profiles array', () => {
    const profiles = [makeProfile('p1', 'A'), makeProfile('p2', 'B')]
    const original = JSON.stringify(profiles)
    resolveNewConversationAction(profiles, 'p1')
    expect(JSON.stringify(profiles)).toBe(original)
  })
})

// ─── executeNewConversationAction ────────────────────────────────────────────

describe('executeNewConversationAction', () => {
  const createTabInDir = vi.fn()
  const createConvTab = vi.fn()

  function setup() {
    createTabInDir.mockClear()
    createConvTab.mockClear()
  }

  // TEST-GAP 1: locked action produces mandated dir+profile (Cmd+T / Cmd+Shift+T path)
  it('locked action calls createConvTab with mandated dir and profileId', () => {
    setup()
    const action = resolveNewConversationAction([], '', makePolicy())
    const result = executeNewConversationAction('/user/dir', action, createTabInDir, createConvTab)
    expect(result).toBe('done')
    expect(createConvTab).toHaveBeenCalledWith('/enterprise/dir', { profileId: 'orion-profile' })
    expect(createTabInDir).not.toHaveBeenCalled()
  })

  it('locked action with empty profileId calls createTabInDir (plain conversation)', () => {
    setup()
    const action = resolveNewConversationAction([], '', makePolicy({ engineProfileId: '' }))
    const result = executeNewConversationAction('/user/dir', action, createTabInDir, createConvTab)
    expect(result).toBe('done')
    expect(createTabInDir).toHaveBeenCalledWith('/enterprise/dir', false)
    expect(createConvTab).not.toHaveBeenCalled()
  })

  it('locked action with empty baseDirectory uses provided dir as fallback', () => {
    setup()
    const action = resolveNewConversationAction([], '', makePolicy({ baseDirectory: '' }))
    const result = executeNewConversationAction('/user/dir', action, createTabInDir, createConvTab)
    expect(result).toBe('done')
    expect(createConvTab).toHaveBeenCalledWith('/user/dir', { profileId: 'orion-profile' })
  })

  // TEST-GAP 2: plain action calls createTabInDir
  it('plain action calls createTabInDir', () => {
    setup()
    const action = resolveNewConversationAction([], '')
    expect(action.kind).toBe('plain')
    const result = executeNewConversationAction('/dir', action, createTabInDir, createConvTab)
    expect(result).toBe('done')
    expect(createTabInDir).toHaveBeenCalledWith('/dir', false)
    expect(createConvTab).not.toHaveBeenCalled()
  })

  // TEST-GAP 1: defaultEngineProfileId honored when set-but-unlocked
  it('profile action calls createConvTab with the default profileId', () => {
    setup()
    const profiles = [makeProfile('def', 'Default')]
    const action = resolveNewConversationAction(profiles, 'def')
    expect(action.kind).toBe('profile')
    const result = executeNewConversationAction('/dir', action, createTabInDir, createConvTab)
    expect(result).toBe('done')
    expect(createConvTab).toHaveBeenCalledWith('/dir', { profileId: 'def' })
    expect(createTabInDir).not.toHaveBeenCalled()
  })

  it('show-picker action returns "show-picker" without calling anything', () => {
    setup()
    const profiles = [makeProfile('p1', 'A')]
    const action = resolveNewConversationAction(profiles, '')
    expect(action.kind).toBe('show-picker')
    const result = executeNewConversationAction('/dir', action, createTabInDir, createConvTab)
    expect(result).toBe('show-picker')
    expect(createTabInDir).not.toHaveBeenCalled()
    expect(createConvTab).not.toHaveBeenCalled()
  })

  // TEST-GAP: DirPicker + enterprise lock = no bypass
  it('dir-picker conversation path respects enterprise lock (locked policy creates tab, not picker)', () => {
    setup()
    const profiles = [makeProfile('p1', 'A')]
    const policy = makePolicy()
    // Simulate what TabStrip's DirectoryPicker handler does after the fix:
    const action = resolveNewConversationAction(profiles, '', policy)
    const result = executeNewConversationAction('/selected/dir', action, createTabInDir, createConvTab)
    expect(result).toBe('done')
    // The lock overrides /selected/dir with the mandated directory
    expect(createConvTab).toHaveBeenCalledWith('/enterprise/dir', { profileId: 'orion-profile' })
  })

  // ── Context-menu "new tab in dir" path: the shared newTabInDirectory helper ──
  // The tab/group-pill/group-picker context menus all route their "New tab in
  // dir" action through `newTabInDirectory`. Two of those call sites previously
  // called `createTabInDirectory` directly and BYPASSED the enterprise lock
  // (the bug these tests pin). Driving the shared helper here means: if any of
  // those call sites stops routing through it, OR the helper stops honoring the
  // lock, the bypass returns and these tests go red.
  it('newTabInDirectory respects locked policy — mandated dir+profile, NOT the tab dir', () => {
    setup()
    const policy = makePolicy({ baseDirectory: '/corp', engineProfileId: 'corp-prof' })
    const result = newTabInDirectory('/tab/workdir', {
      profiles: [makeProfile('p1', 'A')],
      defaultProfileId: '',
      enterprisePolicy: policy,
      createTabInDir,
      createConvTab,
    })
    expect(result).toBe('done')
    // The lock must override the tab's own /tab/workdir with the mandated dir.
    expect(createConvTab).toHaveBeenCalledWith('/corp', { profileId: 'corp-prof' })
    // And it must NOT create a plain tab in the unmandated tab directory.
    expect(createTabInDir).not.toHaveBeenCalledWith('/tab/workdir', expect.anything())
  })

  it('newTabInDirectory with locked plain policy creates plain tab in mandated dir', () => {
    setup()
    const policy = makePolicy({ baseDirectory: '/corp/home', engineProfileId: '' })
    const result = newTabInDirectory('/tab/workdir', {
      profiles: [],
      defaultProfileId: '',
      enterprisePolicy: policy,
      createTabInDir,
      createConvTab,
      shouldUseWorktree: false,
    })
    expect(result).toBe('done')
    expect(createTabInDir).toHaveBeenCalledWith('/corp/home', false)
    expect(createConvTab).not.toHaveBeenCalled()
  })

  it('newTabInDirectory honors defaultEngineProfileId when unlocked', () => {
    setup()
    const result = newTabInDirectory('/tab/workdir', {
      profiles: [makeProfile('user-prof', 'User')],
      defaultProfileId: 'user-prof',
      enterprisePolicy: null,
      createTabInDir,
      createConvTab,
    })
    expect(result).toBe('done')
    expect(createConvTab).toHaveBeenCalledWith('/tab/workdir', { profileId: 'user-prof' })
  })

  it('newTabInDirectory shows picker when unlocked with profiles but no default', () => {
    setup()
    const result = newTabInDirectory('/tab/workdir', {
      profiles: [makeProfile('p1', 'A'), makeProfile('p2', 'B')],
      defaultProfileId: '',
      enterprisePolicy: null,
      createTabInDir,
      createConvTab,
    })
    // Context menus have no picker UI, but the helper must still surface the
    // resolver's intent rather than silently creating an unmandated tab.
    expect(result).toBe('show-picker')
    expect(createTabInDir).not.toHaveBeenCalled()
    expect(createConvTab).not.toHaveBeenCalled()
  })

  it('newTabInDirectory passes shouldUseWorktree through to plain creation', () => {
    setup()
    newTabInDirectory('/tab/workdir', {
      profiles: [],
      defaultProfileId: '',
      enterprisePolicy: null,
      createTabInDir,
      createConvTab,
      shouldUseWorktree: true,
    })
    expect(createTabInDir).toHaveBeenCalledWith('/tab/workdir', true)
  })

  it('passes shouldUseWorktree to createTabInDir for plain and locked-plain actions', () => {
    setup()
    const action: ReturnType<typeof resolveNewConversationAction> = { kind: 'plain' }
    executeNewConversationAction('/dir', action, createTabInDir, createConvTab, true)
    expect(createTabInDir).toHaveBeenCalledWith('/dir', true)
  })
})
