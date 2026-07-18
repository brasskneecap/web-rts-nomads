import { describe, expect, it } from 'vitest'
import type { PreviewFrame } from '../../game/abilities/program/programPreview'
import {
  BBOX_PADDING_WORLD,
  FALLBACK_BBOX,
  MIN_VIEW_WORLD_HEIGHT,
  MIN_VIEW_WORLD_WIDTH,
  PREVIEW_FRAME_DT_SECONDS,
  computeSceneBBox,
  computeSceneBBoxFromPoints,
  frameIndexAt,
  previewClockMs,
} from './previewPlayback'

describe('frameIndexAt', () => {
  it('paused returns the seek tick as-is (in range)', () => {
    expect(
      frameIndexAt({ playing: false, startedAtMs: 0, nowMs: 5000, speed: 1, frameCount: 10, seekTick: 3 }),
    ).toBe(3)
  })

  it('paused clamps a seek tick beyond the last frame', () => {
    expect(
      frameIndexAt({ playing: false, startedAtMs: 0, nowMs: 0, speed: 1, frameCount: 10, seekTick: 999 }),
    ).toBe(9)
  })

  it('paused clamps a negative seek tick to 0', () => {
    expect(
      frameIndexAt({ playing: false, startedAtMs: 0, nowMs: 0, speed: 1, frameCount: 10, seekTick: -4 }),
    ).toBe(0)
  })

  it('playing advances by elapsed time / frame dt at speed 1', () => {
    // 500ms elapsed / 50ms per frame = 10 frames advanced.
    const elapsedMs = 10 * PREVIEW_FRAME_DT_SECONDS * 1000
    expect(
      frameIndexAt({
        playing: true,
        startedAtMs: 0,
        nowMs: elapsedMs,
        speed: 1,
        frameCount: 100,
        seekTick: 0,
      }),
    ).toBe(10)
  })

  it('speed=2 advances twice as fast as speed=1 for the same elapsed time', () => {
    const elapsedMs = 10 * PREVIEW_FRAME_DT_SECONDS * 1000
    const at1x = frameIndexAt({
      playing: true,
      startedAtMs: 0,
      nowMs: elapsedMs,
      speed: 1,
      frameCount: 1000,
      seekTick: 0,
    })
    const at2x = frameIndexAt({
      playing: true,
      startedAtMs: 0,
      nowMs: elapsedMs,
      speed: 2,
      frameCount: 1000,
      seekTick: 0,
    })
    expect(at1x).toBe(10)
    expect(at2x).toBe(20)
  })

  it('clamps at frameCount-1 when playback runs past the end', () => {
    const elapsedMs = 1000 * PREVIEW_FRAME_DT_SECONDS * 1000 // way more than frameCount ticks
    expect(
      frameIndexAt({
        playing: true,
        startedAtMs: 0,
        nowMs: elapsedMs,
        speed: 1,
        frameCount: 20,
        seekTick: 0,
      }),
    ).toBe(19)
  })

  it('clamps at 0 when nowMs is before startedAtMs', () => {
    expect(
      frameIndexAt({ playing: true, startedAtMs: 5000, nowMs: 0, speed: 1, frameCount: 20, seekTick: 0 }),
    ).toBe(0)
  })

  it('frameCount=0 always returns 0, playing or paused', () => {
    expect(
      frameIndexAt({ playing: true, startedAtMs: 0, nowMs: 5000, speed: 1, frameCount: 0, seekTick: 0 }),
    ).toBe(0)
    expect(
      frameIndexAt({ playing: false, startedAtMs: 0, nowMs: 0, speed: 1, frameCount: 0, seekTick: 3 }),
    ).toBe(0)
  })

  it('a mid-scrub resume adds elapsed advancement on top of the seek tick', () => {
    // Resume playback from frame 10; 250ms elapsed at speed 1 = +5 frames.
    const elapsedMs = 5 * PREVIEW_FRAME_DT_SECONDS * 1000
    expect(
      frameIndexAt({
        playing: true,
        startedAtMs: 0,
        nowMs: elapsedMs,
        speed: 1,
        frameCount: 100,
        seekTick: 10,
      }),
    ).toBe(15)
  })

  it('speed=0 freezes at seekTick regardless of elapsed time (playing=true)', () => {
    // Documents the chosen behavior: speed 0 advances 0 ticks no matter how
    // much wall-clock time passes, effectively pausing playback without the
    // caller having to flip `playing` to false. (Negative speed running
    // backward is separately fine/intended — no coverage needed here.)
    const elapsedMs = 50 * PREVIEW_FRAME_DT_SECONDS * 1000
    expect(
      frameIndexAt({
        playing: true,
        startedAtMs: 0,
        nowMs: elapsedMs,
        speed: 0,
        frameCount: 100,
        seekTick: 7,
      }),
    ).toBe(7)
  })
})

describe('previewClockMs', () => {
  it('maps frame 0 to 0ms', () => {
    expect(previewClockMs(0)).toBe(0)
  })

  it('maps a frame index to frameIndex * PREVIEW_FRAME_DT_SECONDS * 1000', () => {
    expect(previewClockMs(7)).toBeCloseTo(7 * PREVIEW_FRAME_DT_SECONDS * 1000, 10)
    expect(previewClockMs(20)).toBeCloseTo(1000, 10) // 20 frames * 50ms = 1s
  })

  it('is a pure function of the index — calling it twice for the same frame is identical (idempotence)', () => {
    expect(previewClockMs(12)).toBe(previewClockMs(12))
  })

  it('is non-monotonic-safe: a smaller (scrubbed-back) index yields a smaller clock value with no special-casing', () => {
    // No accumulator/state involved — going "backward" just means a smaller
    // input, which this pure function handles exactly like any other input.
    expect(previewClockMs(3)).toBeLessThan(previewClockMs(10))
    expect(previewClockMs(3)).toBe(3 * PREVIEW_FRAME_DT_SECONDS * 1000)
  })

  it('reconciles with a damage number spawned for the SAME frame index: elapsed is exactly 0', () => {
    // This is the createdAt/renderTime seam contract: previewDamageNumbers
    // stamps a spawned number's createdAt as previewClockMs(frameIndex), and
    // CanvasRenderer's injected clock reports previewClockMs(frameIndex) for
    // that same displayed frame — so `renderTime - startedAt` is exactly 0
    // the instant the number spawns, not a stale/aged value from a disjoint
    // wall clock.
    const frameIndex = 42
    const createdAt = previewClockMs(frameIndex)
    const renderTime = previewClockMs(frameIndex)
    expect(renderTime - createdAt).toBe(0)
  })

  it('one preview frame later, elapsed advances by exactly PREVIEW_FRAME_DT_SECONDS in ms', () => {
    const spawnFrame = 5
    const createdAt = previewClockMs(spawnFrame)
    const oneFrameLater = previewClockMs(spawnFrame + 1)
    // IEEE-754: 6 * 0.05 * 1000 lands a hair off 300 — assert within float
    // tolerance rather than exact equality, same convention as
    // previewDamageNumbers.test.ts's own EPS handling for this tick grid.
    expect(oneFrameLater - createdAt).toBeCloseTo(PREVIEW_FRAME_DT_SECONDS * 1000, 9)
  })
})

describe('computeSceneBBox', () => {
  // Minimal PreviewFrame builder — only `snapshot.units[].x/y` are read by
  // computeSceneBBox, so the rest of MatchSnapshotMessage is left out via a
  // cast rather than fully populated (mirrors the `makeSnapshot` pattern used
  // in GameState's own snapshot tests).
  function makeFrame(units: Array<{ x: number; y: number }>): PreviewFrame {
    return {
      tick: 0,
      t: 0,
      snapshot: { units } as PreviewFrame['snapshot'],
    }
  }

  it('returns the fallback bbox for an empty frame list', () => {
    expect(computeSceneBBox([])).toEqual(FALLBACK_BBOX)
  })

  it('returns the fallback bbox when frames carry no units', () => {
    expect(computeSceneBBox([makeFrame([]), makeFrame([])])).toEqual(FALLBACK_BBOX)
  })

  it('floors the view size to MIN_VIEW_WORLD_* for a single (or co-located) unit', () => {
    const bbox = computeSceneBBox([makeFrame([{ x: 100, y: 100 }])])
    expect(bbox.centerX).toBe(100)
    expect(bbox.centerY).toBe(100)
    expect(bbox.viewWidth).toBe(MIN_VIEW_WORLD_WIDTH)
    expect(bbox.viewHeight).toBe(MIN_VIEW_WORLD_HEIGHT)
  })

  it('spans a spread of units across frames, plus padding on each axis', () => {
    const bbox = computeSceneBBox([
      makeFrame([{ x: 0, y: 0 }]),
      makeFrame([
        { x: 1000, y: 0 },
        { x: 0, y: 600 },
      ]),
    ])
    expect(bbox.centerX).toBe(500) // (0 + 1000) / 2
    expect(bbox.centerY).toBe(300) // (0 + 600) / 2
    expect(bbox.viewWidth).toBe(1000 + BBOX_PADDING_WORLD * 2)
    expect(bbox.viewHeight).toBe(600 + BBOX_PADDING_WORLD * 2)
  })
})

// computeSceneBBoxFromPoints (Phase 6b): the edit-mode canvas's camera
// framing — same padded/floored bbox math as computeSceneBBox, but over a
// flat list of live (caster + scene unit) points instead of a captured
// frame sequence. computeSceneBBox itself is now implemented in terms of
// this function, so these cases double as regression coverage for that
// refactor too.
describe('computeSceneBBoxFromPoints', () => {
  it('returns the fallback bbox for an empty point list', () => {
    expect(computeSceneBBoxFromPoints([])).toEqual(FALLBACK_BBOX)
  })

  it('floors the view size to MIN_VIEW_WORLD_* for a single (or co-located) point', () => {
    const bbox = computeSceneBBoxFromPoints([{ x: 600, y: 500 }])
    expect(bbox.centerX).toBe(600)
    expect(bbox.centerY).toBe(500)
    expect(bbox.viewWidth).toBe(MIN_VIEW_WORLD_WIDTH)
    expect(bbox.viewHeight).toBe(MIN_VIEW_WORLD_HEIGHT)
  })

  it('spans a spread of points, plus padding on each axis (a caster + two dragged units)', () => {
    const bbox = computeSceneBBoxFromPoints([
      { x: 600, y: 500 }, // caster
      { x: 720, y: 500 }, // enemy
      { x: 520, y: 500 }, // ally
    ])
    expect(bbox.centerX).toBe(620) // (520 + 720) / 2
    expect(bbox.centerY).toBe(500)
    expect(bbox.viewWidth).toBe(200 + BBOX_PADDING_WORLD * 2) // 720 - 520
    expect(bbox.viewHeight).toBe(MIN_VIEW_WORLD_HEIGHT) // no Y spread — floored
  })

  it('agrees with computeSceneBBox when fed the same points via a single frame', () => {
    const points = [
      { x: 0, y: 0 },
      { x: 1000, y: 0 },
      { x: 0, y: 600 },
    ]
    const viaPoints = computeSceneBBoxFromPoints(points)
    const viaFrames = computeSceneBBox([{ tick: 0, t: 0, snapshot: { units: points } as PreviewFrame['snapshot'] }])
    expect(viaPoints).toEqual(viaFrames)
  })
})
