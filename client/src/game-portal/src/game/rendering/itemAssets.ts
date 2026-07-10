// Bundled icon sources: the authored item catalog art AND the icon art library
// (assets/icons/**). Both feed the same keyed maps so an icon-library asset is
// selectable in the item editor's gallery AND resolves at render time when
// assigned to an item. Keys are lowercased basenames; item and icon-library
// basenames are disjoint, so neither shadows the other.
const itemArtGlob = import.meta.glob<string>(['../../assets/items/**/*.png'], {
  eager: true,
  query: '?url',
  import: 'default',
})
// The icon library is organized into groups by top-level subdirectory
// (assets/icons/<group>/*.png). The editor gallery both enumerates and filters
// by group, so we keep this glob separate to recover each icon's group.
const iconLibraryGlob = import.meta.glob<string>(['../../assets/icons/**/*.png'], {
  eager: true,
  query: '?url',
  import: 'default',
})

const images = new Map<string, HTMLImageElement>()
const urlsByKey = new Map<string, string>()

function loadImage(url: string): HTMLImageElement {
  const img = new Image()
  img.src = url
  return img
}

for (const [path, url] of [...Object.entries(itemArtGlob), ...Object.entries(iconLibraryGlob)]) {
  const match = path.match(/\/([^/]+)\.png$/)
  if (!match) continue
  images.set(match[1].toLowerCase(), loadImage(url))
  urlsByKey.set(match[1].toLowerCase(), url)
}

// Icon-library keys grouped by their top-level subdirectory under assets/icons/.
// The editor gallery reads this to offer a per-group filter; item catalog art is
// intentionally excluded so the gallery only surfaces the icon library.
const iconKeysByGroup = new Map<string, Set<string>>()
for (const path of Object.keys(iconLibraryGlob)) {
  const groupMatch = path.match(/\/assets\/icons\/([^/]+)\//)
  const nameMatch = path.match(/\/([^/]+)\.png$/)
  if (!groupMatch || !nameMatch) continue
  const group = groupMatch[1]
  const keys = iconKeysByGroup.get(group) ?? new Set<string>()
  keys.add(nameMatch[1].toLowerCase())
  iconKeysByGroup.set(group, keys)
}

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

// serverIconCache holds lazily-created Images for iconKeys with no bundled
// asset (editor-uploaded icons served by the Go server). Canvas callers
// already guard on img.complete/naturalWidth, so a still-loading image is
// safe to return.
const serverIconCache = new Map<string, HTMLImageElement>()

// Keys whose server-icon fetch 404'd/errored: getItemAssetImage returns null
// for these (restoring the caller's placeholder fallback), and the cache entry
// is evicted so a later successful upload can be picked up after the entry is
// cleared (page reload, or a future explicit invalidation hook).
const serverIconFailed = new Set<string>()

export function getItemAssetImage(iconKey: string): HTMLImageElement | null {
  const key = iconKey.toLowerCase()
  const bundled = images.get(key)
  if (bundled) return bundled
  if (serverIconFailed.has(key)) return null
  const cached = serverIconCache.get(key)
  if (cached) return cached
  const img = new Image()
  img.addEventListener('error', () => {
    serverIconFailed.add(key)
    serverIconCache.delete(key)
  })
  img.src = `${API_BASE}/catalog/items/${encodeURIComponent(key)}/image`
  serverIconCache.set(key, img)
  return img
}

// listItemAssetKeys returns every bundled icon key, sorted — both item catalog
// art and icon-library assets. Used for render-time enumeration and tests.
export function listItemAssetKeys(): string[] {
  return [...images.keys()].sort()
}

export interface IconGroup {
  /** Top-level subdirectory name under assets/icons/ (e.g. "sword", "helm"). */
  name: string
  /** Lowercased icon keys in this group, sorted. */
  keys: string[]
}

// listIconGroups returns the icon library grouped by subdirectory, sorted by
// group name with sorted keys. The item editor gallery enumerates these and
// lets the user filter which groups are shown. Item catalog art is excluded.
export function listIconGroups(): IconGroup[] {
  return [...iconKeysByGroup.entries()]
    .map(([name, keys]) => ({ name, keys: [...keys].sort() }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

// getItemImageSourceUrl resolves an iconKey to an <img src>: the bundled
// asset URL when the build contains it, else the server-served uploaded-icon
// route (finishing the orphaned itemCatalogImages.ts stub, now deleted).
export function getItemImageSourceUrl(iconKey: string): string {
  const key = iconKey.toLowerCase()
  const bundled = urlsByKey.get(key)
  if (bundled) return bundled
  return `${API_BASE}/catalog/items/${encodeURIComponent(key)}/image`
}

// Whether a bundled item asset exists for the given icon key. Assets are globbed
// eagerly at module init, so this is a reliable synchronous check — used to fall
// back to a generic recipe icon when a tier-specific one (e.g. epic_recipe) has
// not been added yet.
export function hasItemAsset(iconKey: string): boolean {
  return images.has(iconKey.toLowerCase())
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
