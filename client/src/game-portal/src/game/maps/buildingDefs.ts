import type { BuildingCapability, JsonObject } from '../network/protocol'

// A rectangular accent layer.
// Coordinates are in cell units relative to the building's top-left origin.
// color: 'player' means "substitute owner/player color at render time".
// kind is optional for backwards compatibility with legacy defs that omit it.
export type BuildingRectLayer = {
  kind?: 'rect'
  x: number
  y: number
  w: number
  h: number
  color: string
}

// A triangle from the 6×6 sub-cell grid.
// cx/cy: cell index within the building footprint.
// sc/sr: sub-column/sub-row within that cell (0–5).
// h: 0 = upper-left triangle (above the / diagonal), 1 = lower-right.
export type BuildingTriLayer = {
  kind: 'tri'
  cx: number
  cy: number
  sc: number
  sr: number
  h: 0 | 1
  color: string
}

export type BuildingRenderLayer = BuildingRectLayer | BuildingTriLayer

// Describes how to visually paint a building.
// inset: padding from the building edge in cell units (default 0.18).
//   Used to anchor HP bars, selection rings, and construction overlays.
// layers: drawn in order, back to front.
export type BuildingRenderDef = {
  inset: number
  layers: BuildingRenderLayer[]
}

// BuildingSpriteRenderDef overrides where and how large a building sprite
// is drawn relative to its grid footprint. Mirrors the obstacle overflow
// system (cell units; omitted fields fall back to the footprint). Grid
// footprint used by pathing, placement, and selection hit-testing is
// unchanged — this only affects the drawn sprite bounding box.
export type BuildingSpriteRenderDef = {
  offsetX?: number
  offsetY?: number
  width?: number
  height?: number
}

// BuildingSelectionRingDef sizes/places the selection & hover ring relative
// to the building's grid footprint. All fields are in cell units. Omitted
// fields fall back to the footprint-derived default ring. Setting just
// radiusX/radiusY enlarges the ring while keeping the default centering.
// Mirrors the Go-side BuildingSelectionRingDef struct.
export type BuildingSelectionRingDef = {
  offsetX?: number
  offsetY?: number
  radiusX?: number
  radiusY?: number
}

// BuildingShadowDef tunes the ground shadow drawn under a building. Distance
// fields are in cell units; omitted/zero fields fall back to a footprint-
// derived default (see resolveBuildingShadow). Mirrors the Go-side struct.
export type BuildingShadowDef = {
  enabled?: boolean
  offsetX?: number
  offsetY?: number
  radiusX?: number
  radiusY?: number
  opacity?: number
}

// Resolved, ready-to-draw building shadow in pixels. center is measured from
// the footprint's top-left world corner.
export type ResolvedBuildingShadow = {
  centerX: number
  centerY: number
  radiusX: number
  radiusY: number
  opacity: number
}

export type BuildingAttackVisual = {
  kind?: 'melee' | 'projectile'
  originX?: number
  originY?: number
  effectLength?: number
}

export type ResolvedBuildingAttackVisual = {
  kind: 'melee' | 'projectile'
  originX: number
  originY: number
  effectLength: number
}

// BuildingStyleRenderDef is a per-art render override for a building that picks
// its sprite per-instance (recipe-shop, via the shopStyle metadata) rather than
// from a single shared sprite. Carries only the render fields that differ
// between art variants; the base BuildingDef stays authoritative for footprint,
// gameplay, etc. Mirrors the Go-side BuildingStyleRenderDef struct. Delivered by
// /catalog/buildings keyed by buildingType → styleName. See getBuildingStyleRender.
export type BuildingStyleRenderDef = {
  spriteRender?: BuildingSpriteRenderDef
  selectionRing?: BuildingSelectionRingDef
  shadow?: BuildingShadowDef
}

export type BuildingClass = 'player' | 'neutral' | 'enemy'

export type BuildingDef = {
  type: string
  class?: BuildingClass
  buildable?: boolean
  width: number
  height: number
  maxHp: number
  buildSeconds: number
  damage?: number
  attackRange?: number
  attackSpeed?: number
  attackVisual?: BuildingAttackVisual
  resourceType?: 'gold' | 'wood'
  resourceAmount?: number
  resourceCost: Record<string, number>
  capabilities: BuildingCapability[]
  spawnUnitTypes: string[]
  // Minimum town-hall tier the owning player must control to build this.
  // 0/omitted ⇒ no requirement. Mirrors the server's BuildingDef field.
  requiresTownhallTier?: number
  // Tier-chain fields (mirrors the server's BuildingDef). A tier def (e.g.
  // keep, castle) sets upgradesFrom to its previous tier and carries the cost
  // + duration to upgrade INTO it. A placed building keeps its base type plus a
  // numeric `tier` in metadata; these defs drive the upgrade action label/cost
  // and the tier display names. See getUpgradeChain.
  upgradesFrom?: string
  upgradeCost?: Record<string, number>
  upgradeSeconds?: number
  metadata: JsonObject
  color: string
  label: string
  hotkey: string
  spriteRender?: BuildingSpriteRenderDef
  selectionRing?: BuildingSelectionRingDef
  shadow?: BuildingShadowDef
}

export function getBuildingClass(def: BuildingDef | undefined | null): BuildingClass {
  return def?.class ?? 'player'
}

export let BUILDING_DEFS: BuildingDef[] = []
export let BUILDABLE_BUILDING_DEFS: BuildingDef[] = []

export let BUILDING_DEF_MAP = new Map<string, BuildingDef>()

export function initBuildingDefs(defs: BuildingDef[]): void {
  BUILDING_DEFS = defs.map((def) => ({
    ...def,
    hotkey: (def.hotkey ?? '').toLowerCase(),
  }))
  BUILDABLE_BUILDING_DEFS = BUILDING_DEFS.filter((def) => def.buildable !== false)
  BUILDING_DEF_MAP = new Map(BUILDING_DEFS.map((def) => [def.type, def]))
}

// Per-style render overrides keyed by buildingType → styleName. Populated from
// /catalog/buildings alongside the building defs. Empty for building types
// without per-sprite art.
export let BUILDING_STYLE_RENDER_MAP: Record<string, Record<string, BuildingStyleRenderDef>> = {}

export function initBuildingStyleRenders(
  styles: Record<string, Record<string, BuildingStyleRenderDef>> | null | undefined,
): void {
  BUILDING_STYLE_RENDER_MAP = styles ?? {}
}

// Returns the render override for a building type + style (e.g. recipe-shop +
// "druid-recipe-vendor"), or null when the type has no per-style config or the
// style is unset/unknown. Callers fall back to the base BuildingDef's render
// config. Style lookup is case-insensitive to match the sprite-stem keying.
export function getBuildingStyleRender(
  buildingType: string,
  style: string | null | undefined,
): BuildingStyleRenderDef | null {
  if (!style) return null
  const byStyle = BUILDING_STYLE_RENDER_MAP[buildingType]
  if (!byStyle) return null
  return byStyle[style] ?? byStyle[style.toLowerCase()] ?? null
}

// Resolves the tier chain rooted at rootType by following upgradesFrom links —
// e.g. getUpgradeChain('townhall') → [townhall, keep, castle]. Mirrors the
// server's upgradeChainFor: deterministic and capped at the catalog size so a
// malformed cycle can't loop forever. Returns [] if rootType is unknown.
export function getUpgradeChain(rootType: string): BuildingDef[] {
  const root = BUILDING_DEF_MAP.get(rootType)
  if (!root) return []
  const chain: BuildingDef[] = [root]
  while (chain.length <= BUILDING_DEFS.length) {
    const current = chain[chain.length - 1]
    const next = BUILDING_DEFS.find((def) => def.upgradesFrom === current.type)
    if (!next) break
    chain.push(next)
  }
  return chain
}

// Display name for a 1-based town-hall tier, sourced from the upgrade chain
// (tier 1 = Town Hall, 2 = Keep, 3 = Castle). Falls back to a generic label.
export function townHallTierName(tier: number): string {
  const chain = getUpgradeChain('townhall')
  return chain[tier - 1]?.label ?? `Tier ${tier}`
}

// Resolves the sprite/asset folder type for a placed building at a given 1-based
// tier. A placed building keeps its base (root) buildingType and carries a
// numeric `tier` in metadata; each higher tier renders with the art of the next
// link in its upgrade chain (townhall → keep → castle). Tier <= 1, an unknown
// root, or a chain shorter than the tier all fall back to rootType, so
// non-tiered buildings and unconfigured tiers keep their own art. Cached per
// (rootType, tier) since the catalog is static after load.
const tierSpriteTypeCache = new Map<string, string>()

export function spriteTypeForTier(rootType: string, tier: number): string {
  if (tier <= 1) return rootType
  const key = `${rootType}:${tier}`
  const cached = tierSpriteTypeCache.get(key)
  if (cached !== undefined) return cached
  const chain = getUpgradeChain(rootType)
  // Don't cache before the catalog has loaded (empty chain) — a later call once
  // BUILDING_DEFS is populated must be able to resolve the real tier art.
  if (chain.length === 0) return rootType
  const resolved = chain[tier - 1]?.type ?? rootType
  tierSpriteTypeCache.set(key, resolved)
  return resolved
}

// Footprint-derived shadow defaults (cell units), used when a building has no
// `shadow` block or leaves individual fields at zero. Mirrors the comment on
// the Go-side BuildingShadowDef.
const BUILDING_SHADOW_RADIUS_X_FROM_WIDTH = 0.42
const BUILDING_SHADOW_RADIUS_Y_FROM_RADIUS_X = 0.3
const BUILDING_SHADOW_CENTER_Y_FROM_HEIGHT = 0.85
const BUILDING_SHADOW_DEFAULT_OPACITY = 0.45

/**
 * Resolves a building's shadow config into concrete pixel draw params, or null
 * when disabled. `center` is measured from the footprint's top-left world
 * corner so the caller adds (worldX, worldY). Distance fields in the config are
 * in cell units; following the codebase convention for these building render
 * defs, a zero value means "use the footprint-derived default" (so 0 and
 * omitted behave identically).
 */
export function resolveBuildingShadow(
  shadow: BuildingShadowDef | undefined,
  widthCells: number,
  heightCells: number,
  cellSize: number,
): ResolvedBuildingShadow | null {
  if (shadow?.enabled === false) return null

  const radiusXCells = shadow?.radiusX || widthCells * BUILDING_SHADOW_RADIUS_X_FROM_WIDTH
  const radiusYCells = shadow?.radiusY || radiusXCells * BUILDING_SHADOW_RADIUS_Y_FROM_RADIUS_X
  const centerXCells = shadow?.offsetX || widthCells / 2
  const centerYCells = shadow?.offsetY || heightCells * BUILDING_SHADOW_CENTER_Y_FROM_HEIGHT
  const opacityRaw = shadow?.opacity || BUILDING_SHADOW_DEFAULT_OPACITY
  const opacity = opacityRaw < 0 ? 0 : opacityRaw > 1 ? 1 : opacityRaw

  return {
    centerX: centerXCells * cellSize,
    centerY: centerYCells * cellSize,
    radiusX: radiusXCells * cellSize,
    radiusY: radiusYCells * cellSize,
    opacity,
  }
}

export function getResolvedBuildingAttackVisual(
  def: BuildingDef | undefined | null,
): ResolvedBuildingAttackVisual {
  const inferredKind = (def?.attackRange ?? 0) > 80 ? 'projectile' : 'melee'
  const kind = def?.attackVisual?.kind ?? inferredKind
  const width = def?.width ?? 1
  const height = def?.height ?? 1

  return {
    kind,
    originX: Number((def?.attackVisual?.originX ?? width * 50).toFixed(2)),
    originY: Number((def?.attackVisual?.originY ?? height * 28).toFixed(2)),
    effectLength: Math.max(
      4,
      Math.round(def?.attackVisual?.effectLength ?? (kind === 'projectile' ? 24 : 10)),
    ),
  }
}
