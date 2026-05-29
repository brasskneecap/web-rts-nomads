// Cursor asset registry.
//
// Authoring: drop a PNG into `src/assets/cursors/` and reference it from
// `src/assets/cursors/cursors.json`. Any key whose file does not resolve is
// silently skipped — callers fall back to the string passed to
// resolveCursor(), which lets the renderer keep its existing inline-SVG
// cursors until an art asset is authored.
//
// Manifest entry fields:
//   file      — PNG filename under src/assets/cursors/
//   hotspot   — [x, y] click point, in FINAL rendered pixels (after scaling).
//   fallback  — CSS keyword shown if the browser rejects the image.
//   size      — target render size in px (number → square, [w, h] → explicit).
//                Omit to render at the PNG's native pixel size.
//   scale     — multiplier applied to the PNG's native size. Ignored when
//                `size` is set. Useful for bulk-upscaling pixel-art sources
//                (e.g. a 16×16 PNG authored at 1× with `scale: 2` → 32×32).
//
// Scaling implementation: browsers DO NOT scale cursor images via CSS, so
// the loader pre-rasterizes each scaled cursor onto an off-screen canvas
// with nearest-neighbor sampling (matching the pixel-art aesthetic of the
// rest of the sprite pipeline) and caches a data-URL. Most browsers cap
// cursor images at 128×128 — beyond that the browser silently uses the
// fallback keyword, so keep scaled outputs within that box.
//
// Typical hotspot values: (0,0) for an arrow-style pointer, (center,center)
// for circular targeting cursors (16,16 for a 32×32 final-render cursor).

interface CursorManifestEntry {
  file: string
  hotspot?: [number, number]
  fallback?: string
  size?: number | [number, number]
  scale?: number
}

type CursorManifest = Record<string, CursorManifestEntry>

const manifestGlob = import.meta.glob<CursorManifest>(
  '../../assets/cursors/cursors.json',
  { eager: true, import: 'default' },
)

const fileGlob = import.meta.glob<string>(
  '../../assets/cursors/*.png',
  { eager: true, query: '?url', import: 'default' },
)

// Mutable at runtime: natural-size entries are written synchronously, and
// entries flagged for rescale overwrite themselves once the off-screen
// canvas finishes rendering. resolveCursor() is called once per mousemove,
// so the swap is picked up on the next frame with no explicit invalidation.
const resolved = new Map<string, string>()

type CursorChangeListener = (key: string) => void
const listeners = new Set<CursorChangeListener>()

function setResolved(key: string, css: string): void {
  resolved.set(key, css)
  for (const l of listeners) l(key)
}

// Subscribe to cursor resolution updates. Fires synchronously when a key's
// CSS value is (re)set — including the async rescale completion. Returns an
// unsubscribe function. Useful for callers that want to apply a cursor to a
// long-lived element (e.g. document.body) without polling.
export function onCursorChange(listener: CursorChangeListener): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

function buildCursorCss(url: string, hx: number, hy: number, fallback: string): string {
  return `url("${url}") ${hx} ${hy}, ${fallback}`
}

function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.onload = () => resolve(img)
    img.onerror = reject
    img.src = url
  })
}

// Rasterizes `img` to a new canvas at (w, h) using nearest-neighbor scaling
// and returns a PNG data URL suitable for a `cursor: url(...)` value.
function rasterizeScaled(img: HTMLImageElement, w: number, h: number): string {
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.round(w))
  canvas.height = Math.max(1, Math.round(h))
  const ctx = canvas.getContext('2d')
  if (!ctx) return img.src
  ctx.imageSmoothingEnabled = false
  ctx.drawImage(img, 0, 0, canvas.width, canvas.height)
  return canvas.toDataURL('image/png')
}

function resolveTargetSize(
  entry: CursorManifestEntry,
  natural: { width: number; height: number },
): [number, number] | null {
  if (entry.size !== undefined) {
    return Array.isArray(entry.size) ? [entry.size[0], entry.size[1]] : [entry.size, entry.size]
  }
  if (entry.scale !== undefined && entry.scale > 0 && entry.scale !== 1) {
    return [natural.width * entry.scale, natural.height * entry.scale]
  }
  return null
}

const manifest = Object.values(manifestGlob)[0] ?? {}
for (const [key, entry] of Object.entries(manifest)) {
  const url = fileGlob[`../../assets/cursors/${entry.file}`]
  if (!url) continue
  const hx = entry.hotspot?.[0] ?? 0
  const hy = entry.hotspot?.[1] ?? 0
  const fallback = entry.fallback ?? 'default'

  // Seed with the natural-size URL so the cursor is usable during the brief
  // window before the async rescale completes.
  setResolved(key, buildCursorCss(url, hx, hy, fallback))

  const needsRescale = entry.size !== undefined || (entry.scale !== undefined && entry.scale !== 1)
  if (!needsRescale) continue

  loadImage(url)
    .then((img) => {
      const target = resolveTargetSize(entry, { width: img.naturalWidth, height: img.naturalHeight })
      if (!target) return
      const dataUrl = rasterizeScaled(img, target[0], target[1])
      setResolved(key, buildCursorCss(dataUrl, hx, hy, fallback))
    })
    .catch(() => {
      /* leave the natural-size URL in place — better than nothing */
    })
}

// Returns the CSS `cursor` value for a registered key, or `fallback` when
// no PNG has been wired up yet for that key. Always returns a usable value
// so callers can assign the result directly to `element.style.cursor`.
export function resolveCursor(key: string, fallback: string): string {
  return resolved.get(key) ?? fallback
}
