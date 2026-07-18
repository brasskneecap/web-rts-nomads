import { describe, expect, it } from 'vitest'
import { targetQueryFieldHint, targetQueryOptionHint } from './targetQueryHints'

describe('targetQueryHints', () => {
  it('returns copy for every known TargetQueryDef field', () => {
    expect(targetQueryFieldHint('source')).toMatch(/Which units to start from/)
    expect(targetQueryFieldHint('origin')).toMatch(/Radius measures from/)
    expect(targetQueryFieldHint('originRef')).toMatch(/named context/)
    expect(targetQueryFieldHint('relations')).toMatch(/relationship to the caster/)
    expect(targetQueryFieldHint('radius')).toMatch(/distance filter/)
    expect(targetQueryFieldHint('ordering')).toMatch(/Sort whoever survived/)
    expect(targetQueryFieldHint('maxCount')).toMatch(/Keep only the first N/)
    expect(targetQueryFieldHint('includeInitialTarget')).toMatch(/Force the clicked unit/)
    expect(targetQueryFieldHint('excludeSource')).toMatch(/Drop the caster/)
    expect(targetQueryFieldHint('aliveState')).toMatch(/how Raise Skeleton works/)
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
    expect(targetQueryOptionHint('source', 'source_object')).toMatch(/not implemented/)
    expect(targetQueryOptionHint('origin', 'projectile_position')).toMatch(/not implemented/)
    expect(targetQueryOptionHint('relations', 'neutral')).toMatch(/not implemented/)
    expect(targetQueryOptionHint('ordering', 'random')).toMatch(/seeded RNG/)
  })

  it('cross-references Origin Ref for the two "named" options', () => {
    expect(targetQueryOptionHint('source', 'named_context')).toMatch(/Origin Ref/)
    expect(targetQueryOptionHint('origin', 'named_context_value')).toMatch(/Origin Ref/)
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
