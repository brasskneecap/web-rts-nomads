const itemGlob = import.meta.glob<string>(
  '../../assets/items/**/*.png',
  { eager: true, query: '?url', import: 'default' },
)

const images = new Map<string, HTMLImageElement>()

function loadImage(url: string): HTMLImageElement {
  const img = new Image()
  img.src = url
  return img
}

for (const [path, url] of Object.entries(itemGlob)) {
  const match = path.match(/\/([^/]+)\.png$/)
  if (!match) continue
  images.set(match[1].toLowerCase(), loadImage(url))
}

export function getItemAssetImage(iconKey: string): HTMLImageElement | null {
  return images.get(iconKey.toLowerCase()) ?? null
}
