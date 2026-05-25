// Loader for sustained channel-beam sprite sheets (e.g. siphon_life).
//
// Mirrors effectSprites.ts but with two beam-specific extensions:
//
//   1. frameDurationMs — beams loop indefinitely while a channel is active, so
//      frame index advances from performance.now() rather than a 0→1 progress
//      value supplied by the server.
//
//   2. axisRotation / headOnRight — the source art may be painted along a
//      diagonal axis (artists often draw beams pointing up-right rather than
//      straight horizontal). The renderer aligns the painted axis with the
//      actual caster→target vector using these hints so the sprite stretches
//      cleanly along the beam line.
//
// Each beam lives under assets/beams/<variant>/ with a sprites.json + sheet.png.

interface BeamManifest {
  frameWidth: number
  frameHeight: number
  frames: number
  sheet: string
  // Milliseconds per animation frame while the beam is channeling. Default 80.
  frameDurationMs?: number
  // Angle (in degrees) of the painted beam axis within each source frame,
  // in canvas convention: 0 = points right (+X), +90 = points down, -90 = up.
  // The renderer rotates by (caster→target angle − axisRotation) so the
  // painted axis ends up aligned with the actual beam direction.
  axisRotation?: number
  // True if the beam's "head" (splash / impact end) is on the RIGHT side of
  // the source frame, false if it's on the LEFT. When false, the renderer
  // mirrors the frame horizontally before stretching so the head always lands
  // on the target side. Default true.
  headOnRight?: boolean
  // Rendered thickness in world pixels. If omitted, defaults to frameHeight
  // (draw at native pixel size). Set this when the source art is taller than
  // the desired in-game beam thickness — e.g. a 56px source rendered at 22px
  // gives a slimmer, less screen-dominating beam without re-exporting the PNG.
  displayHeight?: number
}

export interface BeamSpriteSet {
  image: HTMLImageElement | null
  frameWidth: number
  frameHeight: number
  frames: number
  frameDurationMs: number
  axisRotation: number
  headOnRight: boolean
  displayHeight: number
  loaded: boolean
}

const manifestGlob = import.meta.glob<BeamManifest>(
  '../../assets/beams/*/sprites.json',
  { eager: true, import: 'default' },
)

const sheetGlob = import.meta.glob<string>(
  '../../assets/beams/*/sheet.png',
  { eager: true, query: '?url', import: 'default' },
)

const registry = new Map<string, BeamSpriteSet>()

for (const [manifestPath, manifest] of Object.entries(manifestGlob)) {
  const match = manifestPath.match(/\/assets\/beams\/([^/]+)\/sprites\.json$/)
  if (!match) continue
  const name = match[1].toLowerCase()

  const beamFolder = manifestPath.slice(0, manifestPath.lastIndexOf('/'))
  const sheetKey = `${beamFolder}/${manifest.sheet}`
  const sheetUrl = sheetGlob[sheetKey]

  let image: HTMLImageElement | null = null
  if (sheetUrl) {
    image = new Image()
    image.src = sheetUrl
  }

  registry.set(name, {
    image,
    frameWidth: manifest.frameWidth,
    frameHeight: manifest.frameHeight,
    frames: manifest.frames,
    frameDurationMs: manifest.frameDurationMs ?? 80,
    axisRotation: manifest.axisRotation ?? 0,
    headOnRight: manifest.headOnRight ?? true,
    displayHeight: manifest.displayHeight ?? manifest.frameHeight,
    loaded: !!sheetUrl,
  })
}

const warnedMissing = new Set<string>()

export function getBeamSprite(name: string): BeamSpriteSet | undefined {
  const lower = name.toLowerCase()
  const entry = registry.get(lower)
  if (!entry) {
    if (!warnedMissing.has(lower)) {
      warnedMissing.add(lower)
      console.warn(`[beamSprites] No sprite manifest registered for beam "${name}". Drop sprites.json + sheet.png into assets/beams/${name}/.`)
    }
    return undefined
  }
  return entry
}
