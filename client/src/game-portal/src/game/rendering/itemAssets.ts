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

// CSS cursor strings built from item art, used while an item is armed for
// ground-AoE targeting so the cursor IS the item being aimed. The source art
// is redrawn onto a 32×32 canvas (browsers reject/clip large cursor images)
// with the hotspot at the center — the AoE resolves around the cursor point.
// Cached per icon key; returns null while the source image is still loading
// (callers fall back to the target reticle and retry on the next mousemove).
const CURSOR_SIZE = 32
const cursorCssCache = new Map<string, string>()

export function getItemCursorCss(iconKey: string): string | null {
  const key = iconKey.toLowerCase()
  const cached = cursorCssCache.get(key)
  if (cached) return cached
  const img = images.get(key)
  if (!img || !img.complete || img.naturalWidth === 0) return null
  const canvas = document.createElement('canvas')
  canvas.width = CURSOR_SIZE
  canvas.height = CURSOR_SIZE
  const ctx = canvas.getContext('2d')
  if (!ctx) return null
  ctx.imageSmoothingEnabled = false
  ctx.drawImage(img, 0, 0, CURSOR_SIZE, CURSOR_SIZE)
  const css = `url(${canvas.toDataURL()}) ${CURSOR_SIZE / 2} ${CURSOR_SIZE / 2}, crosshair`
  cursorCssCache.set(key, css)
  return css
}
