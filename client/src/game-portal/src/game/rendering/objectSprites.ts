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
  sprites.set(key, { key, size, animations })
}

export function getObjectSpriteSet(key: string): ObjectSpriteSet | null {
  return sprites.get(key.toLowerCase()) ?? null
}
