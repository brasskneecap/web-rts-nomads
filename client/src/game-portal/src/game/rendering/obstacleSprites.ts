// Eagerly resolves all obstacle sprite URLs at build time.
// Folder name (lowercased) is the key; obstacle.obstacle is lowercased at lookup.
// Mirrors buildingSprites.ts; obstacles without a sprite.png fall back to the
// procedural colored-rect render path.
const spriteUrls = import.meta.glob('../../assets/obstacles/*/sprite.png', {
  eager: true,
  query: '?url',
  import: 'default',
}) as Record<string, string>

const images = new Map<string, HTMLImageElement>()

for (const [path, url] of Object.entries(spriteUrls)) {
  const match = path.match(/\/obstacles\/([^/]+)\/sprite\.png$/)
  if (!match) continue
  const key = match[1].toLowerCase()
  const img = new Image()
  img.src = url
  images.set(key, img)
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
