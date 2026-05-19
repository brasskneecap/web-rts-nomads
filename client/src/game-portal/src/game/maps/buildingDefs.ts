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
  metadata: JsonObject
  color: string
  label: string
  hotkey: string
  render?: BuildingRenderDef
  spriteRender?: BuildingSpriteRenderDef
  selectionRing?: BuildingSelectionRingDef
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
