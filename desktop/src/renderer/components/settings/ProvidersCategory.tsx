import React, { useState, useEffect, useCallback } from 'react'
import { useColors } from '../../theme'
import { SettingSection } from './SettingSection'
import { SettingHeading } from './SettingHeading'
import { useModelStore } from '../../stores/model-store'
import { getProviderDisplayName } from '../../../shared/types-models'
import type { ProviderEntry } from '../../../shared/types-models'

/** Providers that support API key auth (entered manually). */
const API_KEY_PROVIDERS = new Set([
  'groq', 'cerebras', 'mistral',
  'openrouter', 'together', 'fireworks', 'xai', 'deepseek',
  'azure',
])

/** Providers that support OAuth sign-in. */
const OAUTH_PROVIDERS = new Set(['openai', 'google', 'github-copilot'])

/** Providers that need no auth. */
const NO_AUTH_PROVIDERS = new Set(['ollama'])

/** Hint text shown per provider. */
const PROVIDER_HINTS: Record<string, string> = {
  anthropic: 'Anthropic uses the Claude CLI backend for OAuth. Configure in Backend Mode.',
  ollama: 'Ollama runs locally — no API key needed.',
  bedrock: 'AWS Bedrock uses AWS credentials (AWS_ACCESS_KEY_ID env var).',
  azure: 'Set AZURE_OPENAI_API_KEY or enter below.',
  'github-copilot': 'Sign in with your GitHub account to use Copilot models.',
}

/** Display names for OAuth providers. */
const OAUTH_BUTTON_LABELS: Record<string, string> = {
  openai: 'Sign in with OpenAI',
  google: 'Sign in with Google',
  'github-copilot': 'Sign in with GitHub',
}

export function ProvidersCategory() {
  const colors = useColors()
  const fetchModels = useModelStore((s) => s.fetchModels)
  const providers = useModelStore((s) => s.providers)
  const loading = useModelStore((s) => s.loading)

  useEffect(() => { fetchModels() }, [fetchModels])

  return (
    <>
      <SettingHeading first>Providers</SettingHeading>

      {loading && providers.length === 0 && (
        <div style={{ padding: '12px 0', fontSize: 12, color: colors.textTertiary }}>
          Loading providers…
        </div>
      )}

      {providers.map((p) => (
        <ProviderRow key={p.id} provider={p} colors={colors} onCredentialSaved={fetchModels} />
      ))}

      {providers.length === 0 && !loading && (
        <div style={{ padding: '12px 0', fontSize: 12, color: colors.textTertiary }}>
          No providers available. Start the engine to see providers.
        </div>
      )}
    </>
  )
}

function ProviderRow({ provider, colors, onCredentialSaved }: {
  provider: ProviderEntry
  colors: ReturnType<typeof useColors>
  onCredentialSaved: () => void
}) {
  const [apiKey, setApiKey] = useState('')
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // OAuth state
  const [oauthLoading, setOauthLoading] = useState(false)
  const [deviceCode, setDeviceCode] = useState<{ userCode: string; verificationUri: string; deviceCode: string; interval: number; expiresIn: number } | null>(null)

  const isApiKeyProvider = API_KEY_PROVIDERS.has(provider.id)
  const isOAuthProvider = OAUTH_PROVIDERS.has(provider.id)
  const isNoAuth = NO_AUTH_PROVIDERS.has(provider.id)
  const hint = PROVIDER_HINTS[provider.id] || null

  const handleSave = useCallback(async () => {
    if (!apiKey.trim()) return
    setSaving(true)
    setError(null)
    try {
      const result = await window.ion.storeCredential(provider.id, apiKey.trim())
      if (result.ok) {
        setSaved(true)
        setApiKey('')
        setTimeout(() => setSaved(false), 2000)
        onCredentialSaved()
      } else {
        setError(result.error || 'Failed to save')
      }
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }, [apiKey, provider.id, onCredentialSaved])

  const handleOAuthLogin = useCallback(async () => {
    setOauthLoading(true)
    setError(null)
    setDeviceCode(null)

    try {
      if (provider.id === 'github-copilot') {
        // Two-phase device code flow
        const dcResult = await window.ion.oauthDeviceCode(provider.id)
        if (!dcResult.ok) {
          setError(dcResult.error || 'Failed to start device flow')
          setOauthLoading(false)
          return
        }

        setDeviceCode({
          userCode: dcResult.userCode!,
          verificationUri: dcResult.verificationUri!,
          deviceCode: dcResult.deviceCode!,
          interval: dcResult.interval!,
          expiresIn: dcResult.expiresIn!,
        })

        // Open verification URL
        window.ion.openExternal(dcResult.verificationUri!)

        // Poll for completion
        const pollResult = await window.ion.oauthDevicePoll(
          dcResult.deviceCode!, dcResult.interval!, dcResult.expiresIn!,
        )
        if (!pollResult.ok) {
          setError(pollResult.error || 'Device flow failed')
        } else {
          onCredentialSaved()
        }
        setDeviceCode(null)
      } else {
        // Authorization code + PKCE flow
        const result = await window.ion.startOAuth(provider.id)
        if (!result.ok) {
          setError(result.error || 'OAuth failed')
        } else {
          onCredentialSaved()
        }
      }
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setOauthLoading(false)
    }
  }, [provider.id, onCredentialSaved])

  const handleOAuthLogout = useCallback(async () => {
    setError(null)
    try {
      await window.ion.logoutOAuth(provider.id)
      onCredentialSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }, [provider.id, onCredentialSaved])

  const statusBadge = provider.hasAuth
    ? { label: provider.authSource || 'configured', color: '#22c55e' }
    : { label: 'not configured', color: colors.textTertiary }

  return (
    <SettingSection
      label={getProviderDisplayName(provider.id)}
      description={hint || undefined}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: (isApiKeyProvider || isOAuthProvider) && !provider.hasAuth ? 8 : 0 }}>
        <span
          style={{
            fontSize: 11,
            fontWeight: 500,
            color: statusBadge.color,
            padding: '2px 8px',
            borderRadius: 4,
            background: provider.hasAuth ? 'rgba(34,197,94,0.1)' : `${colors.textTertiary}15`,
          }}
        >
          {statusBadge.label}
        </span>

        {/* Show sign-out button for authenticated OAuth providers */}
        {isOAuthProvider && provider.hasAuth && (
          <button
            onClick={handleOAuthLogout}
            style={{
              padding: '2px 8px',
              background: 'transparent',
              color: colors.textTertiary,
              border: `1px solid ${colors.containerBorder}`,
              borderRadius: 4,
              fontSize: 11,
              cursor: 'pointer',
            }}
          >
            Sign out
          </button>
        )}
      </div>

      {/* OAuth sign-in button */}
      {isOAuthProvider && !provider.hasAuth && !deviceCode && (
        <button
          onClick={handleOAuthLogin}
          disabled={oauthLoading}
          style={{
            padding: '8px 16px',
            background: colors.accent,
            color: '#fff',
            border: 'none',
            borderRadius: 6,
            fontSize: 12,
            fontWeight: 600,
            cursor: oauthLoading ? 'not-allowed' : 'pointer',
            opacity: oauthLoading ? 0.6 : 1,
            display: 'flex',
            alignItems: 'center',
            gap: 6,
          }}
        >
          {oauthLoading ? (
            <>
              <Spinner size={12} />
              Waiting for browser…
            </>
          ) : (
            OAUTH_BUTTON_LABELS[provider.id] || `Sign in with ${getProviderDisplayName(provider.id)}`
          )}
        </button>
      )}

      {/* GitHub device code display */}
      {deviceCode && (
        <div
          style={{
            padding: '10px 14px',
            background: colors.surfacePrimary,
            border: `1px solid ${colors.containerBorder}`,
            borderRadius: 8,
            fontSize: 12,
          }}
        >
          <div style={{ marginBottom: 6, color: colors.textSecondary }}>
            Enter this code on GitHub:
          </div>
          <div
            style={{
              fontFamily: 'monospace',
              fontSize: 20,
              fontWeight: 700,
              letterSpacing: 2,
              color: colors.textPrimary,
              userSelect: 'all',
              marginBottom: 6,
            }}
          >
            {deviceCode.userCode}
          </div>
          <div style={{ fontSize: 11, color: colors.textTertiary, display: 'flex', alignItems: 'center', gap: 6 }}>
            <Spinner size={10} />
            Waiting for authorization…
          </div>
        </div>
      )}

      {/* API key input for non-OAuth providers */}
      {isApiKeyProvider && !provider.hasAuth && (
        <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
          <input
            type="password"
            placeholder={`${getProviderDisplayName(provider.id)} API key`}
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSave()}
            style={{
              flex: 1,
              padding: '6px 10px',
              background: colors.surfacePrimary,
              color: colors.textPrimary,
              border: `1px solid ${colors.containerBorder}`,
              borderRadius: 6,
              fontSize: 12,
              outline: 'none',
            }}
          />
          <button
            onClick={handleSave}
            disabled={saving || !apiKey.trim()}
            style={{
              padding: '6px 12px',
              background: colors.accent,
              color: '#fff',
              border: 'none',
              borderRadius: 6,
              fontSize: 12,
              fontWeight: 600,
              cursor: saving || !apiKey.trim() ? 'not-allowed' : 'pointer',
              opacity: saving || !apiKey.trim() ? 0.5 : 1,
            }}
          >
            {saving ? '…' : saved ? '✓' : 'Save'}
          </button>
        </div>
      )}

      {error && (
        <div style={{ marginTop: 4, fontSize: 11, color: '#ef4444' }}>{error}</div>
      )}
    </SettingSection>
  )
}

/** Tiny CSS spinner. */
function Spinner({ size = 14 }: { size?: number }) {
  return (
    <span
      style={{
        display: 'inline-block',
        width: size,
        height: size,
        border: '2px solid rgba(255,255,255,0.3)',
        borderTopColor: '#fff',
        borderRadius: '50%',
        animation: 'ion-spin 0.6s linear infinite',
        flexShrink: 0,
      }}
    />
  )
}
