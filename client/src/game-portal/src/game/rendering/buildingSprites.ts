// Eagerly resolves all building sprite URLs at build time.
// Folder name (lowercased) is the key; building.buildingType is lowercased at lookup.
const spriteUrls = import.meta.glob('../../assets/buildings/*/sprite.png', {
  eager: true,
  query: '?url',
  import: 'default',
}) as Record<string, string>

const images = new Map<string, HTMLImageElement>()

for (const [path, url] of Object.entries(spriteUrls)) {
  const match = path.match(/\/buildings\/([^/]+)\/sprite\.png$/)
  if (!match) continue
  const key = match[1].toLowerCase()
  const img = new Image()
  img.src = url
  images.set(key, img)
}

// Construction animation spritesheets. A construction.png is a horizontal
// strip of CONSTRUCTION_FRAME_COUNT equally-sized frames representing
// progress from freshly-placed (frame 0) to nearly-complete (last frame).
// Swapped to sprite.png on completion.
export const CONSTRUCTION_FRAME_COUNT = 4

// Training animation spritesheets. A training.png is a horizontal strip of
// TRAINING_FRAME_COUNT equally-sized frames that loops while a building is
// actively producing a unit (e.g. a barracks spawning a soldier). Looped on
// wall-clock so all training buildings stay phase-aligned without per-building
// state.
export const TRAINING_FRAME_COUNT = 4
const TRAINING_FPS = 6

// Damaged animation spritesheets. A damaged.png is a DAMAGED_TIER_COUNT-row
// × N-column grid. Each row is a damage tier (always 4 stages) that animates
// through its columns while HP is inside that tier's band.
//
// The column count defaults to DAMAGED_FRAMES_PER_TIER (5). A building whose
// damaged sheet uses a different number of frames per tier can override it with
// a colocated damaged.json — e.g. a static (non-animated) sheet is 4 rows × 1
// column, so its damaged.json is { "framesPerTier": 1 }. See getDamagedFramesPerTier.
export const DAMAGED_TIER_COUNT = 4
export const DAMAGED_FRAMES_PER_TIER = 5
// Per-tier loop speed. 8 fps gives a ~625ms cycle per tier — fast enough to
// read as fire/smoke animation without being distracting.
const DAMAGED_FPS = 8

const constructionUrls = import.meta.glob('../../assets/buildings/*/construction.png', {
  eager: true,
  query: '?url',
  import: 'default',
}) as Record<string, string>

const constructionImages = new Map<string, HTMLImageElement>()

for (const [path, url] of Object.entries(constructionUrls)) {
  const match = path.match(/\/buildings\/([^/]+)\/construction\.png$/)
  if (!match) continue
  const key = match[1].toLowerCase()
  const img = new Image()
  img.src = url
  constructionImages.set(key, img)
}

const trainingUrls = import.meta.glob('../../assets/buildings/*/training.png', {
  eager: true,
  query: '?url',
  import: 'default',
}) as Record<string, string>

const trainingImages = new Map<string, HTMLImageElement>()

for (const [path, url] of Object.entries(trainingUrls)) {
  const match = path.match(/\/buildings\/([^/]+)\/training\.png$/)
  if (!match) continue
  const key = match[1].toLowerCase()
  const img = new Image()
  img.src = url
  trainingImages.set(key, img)
}

const damagedUrls = import.meta.glob('../../assets/buildings/*/damaged.png', {
  eager: true,
  query: '?url',
  import: 'default',
}) as Record<string, string>

const damagedImages = new Map<string, HTMLImageElement>()

for (const [path, url] of Object.entries(damagedUrls)) {
  const match = path.match(/\/buildings\/([^/]+)\/damaged\.png$/)
  if (!match) continue
  const key = match[1].toLowerCase()
  const img = new Image()
  img.src = url
  damagedImages.set(key, img)
}

// Optional per-building override of the damaged sheet's column count. A
// damaged.json colocated with damaged.png is { "framesPerTier": N } — the
// number of animation frames (columns) in each of the 4 tier rows. Buildings
// with no damaged.json fall back to DAMAGED_FRAMES_PER_TIER (5).
type DamagedConfig = { framesPerTier?: number }

const damagedConfigs = import.meta.glob('../../assets/buildings/*/damaged.json', {
  eager: true,
}) as Record<string, { default: DamagedConfig }>

const damagedFramesPerTier = new Map<string, number>()

for (const [path, mod] of Object.entries(damagedConfigs)) {
  const match = path.match(/\/buildings\/([^/]+)\/damaged\.json$/)
  if (!match) continue
  const n = mod.default?.framesPerTier
  if (typeof n === 'number' && n >= 1) {
    damagedFramesPerTier.set(match[1].toLowerCase(), Math.floor(n))
  }
}

// Returns the number of animation frames (columns) per damage tier for the
// given building type: its damaged.json override, or DAMAGED_FRAMES_PER_TIER
// when none is configured.
export function getDamagedFramesPerTier(buildingType: string): number {
  return damagedFramesPerTier.get(buildingType.toLowerCase()) ?? DAMAGED_FRAMES_PER_TIER
}

// Returns a loaded sprite for the given building type, or null if none is
// registered or the image hasn't finished decoding yet. Callers should fall
// back to the procedural render path when this returns null.
export function getBuildingSprite(buildingType: string): HTMLImageElement | null {
  const img = images.get(buildingType.toLowerCase())
  if (!img) return null
  if (!img.complete || img.naturalWidth === 0) return null
  return img
}

// Returns the raw Image element for the given building type regardless of
// load state, or null if no sprite is registered. Use this when you need to
// attach a load listener — callers should check .complete / .naturalWidth
// before drawing.
export function getBuildingSpriteImage(buildingType: string): HTMLImageElement | null {
  return images.get(buildingType.toLowerCase()) ?? null
}

const TINT_ALPHA = 0.05
const tintCache = new Map<string, HTMLCanvasElement>()

// Returns an owner-tinted copy of the sprite, cached per (type, color).
// Returns null if the base sprite isn't loaded yet.
export function getTintedBuildingSprite(
  buildingType: string,
  ownerColor: string,
): HTMLCanvasElement | null {
  const sprite = getBuildingSprite(buildingType)
  if (!sprite) return null

  const key = `${buildingType.toLowerCase()}|${ownerColor}`
  const cached = tintCache.get(key)
  if (cached) return cached

  const canvas = document.createElement('canvas')
  canvas.width = sprite.naturalWidth
  canvas.height = sprite.naturalHeight
  const ctx = canvas.getContext('2d')
  if (!ctx) return null

  ctx.imageSmoothingEnabled = false
  ctx.drawImage(sprite, 0, 0)
  ctx.globalCompositeOperation = 'source-atop'
  ctx.globalAlpha = TINT_ALPHA
  ctx.fillStyle = ownerColor
  ctx.fillRect(0, 0, canvas.width, canvas.height)

  tintCache.set(key, canvas)
  return canvas
}

// Returns the loaded construction spritesheet for the given building type,
// or null if none is registered or the image hasn't finished decoding yet.
export function getConstructionSprite(buildingType: string): HTMLImageElement | null {
  const img = constructionImages.get(buildingType.toLowerCase())
  if (!img) return null
  if (!img.complete || img.naturalWidth === 0) return null
  return img
}

// Maps construction progress in [0, 1) to a frame index in
// [0, CONSTRUCTION_FRAME_COUNT). At progress >= 1 the caller should switch
// to the finished sprite instead of calling this.
export function getConstructionFrameIndex(progress: number): number {
  const p = Math.max(0, Math.min(0.9999, progress))
  return Math.min(CONSTRUCTION_FRAME_COUNT - 1, Math.floor(p * CONSTRUCTION_FRAME_COUNT))
}

const constructionTintCache = new Map<string, HTMLCanvasElement>()

// Returns an owner-tinted copy of the construction spritesheet, cached per
// (type, color). Returns null if the sheet isn't loaded yet.
export function getTintedConstructionSprite(
  buildingType: string,
  ownerColor: string,
): HTMLCanvasElement | null {
  const sprite = getConstructionSprite(buildingType)
  if (!sprite) return null

  const key = `${buildingType.toLowerCase()}|${ownerColor}`
  const cached = constructionTintCache.get(key)
  if (cached) return cached

  const canvas = document.createElement('canvas')
  canvas.width = sprite.naturalWidth
  canvas.height = sprite.naturalHeight
  const ctx = canvas.getContext('2d')
  if (!ctx) return null

  ctx.imageSmoothingEnabled = false
  ctx.drawImage(sprite, 0, 0)
  ctx.globalCompositeOperation = 'source-atop'
  ctx.globalAlpha = TINT_ALPHA
  ctx.fillStyle = ownerColor
  ctx.fillRect(0, 0, canvas.width, canvas.height)

  constructionTintCache.set(key, canvas)
  return canvas
}

// Returns the loaded damaged spritesheet for the given building type, or
// null if none is registered or the image hasn't finished decoding yet.
export function getDamagedSprite(buildingType: string): HTMLImageElement | null {
  const img = damagedImages.get(buildingType.toLowerCase())
  if (!img) return null
  if (!img.complete || img.naturalWidth === 0) return null
  return img
}

// Maps HP ratio (hp / maxHp, clamped to [0, 1]) to a damage tier.
//   -1 → no damage, use sprite.png
//    0 → tier 1 (90-70%)
//    1 → tier 2 (70-40%)
//    2 → tier 3 (40-20%)
//    3 → tier 4 (20-0%) — building is removed when it reaches 0
export function getDamagedTier(hpRatio: number): number {
  if (hpRatio > 0.9) return -1
  if (hpRatio > 0.7) return 0
  if (hpRatio > 0.4) return 1
  if (hpRatio > 0.2) return 2
  return 3
}

// Returns the animated frame column [0, framesPerTier) for a given wall-clock
// time in ms. All buildings share a single phase so they flicker in sync —
// cheap and avoids per-building animation state. framesPerTier defaults to
// DAMAGED_FRAMES_PER_TIER; pass a building's getDamagedFramesPerTier() so a
// single-column (static) sheet resolves to column 0 every frame.
export function getDamagedFrameIndex(
  timeMs: number,
  framesPerTier: number = DAMAGED_FRAMES_PER_TIER,
): number {
  const cols = framesPerTier >= 1 ? framesPerTier : DAMAGED_FRAMES_PER_TIER
  const frame = Math.floor((timeMs / 1000) * DAMAGED_FPS)
  return ((frame % cols) + cols) % cols
}

const damagedTintCache = new Map<string, HTMLCanvasElement>()

// Returns an owner-tinted copy of the damaged spritesheet, cached per
// (type, color). Returns null if the sheet isn't loaded yet.
export function getTintedDamagedSprite(
  buildingType: string,
  ownerColor: string,
): HTMLCanvasElement | null {
  const sprite = getDamagedSprite(buildingType)
  if (!sprite) return null

  const key = `${buildingType.toLowerCase()}|${ownerColor}`
  const cached = damagedTintCache.get(key)
  if (cached) return cached

  const canvas = document.createElement('canvas')
  canvas.width = sprite.naturalWidth
  canvas.height = sprite.naturalHeight
  const ctx = canvas.getContext('2d')
  if (!ctx) return null

  ctx.imageSmoothingEnabled = false
  ctx.drawImage(sprite, 0, 0)
  ctx.globalCompositeOperation = 'source-atop'
  ctx.globalAlpha = TINT_ALPHA
  ctx.fillStyle = ownerColor
  ctx.fillRect(0, 0, canvas.width, canvas.height)

  damagedTintCache.set(key, canvas)
  return canvas
}

// Returns the loaded training spritesheet for the given building type, or
// null if none is registered or the image hasn't finished decoding yet.
export function getTrainingSprite(buildingType: string): HTMLImageElement | null {
  const img = trainingImages.get(buildingType.toLowerCase())
  if (!img) return null
  if (!img.complete || img.naturalWidth === 0) return null
  return img
}

// Returns the animated frame column [0, TRAINING_FRAME_COUNT) for the given
// wall-clock time in ms. All training buildings share a single phase.
export function getTrainingFrameIndex(timeMs: number): number {
  const frame = Math.floor((timeMs / 1000) * TRAINING_FPS)
  return ((frame % TRAINING_FRAME_COUNT) + TRAINING_FRAME_COUNT) % TRAINING_FRAME_COUNT
}

const trainingTintCache = new Map<string, HTMLCanvasElement>()

// Returns an owner-tinted copy of the training spritesheet, cached per
// (type, color). Returns null if the sheet isn't loaded yet.
export function getTintedTrainingSprite(
  buildingType: string,
  ownerColor: string,
): HTMLCanvasElement | null {
  const sprite = getTrainingSprite(buildingType)
  if (!sprite) return null

  const key = `${buildingType.toLowerCase()}|${ownerColor}`
  const cached = trainingTintCache.get(key)
  if (cached) return cached

  const canvas = document.createElement('canvas')
  canvas.width = sprite.naturalWidth
  canvas.height = sprite.naturalHeight
  const ctx = canvas.getContext('2d')
  if (!ctx) return null

  ctx.imageSmoothingEnabled = false
  ctx.drawImage(sprite, 0, 0)
  ctx.globalCompositeOperation = 'source-atop'
  ctx.globalAlpha = TINT_ALPHA
  ctx.fillStyle = ownerColor
  ctx.fillRect(0, 0, canvas.width, canvas.height)

  trainingTintCache.set(key, canvas)
  return canvas
}
