/**
 * Commit form with split-button toolbar, GPG/sign-off toggles, 50/72 ruler,
 * draft autosave, Conventional Commits prefix, and post-commit Undo banner.
 *
 * Last-used action (Commit vs Commit & Push) persisted to localStorage; that
 * choice becomes the primary button on next mount.
 */

import React, { useState, useEffect, useRef, useCallback } from 'react'
import { Check, Robot, ArrowElbowDownRight, ArrowCounterClockwise, ShieldCheck } from '@phosphor-icons/react'
import { useColors } from '../../theme'

interface CommitFormProps {
  directory: string
  branch: string
  stagedCount: number
  onCommit: (message: string, amend: boolean, opts?: { signoff?: boolean; gpg?: boolean }) => Promise<boolean>
  onQuickCommit: () => void
  onPush: () => void
}

type PrimaryAction = 'commit' | 'commit-push'

const PRIMARY_KEY = 'ion:commit-primary-action'
const CONVENTIONAL_PREFIXES = ['feat', 'fix', 'chore', 'docs', 'refactor', 'test', 'style', 'perf']

function draftKey(dir: string, branch: string) { return `ion:commit-draft:${dir}:${branch}` }

export function CommitForm({ directory, branch, stagedCount, onCommit, onQuickCommit, onPush }: CommitFormProps) {
  const colors = useColors()
  const [message, setMessage] = useState('')
  const [amend, setAmend] = useState(false)
  const [signOff, setSignOff] = useState(false)
  const [gpg, setGpg] = useState(false)
  const [primary, setPrimary] = useState<PrimaryAction>(() => (typeof localStorage !== 'undefined' && (localStorage.getItem(PRIMARY_KEY) as PrimaryAction)) || 'commit')
  const [bannerOpen, setBannerOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [prefixOpen, setPrefixOpen] = useState(false)
  const taRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    const saved = localStorage.getItem(draftKey(directory, branch))
    setMessage(saved ?? '')
    setAmend(false)
  }, [directory, branch])

  useEffect(() => {
    const k = draftKey(directory, branch)
    message.trim() ? localStorage.setItem(k, message) : localStorage.removeItem(k)
  }, [message, directory, branch])

  useEffect(() => {
    const ta = taRef.current; if (!ta) return
    ta.style.height = '20px'; ta.style.height = Math.min(ta.scrollHeight, 120) + 'px'
  }, [message])

  useEffect(() => {
    if (typeof localStorage !== 'undefined') localStorage.setItem(PRIMARY_KEY, primary)
  }, [primary])

  const handleCommit = useCallback(async (alsoPush: boolean) => {
    if (!message.trim() || (stagedCount === 0 && !amend)) return
    setBusy(true)
    try {
      const ok = await onCommit(message.trim(), amend, { signoff: signOff, gpg })
      if (ok) {
        setMessage('')
        localStorage.removeItem(draftKey(directory, branch))
        setBannerOpen(true)
        setTimeout(() => setBannerOpen(false), 10000)
        if (alsoPush) onPush()
      }
    } finally {
      setBusy(false)
    }
  }, [message, stagedCount, amend, signOff, gpg, onCommit, onPush, directory, branch])

  const undoCommit = useCallback(async () => {
    try {
      const head = (await window.ion.gitGraph(directory, 0, 2)).commits[1]?.fullHash
      if (head) {
        await window.ion.gitReset(directory, head, 'soft')
        setBannerOpen(false)
      }
    } catch {}
  }, [directory])

  const insertPrefix = (prefix: string) => {
    setMessage((m) => (m.startsWith(prefix + ':') ? m : `${prefix}: ${m}`))
    setPrefixOpen(false)
    setTimeout(() => taRef.current?.focus(), 0)
  }

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault()
      handleCommit(primary === 'commit-push')
    }
  }, [handleCommit, primary])

  const subjectLen = (message.split('\n')[0] || '').length
  const canCommit = message.trim().length > 0 && (stagedCount > 0 || amend) && !busy
  const overLimit = subjectLen > 50
  const farOver = subjectLen > 72

  return (
    <div style={{ flexShrink: 0, borderBottom: `1px solid ${colors.containerBorder}`, background: colors.surfacePrimary, padding: '4px 8px' }}>
      <div style={{ position: 'relative' }}>
        <textarea
          ref={taRef}
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          onKeyDown={handleKeyDown}
          onClick={(e) => e.stopPropagation()}
          placeholder="Commit message…"
          className="text-[10px] bg-transparent outline-none rounded px-1.5 py-1 w-full resize-none"
          style={{
            color: colors.textPrimary,
            border: `1px solid ${colors.containerBorder}`,
            minHeight: 20,
            maxHeight: 120,
            lineHeight: '14px',
            fontFamily: 'var(--font-mono, monospace)',
            backgroundImage: 'linear-gradient(to right, transparent calc(50ch + 6px), rgba(212,168,67,0.12) calc(50ch + 6px) calc(50ch + 6px + 1px), transparent calc(50ch + 6px + 1px), transparent calc(72ch + 6px), rgba(196,112,96,0.16) calc(72ch + 6px) calc(72ch + 6px + 1px), transparent calc(72ch + 6px + 1px))',
            backgroundRepeat: 'no-repeat',
          }}
        />
      </div>

      <div className="flex items-center gap-1 mt-1">
        <span className="text-[9px]" style={{ color: farOver ? '#c47060' : overLimit ? '#d4a843' : colors.textMuted }}>
          {subjectLen || ''}
        </span>

        <div style={{ position: 'relative' }}>
          <button
            onClick={() => setPrefixOpen((v) => !v)}
            className="text-[9px] px-1 py-0.5 rounded"
            style={{ color: colors.textTertiary, background: 'transparent' }}
            title="Insert conventional-commits prefix"
          >cc:</button>
          {prefixOpen && (
            <div className="absolute left-0 mt-1 rounded shadow text-[10px]" style={{ background: colors.surfaceSecondary, border: `1px solid ${colors.containerBorder}`, zIndex: 5 }}>
              {CONVENTIONAL_PREFIXES.map((p) => (
                <button key={p} onClick={() => insertPrefix(p)} className="block w-full text-left px-2 py-0.5 hover:opacity-80" style={{ color: colors.textSecondary }}>{p}</button>
              ))}
            </div>
          )}
        </div>

        <button onClick={() => setAmend(!amend)} className="text-[9px] px-1 py-0.5 rounded flex items-center gap-0.5" style={{ color: amend ? colors.accent : colors.textTertiary, background: amend ? colors.accentLight : 'transparent' }} title="Amend last commit">
          <ArrowElbowDownRight size={9} /> Amend
        </button>
        <button onClick={() => setSignOff(!signOff)} className="text-[9px] px-1 py-0.5 rounded flex items-center gap-0.5" style={{ color: signOff ? colors.accent : colors.textTertiary, background: signOff ? colors.accentLight : 'transparent' }} title="Sign off commit (-s)">✓ Sign</button>
        <button onClick={() => setGpg(!gpg)} className="text-[9px] px-1 py-0.5 rounded flex items-center gap-0.5" style={{ color: gpg ? colors.accent : colors.textTertiary, background: gpg ? colors.accentLight : 'transparent' }} title="GPG-sign commit (-S)">
          <ShieldCheck size={9} /> GPG
        </button>

        <div style={{ flex: 1 }} />

        <button onClick={onQuickCommit} className="p-0.5 rounded transition-colors flex items-center" style={{ color: colors.textTertiary }} title="Let Ion commit">
          <Robot size={12} />
        </button>

        <div className="flex items-center">
          <button
            onClick={() => handleCommit(primary === 'commit-push')}
            disabled={!canCommit}
            className="flex items-center gap-0.5 text-[10px] px-1.5 py-0.5 rounded-l"
            style={{
              color: canCommit ? '#fff' : colors.textMuted,
              background: canCommit ? colors.accent : 'transparent',
              border: canCommit ? 'none' : `1px solid ${colors.containerBorder}`,
              cursor: canCommit ? 'pointer' : 'not-allowed',
              fontWeight: 500,
            }}
          >
            <Check size={10} weight="bold" />
            {primary === 'commit-push' ? ' Commit & Push' : ' Commit'}{amend ? ' (amend)' : ''}
          </button>
          <button
            onClick={() => setPrimary((p) => p === 'commit' ? 'commit-push' : 'commit')}
            className="text-[10px] px-1 py-0.5 rounded-r"
            style={{
              color: canCommit ? '#fff' : colors.textMuted,
              background: canCommit ? colors.accent : 'transparent',
              borderLeft: canCommit ? '1px solid rgba(255,255,255,0.2)' : `1px solid ${colors.containerBorder}`,
              cursor: 'pointer',
            }}
            title="Toggle Commit / Commit & Push"
          >
            {primary === 'commit' ? '↑' : '⌥'}
          </button>
        </div>
      </div>

      {bannerOpen && (
        <div className="flex items-center gap-2 mt-1 text-[9px]" style={{ color: colors.accent }}>
          <span>✓ Committed</span>
          <button onClick={onPush} className="underline" style={{ color: colors.accent }}>Push</button>
          <button onClick={undoCommit} className="flex items-center gap-0.5" style={{ color: '#c47060' }}>
            <ArrowCounterClockwise size={9} /> Undo
          </button>
          <button onClick={() => setBannerOpen(false)} style={{ color: colors.textTertiary }}>Dismiss</button>
        </div>
      )}
    </div>
  )
}
