import type { BuildingCapability } from '../network/protocol'

// A single paint layer in a building's sprite.
// Coordinates are in cell units (1.0 = one grid cell) relative to the
// building's top-left origin, so a 2×2 building occupies a 2×2 space.
// color: 'player' means "substitute owner/player color at render time".
export type BuildingRenderLayer = {
  x: number
  y: number
  w: number
  h: number
  color: string
}

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
  BUILDABLE_BUILDING_DEFS = defs
  BUILDING_DEF_MAP = new Map(defs.map((def) => [def.type, def]))
}
