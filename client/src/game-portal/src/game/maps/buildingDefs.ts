import type { BuildingCapability } from '../network/protocol'

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

export type BuildingDef = {
  type: string
  width: number
  height: number
  maxHp: number
  buildSeconds: number
  resourceCost: Record<string, number>
  capabilities: BuildingCapability[]
  spawnUnitTypes: string[]
  metadata: Record<string, string | number | boolean>
  color: string
  label: string
  hotkey: string
  render: BuildingRenderDef
}

export let BUILDABLE_BUILDING_DEFS: BuildingDef[] = []

export let BUILDING_DEF_MAP = new Map<string, BuildingDef>()

export function initBuildingDefs(defs: BuildingDef[]): void {
  BUILDABLE_BUILDING_DEFS = defs.map((def) => ({
    ...def,
    hotkey: def.hotkey.toLowerCase(),
  }))
  BUILDING_DEF_MAP = new Map(BUILDABLE_BUILDING_DEFS.map((def) => [def.type, def]))
}
