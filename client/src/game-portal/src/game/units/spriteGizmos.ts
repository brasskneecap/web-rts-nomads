// Pure draw + hit-test + drag→value math for the sprite-preview anchor/bounds
// overlay (the "Anchors & bounds" toggle in UnitSpritePreview.vue). Kept in ONE
// place — next to attackOriginPreviewMath.ts, whose geometry seam it reuses — so
// the canvas overlay, the drag handlers, and the numeric inputs can never drift
// apart, and so the drag→value inverse is unit-testable without a DOM.
//
// Every gizmo is expressed against the SAME PreviewDrawGeometry the sprite was
// blitted with, and mirrors the authoritative in-game math so what you drag in
// the editor is exactly what the renderer draws in a match:
//   - selection ring: CanvasRenderer.drawUnits' ring geometry (radii from
//     halfWidth, centre lifted by ringLift + the ringOffset nudge).
//   - ground shadow: resolveUnitShadow (unitShadow.ts) + drawGroundShadow's
//     light-direction shift.
import type { UnitBounds, UnitShadow } from '@/game/maps/unitDefs'
import {
  resolveUnitShadow,
  SHADOW_LIGHT_DX, SHADOW_LIGHT_DY, SHADOW_LIGHT_SHIFT,
} from '@/game/maps/unitShadow'
import {
  unitAnchorCanvas, originToCanvas, canvasToOrigin,
  type PreviewDrawGeometry,
} from './attackOriginPreviewMath'

// Which editable handle a pointer is over. 'foot' is the feet anchor (drives
// bounds.bottom); 'ring'/'shadow' are the ellipse centres; '*-edge' are the
// right-edge radius handles.
export type GizmoHandle = 'foot' | 'ring' | 'ring-edge' | 'shadow' | 'shadow-edge'

// Everything a gizmo needs to place itself, resolved by the caller once per
// frame. ringLift is the sprite's transparent-bottom lift in 1x px
// (spriteSet.size.height * UNIT_SPRITE_SCALE * getSpritePaddingFrac(set).bottom)
// — passed in rather than computed here so this module stays free of the sprite
// loader and trivially testable.
export interface GizmoContext {
  geo: PreviewDrawGeometry
  bounds: UnitBounds
  shadow?: UnitShadow
  flyer?: boolean
  ringLift: number
}

// An ellipse in canvas pixels.
export interface Ellipse { cx: number; cy: number; rx: number; ry: number }

// Which layers to draw / allow editing. Render box is view-only (it's the art).
export interface GizmoOptions {
  renderBox?: boolean
  foot?: boolean
  ring?: boolean
  shadow?: boolean
}

// Selection-ring radii from the body half-width — identical to
// CanvasRenderer.drawUnits (max(15, halfWidth+2), squashed to a shallow ellipse).
export function ringRadii(halfWidth: number): { rx: number; ry: number } {
  const rx = Math.max(15, halfWidth + 2)
  const ry = Math.max(8, Math.min(12, rx * 0.52))
  return { rx, ry }
}

// The feet anchor (unit.x, unit.y) in canvas px — the point the sprite stands on.
export function footAnchorCanvas(ctx: GizmoContext): { x: number; y: number } {
  return unitAnchorCanvas(ctx.geo, ctx.bounds)
}

// The 1x offset from the feet anchor to the ring centre, matching
// CanvasRenderer: unit.y + bounds.bottom - ry*0.35 - ringLift + ringOffsetY.
// (bounds.bottom cancels against unitAnchorCanvas' own bounds.bottom term, so
// the ring lands near the sprite's bottom edge regardless of bounds.bottom.)
function ringCentreOffset(ctx: GizmoContext): { x: number; y: number } {
  const { ry } = ringRadii(ctx.bounds.halfWidth)
  return {
    x: ctx.bounds.ringOffsetX ?? 0,
    y: ctx.bounds.bottom - ry * 0.35 - ctx.ringLift + (ctx.bounds.ringOffsetY ?? 0),
  }
}

export function ringEllipse(ctx: GizmoContext): Ellipse {
  const { rx, ry } = ringRadii(ctx.bounds.halfWidth)
  const anchor = unitAnchorCanvas(ctx.geo, ctx.bounds)
  const c = originToCanvas(ringCentreOffset(ctx), anchor, ctx.geo.scale)
  return { cx: c.x, cy: c.y, rx: rx * ctx.geo.scale, ry: ry * ctx.geo.scale }
}

// The resolved shadow (concrete radii/opacity/offset) or null when disabled.
export function resolvedShadow(ctx: GizmoContext) {
  return resolveUnitShadow(ctx.shadow, ctx.bounds, !!ctx.flyer)
}

// The 1x offset from the feet anchor to the shadow centre, matching
// CanvasRenderer.drawUnits' feetX/feetY PLUS drawGroundShadow's light shift.
function shadowCentreOffset(ctx: GizmoContext, s: { radiusX: number; offsetX: number; offsetY: number }): { x: number; y: number } {
  const ringOffsetX = ctx.bounds.ringOffsetX ?? 0
  const ringOffsetY = ctx.bounds.ringOffsetY ?? 0
  const lightShift = s.radiusX * SHADOW_LIGHT_SHIFT
  return {
    x: ringOffsetX + s.offsetX + SHADOW_LIGHT_DX * lightShift,
    y: ctx.bounds.bottom - ctx.ringLift + ringOffsetY + s.offsetY + SHADOW_LIGHT_DY * lightShift,
  }
}

export function shadowEllipse(ctx: GizmoContext): Ellipse | null {
  const s = resolvedShadow(ctx)
  if (!s) return null
  const anchor = unitAnchorCanvas(ctx.geo, ctx.bounds)
  const c = originToCanvas(shadowCentreOffset(ctx, s), anchor, ctx.geo.scale)
  return { cx: c.x, cy: c.y, rx: s.radiusX * ctx.geo.scale, ry: s.radiusY * ctx.geo.scale }
}

// ── Hit-testing ──────────────────────────────────────────────────────────────
const HANDLE_R = 7 // canvas px grab radius for a centre handle
const EDGE_R = 8 // canvas px grab radius for a right-edge radius handle

function near(pt: { x: number; y: number }, x: number, y: number, r: number): boolean {
  return Math.hypot(pt.x - x, pt.y - y) <= r
}

// Returns the editable handle under `pt`, or null. Order = topmost-first so the
// small centre handles win over the larger ellipse edges they sit inside.
export function hitTestGizmo(pt: { x: number; y: number }, ctx: GizmoContext, opts: GizmoOptions): GizmoHandle | null {
  if (opts.ring) {
    const r = ringEllipse(ctx)
    if (near(pt, r.cx, r.cy, HANDLE_R)) return 'ring'
    if (near(pt, r.cx + r.rx, r.cy, EDGE_R)) return 'ring-edge'
  }
  if (opts.shadow) {
    const s = shadowEllipse(ctx)
    if (s) {
      if (near(pt, s.cx, s.cy, HANDLE_R)) return 'shadow'
      if (near(pt, s.cx + s.rx, s.cy, EDGE_R)) return 'shadow-edge'
    }
  }
  if (opts.foot) {
    const f = footAnchorCanvas(ctx)
    if (near(pt, f.x, f.y, HANDLE_R)) return 'foot'
  }
  return null
}

// ── Drag → value ─────────────────────────────────────────────────────────────
// Result is a sparse patch to apply onto the current bounds / shadow. All values
// are rounded to whole px (bounds/shadow are authored as integers).
export interface GizmoDragResult {
  bounds?: Partial<UnitBounds>
  shadow?: Partial<UnitShadow>
}

const round = Math.round

// Maps a drag of the given handle to `pt` (canvas px) into new field values.
export function applyGizmoDrag(handle: GizmoHandle, pt: { x: number; y: number }, ctx: GizmoContext): GizmoDragResult {
  const { geo } = ctx
  const anchor = unitAnchorCanvas(geo, ctx.bounds)

  switch (handle) {
    case 'foot': {
      // The feet anchor sits bounds.bottom*scale above the sprite's bottom edge;
      // solve bounds.bottom so the anchor lands on the drag point.
      const spriteBottom = geo.dy + geo.h
      return { bounds: { bottom: round((spriteBottom - pt.y) / geo.scale) } }
    }
    case 'ring': {
      const off = canvasToOrigin(pt.x, pt.y, anchor, geo.scale)
      const base = ringCentreOffset({ ...ctx, bounds: { ...ctx.bounds, ringOffsetX: 0, ringOffsetY: 0 } })
      return { bounds: { ringOffsetX: round(off.x - base.x), ringOffsetY: round(off.y - base.y) } }
    }
    case 'ring-edge': {
      const r = ringEllipse(ctx)
      const rxCanvas = Math.max(0, pt.x - r.cx)
      // Inverse of rx = max(15, halfWidth + 2): halfWidth = rx - 2.
      return { bounds: { halfWidth: Math.max(0, round(rxCanvas / geo.scale - 2)) } }
    }
    case 'shadow': {
      const s = resolvedShadow(ctx)
      if (!s) return {}
      const off = canvasToOrigin(pt.x, pt.y, anchor, geo.scale)
      // Subtract everything the centre inherits (ringOffset, bottom, ringLift,
      // light shift) to isolate shadow.offsetX/offsetY.
      const base = shadowCentreOffset(ctx, { radiusX: s.radiusX, offsetX: 0, offsetY: 0 })
      return { shadow: { offsetX: round(off.x - base.x), offsetY: round(off.y - base.y) } }
    }
    case 'shadow-edge': {
      const el = shadowEllipse(ctx)
      if (!el) return {}
      const rxCanvas = Math.max(0, pt.x - el.cx)
      return { shadow: { radiusX: Math.max(0, round(rxCanvas / geo.scale)) } }
    }
  }
}

// ── Drawing ──────────────────────────────────────────────────────────────────
const COL_BOX = 'rgba(148, 163, 184, 0.55)'
const COL_RING = '#38bdf8'
const COL_SHADOW = '#a78bfa'
const COL_FOOT = '#f59e0b'

function strokeEllipse(ctx: CanvasRenderingContext2D, e: Ellipse) {
  ctx.beginPath()
  ctx.ellipse(e.cx, e.cy, Math.max(0.5, e.rx), Math.max(0.5, e.ry), 0, 0, Math.PI * 2)
  ctx.stroke()
}

function handleDot(ctx: CanvasRenderingContext2D, x: number, y: number, color: string, active: boolean) {
  ctx.beginPath()
  ctx.arc(x, y, active ? 5 : 3.5, 0, Math.PI * 2)
  ctx.fillStyle = color
  ctx.fill()
}

// Draws the enabled gizmo layers onto the preview canvas. `active` highlights the
// handle currently hovered/dragged. Purely an overlay — never persisted, never
// touches the sprite bitmap.
export function drawGizmos(
  ctx: CanvasRenderingContext2D,
  g: GizmoContext,
  opts: GizmoOptions,
  active: GizmoHandle | null,
) {
  ctx.save()
  ctx.lineWidth = 1.5

  if (opts.renderBox) {
    ctx.strokeStyle = COL_BOX
    ctx.setLineDash([4, 3])
    ctx.strokeRect(g.geo.dx + 0.5, g.geo.dy + 0.5, g.geo.w - 1, g.geo.h - 1)
    ctx.setLineDash([])
  }

  if (opts.shadow) {
    const s = shadowEllipse(g)
    if (s) {
      ctx.strokeStyle = COL_SHADOW
      strokeEllipse(ctx, s)
      handleDot(ctx, s.cx, s.cy, COL_SHADOW, active === 'shadow')
      handleDot(ctx, s.cx + s.rx, s.cy, COL_SHADOW, active === 'shadow-edge')
    }
  }

  if (opts.ring) {
    const r = ringEllipse(g)
    ctx.strokeStyle = COL_RING
    strokeEllipse(ctx, r)
    handleDot(ctx, r.cx, r.cy, COL_RING, active === 'ring')
    handleDot(ctx, r.cx + r.rx, r.cy, COL_RING, active === 'ring-edge')
  }

  if (opts.foot) {
    const f = footAnchorCanvas(g)
    ctx.strokeStyle = COL_FOOT
    // cross-hair + dot so the exact ground point is unambiguous
    ctx.beginPath()
    ctx.moveTo(f.x - 8, f.y)
    ctx.lineTo(f.x + 8, f.y)
    ctx.moveTo(f.x, f.y - 8)
    ctx.lineTo(f.x, f.y + 8)
    ctx.stroke()
    handleDot(ctx, f.x, f.y, COL_FOOT, active === 'foot')
  }

  ctx.restore()
}
