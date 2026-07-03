import { describe, it, expect } from 'vitest'
import { formatErrorEntry } from './errorReporting'

describe('formatErrorEntry', () => {
  it('captures an Error message and stack under the error level', () => {
    const err = new Error('boom')
    const entry = formatErrorEntry('window.error', err)
    expect(entry.level).toBe('error')
    expect(entry.message).toContain('window.error')
    expect(entry.message).toContain('boom')
    expect(entry.context?.stack).toBe(err.stack)
  })

  it('handles a thrown string', () => {
    const entry = formatErrorEntry('unhandledrejection', 'nope')
    expect(entry.level).toBe('error')
    expect(entry.message).toContain('unhandledrejection')
    expect(entry.message).toContain('nope')
  })

  it('handles a non-Error object without throwing', () => {
    const entry = formatErrorEntry('vue.errorHandler', { code: 42 })
    expect(entry.level).toBe('error')
    expect(entry.message).toContain('vue.errorHandler')
    expect(entry.message).toContain('42')
  })

  it('handles a value that cannot be JSON-stringified', () => {
    const circular: Record<string, unknown> = {}
    circular.self = circular
    const entry = formatErrorEntry('window.error', circular)
    expect(entry.level).toBe('error')
    expect(typeof entry.message).toBe('string')
    expect(entry.message).toContain('window.error')
  })
})
