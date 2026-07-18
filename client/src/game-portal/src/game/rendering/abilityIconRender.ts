// abilityIconRender: the ONE place an ability's action icon is resolved and
// drawn, so the in-game action bar (ActionIcon.vue) and the ability editor's
// preview/picker render byte-identically — pick an icon in the editor and it's
// exactly what shows on the action bar.
//
// An ability's `icon` string carries an optional scheme so it can point at an
// effect sprite-sheet frame or a projectile image, in addition to the legacy
// bundled/uploaded ability art:
//   "effect:<name>[@<frame>]"      — a frame of an effects/<name> sprite sheet
//   "projectile:<id>[@<frame>]"    — a frame of a projectiles/<id>.png strip
//   "<key>"                        — bundled ability art (assets/abilities/<key>)
//                                    or an editor-uploaded icon served by key
// A missing/placeholder value ("", "TODO/x.png") resolves to null and the
// caller falls back to ability-id art → projectile art (legacy order).
//
// Frame handling is unified: effect sheets know their frame count from the
// manifest; projectile/ability art is a flat PNG whose frame count is INFERRED
// from its aspect ratio (an N-wide strip), same as the action bar has always
// done. Selecting a frame just changes which cell is blitted.

import { getEffectSprite } from './effectSprites'
import { getBeamSprite } from './beamSprites'
import { getAbilityIconImageByKey, getAbilityAssetImage, getProjectileAssetImage } from './abilityAssets'
import { inferProjectileFrameCount } from './projectileSprites'

export type AbilityIconSource = 'effect' | 'beam' | 'projectile' | 'key'

export type AbilityIconRef =
  | { source: 'effect'; ref: string; frame: number }
  | { source: 'beam'; ref: string; frame: number }
  | { source: 'projectile'; ref: string; frame: number }
  | { source: 'key'; ref: string; frame: 0 }

// One scheme covers every sheet/flat art source: "<source>:<name>[@<frame>]".
const SCHEME_RE = /^(effect|beam|projectile):([a-z0-9_]+)(?:@(\d+))?$/i
const KEY_RE = /^[a-z0-9_]+$/i

// parseAbilityIcon turns an icon string into a typed ref, or null for an
// empty/placeholder value (which the caller resolves via legacy fallbacks).
export function parseAbilityIcon(icon: string | undefined | null): AbilityIconRef | null {
  if (!icon) return null
  const m = SCHEME_RE.exec(icon)
  if (m) {
    const source = m[1].toLowerCase() as 'effect' | 'beam' | 'projectile'
    return { source, ref: m[2].toLowerCase(), frame: m[3] ? Number(m[3]) : 0 }
  }
  if (KEY_RE.test(icon)) return { source: 'key', ref: icon.toLowerCase(), frame: 0 }
  return null
}

// formatAbilityIcon is the inverse: produce the stored string. Frame 0 is
// omitted so the common case stays clean ("effect:meteor", not "effect:meteor@0").
export function formatAbilityIcon(source: AbilityIconSource, ref: string, frame = 0): string {
  if (source === 'key') return ref
  return frame > 0 ? `${source}:${ref}@${frame}` : `${source}:${ref}`
}

export interface AbilityIconInput {
  /** The stored icon string (scheme or plain key). */
  icon?: string
  /** Fallback: bundled art at assets/abilities/<abilityId>/. */
  abilityId?: string
  /** Fallback: the ability's projectile image. */
  projectile?: string
}

interface SourceImage {
  image: HTMLImageElement
  // Sheet-based art (effect / beam) carries its manifest cell geometry. Flat
  // art (projectile / key) leaves this undefined and infers frames from the
  // decoded image's aspect ratio.
  sheet?: { frameWidth: number; frameHeight: number; frames: number }
}

// resolveSourceImage picks the underlying image for an icon input, or null.
function resolveSourceImage(input: AbilityIconInput): SourceImage | null {
  const parsed = parseAbilityIcon(input.icon)
  if (parsed?.source === 'effect') {
    const set = getEffectSprite(parsed.ref)
    return set?.image
      ? { image: set.image, sheet: { frameWidth: set.frameWidth, frameHeight: set.frameHeight, frames: set.frames } }
      : null
  }
  if (parsed?.source === 'beam') {
    const set = getBeamSprite(parsed.ref)
    return set?.image
      ? { image: set.image, sheet: { frameWidth: set.frameWidth, frameHeight: set.frameHeight, frames: set.frames } }
      : null
  }
  if (parsed?.source === 'projectile') {
    const img = getProjectileAssetImage(parsed.ref)
    return img ? { image: img } : null
  }
  const key = parsed?.source === 'key' ? parsed.ref : undefined
  const img =
    getAbilityIconImageByKey(key) ??
    (input.abilityId ? getAbilityAssetImage(input.abilityId) : null) ??
    (input.projectile ? getProjectileAssetImage(input.projectile) : null)
  return img ? { image: img } : null
}

// frameGeometry resolves the source rect for the CHOSEN frame of a DECODED
// image: sheet art uses the manifest cell size; flat art infers an N-wide strip
// from the aspect ratio.
function frameGeometry(src: SourceImage) {
  if (src.sheet) return { sw: src.sheet.frameWidth, sh: src.sheet.frameHeight, frames: src.sheet.frames }
  const img = src.image
  const frames = inferProjectileFrameCount(img.naturalWidth, img.naturalHeight)
  return { sw: img.naturalWidth / frames, sh: img.naturalHeight, frames }
}

const ICON_PADDING = 3

// drawAbilityIcon blits the resolved icon (chosen frame) centered into a
// `size`×`size` canvas context. Returns true if it drew. When the image is
// still decoding it registers `onReady` (a redraw) and returns false — the
// caller keeps whatever it had (usually a cleared cell) until the image lands.
export function drawAbilityIcon(
  ctx: CanvasRenderingContext2D,
  size: number,
  input: AbilityIconInput,
  onReady?: () => void,
): boolean {
  ctx.clearRect(0, 0, size, size)
  const src = resolveSourceImage(input)
  if (!src) return false
  const img = src.image
  if (!(img.complete && img.naturalWidth > 0)) {
    if (onReady) img.addEventListener('load', onReady, { once: true })
    return false
  }
  const { sw, sh, frames } = frameGeometry(src)
  const parsed = parseAbilityIcon(input.icon)
  const wanted = parsed && parsed.source !== 'key' ? parsed.frame : 0
  const frame = Math.min(Math.max(0, wanted), Math.max(0, frames - 1))
  ctx.imageSmoothingEnabled = false
  const box = size - ICON_PADDING * 2
  const scale = box / Math.max(sw, sh)
  const w = sw * scale
  const h = sh * scale
  ctx.drawImage(img, frame * sw, 0, sw, sh, ICON_PADDING + (box - w) / 2, ICON_PADDING + (box - h) / 2, w, h)
  return true
}

// abilityIconResolves reports whether this input resolves to ANY image (scheme,
// bundled/uploaded key, id art, or projectile). Lets a caller decide up front
// whether to show the canvas icon or fall back to a generic/SVG glyph.
export function abilityIconResolves(input: AbilityIconInput): boolean {
  return resolveSourceImage(input) !== null
}

// abilityIconFrameCount reports how many frames the chosen asset has, for the
// picker's frame slider. Effect counts are known from the manifest immediately;
// flat art needs its image decoded (returns 1 until then, callers re-check on load).
export function abilityIconFrameCount(source: AbilityIconSource, ref: string): number {
  if (source === 'effect') return getEffectSprite(ref)?.frames ?? 1
  if (source === 'beam') return getBeamSprite(ref)?.frames ?? 1
  const img = source === 'projectile' ? getProjectileAssetImage(ref) : getAbilityIconImageByKey(ref)
  if (img && img.complete && img.naturalWidth > 0) return inferProjectileFrameCount(img.naturalWidth, img.naturalHeight)
  return 1
}
