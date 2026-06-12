import React, { useState } from 'react'
import { Archive, ArrowCounterClockwise, CheckCircle, WarningCircle, ShieldCheck, FolderOpen } from '@phosphor-icons/react'
import { useColors } from '../../theme'
import { SettingHeading } from './SettingHeading'

type ExportScope = 'currently-open' | 'all'
type ConflictPolicy = 'skip' | 'overwrite' | 'rename'

interface ExportPreview {
  conversationCount: number
  totalUncompressedBytes: number
  estimatedCompressedBytes: number
}

interface ExportResult {
  ok: boolean
  error?: string
  destinationPath?: string
  conversationCount?: number
  bytesWritten?: number
}

interface RestoreManifest {
  version: number
  createdAt: string
  ionVersion: string
  scope: ExportScope
  conversationCount: number
  backendSnapshot: 'api' | 'cli'
  hostname: string
}

interface RestoreResult {
  ok: boolean
  error?: string
  restored: number
  skipped: number
  overwritten: number
  renamed: number
  errors: string[]
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

export function BackupRestoreCategory() {
  const colors = useColors()

  // ─── Export state ───
  const [exportOpen, setExportOpen] = useState(false)
  const [exportScope, setExportScope] = useState<ExportScope>('all')
  const [exportPreview, setExportPreview] = useState<ExportPreview | null>(null)
  const [exporting, setExporting] = useState(false)
  const [exportProgress, setExportProgress] = useState<{ current: number; total: number; label: string } | null>(null)
  const [exportResult, setExportResult] = useState<ExportResult | null>(null)

  // ─── Restore state ───
  const [restoreOpen, setRestoreOpen] = useState(false)
  const [restorePreview, setRestorePreview] = useState<{ sourcePath?: string; manifest?: RestoreManifest } | null>(null)
  const [conflictPolicy, setConflictPolicy] = useState<ConflictPolicy>('skip')
  const [restoreTabs, setRestoreTabs] = useState(false)
  const [restoring, setRestoring] = useState(false)
  const [restoreResult, setRestoreResult] = useState<RestoreResult | null>(null)

  // ─── Card style (matches MigrationCategory) ───
  const cardStyle: React.CSSProperties = {
    background: colors.surfacePrimary,
    border: `1px solid ${colors.containerBorder}`,
    borderRadius: 8,
    padding: '10px 12px',
    marginBottom: 8,
  }

  // ─── Export handlers ───
  const openExportModal = async () => {
    setExportOpen(true)
    setExportResult(null)
    setExportPreview(null)
    await refreshExportPreview(exportScope)
  }

  const refreshExportPreview = async (scope: ExportScope) => {
    try {
      const res = await window.ion?.conversationExportPreview(scope)
      if (res?.ok) {
        setExportPreview({
          conversationCount: res.conversationCount ?? 0,
          totalUncompressedBytes: res.totalUncompressedBytes ?? 0,
          estimatedCompressedBytes: res.estimatedCompressedBytes ?? 0,
        })
      } else {
        setExportPreview(null)
      }
    } catch {
      setExportPreview(null)
    }
  }

  const handleScopeChange = async (scope: ExportScope) => {
    setExportScope(scope)
    await refreshExportPreview(scope)
  }

  const handleExport = async () => {
    if (exporting) return
    setExporting(true)
    setExportProgress(null)
    setExportResult(null)
    const unsubscribe = window.ion?.onConversationBackupProgress?.((data) => setExportProgress(data))
    try {
      const res = await window.ion?.conversationExport({ scope: exportScope })
      setExportResult(res || { ok: false, error: 'no response', restored: 0, skipped: 0, overwritten: 0, renamed: 0, errors: [] } as any)
    } catch (err: any) {
      setExportResult({ ok: false, error: err.message })
    } finally {
      setExporting(false)
      setExportProgress(null)
      unsubscribe?.()
    }
  }

  // ─── Restore handlers ───
  const openRestoreModal = async () => {
    setRestoreOpen(true)
    setRestoreResult(null)
    setRestorePreview(null)
    try {
      const res = await window.ion?.conversationRestorePreview()
      if (res?.ok && res.manifest) {
        setRestorePreview({ sourcePath: res.sourcePath, manifest: res.manifest as RestoreManifest })
      } else if (res?.error === 'cancelled') {
        setRestoreOpen(false)
      } else {
        setRestorePreview({ sourcePath: res?.sourcePath })
        setRestoreResult({ ok: false, error: res?.error || 'failed to read backup', restored: 0, skipped: 0, overwritten: 0, renamed: 0, errors: [] })
      }
    } catch (err: any) {
      setRestoreResult({ ok: false, error: err.message, restored: 0, skipped: 0, overwritten: 0, renamed: 0, errors: [] })
    }
  }

  const handleRestore = async () => {
    if (restoring || !restorePreview?.sourcePath) return
    setRestoring(true)
    setRestoreResult(null)
    try {
      const res = await window.ion?.conversationRestore({
        sourcePath: restorePreview.sourcePath,
        conflictPolicy,
        restoreTabs,
      })
      setRestoreResult(res || { ok: false, error: 'no response', restored: 0, skipped: 0, overwritten: 0, renamed: 0, errors: [] })
    } catch (err: any) {
      setRestoreResult({ ok: false, error: err.message, restored: 0, skipped: 0, overwritten: 0, renamed: 0, errors: [] })
    } finally {
      setRestoring(false)
    }
  }

  // ─── Render ───
  return (
    <>
      <SettingHeading>Backup &amp; Restore</SettingHeading>

      <div style={{ ...cardStyle, display: 'flex', alignItems: 'center', gap: 8 }}>
        <ShieldCheck size={16} color={colors.accent} />
        <span style={{ fontSize: 12, color: colors.textSecondary }}>
          Export a zip of your conversations as a portable backup. Restoring is always opt-in and never overwrites local files without explicit confirmation.
        </span>
      </div>

      {!exportOpen && !restoreOpen && (
        <div style={{ display: 'flex', gap: 8, marginTop: 6 }}>
          <button onClick={openExportModal} style={primaryButtonStyle(colors)}>
            <Archive size={16} />
            Export conversations…
          </button>
          <button onClick={openRestoreModal} style={secondaryButtonStyle(colors)}>
            <ArrowCounterClockwise size={16} />
            Restore from backup…
          </button>
        </div>
      )}

      {exportOpen && (
        <ExportModalContent
          scope={exportScope}
          preview={exportPreview}
          exporting={exporting}
          progress={exportProgress}
          result={exportResult}
          onScopeChange={handleScopeChange}
          onExport={handleExport}
          onClose={() => { setExportOpen(false); setExportResult(null); setExportPreview(null) }}
          cardStyle={cardStyle}
          colors={colors}
        />
      )}

      {restoreOpen && (
        <RestoreModalContent
          preview={restorePreview}
          conflictPolicy={conflictPolicy}
          restoreTabs={restoreTabs}
          restoring={restoring}
          result={restoreResult}
          onConflictPolicyChange={setConflictPolicy}
          onRestoreTabsChange={setRestoreTabs}
          onRestore={handleRestore}
          onClose={() => { setRestoreOpen(false); setRestoreResult(null); setRestorePreview(null) }}
          cardStyle={cardStyle}
          colors={colors}
        />
      )}
    </>
  )
}

function primaryButtonStyle(colors: ReturnType<typeof useColors>): React.CSSProperties {
  return {
    display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
    flex: 1, padding: '8px 12px',
    background: colors.accent, border: 'none', borderRadius: 8,
    color: '#fff', cursor: 'pointer', fontSize: 13, fontWeight: 600,
  }
}

function secondaryButtonStyle(colors: ReturnType<typeof useColors>): React.CSSProperties {
  return {
    display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
    flex: 1, padding: '8px 12px',
    background: colors.containerBg, border: `1px solid ${colors.containerBorder}`, borderRadius: 8,
    color: colors.textPrimary, cursor: 'pointer', fontSize: 13, fontWeight: 600,
  }
}

// ─── Export modal sub-component ───

interface ExportModalContentProps {
  scope: ExportScope
  preview: ExportPreview | null
  exporting: boolean
  progress: { current: number; total: number; label: string } | null
  result: ExportResult | null
  onScopeChange: (scope: ExportScope) => void
  onExport: () => void
  onClose: () => void
  cardStyle: React.CSSProperties
  colors: ReturnType<typeof useColors>
}

function ExportModalContent({
  scope, preview, exporting, progress, result, onScopeChange, onExport, onClose, cardStyle, colors,
}: ExportModalContentProps) {
  return (
    <div style={{ ...cardStyle, padding: 14, marginTop: 8 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
        <span style={{ fontSize: 13, fontWeight: 600, color: colors.textPrimary }}>Export conversations</span>
        <button
          onClick={onClose}
          disabled={exporting}
          style={{ background: 'none', border: 'none', cursor: 'pointer', color: colors.textTertiary, fontSize: 12 }}
        >
          {exporting ? '' : 'Cancel'}
        </button>
      </div>

      <div style={{ fontSize: 12, color: colors.textSecondary, marginBottom: 6 }}>Scope</div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginBottom: 12 }}>
        <ScopeRadio
          label="Currently open"
          description="Just the conversations your tabs reference, plus their chained continuations."
          checked={scope === 'currently-open'}
          onChange={() => onScopeChange('currently-open')}
          disabled={exporting}
          colors={colors}
        />
        <ScopeRadio
          label="All conversations"
          description="Every conversation file on disk — full archival backup."
          checked={scope === 'all'}
          onChange={() => onScopeChange('all')}
          disabled={exporting}
          colors={colors}
        />
      </div>

      {preview && (
        <div style={{ ...cardStyle, marginBottom: 10 }}>
          <div style={{ fontSize: 12, color: colors.textSecondary }}>
            <strong style={{ color: colors.textPrimary }}>{preview.conversationCount}</strong> conversations,
            {' ~'}{formatBytes(preview.estimatedCompressedBytes)} compressed
            (uncompressed: {formatBytes(preview.totalUncompressedBytes)})
          </div>
        </div>
      )}

      {exporting && progress && (
        <div style={{ ...cardStyle, marginBottom: 10 }}>
          <div style={{ fontSize: 12, color: colors.textPrimary, marginBottom: 4 }}>
            Compressing {progress.current} of {progress.total}…
          </div>
          <div style={{ height: 4, background: colors.containerBg, borderRadius: 2, overflow: 'hidden' }}>
            <div style={{
              height: '100%', background: colors.accent,
              width: `${progress.total > 0 ? (progress.current / progress.total) * 100 : 0}%`,
              transition: 'width 0.15s ease',
            }} />
          </div>
        </div>
      )}

      {!result && (
        <button
          onClick={onExport}
          disabled={exporting || !preview || preview.conversationCount === 0}
          style={{
            display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
            width: '100%', padding: '8px 12px',
            background: !exporting && preview && preview.conversationCount > 0 ? colors.accent : colors.containerBg,
            border: 'none', borderRadius: 8,
            color: !exporting && preview && preview.conversationCount > 0 ? '#fff' : colors.textTertiary,
            cursor: !exporting && preview && preview.conversationCount > 0 ? 'pointer' : 'default',
            fontSize: 13, fontWeight: 600,
          }}
        >
          <Archive size={16} />
          {exporting ? 'Exporting…' : 'Choose destination and export'}
        </button>
      )}

      {result && (
        <ExportResultCard result={result} cardStyle={cardStyle} colors={colors} onClose={onClose} />
      )}
    </div>
  )
}

function ExportResultCard({ result, cardStyle, colors, onClose }: { result: ExportResult; cardStyle: React.CSSProperties; colors: ReturnType<typeof useColors>; onClose: () => void }) {
  if (result.ok) {
    return (
      <div style={{ ...cardStyle, borderColor: '#34d399' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 6 }}>
          <CheckCircle size={16} color="#34d399" weight="fill" />
          <span style={{ fontSize: 12, fontWeight: 600, color: colors.textPrimary }}>
            Exported {result.conversationCount} conversation{result.conversationCount === 1 ? '' : 's'}
          </span>
        </div>
        <div style={{ fontSize: 11, color: colors.textTertiary, wordBreak: 'break-all', marginBottom: 8 }}>
          {result.destinationPath} ({formatBytes(result.bytesWritten ?? 0)})
        </div>
        <div style={{ display: 'flex', gap: 6 }}>
          <button
            onClick={onClose}
            style={{
              flex: 1, padding: '6px 10px', fontSize: 12,
              background: colors.containerBg, border: `1px solid ${colors.containerBorder}`,
              borderRadius: 6, color: colors.textPrimary, cursor: 'pointer',
            }}
          >
            Done
          </button>
        </div>
      </div>
    )
  }
  if (result.error === 'cancelled') {
    return (
      <div style={cardStyle}>
        <span style={{ fontSize: 12, color: colors.textTertiary }}>Export cancelled.</span>
      </div>
    )
  }
  return (
    <div style={{ ...cardStyle, borderColor: '#f87171' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 4 }}>
        <WarningCircle size={16} color="#f87171" weight="fill" />
        <span style={{ fontSize: 12, fontWeight: 600, color: colors.textPrimary }}>Export failed</span>
      </div>
      <div style={{ fontSize: 11, color: colors.textTertiary }}>{result.error}</div>
    </div>
  )
}

function ScopeRadio({ label, description, checked, onChange, disabled, colors }: {
  label: string; description: string; checked: boolean; onChange: () => void; disabled: boolean; colors: ReturnType<typeof useColors>
}) {
  return (
    <label
      style={{
        display: 'flex', alignItems: 'flex-start', gap: 8,
        padding: '8px 10px', borderRadius: 6,
        background: checked ? `${colors.accent}11` : colors.surfacePrimary,
        border: `1px solid ${checked ? colors.accent : colors.containerBorder}`,
        cursor: disabled ? 'default' : 'pointer', opacity: disabled ? 0.6 : 1,
      }}
    >
      <input type="radio" checked={checked} onChange={onChange} disabled={disabled} style={{ accentColor: colors.accent, marginTop: 2 }} />
      <div style={{ flex: 1 }}>
        <div style={{ fontSize: 13, fontWeight: 500, color: colors.textPrimary }}>{label}</div>
        <div style={{ fontSize: 11, color: colors.textTertiary, marginTop: 2 }}>{description}</div>
      </div>
    </label>
  )
}

// ─── Restore modal sub-component ───

interface RestoreModalContentProps {
  preview: { sourcePath?: string; manifest?: RestoreManifest } | null
  conflictPolicy: ConflictPolicy
  restoreTabs: boolean
  restoring: boolean
  result: RestoreResult | null
  onConflictPolicyChange: (p: ConflictPolicy) => void
  onRestoreTabsChange: (v: boolean) => void
  onRestore: () => void
  onClose: () => void
  cardStyle: React.CSSProperties
  colors: ReturnType<typeof useColors>
}

function RestoreModalContent({
  preview, conflictPolicy, restoreTabs, restoring, result,
  onConflictPolicyChange, onRestoreTabsChange, onRestore, onClose, cardStyle, colors,
}: RestoreModalContentProps) {
  return (
    <div style={{ ...cardStyle, padding: 14, marginTop: 8 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
        <span style={{ fontSize: 13, fontWeight: 600, color: colors.textPrimary }}>Restore from backup</span>
        <button
          onClick={onClose}
          disabled={restoring}
          style={{ background: 'none', border: 'none', cursor: 'pointer', color: colors.textTertiary, fontSize: 12 }}
        >
          {restoring ? '' : 'Cancel'}
        </button>
      </div>

      {!preview?.manifest && !result && (
        <div style={cardStyle}>
          <span style={{ fontSize: 12, color: colors.textTertiary }}>Choose a backup file to inspect…</span>
        </div>
      )}

      {preview?.manifest && (
        <div style={{ ...cardStyle, marginBottom: 10 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 4 }}>
            <FolderOpen size={16} color={colors.accent} />
            <span style={{ fontSize: 12, fontWeight: 600, color: colors.textPrimary }}>Backup summary</span>
          </div>
          <div style={{ fontSize: 11, color: colors.textTertiary, lineHeight: 1.5 }}>
            Created {new Date(preview.manifest.createdAt).toLocaleString()}<br />
            On host <strong style={{ color: colors.textSecondary }}>{preview.manifest.hostname}</strong>{' '}
            (Ion {preview.manifest.ionVersion}, backend {preview.manifest.backendSnapshot})<br />
            Contains <strong style={{ color: colors.textSecondary }}>{preview.manifest.conversationCount}</strong> conversations
            ({preview.manifest.scope === 'all' ? 'full archive' : 'currently-open subset'})
          </div>
        </div>
      )}

      {preview?.manifest && !result && (
        <>
          <div style={{ fontSize: 12, color: colors.textSecondary, marginBottom: 6 }}>Conflict policy</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginBottom: 10 }}>
            <ScopeRadio
              label="Skip existing"
              description="Keep local files when the backup contains the same conversation ID. Default — safest."
              checked={conflictPolicy === 'skip'}
              onChange={() => onConflictPolicyChange('skip')}
              disabled={restoring}
              colors={colors}
            />
            <ScopeRadio
              label="Overwrite existing"
              description="Replace local files with the backup version. Use only when you trust the backup is newer."
              checked={conflictPolicy === 'overwrite'}
              onChange={() => onConflictPolicyChange('overwrite')}
              disabled={restoring}
              colors={colors}
            />
            <ScopeRadio
              label="Restore as new IDs"
              description="Give every restored conversation a fresh ID. Useful when carrying a backup from another machine."
              checked={conflictPolicy === 'rename'}
              onChange={() => onConflictPolicyChange('rename')}
              disabled={restoring}
              colors={colors}
            />
          </div>

          <label style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12, cursor: restoring ? 'default' : 'pointer' }}>
            <input
              type="checkbox"
              checked={restoreTabs}
              onChange={(e) => onRestoreTabsChange(e.target.checked)}
              disabled={restoring}
              style={{ accentColor: colors.accent }}
            />
            <span style={{ fontSize: 12, color: colors.textSecondary }}>
              Also restore tab layout from backup (merged — local tabs are preserved)
            </span>
          </label>

          <button
            onClick={onRestore}
            disabled={restoring}
            style={{
              display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
              width: '100%', padding: '8px 12px',
              background: restoring ? colors.containerBg : colors.accent, border: 'none', borderRadius: 8,
              color: restoring ? colors.textTertiary : '#fff',
              cursor: restoring ? 'default' : 'pointer', fontSize: 13, fontWeight: 600,
            }}
          >
            <ArrowCounterClockwise size={16} />
            {restoring ? 'Restoring…' : 'Restore'}
          </button>
        </>
      )}

      {result && (
        <RestoreResultCard result={result} cardStyle={cardStyle} colors={colors} onClose={onClose} />
      )}
    </div>
  )
}

function RestoreResultCard({ result, cardStyle, colors, onClose }: { result: RestoreResult; cardStyle: React.CSSProperties; colors: ReturnType<typeof useColors>; onClose: () => void }) {
  if (result.ok) {
    return (
      <div style={{ ...cardStyle, borderColor: '#34d399' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 6 }}>
          <CheckCircle size={16} color="#34d399" weight="fill" />
          <span style={{ fontSize: 12, fontWeight: 600, color: colors.textPrimary }}>Restore complete</span>
        </div>
        <div style={{ fontSize: 11, color: colors.textTertiary, lineHeight: 1.5 }}>
          Restored {result.restored} new files
          {result.skipped > 0 ? `, skipped ${result.skipped}` : ''}
          {result.overwritten > 0 ? `, overwrote ${result.overwritten}` : ''}
          {result.renamed > 0 ? `, renamed ${result.renamed}` : ''}.
          {result.errors.length > 0 && ` ${result.errors.length} file error${result.errors.length === 1 ? '' : 's'} (see desktop.log).`}
        </div>
        <div style={{ fontSize: 11, color: colors.textTertiary, marginTop: 6, fontStyle: 'italic' }}>
          Restart Ion to see restored tabs in the tab strip.
        </div>
        <button
          onClick={onClose}
          style={{
            marginTop: 8, width: '100%', padding: '6px 10px', fontSize: 12,
            background: colors.containerBg, border: `1px solid ${colors.containerBorder}`,
            borderRadius: 6, color: colors.textPrimary, cursor: 'pointer',
          }}
        >
          Done
        </button>
      </div>
    )
  }
  return (
    <div style={{ ...cardStyle, borderColor: '#f87171' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 4 }}>
        <WarningCircle size={16} color="#f87171" weight="fill" />
        <span style={{ fontSize: 12, fontWeight: 600, color: colors.textPrimary }}>Restore failed</span>
      </div>
      <div style={{ fontSize: 11, color: colors.textTertiary }}>{result.error}</div>
    </div>
  )
}
