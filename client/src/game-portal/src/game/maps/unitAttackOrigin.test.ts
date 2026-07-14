import { beforeEach, describe, expect, it } from 'vitest'
import {
  getResolvedAttackOriginFor, getResolvedUnitAttackOrigin,
  initPathAttackOrigin, initUnitDefs,
} from './unitDefs'

const def = (attackOrigin: any) => ({ type: 't', faction: 'human', attackOrigin }) as any

describe('getResolvedUnitAttackOrigin', () => {
  it('returns null when unauthored (renderer keeps today\'s geometry)', () => {
    expect(getResolvedUnitAttackOrigin(def(undefined), 'east')).toBeNull()
    expect(getResolvedUnitAttackOrigin(def({}), 'east')).toBeNull()
    expect(getResolvedUnitAttackOrigin(null, 'east')).toBeNull()
    expect(getResolvedUnitAttackOrigin(undefined, 'east')).toBeNull()
  })

  it('byFacing wins over default for that facing', () => {
    const d = def({ default: { x: 0, y: -30 }, byFacing: { east: { x: 14, y: -28 } } })
    expect(getResolvedUnitAttackOrigin(d, 'east')).toEqual({ x: 14, y: -28 })
  })

  it('falls back to default for a facing with no override', () => {
    const d = def({ default: { x: 2, y: -30 }, byFacing: { east: { x: 14, y: -28 } } })
    expect(getResolvedUnitAttackOrigin(d, 'north')).toEqual({ x: 2, y: -30 })
  })

  it('uses default when facing is undefined', () => {
    const d = def({ default: { x: 2, y: -30 }, byFacing: { east: { x: 14, y: -28 } } })
    expect(getResolvedUnitAttackOrigin(d, undefined)).toEqual({ x: 2, y: -30 })
  })

  it('returns null when only byFacing exists and the facing has no entry', () => {
    const d = def({ byFacing: { east: { x: 14, y: -28 } } })
    expect(getResolvedUnitAttackOrigin(d, 'north')).toBeNull()
  })

  it('rounds to integer pixels', () => {
    expect(getResolvedUnitAttackOrigin(def({ default: { x: 2.6, y: -30.4 } }), undefined)).toEqual({ x: 3, y: -30 })
  })
})

describe('getResolvedAttackOriginFor (path-first precedence, mirrors getUnitBoundsFor)', () => {
  beforeEach(() => {
    // Reset both module-level maps between tests — they're mutable globals
    // (like PATH_BOUNDS_MAP), so tests must not depend on execution order.
    initPathAttackOrigin([])
    initUnitDefs([])
  })

  it('returns null when neither the path nor the unit def authors an origin', () => {
    initUnitDefs([def(undefined)])
    expect(getResolvedAttackOriginFor({ path: 'marksman', unitType: 't' }, 'east')).toBeNull()
  })

  it('falls back to the unit def origin when the unit is on no path', () => {
    initUnitDefs([def({ default: { x: 5, y: -20 } })])
    expect(getResolvedAttackOriginFor({ path: null, unitType: 't' }, 'east')).toEqual({ x: 5, y: -20 })
  })

  it('falls back to the unit def origin for the "none" sentinel path (mirrors getUnitBoundsFor)', () => {
    initUnitDefs([def({ default: { x: 5, y: -20 } })])
    initPathAttackOrigin([{ path: 'none', attackOrigin: { default: { x: 99, y: 99 } } }])
    expect(getResolvedAttackOriginFor({ path: 'none', unitType: 't' }, 'east')).toEqual({ x: 5, y: -20 })
  })

  it('falls back to the unit def origin when the path authored no attackOrigin at all', () => {
    initUnitDefs([def({ default: { x: 5, y: -20 } })])
    // 'marksman' is simply absent from the path map (path authored bounds
    // only, or nothing) — must NOT be treated as "path present, empty block".
    expect(getResolvedAttackOriginFor({ path: 'marksman', unitType: 't' }, 'east')).toEqual({ x: 5, y: -20 })
  })

  it('path byFacing wins over path default (unit def origin is not consulted at all)', () => {
    initUnitDefs([def({ default: { x: 1, y: 1 } })])
    initPathAttackOrigin([
      { path: 'marksman', attackOrigin: { default: { x: 0, y: -30 }, byFacing: { east: { x: 14, y: -28 } } } },
    ])
    expect(getResolvedAttackOriginFor({ path: 'marksman', unitType: 't' }, 'east')).toEqual({ x: 14, y: -28 })
  })

  it('path present but missing this facing falls back to the PATH default — not the unit def', () => {
    initUnitDefs([def({ default: { x: 1, y: 1 }, byFacing: { north: { x: 99, y: 99 } } })])
    initPathAttackOrigin([
      { path: 'marksman', attackOrigin: { default: { x: 2, y: -30 }, byFacing: { east: { x: 14, y: -28 } } } },
    ])
    expect(getResolvedAttackOriginFor({ path: 'marksman', unitType: 't' }, 'north')).toEqual({ x: 2, y: -30 })
  })

  // The key precedence rule (mirrors getUnitBoundsFor): a path's presence
  // switches the WHOLE source. A path with only byFacing (no default) that
  // misses this facing returns null — it does NOT fall through to the unit
  // def, even though the unit def has a perfectly good origin authored.
  it('a present path with no default and a facing miss returns null rather than falling through to the unit def', () => {
    initUnitDefs([def({ default: { x: 1, y: 1 } })])
    initPathAttackOrigin([{ path: 'marksman', attackOrigin: { byFacing: { east: { x: 14, y: -28 } } } }])
    expect(getResolvedAttackOriginFor({ path: 'marksman', unitType: 't' }, 'north')).toBeNull()
  })

  it('an unknown unit type with no path resolves to null (existing geometric fallback applies)', () => {
    expect(getResolvedAttackOriginFor({ path: null, unitType: 'ghost_unit' }, 'east')).toBeNull()
  })
})
