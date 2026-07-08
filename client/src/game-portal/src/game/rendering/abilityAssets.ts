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
for (const [path, url] of Object.entries(abilityGlob)) {
  // Capture the directory name that sits directly under assets/abilities/.
  const match = path.match(/\/abilities\/([^/]+)\//)
  if (!match) continue
  abilityImages.set(match[1].toLowerCase(), loadImage(url))
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
