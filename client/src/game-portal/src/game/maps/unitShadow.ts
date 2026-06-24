import type { UnitBounds, UnitShadow } from './unitDefs'

// Resolved, ready-to-draw shadow parameters. All values are concrete numbers.
export type ResolvedUnitShadow = {
  radiusX: number
  radiusY: number
  opacity: number
  offsetX: number
  offsetY: number
}

// Default peak alpha at the shadow center.
const DEFAULT_OPACITY = 0.5
// radiusX defaults to a fraction of the body half-width so the shadow tucks
// just inside the footprint instead of fanning out past the feet.
const RADIUS_X_FROM_HALFWIDTH = 0.85
// Ellipse squash: ground shadows are wide and shallow.
const RADIUS_Y_FROM_RADIUS_X = 0.4

// Flyer auto-adjustment: drop the shadow to the ground beneath the floating
// sprite, spread it wider, and fade it to sell altitude. Each only applies when
// the corresponding field was NOT set explicitly in the unit's config.
export const FLYER_SHADOW_DROP = 14
const FLYER_RADIUS_SCALE = 1.15
const FLYER_OPACITY_SCALE = 0.6

// Global scene lighting: the light is treated as coming from the south-east of
// the map, so every ground shadow is cast toward the north-west (up and to the
// left). These describe a unit vector in that direction; the draw layer shifts
// each shadow along it by SHADOW_LIGHT_SHIFT * radiusX, so larger (taller)
// entities throw their shadow proportionally farther — chests barely move,
// buildings move more. Shared by units, buildings, and loot drops so the
// implied light source is consistent everywhere.
export const SHADOW_LIGHT_DX = -Math.SQRT1_2
export const SHADOW_LIGHT_DY = -Math.SQRT1_2
export const SHADOW_LIGHT_SHIFT = 0.3

const clamp01 = (n: number): number => (n < 0 ? 0 : n > 1 ? 1 : n)

/**
 * Resolves a unit's shadow config into concrete draw parameters, applying
 * bounds-derived defaults and the flyer adjustment. Returns null when the
 * shadow is disabled (so callers can skip drawing entirely).
 *
 * Explicit per-file values always win over the flyer auto-adjustment: a flyer
 * with `offsetY: 0` stays planted, a flyer with an explicit `radiusX` is not
 * scaled, etc. The `undefined` check on each field is what distinguishes
 * "author left it to the default" from "author set it on purpose".
 */
export function resolveUnitShadow(
  config: UnitShadow | undefined,
  bounds: UnitBounds,
  flyer: boolean,
): ResolvedUnitShadow | null {
  if (config?.enabled === false) return null

  const radiusXExplicit = config?.radiusX !== undefined
  const radiusYExplicit = config?.radiusY !== undefined
  const opacityExplicit = config?.opacity !== undefined
  const offsetYExplicit = config?.offsetY !== undefined

  let radiusX = radiusXExplicit ? config!.radiusX! : bounds.halfWidth * RADIUS_X_FROM_HALFWIDTH
  let radiusY = radiusYExplicit ? config!.radiusY! : radiusX * RADIUS_Y_FROM_RADIUS_X
  let opacity = clamp01(opacityExplicit ? config!.opacity! : DEFAULT_OPACITY)
  const offsetX = config?.offsetX ?? 0
  let offsetY = config?.offsetY ?? 0

  if (flyer) {
    if (!radiusXExplicit) radiusX *= FLYER_RADIUS_SCALE
    if (!radiusYExplicit) radiusY *= FLYER_RADIUS_SCALE
    if (!opacityExplicit) opacity = clamp01(opacity * FLYER_OPACITY_SCALE)
    if (!offsetYExplicit) offsetY += FLYER_SHADOW_DROP
  }

  return { radiusX, radiusY, opacity, offsetX, offsetY }
}
