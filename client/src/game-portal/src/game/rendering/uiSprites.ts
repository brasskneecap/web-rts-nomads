// Loader for HUD/UI element sprites. Drop a PNG into `src/assets/ui/` and
// reference it by its filename stem (case-insensitive). Examples:
//   - assets/ui/action-panel.png  → getUiSpriteUrl('action-panel')
//   - assets/ui/selection-frame.png → getUiSpriteUrl('selection-frame')
//
// `getUiSpriteUrl` returns the resolved asset URL for use as a CSS
// `background-image: url(...)` value or an <img src>; `getUiSpriteImage`
// returns a preloaded HTMLImageElement for canvas draws.
//
// Static single-frame images — no pack step required.

const urlGlob = import.meta.glob<string>(
  '../../assets/ui/*.png',
  { eager: true, query: '?url', import: 'default' },
)

const urls = new Map<string, string>()
const images = new Map<string, HTMLImageElement>()

for (const [path, url] of Object.entries(urlGlob)) {
  const match = path.match(/\/assets\/ui\/([^/]+)\.png$/)
  if (!match) continue
  urls.set(match[1].toLowerCase(), url)
}

export function getUiSpriteUrl(key: string): string | null {
  return urls.get(key.toLowerCase()) ?? null
}

export function getUiSpriteImage(key: string): HTMLImageElement | null {
  const id = key.toLowerCase()
  const cached = images.get(id)
  if (cached) return cached
  const url = urls.get(id)
  if (!url) return null
  const img = new Image()
  img.src = url
  images.set(id, img)
  return img
}
