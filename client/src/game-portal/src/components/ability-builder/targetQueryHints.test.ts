import { describe, expect, it } from 'vitest'
import {
  targetQueryFieldHint,
  targetQueryFieldLabel,
  targetQueryOptionHint,
  targetQueryOptionLabel,
} from './targetQueryHints'

describe('targetQueryHints', () => {
  it('returns copy for every known TargetQueryDef field', () => {
    expect(targetQueryFieldHint('source')).toMatch(/Which units to start from/)
    expect(targetQueryFieldHint('origin')).toMatch(/Search Radius measures from/)
    expect(targetQueryFieldHint('originRef')).toMatch(/Saved Selection/)
    expect(targetQueryFieldHint('relations')).toMatch(/relationship to the caster/)
    expect(targetQueryFieldHint('radius')).toMatch(/distance filter/)
    expect(targetQueryFieldHint('ordering')).toMatch(/Sort whoever survived/)
    expect(targetQueryFieldHint('maxCount')).toMatch(/Keep only the first N/)
    expect(targetQueryFieldHint('includeInitialTarget')).toMatch(/Force the clicked unit/)
    expect(targetQueryFieldHint('excludeSource')).toMatch(/Drop the caster/)
    expect(targetQueryFieldHint('aliveState')).toMatch(/how Raise Skeleton works/)
  })

  it('maps every field key to a human-readable label', () => {
    expect(targetQueryFieldLabel('source')).toBe('Start With')
    expect(targetQueryFieldLabel('origin')).toBe('Search Around')
    expect(targetQueryFieldLabel('originRef')).toBe('Saved Value')
    expect(targetQueryFieldLabel('relations')).toBe('Relationship to Caster')
    expect(targetQueryFieldLabel('radius')).toBe('Search Radius')
    expect(targetQueryFieldLabel('ordering')).toBe('Prioritize By')
    expect(targetQueryFieldLabel('maxCount')).toBe('Maximum Targets')
    expect(targetQueryFieldLabel('aliveState')).toBe('Unit State')
  })

  it('maps enum wire values to human-readable labels', () => {
    expect(targetQueryOptionLabel('source', 'current_event')).toBe('Triggering Unit')
    expect(targetQueryOptionLabel('source', 'all_in_scene')).toBe('All Units in Scene')
    expect(targetQueryOptionLabel('origin', 'impact_position')).toBe('Projectile Impact Point')
    expect(targetQueryOptionLabel('ordering', 'unit_id')).toBe('Stable Unit Order')
    // relations 'self' reads as 'Caster', never 'Self'.
    expect(targetQueryOptionLabel('relations', 'self')).toBe('Caster')
  })

  // Graceful fallback: an unmapped label (unknown field or a brand-new enum
  // value the server started sending) degrades to the raw wire value, never
  // throws, and never borrows another option's label.
  it('falls back to the raw wire value for unmapped labels', () => {
    expect(targetQueryFieldLabel('somethingBrandNew')).toBe('somethingBrandNew')
    expect(targetQueryOptionLabel('source', 'some_future_source')).toBe('some_future_source')
    expect(targetQueryOptionLabel('unknownField', 'caster')).toBe('caster')
  })

  // Graceful fallback: an unknown field (a future TargetQueryDef addition the
  // client hasn't shipped copy for) must degrade to silence, not throw and
  // not fall back to some other field's text.
  it('returns empty string for an unknown field, without throwing', () => {
    expect(() => targetQueryFieldHint('somethingBrandNew')).not.toThrow()
    expect(targetQueryFieldHint('somethingBrandNew')).toBe('')
    expect(targetQueryFieldHint('')).toBe('')
  })

  it('flags the inert options that would otherwise look like normal choices', () => {
    expect(targetQueryOptionHint('source', 'source_object')).toMatch(/unavailable/)
    expect(targetQueryOptionHint('origin', 'projectile_position')).toMatch(/unavailable/)
    expect(targetQueryOptionHint('relations', 'neutral')).toMatch(/unavailable/)
    expect(targetQueryOptionHint('ordering', 'random')).toMatch(/seeded RNG/)
  })

  it('cross-references Saved Value for the two "named" options', () => {
    expect(targetQueryOptionHint('source', 'named_context')).toMatch(/Saved Value/)
    expect(targetQueryOptionHint('origin', 'named_context_value')).toMatch(/Saved Value/)
  })

  // Graceful fallback: a brand-new enum value the server starts sending
  // (e.g. a new TargetSource) must not throw and must render with no note,
  // never a stale/incorrect one.
  it('returns empty string for an unknown option value, without throwing', () => {
    expect(() => targetQueryOptionHint('source', 'some_future_source')).not.toThrow()
    expect(targetQueryOptionHint('source', 'some_future_source')).toBe('')
    expect(targetQueryOptionHint('somethingBrandNew', 'caster')).toBe('')
  })
})
