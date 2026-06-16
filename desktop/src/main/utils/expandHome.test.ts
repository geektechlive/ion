import { homedir } from 'node:os'
import { describe, it, expect } from 'vitest'
import { expandHome } from './expandHome'

const HOME = homedir()

describe('expandHome', () => {
  it('expands ~/x to <home>/x', () => {
    expect(expandHome('~/foo')).toBe(`${HOME}/foo`)
  })

  it('expands ~/ with nested path', () => {
    expect(expandHome('~/Library/Mobile Documents')).toBe(`${HOME}/Library/Mobile Documents`)
  })

  it('expands bare ~ to home directory', () => {
    expect(expandHome('~')).toBe(HOME)
  })

  it('leaves absolute paths unchanged', () => {
    expect(expandHome('/usr/local/bin')).toBe('/usr/local/bin')
  })

  it('leaves relative paths unchanged', () => {
    expect(expandHome('foo/bar')).toBe('foo/bar')
  })

  it('leaves empty string unchanged', () => {
    expect(expandHome('')).toBe('')
  })
})
