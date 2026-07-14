import { describe, expect, it } from 'vitest'
import { resolveProjectileOriginLift } from './attackOriginResolve'

describe('resolveProjectileOriginLift', () => {
  it('returns the authored lift when present, ignoring the fallback', () => {
    const authored = { x: 12, y: -34 }
    const fallback = { x: 0, y: -18 }
    expect(resolveProjectileOriginLift(authored, fallback)).toEqual(authored)
  })

  it('returns the geometric fallback unchanged when unauthored (null)', () => {
    const fallback = { x: 0, y: -18 }
    expect(resolveProjectileOriginLift(null, fallback)).toBe(fallback)
  })
})
