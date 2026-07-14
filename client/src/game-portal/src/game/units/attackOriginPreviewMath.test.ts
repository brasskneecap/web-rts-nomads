import { describe, expect, it } from 'vitest'
import {
  canvasToOrigin, deriveDefaultOrigin, originToCanvas, resolveFacingOrigin, unitAnchorCanvas,
  type PreviewDrawGeometry,
} from './attackOriginPreviewMath'
import type { UnitBounds } from '@/game/maps/unitDefs'
import type { UnitSpriteSet } from '@/game/rendering/unitSprites'

const bounds: UnitBounds = { halfWidth: 20, top: -60, bottom: 0 }
// A sprite drawn at 5x scale, fit into a 200x200 rect centered with a 10px
// left margin and 5px top margin inside a bigger box (mirrors drawFrame's
// `x = (box - w) / 2` centering math with arbitrary numbers).
const geo: PreviewDrawGeometry = { dx: 10, dy: 5, w: 200, h: 200, scale: 5 }

describe('unitAnchorCanvas', () => {
  it('centers horizontally in the drawn rect and sits bounds.bottom (scaled) above the sprite bottom', () => {
    const anchor = unitAnchorCanvas(geo, bounds)
    expect(anchor.x).toBe(geo.dx + geo.w / 2)
    expect(anchor.y).toBe(geo.dy + geo.h - bounds.bottom * geo.scale)
  })

  it('lifts the anchor up (smaller canvas y) when bounds.bottom is positive', () => {
    const liftedBounds: UnitBounds = { ...bounds, bottom: 8 }
    const anchor = unitAnchorCanvas(geo, liftedBounds)
    const flatAnchor = unitAnchorCanvas(geo, bounds)
    expect(anchor.y).toBeLessThan(flatAnchor.y)
  })
})

describe('originToCanvas / canvasToOrigin round-trip', () => {
  it('maps an authored offset to canvas and back to the exact same offset', () => {
    const anchor = unitAnchorCanvas(geo, bounds)
    const cases: Array<{ x: number; y: number }> = [
      { x: 0, y: 0 }, { x: 12, y: -34 }, { x: -20, y: 40 }, { x: 3, y: -1 },
    ]
    for (const origin of cases) {
      const canvasPt = originToCanvas(origin, anchor, geo.scale)
      const back = canvasToOrigin(canvasPt.x, canvasPt.y, anchor, geo.scale)
      expect(back).toEqual(origin)
    }
  })

  it('rounds a canvas point that lands between pixels to the nearest integer offset', () => {
    const anchor = unitAnchorCanvas(geo, bounds)
    // 2.4 scaled-pixels off from the anchor -> 0.48 authored px -> rounds to 0.
    const back = canvasToOrigin(anchor.x + 2.4, anchor.y, anchor, geo.scale)
    expect(back).toEqual({ x: 0, y: 0 })
  })
})

describe('deriveDefaultOrigin', () => {
  it('seeds an unauthored facing at horizontal center, 30% down the visible sprite', () => {
    const spriteSet = { size: { width: 48, height: 68 } } as UnitSpriteSet
    const point = deriveDefaultOrigin(spriteSet, bounds)
    expect(point.x).toBe(0)
    const h = 68 * 1.25
    const visibleBottom = bounds.bottom - h * 0.15
    const visibleTop = bounds.bottom - h * (1 - 0.15)
    expect(point.y).toBeCloseTo(visibleTop + (visibleBottom - visibleTop) * 0.3)
  })
})

describe('resolveFacingOrigin', () => {
  it('returns null when nothing is authored', () => {
    expect(resolveFacingOrigin(undefined, 'east')).toBeNull()
    expect(resolveFacingOrigin({}, 'east')).toBeNull()
  })

  it('falls back to default when no per-facing override exists', () => {
    expect(resolveFacingOrigin({ default: { x: 1, y: 2 } }, 'east')).toEqual({ x: 1, y: 2 })
  })

  it('prefers byFacing over default for the matching facing', () => {
    const ao = { default: { x: 1, y: 2 }, byFacing: { east: { x: 9, y: -9 } } }
    expect(resolveFacingOrigin(ao, 'east')).toEqual({ x: 9, y: -9 })
    expect(resolveFacingOrigin(ao, 'north')).toEqual({ x: 1, y: 2 })
  })
})
