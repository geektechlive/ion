/**
 * Regression test for React rules-of-hooks violation in FileEditor.tsx.
 *
 * The bug (React error #310): three `useSessionStore` calls appeared AFTER
 * the early `if (typeof document === 'undefined') return null` guard. On
 * the render path where that guard fires, React sees fewer hooks than on a
 * normal render → hook count mismatch → renderer tear → engine SIGKILL.
 *
 * Fix: all hook calls must precede every early return in the component body.
 *
 * This test reads the source file as text and asserts that invariant
 * statically. It will fail if any `use*(` call is re-introduced below the
 * early-return guard, without requiring a full React/DOM render environment.
 *
 * Scope and limits (read before relying on this): this is a targeted static
 * guard for the *specific* regression that occurred — a hook call placed
 * textually after the `if (typeof document === 'undefined') return null`
 * guard. It is deliberately conservative: a hook smuggled in via a different
 * shape (a hook inside an `if` block above the guard, a hook in a callback,
 * a guard written with different text) would not be caught here. The precise
 * mechanism for general rules-of-hooks enforcement is the
 * `react-hooks/rules-of-hooks` ESLint rule; this file is the cheap,
 * always-on guard for the exact pattern that tore the renderer, not a
 * replacement for that rule. False negatives are acceptable; a false
 * positive (failing on correct code) is not.
 *
 * Covered invariants:
 *   1. The early-return guard line exists in the file.
 *   2. No React hook call (`use*(`) appears after the guard line.
 */

import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { resolve } from 'path'

// Resolve relative to this test file's location:
//   __tests__/FileEditor-hooks-order.test.ts
//   → components/FileEditor.tsx
const SOURCE_PATH = resolve(__dirname, '..', 'FileEditor.tsx')

// Pattern matching the early-return guard we're protecting against.
const EARLY_RETURN_RE = /if\s*\(\s*typeof\s+document\s*===\s*['"]undefined['"]\s*\)\s*return\s+null/

// Any call to a React hook: use<Name>(  — covers useState, useEffect,
// useCallback, useRef, useMemo, useSessionStore, useColors, usePreferencesStore,
// useFileEditorPanel, useFileEditorContent, and any custom hook.
// We deliberately exclude the import lines (they don't contain a call-site `(`).
const HOOK_CALL_RE = /\buse[A-Z][A-Za-z]*\s*\(/

describe('FileEditor hook ordering (React rules-of-hooks)', () => {
  const source = readFileSync(SOURCE_PATH, 'utf8')
  const lines = source.split('\n')

  // Find the line number (1-based) of the early-return guard.
  const guardLineIndex = lines.findIndex((l) => EARLY_RETURN_RE.test(l))

  it('contains the early-return document guard', () => {
    expect(guardLineIndex).toBeGreaterThan(-1)
  })

  it('has no hook calls after the early-return guard', () => {
    if (guardLineIndex === -1) return // covered by prior test

    const violations: { lineNumber: number; text: string }[] = []

    for (let i = guardLineIndex + 1; i < lines.length; i++) {
      const line = lines[i]
      // Skip import lines — they reference hook names but are not call sites.
      if (/^\s*import\b/.test(line)) continue
      if (HOOK_CALL_RE.test(line)) {
        violations.push({ lineNumber: i + 1, text: line.trim() })
      }
    }

    if (violations.length > 0) {
      const detail = violations
        .map((v) => `  line ${v.lineNumber}: ${v.text}`)
        .join('\n')
      throw new Error(
        `React #310: hook call(s) found after early return in FileEditor.tsx:\n${detail}\n` +
        `Move all hook calls above the 'if (typeof document === undefined) return null' guard.`,
      )
    }

    expect(violations).toHaveLength(0)
  })
})
