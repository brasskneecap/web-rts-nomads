// Loader for transient visual effect sprite sheets.
// Each effect lives under assets/effects/<name>/ with a sprites.json manifest
// and a sibling sheet.png. The manifest describes a single-row horizontal
// frame strip (one direction, plays once, driven by EffectSnapshot.progress).
//
// Loading is fire-and-forget: the HTMLImageElement is created immediately and
// the renderer skips any effect whose image hasn't finished decoding yet.

interface EffectManifest {
  frameWidth: number
  frameHeight: number
  frames: number
  sheet: string
  // Sprite-frame pixel offsets nudging the rendered effect from the anchor
  // point. Useful when the sheet is centered on a feature that doesn't sit at
  // the unit's foot (e.g. a tornado funnel taller than the unit). Values are
  // scaled by the effect's sizeScale at draw time. Both default to 0.
  offsetX?: number
  offsetY?: number
  // displayScale sizes a unit-anchored *overlay* effect (e.g. the burning
  // flame) relative to the unit's rendered body height: 1.0 = exactly the
  // unit's height, 0.8 = 80% of it, 1.2 = spills past the silhouette. Only the
  // burning overlay reads this today; one-shot effects driven by drawEffects
  // ignore it (they use the server-supplied sizeScale). Defaults to 1.0.
  displayScale?: number
}

export interface EffectSpriteSet {
  image: HTMLImageElement | null
  frameWidth: number
  frameHeight: number
  frames: number
  offsetX: number
  offsetY: number
  displayScale: number
  loaded: boolean
}

const manifestGlob = import.meta.glob<EffectManifest>(
  '../../assets/effects/*/sprites.json',
  { eager: true, import: 'default' },
)

const sheetGlob = import.meta.glob<string>(
  '../../assets/effects/*/sheet.png',
  { eager: true, query: '?url', import: 'default' },
)

const registry = new Map<string, EffectSpriteSet>()

for (const [manifestPath, manifest] of Object.entries(manifestGlob)) {
  const match = manifestPath.match(/\/assets\/effects\/([^/]+)\/sprites\.json$/)
  if (!match) continue
  const name = match[1].toLowerCase()

  // Resolve the sheet URL from the sibling glob entry. The manifest's `sheet`
  // field names the file; we derive the glob key from the manifest's own path.
  const effectFolder = manifestPath.slice(0, manifestPath.lastIndexOf('/'))
  const sheetKey = `${effectFolder}/${manifest.sheet}`
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
    offsetX: manifest.offsetX ?? 0,
    offsetY: manifest.offsetY ?? 0,
    displayScale: manifest.displayScale ?? 1,
    // `loaded` is checked lazily at draw time via imageReady(); the flag here
    // is a quick "did we even find a sheet URL?" sentinel.
    loaded: !!sheetUrl,
  })
}

// Names we have already warned about so the console doesn't spam on every frame.
const warnedMissing = new Set<string>()

export function getEffectSprite(name: string): EffectSpriteSet | undefined {
  const lower = name.toLowerCase()
  const entry = registry.get(lower)
  if (!entry) {
    if (!warnedMissing.has(lower)) {
      warnedMissing.add(lower)
      console.warn(`[effectSprites] No sprite manifest registered for effect "${name}". Drop sprites.json + sheet.png into assets/effects/${name}/.`)
    }
    return undefined
  }
  return entry
}
