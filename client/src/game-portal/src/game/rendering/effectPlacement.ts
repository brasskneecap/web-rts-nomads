// Placement math for unit-anchored effect sprites, extracted as pure functions
// so the geometry is testable without a canvas (jsdom gives no 2D context, and
// an effect sheet never finishes decoding in tests — drawEffects bails before
// reaching any of this).
//
// TWO CONVENTIONS, deliberately. An effect sprite anchored to a unit is placed
// one of two ways, and which one applies is declared by the manifest:
//
//   1. ORIGIN-RELATIVE (no displayScale authored) — the historical one-shot
//      shape. The raw sheet frame is drawn at its authored pixel size, centred
//      on the unit's ORIGIN and lifted by the manifest's offsetY. The origin
//      sits at the unit's feet, which is why every one-shot manifest carries
//      the same -20 lift to get the art onto the body.
//
//   2. BODY OVERLAY (displayScale authored) — sized as a fraction of the
//      unit's RENDERED SPRITE height and placed against that sprite's rect, so
//      it scales with the silhouette instead of staying a fixed pixel size.
//      offsetX/offsetY are NOT applied: they are convention 1's origin
//      compensation, and adding them here would double-count the lift.
//
// Convention 2 exists because `burning` is drawn by two independent renderers:
// drawBurningOverlay (perk-driven, from UnitSnapshot.burningRemaining) and
// drawEffects (from an EffectSnapshot — how a data-authored status's bound
// visual arrives). Both now call spriteRectOverlayRect, so a composable burn
// and a legacy burn are pixel-identical BY CONSTRUCTION rather than by two
// hand-tuned code paths that agreed once and then drifted.
//
// THE SPRITE RECT IS THE RIGHT SPACE, not the logical bounds box. A unit's
// authored `bounds` (halfWidth/top/bottom) is its hit/selection body; the
// drawn sprite is a different, larger rect (spriteSet size × UNIT_SPRITE_SCALE,
// its bottom placed at unit.y + bounds.bottom) that includes the sheet's
// transparent padding. Art overlaid on a unit has to line up with the ART, so
// every function here works in sprite-rect space — the same space
// drawBurningOverlay has always used.

import { UNIT_SPRITE_SCALE } from './unitSprites'
import type { UnitBounds } from '../maps/unitDefs'

export interface SpriteRect {
  x: number
  y: number
  width: number
  height: number
}

export interface EffectRect {
  x: number
  y: number
  size: number
}

// unitSpriteRect reproduces the rect drawUnits draws a unit's frame into:
// full sheet size scaled by UNIT_SPRITE_SCALE, centred horizontally on the
// unit, with the rect's BOTTOM sitting at unit.y + bounds.bottom. Kept here
// (rather than inlined at each call site) so an overlay can be positioned
// against the same rectangle the sprite itself occupies without the caller
// re-deriving it and drifting.
export function unitSpriteRect(args: {
  unitX: number
  unitY: number
  bounds: UnitBounds
  spriteSize: { width: number; height: number }
}): SpriteRect {
  const width = args.spriteSize.width * UNIT_SPRITE_SCALE
  const height = args.spriteSize.height * UNIT_SPRITE_SCALE
  return {
    x: args.unitX - width / 2,
    y: args.unitY + args.bounds.bottom - height,
    width,
    height,
  }
}

// spriteRectOverlayRect places a square overlay frame against a unit's drawn
// sprite rect. `anchor` is the server-authored EffectAnchor: "head" aligns the
// overlay's top to the sprite's top, "feet" its bottom to the sprite's bottom,
// and anything else (including "center" and an unset value) centres it — which
// is what an author asking for "in the centre of the enemy" means.
//
// This is drawBurningOverlay's original geometry, verbatim; that function now
// calls this instead of keeping its own copy.
export function spriteRectOverlayRect(args: {
  rect: SpriteRect
  /** Fraction of the sprite's height: 1.0 = exactly the sprite's height. */
  displayScale: number
  /** Server-supplied per-instance multiplier; 1 when unset. */
  sizeScale?: number
  anchor?: string
}): EffectRect {
  const { rect } = args
  const size = rect.height * args.displayScale * (args.sizeScale ?? 1)

  let y: number
  switch (args.anchor) {
    case 'head':
      y = rect.y
      break
    case 'feet':
      y = rect.y + rect.height - size
      break
    default:
      y = rect.y + rect.height / 2 - size / 2
  }
  return { x: rect.x + rect.width / 2 - size / 2, y, size }
}

// OVERLAY_FRAME_MS is the cadence a body overlay cycles its sprite strip at.
// One constant, shared by both burn renderers — see overlayFrameIndex.
export const OVERLAY_FRAME_MS = 90

// overlayFrameIndex picks a body overlay's animation frame from the RENDER
// CLOCK, cycling the strip continuously.
//
// It must not come from the effect's progress (elapsed / lifetime), which is
// how one-shot effects pick their frame. A one-shot plays its strip exactly
// once across its whole life, and that is correct for a 0.3s fizzle. A body
// overlay lives as long as the status behind it — an 8-second burn spreading 5
// frames over 8 seconds advances once every 1.6s, which does not read as an
// animation at all, it reads as a still image. drawBurningOverlay always got
// this right by looping on the clock; the EffectSnapshot path did not, which is
// why the same fire animated when a perk lit it and froze when an ability did.
//
// Decoupling the two is also what lets the overlay's LIFETIME stay
// server-authoritative (it leaves the snapshot when the status ends) while its
// animation runs at an artist-tuned speed.
export function overlayFrameIndex(timeMs: number, frames: number): number {
  if (frames <= 0) return 0
  return Math.floor(timeMs / OVERLAY_FRAME_MS) % frames
}
