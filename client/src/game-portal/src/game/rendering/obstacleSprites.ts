// Eagerly resolves all obstacle sprite URLs at build time.
// Folder name (lowercased) is the key; obstacle.obstacle is lowercased at lookup.
// Mirrors buildingSprites.ts; obstacles without a sprite.png fall back to the
// procedural colored-rect render path.
//
// In addition to the static sprite.png, any sibling <animName>.png in the same
// folder is treated as a horizontal animation strip whose frame dimensions
// match sprite.png (frame count = sheet.width / sprite.width). Used for
// per-obstacle reactions like a tree shaking while a worker chops it.
const spriteUrls = import.meta.glob('../../assets/obstacles/*/sprite.png', {
  eager: true,
  query: '?url',
  import: 'default',
}) as Record<string, string>

const animationUrls = import.meta.glob('../../assets/obstacles/*/*.png', {
  eager: true,
  query: '?url',
  import: 'default',
}) as Record<string, string>

const images = new Map<string, HTMLImageElement>()

interface ObstacleAnimationEntry {
  image: HTMLImageElement
  frameWidth: number
  frameHeight: number
  frameCount: number
}

// Keyed by `${obstacleType}/${animName}` (both lowercased).
const animations = new Map<string, ObstacleAnimationEntry>()

for (const [path, url] of Object.entries(spriteUrls)) {
  const match = path.match(/\/obstacles\/([^/]+)\/sprite\.png$/)
  if (!match) continue
  const key = match[1].toLowerCase()
  const img = new Image()
  img.src = url
  images.set(key, img)
}

for (const [path, url] of Object.entries(animationUrls)) {
  const match = path.match(/\/obstacles\/([^/]+)\/([^/]+)\.png$/)
  if (!match) continue
  const fileStem = match[2].toLowerCase()
  if (fileStem === 'sprite') continue
  const obstacleKey = match[1].toLowerCase()
  const img = new Image()
  img.src = url
  // Frame dimensions are derived lazily once both sprite.png and the strip
  // have decoded — animation calls return null until then, matching the
  // existing "static sprite not yet ready → procedural fallback" pattern.
  animations.set(`${obstacleKey}/${fileStem}`, {
    image: img,
    frameWidth: 0,
    frameHeight: 0,
    frameCount: 0,
  })
}

export function getObstacleSprite(obstacleType: string): HTMLImageElement | null {
  const img = images.get(obstacleType.toLowerCase())
  if (!img) return null
  if (!img.complete || img.naturalWidth === 0) return null
  return img
}

export function getObstacleSpriteImage(obstacleType: string): HTMLImageElement | null {
  return images.get(obstacleType.toLowerCase()) ?? null
}

export interface ObstacleAnimationFrame {
  image: HTMLImageElement
  srcX: number
  srcY: number
  srcW: number
  srcH: number
  frameCount: number
}

// Returns the drawable frame for an obstacle animation at frame index, or null
// when the strip / its sibling sprite.png haven't finished decoding yet.
// Frame dimensions are inferred from sprite.png (canonical frame size) the
// first time both images are ready.
export function getObstacleAnimationFrame(
  obstacleType: string,
  animName: string,
  frameIndex: number,
): ObstacleAnimationFrame | null {
  const key = `${obstacleType.toLowerCase()}/${animName.toLowerCase()}`
  const entry = animations.get(key)
  if (!entry) return null
  const sprite = images.get(obstacleType.toLowerCase())
  if (!sprite || !sprite.complete || sprite.naturalWidth === 0) return null
  if (!entry.image.complete || entry.image.naturalWidth === 0) return null
  if (entry.frameCount === 0) {
    entry.frameWidth = sprite.naturalWidth
    entry.frameHeight = sprite.naturalHeight
    entry.frameCount = Math.max(1, Math.floor(entry.image.naturalWidth / sprite.naturalWidth))
  }
  const i = ((frameIndex % entry.frameCount) + entry.frameCount) % entry.frameCount
  return {
    image: entry.image,
    srcX: i * entry.frameWidth,
    srcY: 0,
    srcW: entry.frameWidth,
    srcH: entry.frameHeight,
    frameCount: entry.frameCount,
  }
}
