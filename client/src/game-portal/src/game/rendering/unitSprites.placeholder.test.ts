import { describe, expect, it } from 'vitest'
import { DEFAULT_UNIT_BOUNDS, getUnitBounds } from '../maps/unitDefs'
import type { UnitDef } from '../maps/unitDefs'

// Verifies the REAL render/hit-test path — getUnitBodyRect (unitSprites.ts)
// → getUnitBoundsFor/getUnitBounds (unitDefs.ts) — already handles a unit
// def with no `bounds` at all, e.g. a brand-new unit type created blank in
// the unit editor before any art/bounds have been assigned. getUnitBounds
// falls back to DEFAULT_UNIT_BOUNDS, a fixed positive-sized box, so the
// artless unit still renders/hit-tests with real (non-zero, non-NaN)
// dimensions instead of crashing or collapsing to a point.
describe('unit bounds fallback for artless units', () => {
  it('falls back to a positive-sized box when the def has no bounds', () => {
    const def = { type: 'brand_new_unit' } as UnitDef
    const bounds = getUnitBounds(def)

    expect(bounds.halfWidth).toBeGreaterThan(0)
    expect(bounds.bottom - bounds.top).toBeGreaterThan(0)
  })

  it('defaults to the fixed DEFAULT_UNIT_BOUNDS constant when bounds is absent', () => {
    const def = { type: 'brand_new_unit' } as UnitDef
    expect(getUnitBounds(def)).toEqual(DEFAULT_UNIT_BOUNDS)
  })

  it('returns a positive-sized box for a null/undefined def', () => {
    expect(getUnitBounds(undefined).halfWidth).toBeGreaterThan(0)
    expect(getUnitBounds(undefined).bottom - getUnitBounds(undefined).top).toBeGreaterThan(0)
    expect(getUnitBounds(null).halfWidth).toBeGreaterThan(0)
    expect(getUnitBounds(null).bottom - getUnitBounds(null).top).toBeGreaterThan(0)
  })

  it('does not use the fixed fallback when the def HAS bounds — uses the def bounds instead', () => {
    const def = {
      type: 'has_bounds_unit',
      bounds: { halfWidth: 20, top: -40, bottom: 5 },
    } as UnitDef
    const bounds = getUnitBounds(def)

    expect(bounds).toEqual({ halfWidth: 20, top: -40, bottom: 5 })
    expect(bounds).not.toEqual(DEFAULT_UNIT_BOUNDS)
  })
})
