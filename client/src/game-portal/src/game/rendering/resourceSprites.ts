// Loader for resource icon PNGs. Drop a PNG into `src/assets/resources/`
// named after the resource id (e.g. `gold.png`, `wood.png`) and it gets
// picked up automatically — Vite's eager glob registers the URL at build
// time. The MatchHud renders the PNG when present and falls back to the
// CSS gem gradient otherwise.

const urlGlob = import.meta.glob<string>(
  '../../assets/resources/*.png',
  { eager: true, query: '?url', import: 'default' },
)

const urls = new Map<string, string>()

for (const [path, url] of Object.entries(urlGlob)) {
  const match = path.match(/\/assets\/resources\/([^/]+)\.png$/)
  if (!match) continue
  urls.set(match[1].toLowerCase(), url)
}

export function getResourceIconUrl(resourceId: string): string | null {
  return urls.get(resourceId.toLowerCase()) ?? null
}

const images = new Map<string, HTMLImageElement>()

/** Returns a preloaded HTMLImageElement for canvas draws. Lazy-loads on
 *  first access and caches the element for subsequent calls. Returns null
 *  when no PNG is registered for the resource id. */
export function getResourceIconImage(resourceId: string): HTMLImageElement | null {
  const id = resourceId.toLowerCase()
  const cached = images.get(id)
  if (cached) return cached
  const url = urls.get(id)
  if (!url) return null
  const img = new Image()
  img.src = url
  images.set(id, img)
  return img
}
