import { describe, expect, it } from 'vitest'
import { parseAnimationRef, formatAnimationRef, resolveAnimationFrames, oneShotDecalFrame } from './animationRef'
import { getObjectSpriteSet } from './objectSprites'

describe('animationRef scheme', () => {
  it('parses effect / projectile / beam refs', () => {
    expect(parseAnimationRef('effect:explosion')).toEqual({ source: 'effect', ref: 'explosion' })
    expect(parseAnimationRef('projectile:frost_bolt')).toEqual({ source: 'projectile', ref: 'frost_bolt' })
    expect(parseAnimationRef('beam:siphon_life')).toEqual({ source: 'beam', ref: 'siphon_life' })
  })

  it('parses an object ref with and without an animation state', () => {
    expect(parseAnimationRef('object:marker_trap')).toEqual({ source: 'object', ref: 'marker_trap' })
    expect(parseAnimationRef('object:caltrops@electrified')).toEqual({
      source: 'object',
      ref: 'caltrops',
      state: 'electrified',
    })
  })

  it('parses an uploaded-image ref (Upload tab, single static frame)', () => {
    expect(parseAnimationRef('image:my_ability')).toEqual({ source: 'image', ref: 'my_ability' })
    expect(formatAnimationRef('image', 'my_ability')).toBe('image:my_ability')
  })

  it('returns null for empty / unknown-scheme strings', () => {
    expect(parseAnimationRef('')).toBeNull()
    expect(parseAnimationRef(undefined)).toBeNull()
    expect(parseAnimationRef('bogus:thing')).toBeNull()
  })

  it('formats back to the stored string, omitting a bare/idle object state', () => {
    expect(formatAnimationRef('effect', 'explosion')).toBe('effect:explosion')
    expect(formatAnimationRef('object', 'marker_trap')).toBe('object:marker_trap')
    expect(formatAnimationRef('object', 'caltrops', 'electrified')).toBe('object:caltrops@electrified')
    // 'idle' is the default state — omitted so the common case stays clean.
    expect(formatAnimationRef('object', 'fire_pit', 'idle')).toBe('object:fire_pit')
  })

  it('resolves object frames from the chosen animation state (derived from the manifest, not pinned)', () => {
    const idle = getObjectSpriteSet('fire_pit')?.animations.get('idle')
    expect(idle).toBeTruthy()
    const frames = resolveAnimationFrames('object:fire_pit')
    expect(frames).toBeTruthy()
    expect(frames?.frameCount).toBe(idle!.frameCount)
    expect(frames?.loop).toBe(idle!.loop)
  })

  it('oneShotDecalFrame plays 0..N-1 across the decal lifetime (from the start each spawn)', () => {
    const N = 13
    expect(oneShotDecalFrame(0.8, 0.8, N)).toBe(0) // just spawned (remaining == total)
    expect(oneShotDecalFrame(0.4, 0.8, N)).toBe(6) // halfway
    expect(oneShotDecalFrame(0.0, 0.8, N)).toBe(N - 1) // spent → last frame
    // Clamped and safe for degenerate inputs.
    expect(oneShotDecalFrame(1.0, 0, N)).toBe(N - 1)
    expect(oneShotDecalFrame(-0.1, 0.8, N)).toBe(N - 1)
  })

  it('resolves a non-idle object state when one is named', () => {
    const set = getObjectSpriteSet('caltrops')
    const electrified = set?.animations.get('electrified')
    // caltrops ships an electrified state — resolve it explicitly.
    const frames = resolveAnimationFrames({ source: 'object', ref: 'caltrops', state: 'electrified' })
    expect(frames?.frameCount).toBe(electrified!.frameCount)
  })
})
