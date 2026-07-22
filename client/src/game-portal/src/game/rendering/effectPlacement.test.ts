import { describe, expect, it } from 'vitest'
import { DEFAULT_UNIT_BOUNDS } from '../maps/unitDefs'
import { UNIT_SPRITE_SCALE } from './unitSprites'
import {
  OVERLAY_FRAME_MS,
  overlayFrameIndex,
  spriteRectOverlayRect,
  unitSpriteRect,
  type SpriteRect,
} from './effectPlacement'

// Everything below derives from the inputs rather than restating pixel
// constants, so re-tuning UNIT_SPRITE_SCALE or a unit's bounds doesn't silently
// invalidate the assertions.
const spriteSize = { width: 64, height: 96 }

describe('unitSpriteRect', () => {
  const args = { unitX: 100, unitY: 200, bounds: DEFAULT_UNIT_BOUNDS, spriteSize }

  it('scales the sheet size by UNIT_SPRITE_SCALE', () => {
    const r = unitSpriteRect(args)
    expect(r.width).toBeCloseTo(spriteSize.width * UNIT_SPRITE_SCALE)
    expect(r.height).toBeCloseTo(spriteSize.height * UNIT_SPRITE_SCALE)
  })

  it('centres horizontally on the unit', () => {
    const r = unitSpriteRect(args)
    expect(r.x + r.width / 2).toBeCloseTo(args.unitX)
  })

  // This is the whole reason the sprite rect is the right space: the rect's
  // BOTTOM sits at unit.y + bounds.bottom (matching how drawUnits places the
  // frame), so the rect extends well ABOVE the unit's origin. Placing an
  // overlay against the logical bounds box instead put it far too low.
  it("puts the rect's bottom at unit.y + bounds.bottom, so the rect rises above the origin", () => {
    const r = unitSpriteRect(args)
    expect(r.y + r.height).toBeCloseTo(args.unitY + DEFAULT_UNIT_BOUNDS.bottom)
    expect(r.y).toBeLessThan(args.unitY)
  })
})

describe('spriteRectOverlayRect', () => {
  const rect: SpriteRect = { x: 0, y: 0, width: 64, height: 96 }
  const base = { rect, displayScale: 0.4 }

  it('sizes the overlay as a fraction of the sprite height, not the raw sheet size', () => {
    expect(spriteRectOverlayRect(base).size).toBeCloseTo(rect.height * 0.4)
  })

  it('centres horizontally on the sprite rect', () => {
    const r = spriteRectOverlayRect(base)
    expect(r.x + r.size / 2).toBeCloseTo(rect.x + rect.width / 2)
  })

  it('"center" centres vertically on the sprite rect', () => {
    const r = spriteRectOverlayRect({ ...base, anchor: 'center' })
    expect(r.y + r.size / 2).toBeCloseTo(rect.y + rect.height / 2)
  })

  it('an unspecified anchor behaves as "center"', () => {
    expect(spriteRectOverlayRect(base)).toEqual(spriteRectOverlayRect({ ...base, anchor: 'center' }))
  })

  it('"feet" aligns the overlay\'s bottom to the sprite\'s bottom', () => {
    const r = spriteRectOverlayRect({ ...base, anchor: 'feet' })
    expect(r.y + r.size).toBeCloseTo(rect.y + rect.height)
  })

  it('"head" aligns the overlay\'s top to the sprite\'s top', () => {
    expect(spriteRectOverlayRect({ ...base, anchor: 'head' }).y).toBeCloseTo(rect.y)
  })

  it('the server-supplied sizeScale multiplies the size; an unset one does not zero it', () => {
    expect(spriteRectOverlayRect({ ...base, sizeScale: 2 }).size).toBeCloseTo(rect.height * 0.8)
    expect(spriteRectOverlayRect({ ...base, sizeScale: undefined }).size).toBeCloseTo(rect.height * 0.4)
  })

  it('a displayScale of 1 spans exactly the sprite height', () => {
    const r = spriteRectOverlayRect({ ...base, displayScale: 1, anchor: 'feet' })
    expect(r.size).toBeCloseTo(rect.height)
    expect(r.y).toBeCloseTo(rect.y)
  })

  // The contract that makes the two burn renderers agree: given the same
  // sprite rect and manifest, an EffectSnapshot-driven burn and a
  // burningRemaining-driven burn resolve to the identical rectangle. They no
  // longer merely resemble each other — they are one function.
  it('is the single geometry both burn renderers use', () => {
    const legacy = spriteRectOverlayRect({ rect, displayScale: 0.4, anchor: 'center' })
    const composable = spriteRectOverlayRect({
      rect,
      displayScale: 0.4,
      sizeScale: 1,
      anchor: 'center',
    })
    expect(composable).toEqual(legacy)
  })
})

describe('overlayFrameIndex', () => {
  const frames = 5

  it('advances one frame per OVERLAY_FRAME_MS and wraps', () => {
    expect(overlayFrameIndex(0, frames)).toBe(0)
    expect(overlayFrameIndex(OVERLAY_FRAME_MS, frames)).toBe(1)
    expect(overlayFrameIndex(OVERLAY_FRAME_MS * (frames - 1), frames)).toBe(frames - 1)
    expect(overlayFrameIndex(OVERLAY_FRAME_MS * frames, frames)).toBe(0) // wrapped
  })

  // THE BUG. The one-shot rule picks frames from progress (elapsed / lifetime).
  // For an 8s burn over 5 frames that advances once per 1.6s — a still image.
  // The clock-driven rule cycles the whole strip many times over the same span.
  it('cycles many times across a long-lived overlay, unlike the progress rule', () => {
    const lifetimeMs = 8000
    const seen = new Set<number>()
    for (let t = 0; t < lifetimeMs; t += OVERLAY_FRAME_MS) seen.add(overlayFrameIndex(t, frames))
    expect(seen.size).toBe(frames)

    // What the progress rule would have produced across the first second: one
    // single frame, which is exactly what "it just appears as one frame" was.
    const progressFrames = new Set<number>()
    for (let t = 0; t < 1000; t += 50) {
      progressFrames.add(Math.min(frames - 1, Math.floor((t / lifetimeMs) * frames)))
    }
    expect(progressFrames.size).toBe(1)
  })

  it('is stable for a paused clock, so a frozen preview frame does not animate', () => {
    expect(overlayFrameIndex(700, frames)).toBe(overlayFrameIndex(700, frames))
  })

  it('never returns a negative or out-of-range index for a degenerate strip', () => {
    expect(overlayFrameIndex(1234, 0)).toBe(0)
    expect(overlayFrameIndex(1234, 1)).toBe(0)
  })
})
