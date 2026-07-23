import { describe, expect, it } from 'vitest'
import {
  ringRadii, footAnchorCanvas, ringEllipse, shadowEllipse,
  hitTestGizmo, applyGizmoDrag, type GizmoContext,
} from './spriteGizmos'
import type { UnitBounds } from '@/game/maps/unitDefs'

// A deliberately clean geometry: a 20x20 sprite blitted at 4x, centred in an
// 100x100-ish box. spriteBottom = dy + h = 100; feet anchor sits bounds.bottom*4
// above it.
const geo = { dx: 20, dy: 20, w: 80, h: 80, scale: 4 }
const baseBounds: UnitBounds = { halfWidth: 14, top: -20, bottom: 2 }
function ctx(overrides: Partial<GizmoContext> = {}): GizmoContext {
  return { geo, bounds: baseBounds, ringLift: 5, flyer: false, ...overrides }
}

describe('footAnchorCanvas', () => {
  it('places the feet anchor bounds.bottom*scale above the sprite bottom', () => {
    // spriteBottom = 100, bottom=2, scale=4 → y = 100 - 8 = 92; x = centre = 60.
    expect(footAnchorCanvas(ctx())).toEqual({ x: 60, y: 92 })
  })
})

describe('applyGizmoDrag foot → bounds.bottom', () => {
  it('solves bounds.bottom so the anchor lands on the drag point', () => {
    expect(applyGizmoDrag('foot', { x: 60, y: 100 }, ctx()).bounds).toEqual({ bottom: 0 })
    expect(applyGizmoDrag('foot', { x: 60, y: 84 }, ctx()).bounds).toEqual({ bottom: 4 })
    expect(applyGizmoDrag('foot', { x: 60, y: 108 }, ctx()).bounds).toEqual({ bottom: -2 })
  })
})

describe('selection ring', () => {
  it('derives radii from halfWidth exactly like the renderer', () => {
    expect(ringRadii(14)).toEqual({ rx: 16, ry: 16 * 0.52 })
    // clamps: tiny halfWidth floors rx at 15, ry at 8.
    expect(ringRadii(0)).toEqual({ rx: 15, ry: 8 })
  })

  it('round-trips a dragged centre back to ringOffsetX/Y', () => {
    const c = ctx({ bounds: { ...baseBounds, ringOffsetX: 3, ringOffsetY: -2 } })
    const e = ringEllipse(c)
    // Dragging the centre handle to where it already is must recover the offsets.
    expect(applyGizmoDrag('ring', { x: e.cx, y: e.cy }, c).bounds).toEqual({ ringOffsetX: 3, ringOffsetY: -2 })
  })

  it('maps the right-edge handle back to halfWidth', () => {
    const e = ringEllipse(ctx())
    expect(applyGizmoDrag('ring-edge', { x: e.cx + e.rx, y: e.cy }, ctx()).bounds).toEqual({ halfWidth: 14 })
    // Drag the edge out to canvas rx=80 (1x rx=20) → halfWidth = 20 - 2 = 18.
    expect(applyGizmoDrag('ring-edge', { x: e.cx + 80, y: e.cy }, ctx()).bounds).toEqual({ halfWidth: 18 })
  })
})

describe('ground shadow', () => {
  it('round-trips a dragged centre back to shadow.offsetX/Y', () => {
    const c = ctx({ shadow: { offsetX: 2, offsetY: -1 } })
    const e = shadowEllipse(c)!
    expect(e).not.toBeNull()
    expect(applyGizmoDrag('shadow', { x: e.cx, y: e.cy }, c).shadow).toEqual({ offsetX: 2, offsetY: -1 })
  })

  it('maps the right-edge handle back to radiusX', () => {
    // default radiusX = halfWidth*0.85 = 11.9; edge at cx + 11.9*4.
    const c = ctx()
    const e = shadowEllipse(c)!
    expect(applyGizmoDrag('shadow-edge', { x: e.cx + e.rx, y: e.cy }, c).shadow).toEqual({ radiusX: 12 })
  })

  it('is null (nothing to draw or edit) when the shadow is disabled', () => {
    expect(shadowEllipse(ctx({ shadow: { enabled: false } }))).toBeNull()
    expect(applyGizmoDrag('shadow', { x: 0, y: 0 }, ctx({ shadow: { enabled: false } }))).toEqual({})
  })
})

describe('hitTestGizmo', () => {
  const opts = { ring: true, shadow: true, foot: true }
  it('detects each handle at its own centre', () => {
    const c = ctx()
    const ring = ringEllipse(c)
    const foot = footAnchorCanvas(c)
    expect(hitTestGizmo({ x: ring.cx, y: ring.cy }, c, opts)).toBe('ring')
    expect(hitTestGizmo({ x: ring.cx + ring.rx, y: ring.cy }, c, opts)).toBe('ring-edge')
    expect(hitTestGizmo({ x: foot.x, y: foot.y }, c, opts)).toBe('foot')
  })

  it('returns null far from every handle', () => {
    expect(hitTestGizmo({ x: 5, y: 5 }, ctx(), opts)).toBeNull()
  })

  it('ignores layers that are toggled off', () => {
    const c = ctx()
    const foot = footAnchorCanvas(c)
    expect(hitTestGizmo({ x: foot.x, y: foot.y }, c, { ring: true, shadow: true, foot: false })).toBeNull()
  })
})
