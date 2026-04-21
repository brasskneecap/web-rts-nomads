// Loader for packed object sprites. Objects (traps, placeables) are simpler
// than units — no per-direction variants and a fixed facing — so each
// animation is a single horizontal strip of frames indexed by frame number.
//
// Produced by `npm run pack:sprites` from `src/assets/objects/<key>/`:
//   - idle        — derived from the rotations/ folder, looped
//   - <slug>      — per-animation strip, played once by default

interface ObjectAnimationManifest {
  frameCount?: number
  frameWidth?: number
  frameHeight?: number
  sheet?: string
  loop?: boolean
  /** Milliseconds per frame. Defaults to 125 (units) / 60 (exploding) at the
   *  call site — keep the manifest free of opinions and let the renderer pick
   *  the right tempo for the animation context. */
  frameDurationMs?: number
}

interface ObjectManifest {
  key?: string
  size?: { width?: number; height?: number }
  animations?: Record<string, ObjectAnimationManifest>
  /** Per-object render scale override. Replaces the renderer's default
   *  OBJECT_SPRITE_SCALE when present. Use to shrink an oversized asset
   *  (e.g. a 64px source that should draw at 32px → scale: 0.5). */
  scale?: number
  /** Per-object positional nudges, in NATIVE sprite pixels (pre-scale).
   *  Positive X = right, positive Y = down. Scaled at render time by the
   *  effective object scale so the nudge stays proportional across zooms
   *  and scale overrides. Use when the authored art isn't centered on the
   *  intended anchor point (e.g. a barrel whose base sits 3px above the
   *  bottom of its frame → offsetY: 3 to drop it onto the ground). */
  offsetX?: number
  offsetY?: number
}

export interface ObjectAnimation {
  frameCount: number
  frameWidth: number
  frameHeight: number
  sheet: HTMLImageElement
  loop: boolean
  frameDurationMs?: number
}

export interface ObjectSpriteSet {
  key: string
  size: { width: number; height: number }
  animations: Map<string, ObjectAnimation>
  /** Optional render-scale override — when set, the renderer uses this
   *  instead of its OBJECT_SPRITE_SCALE default. */
  scale?: number
  /** Optional positional nudge, native sprite pixels. Scaled at render time. */
  offsetX?: number
  offsetY?: number
}

const manifestGlob = import.meta.glob<ObjectManifest>(
  '../../assets/objects/*/sprites.json',
  { eager: true, import: 'default' },
)

const sheetGlob = import.meta.glob<string>(
  '../../assets/objects/*/packed/*.png',
  { eager: true, query: '?url', import: 'default' },
)

const sprites = new Map<string, ObjectSpriteSet>()

function loadImage(url: string): HTMLImageElement {
  const img = new Image()
  img.src = url
  return img
}

for (const [manifestPath, manifest] of Object.entries(manifestGlob)) {
  const match = manifestPath.match(/\/assets\/objects\/([^/]+)\/sprites\.json$/)
  if (!match) continue

  const key = match[1].toLowerCase()
  const objectFolder = manifestPath.slice(0, manifestPath.lastIndexOf('/'))
  const size = {
    width: manifest.size?.width ?? 32,
    height: manifest.size?.height ?? 32,
  }

  const animations = new Map<string, ObjectAnimation>()
  for (const [animKey, anim] of Object.entries(manifest.animations ?? {})) {
    if (!anim.sheet) continue
    const url = sheetGlob[`${objectFolder}/${anim.sheet}`]
    if (!url) continue
    animations.set(animKey.toLowerCase(), {
      frameCount: anim.frameCount ?? 1,
      frameWidth: anim.frameWidth ?? size.width,
      frameHeight: anim.frameHeight ?? size.height,
      sheet: loadImage(url),
      loop: anim.loop ?? false,
      frameDurationMs: anim.frameDurationMs,
    })
  }

  if (animations.size === 0) continue
  sprites.set(key, {
    key,
    size,
    animations,
    scale: typeof manifest.scale === 'number' && manifest.scale > 0 ? manifest.scale : undefined,
    offsetX: typeof manifest.offsetX === 'number' ? manifest.offsetX : undefined,
    offsetY: typeof manifest.offsetY === 'number' ? manifest.offsetY : undefined,
  })
}

export function getObjectSpriteSet(key: string): ObjectSpriteSet | null {
  return sprites.get(key.toLowerCase()) ?? null
}
