import { describe, expect, it } from 'vitest'
import { PLACEHOLDER_BOUNDS, resolvePlaceholderBounds } from './unitSprites'
import type { UnitDef } from '../maps/unitDefs'

describe('unit placeholder render for artless units', () => {
  it('returns a positive-sized box when the def has no bounds', () => {
    const box = resolvePlaceholderBounds({ type: 'brand_new_unit' } as UnitDef)
    expect(box.w).toBeGreaterThan(0)
    expect(box.h).toBeGreaterThan(0)
  })

  it('defaults to the fixed PLACEHOLDER_BOUNDS constant when bounds is absent', () => {
    const box = resolvePlaceholderBounds({ type: 'brand_new_unit' } as UnitDef)
    expect(box).toEqual(PLACEHOLDER_BOUNDS)
  })

  it('returns a positive-sized box for a null/undefined def', () => {
    expect(resolvePlaceholderBounds(undefined).w).toBeGreaterThan(0)
    expect(resolvePlaceholderBounds(undefined).h).toBeGreaterThan(0)
    expect(resolvePlaceholderBounds(null).w).toBeGreaterThan(0)
    expect(resolvePlaceholderBounds(null).h).toBeGreaterThan(0)
  })

  it('does not use the fixed placeholder when the def HAS bounds — derives the box from them instead', () => {
    const def = {
      type: 'has_bounds_unit',
      bounds: { halfWidth: 20, top: -40, bottom: 5 },
    } as UnitDef
    const box = resolvePlaceholderBounds(def)
    expect(box).toEqual({ w: 40, h: 45 })
    expect(box).not.toEqual(PLACEHOLDER_BOUNDS)
  })
})
