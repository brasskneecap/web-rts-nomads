// Bundled art for ability (spell) action icons, plus the projectile-image
// fallback. Mirrors itemAssets.ts: assets are globbed eagerly at module init
// so lookups are synchronous.
//
// Icon resolution for a spell action cell (see ActionIcon.vue):
//   1. assets/abilities/<abilityId>/*.png   — bespoke ability art (preferred)
//   2. assets/projectiles/<projectileId>.png — the ability's projectile image
//   3. generic action-icon fallback (handled by ActionIcon)

// Ability art lives one directory deep: assets/abilities/<id>/<file>.png. The
// KEY is the directory name (the ability id), not the filename — e.g.
// assets/abilities/fireball/sprite.png → "fireball".
const abilityGlob = import.meta.glob<string>(
  '../../assets/abilities/**/*.png',
  { eager: true, query: '?url', import: 'default' },
)

// Projectile art is flat: assets/projectiles/<id>.png → key "<id>".
const projectileGlob = import.meta.glob<string>(
  '../../assets/projectiles/*.png',
  { eager: true, query: '?url', import: 'default' },
)

function loadImage(url: string): HTMLImageElement {
  const img = new Image()
  img.src = url
  return img
}

const abilityImages = new Map<string, HTMLImageElement>()
const abilityUrlsByKey = new Map<string, string>()
for (const [path, url] of Object.entries(abilityGlob)) {
  // Capture the directory name that sits directly under assets/abilities/.
  const match = path.match(/\/abilities\/([^/]+)\//)
  if (!match) continue
  abilityImages.set(match[1].toLowerCase(), loadImage(url))
  abilityUrlsByKey.set(match[1].toLowerCase(), url)
}

const projectileImages = new Map<string, HTMLImageElement>()
for (const [path, url] of Object.entries(projectileGlob)) {
  const match = path.match(/\/([^/]+)\.png$/)
  if (!match) continue
  projectileImages.set(match[1].toLowerCase(), loadImage(url))
}

// getAbilityAssetImage returns bespoke bundled art for an ability id, or null
// when none exists (the caller then falls back to the projectile image).
export function getAbilityAssetImage(abilityId: string): HTMLImageElement | null {
  return abilityImages.get(abilityId.toLowerCase()) ?? null
}

// getProjectileAssetImage returns the flat projectile art for a projectile-def
// id, or null when none is bundled.
export function getProjectileAssetImage(projectileId: string): HTMLImageElement | null {
  return projectileImages.get(projectileId.toLowerCase()) ?? null
}

// resolveAbilityIconImage applies the ability→projectile fallback order and
// returns the first bundled image found, or null when neither exists.
export function resolveAbilityIconImage(
  abilityId: string,
  projectileId?: string,
): HTMLImageElement | null {
  return (
    getAbilityAssetImage(abilityId) ??
    (projectileId ? getProjectileAssetImage(projectileId) : null)
  )
}

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''
const ABILITY_ICON_KEY_RE = /^[a-z0-9_]+$/

// Server-served (editor-uploaded) ability icons, resolved lazily by key.
const serverAbilityIconCache = new Map<string, HTMLImageElement>()
const serverAbilityIconFailed = new Set<string>()

function getServerAbilityIcon(key: string): HTMLImageElement | null {
  if (serverAbilityIconFailed.has(key)) return null
  const cached = serverAbilityIconCache.get(key)
  if (cached) return cached
  const img = new Image()
  img.addEventListener('error', () => {
    serverAbilityIconFailed.add(key)
    serverAbilityIconCache.delete(key)
  })
  img.src = `${API_BASE}/catalog/abilities/${encodeURIComponent(key)}/image`
  serverAbilityIconCache.set(key, img)
  return img
}

// getAbilityIconImageByKey resolves a chosen icon key: bundled-by-key first,
// else the server-served uploaded icon. Only a key matching the ability-id
// pattern is treated as a real key — placeholder paths (e.g. "TODO/x.png")
// return null so they never trigger a spurious server fetch.
export function getAbilityIconImageByKey(iconKey?: string): HTMLImageElement | null {
  if (!iconKey) return null
  const key = iconKey.toLowerCase()
  if (!ABILITY_ICON_KEY_RE.test(key)) return null
  const bundled = abilityImages.get(key)
  if (bundled) return bundled
  return getServerAbilityIcon(key)
}

// resolveAbilityIconImageKeyed applies the full action-bar resolution order:
// chosen key (bundled-by-key → server) → bundled-by-ability-id → projectile.
export function resolveAbilityIconImageKeyed(
  iconKey: string | undefined,
  abilityId: string,
  projectileId?: string,
): HTMLImageElement | null {
  return (
    getAbilityIconImageByKey(iconKey) ??
    getAbilityAssetImage(abilityId) ??
    (projectileId ? getProjectileAssetImage(projectileId) : null)
  )
}

// listAbilityIconKeys returns the bundled ability-icon keys (folder names),
// sorted — for the editor gallery.
export function listAbilityIconKeys(): string[] {
  return [...abilityImages.keys()].sort()
}

// getAbilityIconSourceUrl resolves a key to an <img>/canvas source URL: the
// bundled url when present, else the server-served route. For the editor.
export function getAbilityIconSourceUrl(iconKey: string): string {
  const key = iconKey.toLowerCase()
  const bundled = abilityUrlsByKey.get(key)
  if (bundled) return bundled
  return `${API_BASE}/catalog/abilities/${encodeURIComponent(key)}/image`
}
