// previewPlayback: pure, DOM-free playback clock for the ability preview
// replay (AbilityPreviewCanvas.vue). The harness (programPreview.ts) captures
// one PreviewFrame per PREVIEW_FRAME_DT_SECONDS of simulated time — this
// module maps wall-clock elapsed time (scaled by a user-selected speed) onto
// a frame INDEX. The visuals for that index are authoritative from the
// snapshot itself; wall-clock only ever selects WHICH frame to show.
//
// Also hosts computeSceneBBox — the (DOM-free) camera-framing bbox math —
// so it's independently testable and reusable ahead of Task 7/8's transport
// controls.

import type { PreviewFrame } from '../../game/abilities/program/programPreview'

// Matches the harness's previewTickDT (server-side tick capture interval).
export const PREVIEW_FRAME_DT_SECONDS = 0.05

// Clamps a raw tick/frame index into the valid [0, frameCount-1] range.
// frameCount === 0 (no frames captured — older response shape or a failed
// cast) always yields 0 so callers never index into an empty array.
function clampTick(tick: number, frameCount: number): number {
  if (frameCount <= 0) return 0
  return Math.min(frameCount - 1, Math.max(0, tick))
}

export interface FrameIndexAtOptions {
  playing: boolean
  /** performance.now()-style timestamp when playback (from seekTick) started. */
  startedAtMs: number
  /** performance.now()-style timestamp "now". */
  nowMs: number
  /** Playback speed multiplier (1 = real-time, 2 = 2x, etc). */
  speed: number
  frameCount: number
  /** The frame index playback resumed from — the paused/scrubbed position. */
  seekTick: number
}

// frameIndexAt computes which frame should be displayed right now.
//
// Paused: the seek position, clamped.
// Playing: seekTick plus however many PREVIEW_FRAME_DT_SECONDS ticks have
// elapsed (in simulated time) since startedAtMs, clamped.
export function frameIndexAt(opts: FrameIndexAtOptions): number {
  const { playing, startedAtMs, nowMs, speed, frameCount, seekTick } = opts
  if (!playing) return clampTick(seekTick, frameCount)
  const elapsedSeconds = (nowMs - startedAtMs) / 1000
  const advancedTicks = Math.floor((elapsedSeconds * speed) / PREVIEW_FRAME_DT_SECONDS)
  return clampTick(seekTick + advancedTicks, frameCount)
}

// ── camera framing ──────────────────────────────────────────────────────
// World-space padding added around the tightest bounding box of every unit
// seen across the whole frame sequence, plus a floor so a single-unit (or
// tightly-clustered) scene doesn't zoom in absurdly tight.
export const BBOX_PADDING_WORLD = 160
export const MIN_VIEW_WORLD_WIDTH = 480
export const MIN_VIEW_WORLD_HEIGHT = 320

export interface SceneBBox {
  centerX: number
  centerY: number
  viewWidth: number
  viewHeight: number
}

export const FALLBACK_BBOX: SceneBBox = {
  centerX: 0,
  centerY: 0,
  viewWidth: MIN_VIEW_WORLD_WIDTH,
  viewHeight: MIN_VIEW_WORLD_HEIGHT,
}

// computeSceneBBox unions every unit's (x, y) across every captured frame —
// cheap for a preview run (a handful of units × a few dozen ticks) — so the
// camera framing stays stable across the whole replay instead of jittering
// as units move tick-to-tick.
export function computeSceneBBox(frames: PreviewFrame[]): SceneBBox {
  let minX = Infinity
  let maxX = -Infinity
  let minY = Infinity
  let maxY = -Infinity
  let found = false
  for (const frame of frames) {
    for (const u of frame.snapshot.units ?? []) {
      found = true
      if (u.x < minX) minX = u.x
      if (u.x > maxX) maxX = u.x
      if (u.y < minY) minY = u.y
      if (u.y > maxY) maxY = u.y
    }
  }
  if (!found) return FALLBACK_BBOX
  return {
    centerX: (minX + maxX) / 2,
    centerY: (minY + maxY) / 2,
    viewWidth: Math.max(maxX - minX + BBOX_PADDING_WORLD * 2, MIN_VIEW_WORLD_WIDTH),
    viewHeight: Math.max(maxY - minY + BBOX_PADDING_WORLD * 2, MIN_VIEW_WORLD_HEIGHT),
  }
}

// ── camera fit (zoom + pan) ──────────────────────────────────────────────
// AbilityPreviewCanvas.vue sets Camera.x/y/zoom DIRECTLY every rendered
// frame, bypassing Camera.centerOn()/clamp() (see that component's
// refreshCamera doc comment for why — those methods' overscan-padded
// clamping is tuned for the live in-game HUD, not a small preview scene).
// This module still needs ITS OWN edge-awareness though: the preview
// replays onto a bare `new GameState()`, whose placeholder map renders
// terrain only across [0, mapWidth] x [0, mapHeight] — starting at world
// (0, 0), NOT centered on it. Preview scenes are authored with the caster
// near that (0, 0) corner (see ability_preview.go's CasterX/Y), so naively
// centering the camera on the scene's bbox routinely reveals negative-
// coordinate space with no terrain to show — a black void, worse the wider
// (and shorter) this component's stage gets. computeCameraFit fixes that by
// clamping each axis into the map's actual valid range, falling back to
// literal bbox-centering only once that clamp is a no-op (i.e. the scene
// isn't pinned against an edge).
export interface CameraFit {
  zoom: number
  x: number
  y: number
}

// clampCameraAxis pins one axis of a naturally-centered camera position so
// the viewport never extends past [0, mapSpan] on that axis. When the
// viewport is itself wider than the map (extreme zoom-out), there's no
// valid "no void" position — center the MAP in the viewport instead, same
// fallback Camera.clamp() uses.
function clampCameraAxis(centeredPos: number, viewSpan: number, mapSpan: number): number {
  if (viewSpan >= mapSpan) return (mapSpan - viewSpan) / 2
  return Math.min(mapSpan - viewSpan, Math.max(0, centeredPos))
}

// computeCameraFit picks a zoom that fits `bbox` into a `canvasWidth` x
// `canvasHeight` stage at ANY aspect ratio (uniform scale — a single `zoom`
// for both axes, since AoE/cast-range ring radii in PreviewOverlays.ts also
// scale by `zoom` alone and would go elliptical under a non-uniform fit),
// clamped to [minZoom, maxZoom], then pans to keep the bbox centered UNLESS
// doing so would reveal off-map void (see clampCameraAxis above).
export function computeCameraFit(
  bbox: SceneBBox,
  canvasWidth: number,
  canvasHeight: number,
  mapWidth: number,
  mapHeight: number,
  minZoom: number,
  maxZoom: number,
): CameraFit {
  const zoomX = canvasWidth / bbox.viewWidth
  const zoomY = canvasHeight / bbox.viewHeight
  const zoom = Math.min(maxZoom, Math.max(minZoom, Math.min(zoomX, zoomY)))
  const viewWorldWidth = canvasWidth / zoom
  const viewWorldHeight = canvasHeight / zoom
  return {
    zoom,
    x: clampCameraAxis(bbox.centerX - viewWorldWidth / 2, viewWorldWidth, mapWidth),
    y: clampCameraAxis(bbox.centerY - viewWorldHeight / 2, viewWorldHeight, mapHeight),
  }
}
