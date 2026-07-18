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

// Packed rotation manifest — emitted by pack:sprites since the rotation
// consolidation pass. Single vertical strip (1 col × N rows) keyed by
// rowOrder[row] = direction. Loaded once per unit, sliced into per-
// direction Image elements at module init so the rendering / portrait
// code keeps its existing DirectionMap<HTMLImageElement> shape.
interface PackedRotations {
  sheet: string
  rowOrder: UnitDirection[]
  frameWidth: number
  frameHeight: number
}

export interface SpriteManifest {
  key?: string
  size?: { width?: number; height?: number }
  // Packed rotations sheet. Legacy per-direction file paths are no longer
  // supported by the runtime — re-run `npm run pack:sprites` to migrate.
  rotations?: PackedRotations
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
  // SUPERSEDED by the authored `attackOrigin` block (UnitDef.attackOrigin /
  // per-path attackOrigin — see unitDefs.ts's getResolvedAttackOriginFor),
  // which the beam render path now reads instead of this field. Kept
  // (parsed, typed, defaulted below) for backward compatibility with any
  // existing sprites.json, but it is CURRENTLY UNUSED — no manifest ships a
  // value for it and nothing reads UnitSpriteSet.beamOrigin at render time
  // anymore. Not removed outright to avoid an unnecessary type/parse-site
  // churn for a field with zero runtime effect either way.
  //
  // Optional world-pixel offset added to the chest anchor when a channel
  // beam (e.g. siphon_life) originates from this unit. +x = right on screen,
  // +y = down on screen. Use to nudge the beam source onto the actual
  // mid-body / weapon hand when the default chest anchor doesn't fit (tall
  // hood, offset shoulder, etc.). Applied in screen-space — does NOT rotate
  // with the unit's facing, so tune for the most common cast direction.
  beamOrigin?: { x?: number; y?: number }
}

function isPackedRotations(r: unknown): r is PackedRotations {
  if (!r || typeof r !== 'object') return false
  const obj = r as Record<string, unknown>
  return typeof obj.sheet === 'string' && Array.isArray(obj.rowOrder)
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
  // World-pixel offset applied to the chest anchor when this unit is the
  // source of a channel beam. Always present; defaults to { x: 0, y: 0 }.
  // SUPERSEDED / UNUSED — see the manifest field's doc comment above; the
  // beam render path resolves origins via getResolvedAttackOriginFor now.
  beamOrigin: { x: number; y: number }
  // Measured fraction of the sprite frame that is transparent padding above the
  // head / below the feet, computed lazily from the idle/south frame's alpha
  // channel (see getSpritePaddingFrac). Undefined until first measured; callers
  // must go through getSpritePaddingFrac, which fills it in and falls back to
  // the UNIT_SPRITE_*_PADDING constants when the image isn't decodable yet
  // (jsdom, cross-origin taint, not-yet-loaded). Replaces the old flat 15%
  // assumption so shadows sit under the real feet and overhead UI hugs the real
  // head, per-sprite, instead of one guess for every unit.
  measuredPadding?: { top: number; bottom: number }
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
//
// `casting → attacking` covers spellcasters whose sprite set lacks a dedicated
// casting sheet (e.g. the Cleric promotion variant of Acolyte): the cast
// reads as the unit's attack swing instead of a frozen rotation pose. Base
// Acolyte has its own casting.png and is unaffected by this fallback.
const ANIMATION_FALLBACK: Record<string, string> = {
  carrying_gold: 'walking',
  casting: 'attacking',
}

const sprites = new Map<string, UnitSpriteSet>()

function loadImage(url: string): HTMLImageElement {
  const img = new Image()
  img.src = url
  return img
}

// Loads a unit's packed rotations sheet and slices it into one HTMLImageElement
// per direction (via canvas → data URL). Per-direction Image placeholders are
// allocated synchronously so consumers can hold refs immediately; their src
// (and thus naturalWidth) becomes valid once the underlying sheet decodes.
// Keeps the public UnitSpriteSet.rotations: DirectionMap<HTMLImageElement>
// shape unchanged, so renderer / portrait / ActionIcon code keeps working.
//
// Also returns the raw sheet Image: the sliced per-direction placeholders are
// NOT themselves the thing that loads over the network (their `src` is a data
// URL assigned synchronously once the sheet decodes), so a decode-gate that
// only inspects `directions` would resolve/reject on placeholders that never
// got a `src` at all when the sheet hasn't decoded yet. Callers that need to
// know "is this rotation set actually ready" must await the sheet, not the
// slices — see `whenSetDecoded`.
function loadPackedRotations(
  sheetUrl: string,
  manifest: PackedRotations,
): { directions: DirectionMap<HTMLImageElement>; sheet: HTMLImageElement } {
  const dest: DirectionMap<HTMLImageElement> = {}
  for (const dir of manifest.rowOrder) {
    dest[dir] = new Image()
  }

  const sliceWhenReady = (sheet: HTMLImageElement) => {
    for (let i = 0; i < manifest.rowOrder.length; i++) {
      const dir = manifest.rowOrder[i]
      const canvas = document.createElement('canvas')
      canvas.width = manifest.frameWidth
      canvas.height = manifest.frameHeight
      const ctx = canvas.getContext('2d')
      if (!ctx) continue
      ctx.drawImage(
        sheet,
        0, i * manifest.frameHeight, manifest.frameWidth, manifest.frameHeight,
        0, 0, manifest.frameWidth, manifest.frameHeight,
      )
      const placeholder = dest[dir]
      if (placeholder) placeholder.src = canvas.toDataURL('image/png')
    }
  }

  const sheet = new Image()
  sheet.onload = () => sliceWhenReady(sheet)
  sheet.src = sheetUrl
  // Vite resolves bundled URLs at module init; the browser may have the
  // image in HTTP cache already, in which case `.complete` is already true
  // and onload won't fire. Slice synchronously in that case.
  if (sheet.complete && sheet.naturalWidth > 0) sliceWhenReady(sheet)

  return { directions: dest, sheet }
}

// Out-of-band link from a built UnitSpriteSet back to the raw packed-rotations
// sheet Image that fed it (when it has one). Keyed by object identity so it
// never leaks into the public UnitSpriteSet shape; see the comment at the end
// of buildSpriteSet and whenSetDecoded for why this exists.
const rotationSheetsBySet = new WeakMap<UnitSpriteSet, HTMLImageElement>()

// Builds a UnitSpriteSet from a manifest. `resolveUrl` maps a manifest-relative
// path (e.g. "packed/walking.png") to a loadable URL, which is the ONLY thing
// that differs between bundled art (resolved through Vite's stripGlob) and
// runtime art (resolved against the server's /assets/units/... base URL).
// Both callers share this so the two paths cannot drift.
export function buildSpriteSet(
  key: string,
  manifest: SpriteManifest,
  resolveUrl: (relative: string) => string | undefined,
): UnitSpriteSet | null {
  if (!key) return null
  const size = {
    width: manifest.size?.width ?? 64,
    height: manifest.size?.height ?? 64,
  }

  let rotations: DirectionMap<HTMLImageElement> = {}
  let rotationSheet: HTMLImageElement | undefined
  if (isPackedRotations(manifest.rotations)) {
    const sheetUrl = resolveUrl(manifest.rotations.sheet)
    if (sheetUrl) {
      const loaded = loadPackedRotations(sheetUrl, manifest.rotations)
      rotations = loaded.directions
      rotationSheet = loaded.sheet
    }
  } else if (manifest.rotations) {
    console.warn(
      `[unitSprites] ${key}: sprites.json uses the legacy per-direction rotation shape; ` +
      `re-run \`npm run pack:sprites\` to migrate to the packed-sheet layout.`,
    )
  }

  const animations = new Map<string, StripAnimation>()
  for (const [animKey, anim] of Object.entries(manifest.animations ?? {})) {
    const directions: DirectionMap<DirectionSource> = {}

    if (anim.sheet && anim.rowOrder) {
      const url = resolveUrl(anim.sheet)
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
        const url = resolveUrl(rel)
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

  const set: UnitSpriteSet = {
    key,
    size,
    rotations,
    animations,
    // Parsed for backward compatibility only — superseded/unused, see the
    // UnitSpriteSet.beamOrigin field doc comment above.
    beamOrigin: { x: manifest.beamOrigin?.x ?? 0, y: manifest.beamOrigin?.y ?? 0 },
  }
  // The sliced per-direction rotation Images don't get a `src` until the raw
  // sheet decodes (see loadPackedRotations), so a decode-gate over `rotations`
  // alone is a no-op for a rotations-only set (no animations). Stash the raw
  // sheet out-of-band so `whenSetDecoded` can gate on the thing that actually
  // loads, without adding an internal field to the public UnitSpriteSet shape.
  if (rotationSheet) rotationSheetsBySet.set(set, rotationSheet)
  return set
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
  const set = buildSpriteSet(key, manifest, (rel) => stripGlob[`${unitFolder}/${rel}`])
  if (set) sprites.set(key, set)
}

// Runtime sprite overlay — art served by the server from the writable art dir,
// which SHADOWS the build-time bundled art above. This is the client-side twin
// of the server's runtimeUnits-over-embedded-catalog model, and it is what lets
// newly-authored art appear without a rebuild.
const runtimeSprites = new Map<string, UnitSpriteSet>()
const runtimePortraits = new Map<string, HTMLImageElement>()

export function registerRuntimeSpriteSet(set: UnitSpriteSet): void {
  runtimeSprites.set(set.key.toLowerCase(), set)
}

export function clearRuntimeSpriteSets(): void {
  runtimeSprites.clear()
  runtimePortraits.clear()
}

interface UnitArtEntry {
  key: string
  baseUrl: string
  manifest: SpriteManifest
}

// Waits for every image in a set to finish decoding. We register a runtime set
// only AFTER this resolves: getUnitFrame returns null for an undecoded image and
// the renderer falls back to a procedural placeholder, so registering early would
// let a half-loaded overlay shadow perfectly-good bundled art and flash every
// unit as a placeholder. Never rejects — a broken sheet just stays not-ready.
//
// Uses HTMLImageElement.decode() rather than onload/onerror/.complete polling:
// decode() is the standard DOM API for exactly this ("resolves once the image
// is decoded and safe to paint"), it resolves immediately for an already-loaded
// image, and it rejects (caught below) for a broken one instead of leaving a
// listener that may never fire.
//
// Rotations gating note: the sliced per-direction Images in `set.rotations`
// get a real `src` (a canvas data URL) only once the RAW packed-rotations
// sheet decodes — see loadPackedRotations. Awaiting the slices alone is a
// no-op for a set built before that sheet has decoded (they have no `src`
// yet, so `decode()` just rejects immediately), which would make this gate
// silently do nothing for a rotations-only runtime set (no animations). We
// close that gap by also awaiting the source sheet via `rotationSheetsBySet`
// (set in buildSpriteSet). This does not change drawing correctness either
// way — `getUnitFrame`'s per-frame `imageReady` check already refuses to draw
// an undecoded rotation — it only restores the intended "no first-frame miss"
// property of the decode-then-register gate for rotations-only sets.
function whenSetDecoded(set: UnitSpriteSet): Promise<void> {
  const images = new Set<HTMLImageElement>()
  for (const img of Object.values(set.rotations)) if (img) images.add(img)
  for (const anim of set.animations.values()) {
    for (const src of Object.values(anim.directions)) if (src) images.add(src.image)
  }
  const rotationSheet = rotationSheetsBySet.get(set)
  if (rotationSheet) images.add(rotationSheet)
  return Promise.all([...images].map((img) => img.decode().catch(() => undefined))).then(
    () => undefined,
  )
}

// Fetches the server's writable art catalog and REPLACES the runtime overlay
// with exactly what it contains. Returns how many sets were registered. Safe
// to call more than once (the editor calls it again after saving new art, and
// again after reverting one) — including a key that was present on a previous
// call but is gone from this one, which must stop shadowing bundled art.
//
// Built into FRESH local maps, decoded, THEN swapped wholesale onto the module
// maps. This preserves the no-flash property (the current overlay stays live
// until the new one is fully decoded) while still actually dropping any key
// that vanished from the catalog — clearing the module maps up front would
// reopen the placeholder-flash window for every unit, including ones whose
// art didn't change.
//
// Never throws: art is an enhancement, and a failure here must degrade to
// bundled art rather than break the app.
export async function loadRuntimeSpriteSets(): Promise<number> {
  const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''
  let entries: UnitArtEntry[]
  try {
    const res = await fetch(`${API_BASE}/catalog/unit-art`)
    if (!res.ok) return 0
    const body = (await res.json()) as { art?: UnitArtEntry[] }
    entries = body.art ?? []
  } catch {
    return 0
  }

  const nextSprites = new Map<string, UnitSpriteSet>()
  const nextPortraits = new Map<string, HTMLImageElement>()

  for (const entry of entries) {
    const key = entry.key.toLowerCase()
    const set = buildSpriteSet(key, entry.manifest, (rel) => `${API_BASE}${entry.baseUrl}/${rel}`)
    if (!set) continue
    nextSprites.set(key, set)
    const portrait = new Image()
    portrait.src = `${API_BASE}${entry.baseUrl}/portrait.png`
    nextPortraits.set(key, portrait)
  }

  await Promise.all([...nextSprites.values()].map(whenSetDecoded))

  runtimeSprites.clear()
  for (const [k, v] of nextSprites) runtimeSprites.set(k, v)
  runtimePortraits.clear()
  for (const [k, v] of nextPortraits) runtimePortraits.set(k, v)
  return nextSprites.size
}

// Picks the first sprite set matching any of the supplied keys (case-insensitive).
// Intended use: pass (unit.path, unit.unitType) so promoted variants win.
export function getUnitSpriteSet(...keys: Array<string | undefined | null>): UnitSpriteSet | null {
  for (const k of keys) {
    if (!k || k === 'none') continue
    const lower = k.toLowerCase()
    const runtime = runtimeSprites.get(lower)
    if (runtime) return runtime
    const bundled = sprites.get(lower)
    if (bundled) return bundled
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
    const lower = k.toLowerCase()
    const runtime = runtimePortraits.get(lower)
    if (imageReady(runtime)) return runtime
    const bundled = portraitImagesByKey.get(lower)
    if (bundled) return bundled
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
    const pad = getSpritePaddingFrac(spriteSet)
    const visibleBottomY = args.y + bounds.bottom - spriteH * pad.bottom
    const visibleTopY = args.y + bounds.bottom - spriteH * (1 - pad.top)
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

// Scratch canvas reused across every padding measurement — a single hidden
// element rather than one per sprite, since measurement is one-shot per set
// (result is memoized on set.measuredPadding).
let paddingScratch: HTMLCanvasElement | null = null

// measureSpritePadding scans the idle/south frame's alpha channel to find the
// first and last rows containing any opaque pixel, returning the transparent
// padding above/below as fractions of the frame height. Returns null (caller
// falls back to the constants) when nothing can be measured: no decoded frame,
// no 2D canvas (jsdom unit tests), or a cross-origin-tainted image whose pixels
// can't be read. Deliberately uses the SAME idle/south frame the unit renders
// at rest, so the measured feet/head match what's actually on screen.
function measureSpritePadding(set: UnitSpriteSet): { top: number; bottom: number } | null {
  const frame = getUnitFrame(set, 'idle', 'south', 0)
  if (!frame) return null
  if (typeof document === 'undefined') return null
  const cv = paddingScratch ?? (paddingScratch = document.createElement('canvas'))
  cv.width = frame.srcW
  cv.height = frame.srcH
  const ctx = cv.getContext('2d')
  if (!ctx) return null
  ctx.clearRect(0, 0, cv.width, cv.height)
  ctx.drawImage(frame.image, frame.srcX, frame.srcY, frame.srcW, frame.srcH, 0, 0, frame.srcW, frame.srcH)
  let data: Uint8ClampedArray
  try {
    data = ctx.getImageData(0, 0, cv.width, cv.height).data
  } catch {
    return null // tainted canvas (cross-origin sprite without CORS)
  }
  const w = cv.width
  const h = cv.height
  let top = -1
  let bottom = -1
  for (let y = 0; y < h; y++) {
    let opaque = false
    const rowBase = y * w * 4
    for (let x = 0; x < w; x++) {
      if (data[rowBase + x * 4 + 3] > 16) {
        opaque = true
        break
      }
    }
    if (opaque) {
      if (top < 0) top = y
      bottom = y
    }
  }
  if (top < 0) return null // fully transparent — nothing to measure
  return { top: top / h, bottom: (h - 1 - bottom) / h }
}

// getSpritePaddingFrac returns the per-sprite transparent-padding fractions,
// measuring + memoizing on first call and falling back to the flat
// UNIT_SPRITE_*_PADDING constants until a real measurement is possible. This is
// the single seam every renderer/hit-test path uses to place shadows under the
// visible feet and overhead UI at the visible head — replacing the old flat
// 15% assumption that under-counted the ~26% padding these sprites actually
// carry (shadow drawn too low, health bars floated too high).
export function getSpritePaddingFrac(set: UnitSpriteSet): { top: number; bottom: number } {
  if (set.measuredPadding) return set.measuredPadding
  const measured = measureSpritePadding(set)
  if (measured) {
    set.measuredPadding = measured
    return measured
  }
  return { top: UNIT_SPRITE_TOP_PADDING, bottom: UNIT_SPRITE_BOTTOM_PADDING }
}
