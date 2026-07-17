import { describe, it, expect } from 'vitest'
import { issuesForPath, hasBlockingError, type ValidationIssue } from './programValidation'

const issues: ValidationIssue[] = [
  { path: 'triggers[0].actions[1]', code: 'empty_required_property', message: 'x', severity: 'error' },
  { path: 'identity.id', code: 'invalid_id', message: 'y', severity: 'error' },
  { path: 'triggers[0]', code: 'no_behavior', message: 'z', severity: 'warning' },
]
describe('programValidation', () => {
  it('filters issues by path prefix', () => {
    expect(issuesForPath(issues, 'triggers[0].actions[1]')).toHaveLength(1)
    expect(issuesForPath(issues, 'identity.id')[0].code).toBe('invalid_id')
  })
  it('detects blocking errors', () => {
    expect(hasBlockingError(issues)).toBe(true)
    expect(hasBlockingError(issues.filter(i => i.severity === 'warning'))).toBe(false)
  })
})
