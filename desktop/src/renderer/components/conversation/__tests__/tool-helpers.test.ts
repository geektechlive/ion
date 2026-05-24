import { describe, it, expect } from 'vitest'
import { stripCdPrefix, getToolDescription } from '../tool-helpers'

describe('stripCdPrefix', () => {
  it('strips an absolute-path cd && prefix', () => {
    expect(stripCdPrefix('cd /Users/josh/src && grep -r foo')).toBe('grep -r foo')
  })

  it('strips a double-quoted path with spaces', () => {
    expect(stripCdPrefix('cd "/Users/josh/path with spaces" && ls')).toBe('ls')
  })

  it('strips a single-quoted path', () => {
    expect(stripCdPrefix("cd '/Users/josh/quoted path' && ls")).toBe('ls')
  })

  it('strips a tilde-relative path', () => {
    expect(stripCdPrefix('cd ~/foo && pwd')).toBe('pwd')
  })

  it('strips a cd ... ; cmd form', () => {
    expect(stripCdPrefix('cd /tmp; echo done')).toBe('echo done')
  })

  it('tolerates extra whitespace around the operator', () => {
    expect(stripCdPrefix('cd /tmp   &&   echo go')).toBe('echo go')
  })

  it('tolerates leading whitespace before cd', () => {
    expect(stripCdPrefix('  cd /tmp && ls')).toBe('ls')
  })

  it('does not touch a command without a leading cd', () => {
    expect(stripCdPrefix('grep -r foo')).toBe('grep -r foo')
  })

  it('does not strip a cd that is not at the start of the command', () => {
    // `echo` is the leading command, so we leave the inner `cd` alone.
    expect(stripCdPrefix('echo cd foo && bar')).toBe('echo cd foo && bar')
  })

  it('only strips the first leading cd hop', () => {
    // Chained cds: we strip exactly one and leave the rest intact so the
    // display still reflects the remaining navigation.
    expect(stripCdPrefix('cd /a && cd /b && cmd')).toBe('cd /b && cmd')
  })

  it('returns an empty string unchanged', () => {
    expect(stripCdPrefix('')).toBe('')
  })
})

describe('getToolDescription Bash', () => {
  it('hides the cd prefix in the JSON-parsed path', () => {
    const input = JSON.stringify({ command: 'cd /Users/josh/src && grep -r foo' })
    expect(getToolDescription('Bash', input)).toBe('grep -r foo')
  })

  it('hides the cd prefix in the streaming-partial path', () => {
    // Trailing brace is missing so JSON.parse throws and we fall into the
    // regex-extraction branch.
    const partial = '{"command": "cd /Users/josh/src && grep -r foo"'
    expect(getToolDescription('Bash', partial)).toBe('grep -r foo')
  })

  it('still truncates to 60 chars after stripping the cd prefix', () => {
    const longCmd = 'a'.repeat(80)
    const input = JSON.stringify({ command: `cd /Users/josh && ${longCmd}` })
    const result = getToolDescription('Bash', input)
    // 57 visible chars + '...' = 60-char display budget.
    expect(result.endsWith('...')).toBe(true)
    expect(result.length).toBe(60)
    // The cd prefix must not be present.
    expect(result.startsWith('cd')).toBe(false)
  })

  it('returns "Bash" when the command is empty', () => {
    const input = JSON.stringify({ command: '' })
    expect(getToolDescription('Bash', input)).toBe('Bash')
  })

  it('passes through commands with no cd prefix untouched', () => {
    const input = JSON.stringify({ command: 'ls -la' })
    expect(getToolDescription('Bash', input)).toBe('ls -la')
  })
})

describe('getToolDescription non-Bash sanity', () => {
  it('formats Read with a file path', () => {
    expect(getToolDescription('Read', JSON.stringify({ file_path: '/a/b.ts' }))).toBe('Read /a/b.ts')
  })

  it('returns the tool name when no input is provided', () => {
    expect(getToolDescription('Grep')).toBe('Grep')
  })
})
