// TypeScript mirror of the Go structured validation issues returned by
// POST /abilities/validate (server/internal/game — Phase 5a T1-T4). The
// server is the validator; the client only renders / filters its output.

export type Severity = 'error' | 'warning'

// ValidationIssue is a single validation finding, addressed at a specific
// path into the AbilityDef/AbilityProgram (e.g. "triggers[0].actions[1]",
// "identity.id").
export interface ValidationIssue {
  path: string
  code: string
  message: string
  severity: Severity
}

// issuesForPath returns the issues whose path is an EXACT match for `path`.
// This is intentionally exact (not a prefix match) — callers that need
// "this path or anything nested under it" should filter separately; a
// prefix variant can be added later if the editor UI needs one.
export function issuesForPath(issues: ValidationIssue[], path: string): ValidationIssue[] {
  return issues.filter((i) => i.path === path)
}

// hasBlockingError reports whether any issue in the list is severity
// 'error' (as opposed to a non-blocking 'warning').
export function hasBlockingError(issues: ValidationIssue[]): boolean {
  return issues.some((i) => i.severity === 'error')
}
