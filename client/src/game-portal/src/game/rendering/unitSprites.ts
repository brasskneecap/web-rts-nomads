// Loader for packed unit sprites. Consumes `sprites.json` (produced by
// `npm run pack:sprites`) and its sibling rotations/ + packed/ strips.
// Raw PixelLab per-frame PNGs are NOT referenced here — they're packed into
// horizontal strips and loaded as a single image per (animation, direction).

export type UnitDirection = 'north' | 'south' | 'east' | 'west'

export const UNIT_DIRECTIONS: readonly UnitDirection[] = ['north', 'south', 'east', 'west']

type DirectionMap<T> = Partial<Record<UnitDirection, T>>

interface SpriteManifest {
  key?: string
  size?: { width?: number; height?: number }
  rotations?: DirectionMap<string>
  animations?: Record<string, {
    frameCount?: number
    frameWidth?: number
    frameHeight?: number
    strips?: DirectionMap<string>
  }>
}

export interface StripAnimation {
  frameCount: number
  frameWidth: number
  frameHeight: number
  strips: DirectionMap<HTMLImageElement>
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

const manifestGlob = import.meta.glob<SpriteManifest>(
  '../../assets/units/*/sprites.json',
  { eager: true, import: 'default' },
)

const rotationGlob = import.meta.glob<string>(
  '../../assets/units/*/rotations/*.png',
  { eager: true, query: '?url', import: 'default' },
)

const stripGlob = import.meta.glob<string>(
  '../../assets/units/*/packed/*.png',
  { eager: true, query: '?url', import: 'default' },
)

const sprites = new Map<string, UnitSpriteSet>()

function loadImage(url: string): HTMLImageElement {
  const img = new Image()
  img.src = url
  return img
}

for (const [manifestPath, manifest] of Object.entries(manifestGlob)) {
  const match = manifestPath.match(/\/assets\/units\/([^/]+)\/sprites\.json$/)
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
    const strips: DirectionMap<HTMLImageElement> = {}
    for (const [dir, rel] of Object.entries(anim.strips ?? {})) {
      if (!rel) continue
      const url = stripGlob[`${unitFolder}/${rel}`]
      if (!url) continue
      strips[dir as UnitDirection] = loadImage(url)
    }
    if (Object.keys(strips).length === 0) continue
    animations.set(animKey.toLowerCase(), {
      frameCount: anim.frameCount ?? 1,
      frameWidth: anim.frameWidth ?? size.width,
      frameHeight: anim.frameHeight ?? size.height,
      strips,
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

function imageReady(img: HTMLImageElement | undefined): img is HTMLImageElement {
  return !!img && img.complete && img.naturalWidth > 0
}

function pickDirection<T>(lookup: DirectionMap<T>, preferred: UnitDirection): T | undefined {
  if (lookup[preferred]) return lookup[preferred]
  for (const d of UNIT_DIRECTIONS) {
    const v = lookup[d]
    if (v) return v
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
  const anim = set.animations.get(animation)
  if (anim) {
    const strip = pickDirection(anim.strips, direction)
    if (imageReady(strip) && anim.frameCount > 0) {
      const i = ((frameIndex % anim.frameCount) + anim.frameCount) % anim.frameCount
      return {
        image: strip,
        srcX: i * anim.frameWidth,
        srcY: 0,
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
