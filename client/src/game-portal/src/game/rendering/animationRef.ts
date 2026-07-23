// animationRef: the ONE place a create_zone visual (presentation / visible
// sprite) resolves its animation source, so the editor picker, the preview, and
// the in-game renderer all speak the same vocabulary and draw byte-identically.
//
// An animation reference is a scheme string pointing at any of the four sprite
// sources the game already ships loaders for:
//   "effect:<name>"                 — an effects/<name> sprite sheet
//   "projectile:<id>"               — a projectiles/<id>.png strip
//   "beam:<name>"                   — a beams/<name> sprite sheet
//   "object:<key>[@<state>]"        — an objects/<key> sprite-set; @state picks
//                                     which animation (idle when omitted)
//
// This mirrors abilityIconRender's icon scheme, but with two differences that
// matter: (1) it adds `object` (with a named animation STATE, not a frame
// index), and (2) it resolves the WHOLE animation (every frame + loop + tempo)
// so a decal can play, where an icon freezes on one frame. Keep the two schemes
// separate: an icon picks a still frame (`@<number>`), an animation picks a
// state (`@<name>`) and plays.

import { getEffectSprite } from './effectSprites'
import { getBeamSprite } from './beamSprites'
import { getProjectileAssetImage, getAbilityIconImageByKey } from './abilityAssets'
import { inferProjectileFrameCount } from './projectileSprites'
import { getObjectSpriteSet } from './objectSprites'

// `image` is a custom per-ability uploaded PNG (the Upload tab), served by key —
// a single static frame, no animation. Every other source is a real sprite
// sheet/strip.
export type AnimationSource = 'effect' | 'projectile' | 'beam' | 'object' | 'image'

export interface AnimationRef {
  source: AnimationSource
  ref: string
  /** Object animation state (idle when omitted). Only meaningful for `object`. */
  state?: string
}

// Frame tempo fallbacks (ms/frame) when a source's loader carries no opinion.
// Effect matches CanvasRenderer's EFFECT_LOOP_FRAME_MS; object matches its
// TRAP_IDLE_FRAME_MS; projectile strips get a middling cycle.
const EFFECT_FRAME_MS = 120
const OBJECT_FRAME_MS = 150
const PROJECTILE_FRAME_MS = 90
const DEFAULT_OBJECT_STATE = 'idle'

const SCHEME_RE = /^(effect|projectile|beam|object|image):([a-z0-9_]+)(?:@([a-z0-9_]+))?$/i

// parseAnimationRef turns a scheme string into a typed ref, or null for an
// empty / unrecognized value (the caller then renders nothing).
export function parseAnimationRef(s: string | undefined | null): AnimationRef | null {
  if (!s) return null
  const m = SCHEME_RE.exec(s)
  if (!m) return null
  const source = m[1].toLowerCase() as AnimationSource
  const ref = m[2].toLowerCase()
  const state = m[3]?.toLowerCase()
  if (source === 'object' && state) return { source, ref, state }
  return { source, ref }
}

// formatAnimationRef is the inverse. A bare or `idle` object state is omitted so
// the common case stays clean ("object:fire_pit", not "object:fire_pit@idle").
export function formatAnimationRef(source: AnimationSource, ref: string, state?: string): string {
  if (source === 'object' && state && state !== DEFAULT_OBJECT_STATE) return `object:${ref}@${state}`
  return `${source}:${ref}`
}

export interface AnimationFrames {
  image: HTMLImageElement
  /** Source-rect geometry of ONE frame within the sheet/strip. */
  frameWidth: number
  frameHeight: number
  frameCount: number
  loop: boolean
  frameDurationMs: number
}

// resolveAnimationFrames maps a ref (typed or scheme string) onto the underlying
// sprite sheet's frame geometry, reusing the same per-source loaders the rest of
// the renderer uses. Returns null when the source art isn't registered. Sheet
// art (effect / beam / object) knows its geometry from a manifest immediately;
// projectile strips infer frame count from the decoded image's aspect ratio, so
// their geometry firms up once the image lands (frameCount 1 until then).
export function resolveAnimationFrames(ref: AnimationRef | string): AnimationFrames | null {
  const parsed = typeof ref === 'string' ? parseAnimationRef(ref) : ref
  if (!parsed) return null

  if (parsed.source === 'effect') {
    const set = getEffectSprite(parsed.ref)
    if (!set?.image) return null
    return {
      image: set.image,
      frameWidth: set.frameWidth,
      frameHeight: set.frameHeight,
      frameCount: set.frames,
      loop: set.loop ?? false,
      frameDurationMs: EFFECT_FRAME_MS,
    }
  }

  if (parsed.source === 'beam') {
    const set = getBeamSprite(parsed.ref)
    if (!set?.image) return null
    return {
      image: set.image,
      frameWidth: set.frameWidth,
      frameHeight: set.frameHeight,
      frameCount: set.frames,
      loop: true,
      frameDurationMs: set.frameDurationMs,
    }
  }

  if (parsed.source === 'projectile') {
    const img = getProjectileAssetImage(parsed.ref)
    if (!img) return null
    const frameCount = img.complete && img.naturalWidth > 0
      ? inferProjectileFrameCount(img.naturalWidth, img.naturalHeight)
      : 1
    return {
      image: img,
      frameWidth: img.naturalWidth > 0 ? img.naturalWidth / frameCount : 0,
      frameHeight: img.naturalHeight,
      frameCount,
      loop: true,
      frameDurationMs: PROJECTILE_FRAME_MS,
    }
  }

  if (parsed.source === 'image') {
    const img = getAbilityIconImageByKey(parsed.ref)
    if (!img) return null
    return {
      image: img,
      frameWidth: img.naturalWidth,
      frameHeight: img.naturalHeight,
      frameCount: 1,
      loop: false,
      frameDurationMs: PROJECTILE_FRAME_MS,
    }
  }

  // object
  const set = getObjectSpriteSet(parsed.ref)
  const anim = set?.animations.get(parsed.state ?? DEFAULT_OBJECT_STATE) ?? set?.animations.get(DEFAULT_OBJECT_STATE)
  if (!anim) return null
  return {
    image: anim.sheet,
    frameWidth: anim.frameWidth,
    frameHeight: anim.frameHeight,
    frameCount: anim.frameCount,
    loop: anim.loop,
    frameDurationMs: anim.frameDurationMs ?? OBJECT_FRAME_MS,
  }
}

// oneShotDecalFrame maps a transient decal's SERVER lifetime onto its animation
// frames: it plays 0..frameCount-1 exactly once as `remaining` counts down from
// `total` to 0. This is what makes every decal spawn play the FULL animation
// from the start — driving the frame off the absolute render clock instead makes
// a later spawn jump straight to the last frame (an explosion that's all smoke).
export function oneShotDecalFrame(remaining: number, total: number, frameCount: number): number {
  const n = Math.max(1, frameCount)
  if (total <= 0) return n - 1
  const progress = Math.min(1, Math.max(0, (total - remaining) / total))
  return Math.min(n - 1, Math.floor(progress * n))
}

// animationFrameIndex selects which frame to draw at a wall-clock time. Looping
// animations cycle on the clock; non-looping ones play once and hold the last
// frame (progress is time-since-spawn / total duration for a timed decal, but a
// clock-only caller just holds frame 0..N-1 by elapsed).
export function animationFrameIndex(frames: AnimationFrames, clockMs: number): number {
  const n = Math.max(1, frames.frameCount)
  const step = Math.floor(clockMs / frames.frameDurationMs)
  return frames.loop ? ((step % n) + n) % n : Math.min(n - 1, Math.max(0, step))
}

// drawAnimationDecal blits the current frame of `ref` centered at (cx, cy),
// scaled by `scale`, using `clockMs` to pick the frame. Returns true if it drew.
// Registers a one-shot redraw via onReady while the image is still decoding.
export function drawAnimationDecal(
  ctx: CanvasRenderingContext2D,
  ref: AnimationRef | string,
  cx: number,
  cy: number,
  scale: number,
  clockMs: number,
  onReady?: () => void,
): boolean {
  const frames = resolveAnimationFrames(ref)
  if (!frames) return false
  const img = frames.image
  if (!(img.complete && img.naturalWidth > 0)) {
    if (onReady) img.addEventListener('load', onReady, { once: true })
    return false
  }
  const frame = animationFrameIndex(frames, clockMs)
  const sw = frames.frameWidth
  const sh = frames.frameHeight
  if (sw <= 0 || sh <= 0) return false
  ctx.imageSmoothingEnabled = false
  const dw = sw * scale
  const dh = sh * scale
  ctx.drawImage(img, frame * sw, 0, sw, sh, cx - dw / 2, cy - dh / 2, dw, dh)
  return true
}
