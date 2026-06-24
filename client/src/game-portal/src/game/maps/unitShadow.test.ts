import { describe, it, expect } from 'vitest'
import { resolveUnitShadow, FLYER_SHADOW_DROP } from './unitShadow'
import type { UnitBounds } from './unitDefs'

const BOUNDS: UnitBounds = { halfWidth: 20, top: -60, bottom: 4 }

describe('resolveUnitShadow', () => {
  it('derives defaults from bounds when no config is given', () => {
    const s = resolveUnitShadow(undefined, BOUNDS, false)
    expect(s).not.toBeNull()
    // radiusX = halfWidth * 0.85, radiusY = radiusX * 0.4
    expect(s!.radiusX).toBeCloseTo(20 * 0.85)
    expect(s!.radiusY).toBeCloseTo(20 * 0.85 * 0.4)
    expect(s!.opacity).toBeCloseTo(0.5)
    expect(s!.offsetX).toBe(0)
    expect(s!.offsetY).toBe(0)
  })

  it('returns null when explicitly disabled', () => {
    expect(resolveUnitShadow({ enabled: false }, BOUNDS, false)).toBeNull()
    // Disabled wins even for a flyer.
    expect(resolveUnitShadow({ enabled: false }, BOUNDS, true)).toBeNull()
  })

  it('honors explicit field overrides', () => {
    const s = resolveUnitShadow(
      { radiusX: 30, radiusY: 9, opacity: 0.5, offsetX: 2, offsetY: -3 },
      BOUNDS,
      false,
    )
    expect(s).toEqual({ radiusX: 30, radiusY: 9, opacity: 0.5, offsetX: 2, offsetY: -3 })
  })

  it('clamps opacity into [0, 1]', () => {
    expect(resolveUnitShadow({ opacity: 5 }, BOUNDS, false)!.opacity).toBe(1)
    expect(resolveUnitShadow({ opacity: -1 }, BOUNDS, false)!.opacity).toBe(0)
  })

  it('applies flyer adjustment to derived values', () => {
    const ground = resolveUnitShadow(undefined, BOUNDS, false)!
    const flyer = resolveUnitShadow(undefined, BOUNDS, true)!
    expect(flyer.offsetY).toBeCloseTo(ground.offsetY + FLYER_SHADOW_DROP)
    expect(flyer.radiusX).toBeCloseTo(ground.radiusX * 1.15)
    expect(flyer.radiusY).toBeCloseTo(ground.radiusY * 1.15)
    expect(flyer.opacity).toBeCloseTo(ground.opacity * 0.6)
  })

  it('lets explicit per-file values win over the flyer auto-adjustment', () => {
    // Explicit offsetY: 0 must stay 0 (no auto-drop), and explicit radius/opacity
    // must not be scaled by the flyer multipliers.
    const s = resolveUnitShadow({ offsetY: 0, radiusX: 25, opacity: 0.4 }, BOUNDS, true)!
    expect(s.offsetY).toBe(0)
    expect(s.radiusX).toBe(25)
    expect(s.opacity).toBeCloseTo(0.4)
    // radiusY was NOT explicitly set, so it derives from the explicit radiusX
    // and still receives the flyer scale.
    expect(s.radiusY).toBeCloseTo(25 * 0.4 * 1.15)
  })
})
