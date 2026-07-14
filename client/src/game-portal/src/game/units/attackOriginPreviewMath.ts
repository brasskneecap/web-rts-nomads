// Pure coordinate math for the attack-origin authoring crosshair
// (UnitSpritePreview.vue). Kept in ONE place so the canvas draw, the
// drag/click handlers, and the numeric inputs can never drift apart.
//
// `attackOrigin` values are screen-space pixel offsets from a unit's feet
// anchor (unit.x, unit.y) at 1x scale — the exact lift the renderer adds to
// proj.originX/originY (see CanvasRenderer.spriteBodyCenterLift and
// attackOriginResolve.ts). The preview draws the sprite scaled and centered
// inside a fixed box, so every function here maps between that authored
// offset and the canvas pixel the author actually clicks/drags.
import type { UnitAttackOrigin, UnitBounds, UnitOriginPoint } from '@/game/maps/unitDefs'
import {
  UNIT_SPRITE_BOTTOM_PADDING, UNIT_SPRITE_SCALE, UNIT_SPRITE_TOP_PADDING,
  type UnitDirection, type UnitSpriteSet,
} from '@/game/rendering/unitSprites'

// The exact geometry UnitSpritePreview's drawFrame computes each time it
// blits a frame: dx/dy are the centering offsets, w/h are the drawn (already
// scaled) sprite rect size, and scale is the integer fit scale. Never
// recomputed independently elsewhere — drawFrame returns this so overlay
// drawing and hit-testing use the SAME numbers that placed the sprite.
export interface PreviewDrawGeometry {
  dx: number
  dy: number
  w: number
  h: number
  scale: number
}

// Maps the unit's feet anchor (unit.x, unit.y in game terms) to a canvas
// pixel coordinate, given the sprite's draw geometry and the unit's bounds.
// Horizontally: the sprite is centered in its drawn rect, so the anchor sits
// at the rect's horizontal midpoint. Vertically: the sprite's bottom edge is
// the canvas bottom of the drawn rect, and the feet anchor sits
// `bounds.bottom` scaled pixels above it (bounds.bottom is a screen-space
// offset from unit.y at 1x scale, same convention as attackOrigin).
export function unitAnchorCanvas(geo: PreviewDrawGeometry, bounds: UnitBounds): { x: number; y: number } {
  const unitXCanvas = geo.dx + geo.w / 2
  const spriteBottomCanvas = geo.dy + geo.h
  const unitYCanvas = spriteBottomCanvas - bounds.bottom * geo.scale
  return { x: unitXCanvas, y: unitYCanvas }
}

// Authored offset -> canvas pixel, given the anchor and scale.
export function originToCanvas(
  origin: UnitOriginPoint,
  anchor: { x: number; y: number },
  scale: number,
): { x: number; y: number } {
  return { x: anchor.x + origin.x * scale, y: anchor.y + origin.y * scale }
}

// Canvas pixel -> authored offset (the inverse of originToCanvas). Rounds to
// whole pixels — attackOrigin is authored/consumed as integers throughout
// (getResolvedUnitAttackOrigin rounds on read too).
export function canvasToOrigin(
  cx: number,
  cy: number,
  anchor: { x: number; y: number },
  scale: number,
): UnitOriginPoint {
  return {
    x: Math.round((cx - anchor.x) / scale),
    y: Math.round((cy - anchor.y) / scale),
  }
}

// The point projectiles/spells leave an UNAUTHORED unit's body today:
// horizontal center, 30% down from the visible sprite top. Matches
// CanvasRenderer's spriteBodyCenterLift(..., TARGET_BODY_CENTER_FRACTION)
// exactly, so seeding the crosshair here makes "nothing moves until you
// drag" visibly true — dragging back to this exact point is a no-op.
export function deriveDefaultOrigin(spriteSet: UnitSpriteSet, bounds: UnitBounds): UnitOriginPoint {
  const h = spriteSet.size.height * UNIT_SPRITE_SCALE
  const visibleBottom = bounds.bottom - h * UNIT_SPRITE_BOTTOM_PADDING
  const visibleTop = bounds.bottom - h * (1 - UNIT_SPRITE_TOP_PADDING)
  return { x: 0, y: visibleTop + (visibleBottom - visibleTop) * 0.3 }
}

// Resolves the authored point for one facing from the in-progress editor
// value (byFacing wins over default), mirroring
// getResolvedUnitAttackOrigin's precedence but operating on the bare
// UnitAttackOrigin block the form holds rather than a full UnitDef. Returns
// null when nothing is authored at all, so callers fall back to the derived
// point instead of treating "unauthored" as (0, 0).
export function resolveFacingOrigin(
  ao: UnitAttackOrigin | undefined,
  facing: UnitDirection,
): UnitOriginPoint | null {
  const pick = ao?.byFacing?.[facing] ?? ao?.default
  return pick ? { x: pick.x, y: pick.y } : null
}
