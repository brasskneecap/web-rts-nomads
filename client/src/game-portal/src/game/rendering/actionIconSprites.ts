// Loader for action icon sprites. HUD action buttons (e.g. attack-move,
// stop, hold, build, set-spawn-point) can opt into a PNG icon by dropping
// `<actionId>.png` into `src/assets/ui/actions/`. The renderer prefers the
// sprite when present and falls back to the SVG path in ACTION_ICON_MAP.
//
// No packing step is required — these are static single-frame icons consumed
// directly. Filenames are matched case-insensitively against the action id.

const iconGlob = import.meta.glob<string>(
  '../../assets/ui/actions/*.png',
  { eager: true, query: '?url', import: 'default' },
)

const images = new Map<string, HTMLImageElement>()

function loadImage(url: string): HTMLImageElement {
  const img = new Image()
  img.src = url
  return img
}

for (const [path, url] of Object.entries(iconGlob)) {
  const match = path.match(/\/assets\/ui\/actions\/([^/]+)\.png$/)
  if (!match) continue
  images.set(match[1].toLowerCase(), loadImage(url))
}

export function getActionIconImage(actionId: string): HTMLImageElement | null {
  return images.get(actionId.toLowerCase()) ?? null
}
