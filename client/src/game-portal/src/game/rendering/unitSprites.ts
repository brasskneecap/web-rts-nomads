// Loader for packed unit sprites. Consumes `sprites.json` (produced by
// `npm run pack:sprites`) and its sibling rotations/ + packed/ sheets.
// Raw PixelLab per-frame PNGs are NOT referenced here.
//
// Supports two packed layouts:
//   - New: one PNG per animation with columns = frames, rows = directions.
//     Manifest carries `sheet` + `rowOrder`.
//   - Legacy: one PNG per (animation, direction) strip, all at row 0.
//     Manifest carries `strips: { north, south, east, west }`.
// Both normalize to the same in-memory shape, so the renderer doesn't care.

import { getUnitBoundsFor } from '../maps/unitDefs'

// Multiplier applied to each unit sprite's native size at draw time. Bump
// until sprites read clearly at common zoom-outs without swamping the UI.
export const UNIT_SPRITE_SCALE = 1.25
// PixelLab exports center a ~48px character in a ~68px canvas, so roughly
// 15% of the sprite height on each side is transparent padding. Used to
// anchor overhead UI to the visible head and to sit the selection ring
// under the visible feet instead of the canvas bottom.
export const UNIT_SPRITE_TOP_PADDING = 0.15
export const UNIT_SPRITE_BOTTOM_PADDING = 0.15

export type UnitDirection =
  | 'north'
  | 'north-east'
  | 'east'
  | 'south-east'
  | 'south'
  | 'south-west'
  | 'west'
  | 'north-west'

// Clockwise from north. Order matters: pickDirection walks neighbors of the
// requested direction outward through this list, so 8-way classifications
// fall back to the nearest cardinal on units that only ship 4-way art.
export const UNIT_DIRECTIONS: readonly UnitDirection[] = [
  'north',
  'north-east',
  'east',
  'south-east',
  'south',
  'south-west',
  'west',
  'north-west',
]

type DirectionMap<T> = Partial<Record<UnitDirection, T>>

interface SpriteManifest {
  key?: string
  size?: { width?: number; height?: number }
  rotations?: DirectionMap<string>
  animations?: Record<string, {
    frameCount?: number
    frameWidth?: number
    frameHeight?: number
    // New layout: single 2D sheet, rowOrder[i] is the direction at row i.
    sheet?: string
    rowOrder?: UnitDirection[]
    // Legacy layout: per-direction horizontal strip (row always 0).
    strips?: DirectionMap<string>
  }>
}

export interface DirectionSource {
  image: HTMLImageElement
  row: number
}

export interface StripAnimation {
  frameCount: number
  frameWidth: number
  frameHeight: number
  directions: DirectionMap<DirectionSource>
}

export interface UnitSpriteSet {
  key: string
  size: { width: number; height: number }
  rotations: DirectionMap<HTMLImageElement>
  animations: Map<string, StripAnimation>
}

export interface DrawableFrame {
  image: HTMLImageElement
  srcX: number
  srcY: number
  srcW: number
  srcH: number
}

// Sprite directories live at assets/units/<faction>/<unit>/ for base units and
// at assets/units/<faction>/<unit>/paths/<path>/ for promotion variants —
// mirroring the server catalog tree. The recursive ** glob picks up both
// layers without needing two separate patterns. The keyed unit/path id is
// always the last directory segment before sprites.json, so a single regex
// covers both.
const manifestGlob = import.meta.glob<SpriteManifest>(
  '../../assets/units/**/sprites.json',
  { eager: true, import: 'default' },
)

const rotationGlob = import.meta.glob<string>(
  '../../assets/units/**/rotations/*.png',
  { eager: true, query: '?url', import: 'default' },
)

const stripGlob = import.meta.glob<string>(
  '../../assets/units/**/packed/*.png',
  { eager: true, query: '?url', import: 'default' },
)

// Optional per-unit / per-path portrait used by UI surfaces (training queue,
// build menu, multi-select cards). Drop a `portrait.png` next to the unit's
// sprites.json (or next to a promotion variant's sprites.json) and the
// keyed lookups below will prefer it. Units without a portrait fall back to
// the south-facing rotation (see getUnitPortraitUrl / ActionIcon.drawUnit).
const portraitGlob = import.meta.glob<string>(
  '../../assets/units/**/portrait.png',
  { eager: true, query: '?url', import: 'default' },
)

// Built once at module load: lowercased unit / path id → preloaded portrait
// image. The captured id is the directory immediately containing the
// portrait, matching the same key convention as the sprite manifest globs
// (so .../human/archer/paths/marksman/portrait.png keys on 'marksman').
// Images are loaded eagerly so the canvas-based ActionIcon can draw them
// immediately when an action becomes visible.
const portraitImagesByKey = new Map<string, HTMLImageElement>()
for (const [filePath, url] of Object.entries(portraitGlob)) {
  const match = filePath.match(/\/([^/]+)\/portrait\.png$/)
  if (!match) continue
  portraitImagesByKey.set(match[1].toLowerCase(), loadImage(url))
}

// If a requested animation isn't defined for a given unit, try this alternate.
// Keeps carrying_gold workers walking normally when only some units have the
// dedicated carrying pose, instead of freezing on the idle rotation.
const ANIMATION_FALLBACK: Record<string, string> = {
  carrying_gold: 'walking',
}

const sprites = new Map<string, UnitSpriteSet>()

function loadImage(url: string): HTMLImageElement {
  const img = new Image()
  img.src = url
  return img
}

for (const [manifestPath, manifest] of Object.entries(manifestGlob)) {
  // The directory immediately containing sprites.json is the sprite key —
  // unit type for base units, path id for promotion variants. Both id
  // namespaces are globally unique, so a single capture works for either
  // layout.
  const match = manifestPath.match(/\/([^/]+)\/sprites\.json$/)
  if (!match) continue

  const key = match[1].toLowerCase()
  const unitFolder = manifestPath.slice(0, manifestPath.lastIndexOf('/'))
  const size = {
    width: manifest.size?.width ?? 64,
    height: manifest.size?.height ?? 64,
  }

  const rotations: DirectionMap<HTMLImageElement> = {}
  for (const [dir, rel] of Object.entries(manifest.rotations ?? {})) {
    if (!rel) continue
    const url = rotationGlob[`${unitFolder}/${rel}`]
    if (!url) continue
    rotations[dir as UnitDirection] = loadImage(url)
  }

  const animations = new Map<string, StripAnimation>()
  for (const [animKey, anim] of Object.entries(manifest.animations ?? {})) {
    const directions: DirectionMap<DirectionSource> = {}

    if (anim.sheet && anim.rowOrder) {
      const url = stripGlob[`${unitFolder}/${anim.sheet}`]
      if (url) {
        const image = loadImage(url)
        for (let row = 0; row < anim.rowOrder.length; row++) {
          const dir = anim.rowOrder[row]
          if (!dir) continue
          directions[dir] = { image, row }
        }
      }
    } else if (anim.strips) {
      for (const [dir, rel] of Object.entries(anim.strips)) {
        if (!rel) continue
        const url = stripGlob[`${unitFolder}/${rel}`]
        if (!url) continue
        directions[dir as UnitDirection] = { image: loadImage(url), row: 0 }
      }
    }

    if (Object.keys(directions).length === 0) continue
    animations.set(animKey.toLowerCase(), {
      frameCount: anim.frameCount ?? 1,
      frameWidth: anim.frameWidth ?? size.width,
      frameHeight: anim.frameHeight ?? size.height,
      directions,
    })
  }

  sprites.set(key, { key, size, rotations, animations })
}

// Picks the first sprite set matching any of the supplied keys (case-insensitive).
// Intended use: pass (unit.path, unit.unitType) so promoted variants win.
export function getUnitSpriteSet(...keys: Array<string | undefined | null>): UnitSpriteSet | null {
  for (const k of keys) {
    if (!k || k === 'none') continue
    const set = sprites.get(k.toLowerCase())
    if (set) return set
  }
  return null
}

// Resolves a static portrait URL for use in DOM <img> tags (training queue,
// build menu, multi-select cards). Path wins over unitType so promoted
// variants show their own art.
//
// Lookup order:
//   1. Dedicated portrait.png next to the unit's sprites.json (or next to a
//      promotion variant's sprites.json). Drop one in to override the
//      auto-fallback for that unit.
//   2. South-facing rotation (the most "portrait-like" pose), then the
//      other compass directions in turn. Keeps every unit showing
//      *something* in the UI without forcing a portrait asset.
export function getUnitPortraitUrl(path?: string, unitType?: string): string | null {
  const portrait = getUnitPortraitImage(path, unitType)
  if (portrait) return portrait.src
  const set = getUnitSpriteSet(path, unitType)
  if (!set) return null
  const img =
    set.rotations.south ??
    set.rotations.north ??
    set.rotations.east ??
    set.rotations.west
  return img?.src ?? null
}

// Companion to getUnitPortraitUrl that returns the preloaded HTMLImageElement
// instead of a URL — needed by canvas-based UI (ActionIcon.drawUnit) that
// can't use <img> directly. Returns null when no dedicated portrait exists
// for the unit / path; callers should fall back to the sprite rotations.
export function getUnitPortraitImage(path?: string, unitType?: string): HTMLImageElement | null {
  for (const k of [path, unitType]) {
    if (!k || k === 'none') continue
    const img = portraitImagesByKey.get(k.toLowerCase())
    if (img) return img
  }
  return null
}

function imageReady(img: HTMLImageElement | undefined): img is HTMLImageElement {
  return !!img && img.complete && img.naturalWidth > 0
}

// Axis-aligned bounding rect of a unit's VISIBLE body in world coordinates.
// Used by hover/click hit-testing so the interactable zone covers the whole
// sprite from head to feet rather than just a small radius at the feet
// anchor (unit.x/unit.y). `padding` grows the rect outward on all sides —
// small positive values (default 3 px) make hovering feel forgiving without
// overlapping neighboring units.
//
// Resolution order:
//   1. Sprite-backed unit → use the sprite's scaled canvas with horizontal
//      and vertical padding fractions applied to trim transparent margins.
//   2. No sprite (placeholder path) → use the def's bounds relative to
//      (unit.x, unit.y), falling back to DEFAULT_UNIT_BOUNDS when absent.
export function getUnitBodyRect(args: {
  x: number
  y: number
  unitType?: string
  path?: string
  padding?: number
}): { minX: number; minY: number; maxX: number; maxY: number } {
  const padding = args.padding ?? 3
  const bounds = getUnitBoundsFor({ path: args.path, unitType: args.unitType })
  const spriteSet = getUnitSpriteSet(args.path, args.unitType)

  if (spriteSet) {
    const spriteH = spriteSet.size.height * UNIT_SPRITE_SCALE
    const visibleBottomY = args.y + bounds.bottom - spriteH * UNIT_SPRITE_BOTTOM_PADDING
    const visibleTopY = args.y + bounds.bottom - spriteH * (1 - UNIT_SPRITE_TOP_PADDING)
    return {
      minX: args.x - bounds.halfWidth - padding,
      maxX: args.x + bounds.halfWidth + padding,
      minY: visibleTopY - padding,
      maxY: visibleBottomY + padding,
    }
  }

  return {
    minX: args.x - bounds.halfWidth - padding,
    maxX: args.x + bounds.halfWidth + padding,
    minY: args.y + bounds.top - padding,
    maxY: args.y + bounds.bottom + padding,
  }
}

export function isPointInUnitBody(
  px: number,
  py: number,
  unit: { x: number; y: number; unitType?: string; path?: string },
  padding?: number,
): boolean {
  const r = getUnitBodyRect({
    x: unit.x,
    y: unit.y,
    unitType: unit.unitType,
    path: unit.path,
    padding,
  })
  return px >= r.minX && px <= r.maxX && py >= r.minY && py <= r.maxY
}

// Walks outward from `preferred` through clockwise/counter-clockwise neighbors
// on the 8-way wheel, so a 4-direction unit asked for 'north-east' resolves to
// 'north' or 'east' rather than something on the opposite side of the sprite.
function pickDirection<T>(lookup: DirectionMap<T>, preferred: UnitDirection): T | undefined {
  const start = UNIT_DIRECTIONS.indexOf(preferred)
  if (start < 0) {
    for (const d of UNIT_DIRECTIONS) {
      const v = lookup[d]
      if (v) return v
    }
    return undefined
  }
  const n = UNIT_DIRECTIONS.length
  for (let offset = 0; offset <= n / 2; offset++) {
    const cw = lookup[UNIT_DIRECTIONS[(start + offset) % n]]
    if (cw) return cw
    if (offset === 0) continue
    const ccw = lookup[UNIT_DIRECTIONS[(start - offset + n) % n]]
    if (ccw) return ccw
  }
  return undefined
}

// Returns the drawable frame for (animation, direction, frameIndex), falling
// back to the idle rotation when the animation is missing or its strip has
// not finished decoding yet. Returns null when nothing is ready — callers
// should draw the procedural render in that case.
export function getUnitFrame(
  set: UnitSpriteSet,
  animation: string,
  direction: UnitDirection,
  frameIndex: number,
): DrawableFrame | null {
  const anim = set.animations.get(animation) ?? set.animations.get(ANIMATION_FALLBACK[animation] ?? '')
  if (anim) {
    const source = pickDirection(anim.directions, direction)
    if (source && imageReady(source.image) && anim.frameCount > 0) {
      const i = ((frameIndex % anim.frameCount) + anim.frameCount) % anim.frameCount
      return {
        image: source.image,
        srcX: i * anim.frameWidth,
        srcY: source.row * anim.frameHeight,
        srcW: anim.frameWidth,
        srcH: anim.frameHeight,
      }
    }
  }

  const idle = pickDirection(set.rotations, direction)
  if (!imageReady(idle)) return null
  return {
    image: idle,
    srcX: 0,
    srcY: 0,
    srcW: idle.naturalWidth,
    srcH: idle.naturalHeight,
  }
}
