// Loader for projectile sprites.
//
// Each projectile lives under assets/projectiles/<id>/sprite.png — a single
// flat frame that points along +x ("east") in its art. The server tags a
// projectile with `variant` = the projectile id (e.g. "fire_bolt", see
// ProjectileDef / Part 7); projectileSprites.ts looks the loaded image up by
// that id and draws it, letting the renderer's existing canvas rotation orient
// it to the flight direction. Drop a sprite.png into a new <id>/ folder and it
// "just works" — no manifest, no per-direction art.
//
// Loading is fire-and-forget (like effectSprites.ts): the Image is created
// immediately and the draw path skips it until its pixels have decoded
// (falling back to the procedural arrow), so there is never a blank frame.

export interface ProjectileSpriteSet {
  /** The single sprite frame (decoded or still decoding). */
  image: HTMLImageElement
}

const spriteGlob = import.meta.glob<string>(
  '../../assets/projectiles/*/sprite.png',
  { eager: true, query: '?url', import: 'default' },
)

const registry = new Map<string, ProjectileSpriteSet>()

for (const [path, url] of Object.entries(spriteGlob)) {
  const match = path.match(/\/assets\/projectiles\/([^/]+)\/sprite\.png$/)
  if (!match || !url) continue
  const img = new Image()
  img.src = url
  registry.set(match[1], { image: img })
}

// Warn-once so a missing/typo'd projectile id doesn't spam the console.
const warnedMissing = new Set<string>()

/** Returns the loaded sprite set for a projectile id, or undefined when no
 *  sprite.png is registered (caller falls back to the procedural arrow). */
export function getProjectileSpriteSet(id: string): ProjectileSpriteSet | undefined {
  const entry = registry.get(id)
  if (!entry) {
    if (!warnedMissing.has(id)) {
      warnedMissing.add(id)
      // Not necessarily an error: most projectiles are procedural by design.
      // Only worth a debug note when a server `variant` has no art.
      console.debug(`[projectileSpriteSheets] no sprite for "${id}" — using procedural draw`)
    }
    return undefined
  }
  return entry
}

/** Every projectile id that ships a sprite.png. Used to auto-register sprite
 *  draw fns so new projectile art "just works". */
export function registeredProjectileSpriteIds(): string[] {
  return [...registry.keys()]
}

/** True once an image has actually decoded and is safe to drawImage(). */
export function projectileImageReady(img: HTMLImageElement | null): img is HTMLImageElement {
  return !!img && img.complete && img.naturalWidth > 0
}
