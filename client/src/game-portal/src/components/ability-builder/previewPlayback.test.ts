import { describe, expect, it } from 'vitest'
import type { PreviewFrame } from '../../game/abilities/program/programPreview'
import {
  BBOX_PADDING_WORLD,
  FALLBACK_BBOX,
  MIN_VIEW_WORLD_HEIGHT,
  MIN_VIEW_WORLD_WIDTH,
  PREVIEW_FRAME_DT_SECONDS,
  computeSceneBBox,
  frameIndexAt,
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
