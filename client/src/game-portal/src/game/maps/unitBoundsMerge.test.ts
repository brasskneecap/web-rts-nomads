import { describe, expect, it } from 'vitest'
import {
  DEFAULT_UNIT_BOUNDS, getUnitBounds, getUnitBoundsFor,
  initPathBounds, initUnitDefs,
  type UnitBounds, type UnitDef,
} from './unitDefs'

// A partial bounds block — exactly what the anchors overlay emits when the
// author drags ONLY the selection ring (ringOffsetX/Y), leaving halfWidth/top/
// bottom unauthored. Consumers must merge it over the defaults/base, never
// replace, or the sprite anchors at NaN and the unit renders invisibly.
const partial = { ringOffsetX: -1, ringOffsetY: 3 } as Partial<UnitBounds> as UnitBounds

describe('getUnitBounds merges partial bounds over defaults', () => {
  it('fills missing halfWidth/top/bottom from DEFAULT_UNIT_BOUNDS', () => {
    const def = { type: 'x', bounds: partial } as unknown as UnitDef
    expect(getUnitBounds(def)).toEqual({
      halfWidth: DEFAULT_UNIT_BOUNDS.halfWidth,
      top: DEFAULT_UNIT_BOUNDS.top,
      bottom: DEFAULT_UNIT_BOUNDS.bottom,
      ringOffsetX: -1,
      ringOffsetY: 3,
    })
  })

  it('a complete bounds fully overrides the defaults', () => {
    const full: UnitBounds = { halfWidth: 20, top: -72, bottom: 31 }
    const def = { type: 'x', bounds: full } as unknown as UnitDef
    expect(getUnitBounds(def)).toEqual(full)
  })

  it('falls back to defaults when no bounds authored', () => {
    expect(getUnitBounds({ type: 'x' } as unknown as UnitDef)).toEqual(DEFAULT_UNIT_BOUNDS)
  })
})

describe('getUnitBoundsFor merges a partial PATH bounds over the base unit', () => {
  it('inherits halfWidth/top/bottom from the base unit, applies the path ring tweak', () => {
    initUnitDefs([{ type: 'acolyte', bounds: { halfWidth: 20, top: -72, bottom: 31 } } as unknown as UnitDef])
    initPathBounds([{ path: 'siphoner', bounds: partial }])
    // The regression: the Siphoner path authored only ring offsets; without the
    // merge, halfWidth/bottom come back undefined and the sprite vanishes.
    expect(getUnitBoundsFor({ path: 'siphoner', unitType: 'acolyte' })).toEqual({
      halfWidth: 20,
      top: -72,
      bottom: 31,
      ringOffsetX: -1,
      ringOffsetY: 3,
    })
  })
})
