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

// Returns a loaded sprite for the given building type, or null if none is
// registered or the image hasn't finished decoding yet. Callers should fall
// back to the procedural render path when this returns null.
export function getBuildingSprite(buildingType: string): HTMLImageElement | null {
  const img = images.get(buildingType.toLowerCase())
  if (!img) return null
  if (!img.complete || img.naturalWidth === 0) return null
  return img
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
