const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

const imageCache = new Map<string, HTMLImageElement>()

export function getItemCatalogImage(
  itemId: string,
  onLoad: () => void,
): HTMLImageElement | null {
  const cached = imageCache.get(itemId)
  if (cached) {
    return cached.complete && cached.naturalWidth > 0 ? cached : null
  }
  const img = new Image()
  imageCache.set(itemId, img)
  img.addEventListener('load', onLoad, { once: true })
  img.addEventListener('error', () => imageCache.delete(itemId), { once: true })
  img.src = `${API_BASE}/catalog/items/${encodeURIComponent(itemId)}/image`
  return null
}
