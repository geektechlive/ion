/**
 * Pure option-list helpers for the default-profile dropdown in GeneralCategory.
 * Kept in a separate file so they can be tested in the node environment
 * without pulling in the React/store dependency chain.
 */

import type { EngineProfile } from '../../../shared/types'

/** Option shape used by the default-profile dropdown. */
export interface ProfileOption {
  value: string
  label: string
}

/**
 * Derive the ordered option list for the default-profile dropdown.
 * First entry is always "Plain conversation (no extensions)" -> ''.
 * Subsequent entries are one entry per profile in the order stored.
 */
export function deriveProfileOptions(profiles: EngineProfile[]): ProfileOption[] {
  const plain: ProfileOption = { value: '', label: 'Plain conversation (no extensions)' }
  return [plain, ...profiles.map((p) => ({ value: p.id, label: p.name }))]
}

/**
 * Return the option value that should be selected given the current stored
 * defaultEngineProfileId. Falls back to '' when the stored id no longer
 * refers to a known profile (e.g. profile was deleted).
 */
export function resolveSelectedProfileOption(
  storedId: string,
  profiles: EngineProfile[],
): string {
  if (!storedId) return ''
  return profiles.some((p) => p.id === storedId) ? storedId : ''
}
