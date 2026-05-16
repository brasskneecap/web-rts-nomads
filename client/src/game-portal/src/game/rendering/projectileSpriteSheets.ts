// Loader for baked projectile rotation sprites.
//
// Each projectile lives under assets/projectiles/<id>/ with a sprites.json
// manifest (mirrors the unit `rotations` shape) and the rotation PNGs it
// points at. The server tags a projectile with `variant` = the projectile id
// (e.g. "fire_bolt", see ProjectileDef / Part 7); projectileSprites.ts looks
// the loaded set up by that id and draws it.
//
// Loading is fire-and-forget (like effectSprites.ts): Images are created
// immediately and the draw path skips any whose pixels haven't decoded yet
// (falling back to the procedural arrow), so there is never a blank frame.
//
// All 8 rotations are loaded even though the current draw model uses only the
// forward (+x / "east") frame and lets the renderer's existing canvas
// rotation orient it. Keeping every rotation resident makes switching to a
// baked 8-way draw a one-line change in projectileSprites.ts if the single
// rotated frame ever looks wrong at some angles.

export type ProjectileDirection =
  | 'north'
  | 'north-east'
  | 'east'
  | 'south-east'
  | 'south'
  | 'south-west'
  | 'west'
  | 'north-west'

export const PROJECTILE_DIRECTIONS: ProjectileDirection[] = [
  'north', 'north-east', 'east', 'south-east',
  'south', 'south-west', 'west', 'north-west',
]

interface ProjectileManifest {
  key?: string
  size?: { width: number; height: number }
  rotations?: Partial<Record<ProjectileDirection, string>>
}

export interface ProjectileSpriteSet {
  /** Native frame size from the manifest (px). Falls back to 48×48. */
  width: number
  height: number
  /** Decoded (or decoding) image per direction. */
  rotations: Partial<Record<ProjectileDirection, HTMLImageElement>>
  /** The +x ("east") frame — the one drawn under the single-sprite model.
   *  Falls back to any available rotation so a partial set still renders. */
  forward: HTMLImageElement | null
}

const manifestGlob = import.meta.glob<ProjectileManifest>(
  '../../assets/projectiles/*/sprites.json',
  { eager: true, import: 'default' },
)

const pngGlob = import.meta.glob<string>(
  '../../assets/projectiles/**/*.png',
  { eager: true, query: '?url', import: 'default' },
)

const registry = new Map<string, ProjectileSpriteSet>()

function loadImage(url: string): HTMLImageElement {
  const img = new Image()
  img.src = url
  return img
}

for (const [manifestPath, manifest] of Object.entries(manifestGlob)) {
  const match = manifestPath.match(/\/assets\/projectiles\/([^/]+)\/sprites\.json$/)
  if (!match) continue
  const id = match[1]
  const folder = manifestPath.slice(0, manifestPath.lastIndexOf('/'))

  const rotations: Partial<Record<ProjectileDirection, HTMLImageElement>> = {}
  for (const [dir, rel] of Object.entries(manifest.rotations ?? {})) {
    if (!rel) continue
    // Manifest paths are relative to the manifest folder, like
    // "states/<state>/rotations/east.png" — resolve against the png glob key.
    const url = pngGlob[`${folder}/${rel}`]
    if (url) rotations[dir as ProjectileDirection] = loadImage(url)
  }

  const forward =
    rotations.east ??
    rotations['south-east'] ??
    rotations['north-east'] ??
    Object.values(rotations)[0] ??
    null

  registry.set(id, {
    width: manifest.size?.width ?? 48,
    height: manifest.size?.height ?? 48,
    rotations,
    forward,
  })
}

// Warn-once so a missing/typo'd projectile id doesn't spam the console.
const warnedMissing = new Set<string>()

/** Returns the loaded sprite set for a projectile id, or undefined when no
 *  manifest is registered (caller falls back to the procedural arrow). */
export function getProjectileSpriteSet(id: string): ProjectileSpriteSet | undefined {
  const entry = registry.get(id)
  if (!entry) {
    if (!warnedMissing.has(id)) {
      warnedMissing.add(id)
      // Not necessarily an error: most projectiles are procedural by design.
      // Only worth a debug note when a server `variant` has no art.
      console.debug(`[projectileSpriteSheets] no sprite manifest for "${id}" — using procedural draw`)
    }
    return undefined
  }
  return entry
}

/** Every projectile id that has a loaded sprite manifest. Used to
 *  auto-register sprite draw fns so new projectile art "just works". */
export function registeredProjectileSpriteIds(): string[] {
  return [...registry.keys()]
}

/** True once an image has actually decoded and is safe to drawImage(). */
export function projectileImageReady(img: HTMLImageElement | null): img is HTMLImageElement {
  return !!img && img.complete && img.naturalWidth > 0
}
