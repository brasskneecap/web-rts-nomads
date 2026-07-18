import { describe, expect, it } from 'vitest'
import { overlayCircles, screenToWorld, worldToScreen, type OverlayCamera, type OverlayInput } from './PreviewOverlays'

// Base input: caster and cast point both provided, both toggles on, both
// radii set. Individual tests override just the field under test.
function baseInput(overrides: Partial<OverlayInput> = {}): OverlayInput {
  return {
    castRange: 200,
    aoeRadius: 100,
    casterX: 50,
    casterY: 60,
    castX: 300,
    castY: 60,
    showCastRange: true,
    showAoe: true,
    ...overrides,
  }
}

describe('overlayCircles', () => {
  it('projects a known world point at a known camera to the expected screen coords', () => {
    // Camera.worldToScreen: screenX = (worldX - cam.x) * zoom.
    const cam = { x: 10, y: 20, zoom: 2 }
    const { castRange } = overlayCircles(
      baseInput({ casterX: 50, casterY: 60, aoeRadius: undefined }),
      cam,
      800,
      600,
    )
    expect(castRange).not.toBeNull()
    expect(castRange!.cx).toBe((50 - 10) * 2) // 80
    expect(castRange!.cy).toBe((60 - 20) * 2) // 80
  })

  it('scales world radius by zoom (r=100 @ zoom 2 -> 200 screen px)', () => {
    const cam = { x: 0, y: 0, zoom: 2 }
    const { aoe } = overlayCircles(baseInput({ aoeRadius: 100, castX: 0, castY: 0 }), cam, 800, 600)
    expect(aoe).not.toBeNull()
    expect(aoe!.radius).toBe(200)
  })

  it('projects the AoE circle at the cast point, independent of the caster position', () => {
    const cam = { x: 0, y: 0, zoom: 1 }
    const { aoe } = overlayCircles(baseInput({ castX: 300, castY: 60, casterX: 50, casterY: 60 }), cam, 800, 600)
    expect(aoe).not.toBeNull()
    expect(aoe!.cx).toBe(300)
    expect(aoe!.cy).toBe(60)
  })

  it('castRange is null when showCastRange is false', () => {
    const cam = { x: 0, y: 0, zoom: 1 }
    const { castRange } = overlayCircles(baseInput({ showCastRange: false }), cam, 800, 600)
    expect(castRange).toBeNull()
  })

  it('castRange is null when castRange is absent', () => {
    const cam = { x: 0, y: 0, zoom: 1 }
    const { castRange } = overlayCircles(baseInput({ castRange: undefined }), cam, 800, 600)
    expect(castRange).toBeNull()
  })

  it('castRange is null when castRange is 0', () => {
    const cam = { x: 0, y: 0, zoom: 1 }
    const { castRange } = overlayCircles(baseInput({ castRange: 0 }), cam, 800, 600)
    expect(castRange).toBeNull()
  })

  it('aoe is null when showAoe is false', () => {
    const cam = { x: 0, y: 0, zoom: 1 }
    const { aoe } = overlayCircles(baseInput({ showAoe: false }), cam, 800, 600)
    expect(aoe).toBeNull()
  })

  it('aoe is null when aoeRadius is absent', () => {
    const cam = { x: 0, y: 0, zoom: 1 }
    const { aoe } = overlayCircles(baseInput({ aoeRadius: undefined }), cam, 800, 600)
    expect(aoe).toBeNull()
  })

  it('aoe is null when aoeRadius is 0', () => {
    const cam = { x: 0, y: 0, zoom: 1 }
    const { aoe } = overlayCircles(baseInput({ aoeRadius: 0 }), cam, 800, 600)
    expect(aoe).toBeNull()
  })

  it('both circles present at once are independent (caster + cast point differ)', () => {
    const cam = { x: 0, y: 0, zoom: 1.5 }
    const { castRange, aoe } = overlayCircles(
      baseInput({ casterX: 0, casterY: 0, castRange: 200, castX: 400, castY: 0, aoeRadius: 80 }),
      cam,
      800,
      600,
    )
    expect(castRange).toEqual({ cx: 0, cy: 0, radius: 300 })
    expect(aoe).toEqual({ cx: 600, cy: 0, radius: 120 })
  })
})

// screenToWorld (Phase 6b: drag-to-place) — the preview canvas's drag
// handlers use this to convert a pointer event's screen position into the
// world position a dragged unit/caster should move to. It MUST be the exact
// algebraic inverse of worldToScreen (the same formula overlayCircles uses
// above), or a dragged unit would visibly drift from the cursor.
describe('screenToWorld', () => {
  const cams: OverlayCamera[] = [
    { x: 0, y: 0, zoom: 1 },
    { x: 10, y: 20, zoom: 2 },
    { x: -50, y: 300, zoom: 0.5 },
    { x: 600, y: 500, zoom: 1.75 },
  ]

  it('is the exact inverse of worldToScreen for a range of world points and cameras', () => {
    const worldPoints = [
      { x: 0, y: 0 },
      { x: 123.5, y: -45 },
      { x: -1000, y: 2000 },
      { x: 600, y: 500 },
    ]
    for (const cam of cams) {
      for (const p of worldPoints) {
        const screen = worldToScreen(p.x, p.y, cam)
        const roundTripped = screenToWorld(screen.x, screen.y, cam)
        expect(roundTripped.x).toBeCloseTo(p.x, 9)
        expect(roundTripped.y).toBeCloseTo(p.y, 9)
      }
    }
  })

  it('is the exact inverse in the other direction too: worldToScreen(screenToWorld(...)) round-trips', () => {
    const screenPoints = [
      { x: 0, y: 0 },
      { x: 400, y: 300 },
      { x: -20, y: 1000 },
    ]
    for (const cam of cams) {
      for (const s of screenPoints) {
        const world = screenToWorld(s.x, s.y, cam)
        const roundTripped = worldToScreen(world.x, world.y, cam)
        expect(roundTripped.x).toBeCloseTo(s.x, 9)
        expect(roundTripped.y).toBeCloseTo(s.y, 9)
      }
    }
  })

  it('matches the documented formula directly (world = screen / zoom + cam)', () => {
    const cam = { x: 10, y: 20, zoom: 2 }
    const world = screenToWorld(80, 80, cam)
    expect(world.x).toBe(80 / 2 + 10) // 50
    expect(world.y).toBe(80 / 2 + 20) // 60
  })

  it('zoom 1, camera at origin is the identity mapping', () => {
    const cam = { x: 0, y: 0, zoom: 1 }
    expect(screenToWorld(42, -17, cam)).toEqual({ x: 42, y: -17 })
  })
})
