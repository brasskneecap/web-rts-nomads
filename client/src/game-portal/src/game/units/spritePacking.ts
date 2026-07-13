// Pure TypeScript reimplementation of the sheet-layout math in
// `scripts/pack-unit-sprites.mjs` (packAnimation / packRotationsSheet /
// packUnit's manifest assembly). This module computes a *blit plan* — which
// source frame goes to which destination rectangle on which sheet — and
// never touches pixels. No canvas, no DOM, no pngjs import here; those
// belong to the caller (a browser rasterizer) and to this module's test file
// (for golden conformance against the real CLI output).
//
// Keep this file's math byte-identical to the .mjs reference. If you find a
// divergence, fix this file — the .mjs script is the golden source and must
// not be edited to match this one.

import type { UnitDirection } from '../rendering/unitSprites'

// Cardinals listed first — mirrors the comment in pack-unit-sprites.mjs:
// 4-direction units pack to a row layout that's bit-identical to the
// pre-8-way pipeline; diagonals tack onto the bottom rows.
export const DIRECTION_ORDER: readonly UnitDirection[] = [
  'north',
  'south',
  'east',
  'west',
  'north-east',
  'south-east',
  'south-west',
  'north-west',
]

/** Frame pixel dimensions, keyed by the frame's source path string. Input to
 * the core — the caller decodes PNGs (or reads cached dims) and passes these
 * in, since this module never touches pixels itself. */
export interface FrameDims {
  w: number
  h: number
}

/** One source-frame -> destination-rectangle copy instruction. `srcKey` is
 * the frame's path string exactly as it appears in the metadata (e.g.
 * `rotations/south.png` or `animations/Walking-abc/north/frame_000.png`). */
export interface Blit {
  srcKey: string
  dstX: number
  dstY: number
  w: number
  h: number
}

/** The layout of a single output sheet PNG. `name` is the manifest-relative
 * sheet path (e.g. `packed/walking.png`). */
export interface SheetPlan {
  name: string
  width: number
  height: number
  blits: Blit[]
}

interface PackedRotationsManifest {
  sheet: string
  rowOrder: UnitDirection[]
  frameWidth: number
  frameHeight: number
}

interface PackedAnimationManifest {
  frameCount: number
  frameWidth: number
  frameHeight: number
  sheet: string
  rowOrder: UnitDirection[]
}

/** Manifest shape produced by this core — mirrors packUnit's manifest
 * assembly in the CLI script, minus two fields the CLI derives from
 * filesystem context that this pure function never receives:
 *   - `key` (CLI: `path.basename(unitDir)` — the containing folder's name;
 *     PixelLab's metadata.json never carries this itself). The caller (a
 *     later browser drop-zone task) knows the dropped folder's name and can
 *     attach `key` itself when assembling the final manifest to upload.
 *   - `packedAt` (timestamp — the caller/server owns timestamps).
 */
export interface SpriteManifestJSON {
  size: { width: number; height: number }
  rotations?: PackedRotationsManifest
  animations: Record<string, PackedAnimationManifest>
}

/** Loosely-typed PixelLab export metadata shape (post- or pre-normalization).
 * Matches what pack-unit-sprites.mjs reads off `meta`. */
export interface PixelLabMeta {
  character?: { size?: { width?: number; height?: number } }
  frames?: {
    rotations?: Partial<Record<UnitDirection, string>>
    animations?: Record<string, Partial<Record<UnitDirection, string[]>>>
  }
  states?: PixelLabMeta[]
}

// Normalizes the metadata.json shape across PixelLab export formats:
//   - Old/flat format: { character, frames } at the root.
//   - New "states" format: { states: [{ character, folder, frames }, ...] }.
// In the new format we use states[0] (single-state characters). Mirrors
// normalizeExportShape in pack-unit-sprites.mjs exactly.
export function normalizeExportShape(meta: PixelLabMeta): PixelLabMeta {
  if (meta && !meta.frames && Array.isArray(meta.states) && meta.states[0]) {
    return meta.states[0]
  }
  return meta
}

// "Walking-1656a518" -> "walking". Mirrors animSlugFromHashedName in
// pack-unit-sprites.mjs exactly.
export function animSlugFromHashedName(name: string): string {
  return name.split('-')[0].toLowerCase()
}

function requireDims(frameDims: Record<string, FrameDims>, key: string): FrameDims {
  const dims = frameDims[key]
  if (!dims) {
    throw new Error(`spritePacking: missing frame dims for "${key}"`)
  }
  return dims
}

// Builds the blit plan + manifest entry for one animation's 2D sheet
// (columns = frames, rows = directions). Mirrors packAnimation in
// pack-unit-sprites.mjs. Returns null when no direction has any frames
// (mirrors the CLI's `dirs.length === 0` early return).
function planAnimation(
  slug: string,
  byDir: Partial<Record<UnitDirection, string[]>>,
  frameDims: Record<string, FrameDims>,
): { manifest: PackedAnimationManifest; sheet: SheetPlan } | null {
  const dirs = DIRECTION_ORDER.filter((d) => Array.isArray(byDir[d]) && (byDir[d] as string[]).length > 0)
  if (dirs.length === 0) return null

  let frameWidth = 0
  let frameHeight = 0
  let frameCount = 0

  for (const dir of dirs) {
    const frames = byDir[dir] as string[]
    frameCount = Math.max(frameCount, frames.length)
    if (!frameWidth) {
      const first = requireDims(frameDims, frames[0])
      frameWidth = first.w
      frameHeight = first.h
    }
  }

  for (const dir of dirs) {
    for (const key of byDir[dir] as string[]) {
      const dims = requireDims(frameDims, key)
      if (dims.w !== frameWidth || dims.h !== frameHeight) {
        throw new Error(
          `frame size mismatch in ${slug}/${dir}: expected ${frameWidth}x${frameHeight}, got ${dims.w}x${dims.h}`,
        )
      }
    }
  }

  const sheetW = frameWidth * frameCount
  const sheetH = frameHeight * dirs.length
  const blits: Blit[] = []

  for (let r = 0; r < dirs.length; r++) {
    const frames = byDir[dirs[r]] as string[]
    for (let f = 0; f < frames.length; f++) {
      blits.push({
        srcKey: frames[f],
        dstX: f * frameWidth,
        dstY: r * frameHeight,
        w: frameWidth,
        h: frameHeight,
      })
    }
  }

  const outName = `${slug}.png`
  return {
    manifest: {
      frameCount,
      frameWidth,
      frameHeight,
      sheet: `packed/${outName}`,
      rowOrder: dirs,
    },
    sheet: { name: `packed/${outName}`, width: sheetW, height: sheetH, blits },
  }
}

// Builds the blit plan + manifest entry for the rotations vertical strip (1
// column, N rows). Mirrors packRotationsSheet in pack-unit-sprites.mjs.
// Returns null when there are no rotation directions declared.
function planRotations(
  rotations: Partial<Record<UnitDirection, string>>,
  frameDims: Record<string, FrameDims>,
): { manifest: PackedRotationsManifest; sheet: SheetPlan } | null {
  const dirs = DIRECTION_ORDER.filter((d) => typeof rotations[d] === 'string')
  if (dirs.length === 0) return null

  let frameWidth = 0
  let frameHeight = 0
  const blits: Blit[] = []

  for (let r = 0; r < dirs.length; r++) {
    const key = rotations[dirs[r]] as string
    const dims = requireDims(frameDims, key)
    if (!frameWidth) {
      frameWidth = dims.w
      frameHeight = dims.h
    } else if (dims.w !== frameWidth || dims.h !== frameHeight) {
      throw new Error(
        `rotation size mismatch: expected ${frameWidth}x${frameHeight}, got ${dims.w}x${dims.h} for ${dirs[r]}`,
      )
    }
    blits.push({ srcKey: key, dstX: 0, dstY: r * frameHeight, w: frameWidth, h: frameHeight })
  }

  const sheetH = frameHeight * dirs.length
  return {
    manifest: { sheet: 'packed/rotations.png', rowOrder: dirs, frameWidth, frameHeight },
    sheet: { name: 'packed/rotations.png', width: frameWidth, height: sheetH, blits },
  }
}

/** Computes the full blit plan + manifest for one unit's PixelLab export.
 * Pure arithmetic — frame dimensions are supplied by the caller (already
 * decoded), so this function never touches pixels. Mirrors packUnit's
 * manifest assembly in pack-unit-sprites.mjs, minus packedAt (timestamp-free
 * by design — the caller owns timestamps). */
export function planSpriteSheets(meta: PixelLabMeta, frameDims: Record<string, FrameDims>): PackPlan {
  const size = {
    width: meta?.character?.size?.width ?? 64,
    height: meta?.character?.size?.height ?? 64,
  }

  const sheets: SheetPlan[] = []

  let rotationsManifest: PackedRotationsManifest | undefined
  const rot = planRotations(meta?.frames?.rotations ?? {}, frameDims)
  if (rot) {
    rotationsManifest = rot.manifest
    sheets.push(rot.sheet)
  }

  const animations: Record<string, PackedAnimationManifest> = {}
  for (const [hashedName, byDir] of Object.entries(meta?.frames?.animations ?? {})) {
    const slug = animSlugFromHashedName(hashedName)
    const result = planAnimation(slug, byDir ?? {}, frameDims)
    if (!result) continue
    animations[slug] = result.manifest
    sheets.push(result.sheet)
  }

  const manifest: SpriteManifestJSON = {
    size,
    ...(rotationsManifest ? { rotations: rotationsManifest } : {}),
    animations,
  }

  return { manifest, sheets }
}

/** Result of `planSpriteSheets`: the derived manifest plus the per-sheet blit
 * plans a rasterizer needs to actually paint the packed PNGs. */
export interface PackPlan {
  manifest: SpriteManifestJSON
  sheets: SheetPlan[]
}
